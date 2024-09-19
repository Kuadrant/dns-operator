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

package google

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/go-logr/logr"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	googleRecordTTL = 300
)

type managedZonesCreateCallInterface interface {
	Do(opts ...googleapi.CallOption) (*dns.ManagedZone, error)
}

type managedZonesListCallInterface interface {
	Pages(ctx context.Context, f func(*dns.ManagedZonesListResponse) error) error
}

type managedZonesServiceInterface interface {
	Create(project string, managedzone *dns.ManagedZone) managedZonesCreateCallInterface
	List(project string) managedZonesListCallInterface
}

type resourceRecordSetsListCallInterface interface {
	Pages(ctx context.Context, f func(*dns.ResourceRecordSetsListResponse) error) error
}

type resourceRecordSetsClientInterface interface {
	List(project string, managedZone string) resourceRecordSetsListCallInterface
}

type changesCreateCallInterface interface {
	Do(opts ...googleapi.CallOption) (*dns.Change, error)
}

type changesServiceInterface interface {
	Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface
}

type resourceRecordSetsService struct {
	service *dns.ResourceRecordSetsService
}

func (r resourceRecordSetsService) List(project string, managedZone string) resourceRecordSetsListCallInterface {
	return r.service.List(project, managedZone)
}

type managedZonesService struct {
	service *dns.ManagedZonesService
}

func (m managedZonesService) Create(project string, managedzone *dns.ManagedZone) managedZonesCreateCallInterface {
	return m.service.Create(project, managedzone)
}

func (m managedZonesService) List(project string) managedZonesListCallInterface {
	return m.service.List(project)
}

type changesService struct {
	service *dns.ChangesService
}

func (c changesService) Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface {
	return c.service.Create(project, managedZone, change)
}

// GoogleProvider is an implementation of Provider for Google CloudDNS.
type GoogleProvider struct {
	provider.BaseProvider
	// The Google project to work in
	project string
	// Enabled dry-run will print any modifying actions rather than execute them.
	dryRun bool
	// Max batch size to submit to Google Cloud DNS per transaction.
	batchChangeSize int
	// Interval between batch updates.
	batchChangeInterval time.Duration
	// only consider hosted zones managing domains ending in this suffix
	domainFilter endpoint.DomainFilter
	// filter for zones based on visibility
	zoneTypeFilter provider.ZoneTypeFilter
	// only consider hosted zones ending with this zone id
	zoneIDFilter provider.ZoneIDFilter
	// A client for managing resource record sets
	resourceRecordSetsClient resourceRecordSetsClientInterface
	// A client for managing hosted zones
	managedZonesClient managedZonesServiceInterface
	// A client for managing change sets
	changesClient changesServiceInterface
	// The context parameter to be passed for gcloud API calls.
	ctx context.Context

	logger logr.Logger
}

// GoogleConfig contains configuration to create a new Google provider.
type GoogleConfig struct {
	Project             string
	DomainFilter        endpoint.DomainFilter
	ZoneIDFilter        provider.ZoneIDFilter
	ZoneTypeFilter      provider.ZoneTypeFilter
	BatchChangeSize     int
	BatchChangeInterval time.Duration
	DryRun              bool
}

// NewGoogleProvider initializes a new Google CloudDNS based Provider.
func NewGoogleProvider(ctx context.Context, config GoogleConfig) (*GoogleProvider, error) {
	gcloud, err := google.DefaultClient(ctx, dns.NdevClouddnsReadwriteScope)
	if err != nil {
		return nil, err
	}

	dnsClient, err := dns.NewService(ctx, option.WithHTTPClient(gcloud))
	if err != nil {
		return nil, err
	}

	return NewGoogleProviderWithService(ctx, config, dnsClient)
}

