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
	if string(s.Data["azure.json"]) == "" {
		return nil, fmt.Errorf("the Azure provider credentials is empty")
	}

	configString := string(s.Data["azure.json"])
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

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider("azure", NewAzureProviderFromSecret, false)
}

func (p *AzureProvider) HealthCheckReconciler() provider.HealthCheckReconciler {
	return NewAzureHealthCheckReconciler()
}

func (p *AzureProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *AzureProvider) EnsureManagedZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var zoneID string

	if managedZone.Spec.ID != "" {
		zoneID = managedZone.Spec.ID
	} else {
		zoneID = managedZone.Status.ID
	}

	if zoneID != "" {
		//Get existing managed zone
		return p.getManagedZone(ctx, zoneID)
	}
	//Create new managed zone
	return p.createManagedZone(ctx, managedZone)
}

// DeleteManagedZone not implemented as managed zones are going away
func (p *AzureProvider) DeleteManagedZone(_ *v1alpha1.ManagedZone) error {
	return nil // p.zonesClient.Delete(p.project, managedZone.Status.ID).Do()
}

func (p *AzureProvider) getManagedZone(ctx context.Context, zoneID string) (provider.ManagedZoneOutput, error) {
	logger := crlog.FromContext(ctx).WithName("getManagedZone")
	zones, err := p.Zones(ctx)
	if err != nil {
		return provider.ManagedZoneOutput{}, err
	}

	for _, zone := range zones {
		logger.Info("comparing zone IDs", "found zone ID", zone.ID, "wanted zone ID", zoneID)
		if *zone.ID == zoneID {
			logger.Info("found zone ID", "found zone ID", zoneID, "wanted zone ID", zoneID)
			return provider.ManagedZoneOutput{
				ID:          *zone.ID,
				DNSName:     *zone.Name,
				NameServers: zone.Properties.NameServers,
				RecordCount: *zone.Properties.NumberOfRecordSets,
			}, nil
		}
	}

	return provider.ManagedZoneOutput{}, fmt.Errorf("zone %s not found", zoneID)
}

// createManagedZone not implemented as managed zones are going away
func (p *AzureProvider) createManagedZone(_ context.Context, _ *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	return provider.ManagedZoneOutput{}, nil
}
