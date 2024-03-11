package fake

import (
	"context"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsplan "sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type Provider struct {
	RecordsFunc           func(context.Context) ([]*externaldnsendpoint.Endpoint, error)
	ApplyChangesFunc      func(context.Context, *externaldnsplan.Changes) error
	AdjustEndpointsFunc   func([]*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error)
	GetDomainFilterFunc   func() externaldnsendpoint.DomainFilter
	EnsureManagedZoneFunc func(*v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error)
	DeleteManagedZoneFunc func(*v1alpha1.ManagedZone) error
}

var _ provider.Provider = &Provider{}

// #### External DNS Provider ####

func (p Provider) Records(ctx context.Context) ([]*externaldnsendpoint.Endpoint, error) {
	return p.RecordsFunc(ctx)
}

func (p Provider) ApplyChanges(ctx context.Context, changes *externaldnsplan.Changes) error {
	return p.ApplyChangesFunc(ctx, changes)
}

func (p Provider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	return p.AdjustEndpointsFunc(endpoints)
}

func (p Provider) GetDomainFilter() externaldnsendpoint.DomainFilter {
	return p.GetDomainFilterFunc()
}

// #### DNS Operator Provider ####

func (p Provider) EnsureManagedZone(managedZone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	return p.EnsureManagedZoneFunc(managedZone)
}

func (p Provider) DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error {
	return p.DeleteManagedZoneFunc(managedZone)
}
