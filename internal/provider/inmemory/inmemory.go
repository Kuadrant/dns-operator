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

package inmemory

import (
	"context"

	v1 "k8s.io/api/core/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/external-dns/provider/inmemory"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type InMemoryDNSProvider struct {
	*inmemory.InMemoryProvider
	ctx context.Context
}

var client *inmemory.InMemoryClient

var _ provider.Provider = &InMemoryDNSProvider{}

func NewProviderFromSecret(ctx context.Context, _ *v1.Secret, c provider.Config) (provider.Provider, error) {
	inmemoryProvider := inmemory.NewInMemoryProvider(inmemory.InMemoryWithClient(client), inmemory.InMemoryWithDomain(c.DomainFilter), inmemory.InMemoryWithLogging())
	p := &InMemoryDNSProvider{
		InMemoryProvider: inmemoryProvider,
		ctx:              ctx,
	}
	return p, nil
}

func (i InMemoryDNSProvider) EnsureManagedZone(mz *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var zoneID string
	if mz.Spec.ID != "" {
		zoneID = mz.Spec.ID
	} else {
		zoneID = mz.Status.ID
	}

	if zoneID != "" {
		_, err := i.GetZone(zoneID)
		return provider.ManagedZoneOutput{
			ID:          zoneID,
			NameServers: nil,
			RecordCount: 0,
		}, err
	}
	err := i.CreateZone(mz.Spec.DomainName)
	return provider.ManagedZoneOutput{
		ID:          mz.Spec.DomainName,
		NameServers: nil,
		RecordCount: 0,
	}, err
}

func (i InMemoryDNSProvider) DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error {
	return i.DeleteZone(managedZone.Spec.DomainName)
}

func (i InMemoryDNSProvider) HealthCheckReconciler() provider.HealthCheckReconciler {
	return &provider.FakeHealthCheckReconciler{}
}

func (i InMemoryDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

// Register this Provider with the provider factory
func init() {
	client = inmemory.NewInMemoryClient()
	provider.RegisterProvider("inmemory", NewProviderFromSecret)
}
