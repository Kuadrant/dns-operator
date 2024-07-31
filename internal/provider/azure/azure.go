package azure

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"

	v1 "k8s.io/api/core/v1"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsproviderazure "github.com/kuadrant/dns-operator/internal/external-dns/provider/azure"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type AzureProvider struct {
	*externaldnsproviderazure.AzureProvider
	azureConfig externaldnsproviderazure.Config
	logger      logr.Logger
}

var _ provider.Provider = &AzureProvider{}

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

func (p *AzureProvider) HealthCheckReconciler() provider.HealthCheckReconciler {
	return NewAzureHealthCheckReconciler()
}

func (p *AzureProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider("azure", NewAzureProviderFromSecret, false)
}
