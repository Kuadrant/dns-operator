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
	"net"
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

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-dns-operator/internal/provider"
)

type action string

const (
	GoogleBatchChangeSize            = 1000
	GoogleBatchChangeInterval        = time.Second
	DryRun                           = false
	upsertAction              action = "UPSERT"
	deleteAction              action = "DELETE"
	defaultGeo                       = "europe-west1"
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

func NewProviderFromSecret(ctx context.Context, s *v1.Secret) (provider.Provider, error) {

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
		resourceRecordSetsClient: resourceRecordSetsService{dnsClient.ResourceRecordSets},
		managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
		changesClient:            changesService{dnsClient.Changes},
		ctx:                      ctx,
	}

	return p, nil
}

// ManagedZones

func (g *GoogleDNSProvider) DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error {
	return g.managedZonesClient.Delete(g.project, managedZone.Status.ID).Do()
}

func (g *GoogleDNSProvider) EnsureManagedZone(managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var zoneID string

	if managedZone.Spec.ID != "" {
		zoneID = managedZone.Spec.ID
	} else {
		zoneID = managedZone.Status.ID
	}

	if zoneID != "" {
		//Get existing managed zone
		return g.getManagedZone(zoneID)
	}
	//Create new managed zone
	return g.createManagedZone(managedZone)
}

func (g *GoogleDNSProvider) createManagedZone(managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	zoneID := strings.Replace(managedZone.Spec.DomainName, ".", "-", -1)
	zone := dnsv1.ManagedZone{
		Name:        zoneID,
		DnsName:     ensureTrailingDot(managedZone.Spec.DomainName),
		Description: managedZone.Spec.Description,
	}
	mz, err := g.managedZonesClient.Create(g.project, &zone).Do()
	if err != nil {
		return provider.ManagedZoneOutput{}, err
	}
	return g.toManagedZoneOutput(mz)
}

func (g *GoogleDNSProvider) getManagedZone(zoneID string) (provider.ManagedZoneOutput, error) {
	mz, err := g.managedZonesClient.Get(g.project, zoneID).Do()
	if err != nil {
		return provider.ManagedZoneOutput{}, err
	}
	return g.toManagedZoneOutput(mz)
}

func (g *GoogleDNSProvider) toManagedZoneOutput(mz *dnsv1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var managedZoneOutput provider.ManagedZoneOutput

	zoneID := mz.Name
	var nameservers []*string
	for i := range mz.NameServers {
		nameservers = append(nameservers, &mz.NameServers[i])
	}
	managedZoneOutput.ID = zoneID
	managedZoneOutput.NameServers = nameservers

	currentRecords, err := g.getResourceRecordSets(g.ctx, zoneID)
	if err != nil {
		return managedZoneOutput, err
	}
	managedZoneOutput.RecordCount = int64(len(currentRecords))

	return managedZoneOutput, nil
}

//DNSRecords

func (g *GoogleDNSProvider) Ensure(record *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone) error {
	return g.updateRecord(record, managedZone.Status.ID, upsertAction)
}

func (g *GoogleDNSProvider) Delete(record *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone) error {
	return g.updateRecord(record, managedZone.Status.ID, deleteAction)
}

func (g *GoogleDNSProvider) updateRecord(dnsRecord *v1alpha1.DNSRecord, zoneID string, action action) error {
	// When updating records the Google DNS API expects you to delete any existing record and add the new one as part of
	// the same change request. The record to be deleted must match exactly what currently exists in the provider or the
	// change request will fail. To make sure we can always remove the records, we first get all records that exist in
	// the zone and build up the deleting list from `dnsRecord.Status` but use the most recent version of it retrieved
	// from the provider in the change request.
	currentRecords, err := g.getResourceRecordSets(g.ctx, zoneID)
	if err != nil {
		return err
	}
	currentRecordsMap := make(map[string]*dnsv1.ResourceRecordSet)
	for _, record := range currentRecords {
		currentRecordsMap[record.Name] = record
	}
	statusRecords := toResourceRecordSets(dnsRecord.Status.Endpoints)
	statusRecordsMap := make(map[string]*dnsv1.ResourceRecordSet)
	for _, record := range statusRecords {
		statusRecordsMap[record.Name] = record
	}

	var deletingRecords []*dnsv1.ResourceRecordSet
	for name := range statusRecordsMap {
		if record, ok := currentRecordsMap[name]; ok {
			deletingRecords = append(deletingRecords, record)
		}
	}
	addingRecords := toResourceRecordSets(dnsRecord.Spec.Endpoints)

	g.logger.V(1).Info("updateRecord", "currentRecords", currentRecords, "deletingRecords", deletingRecords, "addingRecords", addingRecords)

	change := &dnsv1.Change{}
	if action == deleteAction {
		change.Deletions = deletingRecords
	} else {
		change.Deletions = deletingRecords
		change.Additions = addingRecords
	}

	return g.submitChange(change, zoneID)
}

func (g *GoogleDNSProvider) submitChange(change *dnsv1.Change, zone string) error {
	if len(change.Additions) == 0 && len(change.Deletions) == 0 {
		g.logger.Info("All records are already up to date")
		return nil
	}

	for batch, c := range g.batchChange(change, g.batchChangeSize) {
		g.logger.V(1).Info("Change zone", "zone", zone, "batch", batch)
		for _, del := range c.Deletions {
			g.logger.V(1).Info("Del records", "name", del.Name, "type", del.Type, "Rrdatas",
				del.Rrdatas, "RoutingPolicy", del.RoutingPolicy, "ttl", del.Ttl)
		}
		for _, add := range c.Additions {
			g.logger.V(1).Info("Add records", "name", add.Name, "type", add.Type, "Rrdatas",
				add.Rrdatas, "RoutingPolicy", add.RoutingPolicy, "ttl", add.Ttl)
		}
		if g.dryRun {
			continue
		}

		if _, err := g.changesClient.Create(g.project, zone, c).Do(); err != nil {
			return err
		}
		time.Sleep(g.batchChangeInterval)
	}
	return nil
}

