/*
Copyright 2024.

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

package google

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	dnsv1 "google.golang.org/api/dns/v1"
	googleapi "google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsplan "sigs.k8s.io/external-dns/plan"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	googleRecordTTL           = 300
	GoogleBatchChangeSize     = 1000
	GoogleBatchChangeInterval = time.Second
	DryRun                    = false
)

// Based on the external-dns google provider https://github.com/kubernetes-sigs/external-dns/blob/master/provider/google/google.go

// Managed zone interfaces
type managedZonesCreateCallInterface interface {
	Do(opts ...googleapi.CallOption) (*dnsv1.ManagedZone, error)
}

type managedZonesGetCallInterface interface {
	Do(opts ...googleapi.CallOption) (*dnsv1.ManagedZone, error)
}
type managedZonesDeleteCallInterface interface {
	Do(opts ...googleapi.CallOption) error
}

type managedZonesListCallInterface interface {
	Pages(ctx context.Context, f func(*dnsv1.ManagedZonesListResponse) error) error
}

type managedZonesServiceInterface interface {
	Create(project string, managedzone *dnsv1.ManagedZone) managedZonesCreateCallInterface
	Get(project string, managedZone string) managedZonesGetCallInterface
	List(project string) managedZonesListCallInterface
	Delete(project string, managedzone string) managedZonesDeleteCallInterface
}

type managedZonesService struct {
	service *dnsv1.ManagedZonesService
}

func (m managedZonesService) Create(project string, managedzone *dnsv1.ManagedZone) managedZonesCreateCallInterface {
	return m.service.Create(project, managedzone)
}

func (m managedZonesService) Get(project string, managedZone string) managedZonesGetCallInterface {
	return m.service.Get(project, managedZone)
}

func (m managedZonesService) List(project string) managedZonesListCallInterface {
	return m.service.List(project)
}
func (m managedZonesService) Delete(project string, managedzone string) managedZonesDeleteCallInterface {
	return m.service.Delete(project, managedzone)
}

// Record set interfaces
type resourceRecordSetsListCallInterface interface {
	Pages(ctx context.Context, f func(*dnsv1.ResourceRecordSetsListResponse) error) error
}

type resourceRecordSetsClientInterface interface {
	List(project string, managedZone string) resourceRecordSetsListCallInterface
}

type changesCreateCallInterface interface {
	Do(opts ...googleapi.CallOption) (*dnsv1.Change, error)
}

type changesServiceInterface interface {
	Create(project string, managedZone string, change *dnsv1.Change) changesCreateCallInterface
}

type changesService struct {
	service *dnsv1.ChangesService
}

func (c changesService) Create(project string, managedZone string, change *dnsv1.Change) changesCreateCallInterface {
	return c.service.Create(project, managedZone, change)
}

type resourceRecordSetsService struct {
	service *dnsv1.ResourceRecordSetsService
}

func (r resourceRecordSetsService) List(project string, managedZone string) resourceRecordSetsListCallInterface {
	return r.service.List(project, managedZone)
}

type GoogleDNSProvider struct {
	logger logr.Logger
	// The Google project to work in
	project string
	// Enabled dry-run will print any modifying actions rather than execute them.
	dryRun bool
	// Max batch size to submit to Google Cloud DNS per transaction.
	batchChangeSize int
	// Interval between batch updates.
	batchChangeInterval time.Duration
	// only consider hosted zones managing domains ending in this suffix
	domainFilter externaldnsendpoint.DomainFilter
	// filter for zones based on visibility
	zoneTypeFilter externaldnsprovider.ZoneTypeFilter
	// only consider hosted zones ending with this zone id
	zoneIDFilter externaldnsprovider.ZoneIDFilter
	// A client for managing resource record sets
	resourceRecordSetsClient resourceRecordSetsClientInterface
	// A client for managing hosted zones
	managedZonesClient managedZonesServiceInterface
	// A client for managing change sets
	changesClient changesServiceInterface
	// The context parameter to be passed for gcloud API calls.
	ctx context.Context
}

var _ provider.Provider = &GoogleDNSProvider{}

func NewProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {

	if string(s.Data["GOOGLE"]) == "" || string(s.Data["PROJECT_ID"]) == "" {
		return nil, fmt.Errorf("GCP Provider credentials is empty")
	}

	dnsClient, err := dnsv1.NewService(ctx, option.WithCredentialsJSON(s.Data["GOOGLE"]))
	if err != nil {
		return nil, err
	}

	var project = string(s.Data["PROJECT_ID"])

	p := &GoogleDNSProvider{
		logger:                   log.Log.WithName("google-dns").WithValues("project", project),
		project:                  project,
		dryRun:                   DryRun,
		batchChangeSize:          GoogleBatchChangeSize,
		batchChangeInterval:      GoogleBatchChangeInterval,
		domainFilter:             c.DomainFilter,
		zoneTypeFilter:           c.ZoneTypeFilter,
		zoneIDFilter:             c.ZoneIDFilter,
		resourceRecordSetsClient: resourceRecordSetsService{dnsClient.ResourceRecordSets},
		managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
		changesClient:            changesService{dnsClient.Changes},
		ctx:                      ctx,
	}

	return p, nil
}

// #### External DNS Provider ####

// Zones returns the list of hosted zones.
func (p *GoogleDNSProvider) Zones(ctx context.Context) (map[string]*dnsv1.ManagedZone, error) {
	zones := make(map[string]*dnsv1.ManagedZone)

	f := func(resp *dnsv1.ManagedZonesListResponse) error {
		for _, zone := range resp.ManagedZones {
			if zone.PeeringConfig == nil {
				if p.domainFilter.Match(zone.DnsName) && p.zoneTypeFilter.Match(zone.Visibility) && (p.zoneIDFilter.Match(fmt.Sprintf("%v", zone.Id)) || p.zoneIDFilter.Match(fmt.Sprintf("%v", zone.Name))) {
					zones[zone.Name] = zone
					p.logger.Info(fmt.Sprintf("Matched %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
				} else {
					p.logger.Info(fmt.Sprintf("Filtered %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
				}
			} else {
				p.logger.Info(fmt.Sprintf("Filtered peering zone %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
			}
		}

		return nil
	}

	p.logger.Info(fmt.Sprintf("Matching zones against domain filters: %v", p.domainFilter))
	if err := p.managedZonesClient.List(p.project).Pages(ctx, f); err != nil {
		return nil, err
	}

	if len(zones) == 0 {
		p.logger.Info(fmt.Sprintf("No zones in the project, %s, match domain filters: %v", p.project, p.domainFilter))
	}

	for _, zone := range zones {
		p.logger.Info(fmt.Sprintf("Considering zone: %s (domain: %s)", zone.Name, zone.DnsName))
	}

	return zones, nil
}

// Records returns records from the provider in google specific format
func (p *GoogleDNSProvider) Records(ctx context.Context) (endpoints []*externaldnsendpoint.Endpoint, _ error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	var records []*dnsv1.ResourceRecordSet
	f := func(resp *dnsv1.ResourceRecordSetsListResponse) error {
		for _, r := range resp.Rrsets {
			if !p.SupportedRecordType(r.Type) {
				continue
			}
			records = append(records, r)
		}
		return nil
	}

	for _, z := range zones {
		if err := p.resourceRecordSetsClient.List(p.project, z.Name).Pages(ctx, f); err != nil {
			return nil, err
		}
	}

	return endpointsFromResourceRecordSets(records), nil
}

func (p *GoogleDNSProvider) ApplyChanges(ctx context.Context, changes *externaldnsplan.Changes) error {
	change := &dnsv1.Change{}

	change.Additions = append(change.Additions, p.newFilteredRecords(changes.Create)...)

	change.Additions = append(change.Additions, p.newFilteredRecords(changes.UpdateNew)...)
	change.Deletions = append(change.Deletions, p.newFilteredRecords(changes.UpdateOld)...)

	change.Deletions = append(change.Deletions, p.newFilteredRecords(changes.Delete)...)

	return p.submitChange(ctx, change)
}

// AdjustEndpoints takes source endpoints and translates them to a google specific format
func (p *GoogleDNSProvider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	return endpointsToGoogleFormat(endpoints), nil
}

func (p *GoogleDNSProvider) GetDomainFilter() externaldnsendpoint.DomainFilter {
	return externaldnsendpoint.DomainFilter{}
}

// SupportedRecordType returns true if the record type is supported by the provider
func (p *GoogleDNSProvider) SupportedRecordType(recordType string) bool {
	switch recordType {
	case "MX":
		return true
	default:
		return externaldnsprovider.SupportedRecordType(recordType)
	}
}

// newFilteredRecords returns a collection of RecordSets based on the given endpoints and domainFilter.
func (p *GoogleDNSProvider) newFilteredRecords(endpoints []*externaldnsendpoint.Endpoint) []*dnsv1.ResourceRecordSet {
	records := []*dnsv1.ResourceRecordSet{}

	for _, endpoint := range endpoints {
		if p.domainFilter.Match(endpoint.DNSName) {
			records = append(records, newRecord(endpoint))
		}
	}

	return records
}

// submitChange takes a zone and a Change and sends it to Google.
func (p *GoogleDNSProvider) submitChange(ctx context.Context, change *dnsv1.Change) error {
	if len(change.Additions) == 0 && len(change.Deletions) == 0 {
		p.logger.Info("All records are already up to date")
		return nil
	}

	zones, err := p.Zones(ctx)
	if err != nil {
		return err
	}

	// separate into per-zone change sets to be passed to the API.
	changes := separateChange(zones, change)

	for zone, change := range changes {
		for batch, c := range batchChange(change, p.batchChangeSize) {
			p.logger.Info(fmt.Sprintf("Change zone: %v batch #%d", zone, batch))
			for _, del := range c.Deletions {
				p.logger.Info(fmt.Sprintf("Del records: %s %s %s %d", del.Name, del.Type, del.Rrdatas, del.Ttl))
			}
			for _, add := range c.Additions {
				p.logger.Info(fmt.Sprintf("Add records: %s %s %s %d", add.Name, add.Type, add.Rrdatas, add.Ttl))
			}

			if p.dryRun {
				continue
			}

			if _, err := p.changesClient.Create(p.project, zone, c).Do(); err != nil {
				return err
			}

			time.Sleep(p.batchChangeInterval)
		}
	}

	return nil
}

// batchChange separates a zone in multiple transaction.
func batchChange(change *dnsv1.Change, batchSize int) []*dnsv1.Change {
	changes := []*dnsv1.Change{}

	if batchSize == 0 {
		return append(changes, change)
	}

	type dnsChange struct {
		additions []*dnsv1.ResourceRecordSet
		deletions []*dnsv1.ResourceRecordSet
	}

	changesByName := map[string]*dnsChange{}

	for _, a := range change.Additions {
		change, ok := changesByName[a.Name]
		if !ok {
			change = &dnsChange{}
			changesByName[a.Name] = change
		}

		change.additions = append(change.additions, a)
	}

	for _, a := range change.Deletions {
		change, ok := changesByName[a.Name]
		if !ok {
			change = &dnsChange{}
			changesByName[a.Name] = change
		}

		change.deletions = append(change.deletions, a)
	}

	names := make([]string, 0)
	for v := range changesByName {
		names = append(names, v)
	}
	sort.Strings(names)

	currentChange := &dnsv1.Change{}
	var totalChanges int
	for _, name := range names {
		c := changesByName[name]

		totalChangesByName := len(c.additions) + len(c.deletions)

		if totalChangesByName > batchSize {
			log.Log.Info(fmt.Sprintf("Total changes for %s exceeds max batch size of %d, total changes: %d", name, batchSize, totalChangesByName))
			continue
		}

		if totalChanges+totalChangesByName > batchSize {
			totalChanges = 0
			changes = append(changes, currentChange)
			currentChange = &dnsv1.Change{}
		}

		currentChange.Additions = append(currentChange.Additions, c.additions...)
		currentChange.Deletions = append(currentChange.Deletions, c.deletions...)

		totalChanges += totalChangesByName
	}

	if totalChanges > 0 {
		changes = append(changes, currentChange)
	}

	return changes
}

// separateChange separates a multi-zone change into a single change per zone.
func separateChange(zones map[string]*dnsv1.ManagedZone, change *dnsv1.Change) map[string]*dnsv1.Change {
	changes := make(map[string]*dnsv1.Change)
	zoneNameIDMapper := externaldnsprovider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper[z.Name] = z.DnsName
		changes[z.Name] = &dnsv1.Change{
			Additions: []*dnsv1.ResourceRecordSet{},
			Deletions: []*dnsv1.ResourceRecordSet{},
		}
	}
	for _, a := range change.Additions {
		if zoneName, _ := zoneNameIDMapper.FindZone(externaldnsprovider.EnsureTrailingDot(a.Name)); zoneName != "" {
			changes[zoneName].Additions = append(changes[zoneName].Additions, a)
		} else {
			log.Log.Info(fmt.Sprintf("No matching zone for record addition: %s %s %s %d", a.Name, a.Type, a.Rrdatas, a.Ttl))
		}
	}

	for _, d := range change.Deletions {
		if zoneName, _ := zoneNameIDMapper.FindZone(externaldnsprovider.EnsureTrailingDot(d.Name)); zoneName != "" {
			changes[zoneName].Deletions = append(changes[zoneName].Deletions, d)
		} else {
			log.Log.Info(fmt.Sprintf("No matching zone for record deletion: %s %s %s %d", d.Name, d.Type, d.Rrdatas, d.Ttl))
		}
	}

	// separating a change could lead to empty sub changes, remove them here.
	for zone, change := range changes {
		if len(change.Additions) == 0 && len(change.Deletions) == 0 {
			delete(changes, zone)
		}
	}

	return changes
}

// endpointsFromResourceRecordSets converts a list of `ResourceRecordSet` into endpoints (google format).
func endpointsFromResourceRecordSets(resourceRecordSets []*dnsv1.ResourceRecordSet) []*externaldnsendpoint.Endpoint {
	var endpoints []*externaldnsendpoint.Endpoint

	for _, rrs := range resourceRecordSets {
		if rrs.RoutingPolicy != nil {
			endpoint := externaldnsendpoint.NewEndpointWithTTL(rrs.Name, rrs.Type, externaldnsendpoint.TTL(rrs.Ttl), []string{}...)

			if rrs.RoutingPolicy.Wrr != nil {
				endpoint.WithProviderSpecific("routingpolicy", "weighted")
				for i := range rrs.RoutingPolicy.Wrr.Items {
					weight := strconv.FormatFloat(rrs.RoutingPolicy.Wrr.Items[i].Weight, 'f', -1, 64)
					for idx := range rrs.RoutingPolicy.Wrr.Items[i].Rrdatas {
						target := strings.TrimSuffix(rrs.RoutingPolicy.Wrr.Items[i].Rrdatas[idx], ".")
						endpoint.Targets = append(endpoint.Targets, target)
						endpoint.WithProviderSpecific(target, weight)
					}
				}
			} else if rrs.RoutingPolicy.Geo != nil {
				endpoint.WithProviderSpecific("routingpolicy", "geo")
				for i := range rrs.RoutingPolicy.Geo.Items {
					location := rrs.RoutingPolicy.Geo.Items[i].Location
					for idx := range rrs.RoutingPolicy.Geo.Items[i].Rrdatas {
						target := strings.TrimSuffix(rrs.RoutingPolicy.Geo.Items[i].Rrdatas[idx], ".")
						endpoint.Targets = append(endpoint.Targets, target)
						endpoint.WithProviderSpecific(target, location)
					}
				}
			} else {
				//Not good !!
				continue
			}
			endpoints = append(endpoints, endpoint)
		} else {
			endpoints = append(endpoints, externaldnsendpoint.NewEndpointWithTTL(rrs.Name, rrs.Type, externaldnsendpoint.TTL(rrs.Ttl), rrs.Rrdatas...))
		}
	}

	return endpoints
}

// endpointsToProviderFormat converts a list of endpoints into a google specific format.
func endpointsToGoogleFormat(eps []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	endpointMap := make(map[string][]*externaldnsendpoint.Endpoint)
	for i := range eps {
		endpointMap[eps[i].DNSName] = append(endpointMap[eps[i].DNSName], eps[i])
	}

	translatedEndpoints := []*externaldnsendpoint.Endpoint{}

	for dnsName, endpoints := range endpointMap {
		// A set of endpoints belonging to the same group(`dnsName`) must always be of the same type, have the same ttl
		// and contain the same rrdata (weighted or geo), so we can just get that from the first endpoint in the list.
		ttl := int64(endpoints[0].RecordTTL)
		recordType := endpoints[0].RecordType
		_, isWeighted := endpoints[0].GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
		_, isGeo := endpoints[0].GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)

		if !isGeo && !isWeighted {
			//ToDO DO we need to worry about there being more than one here?
			translatedEndpoints = append(translatedEndpoints, endpoints[0])
			continue
		}

		translatedEndpoint := externaldnsendpoint.NewEndpointWithTTL(dnsName, recordType, externaldnsendpoint.TTL(ttl))

		if isGeo {
			translatedEndpoint.WithProviderSpecific("routingpolicy", "geo")
		} else if isWeighted {
			translatedEndpoint.WithProviderSpecific("routingpolicy", "weighted")
		}

		//ToDo this has the potential to add duplicates
		for _, ep := range endpoints {
			for _, t := range ep.Targets {
				if isGeo {
					geo, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
					if geo == "*" {
						continue
					}
					translatedEndpoint.WithProviderSpecific(t, geo)
				} else if isWeighted {
					weight, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
					translatedEndpoint.WithProviderSpecific(t, weight)
				}
				translatedEndpoint.Targets = append(translatedEndpoint.Targets, t)
			}
		}

		translatedEndpoints = append(translatedEndpoints, translatedEndpoint)
	}

	return translatedEndpoints
}

// resourceRecordSetFromEndpoint converts an endpoint(google format) into a `ResourceRecordSet`.
func resourceRecordSetFromEndpoint(ep *externaldnsendpoint.Endpoint) *dnsv1.ResourceRecordSet {

	// no annotation results in a Ttl of 0, default to 300 for backwards-compatibility
	var ttl int64 = googleRecordTTL
	if ep.RecordTTL.IsConfigured() {
		ttl = int64(ep.RecordTTL)
	}

	rrs := &dnsv1.ResourceRecordSet{
		Name: externaldnsprovider.EnsureTrailingDot(ep.DNSName),
		Ttl:  ttl,
		Type: ep.RecordType,
	}

	if rp, ok := ep.GetProviderSpecificProperty("routingpolicy"); ok && ep.RecordType != externaldnsendpoint.RecordTypeTXT {
		if rp == "geo" {
			rrs.RoutingPolicy = &dnsv1.RRSetRoutingPolicy{
				Geo: &dnsv1.RRSetRoutingPolicyGeoPolicy{},
			}
			//Map location to targets, can ony have one location the same
			targetMap := make(map[string][]string)
			for i := range ep.Targets {
				if location, ok := ep.GetProviderSpecificProperty(ep.Targets[i]); ok {
					targetMap[location] = append(targetMap[location], externaldnsprovider.EnsureTrailingDot(ep.Targets[i]))
				}
			}
			for l, t := range targetMap {
				item := &dnsv1.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
					Location: l,
					Rrdatas:  t,
				}
				rrs.RoutingPolicy.Geo.Items = append(rrs.RoutingPolicy.Geo.Items, item)
			}
		} else if rp == "weighted" {
			rrs.RoutingPolicy = &dnsv1.RRSetRoutingPolicy{
				Wrr: &dnsv1.RRSetRoutingPolicyWrrPolicy{},
			}

			for i := range ep.Targets {
				if weightStr, ok := ep.GetProviderSpecificProperty(ep.Targets[i]); ok {
					weight, err := strconv.ParseFloat(weightStr, 64)
					if err != nil {
						weight = 0.0
					}
					item := &dnsv1.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
						Rrdatas: []string{externaldnsprovider.EnsureTrailingDot(ep.Targets[i])},
						Weight:  weight,
					}
					rrs.RoutingPolicy.Wrr.Items = append(rrs.RoutingPolicy.Wrr.Items, item)
				}
			}
		}

		return rrs
	}

	targets := make([]string, len(ep.Targets))
	copy(targets, []string(ep.Targets))
	if ep.RecordType == externaldnsendpoint.RecordTypeCNAME {
		if len(targets) > 0 {
			targets[0] = externaldnsprovider.EnsureTrailingDot(targets[0])
		}
	}

	if ep.RecordType == externaldnsendpoint.RecordTypeMX {
		for i, mxRecord := range ep.Targets {
			targets[i] = externaldnsprovider.EnsureTrailingDot(mxRecord)
		}
	}

	if ep.RecordType == externaldnsendpoint.RecordTypeSRV {
		for i, srvRecord := range ep.Targets {
			targets[i] = externaldnsprovider.EnsureTrailingDot(srvRecord)
		}
	}

	rrs.Rrdatas = targets

	return rrs
}

// newRecord returns a RecordSet based on the given endpoint(google format).
func newRecord(ep *externaldnsendpoint.Endpoint) *dnsv1.ResourceRecordSet {
	return resourceRecordSetFromEndpoint(ep)
}

// #### DNS Operator Provider ####

func (p *GoogleDNSProvider) EnsureManagedZone(managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var zoneID string

	if managedZone.Spec.ID != "" {
		zoneID = managedZone.Spec.ID
	} else {
		zoneID = managedZone.Status.ID
	}

	if zoneID != "" {
		//Get existing managed zone
		return p.getManagedZone(zoneID)
	}
	//Create new managed zone
	return p.createManagedZone(managedZone)
}

func (p *GoogleDNSProvider) DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error {
	return p.managedZonesClient.Delete(p.project, managedZone.Status.ID).Do()
}

// ManagedZones

func (p *GoogleDNSProvider) createManagedZone(managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	zoneID := strings.Replace(managedZone.Spec.DomainName, ".", "-", -1)
	zone := dnsv1.ManagedZone{
		Name:        zoneID,
		DnsName:     externaldnsprovider.EnsureTrailingDot(managedZone.Spec.DomainName),
		Description: managedZone.Spec.Description,
	}
	mz, err := p.managedZonesClient.Create(p.project, &zone).Do()
	if err != nil {
		return provider.ManagedZoneOutput{}, err
	}
	return p.toManagedZoneOutput(mz)
}

func (p *GoogleDNSProvider) getManagedZone(zoneID string) (provider.ManagedZoneOutput, error) {
	mz, err := p.managedZonesClient.Get(p.project, zoneID).Do()
	if err != nil {
		return provider.ManagedZoneOutput{}, err
	}
	return p.toManagedZoneOutput(mz)
}

func (p *GoogleDNSProvider) toManagedZoneOutput(mz *dnsv1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var managedZoneOutput provider.ManagedZoneOutput

	zoneID := mz.Name
	var nameservers []*string
	for i := range mz.NameServers {
		nameservers = append(nameservers, &mz.NameServers[i])
	}
	managedZoneOutput.ID = zoneID
	managedZoneOutput.NameServers = nameservers

	currentRecords, err := p.getResourceRecordSets(p.ctx, zoneID)
	if err != nil {
		return managedZoneOutput, err
	}
	managedZoneOutput.RecordCount = int64(len(currentRecords))

	return managedZoneOutput, nil
}

// ToDo Can be replaced with a call to Records if/when we update that to optionally accept a zone id
// getResourceRecordSets returns the records for a managed zone of the currently configured provider.
func (p *GoogleDNSProvider) getResourceRecordSets(ctx context.Context, zoneID string) ([]*dnsv1.ResourceRecordSet, error) {
	var records []*dnsv1.ResourceRecordSet

	f := func(resp *dnsv1.ResourceRecordSetsListResponse) error {
		records = append(records, resp.Rrsets...)
		return nil
	}

	if err := p.resourceRecordSetsClient.List(p.project, zoneID).Pages(ctx, f); err != nil {
		return nil, err
	}

	return records, nil
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider("google", NewProviderFromSecret)
}
