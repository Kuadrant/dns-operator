/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plan

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"

	"sigs.k8s.io/external-dns/endpoint"
	externaldnsplan "sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
)

var (
	ErrInvalidTarget      = errors.New("invalid target")
	ErrOwnerConflict      = errors.New("owner conflict")
	ErrRecordTypeConflict = errors.New("record type conflict")
)

// PropertyComparator is used in Plan for comparing the previous and current custom annotations.
type PropertyComparator func(name string, previous string, current string) bool

// Plan can convert a list of desired and current records to a series of create,
// update and delete actions.
type Plan struct {
	// List of current records
	Current []*endpoint.Endpoint
	// List of the records that were successfully resolved by this instance previously.
	Previous []*endpoint.Endpoint
	// List of desired records
	Desired []*endpoint.Endpoint
	// Policies under which the desired changes are calculated
	Policies []Policy
	// List of changes necessary to move towards desired state
	// Populated after calling Calculate()
	Changes *externaldnsplan.Changes
	// DomainFilter matches DNS names
	DomainFilter endpoint.MatchAllDomainFilters
	// ManagedRecords are DNS record types that will be considered for management.
	ManagedRecords []string
	// ExcludeRecords are DNS record types that will be excluded from management.
	ExcludeRecords []string
	// OwnerID of records to manage
	OwnerID string
	// Errors list of errors describing issues that can't be resolved by the plan.
	// Populated after calling Calculate()
	Errors []error
	// RootHost the host dns name being managed by the set of records in the plan.
	RootHost *string
	// Owners list of owners ids contributing to this record set.
	// Populated after calling Calculate()
	Owners []string

	logger logr.Logger

	registry registry.Registry
}

// NewPlan returns new Plan object
func NewPlan(ctx context.Context, current []*endpoint.Endpoint, previous []*endpoint.Endpoint, desired []*endpoint.Endpoint,
	policies []Policy, domainFilter endpoint.MatchAllDomainFilters, managedRecords []string, excludeRecords []string,
	rootHost *string, registry registry.Registry) *Plan {
	logger := logr.FromContextOrDiscard(ctx).
		WithName("plan")
	logger.V(1).Info("initializing plan", "ownerID", registry.OwnerID(), "rooHost", rootHost, "policies", policies, "domainFilter", domainFilter)

	return &Plan{
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		Policies:       policies,
		DomainFilter:   domainFilter,
		ManagedRecords: managedRecords,
		ExcludeRecords: excludeRecords,
		OwnerID:        registry.OwnerID(),
		RootHost:       rootHost,
		logger:         logger,
		registry:       registry,
	}
}

// planKey is a key for a row in `planTable`.
type planKey struct {
	dnsName       string
	setIdentifier string
}

// planTable is a supplementary struct for Plan
// each row correspond to a planKey -> (current records + all desired records)
//
//	planTable (-> = target)
//	--------------------------------------------------------------
//	DNSName | Current record       | Desired Records             |
//	--------------------------------------------------------------
//	foo.com | [->1.1.1.1 ]         | [->1.1.1.1]                 |  = no action
//	--------------------------------------------------------------
//	bar.com |                      | [->191.1.1.1, ->190.1.1.1]  |  = create (bar.com [-> 190.1.1.1])
//	--------------------------------------------------------------
//	dog.com | [->1.1.1.2]          |                             |  = delete (dog.com [-> 1.1.1.2])
//	--------------------------------------------------------------
//	cat.com | [->::1, ->1.1.1.3]   | [->1.1.1.3]                 |  = update old (cat.com [-> ::1, -> 1.1.1.3]) new (cat.com [-> 1.1.1.3])
//	--------------------------------------------------------------
//	big.com | [->1.1.1.4]          | [->ing.elb.com]             |  = update old (big.com [-> 1.1.1.4]) new (big.com [-> ing.elb.com])
//	--------------------------------------------------------------
//	"=", i.e. result of calculation relies on supplied ConflictResolver
type planTable struct {
	rows     map[planKey]*planTableRow
	resolver ConflictResolver
}

func newPlanTable(packer registry.LabelsPacker) planTable { // TODO: make resolver configurable
	return planTable{map[planKey]*planTableRow{}, PerResource{
		packer: packer,
	}}
}

