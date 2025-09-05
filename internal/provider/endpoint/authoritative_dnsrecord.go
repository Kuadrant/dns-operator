package endpoint

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/internal/provider"
)

var (
	errIncompatibleAccessorType = fmt.Errorf("authoritative DNSRecords can only be created for resources of the same type (DNSRecord)")
)

var _ provider.Provider = &AuthoritativeDNSRecordProvider{}

// AuthoritativeDNSRecordProvider adapts the endpoint provider behaviour for the management of authoritative DNSRecord resources for the delegation feature.
// This provider is intended to be used internally by the provider factory and is not exposed as a provider for user consumption.
//
// Calls to DNSZoneForHost are adapted to ensure the existence of the expected authoritative record before calling the normal endpoint impl.
type AuthoritativeDNSRecordProvider struct {
	*EndpointProvider
	client.Client
	v1alpha1.DNSRecord
}

func NewAuthoritativeDNSRecordProvider(ctx context.Context, c client.Client, dc dynamic.Interface, pAccessor v1alpha1.ProviderAccessor, pConfig provider.Config) (provider.Provider, error) {
	dnsRecord, ok := pAccessor.(*v1alpha1.DNSRecord)
	if !ok {
		return nil, errIncompatibleAccessorType
	}
	ep, err := newEndpointProviderFromSecret(ctx, dc, authoritativeRecordProviderSecret(dnsRecord), pConfig)
	if err != nil {
		return nil, err
	}

	adp := &AuthoritativeDNSRecordProvider{
		EndpointProvider: ep,
		Client:           c,
		DNSRecord:        *dnsRecord,
	}

	return adp, nil
}

// authoritativeRecordProviderSecret returns an "endpoint" provider secret configured for DNSRecord resources and a label selector for the given DNSRecord.
func authoritativeRecordProviderSecret(record *v1alpha1.DNSRecord) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint",
			Namespace: record.GetNamespace(),
		},
		Type: v1alpha1.SecretTypeKuadrantEndpoint,
		Data: map[string][]byte{
			v1alpha1.EndpointLabelSelectorKey: []byte(authoritativeRecordSelectorFor(*record)),
			v1alpha1.EndpointGVRKey:           []byte(v1alpha1.DefaultEndpointGVR),
		},
	}
}

// Provider Impl

func (p AuthoritativeDNSRecordProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	_, err := p.ensureAuthoritativeRecord(ctx, p.DNSRecord)
	if err != nil {
		return nil, err
	}
	return p.EndpointProvider.DNSZoneForHost(ctx, host)
}

func (p AuthoritativeDNSRecordProvider) ensureAuthoritativeRecord(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord, err := p.getAuthoritativeRecordFor(ctx, record)
	if err != nil {
		return nil, err
	}
	if aRecord != nil {
		return aRecord, nil
	}
	return p.createAuthoritativeRecordFor(ctx, record)
}

func (p AuthoritativeDNSRecordProvider) getAuthoritativeRecordFor(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecords := v1alpha1.DNSRecordList{}

	labelSelector, err := labels.Parse(authoritativeRecordSelectorFor(record))
	if err != nil {
		return nil, err
	}

	if err = p.Client.List(ctx, &aRecords, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return nil, fmt.Errorf("failed to get authoritative record: %w", err)
	}

	if len(aRecords.Items) > 0 {
		return &aRecords.Items[0], nil
	}

	return nil, nil
}

func (p AuthoritativeDNSRecordProvider) createAuthoritativeRecordFor(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord := authoritativeRecordFor(record)

	if err := p.Client.Create(ctx, aRecord, &client.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create authoritative record: %w", err)
	}

	return aRecord, nil
}

func authoritativeRecordFor(record v1alpha1.DNSRecord) *v1alpha1.DNSRecord {
	rootHostHash := common.HashRootHost(record.GetRootHost())
	pRef := record.GetProviderRef()
	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toAuthoritativeRecordName(rootHostHash),
			Namespace: record.GetNamespace(),
			Labels: map[string]string{
				v1alpha1.AuthoritativeRecordLabel:     "true",
				v1alpha1.AuthoritativeRecordHashLabel: rootHostHash,
			},
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost:    record.GetRootHost(),
			ProviderRef: &pRef,
		},
	}
}

func toAuthoritativeRecordName(rootHostHash string) string {
	return fmt.Sprintf("authoritative-record-%s", rootHostHash)
}

func authoritativeRecordSelectorFor(record v1alpha1.DNSRecord) string {
	return fmt.Sprintf("%s=true, %s=%s", v1alpha1.AuthoritativeRecordLabel, v1alpha1.AuthoritativeRecordHashLabel, common.HashRootHost(record.GetRootHost()))
}
