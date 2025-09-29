//go:build unit

package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	cgfake "k8s.io/client-go/dynamic/fake"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func TestNewFactory(t *testing.T) {
	scheme := runtime.NewScheme()

	RegisterProvider("inmemory", newFakeInMemoryProvider, false)
	dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{}...)
	c := crfake.NewClientBuilder().WithScheme(scheme).Build()

	t.Run("returns factory with registered provider", func(t *testing.T) {
		f, err := NewFactory(c, dc, []string{"inmemory"}, nil)
		assert.NotNil(t, f)
		assert.NoError(t, err)
	})

	t.Run("returns error for provider that is not registered", func(t *testing.T) {
		f, err := NewFactory(c, dc, []string{"notregistered"}, nil)
		assert.NotNil(t, f)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "provider 'notregistered' not registered")
	})
}

func TestProviderFor(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	RegisterProvider("inmemory", newFakeInMemoryProvider, false)
	//CoreDNS provider requires delegation
	RegisterProvider("coredns", newFakeCoreDNSProvider, false)

	inmemorySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-inmemory-secret",
			Namespace: "test-namespace",
		},
		Type: v1alpha1.SecretTypeKuadrantInmemory,
	}

	corednsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-coredns-secret",
			Namespace: "test-namespace",
		},
		Type: v1alpha1.SecretTypeKuadrantCoreDNS,
	}

	dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{}...)
	c := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(inmemorySecret, corednsSecret).Build()

	f, err := NewFactory(c, dc, []string{"inmemory", "coredns"}, newFakeDelegationProvider)
	assert.NotNil(t, f)
	assert.NoError(t, err)

	t.Run("returns error if delegation required but not configured", func(t *testing.T) {
		f2, err := NewFactory(c, dc, []string{"inmemory"}, nil)
		assert.NotNil(t, f)
		assert.NoError(t, err)

		pa := &fakeProviderAccessor{
			namespace:       "test-namespace",
			providerRef:     v1alpha1.ProviderRef{},
			rootHost:        "example.com",
			isAuthoritative: false,
			isDelegating:    true,
		}

		p, err := f2.ProviderFor(t.Context(), pa, Config{})
		assert.Nil(t, p)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errDelegationProviderNotConfigured)
	})

	t.Run("returns error if accessor has no provider ref and is not delegating", func(t *testing.T) {
		pa := &fakeProviderAccessor{
			namespace:       "test-namespace",
			providerRef:     v1alpha1.ProviderRef{},
			rootHost:        "example.com",
			isAuthoritative: false,
			isDelegating:    false,
		}

		p, err := f.ProviderFor(t.Context(), pa, Config{})
		assert.Nil(t, p)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errProviderRefRequired)
	})

	t.Run("returns error if provider secret not found", func(t *testing.T) {
		pa := &fakeProviderAccessor{
			namespace: "test-namespace",
			providerRef: v1alpha1.ProviderRef{
				Name: "idonotexist",
			},
			rootHost:        "example.com",
			isAuthoritative: false,
			isDelegating:    false,
		}

		p, err := f.ProviderFor(t.Context(), pa, Config{})
		assert.Nil(t, p)
		assert.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("returns configured delegation provider if accessor requests delegation", func(t *testing.T) {
		pa := &fakeProviderAccessor{
			namespace:       "test-namespace",
			providerRef:     v1alpha1.ProviderRef{},
			rootHost:        "example.com",
			isAuthoritative: false,
			isDelegating:    true,
		}

		p, err := f.ProviderFor(t.Context(), pa, Config{})
		assert.NotNil(t, p)
		assert.NoError(t, err)
		assert.Equal(t, string(p.Name()), "fake-delegation-provider")
	})

	t.Run("returns configured delegation provider if provider requires delegation", func(t *testing.T) {
		t.Run("when accessor requests delegation and is not authoritative", func(t *testing.T) {
			pa := &fakeProviderAccessor{
				namespace: "test-namespace",
				providerRef: v1alpha1.ProviderRef{
					Name: "my-coredns-secret",
				},
				rootHost:        "example.com",
				isAuthoritative: false,
				isDelegating:    true,
			}
			p, err := f.ProviderFor(t.Context(), pa, Config{})
			assert.NotNil(t, p)
			assert.NoError(t, err)
			assert.Equal(t, string(p.Name()), "fake-delegation-provider")
		})

		t.Run("when accessor requests delegation and is authoritative", func(t *testing.T) {
			pa := &fakeProviderAccessor{
				namespace: "test-namespace",
				providerRef: v1alpha1.ProviderRef{
					Name: "my-coredns-secret",
				},
				rootHost:        "example.com",
				isAuthoritative: true,
				isDelegating:    true,
			}
			p, err := f.ProviderFor(t.Context(), pa, Config{})
			assert.NotNil(t, p)
			assert.NoError(t, err)
			assert.Equal(t, string(p.Name()), "fake-delegation-provider")
		})

		t.Run("when accessor requests no delegation and is not authoritative", func(t *testing.T) {
			pa := &fakeProviderAccessor{
				namespace: "test-namespace",
				providerRef: v1alpha1.ProviderRef{
					Name: "my-coredns-secret",
				},
				rootHost:        "example.com",
				isAuthoritative: false,
				isDelegating:    false,
			}
			p, err := f.ProviderFor(t.Context(), pa, Config{})
			assert.NotNil(t, p)
			assert.NoError(t, err)
			assert.Equal(t, string(p.Name()), "fake-delegation-provider")
		})
	})

	t.Run("returns expected provider if provider requires delegation but is authoritative", func(t *testing.T) {
		pa := &fakeProviderAccessor{
			namespace: "test-namespace",
			providerRef: v1alpha1.ProviderRef{
				Name: "my-coredns-secret",
			},
			rootHost:        "example.com",
			isAuthoritative: true,
			isDelegating:    false,
		}
		p, err := f.ProviderFor(t.Context(), pa, Config{})
		assert.NotNil(t, p)
		assert.NoError(t, err)
		assert.Equal(t, string(p.Name()), "coredns")
	})

	t.Run("returns expected provider if no delegation", func(t *testing.T) {
		pa := &fakeProviderAccessor{
			namespace: "test-namespace",
			providerRef: v1alpha1.ProviderRef{
				Name: "my-inmemory-secret",
			},
			rootHost:        "example.com",
			isAuthoritative: false,
			isDelegating:    false,
		}

		p, err := f.ProviderFor(t.Context(), pa, Config{})
		assert.NotNil(t, p)
		assert.NoError(t, err)
		assert.Equal(t, string(p.Name()), "inmemory")
	})
}

var _ v1alpha1.ProviderAccessor = fakeProviderAccessor{}

type fakeProviderAccessor struct {
	namespace       string
	providerRef     v1alpha1.ProviderRef
	rootHost        string
	isAuthoritative bool
	isDelegating    bool
}

func (f fakeProviderAccessor) GetNamespace() string {
	return f.namespace
}

func (f fakeProviderAccessor) GetProviderRef() v1alpha1.ProviderRef {
	return f.providerRef
}

func (f fakeProviderAccessor) GetRootHost() string {
	return f.rootHost
}

func (f fakeProviderAccessor) IsDelegating() bool {
	return f.isDelegating
}

func (f fakeProviderAccessor) IsAuthoritativeRecord() bool {
	return f.isAuthoritative
}

var _ Provider = fakeProvider{}

type fakeProvider struct {
	name DNSProviderName
}

func newFakeInMemoryProvider(_ context.Context, _ *corev1.Secret, _ Config) (Provider, error) {
	return &fakeProvider{"inmemory"}, nil
}

func newFakeCoreDNSProvider(_ context.Context, _ *corev1.Secret, _ Config) (Provider, error) {
	return &fakeProvider{"coredns"}, nil
}

func newFakeDelegationProvider(_ context.Context, _ dynamic.Interface, _ v1alpha1.ProviderAccessor, _ Config) (Provider, error) {
	return &fakeProvider{"fake-delegation-provider"}, nil
}

func (f fakeProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}

func (f fakeProvider) ApplyChanges(_ context.Context, _ *plan.Changes) error {
	return nil
}

func (f fakeProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (f fakeProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

func (f fakeProvider) DNSZones(_ context.Context) ([]DNSZone, error) {
	return []DNSZone{}, nil
}

func (f fakeProvider) DNSZoneForHost(_ context.Context, _ string) (*DNSZone, error) {
	return &DNSZone{}, nil
}

func (f fakeProvider) ProviderSpecific() ProviderSpecificLabels {
	return ProviderSpecificLabels{}
}

func (f fakeProvider) Name() DNSProviderName {
	return f.name
}

func (f fakeProvider) Labels() map[string]string {
	return map[string]string{}
}
