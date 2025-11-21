package common

import (
	"context"

	"github.com/pkg/errors"

	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/endpoint"
)

func GetProviderFactory() (provider.Factory, error) {
	k8sClient, err := GetK8SClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get provider factory; failed to get k8s client")
	}

	dynClient, err := GetDynamicClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get provider factory; failed to get dynamic client")
	}

	return provider.NewFactory(k8sClient, dynClient, provider.RegisteredDefaultProviders(), endpoint.NewAuthoritativeDNSRecordProvider)
}

func GetProviderForConfig(ctx context.Context, secretRef *ResourceRef, providerConfig provider.Config) (provider.Provider, error) {
	providerFactory, err := GetProviderFactory()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get provider factory")
	}

	secret, err := GetProviderSecret(ctx, secretRef)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get provider secret")
	}

	return providerFactory.ProviderForSecret(ctx, secret, providerConfig)
}
