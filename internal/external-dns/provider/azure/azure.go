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

//nolint:staticcheck // Required due to the current dependency on a deprecated version of azure-sdk-for-go
package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"

	azcoreruntime "github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	dns "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/go-logr/logr"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	azureRecordTTL = 300
)

// ZonesClient is an interface of dns.ZoneClient that can be stubbed for testing.
type ZonesClient interface {
	NewListByResourceGroupPager(resourceGroupName string, options *dns.ZonesClientListByResourceGroupOptions) *azcoreruntime.Pager[dns.ZonesClientListByResourceGroupResponse]
}

// RecordSetsClient is an interface of dns.RecordSetsClient that can be stubbed for testing.
type RecordSetsClient interface {
	NewListAllByDNSZonePager(resourceGroupName string, zoneName string, options *dns.RecordSetsClientListAllByDNSZoneOptions) *azcoreruntime.Pager[dns.RecordSetsClientListAllByDNSZoneResponse]
	Delete(ctx context.Context, resourceGroupName string, zoneName string, relativeRecordSetName string, recordType dns.RecordType, options *dns.RecordSetsClientDeleteOptions) (dns.RecordSetsClientDeleteResponse, error)
	CreateOrUpdate(ctx context.Context, resourceGroupName string, zoneName string, relativeRecordSetName string, recordType dns.RecordType, parameters dns.RecordSet, options *dns.RecordSetsClientCreateOrUpdateOptions) (dns.RecordSetsClientCreateOrUpdateResponse, error)
}

// AzureProvider implements the DNS provider for Microsoft's Azure cloud platform.
type AzureProvider struct {
	provider.BaseProvider
	DomainFilter                   endpoint.DomainFilter
	ZoneNameFilter                 endpoint.DomainFilter
	zoneIDFilter                   provider.ZoneIDFilter
	DryRun                         bool
	ResourceGroup                  string
	userAssignedIdentityClientID   string
	zonesClient                    ZonesClient
	RecordSetsClient               RecordSetsClient
	TrafficManagerEndpointsClient  *armtrafficmanager.EndpointsClient
	TrafficManagerGeographicClient *armtrafficmanager.GeographicHierarchiesClient
	TrafficManagerProfilesClient   *armtrafficmanager.ProfilesClient
	logger                         logr.Logger
}

func NewAzureProviderFromConfig(ctx context.Context, azureConfig Config) (*AzureProvider, error) {
	cred, clientOpts, err := getCredentials(ctx, azureConfig)
	if err != nil {
		return nil, err
	}

	zonesClient, err := dns.NewZonesClient(azureConfig.SubscriptionID, cred, clientOpts)
	if err != nil {
		return nil, err
	}

	recordSetsClient, err := dns.NewRecordSetsClient(azureConfig.SubscriptionID, cred, clientOpts)
	if err != nil {
		return nil, err
	}

	clientFactory, err := armtrafficmanager.NewClientFactory(azureConfig.SubscriptionID, cred, clientOpts)
	if err != nil {
		return nil, err
	}

	p := &AzureProvider{
		DomainFilter:                   azureConfig.DomainFilter,
		ZoneNameFilter:                 azureConfig.ZoneNameFilter,
		zoneIDFilter:                   azureConfig.IDFilter,
		DryRun:                         azureConfig.DryRun,
		zonesClient:                    zonesClient,
		RecordSetsClient:               recordSetsClient,
		TrafficManagerEndpointsClient:  clientFactory.NewEndpointsClient(),
		TrafficManagerGeographicClient: clientFactory.NewGeographicHierarchiesClient(),
		TrafficManagerProfilesClient:   clientFactory.NewProfilesClient(),
		ResourceGroup:                  azureConfig.ResourceGroup,
		logger:                         logr.FromContextOrDiscard(ctx),
	}

	return p, nil
}

