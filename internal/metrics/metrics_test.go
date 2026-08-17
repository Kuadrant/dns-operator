package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/types"
)

func collectMetrics(ch chan prometheus.Metric) []*dto.Metric {
	var results []*dto.Metric
	for m := range ch {
		pb := &dto.Metric{}
		_ = m.Write(pb)
		results = append(results, pb)
	}
	return results
}

func labelValue(m *dto.Metric, name string) string {
	for _, lp := range m.Label {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}

func TestRecordGroupInfoMetric_Grouped(t *testing.T) {
	record := v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-record",
			Namespace: "test-ns",
		},
		Status: v1alpha1.DNSRecordStatus{
			Group: types.Group("us-east"),
		},
	}

	ch := make(chan prometheus.Metric, 1)
	recordGroupInfoMetric(ch, record)
	close(ch)

	metrics := collectMetrics(ch)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if got := m.Gauge.GetValue(); got != 1 {
		t.Errorf("expected gauge value 1, got %f", got)
	}
	if got := labelValue(m, "dns_record_name"); got != "test-record" {
		t.Errorf("expected dns_record_name=test-record, got %s", got)
	}
	if got := labelValue(m, "dns_record_namespace"); got != "test-ns" {
		t.Errorf("expected dns_record_namespace=test-ns, got %s", got)
	}
	if got := labelValue(m, "group"); got != "us-east" {
		t.Errorf("expected group=us-east, got %s", got)
	}
}

func TestRecordGroupInfoMetric_Ungrouped(t *testing.T) {
	record := v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ungrouped-record",
			Namespace: "default",
		},
		Status: v1alpha1.DNSRecordStatus{},
	}

	ch := make(chan prometheus.Metric, 1)
	recordGroupInfoMetric(ch, record)
	close(ch)

	metrics := collectMetrics(ch)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if got := m.Gauge.GetValue(); got != 1 {
		t.Errorf("expected gauge value 1, got %f", got)
	}
	if got := labelValue(m, "dns_record_name"); got != "ungrouped-record" {
		t.Errorf("expected dns_record_name=ungrouped-record, got %s", got)
	}
	if got := labelValue(m, "dns_record_namespace"); got != "default" {
		t.Errorf("expected dns_record_namespace=default, got %s", got)
	}
	if got := labelValue(m, "group"); got != "" {
		t.Errorf("expected group to be empty, got %s", got)
	}
}

func TestRecordGroupActiveMetric_Active(t *testing.T) {
	record := v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-record",
			Namespace: "test-ns",
		},
		Status: v1alpha1.DNSRecordStatus{
			Group: types.Group("us-east"),
			Conditions: []metav1.Condition{
				{
					Type:   string(v1alpha1.ConditionTypeActive),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	ch := make(chan prometheus.Metric, 1)
	recordGroupActiveMetric(ch, record)
	close(ch)

	metrics := collectMetrics(ch)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if got := m.Gauge.GetValue(); got != 1 {
		t.Errorf("expected gauge value 1, got %f", got)
	}
	if got := labelValue(m, "dns_record_name"); got != "active-record" {
		t.Errorf("expected dns_record_name=active-record, got %s", got)
	}
	if got := labelValue(m, "dns_record_namespace"); got != "test-ns" {
		t.Errorf("expected dns_record_namespace=test-ns, got %s", got)
	}
	if got := labelValue(m, "group"); got != "us-east" {
		t.Errorf("expected group=us-east, got %s", got)
	}
}

func TestRecordGroupActiveMetric_Inactive(t *testing.T) {
	record := v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inactive-record",
			Namespace: "test-ns",
		},
		Status: v1alpha1.DNSRecordStatus{
			Group: types.Group("us-west"),
			Conditions: []metav1.Condition{
				{
					Type:   string(v1alpha1.ConditionTypeActive),
					Status: metav1.ConditionFalse,
				},
			},
		},
	}

	ch := make(chan prometheus.Metric, 1)
	recordGroupActiveMetric(ch, record)
	close(ch)

	metrics := collectMetrics(ch)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if got := m.Gauge.GetValue(); got != 0 {
		t.Errorf("expected gauge value 0, got %f", got)
	}
	if got := labelValue(m, "group"); got != "us-west" {
		t.Errorf("expected group=us-west, got %s", got)
	}
}

func TestRecordGroupActiveMetric_Ungrouped(t *testing.T) {
	record := v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ungrouped-record",
			Namespace: "default",
		},
		Status: v1alpha1.DNSRecordStatus{},
	}

	ch := make(chan prometheus.Metric, 1)
	recordGroupActiveMetric(ch, record)
	close(ch)

	metrics := collectMetrics(ch)
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if got := m.Gauge.GetValue(); got != 0 {
		t.Errorf("expected gauge value 0 for ungrouped record (no Active condition), got %f", got)
	}
	if got := labelValue(m, "group"); got != "" {
		t.Errorf("expected group to be empty, got %s", got)
	}
}
