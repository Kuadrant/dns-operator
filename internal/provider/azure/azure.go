package azure

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	dns "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/go-logr/logr"
	multierr "github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsproviderazure "github.com/kuadrant/dns-operator/internal/external-dns/provider/azure"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type AzureProvider struct {
	*externaldnsproviderazure.AzureProvider
	azureConfig externaldnsproviderazure.Config
	logger      logr.Logger
}

var p provider.Provider = &AzureProvider{}

func (*AzureProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderAzure
}

func (*AzureProvider) RecordsForHost(ctx context.Context, host string) ([]*externaldnsendpoint.Endpoint, error) {
	return []*externaldnsendpoint.Endpoint{}, fmt.Errorf("not impl")
}

func NewAzureProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	if string(s.Data[v1alpha1.AzureJsonKey]) == "" {
		return nil, fmt.Errorf("the Azure provider credentials is empty")
	}

	configString := string(s.Data[v1alpha1.AzureJsonKey])
	var azureConfig externaldnsproviderazure.Config
	err := yaml.Unmarshal([]byte(configString), &azureConfig)
	if err != nil {
		return nil, err
	}

	logger := crlog.FromContext(ctx).
		WithName("azure-dns").
		WithValues("tenantId", azureConfig.TenantID, "resourceGroup", azureConfig.ResourceGroup)
	ctx = crlog.IntoContext(ctx, logger)

	azureConfig.DomainFilter = c.DomainFilter
	azureConfig.ZoneNameFilter = c.DomainFilter
	azureConfig.IDFilter = c.ZoneIDFilter
	azureConfig.DryRun = false

	azureConfig.Transporter = metrics.NewInstrumentedClient(provider.DNSProviderAzure.String(), nil)

	azureProvider, err := externaldnsproviderazure.NewAzureProviderFromConfig(ctx, azureConfig)

	if err != nil {
		return nil, fmt.Errorf("unable to create azure provider: %s", err)
	}

	p := &AzureProvider{
		AzureProvider: azureProvider,
		azureConfig:   azureConfig,
		logger:        logger,
	}

	return p, nil

}

func (p *AzureProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	for _, z := range zones {
		hz := provider.DNSZone{
			ID:      *z.ID,
			DNSName: *z.Name,
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

func (p *AzureProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FindDNSZoneForHost(ctx, host, zones)
}

func (p *AzureProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider(p.Name().String(), NewAzureProviderFromSecret, true)
}

// Records gets the current records.
//
// Returns the current records or an error if the operation failed.
func (p *AzureProvider) Records(ctx context.Context) (endpoints []*externaldnsendpoint.Endpoint, _ error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		testForTrafficManagerProfile := []dns.RecordSet{}
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
				name := externaldnsproviderazure.FormatAzureDNSName(*recordSet.Name, *zone.Name)
				if len(p.ZoneNameFilter.Filters) > 0 && !p.DomainFilter.Match(name) {
					p.logger.V(1).Info("skipping return of record because it was filtered out by the specified --domain-filter", "record name", name)
					continue
				}
				targets := externaldnsproviderazure.ExtractAzureTargets(recordSet)
				if len(targets) == 0 {
					testForTrafficManagerProfile = append(testForTrafficManagerProfile, *recordSet)
					p.logger.V(1).Info("failed to extract targets from record set", "record name", name, "record type", recordType)
					continue
				}
				var ttl externaldnsendpoint.TTL
				if recordSet.Properties.TTL != nil {
					ttl = externaldnsendpoint.TTL(*recordSet.Properties.TTL)
				}
				ep := externaldnsendpoint.NewEndpointWithTTL(name, recordType, ttl, targets...)

				p.logger.V(1).Info("found record set", "record type", ep.RecordType, "DNS Name", ep.DNSName, "targets", ep.Targets)
				endpoints = append(endpoints, ep)
			}
		}
		for _, recordSet := range testForTrafficManagerProfile {
			tmEndpoint, err := p.endpointFromTrafficManager(ctx, &recordSet)
			if err != nil {
				p.logger.Error(err, "error extracting traffic manager profile for recordset", "recordset", recordSet)
				continue
			}
			if tmEndpoint != nil {
				endpoints = append(endpoints, tmEndpoint)
			}
		}
	}

	return endpoints, nil
}

