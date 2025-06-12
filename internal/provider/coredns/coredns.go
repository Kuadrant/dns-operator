// Core DNS provider is responsible for calling out to core dns instances via DNS for hosts it knows about to pull together the full set of "merged endpoints". This set of merged endpoints reprent the entire record set for a given host.

package coredns

import (
	"context"
	"fmt"
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
	nameservers    []*string
	availableZones []string
	hostFilter     endpoint.DomainFilter
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
		logger:     logger,
		hostFilter: c.HostDomainFilter,
	}
	if _, ok := s.Data[v1alpha1.CoreDNSZonesKey]; ok {
		p.availableZones = strings.Split(strings.TrimSpace(string(s.Data[v1alpha1.CoreDNSZonesKey])), ",")
	}
	return p, nil
}

func (p *CoreDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderCoreDNS
}

// DNSZones returns a list of dns zones accessible for this provider
func (p *CoreDNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	zones := []provider.DNSZone{}
	for id, zone := range p.availableZones {
		zones = append(zones, provider.DNSZone{
			ID:          fmt.Sprintf("id-%d", id),
			DNSName:     zone,
			NameServers: p.nameservers,
		})
	}
	return zones, nil
}

// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
func (p *CoreDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	var assignedZone *provider.DNSZone
	zones, _ := p.DNSZones(ctx)
	for _, z := range zones {
		if strings.HasSuffix(host, z.DNSName) {
			if assignedZone == nil {
				assignedZone = &z
			}
			if len(assignedZone.DNSName) < len(z.DNSName) {
				assignedZone = &z
			}
		}
	}
	if assignedZone == nil {
		return nil, provider.ErrNoZoneForHost
	}
	return assignedZone, nil
}

func (p *CoreDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *CoreDNSProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}

func (p *CoreDNSProvider) ApplyChanges(_ context.Context, _ *plan.Changes) error {
	return nil
}

func (p *CoreDNSProvider) AdjustEndpoints(_ []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}
func (p *CoreDNSProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}
