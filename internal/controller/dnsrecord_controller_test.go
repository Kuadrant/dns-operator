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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"
)

var _ = Describe("DNSRecordReconciler", func() {
	var dnsRecord *v1alpha1.DNSRecord
	var dnsRecord2 *v1alpha1.DNSRecord
	var dnsProviderSecret *v1.Secret
	var testNamespace string
	var testZoneDomainName string
	var testZoneID string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		testZoneDomainName = "example.com"
		// In memory provider currently uses the same value for domain and id
		// Issue here to change this https://github.com/Kuadrant/dns-operator/issues/208
		testZoneID = testZoneDomainName

		dnsProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
			For(v1alpha1.SecretTypeKuadrantInmemory).
			WithZonesInitialisedFor(testZoneDomainName).
			Build()
		Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())

		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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
			err := k8sClient.Delete(ctx, dnsRecord2)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if dnsProviderSecret != nil {
			err := k8sClient.Delete(ctx, dnsProviderSecret)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	// Test cases covering validation of the DNSRecord resource fields
	Context("validation", func() {
		It("should error with no providerRef", func(ctx SpecContext) {
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost:    "bar.example.com",
					Endpoints:   getTestEndpoints("bar.example.com", "127.0.0.1"),
					HealthCheck: nil,
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			// It doesn't seem to be possible to have a field marked as required and include the omitempty json struct tag
			// so even though we don't include the providerRef in the test an empty one is being added.
			// The error in this case when created via the json openapi would actually be `spec.providerRef: Required value`
			Expect(err).To(MatchError(ContainSubstring("spec.providerRef.name in body should be at least 1 chars long")))
		})

		It("should error with no rootHost", func(ctx SpecContext) {
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					ProviderRef: v1alpha1.ProviderRef{
						Name: dnsProviderSecret.Name,
					},
					Endpoints:   getTestEndpoints("bar.example.com", "127.0.0.1"),
					HealthCheck: nil,
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			// as above
			// The error in this case when created via the json openapi would actually be `spec.providerRef: Required value`
			Expect(err).To(MatchError(ContainSubstring("spec.rootHost in body should be at least 1 chars long")))
		})

		It("prevents creation of invalid records", func(ctx SpecContext) {
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "bar.example .com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: dnsProviderSecret.Name,
					},
					Endpoints: getTestEndpoints("bar.example.com", "127.0.0.1"),
					HealthCheck: &v1alpha1.HealthCheckSpec{
						Endpoint:         "health",
						Port:             ptr.To(5),
						Protocol:         ptr.To(v1alpha1.HealthProtocol("cat")),
						FailureThreshold: ptr.To(-1),
					},
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).To(MatchError(ContainSubstring("spec.rootHost: Invalid value")))
			Expect(err).To(MatchError(ContainSubstring("spec.healthCheck.endpoint: Invalid value")))
			Expect(err).To(MatchError(ContainSubstring("Only ports 80, 443, 1024-49151 are allowed")))
			Expect(err).To(MatchError(ContainSubstring("Only HTTP or HTTPS protocols are allowed")))
			Expect(err).To(MatchError(ContainSubstring("Failure threshold must be greater than 0")))
		})
	})

	It("handles records with similar root hosts", func(ctx SpecContext) {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "bar.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints:   getTestEndpoints("bar.example.com", "127.0.0.1"),
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
					"Status":             Equal(metav1.ConditionTrue),
					"Reason":             Equal("ProviderSuccess"),
					"Message":            Equal("Provider ensured the dns record"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.bar.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.bar.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints:   getTestEndpoints("foo.bar.example.com", "127.0.0.2"),
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
					"Status":             Equal(metav1.ConditionTrue),
					"Reason":             Equal("ProviderSuccess"),
					"Message":            Equal("Provider ensured the dns record"),
					"ObservedGeneration": Equal(dnsRecord2.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		Consistently(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.WriteCounter).To(Not(BeNumerically(">", int64(1))))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord2.Status.WriteCounter).To(Not(BeNumerically(">", int64(1))))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("can delete a record with a valid dns provider secret", func(ctx SpecContext) {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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
					"Status":             Equal(metav1.ConditionTrue),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
			g.Expect(dnsRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
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
			g.Expect(dnsRecord.Spec.OwnerID).To(BeEmpty())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))
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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should use dnsrecord UID for ownerID if none set in spec and not allow it to be updated after", func() {
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(BeEmpty())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))
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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Does not allow ownerID to change once set
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))

			dnsRecord.Spec.OwnerID = "foobarbaz"
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).To(MatchError(ContainSubstring("OwnerID can't be set if it was previously unset")))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))

		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should allow ownerID to be set explicitly and not allow it to be updated after", func() {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				OwnerID:  "owner1",
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getDefaultTestEndpoints(),
			},
		}
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Spec.OwnerID).To(Equal("owner1"))
			g.Expect(dnsRecord.Status.OwnerID).To(Equal("owner1"))
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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Does not allow ownerID to change once set
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal("owner1"))

			dnsRecord.Spec.OwnerID = "foobarbaz"
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).To(MatchError(ContainSubstring("OwnerID is immutable")))

			dnsRecord.Spec.OwnerID = ""
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).To(MatchError(ContainSubstring("OwnerID can't be unset if it was previously set")))

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.OwnerID).To(Equal("owner1"))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should detect a conflict and the resolution of a conflict", func() {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-1",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
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
			dnsRecord2.Spec.Endpoints = getDefaultTestEndpoints()
			Expect(k8sClient.Update(ctx, dnsRecord2)).To(Succeed())
		}, TestTimeoutShort, time.Second).Should(Succeed())

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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "foo.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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
			g.Expect(dnsRecord2.Status.OwnerID).To(Equal(dnsRecord2.GetUIDHash()))
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
			g.Expect(dnsRecord.Status.RootHost).To(Equal(dnsRecord.Spec.RootHost))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar.example.com",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "bar.example.com",
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
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

	// DNS Provider configuration specific test cases
	Context("dns provider", func() {

		var pBuilder *builder.ProviderBuilder
		var pSecret *v1.Secret

		BeforeEach(func() {
			pBuilder = builder.NewProviderBuilder("inmemory-credentials-2", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory)
		})

		AfterEach(func() {
			if pSecret != nil {
				err := k8sClient.Delete(ctx, pSecret)
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}
		})

		It("should assign the most suitable zone for the provider", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "foo.example.com", "bar.foo.example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "bar.foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("bar.foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("foo.example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("foo.example.com"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should assign the most suitable zone when domain filters are set on the provider", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "foo.example.com", "bar.foo.example.com").
				WithZoneDomainFilter("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "bar.foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("bar.foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				// Note: filters match the suffix so filter "example.com" still matches "foo.example.com"
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("foo.example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("foo.example.com"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should assign the most suitable zone when id filters are set on the provider", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "foo.example.com", "bar.foo.example.com", "foo.bar.example.com").
				WithZoneIDFilter("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bar.foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "bar.foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("bar.foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				// Note: filters match the suffix so filter "example.com" still matches "foo.example.com"
				// In a normal provider the id would not be equal to the domain and this should filter to a specific zone (example.com)
				// This is a limitation of the in memory provider implementation, issue to address this is here https://github.com/Kuadrant/dns-operator/issues/208
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("foo.example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("foo.example.com"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should report an error when no suitable zone can be found for the provider", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo.noexist.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "foo.noexist.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("foo.noexist.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(BeEmpty())
				g.Expect(dnsRecord.Status.ZoneDomainName).To(BeEmpty())
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionFalse),
						"Reason":             Equal("DNSProviderError"),
						"Message":            Equal("Unable to find suitable zone in provider: no valid zone found for host: foo.noexist.com"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should update broken record when provider is updated", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo.otherdomain.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "foo.otherdomain.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("foo.otherdomain.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(BeEmpty())
				g.Expect(dnsRecord.Status.ZoneDomainName).To(BeEmpty())
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionFalse),
						"Reason":             Equal("DNSProviderError"),
						"Message":            Equal("Unable to find suitable zone in provider: no valid zone found for host: foo.otherdomain.com"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			pSecretUpdate := builder.NewProviderBuilder("inmemory-credentials-2", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("otherdomain.com").
				Build()

			// Update the provider secrets domain filter to match the other domain, now matches for record with root host `foo.example.com`
			pSecret.StringData = pSecretUpdate.StringData
			Expect(k8sClient.Update(ctx, pSecret)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("otherdomain.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("otherdomain.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should assign new zone correctly for the provider when the root host changes", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			// Update the record root host
			dnsRecord.Spec.RootHost = "foo.otherdomain.com"
			dnsRecord.Spec.Endpoints = getTestEndpoints("foo.otherdomain.com", "127.0.0.1")
			Expect(k8sClient.Update(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("otherdomain.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("otherdomain.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		// Test the deletion behaviour when a record was successfully created but the provider becomes incompatible with
		// the existing record. A provider could become incompatible for a number of reasons, invalidated credentials etc.
		// but here we just manipulate the provider secrets domain filter to force it. In this scenario the record should
		// not be removed by the dnsrecord controller until it becomes valid again in order to clean up the provider.
		It("should not allow a record that has updated a provider zone to be deleted when the provider becomes invalid", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			pSecretUpdate := builder.NewProviderBuilder("inmemory-credentials-2", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("otherdomain.com").
				Build()

			// Update the provider secrets domain filter to match the other domain, no longer matches for record with root host `foo.example.com`
			pSecret.StringData = pSecretUpdate.StringData
			Expect(k8sClient.Update(ctx, pSecret)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionFalse),
						"Reason":             Equal("DNSProviderError"),
						"Message":            Equal("The dns provider could not be loaded: zone domain name 'example.com' is not listed in the providers domain filter"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			err := k8sClient.Delete(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			// Should not remove the dns record while its provider is incompatible
			Consistently(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dnsRecord.DeletionTimestamp).ToNot(BeNil())
			}, 10*time.Second, time.Second, ctx).Should(Succeed())

			// Update the provider secrets domain filter to match for `foo.example.com`
			pSecretUpdate = builder.NewProviderBuilder("inmemory-credentials-2", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("example.com").
				Build()
			pSecret.StringData = pSecretUpdate.StringData
			Expect(k8sClient.Update(ctx, pSecret)).To(Succeed())

			// Should remove the dns record now its provider is compatible again
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 5*time.Second, time.Second, ctx).Should(Succeed())
		})

		// Test the deletion behaviour when a record was successfully created but the root host was changed to one that
		// is incompatible with the records' provider.
		// In this scenario the record should be removed by the dnsrecord controller and cleanup the zone for the previous
		// root host.
		It("should allow a record to be deleted when the root host changes but the provider is incompatible with the new root host", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor("example.com", "otherdomain.com").
				WithZoneDomainFilter("example.com").
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo.example.com",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints("foo.example.com", "127.0.0.1"),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			// Update the record root host to one no longer managed by the provider
			dnsRecord.Spec.RootHost = "foo.otherdomain.com"
			dnsRecord.Spec.Endpoints = getTestEndpoints("foo.otherdomain.com", "127.0.0.1")
			Expect(k8sClient.Update(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				// Important that these zone values in the status are not updated
				g.Expect(dnsRecord.Status.ZoneID).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal("example.com"))
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionFalse),
						"Reason":             Equal("DNSProviderError"),
						"Message":            Equal("Unable to find suitable zone in provider: no valid zone found for host: foo.otherdomain.com"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			// Should remove the dns record since it can still cleanup the previous zone
			err := k8sClient.Delete(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 5*time.Second, time.Second, ctx).Should(Succeed())
		})

	})

})
