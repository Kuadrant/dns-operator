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

func Test_SetAnnotation(t *testing.T) {
	RegisterTestingT(t)

	const key = "kuadrant.io/zone-id-filter"

	scenarios := []struct {
		Name      string
		DNSRecord *v1alpha1.DNSRecord
		value     string
		expect    bool
	}{
		{
			Name:      "Set annotation on object with no annotations",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{}},
			value:     "/hostedzone/Z1234567890",
			expect:    true,
		},
		{
			Name:      "Add annotation to existing annotations",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{"other": "value"}}},
			value:     "/hostedzone/Z1234567890",
			expect:    true,
		},
		{
			Name:      "Update annotation with a different value",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{key: "/hostedzone/ZOTHERZONE"}}},
			value:     "/hostedzone/Z1234567890",
			expect:    true,
		},
		{
			Name:      "No update required",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{key: "/hostedzone/Z1234567890"}}},
			value:     "/hostedzone/Z1234567890",
			expect:    false,
		},
		{
			Name:      "Empty value on object with no annotations",
			DNSRecord: &v1alpha1.DNSRecord{ObjectMeta: v1.ObjectMeta{}},
			value:     "",
			expect:    false,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			result := SetAnnotation(scenario.DNSRecord, key, scenario.value)
			Expect(result).To(Equal(scenario.expect))
			if scenario.expect {
				Expect(scenario.DNSRecord.GetAnnotations()[key]).To(Equal(scenario.value))
			}
		})
	}
}
