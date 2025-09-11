//go:build unit

package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cgfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

func TestNewAuthoritativeDNSRecordProvider(t *testing.T) {
	scheme := runtime.NewScheme()

	dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{}...)
	c := crfake.NewClientBuilder().WithScheme(scheme).Build()

	t.Run("returns error for accessor that is not a DNSRecord", func(t *testing.T) {
		pa := dummyProviderAccessor{}

		actualProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), c, dc, pa, provider.Config{})
		assert.Nil(t, actualProvider)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errIncompatibleAccessorType)
	})

	t.Run("returns provider for DNSRecord accessor", func(t *testing.T) {
		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}

		actualProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), c, dc, pa, provider.Config{})
		assert.NotNil(t, actualProvider)
		assert.NoError(t, err)
	})
}

func TestAuthoritativeDNSRecordProvider_DNSZoneForHost(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	authRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authoritative-record-1jbcyj4z",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"kuadrant.io/authoritative-record-hash": "1jbcyj4z",
				"kuadrant.io/authoritative-record":      "true",
			},
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: "example.com",
		},
	}

	t.Run("creates authoritative record if one does not already exist", func(t *testing.T) {
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)
		c := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(authRecord).Build()

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), c, dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "foo.example.com")
		assert.Nil(t, zone)  // Check this even though it should not be nil
		assert.Error(t, err) // Check this even though it should be nil

		// This isn't really working as desired because we are using two different fake clients.
		// All we can do is check the expected record was created in the cr fake client that the AuthoritativeDNSRecordProvider uses.
		dnsRecords := &v1alpha1.DNSRecordList{}
		err = c.List(t.Context(), dnsRecords, client.InNamespace("test-namespace"))
		assert.NoError(t, err)
		assert.Len(t, dnsRecords.Items, 2)

		desiredAuthRecord := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authoritative-record-qtn4u6on",
				Namespace: "test-namespace",
				Labels: map[string]string{
					"kuadrant.io/authoritative-record-hash": "qtn4u6on",
					"kuadrant.io/authoritative-record":      "true",
				},
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
			},
		}

		actualAuthRecord := &v1alpha1.DNSRecord{}
		err = c.Get(t.Context(), client.ObjectKeyFromObject(desiredAuthRecord), actualAuthRecord)
		assert.NoError(t, err)
		assert.Equal(t, desiredAuthRecord.Name, actualAuthRecord.Name)
		assert.Equal(t, desiredAuthRecord.Namespace, actualAuthRecord.Namespace)
		assert.Equal(t, desiredAuthRecord.Labels, actualAuthRecord.Labels)
		assert.Equal(t, desiredAuthRecord.Spec.RootHost, actualAuthRecord.Spec.RootHost)
	})

	t.Run("uses existing authoritative record", func(t *testing.T) {
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)
		c := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(authRecord).Build()

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), c, dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "example.com")
		assert.NotNil(t, zone)
		assert.NoError(t, err)

		assert.Equal(t, zone.ID, authRecord.Name)
		assert.Equal(t, zone.DNSName, authRecord.Spec.RootHost)

		//Check only the one record exists
		dnsRecords := &v1alpha1.DNSRecordList{}
		err = c.List(t.Context(), dnsRecords, client.InNamespace("test-namespace"))
		assert.NoError(t, err)
		assert.Len(t, dnsRecords.Items, 1)
	})
}

func Test_authoritativeRecordProviderSecret(t *testing.T) {
	record := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-record",
			Namespace: "test-namespace",
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: "example.com",
		},
	}

	expectedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"ENDPOINT_ZONE_RECORD_LABEL": []byte("kuadrant.io/authoritative-record=true, kuadrant.io/authoritative-record-hash=1jbcyj4z"),
			"ENDPOINT_GVR":               []byte("kuadrant.io/v1alpha1.dnsrecords"),
		},
		Type: "kuadrant.io/endpoint",
	}

	actualSecret := authoritativeRecordProviderSecret(record)
	assert.Equal(t, expectedSecret, actualSecret)
}

var _ v1alpha1.ProviderAccessor = dummyProviderAccessor{}

type dummyProviderAccessor struct{}

func (f dummyProviderAccessor) GetNamespace() string {
	return ""
}

func (f dummyProviderAccessor) GetProviderRef() v1alpha1.ProviderRef {
	return v1alpha1.ProviderRef{}
}

func (f dummyProviderAccessor) GetRootHost() string {
	return ""
}

func (f dummyProviderAccessor) IsAuthoritativeRecord() bool {
	return false
}

func (f dummyProviderAccessor) IsDelegating() bool {
	return false
}
