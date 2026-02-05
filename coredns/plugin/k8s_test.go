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

func TestNewKubeController_GracefulDegradation(t *testing.T) {
	tests := []struct {
		name        string
		clientError error
		zones       map[string]*Zone
	}{
		{
			name:        "NoMatchError - CRDs not registered",
			clientError: &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{Group: "kuadrant.io", Resource: "dnsrecords"}},
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			},
		},
		{
			name:        "NotRegisteredError - CRDs not in scheme",
			clientError: runtime.NewNotRegisteredErrForKind("test", schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1alpha1", Kind: "DNSRecord"}),
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			},
		},
		{
			name:        "NotFound - CRDs not found",
			clientError: apierrors.NewNotFound(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, ""),
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			},
		},
		{
			name:        "Forbidden - RBAC issue with zones",
			clientError: apierrors.NewForbidden(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, "", errors.New("forbidden")),
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			},
		},
		{
			name:        "CRDs not found - no zones configured",
			clientError: apierrors.NewNotFound(schema.GroupResource{Group: "kuadrant.io", Resource: "dnsrecords"}, ""),
			zones:       map[string]*Zone{},
		},
		{
			name:        "CRDs exist - no zones (empty map)",
			clientError: nil,
			zones:       map[string]*Zone{},
		},
		{
			name:        "CRDs exist - no zones (nil map)",
			clientError: nil,
			zones:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDNSRecordClient{
				listError: tt.clientError,
			}

			ctx := t.Context()
			ctrl := newKubeController(ctx, mockClient, tt.zones)

			assert.NotNil(t, ctrl, "Controller should never be nil")
			assert.Empty(t, ctrl.controllers, "Controllers should be empty - gracefully degraded")
			assert.Equal(t, mockClient, ctrl.client)
		})
	}
}

func TestNewKubeController_WithZonesAndCRDs(t *testing.T) {
	tests := []struct {
		name              string
		zones             map[string]*Zone
		expectedZoneCount int
	}{
		{
			name: "single zone configured",
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			},
			expectedZoneCount: 1,
		},
		{
			name: "multiple zones configured",
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
				"test.org.":    NewZone("test.org.", ""),
			},
			expectedZoneCount: 2,
		},
		{
			name: "zone with custom rname",
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", "admin@example.com"),
			},
			expectedZoneCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDNSRecordClient{
				listFunc: func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
					return &v1alpha1.DNSRecordList{}, nil
				},
			}

			ctx := t.Context()
			ctrl := newKubeController(ctx, mockClient, tt.zones)

			assert.NotNil(t, ctrl)
			assert.Equal(t, mockClient, ctrl.client)
			assert.Len(t, ctrl.controllers, tt.expectedZoneCount, "Should create one controller per zone")

			// Verify each zone controller is properly set up
			for _, zi := range ctrl.controllers {
				assert.NotNil(t, zi.zone)
				assert.NotEmpty(t, zi.zoneOrigin)
				// Each zone should have at least one informer (for NamespaceAll by default)
				assert.NotEmpty(t, zi.informers, "Each zone should have at least one informer")
			}
		})
	}
}

func TestNewKubeController_MultipleNamespaces(t *testing.T) {
	// Note: t.Setenv automatically restores the environment variable after each subtest completes.
	// No manual t.Cleanup() is needed - it's handled internally by t.Setenv.
	// Cannot use t.Parallel() with t.Setenv as env vars affect the whole process.
	tests := []struct {
		name                  string
		namespaceEnvValue     string
		expectedInformerCount int
	}{
		{
			name:                  "single namespace",
			namespaceEnvValue:     "default",
			expectedInformerCount: 1,
		},
		{
			name:                  "multiple namespaces",
			namespaceEnvValue:     "default,kube-system,test",
			expectedInformerCount: 3,
		},
		{
			name:                  "no namespace env - defaults to all",
			namespaceEnvValue:     "",
			expectedInformerCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(watchNamespacesEnvVar, tt.namespaceEnvValue)

			mockClient := &mockDNSRecordClient{
				listFunc: func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
					return &v1alpha1.DNSRecordList{}, nil
				},
			}

			zones := map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
			}

			ctx := t.Context()
			ctrl := newKubeController(ctx, mockClient, zones)

			require.Len(t, ctrl.controllers, 1)
			assert.Len(t, ctrl.controllers[0].informers, tt.expectedInformerCount)
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

func TestNewKubeController_NilZoneValues(t *testing.T) {
	tests := []struct {
		name              string
		zones             map[string]*Zone
		expectedCtrlCount int
	}{
		{
			name: "single nil zone - skipped",
			zones: map[string]*Zone{
				"example.com.": nil,
			},
			expectedCtrlCount: 0,
		},
		{
			name: "mixed valid and nil zones",
			zones: map[string]*Zone{
				"example.com.": NewZone("example.com.", ""),
				"bad.com.":     nil,
				"good.org.":    NewZone("good.org.", ""),
			},
			expectedCtrlCount: 2,
		},
		{
			name: "all nil zones",
			zones: map[string]*Zone{
				"bad1.com.": nil,
				"bad2.com.": nil,
			},
			expectedCtrlCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDNSRecordClient{
				listFunc: func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
					return &v1alpha1.DNSRecordList{}, nil
				},
			}

			ctx := t.Context()
			ctrl := newKubeController(ctx, mockClient, tt.zones)

			assert.NotNil(t, ctrl)
			assert.Len(t, ctrl.controllers, tt.expectedCtrlCount,
				"Should skip nil zones and only create controllers for valid zones")
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
