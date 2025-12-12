//go:build unit

package endpoint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cgfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/utils/ptr"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/internal/provider"
)

func TestNewAuthoritativeDNSRecordProvider(t *testing.T) {
	scheme := runtime.NewScheme()

	dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{}...)

	t.Run("returns error for accessor that is not a DNSRecord", func(t *testing.T) {
		pa := dummyProviderAccessor{}

		actualProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
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

		actualProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
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
		Status: v1alpha1.DNSRecordStatus{
			ZoneDomainName: "example.com",
		},
	}

	t.Run("creates authoritative record if one does not already exist", func(t *testing.T) {
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "foo.example.com")
		// expect an error until zone is set in status
		assert.Nil(t, zone)
		assert.Error(t, err)

		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")

		uList, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").List(t.Context(), metav1.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, uList.Items, 2)

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

		unst, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), desiredAuthRecord.Name, metav1.GetOptions{})
		assert.NoError(t, err)
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unst.Object, &actualAuthRecord)
		assert.NoError(t, err)

		// set the zoneDomainName in the status
		actualAuthRecord.Status.ZoneDomainName = "example.com"
		unstr, err := runtime.DefaultUnstructuredConverter.ToUnstructured(actualAuthRecord)
		assert.NoError(t, err)
		unst.Object = unstr
		_, err = dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Update(t.Context(), unst, metav1.UpdateOptions{})
		assert.NoError(t, err)

		zone, err = authProvider.DNSZoneForHost(t.Context(), "foo.example.com")
		assert.NotNil(t, zone)
		assert.NoError(t, err)

		assert.Equal(t, desiredAuthRecord.Name, actualAuthRecord.Name)
		assert.Equal(t, desiredAuthRecord.Namespace, actualAuthRecord.Namespace)
		assert.Equal(t, desiredAuthRecord.Labels, actualAuthRecord.Labels)
		assert.Equal(t, desiredAuthRecord.Spec.RootHost, actualAuthRecord.Spec.RootHost)
	})

	t.Run("uses existing authoritative record", func(t *testing.T) {
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "example.com")
		assert.NotNil(t, zone)
		assert.NoError(t, err)

		assert.Equal(t, zone.ID, authRecord.Name)
		// When no ZoneDomainName is set in status, falls back to RootHost
		assert.Equal(t, zone.DNSName, authRecord.Spec.RootHost)

		//Check only the one record exists
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		uList, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").List(t.Context(), metav1.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, uList.Items, 1)
	})

	t.Run("uses zone domain name from authoritative record status", func(t *testing.T) {
		rootHost := "loadbalanced.k.example.com"
		authRecordWithZone := authRecord.DeepCopy()
		authRecordWithZone.Spec.RootHost = rootHost
		authRecordWithZone.Name = fmt.Sprintf("authoritative-record-%s", common.HashRootHost(rootHost))
		authRecordWithZone.Labels = map[string]string{
			"kuadrant.io/authoritative-record-hash": common.HashRootHost(rootHost),
			"kuadrant.io/authoritative-record":      "true",
		}

		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecordWithZone}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "loadbalanced.k.example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		// Update the status with the zone domain name
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		unst, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), authRecordWithZone.Name, metav1.GetOptions{})
		assert.NoError(t, err)

		actualAuthRecord := &v1alpha1.DNSRecord{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unst.Object, &actualAuthRecord)
		assert.NoError(t, err)

		actualAuthRecord.Status.ZoneDomainName = "k.example.com"
		unstr, err := runtime.DefaultUnstructuredConverter.ToUnstructured(actualAuthRecord)
		assert.NoError(t, err)
		unst.Object = unstr
		_, err = dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Update(t.Context(), unst, metav1.UpdateOptions{})
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "loadbalanced.k.example.com")
		assert.NotNil(t, zone)
		assert.NoError(t, err)

		assert.Equal(t, zone.ID, authRecordWithZone.Name)
		// Should use the zone domain name from status, not the rootHost
		assert.Equal(t, zone.DNSName, "k.example.com")
		assert.NotEqual(t, zone.DNSName, authRecordWithZone.Spec.RootHost)
	})

	t.Run("adds missing labels to authoritative record", func(t *testing.T) {
		authRecord.Labels = map[string]string{
			"my-label": "foo",
		}
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		zone, err := authProvider.DNSZoneForHost(t.Context(), "example.com")
		assert.NotNil(t, zone)
		assert.NoError(t, err)

		//Check the auth record has correct labels
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		uRecord, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), "authoritative-record-1jbcyj4z", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, uRecord)

		actualAuthRecord := &v1alpha1.DNSRecord{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uRecord.Object, &actualAuthRecord)
		assert.NoError(t, err)

		expectedLabels := map[string]string{
			"my-label":                              "foo",
			"kuadrant.io/authoritative-record-hash": "1jbcyj4z",
			"kuadrant.io/authoritative-record":      "true",
		}
		assert.Equal(t, expectedLabels, actualAuthRecord.Labels)
	})
}

