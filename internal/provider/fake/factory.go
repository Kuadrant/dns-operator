package fake

import (
	"context"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type Factory struct {
	ProviderForFunc func(ctx context.Context, pa v1alpha1.ProviderAccessor) (provider.Provider, error)
}

var _ provider.Factory = &Factory{}

func (f *Factory) ProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor) (provider.Provider, error) {
	return f.ProviderForFunc(ctx, pa)
}