// NewAzureProvider creates a new Azure provider.
//
// Returns the provider or an error if a provider could not be created.
func NewAzureProvider(ctx context.Context, configFile string, domainFilter endpoint.DomainFilter, zoneNameFilter endpoint.DomainFilter, zoneIDFilter provider.ZoneIDFilter, resourceGroup string, userAssignedIdentityClientID string, dryRun bool) (*AzureProvider, error) {
	cfg, err := getConfig(configFile, resourceGroup, userAssignedIdentityClientID)
	if err != nil {
		return nil, fmt.Errorf("failed to read Azure config file '%s': %v", configFile, err)
	}

	cfg.DomainFilter = domainFilter
	cfg.ZoneNameFilter = zoneNameFilter
	cfg.IDFilter = zoneIDFilter
	cfg.DryRun = dryRun

	return NewAzureProviderFromConfig(ctx, *cfg)
}

// Records gets the current records.
//
// Returns the current records or an error if the operation failed.
func (p *AzureProvider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, _ error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		pager := p.RecordSetsClient.NewListAllByDNSZonePager(p.ResourceGroup, *zone.Name, &dns.RecordSetsClientListAllByDNSZoneOptions{Top: nil})
		for pager.More() {
			nextResult, err := pager.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, recordSet := range nextResult.Value {
				if recordSet.Name == nil || recordSet.Type == nil {
					p.logger.Error(errors.New("record set has nil name or type"), "Skipping invalid record set with nil name or type")
					continue
				}
				recordType := strings.TrimPrefix(*recordSet.Type, "Microsoft.Network/dnszones/")
				if !p.SupportedRecordType(recordType) {
					continue
				}
				name := FormatAzureDNSName(*recordSet.Name, *zone.Name)
				if len(p.ZoneNameFilter.Filters) > 0 && !p.DomainFilter.Match(name) {
					p.logger.V(1).Info("skipping return of record because it was filtered out by the specified --domain-filter", "record name", name)
					continue
				}
				targets := ExtractAzureTargets(recordSet)
				if len(targets) == 0 {
					p.logger.V(1).Info("failed to extract targets from record set", "record name", name, "record type", recordType)
					continue
				}
				var ttl endpoint.TTL
				if recordSet.Properties.TTL != nil {
					ttl = endpoint.TTL(*recordSet.Properties.TTL)
				}
				ep := endpoint.NewEndpointWithTTL(name, recordType, ttl, targets...)
				p.logger.V(1).Info("found record set", "record type", ep.RecordType, "DNS Name", ep.DNSName, "targets", ep.Targets)
				endpoints = append(endpoints, ep)
			}
		}
	}
	return endpoints, nil
}

// ApplyChanges applies the given changes.
//
// Returns nil if the operation was successful or an error if the operation failed.
func (p *AzureProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	zones, err := p.Zones(ctx)
	if err != nil {
		return err
	}

	deleted, updated := p.MapChanges(zones, changes)
	p.DeleteRecords(ctx, deleted)
	p.UpdateRecords(ctx, updated)
	return nil
}

func (p *AzureProvider) Zones(ctx context.Context) ([]dns.Zone, error) {
	p.logger.V(1).Info("retrieving azure DNS Zones for resource group", "resource group", p.ResourceGroup)
	var zones []dns.Zone
	pager := p.zonesClient.NewListByResourceGroupPager(p.ResourceGroup, &dns.ZonesClientListByResourceGroupOptions{Top: nil})
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, zone := range nextResult.Value {
			if zone.Name != nil && p.DomainFilter.Match(*zone.Name) && p.zoneIDFilter.Match(*zone.ID) {
				zones = append(zones, *zone)
			} else if zone.Name != nil && len(p.ZoneNameFilter.Filters) > 0 && p.ZoneNameFilter.Match(*zone.Name) {
				// Handle ZoneNameFilter
				zones = append(zones, *zone)
			}
		}
	}
	p.logger.V(1).Info("found azure DNS Zones", "zones count", len(zones))
	return zones, nil
}

func (p *AzureProvider) SupportedRecordType(recordType string) bool {
	switch recordType {
	case "MX":
		return true
	default:
		return provider.SupportedRecordType(recordType)
	}
}

type AzureChangeMap map[string][]*endpoint.Endpoint

