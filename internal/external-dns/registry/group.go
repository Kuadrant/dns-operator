package registry

import (
	"context"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/registry"

	"github.com/kuadrant/dns-operator/types"
)

var _ registry.Registry = &GroupRegistry{}

// GroupRegistry wraps a registry implementation to provide group functionality.
type GroupRegistry struct {
	Registry registry.Registry
	Group    types.Group
}

func (g GroupRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return g.Registry.Records(ctx)
}

func (g GroupRegistry) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	return g.Registry.ApplyChanges(ctx, changes)
}

func (g GroupRegistry) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	eps, err := g.Registry.AdjustEndpoints(endpoints)
	if err != nil {
		return eps, err
	}

	for _, ep := range eps {
		if g.Group.IsSet() {
			if ep.Labels == nil {
				ep.Labels = endpoint.NewLabels()
			}
			ep.Labels[types.GroupLabelKey] = g.Group.String()
			ep.Labels[types.TargetsLabelKey] = ep.Targets.String()
		} else {
			delete(ep.Labels, types.GroupLabelKey)
			delete(ep.Labels, types.TargetsLabelKey)
		}
	}

	return eps, nil
}

func (g GroupRegistry) GetDomainFilter() endpoint.DomainFilter {
	return g.Registry.GetDomainFilter()
}

func (g GroupRegistry) OwnerID() string {
	return g.Registry.OwnerID()
}
