package crd

import (
	"context"
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	defaultZoneRecordLabel = "kuadrant.io/crd-provider-zone-record"
)

var (
	scheme = runtime.NewScheme()
)

type Provider struct {
	client          client.Client
	config          provider.Config
	object          client.Object
	zoneRecordLabel string
}

func NewProviderFromSecret(_ context.Context, c client.Client, s *v1.Secret, config provider.Config) (provider.Provider, error) {
	if s == nil {
		return nil, fmt.Errorf("secret cannot be nil")
	}

	if s.GetNamespace() == "" {
		return nil, fmt.Errorf("namespace not set")
	}

	zoneRecordSelectorLabel := defaultZoneRecordLabel
	if val, ok := s.Data[v1alpha1.CRDZoneRecordLabelKey]; ok && string(val) != "" {
		zoneRecordSelectorLabel = string(val)
	}

	return &Provider{
		client:          client.NewNamespacedClient(c, s.GetNamespace()),
		config:          config,
		object:          s,
		zoneRecordLabel: zoneRecordSelectorLabel,
	}, nil
}

var p provider.Provider = &Provider{}

// Records get authoritative endpoints, endpoints of the merged, authoritative, DNSrecord (ones labeled as such?)
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	aRecord, err := p.getRecordForZoneIDFilter(ctx)
	if err != nil {
		return nil, err
	}
	return aRecord.Spec.Endpoints, nil
}

// ApplyChanges process the changes and add/update/delete endpoints from the authoritative DNSRecord
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	aRecord, err := p.getRecordForZoneIDFilter(ctx)
	if err != nil {
		return err
	}

	//Apply all changes to the authoritative records endpoints
	endpoints := aRecord.Spec.Endpoints
	for _, newEp := range changes.Create {
		endpoints = append(endpoints, newEp)
	}

	for _, newEp := range changes.UpdateNew {
		for idx, e := range endpoints {
			if e.Key() == newEp.Key() {
				endpoints[idx] = newEp
				break
			}
		}
	}

	for _, deleteEp := range changes.Delete {
		for idx, e := range endpoints {
			if e.Key() == deleteEp.Key() {
				endpoints = slices.Delete(endpoints, idx, idx+1)
				break
			}
		}
	}

	aRecord.Spec.Endpoints = endpoints

	return p.client.Update(ctx, aRecord)
}

// AdjustEndpoints nothing to do here
func (p *Provider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (p *Provider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

// DNSZones return all authoritative DNSRecords, transformed into DNSZone resources
// DNSZone.ID = DNSRecord.metadata.name
// DNSZone.DNSName = DNSRecord.spec.rootHost
func (p *Provider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone

	aRecords := &v1alpha1.DNSRecordList{}
	req, err := labels.NewRequirement(p.zoneRecordLabel, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}
	labelSelector := labels.NewSelector().Add(*req)
	if err := p.client.List(ctx, aRecords, &client.ListOptions{Namespace: p.object.GetNamespace(), LabelSelector: labelSelector}); err != nil {
		return nil, err
	}

	for _, zr := range aRecords.Items {
		hz := provider.DNSZone{
			//ToDo This should probably use a locator(namespace/name) format here
			ID:      zr.Name,
			DNSName: zr.Spec.RootHost,
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

// DNSZoneForHost return the first authoritative DNSRecord with the same DNSRecord.spec.rootHost
func (p *Provider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}

	for _, z := range zones {
		if z.DNSName == host {
			return &z, nil
		}
	}

	return nil, fmt.Errorf("no zone found for host: %s", host)
}

func (p *Provider) getRecordForZoneIDFilter(ctx context.Context) (*v1alpha1.DNSRecord, error) {
	if !p.config.ZoneIDFilter.IsConfigured() {
		return nil, fmt.Errorf("no zone id filter specified for CRD Provider")
	}
	aRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.config.ZoneIDFilter.ZoneIDs[0],
			Namespace: p.object.GetNamespace(),
		},
	}
	if err := p.client.Get(ctx, client.ObjectKeyFromObject(aRecord), aRecord); err != nil {
		return nil, err
	}

	return aRecord, nil
}

func (p *Provider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *Provider) Name() provider.DNSProviderName {
	return provider.DNSProviderCRD
}

// Register this Provider with the provider factory
func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	// This doesn't get registered with the factory
	provider.RegisterProviderWithClient(p.Name().String(), NewProviderFromSecret, true)
}