func (p *AzureProvider) MapChanges(zones []dns.Zone, changes *plan.Changes) (AzureChangeMap, AzureChangeMap) {
	ignored := map[string]bool{}
	deleted := AzureChangeMap{}
	updated := AzureChangeMap{}
	zoneNameIDMapper := provider.ZoneIDName{}
	for _, z := range zones {
		if z.Name != nil {
			zoneNameIDMapper.Add(*z.Name, *z.Name)
		}
	}
	mapChange := func(changeMap AzureChangeMap, change *endpoint.Endpoint) {
		zone, _ := zoneNameIDMapper.FindZone(change.DNSName)
		if zone == "" {
			if _, ok := ignored[change.DNSName]; !ok {
				ignored[change.DNSName] = true
				p.logger.Info("ignoring changes because a suitable Azure DNS zone was not found", "change DNS Name", change.DNSName)
			}
			return
		}
		// Ensure the record type is suitable
		changeMap[zone] = append(changeMap[zone], change)
	}

	for _, change := range changes.Delete {
		mapChange(deleted, change)
	}

	for _, change := range changes.Create {
		mapChange(updated, change)
	}

	for _, change := range changes.UpdateNew {
		mapChange(updated, change)
	}
	return deleted, updated
}

func (p *AzureProvider) DeleteRecords(ctx context.Context, deleted AzureChangeMap) {
	// Delete records first
	for zone, endpoints := range deleted {
		for _, ep := range endpoints {
			name := p.RecordSetNameForZone(zone, ep)
			if !p.DomainFilter.Match(ep.DNSName) {
				p.logger.V(1).Info("skipping deletion of record as it was filtered out by the specified --domain-filter", "record name", ep.DNSName)
				continue
			}
			if p.DryRun {
				p.logger.Info("would delete record", "record type", ep.RecordType, "record name", name, "zone", zone)
			} else {
				p.logger.Info("deleting record", "record type", ep.RecordType, "record name", name, "zone", zone)
				if _, err := p.RecordSetsClient.Delete(ctx, p.ResourceGroup, zone, name, dns.RecordType(ep.RecordType), nil); err != nil {
					p.logger.Error(err, "failed to delete record", "record type", ep.RecordType, "record name", name, "zone", zone)
				}
			}
		}
	}
}

func (p *AzureProvider) UpdateRecords(ctx context.Context, updated AzureChangeMap) {
	for zone, endpoints := range updated {
		for _, ep := range endpoints {
			name := p.RecordSetNameForZone(zone, ep)
			if !p.DomainFilter.Match(ep.DNSName) {
				p.logger.V(1).Info("skipping update of record because it was filtered by the specified --domain-filter", "record name", ep.DNSName)
				continue
			}
			if p.DryRun {
				p.logger.Info("would update record", "record type", ep.RecordType, "record name", name, "targets", ep.Targets, "zone", zone)
				continue
			}
			p.logger.Info("updating record", "record type", ep.RecordType, "record name", name, "targets", ep.Targets, "zone", zone)

			recordSet, err := p.NewRecordSet(ep)
			if err == nil {
				_, err = p.RecordSetsClient.CreateOrUpdate(
					ctx,
					p.ResourceGroup,
					zone,
					name,
					dns.RecordType(ep.RecordType),
					recordSet,
					nil,
				)
			}
			if err != nil {
				p.logger.Error(err, "failed to update record", "record type", ep.RecordType, "record name", name, "targets", ep.Targets, "zone", zone)
			}
		}
	}
}

func (p *AzureProvider) RecordSetNameForZone(zone string, endpoint *endpoint.Endpoint) string {
	// Remove the zone from the record set
	name := endpoint.DNSName
	name = name[:len(name)-len(zone)]
	name = strings.TrimSuffix(name, ".")

	// For root, use @
	if name == "" {
		return "@"
	}
	return name
}

