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
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

var _ = Describe("DNSRecordReconciler", func() {
	var dnsRecord *v1alpha1.DNSRecord
	var dnsRecord2 *v1alpha1.DNSRecord
	var dnsProviderSecret *v1.Secret
	var managedZone *v1alpha1.ManagedZone
	var brokenZone *v1alpha1.ManagedZone
	var testNamespace string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		dnsProviderSecret = testBuildInMemoryCredentialsSecret("inmemory-credentials", testNamespace)
		managedZone = testBuildManagedZone("mz-example-com", testNamespace, "example.com", dnsProviderSecret.Name)
		brokenZone = testBuildManagedZone("mz-fix-com", testNamespace, "fix.com", "not-there")

		Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())
		Expect(k8sClient.Create(ctx, brokenZone)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(managedZone.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionTrue),
					"ObservedGeneration": Equal(managedZone.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(brokenZone), brokenZone)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(brokenZone.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"ObservedGeneration": Equal(brokenZone.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "foo.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: getDefaultTestEndpoints(),
			},
		}
	})

	AfterEach(func() {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if dnsRecord2 != nil {
			err := k8sClient.Delete(ctx, dnsRecord)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if managedZone != nil {
			err := k8sClient.Delete(ctx, managedZone)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if brokenZone != nil {
			err := k8sClient.Delete(ctx, brokenZone)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	It("dns records are reconciled once zone is fixed", func(ctx SpecContext) {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.fix.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "foo.fix.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: brokenZone.Name,
				},
				Endpoints:   getTestEndpoints("foo.fix.com", "127.0.0.1"),
				HealthCheck: nil,
			},
		}
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("ProviderError"),
					"Message":            ContainSubstring("The DNS provider failed to ensure the record"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
			g.Expect(dnsRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		fixedZone := brokenZone.DeepCopy()
		fixedZone.Spec.SecretRef.Name = dnsProviderSecret.Name
		Expect(k8sClient.Update(ctx, fixedZone)).NotTo(HaveOccurred())

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
	})

	It("can delete a record with an invalid managed zone", func(ctx SpecContext) {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "foo.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: "doesnotexist",
				},
				Endpoints:   getDefaultTestEndpoints(),
				HealthCheck: nil,
			},
		}
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("ProviderError"),
					"Message":            Equal("The DNS provider failed to ensure the record: ManagedZone.kuadrant.io \"doesnotexist\" not found"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
			g.Expect(dnsRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		err := k8sClient.Delete(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("not found")))
		}, 5*time.Second, time.Second, ctx).Should(Succeed())
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
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Does not allow ownerID to change once set
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(Equal("owner1"))

			dnsRecord.Spec.OwnerID = "foobarbaz"
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).To(MatchError(ContainSubstring("OwnerID is immutable")))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(Equal("owner1"))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should detect a conflict and the resolution of a conflict", func() {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-1",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "foo.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: getDefaultTestEndpoints(),
			},
		}
		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner2",
				RootHost: "foo.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: getTestEndpoints("foo.example.com", "127.0.0.2"),
			},
		}

		By("creating dnsrecord " + dnsRecord.Name + " with endpoint dnsName: `foo.example.com` and target: `127.0.0.1`")
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		By("checking dnsrecord " + dnsRecord.Name + " becomes ready")
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

		By("creating dnsrecord " + dnsRecord2.Name + " with endpoint dnsName: `foo.example.com` and target: `127.0.0.2`")
		Expect(k8sClient.Create(ctx, dnsRecord2)).To(Succeed())

		By("checking dnsrecord " + dnsRecord.Name + " and " + dnsRecord2.Name + " conflict")
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
			g.Expect(dnsRecord.Status.WriteCounter).To(BeNumerically(">", int64(1)))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("AwaitingValidation"),
					"Message":            Equal("Awaiting validation"),
					"ObservedGeneration": Equal(dnsRecord2.Generation),
				})),
			)
			g.Expect(dnsRecord2.Status.WriteCounter).To(BeNumerically(">", int64(1)))
		}, TestTimeoutLong, time.Second).Should(Succeed())

		By("fixing conflict with dnsrecord " + dnsRecord2.Name + " with endpoint dnsName: `foo.example.com` and target: `127.0.0.1`")
		Eventually(func(g Gomega) {
			// refresh the second record CR
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutShort, time.Second).Should(Succeed())
		dnsRecord2.Spec.Endpoints = getDefaultTestEndpoints()
		Expect(k8sClient.Update(ctx, dnsRecord2)).To(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionTrue),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("Provider ensured the dns record"),
				})),
			)

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionTrue),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("Provider ensured the dns record"),
				})),
			)
		}, TestTimeoutLong, time.Second).Should(Succeed())
	})

	It("should not allow second record to change the type", func() {
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
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner2",
				RootHost: "foo.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName: "foo.example.com",
						Targets: []string{
							"v1",
						},
						RecordType:       "CNAME",
						SetIdentifier:    "foo",
						RecordTTL:        60,
						Labels:           nil,
						ProviderSpecific: nil,
					},
				},
				HealthCheck: nil,
			},
		}
		Expect(k8sClient.Create(ctx, dnsRecord2)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("ProviderError"),
					"Message":            ContainSubstring("record type conflict, cannot update endpoint 'foo.example.com' with record type 'CNAME' when endpoint already exists with record type 'A'"),
					"ObservedGeneration": Equal(dnsRecord2.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should not allow a record to have a target that matches the root host if an endpoint doesn't exist for the target dns name", func() {
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
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "bar.example.com",
				ManagedZoneRef: &v1alpha1.ManagedZoneReference{
					Name: managedZone.Name,
				},
				Endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName: "bar.example.com",
						Targets: []string{
							"foo.bar.example.com",
						},
						RecordType:       "CNAME",
						SetIdentifier:    "",
						RecordTTL:        60,
						Labels:           nil,
						ProviderSpecific: nil,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dnsRecord2)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal("ProviderError"),
					"Message":            ContainSubstring("invalid target, endpoint 'bar.example.com' has target 'foo.bar.example.com' that matches the root host filters '[bar.example.com]' but does not exist in the list of local or remote endpoints"),
					"ObservedGeneration": Equal(dnsRecord2.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

})