// planTableRow represents a set of current and desired domain resource records.
type planTableRow struct {
	// current corresponds to the records currently occupying dns name on the dns provider. More than one record may
	// be represented here: for example A and AAAA. If the current domain record is a CNAME, no other record types
	// are allowed per [RFC 1034 3.6.2]
	//
	// [RFC 1034 3.6.2]: https://datatracker.ietf.org/doc/html/rfc1034#autoid-15
	current []*endpoint.Endpoint
	// previous corresponds to the list of records that were last used to create/update this dnsName.
	previous []*endpoint.Endpoint
	// candidates corresponds to the list of records which would like to have this dnsName.
	candidates []*endpoint.Endpoint
	// records is a grouping of current and candidates by record type, for example A, AAAA, CNAME.
	records map[string]*domainEndpoints
}

// domainEndpoints is a grouping of current, which are existing records from the registry, and candidates,
// which are desired records from the source. All records in this grouping have the same record type.
type domainEndpoints struct {
	// current corresponds to existing record from the registry. Maybe nil if no current record of the type exists.
	current *endpoint.Endpoint
	// previous corresponds to the record which was previously used doe dnsName during the last create/update.
	previous *endpoint.Endpoint
	// candidates corresponds to the list of records which would like to have this dnsName.
	candidates []*endpoint.Endpoint
}

func (t planTableRow) String() string {
	return fmt.Sprintf("planTableRow{current=%v, candidates=%v}", t.current, t.candidates)
}

func (t planTable) addCurrent(e *endpoint.Endpoint) {
	key := t.newPlanKey(e)
	t.rows[key].current = append(t.rows[key].current, e)
	t.rows[key].records[e.RecordType].current = e
}

func (t planTable) addPrevious(e *endpoint.Endpoint) {
	key := t.newPlanKey(e)
	t.rows[key].previous = append(t.rows[key].previous, e)
	t.rows[key].records[e.RecordType].previous = e
}

func (t planTable) addCandidate(e *endpoint.Endpoint) {
	key := t.newPlanKey(e)
	t.rows[key].candidates = append(t.rows[key].candidates, e)
	t.rows[key].records[e.RecordType].candidates = append(t.rows[key].records[e.RecordType].candidates, e)
}

func (t *planTable) newPlanKey(e *endpoint.Endpoint) planKey {
	key := planKey{
		dnsName:       normalizeDNSName(e.DNSName),
		setIdentifier: e.SetIdentifier,
	}

	if _, ok := t.rows[key]; !ok {
		t.rows[key] = &planTableRow{
			records: make(map[string]*domainEndpoints),
		}
	}

	if _, ok := t.rows[key].records[e.RecordType]; !ok {
		t.rows[key].records[e.RecordType] = &domainEndpoints{}
	}

	return key
}

// Error returns all Errors as a single error or nil if there are none.
func (p *Plan) Error() error {
	return errors.Join(p.Errors...)
}