func (p *AzureProvider) endpointFromTrafficManager(ctx context.Context, recordSet *dns.RecordSet) (endpoint *externaldnsendpoint.Endpoint, err error) {
	if recordSet.Properties.TargetResource == nil || recordSet.Properties.TargetResource.ID == nil {
		return nil, nil
	}
	profileNameParts := strings.Split(*recordSet.Properties.TargetResource.ID, "/")
	profileName := profileNameParts[len(profileNameParts)-1]
	profile, err := p.TrafficManagerProfilesClient.Get(ctx, p.ResourceGroup, profileName, nil)
	if err != nil {
		return nil, err
	}

	recordType := strings.Split(*recordSet.Type, "/")

	ep := externaldnsendpoint.NewEndpointWithTTL(*recordSet.Properties.Fqdn, recordType[len(recordType)-1], externaldnsendpoint.TTL(*recordSet.Properties.TTL), []string{}...)
	ep.WithProviderSpecific("routingpolicy", string(ptr.Deref(profile.Properties.TrafficRoutingMethod, "")))
	for _, e := range profile.Properties.Endpoints {
		ep.Targets = append(ep.Targets, ptr.Deref(e.Properties.Target, ""))
		if string(*profile.Properties.TrafficRoutingMethod) == "Geographic" {
			ep.WithProviderSpecific(*e.Properties.Target, *e.Properties.GeoMapping[0])
		}
		if string(*profile.Properties.TrafficRoutingMethod) == "Weighted" {
			ep.WithProviderSpecific(*e.Properties.Target, fmt.Sprint(*e.Properties.Weight))
		}
	}
	return ep, nil
}

// AdjustEndpoints takes source endpoints and translates them to an azure specific format
func (p *AzureProvider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	return endpointsToAzureFormat(endpoints), nil
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

	changeErrs := &multierr.Error{}
	changeWG := &sync.WaitGroup{}

	p.logger.Info("applying changes", "deleted", len(deleted), "updated", len(updated))
	p.DeleteRecords(ctx, deleted, changeWG, changeErrs)
	p.UpdateRecords(ctx, updated, changeWG, changeErrs)

	p.logger.Info("waiting for change group")
	changeWG.Wait()
	p.logger.Info("change group completed", "error", changeErrs.ErrorOrNil())
	return changeErrs.ErrorOrNil()
}

func (p *AzureProvider) DeleteRecords(ctx context.Context, deleted externaldnsproviderazure.AzureChangeMap, changeWG *sync.WaitGroup, changeErrs *multierr.Error) {
	for zone, endpoints := range deleted {
		for _, ep := range endpoints {
			if _, ok := ep.GetProviderSpecificProperty("routingpolicy"); ok && ep.RecordType != "TXT" {

				profileName := GenerateProfileName(p.ResourceGroup, ep)
				if p.DryRun {
					p.logger.Info("would delete traffic manager profile", "name", profileName)
					continue
				}

				changeWG.Add(1)
				go func() {
					defer changeWG.Done()
					p.logger.Info("deleting endpoint with routingpolicy", "profile name", profileName)
					_, err := p.TrafficManagerProfilesClient.Delete(ctx, p.ResourceGroup, profileName, nil)
					if err != nil {
						changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
					}
				}()

				name := p.RecordSetNameForZone(zone, ep)
				changeWG.Add(1)
				go func(z string) {
					defer changeWG.Done()
					p.logger.Info("deleting record that used traffic manager profile", "name", name, "profile name", profileName)
					_, err := p.RecordSetsClient.Delete(ctx, p.ResourceGroup, z, name, dns.RecordTypeCNAME, nil)
					if err != nil {
						changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
					}
				}(zone)

			} else {
				name := p.RecordSetNameForZone(zone, ep)
				if !p.DomainFilter.Match(ep.DNSName) {
					p.logger.V(1).Info("skipping deletion of record as it was filtered out by the specified --domain-filter", "record name", ep.DNSName)
					continue
				}
				if p.DryRun {
					p.logger.Info("would delete record", "record type", ep.RecordType, "record name", name, "zone", zone)
				} else {
					changeWG.Add(1)
					go func(z string, e *externaldnsendpoint.Endpoint) {
						defer changeWG.Done()
						p.logger.Info("deleting record", "record type", e.RecordType, "record name", name, "zone", z)
						_, err := p.RecordSetsClient.Delete(ctx, p.ResourceGroup, z, name, dns.RecordType(e.RecordType), nil)
						if err != nil {
							changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
						}
					}(zone, ep)
				}
			}
		}
	}
}

