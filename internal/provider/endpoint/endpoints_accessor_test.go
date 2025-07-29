/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package endpoint

import (
	"testing"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func Test_EndpointsAccessor(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name   string
		Record *v1alpha1.DNSRecord
		Verify func(ea *endpointAccessor, err error)
	}{
		{
			Name: "Regular DNSRecord parses as expected and can be modified",
			Record: &v1alpha1.DNSRecord{
				Spec: v1alpha1.DNSRecordSpec{
					Endpoints: []*endpoint.Endpoint{
						{
							DNSName:    "test-root",
							Targets:    []string{"127.0.0.1"},
							RecordType: "A",
							RecordTTL:  60,
						},
					},
					RootHost: "test-root",
				},
			},
			Verify: func(ea *endpointAccessor, err error) {
				Expect(err).To(BeNil())
				Expect(ea.GetRootHost()).To(Equal("test-root"))
				Expect(len(ea.GetEndpoints())).To(Equal(1))

				Expect(ea.EnsureEndpoint(&endpoint.Endpoint{
					DNSName:    "sub.test-root",
					Targets:    []string{"test-root"},
					RecordType: "CNAME",
					RecordTTL:  60,
				})).To(BeNil())

				Expect(len(ea.GetEndpoints())).To(Equal(2))

				Expect(len(ea.GetObject().Object["spec"].(map[string]interface{})["endpoints"].([]interface{}))).To(Equal(2))

				Expect(ea.RemoveEndpoint(&endpoint.Endpoint{
					DNSName:    "sub.test-root",
					Targets:    []string{"test-root"},
					RecordType: "CNAME",
					RecordTTL:  60,
				})).To(BeNil())

				Expect(len(ea.GetEndpoints())).To(Equal(1))

				Expect(len(ea.GetObject().Object["spec"].(map[string]interface{})["endpoints"].([]interface{}))).To(Equal(1))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			mapObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scenario.Record)
			unstrObj := &unstructured.Unstructured{
				Object: mapObj,
			}
			Expect(err).To(BeNil())
			scenario.Verify(NewEndpointAccessor(unstrObj))

		})
	}
}