// Calculate computes the actions needed to move current state towards desired
// state. It then passes those changes to the current policy for further
// processing. It returns a copy of Plan with the changes populated.
func (p *Plan) Calculate() *Plan {
	t := newPlanTable(p.registry.GetLabelsPacker())

	var errs []error
	var rootDomainFilter endpoint.DomainFilter

	if p.DomainFilter == nil {
		p.DomainFilter = endpoint.MatchAllDomainFilters(nil)
	}

	if p.RootHost != nil {
		rootDomainName, _ := strings.CutPrefix(*p.RootHost, v1alpha1.WildcardPrefix)
		rootDomainFilter = endpoint.NewDomainFilter([]string{rootDomainName})
		p.DomainFilter = append(p.DomainFilter, &rootDomainFilter)
	}

	for _, current := range p.filterRecordsForPlan(p.Current) {
		t.addCurrent(current)
	}
	for _, previous := range p.filterRecordsForPlan(p.Previous) {
		t.addPrevious(previous)
	}
	for _, desired := range p.filterRecordsForPlan(p.Desired) {
		t.addCandidate(desired)
	}

	managedChanges := managedRecordSetChanges{
		ownerID:          p.OwnerID,
		rootDomainFilter: rootDomainFilter,
		creates:          []*endpoint.Endpoint{},
		deletes:          []*endpoint.Endpoint{},
		updates:          []*endpointUpdate{},
		dnsNameOwners:    map[string][]string{},
		errors:           []error{},
		logger:           p.logger,
	}

	for key, row := range t.rows {
		if _, ok := managedChanges.dnsNameOwners[key.dnsName]; !ok {
			managedChanges.dnsNameOwners[key.dnsName] = []string{}
		}

		// dns name not taken (Create)
		if len(row.current) == 0 {
			recordsByType := t.resolver.ResolveRecordTypes(key, row)
			for _, records := range recordsByType {
				if len(records.candidates) > 0 {
					managedChanges.creates = append(managedChanges.creates, t.resolver.ResolveCreate(records.candidates))
					managedChanges.dnsNameOwners[key.dnsName] = []string{p.OwnerID}
				}
			}
		}

		// dns name released or possibly owned by a different external dns (Delete)
		if len(row.current) > 0 && len(row.candidates) == 0 {
			recordsByType := t.resolver.ResolveRecordTypes(key, row)
			for _, records := range recordsByType {
				if records.current != nil {
					candidate := records.current.DeepCopy()
					owners := []string{}

					if p.OwnerID != "" {
						delete(candidate.Labels, p.OwnerID)
						for owner := range candidate.Labels {
							if owner != "" {
								owners = append(owners, owner)
							}
						}
					}

					if len(owners) == 0 {
						managedChanges.deletes = append(managedChanges.deletes, records.current)
					} else {
						managedChanges.updates = append(managedChanges.updates, &endpointUpdate{desired: candidate, current: records.current, previous: records.previous, isDelete: true})
						managedChanges.dnsNameOwners[key.dnsName] = append(managedChanges.dnsNameOwners[key.dnsName], owners...)
					}
				}
			}
		}

		// dns name is taken (Update)
		if len(row.current) > 0 && len(row.candidates) > 0 {
			// apply changes for each record type
			var rTypeUpdate endpointUpdate
			recordsByType := t.resolver.ResolveRecordTypes(key, row)
			for _, records := range recordsByType {

				// record type not desired
				if records.current != nil && len(records.candidates) == 0 {
					rTypeUpdate.current = records.current
				}
				// new record type desired
				if records.current == nil && len(records.candidates) > 0 {
					rTypeUpdate.desired = records.candidates[0]
				}

				// update existing record
				if records.current != nil && len(records.candidates) > 0 {
					candidate := t.resolver.ResolveUpdate(records.current, p.OwnerID, records.candidates)
					current := records.current.DeepCopy()
					owners := []string{}

					if _, hasEmptyOwner := current.Labels[""]; len(current.Labels) > 0 && !hasEmptyOwner {
						if p.OwnerID == "" {
							// Only allow owned records to be updated by other owned records
							errs = append(errs, fmt.Errorf("%w, cannot update endpoint '%s' with no owner when existing endpoint is already owned", ErrOwnerConflict, candidate.DNSName))
							continue
						}

						if _, alreadyOwned := current.Labels[p.OwnerID]; !alreadyOwned {
							current.Labels[p.OwnerID] = ""
						}
					} else {
						if p.OwnerID != "" {
							// Only allow unowned records to be updated by other unowned records
							errs = append(errs, fmt.Errorf("%w, cannot update endpoint '%s' with owner when existing endpoint is not owned", ErrOwnerConflict, candidate.DNSName))
							continue
						}
					}

					for owner := range current.Labels {
						if owner != "" {
							owners = append(owners, owner)
						}
					}
					candidate.Labels = current.Labels
					managedChanges.updates = append(managedChanges.updates, &endpointUpdate{desired: candidate, current: records.current, previous: records.previous, isDelete: false})
					managedChanges.dnsNameOwners[key.dnsName] = append(managedChanges.dnsNameOwners[key.dnsName], owners...)
				}
			}

			if rTypeUpdate.current != nil && rTypeUpdate.desired != nil {
				errs = append(errs, fmt.Errorf("%w, cannot update endpoint '%s' with record type '%s' when endpoint already exists with record type '%s'",
					ErrRecordTypeConflict, rTypeUpdate.current.DNSName, rTypeUpdate.desired.RecordType, rTypeUpdate.current.RecordType))
			}
		}

		slices.Sort(managedChanges.dnsNameOwners[key.dnsName])
		managedChanges.dnsNameOwners[key.dnsName] = slices.Compact[[]string, string](managedChanges.dnsNameOwners[key.dnsName])
	}

	changes := managedChanges.Calculate()
	errs = append(errs, managedChanges.errors...)

	for _, pol := range p.Policies {
		changes = pol.Apply(changes)
	}

	// filter out updates this external dns does not have ownership claim over
	if p.OwnerID != "" {
		changes.Delete = p.registry.FilterEndpointsByOwnerID(p.OwnerID, changes.Delete)
		//ToDo Ideally we would still be able to ensure ownership on update
		//changes.UpdateOld = endpoint.FilterEndpointsByOwnerID(p.OwnerID, changes.UpdateOld)
		//changes.UpdateNew = endpoint.FilterEndpointsByOwnerID(p.OwnerID, changes.UpdateNew)
	}

	plan := &Plan{
		Current:        p.Current,
		Desired:        p.Desired,
		Changes:        changes,
		Errors:         errs,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	if p.RootHost != nil {
		p.logger.V(1).Info("plan", "record dnsOwners", managedChanges.dnsNameOwners, "record rootHost", *p.RootHost, "record normalized", normalizeDNSName(*p.RootHost))

		plan.Owners = managedChanges.dnsNameOwners[normalizeDNSName(*p.RootHost)]

	}

	return plan
}

// filterRecordsForPlan removes records that are not relevant to the planner.
// Currently, this just removes TXT records to prevent them from being
// deleted erroneously by the planner (only the TXT registry should do this.)
//
// Per RFC 1034, CNAME records conflict with all other records - it is the
// only record with this property. The behavior of the planner may need to be
// made more sophisticated to codify this.
func (p *Plan) filterRecordsForPlan(records []*endpoint.Endpoint) []*endpoint.Endpoint {
	filtered := []*endpoint.Endpoint{}

	for _, record := range records {
		// Ignore records that do not match the plan domain filter
		if !p.DomainFilter.Match(record.DNSName) {
			p.logger.V(1).Info(fmt.Sprintf("ignoring record %s that does not match domain filter", record.DNSName))
			continue
		}
		if IsManagedRecord(record.RecordType, p.ManagedRecords, p.ExcludeRecords) {
			filtered = append(filtered, record)
		}
	}

	return filtered
}

type endpointUpdate struct {
	isDelete bool
	current  *endpoint.Endpoint
	previous *endpoint.Endpoint
	desired  *endpoint.Endpoint
}

func (e *endpointUpdate) ShouldUpdate() bool {
	return shouldUpdateOwner(e.desired, e.current) || shouldUpdateTTL(e.desired, e.current) || targetChanged(e.desired, e.current) || shouldUpdateProviderSpecific(e.desired, e.current)
}

func (e *endpointUpdate) IsDeleting() bool {
	return e.isDelete
}

type managedRecordSetChanges struct {
	ownerID          string
	rootDomainFilter endpoint.DomainFilter
	creates          []*endpoint.Endpoint
	deletes          []*endpoint.Endpoint
	updates          []*endpointUpdate
	dnsNameOwners    map[string][]string
	errors           []error
	logger           logr.Logger
}

func (e *managedRecordSetChanges) Calculate() *externaldnsplan.Changes {
	changes := &externaldnsplan.Changes{
		Delete: e.deletes,
	}

	for _, ep := range e.creates {
		if err := e.validTargets(ep); err != nil {
			e.errors = append(e.errors, err)
		} else {
			changes.Create = append(changes.Create, ep)
		}
	}

	for _, update := range e.updates {
		e.calculateDesired(update)
		if update.ShouldUpdate() {
			if !update.IsDeleting() {
				if err := e.validTargets(update.desired); err != nil {
					e.errors = append(e.errors, err)
					continue
				}
			}
			changes.UpdateNew = append(changes.UpdateNew, update.desired)
			changes.UpdateOld = append(changes.UpdateOld, update.current)
		}
	}

	return changes
}

// validTargets returns true if the endpoints targets pass all target validation checks.
// validates that CNAME record target values must exist if the target matches the current plans root domain filter.
func (e *managedRecordSetChanges) validTargets(ep *endpoint.Endpoint) (err error) {
	if ep.RecordType == endpoint.RecordTypeCNAME && e.rootDomainFilter.IsConfigured() {
		for idx := range ep.Targets {
			t := ep.Targets[idx]
			if e.rootDomainFilter.Match(t) {
				tDNSName := normalizeDNSName(t)
				if _, tIsManaged := e.dnsNameOwners[tDNSName]; !tIsManaged {
					err = fmt.Errorf("%w, endpoint '%s' has target '%s' that matches the root host filters '%v' but does not exist in the list of local or remote endpoints", ErrInvalidTarget, ep.DNSName, t, e.rootDomainFilter.Filters)
				}
			}
		}
	}
	return
}

// calculateDesired changes the value of update.desired based on all information (desired/current/previous) available about the endpoint.
func (e *managedRecordSetChanges) calculateDesired(update *endpointUpdate) {
	if e.ownerID == "" {
		e.logger.V(1).Info(fmt.Sprintf("skipping update of desired for %s, no ownerID set for plan", update.desired.DNSName))
		return
	}

	// If the record is using a `SetIdentifier` the provider will only ever allow a single target value for any record type (A or CNAME)
	// In the case just return and the desired target value will be used (i.e. AWS route53 geo or weighted records)
	if update.current.SetIdentifier != "" {
		e.logger.V(1).Info(fmt.Sprintf("skipping update of desired for %s, has SetIdentifier", update.desired.DNSName))
		return
	}

	// If we are deleting we need to remove the previous target values, but not if we are going to be left with none.
	// This can happen if you are creating multiple records with the same hostname on the same cluster since they will
	// all have the same addresses. If we get to this point we also know there is still another owner so removing all the
	// target values is never going to be correct.
	if update.isDelete && update.previous != nil {
		desiredCopy := update.desired.DeepCopy()
		removeEndpointTargets(update.previous.Targets, desiredCopy)
		if len(desiredCopy.Targets) > 0 {
			update.desired.Targets = desiredCopy.Targets
		}
	}

	currentCopy := update.current.DeepCopy()
	desiredCopy := update.desired.DeepCopy()

	// A Records can be merged, but we remove the known previous target values first in order to ensure potentially stale values are removed
	if update.current.RecordType == endpoint.RecordTypeA {
		if update.previous != nil {
			removeEndpointTargets(update.previous.Targets, currentCopy)
		}
		mergeEndpointTargets(desiredCopy, currentCopy)
	}

	// CNAME records can be merged, it's expected that the provider implementation understands that a CNAME might have
	// multiple target values and adjusts accordingly during apply.
	if update.current.RecordType == endpoint.RecordTypeCNAME {
		if update.previous != nil {
			removeEndpointTargets(update.previous.Targets, currentCopy)
		}
		mergeEndpointTargets(desiredCopy, currentCopy)

		//ToDo manirn Check this is actually needed, and if it is add a test that requires it to be here
		if len(desiredCopy.Targets) <= 1 {
			e.logger.V(1).Info("skipping check for managed dnsNames for CNAME with single target value")
			return
		}

		// Calculate if any of the new desired targets are also managed dnsNames within this record set.
		// If a target is not managed, do nothing and continue with the current targets.
		// If a target is managed:
		// - If after the update the dnsName will no longer be owned by this endpoint(update.desired), remove it from the list of targets.
		// - If after the update the dnsName will have no owners (it's going to be deleted), remove it from the list of targets.
		for idx := range desiredCopy.Targets {
			t := desiredCopy.Targets[idx]
			tDNSName := normalizeDNSName(t)
			e.logger.V(1).Info(fmt.Sprintf("checking target %s owners", t))
			if tOwners, tIsManaged := e.dnsNameOwners[tDNSName]; tIsManaged {
				e.logger.V(1).Info(fmt.Sprintf("target dnsName %s is managed and has owners %v", tDNSName, tOwners))

				// If the target has no owners we can just remove it
				if len(tOwners) == 0 {
					removeEndpointTarget(t, desiredCopy)
					break
				}

				// Remove the target if there is no mutual ownership between the desired endpoint and the managed target
				if eOwners, eIsManaged := e.dnsNameOwners[normalizeDNSName(desiredCopy.DNSName)]; eIsManaged {
					hasMutualOwner := false
					for _, ownerID := range eOwners {
						if slices.Contains(tOwners, ownerID) {
							hasMutualOwner = true
							break
						}
					}
					if !hasMutualOwner {
						removeEndpointTarget(t, desiredCopy)
					}
				}

			}
		}
	}
	update.desired = desiredCopy
}

func removeEndpointTarget(target string, endpoint *endpoint.Endpoint) {
	removeEndpointTargets([]string{target}, endpoint)
}

func removeEndpointTargets(targets []string, endpoint *endpoint.Endpoint) {
	undesiredMap := map[string]string{}
	for idx := range targets {
		undesiredMap[targets[idx]] = targets[idx]
	}
	desiredTargets := []string{}
	for idx := range endpoint.Targets {
		if _, ok := undesiredMap[endpoint.Targets[idx]]; ok {
			endpoint.DeleteProviderSpecificProperty(endpoint.Targets[idx])
		} else {
			desiredTargets = append(desiredTargets, endpoint.Targets[idx])
		}
	}
	endpoint.Targets = desiredTargets
}

func mergeEndpointTargets(desired, current *endpoint.Endpoint) {
	desired.Targets = append(desired.Targets, current.Targets...)
	slices.Sort(desired.Targets)
	desired.Targets = slices.Compact[[]string, string](desired.Targets)

	for idx := range desired.Targets {
		if val, ok := current.GetProviderSpecificProperty(desired.Targets[idx]); ok {
			desired.DeleteProviderSpecificProperty(desired.Targets[idx])
			desired.SetProviderSpecificProperty(desired.Targets[idx], val)
		}
	}
}

func targetChanged(desired, current *endpoint.Endpoint) bool {
	return !desired.Targets.Same(current.Targets)
}

func shouldUpdateOwner(desired, current *endpoint.Endpoint) bool {
	desiredOwners := map[string]struct{}{}
	currentOwners := map[string]struct{}{}

	for desiredOwner := range desired.Labels {
		if desiredOwner != "" {
			desiredOwners[desiredOwner] = struct{}{}
		}
	}

	for currentOwner := range current.Labels {
		if currentOwner != "" {
			currentOwners[currentOwner] = struct{}{}
		}
	}

	if len(desiredOwners) > 0 && len(currentOwners) > 0 {
		if len(desiredOwners) != len(currentOwners) {
			return true
		}
		for desiredOwner := range desiredOwners {
			if _, ok := currentOwners[desiredOwner]; !ok {
				return true
			}
		}
	}
	return false
}

func shouldUpdateTTL(desired, current *endpoint.Endpoint) bool {
	if !desired.RecordTTL.IsConfigured() {
		return false
	}
	return desired.RecordTTL != current.RecordTTL
}

func shouldUpdateProviderSpecific(desired, current *endpoint.Endpoint) bool {
	desiredProperties := map[string]endpoint.ProviderSpecificProperty{}

	for _, d := range desired.ProviderSpecific {
		desiredProperties[d.Name] = d
	}
	for _, c := range current.ProviderSpecific {
		if d, ok := desiredProperties[c.Name]; ok {
			if c.Value != d.Value {
				return true
			}
			delete(desiredProperties, c.Name)
		} else {
			return true
		}
	}

	return len(desiredProperties) > 0
}

// normalizeDNSName converts a DNS name to a canonical form, so that we can use string equality
// it: removes space, converts to lower case, ensures there is a trailing dot
func normalizeDNSName(dnsName string) string {
	s := strings.TrimSpace(strings.ToLower(dnsName))
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func IsManagedRecord(record string, managedRecords, excludeRecords []string) bool {
	for _, r := range excludeRecords {
		if record == r {
			return false
		}
	}
	for _, r := range managedRecords {
		if record == r {
			return true
		}
	}
	return false
}