func (p *AzureProvider) UpdateRecords(ctx context.Context, updated externaldnsproviderazure.AzureChangeMap, changeWG *sync.WaitGroup, changeErrs *multierr.Error) {
	for zone, endpoints := range updated {
		for _, ep := range endpoints {
			if !p.DomainFilter.Match(ep.DNSName) {
				p.logger.V(1).Info("skipping update of record because it was filtered by the specified --domain-filter", "record name", ep.DNSName)
				continue
			}
			if policy, ok := ep.GetProviderSpecificProperty("routingpolicy"); ok && ep.RecordType != "TXT" {
				p.logger.Info("got endpoint with routing policy", "routing policy", policy)
				profileName := GenerateProfileName(p.ResourceGroup, ep)

				p.logger.Info("updating endpoint with routingpolicy", "endpoint", ep)
				tmEndpoints := []*armtrafficmanager.Endpoint{}
				for _, target := range ep.Targets {
					if policy == "Geographic" {
						geo, ok := ep.GetProviderSpecificProperty(target)
						if !ok {
							p.logger.Error(fmt.Errorf("could not find geo string for target: '%s'", target), "no geo property set", "endpoint", ep)
							changeErrs = multierr.Append(changeErrs, fmt.Errorf("could not find geo string for target: '%s'", target))
						}
						tmEndpoint := armtrafficmanager.Endpoint{
							Type: ptr.To("Microsoft.Network/trafficManagerProfiles/externalEndpoints"),
							Name: ptr.To(strings.ReplaceAll(target, ".", "-")),
							Properties: &armtrafficmanager.EndpointProperties{
								GeoMapping:  []*string{ptr.To(geo)},
								Target:      ptr.To(target),
								AlwaysServe: ptr.To(armtrafficmanager.AlwaysServeEnabled),
							},
						}
						tmEndpoints = append(tmEndpoints, &tmEndpoint)
					} else if policy == "Weighted" {
						strWeight, ok := ep.GetProviderSpecificProperty(target)
						if !ok {
							p.logger.Error(fmt.Errorf("could not find weight string for target: '%s'", target), "no weight property set", "endpoint", ep)
							changeErrs = multierr.Append(changeErrs, fmt.Errorf("could not find weight string for target: '%s'", target))
						}

						weight, err := strconv.Atoi(strWeight)
						if err != nil {
							p.logger.Error(err, "could not convert weight value to int", "weight value", strWeight)
							changeErrs = multierr.Append(changeErrs, err)
						}
						tmEndpoint := armtrafficmanager.Endpoint{
							Type: ptr.To("Microsoft.Network/trafficManagerProfiles/externalEndpoints"),
							Name: ptr.To(strings.ReplaceAll(target, ".", "-")),
							Properties: &armtrafficmanager.EndpointProperties{
								Weight:      ptr.To(int64(weight)),
								Target:      ptr.To(target),
								AlwaysServe: ptr.To(armtrafficmanager.AlwaysServeEnabled),
							},
						}
						tmEndpoints = append(tmEndpoints, &tmEndpoint)
					}
				}
				var ttl int64 = 60
				var port int64 = 80
				profile := armtrafficmanager.Profile{
					Location: ptr.To("global"),
					Properties: &armtrafficmanager.ProfileProperties{
						TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethod(policy)),
						Endpoints:            tmEndpoints,
						DNSConfig: &armtrafficmanager.DNSConfig{
							RelativeName: &profileName,
							TTL:          &ttl,
						},
						MonitorConfig: &armtrafficmanager.MonitorConfig{
							Path:     ptr.To("/"),
							Port:     &port,
							Protocol: ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						},
					},
				}
				if p.DryRun {
					p.logger.Info("would update traffic manager profile", "name", profileName, "profile", profile)
					continue
				}
				changeWG.Add(1)
				go func(z string, e *externaldnsendpoint.Endpoint) {
					defer changeWG.Done()

					p.logger.Info("updating traffic manager profile", "name", profileName, "profile", profile)
					tmResp, err := p.TrafficManagerProfilesClient.CreateOrUpdate(ctx, p.ResourceGroup, profileName, profile, nil)

					if err != nil {
						p.logger.Error(externaldnsproviderazure.CleanAzureError(err), "error updating traffic manager", "name", profileName, "profile", profile)
						changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
						return
					}

					name := p.RecordSetNameForZone(z, e)
					var epTTL int64 = int64(e.RecordTTL)
					p.logger.Info("updating record to use traffic manager profile", "name", name, "profile ID", tmResp.ID)
					_, err = p.RecordSetsClient.CreateOrUpdate(
						ctx,
						p.ResourceGroup,
						z,
						name,
						dns.RecordTypeCNAME,
						dns.RecordSet{
							Properties: &dns.RecordSetProperties{
								TTL: &epTTL,
								TargetResource: &dns.SubResource{
									ID: tmResp.ID,
								},
							},
						},
						nil,
					)
					if err != nil {
						changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
					}
				}(zone, ep)
			} else {
				name := p.RecordSetNameForZone(zone, ep)
				if p.DryRun {
					p.logger.Info("would update record", "record type", ep.RecordType, "record name", name, "targets", ep.Targets, "zone", zone)
					continue
				}

				changeWG.Add(1)
				go func(z string, e *externaldnsendpoint.Endpoint) {
					defer changeWG.Done()
					recordSet, err := p.NewRecordSet(e)
					if err != nil {
						changeErrs = multierr.Append(changeErrs, err)
						return
					}
					p.logger.Info("updating record", "record type", e.RecordType, "record name", name, "targets", e.Targets, "zone", z)
					_, err = p.RecordSetsClient.CreateOrUpdate(
						ctx,
						p.ResourceGroup,
						z,
						name,
						dns.RecordType(e.RecordType),
						recordSet,
						nil,
					)
					if err != nil {
						changeErrs = multierr.Append(changeErrs, externaldnsproviderazure.CleanAzureError(err))
					}
				}(zone, ep)
			}
		}
	}
}