func (p *AzureProvider) NewRecordSet(endpoint *endpoint.Endpoint) (dns.RecordSet, error) {
	var ttl int64 = azureRecordTTL
	if endpoint.RecordTTL.IsConfigured() {
		ttl = int64(endpoint.RecordTTL)
	}
	switch dns.RecordType(endpoint.RecordType) {
	case dns.RecordTypeA:
		aRecords := make([]*dns.ARecord, len(endpoint.Targets))
		for i, target := range endpoint.Targets {
			aRecords[i] = &dns.ARecord{
				IPv4Address: to.Ptr(target),
			}
		}
		return dns.RecordSet{
			Properties: &dns.RecordSetProperties{
				TTL:      to.Ptr(ttl),
				ARecords: aRecords,
			},
		}, nil
	case dns.RecordTypeAAAA:
		aaaaRecords := make([]*dns.AaaaRecord, len(endpoint.Targets))
		for i, target := range endpoint.Targets {
			aaaaRecords[i] = &dns.AaaaRecord{
				IPv6Address: to.Ptr(target),
			}
		}
		return dns.RecordSet{
			Properties: &dns.RecordSetProperties{
				TTL:         to.Ptr(ttl),
				AaaaRecords: aaaaRecords,
			},
		}, nil
	case dns.RecordTypeCNAME:
		return dns.RecordSet{
			Properties: &dns.RecordSetProperties{
				TTL: to.Ptr(ttl),
				CnameRecord: &dns.CnameRecord{
					Cname: to.Ptr(endpoint.Targets[0]),
				},
			},
		}, nil
	case dns.RecordTypeMX:
		mxRecords := make([]*dns.MxRecord, len(endpoint.Targets))
		for i, target := range endpoint.Targets {
			mxRecord, err := parseMxTarget[dns.MxRecord](target)
			if err != nil {
				return dns.RecordSet{}, err
			}
			mxRecords[i] = &mxRecord
		}
		return dns.RecordSet{
			Properties: &dns.RecordSetProperties{
				TTL:       to.Ptr(ttl),
				MxRecords: mxRecords,
			},
		}, nil
	case dns.RecordTypeTXT:
		return dns.RecordSet{
			Properties: &dns.RecordSetProperties{
				TTL: to.Ptr(ttl),
				TxtRecords: []*dns.TxtRecord{
					{
						Value: []*string{
							&endpoint.Targets[0],
						},
					},
				},
			},
		}, nil
	}
	return dns.RecordSet{}, fmt.Errorf("unsupported record type '%s'", endpoint.RecordType)
}

// Helper function (shared with test code)
func FormatAzureDNSName(recordName, zoneName string) string {
	if recordName == "@" {
		return zoneName
	}
	return fmt.Sprintf("%s.%s", recordName, zoneName)
}

// Helper function (shared with test code)
func ExtractAzureTargets(recordSet *dns.RecordSet) []string {
	properties := recordSet.Properties
	if properties == nil {
		return []string{}
	}

	// Check for A records
	aRecords := properties.ARecords
	if len(aRecords) > 0 && (aRecords)[0].IPv4Address != nil {
		targets := make([]string, len(aRecords))
		for i, aRecord := range aRecords {
			targets[i] = *aRecord.IPv4Address
		}
		return targets
	}

	// Check for AAAA records
	aaaaRecords := properties.AaaaRecords
	if len(aaaaRecords) > 0 && (aaaaRecords)[0].IPv6Address != nil {
		targets := make([]string, len(aaaaRecords))
		for i, aaaaRecord := range aaaaRecords {
			targets[i] = *aaaaRecord.IPv6Address
		}
		return targets
	}

	// Check for CNAME records
	cnameRecord := properties.CnameRecord
	if cnameRecord != nil && cnameRecord.Cname != nil {
		return []string{*cnameRecord.Cname}
	}

	// Check for MX records
	mxRecords := properties.MxRecords
	if len(mxRecords) > 0 && (mxRecords)[0].Exchange != nil {
		targets := make([]string, len(mxRecords))
		for i, mxRecord := range mxRecords {
			targets[i] = fmt.Sprintf("%d %s", *mxRecord.Preference, *mxRecord.Exchange)
		}
		return targets
	}

	// Check for TXT records
	txtRecords := properties.TxtRecords
	if len(txtRecords) > 0 && (txtRecords)[0].Value != nil {
		values := (txtRecords)[0].Value
		if len(values) > 0 {
			return []string{*(values)[0]}
		}
	}
	return []string{}
}