func (g *GoogleDNSProvider) batchChange(change *dnsv1.Change, batchSize int) []*dnsv1.Change {
	changes := []*dnsv1.Change{}

	if batchSize == 0 {
		return append(changes, change)
	}

	type dnsv1Change struct {
		additions []*dnsv1.ResourceRecordSet
		deletions []*dnsv1.ResourceRecordSet
	}

	changesByName := map[string]*dnsv1Change{}

	for _, a := range change.Additions {
		change, ok := changesByName[a.Name]
		if !ok {
			change = &dnsv1Change{}
			changesByName[a.Name] = change
		}

		change.additions = append(change.additions, a)
	}

	for _, a := range change.Deletions {
		change, ok := changesByName[a.Name]
		if !ok {
			change = &dnsv1Change{}
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
			g.logger.V(1).Info("Total changes for %s exceeds max batch size of %d, total changes: %d", name,
				batchSize, totalChangesByName)
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

// getResourceRecordSets returns the records for a managed zone of the currently configured provider.
func (g *GoogleDNSProvider) getResourceRecordSets(ctx context.Context, zoneID string) ([]*dnsv1.ResourceRecordSet, error) {
	var records []*dnsv1.ResourceRecordSet

	f := func(resp *dnsv1.ResourceRecordSetsListResponse) error {
		records = append(records, resp.Rrsets...)
		return nil
	}

	if err := g.resourceRecordSetsClient.List(g.project, zoneID).Pages(ctx, f); err != nil {
		return nil, err
	}

	return records, nil
}

// toResourceRecordSets converts a list of endpoints into `ResourceRecordSet` resources.
func toResourceRecordSets(allEndpoints []*v1alpha1.Endpoint) []*dnsv1.ResourceRecordSet {
	var records []*dnsv1.ResourceRecordSet

	// Google DNS requires a record to be created per `dnsName`, so the first thing we need to do is group all the
	// endpoints with the same dnsName together.
	endpointMap := make(map[string][]*v1alpha1.Endpoint)
	for _, ep := range allEndpoints {
		endpointMap[ep.DNSName] = append(endpointMap[ep.DNSName], ep)
	}

	for dnsName, endpoints := range endpointMap {
		// A set of endpoints belonging to the same group(`dnsName`) must always be of the same type, have the same ttl
		// and contain the same rrdata (weighted or geo), so we can just get that from the first endpoint in the list.
		ttl := int64(endpoints[0].RecordTTL)
		recordType := endpoints[0].RecordType
		_, weighted := endpoints[0].GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
		_, geoCode := endpoints[0].GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)

		record := &dnsv1.ResourceRecordSet{
			Name: ensureTrailingDot(dnsName),
			Ttl:  ttl,
			Type: recordType,
		}
		if weighted {
			record.RoutingPolicy = &dnsv1.RRSetRoutingPolicy{
				Wrr: &dnsv1.RRSetRoutingPolicyWrrPolicy{},
			}
		} else if geoCode {
			record.RoutingPolicy = &dnsv1.RRSetRoutingPolicy{
				Geo: &dnsv1.RRSetRoutingPolicyGeoPolicy{},
			}
		}

		for _, ep := range endpoints {
			targets := make([]string, len(ep.Targets))
			copy(targets, ep.Targets)
			if ep.RecordType == string(v1alpha1.CNAMERecordType) {
				targets[0] = ensureTrailingDot(targets[0])
			}

			if !weighted && !geoCode {
				record.Rrdatas = targets
			}
			if weighted {
				weightProp, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
				weight, err := strconv.ParseFloat(weightProp.Value, 64)
				if err != nil {
					weight = 0
				}
				item := &dnsv1.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
					Rrdatas: targets,
					Weight:  weight,
				}
				record.RoutingPolicy.Wrr.Items = append(record.RoutingPolicy.Wrr.Items, item)
			}
			if geoCode {
				geoCodeProp, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
				geoCodeValue := geoCodeProp.Value
				targetIsDefaultGroup := strings.HasPrefix(ep.Targets[0], v1alpha1.DefaultGeo)
				// GCP doesn't accept * as value for default geolocations like AWS does.
				// To ensure the dns chain doesn't break if a * is given we map the value to europe-west1 instead
				// We cant drop the record as the chain will break
				if geoCodeValue == "*" {
					if !targetIsDefaultGroup {
						continue
					}
					geoCodeValue = defaultGeo
				}
				item := &dnsv1.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
					Location: geoCodeValue,
					Rrdatas:  targets,
				}
				record.RoutingPolicy.Geo.Items = append(record.RoutingPolicy.Geo.Items, item)
			}
		}
		records = append(records, record)
	}
	return records
}

// ensureTrailingDot ensures that the hostname receives a trailing dot if it hasn't already.
func ensureTrailingDot(hostname string) string {
	if net.ParseIP(hostname) != nil {
		return hostname
	}

	return strings.TrimSuffix(hostname, ".") + "."
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider("google", NewProviderFromSecret)
}
