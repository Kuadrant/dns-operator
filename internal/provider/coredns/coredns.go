// Core DNS provider is responsible for calling out to core dns instances via DNS for hosts it knows about to pull together the full set of "merged endpoints". This set of merged endpoints reprent the entire record set for a given host.

package coredns

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type CoreDNSProvider struct {
	logger         logr.Logger
	availableZones []string
	hostFilter     endpoint.DomainFilter
	domainFilter   endpoint.DomainFilter
}

var p provider.Provider = &CoreDNSProvider{}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider(p.Name().String(), NewCoreDNSProviderFromSecret, true)
}

func NewCoreDNSProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName(p.Name().String())

	if string(s.Data[v1alpha1.CoreDNSZonesKey]) == "" {
		return nil, fmt.Errorf("CoreDNS Provider credentials does not contain %s", v1alpha1.CoreDNSZonesKey)
	}

	p := &CoreDNSProvider{
		logger:       logger,
		hostFilter:   c.HostDomainFilter,
		domainFilter: c.DomainFilter,
	}
	if _, ok := s.Data[v1alpha1.CoreDNSZonesKey]; ok {
		p.availableZones = strings.Split(strings.TrimSpace(string(s.Data[v1alpha1.CoreDNSZonesKey])), ",")
	}
	return p, nil
}

func (p *CoreDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderCoreDNS
}

func (p *CoreDNSProvider) Labels() map[string]string {
	labels := map[string]string{
		provider.DNSProviderLabel: p.Name().String(),
	}
	if p.domainFilter.IsConfigured() {
		labels[provider.CoreDNSRecordZoneLabel] = p.domainFilter.Filters[0]
	}
	return labels
}

// DNSZones returns a list of dns zones accessible for this provider
func (p *CoreDNSProvider) DNSZones(_ context.Context) ([]provider.DNSZone, error) {
	zones := []provider.DNSZone{}
	for _, zone := range p.availableZones {
		zones = append(zones, provider.DNSZone{
			ID:      zone,
			DNSName: zone,
		})
	}
	return zones, nil
}

// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
func (p *CoreDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	// CoreDNS supports apex domains (denyApex=false), similar to EndpointProvider
	return provider.FindDNSZoneForHost(ctx, host, zones, false)
}

func (p *CoreDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *CoreDNSProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}

func (p *CoreDNSProvider) ApplyChanges(_ context.Context, changes *plan.Changes) error {
	// We don't need to apply changes since the record itself is the source of truth for CoreDNS.
	// We just run validations here to ensure any error cases can be reported into the record status.
	// ToDo What should happen if record is invalid? Currently it will still get published(labelled) and picked up by the coredns plugin!!
	return validateEndpoints(append(changes.Create, changes.UpdateNew...))
}

func (p *CoreDNSProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (p *CoreDNSProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

// validateEndpoints runs validations on the given endpoints to ensure they conform to the CoreDNS provider API
func validateEndpoints(endpoints []*endpoint.Endpoint) error {
	geoContinentPrefix := "GEO-"
	for _, ep := range endpoints {
		for _, ps := range ep.ProviderSpecific {
			if ps.Name == "geo-code" {
				if strings.HasPrefix(ps.Value, geoContinentPrefix) {
					continent := strings.Replace(ps.Value, geoContinentPrefix, "", -1)
					if !provider.IsContinentCode(continent) {
						return fmt.Errorf("unexpected continent code. %s", continent)
					}
				} else if !provider.IsISO3166Alpha2Code(ps.Value) && ps.Value != "*" {
					return fmt.Errorf("unexpected geo code. Prefix with %s for continents or use ISO_3166 Alpha 2 supported code for countries", geoContinentPrefix)
				}
			} else if ps.Name == "weight" {
				// we need a weight >=0 if we can't parse unsigned int then fail
				if _, err := strconv.ParseUint(ps.Value, 10, 64); err != nil {
					return fmt.Errorf("invalid weight expected a value >= 0")
				}
			}
		}
	}
	return nil
}
