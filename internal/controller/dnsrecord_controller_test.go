//go:build integration

/*
Copyright 2024.

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

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

var _ = Describe("DNSRecordReconciler", func() {
	var dnsRecord *v1alpha1.DNSRecord
	var managedZone *v1alpha1.ManagedZone
	var testNamespace string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		managedZone = testBuildManagedZone("mz-example-com", testNamespace, "example.com")
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())

		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: getTestEndpoints(),
			},
		}
	})

	AfterEach(func() {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if managedZone != nil {
			err := k8sClient.Delete(ctx, managedZone)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	It("should have ready condition with status true", func() {
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionTrue),
					"Reason":             Equal("ProviderSuccess"),
					"Message":            Equal("Provider ensured the dns record"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
			g.Expect(dnsRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
			g.Expect(dnsRecord.Status.WriteCounter).To(BeZero())
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should not allow ownerID to be updated once set", func() {
		Expect(k8sClient.Create(ctx, dnsRecord)).To(BeNil())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionTrue),
					"Reason":             Equal("ProviderSuccess"),
					"Message":            Equal("Provider ensured the dns record"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
			g.Expect(dnsRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Allows updating from not owned to owned
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())

			dnsRecord.Spec.OwnerID = ptr.To("foobar")
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Does not allow ownerID to change once set
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(PointTo(Equal("foobar")))

			dnsRecord.Spec.OwnerID = ptr.To("foobarbaz")
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).To(MatchError(ContainSubstring("OwnerID is immutable")))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(PointTo(Equal("foobar")))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

	})

	It("should increase write counter if fail to publish record or record is overridden", func() {
		dnsRecord.Spec.Endpoints = getTestNonExistingEndpoints()
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		// should be requeue record for validation after the write attempt
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("AwaitingValidation"),
					"Message":            Equal("Awaiting validation"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// should be increasing write counter
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.WriteCounter).To(BeNumerically(">", int64(0)))
		}, TestTimeoutLong, time.Second).Should(Succeed())
	})

})
