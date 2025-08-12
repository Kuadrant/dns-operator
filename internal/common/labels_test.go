//go:build unit

package common

import (
	"testing"

	. "github.com/onsi/gomega"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func Test_MergeLabels(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name      string
		DNSRecord *v1alpha1.DNSRecord
		labels    map[string]string
		expect    bool
	}{
		{
			Name:      "Empty object labels",
			DNSRecord: &v1alpha1.DNSRecord{},
			labels:    map[string]string{"default": "value"},
			expect:    true,
		},
		{
			Name:      "label needs updating",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Labels: map[string]string{"default": "wrong value"}}},
			labels:    map[string]string{"default": "value"},
			expect:    true,
		},
		{
			Name:      "Add label to existing",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Labels: map[string]string{"default1": "other value"}}},
			labels:    map[string]string{"default": "value"},
			expect:    true,
		},
		{
			Name:      "Upate label in existing labels",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Labels: map[string]string{"default1": "other value", "default": "wrong value"}}},
			labels:    map[string]string{"default": "value"},
			expect:    true,
		},
		{
			Name:      "No update required",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Labels: map[string]string{"default1": "other value", "default": "value"}}},
			labels:    map[string]string{"default": "value"},
			expect:    false,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			result := MergeLabels(scenario.DNSRecord, scenario.labels)
			Expect(result).To(Equal(scenario.expect))
			labels := scenario.DNSRecord.GetLabels()
			for key, value := range scenario.labels {
				label, exists := labels[key]
				Expect(exists).To(Equal(true))
				Expect(label).To(Equal(value))
			}
		})
	}
}