func NewGoogleProviderWithService(ctx context.Context, config GoogleConfig, dnsClient *dns.Service) (*GoogleProvider, error) {
	logger := logr.FromContextOrDiscard(ctx)
	if config.Project == "" {
		mProject, mErr := metadata.ProjectID()
		if mErr != nil {
			return nil, fmt.Errorf("failed to auto-detect the project id: %w", mErr)
		}
		logger.Info(fmt.Sprintf("Google project auto-detected: %s", mProject))
		config.Project = mProject
	}

	p := &GoogleProvider{
		logger:                   logger,
		project:                  config.Project,
		dryRun:                   config.DryRun,
		batchChangeSize:          config.BatchChangeSize,
		batchChangeInterval:      config.BatchChangeInterval,
		domainFilter:             config.DomainFilter,
		zoneTypeFilter:           config.ZoneTypeFilter,
		zoneIDFilter:             config.ZoneIDFilter,
		resourceRecordSetsClient: resourceRecordSetsService{dnsClient.ResourceRecordSets},
		managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
		changesClient:            changesService{dnsClient.Changes},
		ctx:                      ctx,
	}

	return p, nil
}

// Zones returns the list of hosted zones.
func (p *GoogleProvider) Zones(ctx context.Context) (map[string]*dns.ManagedZone, error) {
	zones := make(map[string]*dns.ManagedZone)

	f := func(resp *dns.ManagedZonesListResponse) error {
		for _, zone := range resp.ManagedZones {
			if zone.PeeringConfig == nil {
				if p.domainFilter.Match(zone.DnsName) && p.zoneTypeFilter.Match(zone.Visibility) && (p.zoneIDFilter.Match(fmt.Sprintf("%v", zone.Id)) || p.zoneIDFilter.Match(fmt.Sprintf("%v", zone.Name))) {
					zones[zone.Name] = zone
					p.logger.V(1).Info(fmt.Sprintf("Matched %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
				} else {
					p.logger.V(1).Info(fmt.Sprintf("Filtered %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
				}
			} else {
				p.logger.V(1).Info(fmt.Sprintf("Filtered peering zone %s (zone: %s) (visibility: %s)", zone.DnsName, zone.Name, zone.Visibility))
			}
		}

		return nil
	}

	p.logger.V(1).Info(fmt.Sprintf("Matching zones against domain filters: %v", p.domainFilter))
	if err := p.managedZonesClient.List(p.project).Pages(ctx, f); err != nil {
		return nil, err
	}

	if len(zones) == 0 {
		p.logger.Info(fmt.Sprintf("No zones in the project, %s, match domain filters: %v", p.project, p.domainFilter))
	}

	for _, zone := range zones {
		p.logger.V(1).Info(fmt.Sprintf("Considering zone: %s (domain: %s)", zone.Name, zone.DnsName))
	}

	return zones, nil
}

// Records returns the list of records in all relevant zones.
func (p *GoogleProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	f := func(resp *dns.ResourceRecordSetsListResponse) error {
		for _, r := range resp.Rrsets {
			if !p.SupportedRecordType(r.Type) {
				continue
			}
			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(r.Name, r.Type, endpoint.TTL(r.Ttl), r.Rrdatas...))
		}

		return nil
	}

	for _, z := range zones {
		if err := p.resourceRecordSetsClient.List(p.project, z.Name).Pages(ctx, f); err != nil {
			return nil, err
		}
	}

	return endpoints, nil
}

// CreateRecords creates a given set of DNS records in the given hosted zone.
func (p *GoogleProvider) CreateRecords(endpoints []*endpoint.Endpoint) error {
	change := &dns.Change{}

	change.Additions = append(change.Additions, p.newFilteredRecords(endpoints)...)

	return p.submitChange(p.ctx, change)
}

// UpdateRecords updates a given set of old records to a new set of records in a given hosted zone.
func (p *GoogleProvider) UpdateRecords(records, oldRecords []*endpoint.Endpoint) error {
	change := &dns.Change{}

	change.Additions = append(change.Additions, p.newFilteredRecords(records)...)
	change.Deletions = append(change.Deletions, p.newFilteredRecords(oldRecords)...)

	return p.submitChange(p.ctx, change)
}

// DeleteRecords deletes a given set of DNS records in a given zone.
func (p *GoogleProvider) DeleteRecords(endpoints []*endpoint.Endpoint) error {
	change := &dns.Change{}

	change.Deletions = append(change.Deletions, p.newFilteredRecords(endpoints)...)

	return p.submitChange(p.ctx, change)
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *GoogleProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	change := &dns.Change{}

	change.Additions = append(change.Additions, p.newFilteredRecords(changes.Create)...)

	change.Additions = append(change.Additions, p.newFilteredRecords(changes.UpdateNew)...)
	change.Deletions = append(change.Deletions, p.newFilteredRecords(changes.UpdateOld)...)

	change.Deletions = append(change.Deletions, p.newFilteredRecords(changes.Delete)...)

	return p.submitChange(ctx, change)
}

// SupportedRecordType returns true if the record type is supported by the provider
func (p *GoogleProvider) SupportedRecordType(recordType string) bool {
	switch recordType {
	case "MX":
		return true
	default:
		return provider.SupportedRecordType(recordType)
	}
}

// newFilteredRecords returns a collection of RecordSets based on the given endpoints and domainFilter.
func (p *GoogleProvider) newFilteredRecords(endpoints []*endpoint.Endpoint) []*dns.ResourceRecordSet {
	records := []*dns.ResourceRecordSet{}

	for _, endpoint := range endpoints {
		if p.domainFilter.Match(endpoint.DNSName) {
			records = append(records, newRecord(endpoint))
		}
	}

	return records
}

// submitChange takes a zone and a Change and sends it to Google.
func (p *GoogleProvider) submitChange(ctx context.Context, change *dns.Change) error {
	if len(change.Additions) == 0 && len(change.Deletions) == 0 {
		p.logger.Info("All records are already up to date")
		return nil
	}

	zones, err := p.Zones(ctx)
	if err != nil {
		return err
	}

	// separate into per-zone change sets to be passed to the API.
	changes := separateChange(ctx, zones, change)

	for zone, change := range changes {
		for batch, c := range batchChange(ctx, change, p.batchChangeSize) {
			p.logger.V(1).Info(fmt.Sprintf("Change zone: %v batch #%d", zone, batch))
			for _, del := range c.Deletions {
				p.logger.V(1).Info(fmt.Sprintf("Del records: %s %s %s %d", del.Name, del.Type, del.Rrdatas, del.Ttl))
			}
			for _, add := range c.Additions {
				p.logger.V(1).Info(fmt.Sprintf("Add records: %s %s %s %d", add.Name, add.Type, add.Rrdatas, add.Ttl))
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
func batchChange(ctx context.Context, change *dns.Change, batchSize int) []*dns.Change {
	logger := logr.FromContextOrDiscard(ctx)
	changes := []*dns.Change{}

	if batchSize == 0 {
		return append(changes, change)
	}

	type dnsChange struct {
		additions []*dns.ResourceRecordSet
		deletions []*dns.ResourceRecordSet
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

	currentChange := &dns.Change{}
	var totalChanges int
	for _, name := range names {
		c := changesByName[name]

		totalChangesByName := len(c.additions) + len(c.deletions)

		if totalChangesByName > batchSize {
			logger.Info(fmt.Sprintf("Total changes for %s exceeds max batch size of %d, total changes: %d", name,
				batchSize, totalChangesByName))
			continue
		}

		if totalChanges+totalChangesByName > batchSize {
			totalChanges = 0
			changes = append(changes, currentChange)
			currentChange = &dns.Change{}
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
func separateChange(ctx context.Context, zones map[string]*dns.ManagedZone, change *dns.Change) map[string]*dns.Change {
	logger := logr.FromContextOrDiscard(ctx)
	changes := make(map[string]*dns.Change)
	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		zoneNameIDMapper[z.Name] = z.DnsName
		changes[z.Name] = &dns.Change{
			Additions: []*dns.ResourceRecordSet{},
			Deletions: []*dns.ResourceRecordSet{},
		}
	}
	for _, a := range change.Additions {
		if zoneName, _ := zoneNameIDMapper.FindZone(provider.EnsureTrailingDot(a.Name)); zoneName != "" {
			changes[zoneName].Additions = append(changes[zoneName].Additions, a)
		} else {
			logger.Info(fmt.Sprintf("No matching zone for record addition: %s %s %s %d", a.Name, a.Type, a.Rrdatas, a.Ttl))
		}
	}

	for _, d := range change.Deletions {
		if zoneName, _ := zoneNameIDMapper.FindZone(provider.EnsureTrailingDot(d.Name)); zoneName != "" {
			changes[zoneName].Deletions = append(changes[zoneName].Deletions, d)
		} else {
			logger.Info(fmt.Sprintf("No matching zone for record deletion: %s %s %s %d", d.Name, d.Type, d.Rrdatas, d.Ttl))
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

// newRecord returns a RecordSet based on the given endpoint(google format).
func newRecord(ep *endpoint.Endpoint) *dns.ResourceRecordSet {
	return resourceRecordSetFromEndpoint(ep)
}

// resourceRecordSetFromEndpoint converts an endpoint(google format) into a `ResourceRecordSet`.
func resourceRecordSetFromEndpoint(ep *endpoint.Endpoint) *dns.ResourceRecordSet {

	// no annotation results in a Ttl of 0, default to 300 for backwards-compatibility
	var ttl int64 = googleRecordTTL
	if ep.RecordTTL.IsConfigured() {
		ttl = int64(ep.RecordTTL)
	}

	rrs := &dns.ResourceRecordSet{
		Name: provider.EnsureTrailingDot(ep.DNSName),
		Ttl:  ttl,
		Type: ep.RecordType,
	}

	if rp, ok := ep.GetProviderSpecificProperty("routingpolicy"); ok && ep.RecordType != endpoint.RecordTypeTXT {
		if rp == "geo" {
			rrs.RoutingPolicy = &dns.RRSetRoutingPolicy{
				Geo: &dns.RRSetRoutingPolicyGeoPolicy{},
			}
			//Map location to targets, can ony have one location the same
			targetMap := make(map[string][]string)
			for i := range ep.Targets {
				if location, ok := ep.GetProviderSpecificProperty(ep.Targets[i]); ok {
					targetMap[location] = append(targetMap[location], provider.EnsureTrailingDot(ep.Targets[i]))
				}
			}
			for l, t := range targetMap {
				item := &dns.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
					Location: l,
					Rrdatas:  t,
				}
				rrs.RoutingPolicy.Geo.Items = append(rrs.RoutingPolicy.Geo.Items, item)
			}
		} else if rp == "weighted" {
			rrs.RoutingPolicy = &dns.RRSetRoutingPolicy{
				Wrr: &dns.RRSetRoutingPolicyWrrPolicy{},
			}

			for i := range ep.Targets {
				if weightStr, ok := ep.GetProviderSpecificProperty(ep.Targets[i]); ok {
					weight, err := strconv.ParseFloat(weightStr, 64)
					if err != nil {
						weight = 0.0
					}
					item := &dns.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
						Rrdatas: []string{provider.EnsureTrailingDot(ep.Targets[i])},
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
	if ep.RecordType == endpoint.RecordTypeCNAME {
		if len(targets) > 0 {
			targets[0] = provider.EnsureTrailingDot(targets[0])
		}
	}

	if ep.RecordType == endpoint.RecordTypeMX {
		for i, mxRecord := range ep.Targets {
			targets[i] = provider.EnsureTrailingDot(mxRecord)
		}
	}

	if ep.RecordType == endpoint.RecordTypeSRV {
		for i, srvRecord := range ep.Targets {
			targets[i] = provider.EnsureTrailingDot(srvRecord)
		}
	}

	rrs.Rrdatas = targets

	return rrs
}
