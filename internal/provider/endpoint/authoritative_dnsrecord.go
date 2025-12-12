package endpoint

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

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
// DNSZoneForHost and ApplyChanges are adapted to ensure the existence of the expected authoritative record.
// ApplyChanges will remove the authoritative record for a deleting delegating DNSRecord if no endpoints exist in it after endpoint removal.
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

// DNSZoneForHost implements Provider.DNSZoneForHost.
// Ensures the existence of the authoritative record and returns the provider.DNSZone representation of it.
func (p AuthoritativeDNSRecordProvider) DNSZoneForHost(ctx context.Context, _ string) (*provider.DNSZone, error) {
	aRecord, err := p.ensureAuthoritativeRecord(ctx)
	if err != nil {
		return nil, err
	}

	zoneDomainName := aRecord.Status.ZoneDomainName
	if zoneDomainName == "" {
		return nil, fmt.Errorf("Authoritative zone does not yet have a zone domain name set")
	}

	zone := &provider.DNSZone{
		ID:      aRecord.GetName(),
		DNSName: zoneDomainName,
	}

	return zone, nil
}

// ApplyChanges implements Provider.ApplyChanges by delegating the request to the embedded endpoint provider.
// Ensures the existence of the authoritative record before applying changes.
// Removes the authoritative record if the delegating record is deleting and the result of applying changes produces
// an empty authoritative record i.e. no other delegating record is adding endpoints to it.
func (p AuthoritativeDNSRecordProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	_, err := p.ensureAuthoritativeRecord(ctx)
	if err != nil {
		return err
	}

	err = p.EndpointProvider.ApplyChanges(ctx, changes)
	if err != nil {
		return err
	}

	if p.DNSRecord.IsDeleting() {
		aRecord, err := p.getAuthoritativeRecord(ctx)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if len(aRecord.Spec.Endpoints) == 0 {
			if err = p.NamespacedClient.Delete(ctx, aRecord.GetName(), metav1.DeleteOptions{}); !apierrors.IsNotFound(err) {
				return err
			}
			p.logger.Info("deleted authoritative record", "name", aRecord.GetName())
		}
	}

	return nil
}

// Records implements Provider.Records by delegating the request to the embedded endpoint provider.
// Ignores any potential not found errors due to the authoritative record having been deleted returning an empty slice in this case.
func (p AuthoritativeDNSRecordProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	recs, err := p.EndpointProvider.Records(ctx)
	if apierrors.IsNotFound(err) {
		return []*endpoint.Endpoint{}, nil
	}
	return recs, err
}

// ensureAuthoritativeRecord ensures that a DNSRecord exists to act as the authoritative record for the DNSRecord requesting delegation.
// If no record exists that matches the name of the expected authoritative record one is created.
// If a record already exists with the expected authoritative record name it is used.
// Ensures that the labels expected of an authoritative record are in place, updating the record if required.
func (p AuthoritativeDNSRecordProvider) ensureAuthoritativeRecord(ctx context.Context) (*v1alpha1.DNSRecord, error) {
	aRecord, err := p.getAuthoritativeRecord(ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if aRecord == nil {
		aRecord, err = p.createAuthoritativeRecord(ctx)
		if err != nil {
			return nil, err
		}
		p.logger.Info("created authoritative record", "name", aRecord.GetName())
		return aRecord, nil
	}
	if common.MergeLabels(aRecord, authoritativeRecordFor(p.DNSRecord).GetLabels()) {
		var uRecord *unstructured.Unstructured
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(aRecord)
		if err != nil {
			return nil, err
		}

		uRecord, err = p.NamespacedClient.Update(ctx, &unstructured.Unstructured{Object: obj}, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}

		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(uRecord.Object, &aRecord); err != nil {
			return nil, err
		}
	}

	return aRecord, nil
}

// getAuthoritativeRecord retrieves the expected authoritative record for the current DNSRecord requesting delegation.
func (p AuthoritativeDNSRecordProvider) getAuthoritativeRecord(ctx context.Context) (*v1alpha1.DNSRecord, error) {
	var aRecord *v1alpha1.DNSRecord
	uRecord, err := p.NamespacedClient.Get(ctx, authoritativeRecordFor(p.DNSRecord).GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(uRecord.Object, &aRecord); err != nil {
		return nil, err
	}
	return aRecord, nil
}

// createAuthoritativeRecord creates the expected authoritative record for the current DNSRecord requesting delegation.
func (p AuthoritativeDNSRecordProvider) createAuthoritativeRecord(ctx context.Context) (*v1alpha1.DNSRecord, error) {
	var aRecord *v1alpha1.DNSRecord
	expectedRecord := authoritativeRecordFor(p.DNSRecord)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(expectedRecord)
	if err != nil {
		return nil, err
	}

	uRecord, err := p.NamespacedClient.Create(ctx, &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(uRecord.Object, &aRecord); err != nil {
		return nil, err
	}

	return aRecord, nil
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
