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
	"errors"
	"fmt"
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	externaldnsplan "sigs.k8s.io/external-dns/plan"
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
	// ConflictErrors list of errors describing conflicts that can't be resolved.
	// Populated after calling Calculate()
	ConflictErrors []error
	// RootHost the host dns name being managed by the set of records in the plan.
	RootHost *string
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

func newPlanTable() planTable { // TODO: make resolver configurable
	return planTable{map[planKey]*planTableRow{}, PerResource{}}
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

// ConflictError returns all ConflictErrors as a single error or nil if there are none.
func (p *Plan) ConflictError() error {
	return errors.Join(p.ConflictErrors...)
}

// Calculate computes the actions needed to move current state towards desired
// state. It then passes those changes to the current policy for further
// processing. It returns a copy of Plan with the changes populated.
func (p *Plan) Calculate() *Plan {
	t := newPlanTable()

	var conflictErrs []error
	var rootDomainFilter endpoint.DomainFilter

	if p.DomainFilter == nil {
		p.DomainFilter = endpoint.MatchAllDomainFilters(nil)
	}

	if p.RootHost != nil {
		rootDomainFilter = endpoint.NewDomainFilter([]string{*p.RootHost})
		p.DomainFilter = append(p.DomainFilter, &rootDomainFilter)
	}

	for _, current := range filterRecordsForPlan(p.Current, p.DomainFilter, p.ManagedRecords, p.ExcludeRecords) {
		t.addCurrent(current)
	}
	for _, previous := range filterRecordsForPlan(p.Previous, p.DomainFilter, p.ManagedRecords, p.ExcludeRecords) {
		t.addPrevious(previous)
	}
	for _, desired := range filterRecordsForPlan(p.Desired, p.DomainFilter, p.ManagedRecords, p.ExcludeRecords) {
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
					if endpointOwner, hasOwner := records.current.Labels[endpoint.OwnerLabelKey]; hasOwner && p.OwnerID != "" {
						owners = strings.Split(endpointOwner, OwnerLabelDeliminator)
						for i, v := range owners {
							if v == p.OwnerID {
								owners = append(owners[:i], owners[i+1:]...)
								break
							}
						}
						slices.Sort(owners)
						owners = slices.Compact[[]string, string](owners)
						candidate.Labels[endpoint.OwnerLabelKey] = strings.Join(owners, OwnerLabelDeliminator)
					}

					if len(owners) == 0 {
						managedChanges.deletes = append(managedChanges.deletes, records.current)
					} else {
						//ToDO Not ideal that this is also manipulating the desired record values here but we dont know if
						// the update was caused by the deletion of a record or not later so we have to remove the previous
						// values from the desired like this for now.
						if records.previous != nil && len(candidate.Targets) > 1 {
							removeEndpointTargets(records.previous.Targets, candidate)
						}
						//ToDo Need a test that tests the deletion of a record with two owners who are adding the same value
						// If you delete one owner record, it currently removes the endpoint form desired causing an invalid record

						managedChanges.updates = append(managedChanges.updates, &endpointUpdate{desired: candidate, current: records.current, previous: records.previous})
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
					candidate := t.resolver.ResolveUpdate(records.current, records.candidates)
					current := records.current.DeepCopy()
					owners := []string{}
					if endpointOwner, hasOwner := current.Labels[endpoint.OwnerLabelKey]; hasOwner {
						if p.OwnerID == "" {
							// Only allow owned records to be updated by other owned records
							conflictErrs = append(conflictErrs, fmt.Errorf("%w, cannot update endpoint '%s' with no owner when existing endpoint is already owned", ErrOwnerConflict, candidate.DNSName))
							continue
						}

						owners = strings.Split(endpointOwner, OwnerLabelDeliminator)
						owners = append(owners, p.OwnerID)
						slices.Sort(owners)
						owners = slices.Compact[[]string, string](owners)
						current.Labels[endpoint.OwnerLabelKey] = strings.Join(owners, OwnerLabelDeliminator)
					} else {
						if p.OwnerID != "" {
							// Only allow unowned records to be updated by other unowned records
							conflictErrs = append(conflictErrs, fmt.Errorf("%w, cannot update endpoint '%s' with owner when existing endpoint is not owned", ErrOwnerConflict, candidate.DNSName))
							continue
						}
					}
					inheritOwner(current, candidate)
					managedChanges.updates = append(managedChanges.updates, &endpointUpdate{desired: candidate, current: records.current, previous: records.previous})
					managedChanges.dnsNameOwners[key.dnsName] = append(managedChanges.dnsNameOwners[key.dnsName], owners...)
				}
			}

			if rTypeUpdate.current != nil && rTypeUpdate.desired != nil {
				conflictErrs = append(conflictErrs, fmt.Errorf("%w, cannot update endpoint '%s' with record type '%s' when endpoint already exists with record type '%s'",
					ErrRecordTypeConflict, rTypeUpdate.current.DNSName, rTypeUpdate.desired.RecordType, rTypeUpdate.current.RecordType))
			}
		}

		slices.Sort(managedChanges.dnsNameOwners[key.dnsName])
		managedChanges.dnsNameOwners[key.dnsName] = slices.Compact[[]string, string](managedChanges.dnsNameOwners[key.dnsName])
	}

	changes := managedChanges.Calculate()
	conflictErrs = append(conflictErrs, managedChanges.errors...)

	for _, pol := range p.Policies {
		changes = pol.Apply(changes)
	}

	// filter out updates this external dns does not have ownership claim over
	if p.OwnerID != "" {
		changes.Delete = endpoint.FilterEndpointsByOwnerID(p.OwnerID, changes.Delete)
		//ToDo Ideally we would still be able to ensure ownership on update
		//changes.UpdateOld = endpoint.FilterEndpointsByOwnerID(p.OwnerID, changes.UpdateOld)
		//changes.UpdateNew = endpoint.FilterEndpointsByOwnerID(p.OwnerID, changes.UpdateNew)
	}

	plan := &Plan{
		Current:        p.Current,
		Desired:        p.Desired,
		Changes:        changes,
		ConflictErrors: conflictErrs,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	return plan
}