func TestAuthoritativeDNSRecordProvider_ApplyChanges(t *testing.T) {
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

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{"authoritative-record-qtn4u6on"},
			},
		})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		err = authProvider.ApplyChanges(t.Context(), &plan.Changes{})
		assert.NoError(t, err)

		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")

		uList, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").List(t.Context(), metav1.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, uList.Items, 2)

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

		unst, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), desiredAuthRecord.Name, metav1.GetOptions{})
		assert.NoError(t, err)
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unst.Object, &actualAuthRecord)
		assert.NoError(t, err)

		assert.Equal(t, desiredAuthRecord.Name, actualAuthRecord.Name)
		assert.Equal(t, desiredAuthRecord.Namespace, actualAuthRecord.Namespace)
		assert.Equal(t, desiredAuthRecord.Labels, actualAuthRecord.Labels)
		assert.Equal(t, desiredAuthRecord.Spec.RootHost, actualAuthRecord.Spec.RootHost)
	})

	t.Run("adds missing labels to authoritative record", func(t *testing.T) {
		authRecord.Labels = map[string]string{
			"my-label": "foo",
		}
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-record",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{"authoritative-record-1jbcyj4z"},
			},
		})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		err = authProvider.ApplyChanges(t.Context(), &plan.Changes{})
		assert.NoError(t, err)

		//Check the auth record has correct labels
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		uRecord, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), "authoritative-record-1jbcyj4z", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, uRecord)

		actualAuthRecord := &v1alpha1.DNSRecord{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uRecord.Object, &actualAuthRecord)
		assert.NoError(t, err)

		expectedLabels := map[string]string{
			"my-label":                              "foo",
			"kuadrant.io/authoritative-record-hash": "1jbcyj4z",
			"kuadrant.io/authoritative-record":      "true",
		}
		assert.Equal(t, expectedLabels, actualAuthRecord.Labels)
	})

	t.Run("removes empty authoritative record for deleting delegating record", func(t *testing.T) {
		authRecord.Spec.Endpoints = []*externaldns.Endpoint{}
		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-record",
				Namespace:         "test-namespace",
				DeletionTimestamp: ptr.To(metav1.Time{Time: time.Now()}),
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost:  "example.com",
				Endpoints: []*externaldns.Endpoint{},
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{"authoritative-record-1jbcyj4z"},
			},
		})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		err = authProvider.ApplyChanges(t.Context(), &plan.Changes{})
		assert.NoError(t, err)

		//Check no record exists
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		uList, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").List(t.Context(), metav1.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, uList.Items, 0)
	})

	t.Run("does not remove authoritative record that still contains endpoints for deleting delegating record", func(t *testing.T) {
		authRecord.Spec.Endpoints = []*externaldns.Endpoint{
			{
				DNSName:    "foo.example.com",
				Targets:    []string{"127.0.0.1"},
				RecordType: "A",
				RecordTTL:  60,
			},
		}

		dc := cgfake.NewSimpleDynamicClient(scheme, []runtime.Object{authRecord}...)

		pa := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-record",
				Namespace:         "test-namespace",
				DeletionTimestamp: ptr.To(metav1.Time{Time: time.Now()}),
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "example.com",
			},
		}
		authProvider, err := NewAuthoritativeDNSRecordProvider(t.Context(), dc, pa, provider.Config{
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{"authoritative-record-1jbcyj4z"},
			},
		})
		assert.NotNil(t, authProvider)
		assert.NoError(t, err)

		err = authProvider.ApplyChanges(t.Context(), &plan.Changes{})
		assert.NoError(t, err)

		//Check the auth record still exists
		dnsRecordsGVR := v1alpha1.GroupVersion.WithResource("dnsrecords")
		uList, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").List(t.Context(), metav1.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, uList.Items, 1)
		uRecord, err := dc.Resource(dnsRecordsGVR).Namespace("test-namespace").Get(t.Context(), "authoritative-record-1jbcyj4z", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.NotNil(t, uRecord)
	})
}

func Test_authoritativeRecordProviderSecretFor(t *testing.T) {
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

	actualSecret := authoritativeRecordProviderSecretFor(record)
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
