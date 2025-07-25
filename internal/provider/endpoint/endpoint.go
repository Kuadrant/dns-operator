/*
Copyright 2017 The Kubernetes Authors.

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

package endpoint

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/internal/provider"
)

// EndpointProvider - dns provider only used for testing purposes
// initialized as dns provider with no records
type EndpointProvider struct {
	config           provider.Config
	object           client.Object
	logger           logr.Logger
	NamespacedClient dynamic.ResourceInterface
	labelSelector    *metav1.LabelSelector
}

var p provider.Provider = &EndpointProvider{}

// DNSZoneForHost return the first authoritative DNSRecord with the same DNSRecord.spec.rootHost
func (p *EndpointProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FindDNSZoneForHost(ctx, host, zones, false)
}

// ApplyChanges implements provider.Provider.
func (p *EndpointProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	zoneAccessor, err := p.getZoneAccessorForZoneIDFilter(ctx)
	if err != nil {
		return err
	}
	me := &multierror.Error{}

	for _, newEndpoint := range changes.Create {
		err = zoneAccessor.EnsureEndpoint(newEndpoint)
		if err != nil {
			me = multierror.Append(me, err)
		}
	}
	for _, updateEndpoint := range changes.UpdateNew {
		err = zoneAccessor.EnsureEndpoint(updateEndpoint)
		if err != nil {
			me = multierror.Append(me, err)
		}
	}
	for _, deleteEndpoint := range changes.Delete {
		err = zoneAccessor.RemoveEndpoint(deleteEndpoint)
		if err != nil {
			me = multierror.Append(me, err)
		}
	}
	_, err = p.NamespacedClient.Update(ctx, zoneAccessor.GetObject(), metav1.UpdateOptions{})
	if err != nil {
		me = multierror.Append(me, err)
	}
	return me.ErrorOrNil()
}

// Records returns the list of endpoints
func (p *EndpointProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zoneAccessor, err := p.getZoneAccessorForZoneIDFilter(ctx)
	if err != nil {
		return nil, err
	}

	return zoneAccessor.GetEndpoints(), nil
}

func (p *EndpointProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone
	p.logger.Info("label selector", "string", labels.Set(p.labelSelector.MatchLabels).String())
	zones, err := p.NamespacedClient.List(
		ctx,
		metav1.ListOptions{LabelSelector: labels.Set(p.labelSelector.MatchLabels).String()},
	)
	if err != nil {
		return nil, err
	}

	for _, z := range zones.Items {
		za, err := NewEndpointAccessor(&z)
		if err != nil {
			p.logger.Info("badly formatted zone", "zone name", z.GetName())
			continue
		}
		hz := provider.DNSZone{
			ID:      z.GetName(),
			DNSName: za.GetRootHost(),
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

func (p *EndpointProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *EndpointProvider) getZoneAccessorForZoneIDFilter(ctx context.Context) (*endpointAccessor, error) {
	if !p.config.ZoneIDFilter.IsConfigured() {
		return nil, fmt.Errorf("no zone id filter specified for Endpoint Provider")
	}

	p.logger.Info("getting zone accessor for zone id filter", "filter", p.config.ZoneIDFilter, "object", p.object)

	zone, err := p.NamespacedClient.Get(ctx, p.config.ZoneIDFilter.ZoneIDs[0], metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return NewEndpointAccessor(zone)
}

// GetDomainFilter implements provider.Provider.
// Subtle: this method shadows the method (BaseProvider).GetDomainFilter of EndpointProvider.BaseProvider.
func (p *EndpointProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

// Name implements provider.Provider.
func (p *EndpointProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderEndpoint
}

// AdjustEndpoints nothing to do here
func (p *EndpointProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func NewProviderFromSecret(ctx context.Context, client dynamic.Interface, s *v1.Secret, providerConfig provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName("endpoint-dns")

	if s == nil {
		return nil, fmt.Errorf("provider secret cannot be nil")
	}

	if s.GetNamespace() == "" {
		return nil, fmt.Errorf("namespace not set on provider secret")
	}

	var gvr schema.GroupVersionResource
	var err error

	var gvrStr string
	if gvrStr = string(s.Data[v1alpha1.EndpointGVRKey]); gvrStr == "" {
		gvrStr = v1alpha1.DefaultEndpointGVR
	}
	logger.Info("got GVR string", "GVR", gvrStr)
	gvr, err = common.ParseGVRString(gvrStr)
	if err != nil {
		return nil, err
	}
	var labelSelectorString string
	if labelSelectorString = string(s.Data[v1alpha1.EndpointLabelSelectorKey]); labelSelectorString == "" {
		labelSelectorString = v1alpha1.DefaultLabelSelector
	}

	labelSelector, err := metav1.ParseToLabelSelector(labelSelectorString)
	if err != nil {
		return nil, err
	}
	logger.Info("got label selector", "labelSelector", labelSelector)
	namespacedClient := client.Resource(gvr).Namespace(s.GetNamespace())

	endpointProvider := &EndpointProvider{
		logger:           logger,
		config:           providerConfig,
		NamespacedClient: namespacedClient,
		labelSelector:    labelSelector,
		object:           s,
	}

	return endpointProvider, nil
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProviderWithClient(p.Name().String(), NewProviderFromSecret, true)
}
