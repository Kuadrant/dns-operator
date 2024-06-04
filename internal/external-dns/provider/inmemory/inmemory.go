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

package inmemory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

var (
	// ErrZoneAlreadyExists error returned when Zone cannot be created when it already exists
	ErrZoneAlreadyExists = errors.New("specified Zone already exists")
	// ErrZoneNotFound error returned when specified Zone does not exists
	ErrZoneNotFound = errors.New("specified Zone not found")
	// ErrRecordAlreadyExists when create request is sent but record already exists
	ErrRecordAlreadyExists = errors.New("record already exists")
	// ErrRecordNotFound when update/delete request is sent but record not found
	ErrRecordNotFound = errors.New("record not found")
	// ErrDuplicateRecordFound when record is repeated in create/update/delete
	ErrDuplicateRecordFound = errors.New("invalid batch request")
)

// InMemoryProvider - dns provider only used for testing purposes
// initialized as dns provider with no records
type InMemoryProvider struct {
	provider.BaseProvider
	logger         logr.Logger
	domain         endpoint.DomainFilter
	client         *InMemoryClient
	filter         *filter
	OnApplyChanges func(ctx context.Context, changes *plan.Changes)
	OnRecords      func()
}

// InMemoryOption allows to extend in-memory provider
type InMemoryOption func(*InMemoryProvider)

// InMemoryWithLogging injects logging when ApplyChanges is called
func InMemoryWithLogging() InMemoryOption {
	return func(p *InMemoryProvider) {
		p.OnApplyChanges = func(ctx context.Context, changes *plan.Changes) {
			for _, v := range changes.Create {
				p.logger.Info(fmt.Sprintf("CREATE: %v", v))
			}
			for _, v := range changes.UpdateOld {
				p.logger.Info(fmt.Sprintf("UPDATE (old): %v", v))
			}
			for _, v := range changes.UpdateNew {
				p.logger.Info(fmt.Sprintf("UPDATE (new): %v", v))
			}
			for _, v := range changes.Delete {
				p.logger.Info(fmt.Sprintf("DELETE: %v", v))
			}
		}
	}
}

// InMemoryWithDomain modifies the domain on which dns zones are filtered
func InMemoryWithDomain(domainFilter endpoint.DomainFilter) InMemoryOption {
	return func(p *InMemoryProvider) {
		p.domain = domainFilter
	}
}

// InMemoryInitZones pre-seeds the InMemoryProvider with given zones
func InMemoryInitZones(zones []string) InMemoryOption {
	return func(p *InMemoryProvider) {
		for _, z := range zones {
			if err := p.CreateZone(z); err != nil {
				p.logger.Info("Unable to initialize zones for inmemory provider")
			}
		}
	}
}

// InMemoryWithClient modifies the client which the provider will use
func InMemoryWithClient(memoryClient *InMemoryClient) InMemoryOption {
	return func(p *InMemoryProvider) {
		p.client = memoryClient
	}
}

// NewInMemoryProvider returns InMemoryProvider DNS provider interface implementation
func NewInMemoryProvider(ctx context.Context, opts ...InMemoryOption) *InMemoryProvider {
	logger := logr.FromContextOrDiscard(ctx)
	im := &InMemoryProvider{
		logger:         logger,
		filter:         &filter{},
		OnApplyChanges: func(ctx context.Context, changes *plan.Changes) {},
		OnRecords:      func() {},
		domain:         endpoint.NewDomainFilter([]string{""}),
		client:         NewInMemoryClient(),
	}

	for _, opt := range opts {
		opt(im)
	}

	return im
}

// CreateZone adds new Zone if not present
func (im *InMemoryProvider) CreateZone(newZone string) error {
	return im.client.CreateZone(newZone)
}

// DeleteZone deletes a Zone if present
func (im *InMemoryProvider) DeleteZone(zone string) error {
	return im.client.DeleteZone(zone)
}

// GetZone gets a Zone if present
func (im *InMemoryProvider) GetZone(zone string) (Zone, error) {
	return im.client.GetZone(zone)
}

// Zones returns filtered zones as specified by domain
func (im *InMemoryProvider) Zones() map[string]string {
	return im.filter.Zones(im.client.Zones())
}

// Records returns the list of endpoints
func (im *InMemoryProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	defer im.OnRecords()

	endpoints := make([]*endpoint.Endpoint, 0)

	for zoneID := range im.Zones() {
		records, err := im.client.Records(zoneID)
		if err != nil {
			return nil, err
		}

		endpoints = append(endpoints, copyEndpoints(records)...)
	}

	return endpoints, nil
}

// ApplyChanges simply modifies records in memory
// error checking occurs before any modifications are made, i.e. batch processing
// create record - record should not exist
// update/delete record - record should exist
// create/update/delete lists should not have overlapping records
func (im *InMemoryProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	defer im.OnApplyChanges(ctx, changes)

	perZoneChanges := map[string]*plan.Changes{}

	zones := im.Zones()
	for zoneID := range zones {
		perZoneChanges[zoneID] = &plan.Changes{}
	}

	for _, ep := range changes.Create {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].Create = append(perZoneChanges[zoneID].Create, ep)
	}
	for _, ep := range changes.UpdateNew {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].UpdateNew = append(perZoneChanges[zoneID].UpdateNew, ep)
	}
	for _, ep := range changes.UpdateOld {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].UpdateOld = append(perZoneChanges[zoneID].UpdateOld, ep)
	}
	for _, ep := range changes.Delete {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].Delete = append(perZoneChanges[zoneID].Delete, ep)
	}

	for zoneID := range perZoneChanges {
		change := &plan.Changes{
			Create:    perZoneChanges[zoneID].Create,
			UpdateNew: perZoneChanges[zoneID].UpdateNew,
			UpdateOld: perZoneChanges[zoneID].UpdateOld,
			Delete:    perZoneChanges[zoneID].Delete,
		}
		err := im.client.ApplyChanges(ctx, zoneID, change)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	records := make([]*endpoint.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		newEp := endpoint.NewEndpointWithTTL(ep.DNSName, ep.RecordType, ep.RecordTTL, ep.Targets...).WithSetIdentifier(ep.SetIdentifier)
		newEp.Labels = endpoint.NewLabels()
		for k, v := range ep.Labels {
			newEp.Labels[k] = v
		}
		newEp.ProviderSpecific = append(endpoint.ProviderSpecific(nil), ep.ProviderSpecific...)
		records = append(records, newEp)
	}
	return records
}

