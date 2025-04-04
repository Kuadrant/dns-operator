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
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	dnsv1 "google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsgoogle "github.com/kuadrant/dns-operator/internal/external-dns/provider/google"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	GoogleBatchChangeSize     = 1000
	GoogleBatchChangeInterval = time.Second
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

type resourceRecordSetsService struct {
	service *dnsv1.ResourceRecordSetsService
}

func (r resourceRecordSetsService) List(project string, managedZone string) resourceRecordSetsListCallInterface {
	return r.service.List(project, managedZone)
}

type GoogleDNSProvider struct {
	*externaldnsgoogle.GoogleProvider
	googleConfig externaldnsgoogle.GoogleConfig
	logger       logr.Logger
	// A client for managing resource record sets
	resourceRecordSetsClient resourceRecordSetsClientInterface
	// A client for managing hosted zones
	managedZonesClient managedZonesServiceInterface
}

var p provider.Provider = &GoogleDNSProvider{}

func (p *GoogleDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderGCP
}

func NewProviderFromSecret(ctx context.Context, s *corev1.Secret, c provider.Config) (provider.Provider, error) {
	if string(s.Data[v1alpha1.GoogleJsonKey]) == "" || string(s.Data[v1alpha1.GoogleProjectIDKey]) == "" {
		return nil, fmt.Errorf("GCP Provider credentials is empty")
	}

	creds, err := google.CredentialsFromJSON(ctx, s.Data[v1alpha1.GoogleJsonKey], dnsv1.NdevClouddnsReadwriteScope)
	if err != nil {
		return nil, err
	}

	httpClient := metrics.NewInstrumentedClient(provider.DNSProviderGCP.String(), oauth2.NewClient(ctx, creds.TokenSource))

	dnsClient, err := dnsv1.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}

	project := string(s.Data[v1alpha1.GoogleProjectIDKey])

	googleConfig := externaldnsgoogle.GoogleConfig{
		Project:             project,
		DomainFilter:        c.DomainFilter,
		ZoneIDFilter:        c.ZoneIDFilter,
		ZoneTypeFilter:      c.ZoneTypeFilter,
		BatchChangeSize:     GoogleBatchChangeSize,
		BatchChangeInterval: GoogleBatchChangeInterval,
		DryRun:              false,
	}

	logger := log.FromContext(ctx).WithName("google-dns").WithValues("project", project)
	ctx = log.IntoContext(ctx, logger)

	googleProvider, err := externaldnsgoogle.NewGoogleProviderWithService(ctx, googleConfig, dnsClient)
	if err != nil {
		return nil, fmt.Errorf("unable to create google provider: %s", err)
	}

	p := &GoogleDNSProvider{
		GoogleProvider:           googleProvider,
		googleConfig:             googleConfig,
		logger:                   logger,
		resourceRecordSetsClient: resourceRecordSetsService{dnsClient.ResourceRecordSets},
		managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
	}

	return p, nil
}

// #### External DNS Provider ####

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
		if err := p.resourceRecordSetsClient.List(p.googleConfig.Project, z.Name).Pages(ctx, f); err != nil {
			return nil, err
		}
	}

	return endpointsFromResourceRecordSets(records), nil
}

// AdjustEndpoints takes source endpoints and translates them to a google specific format
func (p *GoogleDNSProvider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	return endpointsToGoogleFormat(endpoints), nil
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

// #### DNS Operator Provider ####

func (p *GoogleDNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	for _, z := range zones {
		hz := provider.DNSZone{
			ID:      z.Name,
			DNSName: strings.ToLower(strings.TrimSuffix(z.DnsName, ".")),
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

func (p *GoogleDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FindDNSZoneForHost(ctx, host, zones)
}

func (p *GoogleDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider(p.Name().String(), NewProviderFromSecret, true)
}