func inheritOwner(from, to *endpoint.Endpoint) {
	if to.Labels == nil {
		to.Labels = map[string]string{}
	}
	if from.Labels == nil {
		from.Labels = map[string]string{}
	}
	to.Labels[endpoint.OwnerLabelKey] = from.Labels[endpoint.OwnerLabelKey]
}

type endpointUpdate struct {
	current  *endpoint.Endpoint
	previous *endpoint.Endpoint
	desired  *endpoint.Endpoint
}

func (e *endpointUpdate) ShouldUpdate() bool {
	return shouldUpdateOwner(e.desired, e.current) || shouldUpdateTTL(e.desired, e.current) || targetChanged(e.desired, e.current) || shouldUpdateProviderSpecific(e.desired, e.current)
}

type managedRecordSetChanges struct {
	ownerID          string
	rootDomainFilter endpoint.DomainFilter
	creates          []*endpoint.Endpoint
	deletes          []*endpoint.Endpoint
	updates          []*endpointUpdate
	dnsNameOwners    map[string][]string
	errors           []error
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
			if err := e.validTargets(update.desired); err != nil {
				e.errors = append(e.errors, err)
			} else {
				changes.UpdateNew = append(changes.UpdateNew, update.desired)
				changes.UpdateOld = append(changes.UpdateOld, update.current)
			}
		}
	}

	return changes
}

// validTargets returns true if the endpoints targets pass all target validation checks
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
		log.Debugf("skipping update of desired for %s, no ownerID set for plan", update.desired.DNSName)
		return
	}

	// If the record is using a `SetIdentifier` the provider will only ever allow a single target value for any record type (A or CNAME)
	// In the case just return and the desired target value will be used (i.e. AWS route53 geo or weighted records)
	if update.current.SetIdentifier != "" {
		log.Debugf("skipping update of desired for %s, has SetIdentifier", update.desired.DNSName)
		return
	}

	currentCopy := update.current.DeepCopy()

	// A Records can be merged, but we remove the known previous target values first in order to ensure potentially stale values are removed
	if update.current.RecordType == endpoint.RecordTypeA {
		if update.previous != nil {
			removeEndpointTargets(update.previous.Targets, currentCopy)
		}
		mergeEndpointTargets(update.desired, currentCopy)
	}

	// CNAME records can be merged, it's expected that the provider implementation understands that a CNAME might have
	// multiple target values and adjusts accordingly during apply.
	if update.current.RecordType == endpoint.RecordTypeCNAME {
		if update.previous != nil {
			removeEndpointTargets(update.previous.Targets, currentCopy)
		}
		mergeEndpointTargets(update.desired, currentCopy)

		//ToDo manirn Check this is actually needed, and if it is add a test that requires it to be here
		if len(update.desired.Targets) <= 1 {
			log.Debugf("skipping check for managed dnsNames for CNAME with single target value")
			return
		}

		desiredCopy := update.desired.DeepCopy()

		// Calculate if any of the new desired targets are also managed dnsNames within this record set.
		// If a target is not managed, do nothing and continue with the current targets.
		// If a target is managed:
		// - If after the update the dnsName will no longer be owned by this endpoint(update.desired), remove it from the list of targets.
		// - If after the update the dnsName will have no owners (it's going to be deleted), remove it from the list of targets.
		for idx := range desiredCopy.Targets {
			t := desiredCopy.Targets[idx]
			tDNSName := normalizeDNSName(t)
			log.Debugf("checking target %s owners", t)
			if tOwners, tIsManaged := e.dnsNameOwners[tDNSName]; tIsManaged {
				log.Debugf("target dnsName %s is managed and has owners %v", tDNSName, tOwners)

				// If the target has no owners we can just remove it
				if len(tOwners) == 0 {
					removeEndpointTarget(t, update.desired)
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
						removeEndpointTarget(t, update.desired)
					}
				}

			}
		}
	}
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
	currentOwner, hasCurrentOwner := current.Labels[endpoint.OwnerLabelKey]
	desiredOwner, hasDesiredOwner := desired.Labels[endpoint.OwnerLabelKey]
	if hasCurrentOwner && hasDesiredOwner {
		return currentOwner != desiredOwner
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

// filterRecordsForPlan removes records that are not relevant to the planner.
// Currently this just removes TXT records to prevent them from being
// deleted erroneously by the planner (only the TXT registry should do this.)
//
// Per RFC 1034, CNAME records conflict with all other records - it is the
// only record with this property. The behavior of the planner may need to be
// made more sophisticated to codify this.
func filterRecordsForPlan(records []*endpoint.Endpoint, domainFilter endpoint.MatchAllDomainFilters, managedRecords, excludeRecords []string) []*endpoint.Endpoint {
	filtered := []*endpoint.Endpoint{}

	for _, record := range records {
		// Ignore records that do not match the domain filter provided
		if !domainFilter.Match(record.DNSName) {
			log.Debugf("ignoring record %s that does not match domain filter", record.DNSName)
			continue
		}
		if IsManagedRecord(record.RecordType, managedRecords, excludeRecords) {
			filtered = append(filtered, record)
		}
	}

	return filtered
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
