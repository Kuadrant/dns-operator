package provider

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
)

type DelegationHandler interface {
	// AdaptAuthoritativeRecordProvider returns the given provider adapted as required.
	AdaptAuthoritativeRecordProvider(Provider, v1alpha1.ProviderAccessor) Provider
	// AuthoritativeRecordProviderSecret returns the secret for the provider that should be used for `authoritative` records.
	AuthoritativeRecordProviderSecret(v1alpha1.ProviderAccessor) *v1.Secret
}

var _ DelegationHandler = DNSRecordDelegationHandler{}

type DNSRecordDelegationHandler struct {
	client.Client
}

// AdaptAuthoritativeRecordProvider modifies the provider to ensure the existence of a suitable authoritative DNSRecord in the given accessors(DNSRecord) namespace.
// The authoritative DNSRecord will be created if required on calls to DNSZoneForHost.
// An authoritative DNSRecord must have the delegation label with a value matching the given DNSRecords rootHost.
// i.e. kuadrant.io/delegation-authoritative-record=<hash of record.spec.rootHost>
func (r DNSRecordDelegationHandler) AdaptAuthoritativeRecordProvider(provider Provider, pa v1alpha1.ProviderAccessor) Provider {
	return newAuthoritativeRecordAdapter(provider, pa, r.Client)
}

// AuthoritativeRecordProviderSecret returns an "endpoint" provider secret configured for DNSRecord resources and a label selector for the given accessor(DNSRecord).
func (r DNSRecordDelegationHandler) AuthoritativeRecordProviderSecret(pa v1alpha1.ProviderAccessor) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint",
			Namespace: pa.GetNamespace(),
		},
		Type: v1alpha1.SecretTypeKuadrantEndpoint,
		Data: map[string][]byte{
			v1alpha1.EndpointLabelSelectorKey: []byte(authoritativeRecordSelectorFor(pa)),
			v1alpha1.EndpointGVRKey:           []byte(v1alpha1.DefaultEndpointGVR),
		},
	}
}

var _ Provider = &authoritativeRecordAdapter{}

type authoritativeRecordAdapter struct {
	client.Client
	Provider
	v1alpha1.ProviderAccessor
}

func newAuthoritativeRecordAdapter(p Provider, pa v1alpha1.ProviderAccessor, c client.Client) Provider {
	return &authoritativeRecordAdapter{
		Provider:         NewWrappedProvider(p),
		ProviderAccessor: pa,
		Client:           c,
	}
}

func (p authoritativeRecordAdapter) DNSZoneForHost(ctx context.Context, host string) (*DNSZone, error) {
	err := p.ensureAuthoritativeRecord(ctx, p.ProviderAccessor)
	if err != nil {
		return nil, err
	}
	return p.Provider.DNSZoneForHost(ctx, host)
}

func (p authoritativeRecordAdapter) ensureAuthoritativeRecord(ctx context.Context, pa v1alpha1.ProviderAccessor) error {
	aRecord, err := p.getAuthoritativeRecordFor(ctx, pa)
	if err != nil {
		return err
	}
	if aRecord != nil {
		return err
	}
	_, err = p.createAuthoritativeRecordFor(ctx, pa)
	return err
}

func (p authoritativeRecordAdapter) getAuthoritativeRecordFor(ctx context.Context, pa v1alpha1.ProviderAccessor) (*v1alpha1.DNSRecord, error) {
	aRecords := v1alpha1.DNSRecordList{}

	labelSelector, err := labels.Parse(authoritativeRecordSelectorFor(pa))
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

func (p authoritativeRecordAdapter) createAuthoritativeRecordFor(ctx context.Context, pa v1alpha1.ProviderAccessor) (*v1alpha1.DNSRecord, error) {
	aRecord := authoritativeRecordFor(pa)

	if err := p.Client.Create(ctx, aRecord, &client.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create authoritative record: %w", err)
	}

	return aRecord, nil
}

func authoritativeRecordFor(pa v1alpha1.ProviderAccessor) *v1alpha1.DNSRecord {
	rootHostHash := common.HashRootHost(pa.GetRootHost())
	pRef := pa.GetProviderRef()
	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toAuthoritativeRecordName(rootHostHash),
			Namespace: pa.GetNamespace(),
			Labels: map[string]string{
				v1alpha1.AuthoritativeRecordLabel:     "true",
				v1alpha1.AuthoritativeRecordHashLabel: rootHostHash,
			},
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost:    pa.GetRootHost(),
			ProviderRef: &pRef,
		},
	}
}

func toAuthoritativeRecordName(rootHostHash string) string {
	return fmt.Sprintf("authoritative-record-%s", rootHostHash)
}

func authoritativeRecordSelectorFor(pa v1alpha1.ProviderAccessor) string {
	return fmt.Sprintf("%s=true, %s=%s", v1alpha1.AuthoritativeRecordLabel, v1alpha1.AuthoritativeRecordHashLabel, common.HashRootHost(pa.GetRootHost()))
}