type filter struct {
	domain string
}

// Zones filters map[zoneID]zoneName for names having f.domain as suffix
func (f *filter) Zones(zones map[string]string) map[string]string {
	result := map[string]string{}
	for zoneID, zoneName := range zones {
		if strings.HasSuffix(zoneName, f.domain) {
			result[zoneID] = zoneName
		}
	}
	return result
}

// EndpointZoneID determines zoneID for endpoint from map[zoneID]zoneName by taking longest suffix zoneName match in endpoint DNSName
// returns empty string if no match found
func (f *filter) EndpointZoneID(endpoint *endpoint.Endpoint, zones map[string]string) (zoneID string) {
	var matchZoneID, matchZoneName string
	for zoneID, zoneName := range zones {
		if strings.HasSuffix(endpoint.DNSName, zoneName) && len(zoneName) > len(matchZoneName) {
			matchZoneName = zoneName
			matchZoneID = zoneID
		}
	}
	return matchZoneID
}

type Zone map[endpoint.EndpointKey]*endpoint.Endpoint

type InMemoryClient struct {
	zones map[string]Zone
}

func NewInMemoryClient() *InMemoryClient {
	return &InMemoryClient{map[string]Zone{}}
}

func (c *InMemoryClient) Records(zone string) ([]*endpoint.Endpoint, error) {
	if _, ok := c.zones[zone]; !ok {
		return nil, ErrZoneNotFound
	}

	var records []*endpoint.Endpoint
	for _, rec := range c.zones[zone] {
		records = append(records, rec)
	}
	return records, nil
}

func (c *InMemoryClient) Zones() map[string]string {
	zones := map[string]string{}
	for zone := range c.zones {
		zones[zone] = zone
	}
	return zones
}

func (c *InMemoryClient) CreateZone(zone string) error {
	if _, ok := c.zones[zone]; ok {
		return ErrZoneAlreadyExists
	}
	c.zones[zone] = map[endpoint.EndpointKey]*endpoint.Endpoint{}

	return nil
}

func (c *InMemoryClient) DeleteZone(zone string) error {
	if _, ok := c.zones[zone]; ok {
		delete(c.zones, zone)
		return nil
	}
	return ErrZoneNotFound
}

func (c *InMemoryClient) GetZone(zone string) (Zone, error) {
	if _, ok := c.zones[zone]; ok {
		return c.zones[zone], nil
	}
	return nil, ErrZoneNotFound
}

func (c *InMemoryClient) ApplyChanges(ctx context.Context, zoneID string, changes *plan.Changes) error {
	if err := c.validateChangeBatch(zoneID, changes); err != nil {
		return err
	}
	for _, newEndpoint := range changes.Create {
		c.zones[zoneID][newEndpoint.Key()] = newEndpoint
	}
	for _, updateEndpoint := range changes.UpdateNew {
		c.zones[zoneID][updateEndpoint.Key()] = updateEndpoint
	}
	for _, deleteEndpoint := range changes.Delete {
		delete(c.zones[zoneID], deleteEndpoint.Key())
	}
	return nil
}

func (c *InMemoryClient) updateMesh(mesh sets.Set[endpoint.EndpointKey], record *endpoint.Endpoint) error {
	if mesh.Has(record.Key()) {
		return ErrDuplicateRecordFound
	}
	mesh.Insert(record.Key())
	return nil
}

// validateChangeBatch validates that the changes passed to InMemory DNS provider is valid
func (c *InMemoryClient) validateChangeBatch(zone string, changes *plan.Changes) error {
	curZone, ok := c.zones[zone]
	if !ok {
		return ErrZoneNotFound
	}
	mesh := sets.New[endpoint.EndpointKey]()
	for _, newEndpoint := range changes.Create {
		if _, exists := curZone[newEndpoint.Key()]; exists {
			return ErrRecordAlreadyExists
		}
		if err := c.updateMesh(mesh, newEndpoint); err != nil {
			return err
		}
	}
	for _, updateEndpoint := range changes.UpdateNew {
		if _, exists := curZone[updateEndpoint.Key()]; !exists {
			return ErrRecordNotFound
		}
		if err := c.updateMesh(mesh, updateEndpoint); err != nil {
			return err
		}
	}
	for _, updateOldEndpoint := range changes.UpdateOld {
		if rec, exists := curZone[updateOldEndpoint.Key()]; !exists || rec.Targets[0] != updateOldEndpoint.Targets[0] {
			return ErrRecordNotFound
		}
	}
	for _, deleteEndpoint := range changes.Delete {
		if rec, exists := curZone[deleteEndpoint.Key()]; !exists || rec.Targets[0] != deleteEndpoint.Targets[0] {
			return ErrRecordNotFound
		}
		if err := c.updateMesh(mesh, deleteEndpoint); err != nil {
			return err
		}
	}
	return nil
}
