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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/external-dns/provider/inmemory"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type InMemoryDNSProvider struct {
	*inmemory.InMemoryProvider
}

var client *inmemory.InMemoryClient

var p provider.Provider = &InMemoryDNSProvider{}

func (p *InMemoryDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderInMem
}

func (*InMemoryDNSProvider) RecordsForHost(ctx context.Context, host string) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, fmt.Errorf("not impl")
}

func NewProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName("inmemory-dns")
	ctx = log.IntoContext(ctx, logger)

	initZones := []string{}
	if z := string(s.Data[v1alpha1.InmemInitZonesKey]); z != "" {
		initZones = strings.Split(z, ",")
	}

	inmemoryProvider := inmemory.NewInMemoryProvider(
		ctx,
		inmemory.InMemoryWithClient(client),
		inmemory.InMemoryInitZones(initZones),
		inmemory.InMemoryWithDomain(c.DomainFilter),
		inmemory.InMemoryWithLogging())
	p := &InMemoryDNSProvider{
		InMemoryProvider: inmemoryProvider,
	}

	availableZones := []string{}
	z, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}

	for _, zone := range z {
		availableZones = append(availableZones, zone.DNSName)
	}
	logger.V(1).Info("provider initialised", "availableZones", availableZones)

	return p, nil
}

func (p *InMemoryDNSProvider) DNSZones(_ context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone
	zones := p.Zones()
	for id, name := range zones {
		hz := provider.DNSZone{
			ID:      id,
			DNSName: name,
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

func (p *InMemoryDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FindDNSZoneForHost(ctx, host, zones)
}

func (i *InMemoryDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

// Register this Provider with the provider factory
func init() {
	client = inmemory.NewInMemoryClient()
	provider.RegisterProvider(p.Name().String(), NewProviderFromSecret, false)
}
