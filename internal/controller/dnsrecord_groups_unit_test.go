//go:build unit

package controller

import (
	"context"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/metrics"
	internalprovider "github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"
)

type stubProvider struct {
	endpoints []*endpoint.Endpoint
	applied   []*plan.Changes
}

func (s *stubProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return s.endpoints, nil
}

func (s *stubProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	s.applied = append(s.applied, changes)
	return nil
}

func (s *stubProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (s *stubProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

func (s *stubProvider) DNSZones(ctx context.Context) ([]internalprovider.DNSZone, error) {
	return nil, nil
}

func (s *stubProvider) DNSZoneForHost(ctx context.Context, host string) (*internalprovider.DNSZone, error) {
	return nil, nil
}

func (s *stubProvider) ProviderSpecific() internalprovider.ProviderSpecificLabels {
	return internalprovider.ProviderSpecificLabels{}
}

func (s *stubProvider) Name() internalprovider.DNSProviderName {
	return "stub"
}

func (s *stubProvider) Labels() map[string]string {
	return nil
}

type stubTXTResolver struct {
	groups string
}

func (s *stubTXTResolver) LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error) {
	if s.groups != "" {
		return []string{s.groups}, nil
	}
	return nil, nil
}

func getCleanupCounter(name, namespace, group string) float64 {
	counter, err := metrics.GetInactiveGroupCleanupMetric(name, namespace, group)
	if err != nil || counter == nil {
		return 0
	}
	pb := &dto.Metric{}
	_ = counter.Write(pb)
	return pb.Counter.GetValue()
}

func TestUnpublishInactiveGroups_TXTOnlyCleanup(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx := log.IntoContext(context.Background(), log.Log)

	metrics.ResetInactiveGroupCleanup()

	record := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-record",
			Namespace: "test-ns",
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: "foo.example.com",
		},
		Status: v1alpha1.DNSRecordStatus{
			Group:          types.Group("active-group"),
			ZoneDomainName: "example.com",
		},
	}
	accessor := &DNSRecord{DNSRecord: record}

	prov := &stubProvider{
		endpoints: []*endpoint.Endpoint{
			// TXT registry entry for an inactive group — should be deleted
			{
				DNSName:    "kuadrant-foo.example.com",
				RecordType: "TXT",
				Targets:    []string{"\"heritage=external-dns,external-dns/owner=owner1,external-dns/group=old-group\""},
			},
			// TXT registry entry for the active group — should be kept
			{
				DNSName:    "kuadrant-foo.example.com",
				RecordType: "TXT",
				Targets:    []string{"\"heritage=external-dns,external-dns/owner=owner2,external-dns/group=active-group\""},
			},
		},
	}

	reconciler := &BaseDNSRecordReconciler{
		TXTResolver: &stubTXTResolver{
			groups: "groups=active-group",
		},
	}

	fakeClient := fake.NewClientBuilder().Build()

	err := reconciler.unpublishInactiveGroups(ctx, fakeClient, accessor, prov)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify only TXT phase applied changes (no DNS record changes)
	if len(prov.applied) != 1 {
		t.Fatalf("expected 1 ApplyChanges call (TXT-only), got %d", len(prov.applied))
	}
	if len(prov.applied[0].Delete) != 1 {
		t.Fatalf("expected 1 TXT record deleted, got %d", len(prov.applied[0].Delete))
	}
	deleted := prov.applied[0].Delete[0]
	if deleted.DNSName != "kuadrant-foo.example.com" {
		t.Errorf("expected deleted TXT record for kuadrant-foo.example.com, got %s", deleted.DNSName)
	}
	if len(deleted.Targets) == 0 || !strings.Contains(deleted.Targets[0], "external-dns/group=old-group") {
		t.Errorf("expected deleted TXT target to contain external-dns/group=old-group, got %v", deleted.Targets)
	}

	// Verify the cleanup metric was incremented
	val := getCleanupCounter("test-record", "test-ns", "active-group")
	if val != 1 {
		t.Errorf("expected cleanup counter to be 1, got %f", val)
	}
}