// endpointsToProviderFormat converts a list of endpoints into an azure specific format.
func endpointsToAzureFormat(eps []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	endpointMap := make(map[string][]*externaldnsendpoint.Endpoint)
	for i := range eps {
		eps[i].SetIdentifier = ""
		endpointMap[eps[i].DNSName] = append(endpointMap[eps[i].DNSName], eps[i])
	}

	var translatedEndpoints []*externaldnsendpoint.Endpoint

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
			translatedEndpoint.WithProviderSpecific("routingpolicy", "Geographic")
		} else if isWeighted {
			translatedEndpoint.WithProviderSpecific("routingpolicy", "Weighted")
		}

		defaultTarget := FindDefaultGeoTarget(endpoints)
		//ToDo this has the potential to add duplicates
		for _, ep := range endpoints {
			for _, t := range ep.Targets {
				if isGeo {
					geo, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
					if t == defaultTarget && geo != "*" {
						continue
					}
					if geo == "*" {
						geo = "WORLD"
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

func FindDefaultGeoTarget(endpoints []*externaldnsendpoint.Endpoint) string {
	for _, ep := range endpoints {
		geo, _ := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
		if geo == "*" || geo == "WORLD" {
			// todo: can there ever be more than one?
			return ep.Targets[0]
		}
	}
	return ""
}

func GenerateProfileName(resourceGroup string, ep *externaldnsendpoint.Endpoint) string {
	data := []byte(ep.DNSName)
	hash := fmt.Sprintf("%x", md5.Sum(data))
	return resourceGroup + "-" + hash[0:16]
}
