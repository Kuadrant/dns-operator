package endpoint

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

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
	v1alpha1.DNSRecord
}

func NewAuthoritativeDNSRecordProvider(ctx context.Context, dc dynamic.Interface, pAccessor v1alpha1.ProviderAccessor, pConfig provider.Config) (provider.Provider, error) {
	dnsRecord, ok := pAccessor.(*v1alpha1.DNSRecord)
	if !ok {
		return nil, errIncompatibleAccessorType
	}
	ep, err := newEndpointProviderFromSecret(ctx, dc, authoritativeRecordProviderSecretFor(dnsRecord), pConfig)
	if err != nil {
		return nil, err
	}

	adp := &AuthoritativeDNSRecordProvider{
		EndpointProvider: ep,
		DNSRecord:        *dnsRecord,
	}

	return adp, nil
}

// Provider Impl

func (p AuthoritativeDNSRecordProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	err := p.ensureAuthoritativeRecord(ctx)
	if err != nil {
		return nil, err
	}
	return p.EndpointProvider.DNSZoneForHost(ctx, host)
}

func (p AuthoritativeDNSRecordProvider) ensureAuthoritativeRecord(ctx context.Context) error {
	aRecord, err := p.getAuthoritativeRecord(ctx)
	if err != nil {
		return err
	}
	if aRecord != nil {
		return nil
	}
	return p.createAuthoritativeRecord(ctx)
}

func (p AuthoritativeDNSRecordProvider) getAuthoritativeRecord(ctx context.Context) (*v1alpha1.DNSRecord, error) {
	uList, err := p.NamespacedClient.List(ctx, metav1.ListOptions{LabelSelector: labels.Set(p.labelSelector.MatchLabels).String()})
	if err != nil {
		return nil, err
	}

	if len(uList.Items) > 0 {
		var aRecord = v1alpha1.DNSRecord{}
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(uList.Items[0].Object, &aRecord); err != nil {
			return nil, err
		}
		return &aRecord, nil
	}

	return nil, nil
}

func (p AuthoritativeDNSRecordProvider) createAuthoritativeRecord(ctx context.Context) error {
	aRecord := authoritativeRecordFor(p.DNSRecord)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(aRecord)
	if err != nil {
		return err
	}

	_, err = p.NamespacedClient.Create(ctx, &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func authoritativeRecordFor(record v1alpha1.DNSRecord) *v1alpha1.DNSRecord {
	rootHostHash := common.HashRootHost(record.GetRootHost())
	pRef := record.GetProviderRef()
	return &v1alpha1.DNSRecord{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DNSRecord",
			APIVersion: "kuadrant.io/v1alpha1",
		},
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

// authoritativeRecordProviderSecretFor returns an "endpoint" provider secret configured for DNSRecord resources and a label selector for the given DNSRecord.
func authoritativeRecordProviderSecretFor(record *v1alpha1.DNSRecord) *corev1.Secret {
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

func authoritativeRecordSelectorFor(record v1alpha1.DNSRecord) string {
	return fmt.Sprintf("%s=true, %s=%s", v1alpha1.AuthoritativeRecordLabel, v1alpha1.AuthoritativeRecordHashLabel, common.HashRootHost(record.GetRootHost()))
}
