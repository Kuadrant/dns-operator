//go:build unit

package common

import (
	"testing"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Test_ParseGVKString(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name      string
		GVKString string
		Verify    func(gvk schema.GroupVersionKind, err error)
	}{
		{
			Name:      "Good GVK parses as expected",
			GVKString: "kuadrant.io/v1alpha1.dnsrecord",
			Verify: func(gvk schema.GroupVersionKind, err error) {
				Expect(err).To(BeNil())
				Expect(gvk).To(BeEquivalentTo(
					schema.GroupVersionKind{
						Group:   "kuadrant.io",
						Version: "v1alpha1",
						Kind:    "dnsrecord",
					},
				))
			},
		},
		{
			Name:      "error on bad GVK format",
			GVKString: "kuadrant.io.v1alpha1.dnsrecord",
			Verify: func(gvk schema.GroupVersionKind, err error) {
				Expect(err).NotTo(BeNil())
				Expect(gvk).To(BeEquivalentTo(
					schema.GroupVersionKind{},
				))
			},
		},
		{
			Name:      "GVK with long kind names parses as expected",
			GVKString: "kuadrant.io/v1alpha1.dnsrecord.kuadrant.io",
			Verify: func(gvk schema.GroupVersionKind, err error) {
				Expect(err).To(BeNil())
				Expect(gvk).To(BeEquivalentTo(
					schema.GroupVersionKind{
						Group:   "kuadrant.io",
						Version: "v1alpha1",
						Kind:    "dnsrecord.kuadrant.io",
					},
				))
			},
		},
		{
			Name:      "GVK with underscore in place of slash parses as expected",
			GVKString: "kuadrant.io_v1alpha1.dnsrecord.kuadrant.io",
			Verify: func(gvk schema.GroupVersionKind, err error) {
				Expect(err).To(BeNil())
				Expect(gvk).To(BeEquivalentTo(
					schema.GroupVersionKind{
						Group:   "kuadrant.io",
						Version: "v1alpha1",
						Kind:    "dnsrecord.kuadrant.io",
					},
				))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			scenario.Verify(ParseGVKString(scenario.GVKString))

		})
	}
}
