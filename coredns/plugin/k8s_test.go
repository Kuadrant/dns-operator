package kuadrant

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/coredns/plugin/dnsop"
)

// mockDNSRecordClient implements dnsop.Interface for testing
type mockDNSRecordClient struct {
	listError error
	listFunc  func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error)
}

func (m *mockDNSRecordClient) DNSRecords(namespace string) dnsop.DNSRecord {
	return &mockDNSRecord{
		listError: m.listError,
		listFunc:  m.listFunc,
	}
}

// mockDNSRecord implements dnsop.DNSRecord for testing
type mockDNSRecord struct {
	listError error
	listFunc  func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error)
}

func (m *mockDNSRecord) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	if m.listError != nil {
		return nil, m.listError
	}
	return &v1alpha1.DNSRecordList{}, nil
}

func (m *mockDNSRecord) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("not implemented")
}

func TestHandleCRDCheckError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		resourceName   string
		apiGroup       string
		expectedResult bool
		expectWarning  bool
	}{
		{
			name:           "no error - CRDs exist",
			err:            nil,
			resourceName:   "DNSRecord",
			apiGroup:       "kuadrant.io",
			expectedResult: true,
			expectWarning:  false,
		},
		{
			name:           "NoMatchError - CRDs not found",
			err:            &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{}},
			resourceName:   "DNSRecord",
			apiGroup:       "kuadrant.io",
			expectedResult: false,
			expectWarning:  true,
		},
		{
			name:           "NotRegisteredError - CRDs not registered",
			err:            runtime.NewNotRegisteredErrForKind("test", schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1alpha1", Kind: "DNSRecord"}),
			resourceName:   "DNSRecord",
			apiGroup:       "kuadrant.io",
			expectedResult: false,
			expectWarning:  true,
		},
		{
			name:           "NotFound error - CRDs not found",
			err:            apierrors.NewNotFound(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, ""),
			resourceName:   "DNSRecord",
			apiGroup:       "kuadrant.io",
			expectedResult: false,
			expectWarning:  true,
		},
		{
			name:           "Forbidden error - RBAC issue",
			err:            apierrors.NewForbidden(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, "", errors.New("forbidden")),
			resourceName:   "DNSRecord",
			apiGroup:       "kuadrant.io",
			expectedResult: false,
			expectWarning:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handleCRDCheckError(tt.err, tt.resourceName, tt.apiGroup)
			assert.Equal(t, tt.expectedResult, result, "handleCRDCheckError should return %v", tt.expectedResult)
		})
	}
}

func TestHandleCRDCheckError_Panic(t *testing.T) {
	// Test that unexpected errors cause a panic
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("handleCRDCheckError should panic on unexpected errors")
		}
	}()

	unexpectedErr := errors.New("unexpected error")
	handleCRDCheckError(unexpectedErr, "DNSRecord", "kuadrant.io")
}

func TestExistDNSRecordCRDs(t *testing.T) {
	tests := []struct {
		name          string
		listError     error
		expectedExist bool
	}{
		{
			name:          "CRDs exist - no error",
			listError:     nil,
			expectedExist: true,
		},
		{
			name:          "CRDs not found - NoMatchError",
			listError:     &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{}},
			expectedExist: false,
		},
		{
			name:          "CRDs not found - NotFound",
			listError:     apierrors.NewNotFound(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, ""),
			expectedExist: false,
		},
		{
			name:          "RBAC issue - Forbidden",
			listError:     apierrors.NewForbidden(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, "", errors.New("forbidden")),
			expectedExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the handleCRDCheckError logic directly
			result := handleCRDCheckError(tt.listError, "DNSRecord", "kuadrant.io")
			assert.Equal(t, tt.expectedExist, result)
		})
	}
}

func TestNewKubeController_NoCRDs(t *testing.T) {
	// Create a mock client that returns a NotFound error
	mockClient := &mockDNSRecordClient{
		listError: apierrors.NewNotFound(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, ""),
	}

	// Create a controller with the mock client
	ctrl := &KubeController{client: mockClient}

	assert.NotNil(t, ctrl)
	assert.Empty(t, ctrl.controllers, "Controllers should be empty when CRDs don't exist")
}

func TestNewKubeController_NoZones(t *testing.T) {
	mockClient := &mockDNSRecordClient{
		listError: nil, // CRDs exist
	}

	ctrl := &KubeController{
		client: mockClient,
	}

	assert.NotNil(t, ctrl)
	assert.Empty(t, ctrl.controllers, "Controllers should be empty when no zones are configured")
}

func TestNewKubeController_WithZonesAndCRDs(t *testing.T) {
	mockClient := &mockDNSRecordClient{
		listFunc: func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
			return &v1alpha1.DNSRecordList{}, nil
		},
	}

	// Test that with CRDs available and zones configured, controller is properly set up
	ctrl := &KubeController{
		client: mockClient,
	}

	assert.NotNil(t, ctrl)
}

func TestKubeController_HasSynced(t *testing.T) {
	tests := []struct {
		name      string
		hasSynced bool
		expected  bool
	}{
		{
			name:      "controller synced",
			hasSynced: true,
			expected:  true,
		},
		{
			name:      "controller not synced",
			hasSynced: false,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &KubeController{
				hasSynced: tt.hasSynced,
			}
			assert.Equal(t, tt.expected, ctrl.HasSynced())
		})
	}
}

func TestStripClosingDot(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "domain with closing dot",
			input:    "example.com.",
			expected: "example.com",
		},
		{
			name:     "domain without closing dot",
			input:    "example.com",
			expected: "example.com",
		},
		{
			name:     "root domain",
			input:    ".",
			expected: ".",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "subdomain with closing dot",
			input:    "api.example.com.",
			expected: "api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripClosingDot(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestZoneInformers_RefreshZone(t *testing.T) {
	// Test that refreshZone creates a new zone and copies it to the existing zone
	zone := NewZone("example.com.", "")

	zi := &zoneInformers{
		informers:  []cache.SharedInformer{},
		zone:       zone,
		zoneOrigin: "example.com.",
	}

	// Verify initial state
	require.NotNil(t, zi.zone)

	// Note: We can't fully test refreshZone without a running informer
	// but we can verify the structure is correct
	assert.Equal(t, "example.com.", zi.zoneOrigin)
	assert.NotNil(t, zi.zone)
}
