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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"
)

var _ = Describe("DNSRecordReconciler", func() {
	var (
		dnsRecord         *v1alpha1.DNSRecord
		dnsRecord2        *v1alpha1.DNSRecord
		dnsProviderSecret *v1.Secret
		testNamespace     string
		// testZoneDomainName generated domain for this test e.g. xyz.example.com
		testZoneDomainName string
		// testZoneID generated zoneID for this test. Currently, the same as testZoneDomainName for inmemory provider.
		testZoneID string
		// testHostname generated host for this test e.g. foo.xyz.example.com
		testHostname string
	)

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		testZoneDomainName = strings.Join([]string{GenerateName(), "example.com"}, ".")
		testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")
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
				Name:      testHostname,
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			},
		}
	})

	// Test cases covering validation of the DNSRecord resource fields
	Context("validation", func() {
		It("should error with no providerRef", func(ctx SpecContext) {
			testHostname = strings.Join([]string{"bar", testZoneDomainName}, ".")
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost:    testHostname,
					Endpoints:   getTestEndpoints(testHostname, []string{"127.0.0.1"}),
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
			testHostname = strings.Join([]string{"bar", testZoneDomainName}, ".")
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					ProviderRef: v1alpha1.ProviderRef{
						Name: dnsProviderSecret.Name,
					},
					Endpoints:   getTestEndpoints(testHostname, []string{"127.0.0.1"}),
					HealthCheck: nil,
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			// as above
			// The error in this case when created via the json openapi would actually be `spec.providerRef: Required value`
			Expect(err).To(MatchError(ContainSubstring("spec.rootHost in body should be at least 1 chars long")))
		})

		It("prevents updating rootHost", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

			//Does not allow rootHost to change once set
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())

				dnsRecord.Spec.RootHost = "bar.example.com"
				err = k8sClient.Update(ctx, dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("RootHost is immutable")))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
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
					Endpoints: getTestEndpoints("bar.example.com", []string{"127.0.0.1"}),
					HealthCheck: &v1alpha1.HealthCheckSpec{
						Path:             "health",
						Port:             5,
						Protocol:         v1alpha1.Protocol("cat"),
						FailureThreshold: -1,
					},
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).To(MatchError(ContainSubstring("spec.rootHost: Invalid value")))
			Expect(err).To(MatchError(ContainSubstring("spec.healthCheck.path: Invalid value")))
			Expect(err).To(MatchError(ContainSubstring("Only ports 80, 443, 1024-49151 are allowed")))
			Expect(err).To(MatchError(ContainSubstring("Only HTTP or HTTPS protocols are allowed")))
			Expect(err).To(MatchError(ContainSubstring("Failure threshold must be greater than 0")))
		})
	})

	It("handles records with similar root hosts", func(ctx SpecContext) {
		//Create default test dnsrecord with root host e.g. foo.xyz.example.com
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
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash()))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		//Create another dnsrecord with a similar root host e.g. bar.foo.xyz.example.com
		testHostname2 := strings.Join([]string{"bar", testHostname}, ".")
		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testHostname2,
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname2,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints:   getTestEndpoints(testHostname2, []string{"127.0.0.1"}),
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
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		err := k8sClient.Delete(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("not found")))
		}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash()))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should use dnsrecord UID for ownerID if none set in spec and not allow it to be updated after", func() {
		//Create default test dnsrecord (foo.xyz.example.com)
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash()))
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
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash()))

		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should allow ownerID to be set explicitly and not allow it to be updated after", func() {
		dnsRecord.Spec.OwnerID = "owner1"
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf("owner1"))
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
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf("owner1"))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should report related endpoints correctly", func() {
		// This will come in play only for the lb strategy
		// in this test I simulate 3 possible scenarios using hand-made simple endpoints
		// scenarios:
		// 1. Record A in a subdomain of record B. Record B should have endpoints of record A and record B
		// 2. Record A and record B share domain. Endpoints should be in Spec.ZoneEndpoints as they will be in the Spec.Endpoints
		// 3. Record A and record B does not share domain in the zone. They should not have each other's endpoints

		// record for testHostname
		dnsRecord1 := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-1",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			},
		}

		// record for sub.testHostname
		dnsRecord2 := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: "sub." + testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints("sub."+testHostname, []string{"127.0.0.1"}),
			},
		}

		// record for testHostname
		dnsRecord3 := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-3",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			},
		}

		// record for testHostname2
		testHostname2 := strings.Join([]string{"bar", testZoneDomainName}, ".")
		dnsRecord4 := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-4",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname2,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname2, []string{"127.0.0.1"}),
			},
		}

		// create all records
		Expect(k8sClient.Create(ctx, dnsRecord1)).To(Succeed())
		Expect(k8sClient.Create(ctx, dnsRecord2)).To(Succeed())
		Expect(k8sClient.Create(ctx, dnsRecord3)).To(Succeed())
		Expect(k8sClient.Create(ctx, dnsRecord4)).To(Succeed())

		// check first record to have EP from second record and not have EPs from third
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord3), dnsRecord3)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord4), dnsRecord4)).To(Succeed())

			g.Expect(dnsRecord1.Status.ZoneEndpoints).ToNot(BeNil())

			// Scenario 1
			// endpoints from the record2 should be present in zone EPs as record2 in subdomain of record 1 rootDomain
			// record must have it's own endpoints (that are identical to the record3 endpoints)
			g.Expect(dnsRecord1.Status.ZoneEndpoints).To(And(
				ContainElements(dnsRecord2.Status.Endpoints),
				ContainElements(dnsRecord1.Status.Endpoints)))
			// record1 and 3 share root domain - all of the above should also apply to this record
			g.Expect(dnsRecord3.Status.ZoneEndpoints).To(And(
				ContainElements(dnsRecord2.Status.Endpoints),
				ContainElements(dnsRecord3.Status.Endpoints)))

			// Scenario 2
			// endpoints from the third record should be present in ZoneEndpoints as it is in the same rootDomain
			g.Expect(dnsRecord1.Status.ZoneEndpoints).To(ContainElements(dnsRecord3.Status.Endpoints))
			// the same true to record 3 as well
			g.Expect(dnsRecord3.Status.ZoneEndpoints).To(ContainElements(dnsRecord1.Status.Endpoints))
			// also check equality of status.Endpoints
			g.Expect(dnsRecord1.Status.Endpoints).To(ConsistOf(dnsRecord3.Status.Endpoints))

			// Scenario 3
			// endpoints from the forth record should not be present as record 4 have unique rootHosts
			g.Expect(dnsRecord1.Status.ZoneEndpoints).ToNot(ContainElements(dnsRecord4.Status.Endpoints))
			g.Expect(dnsRecord2.Status.ZoneEndpoints).ToNot(ContainElements(dnsRecord4.Status.Endpoints))
			g.Expect(dnsRecord3.Status.ZoneEndpoints).ToNot(ContainElements(dnsRecord4.Status.Endpoints))

		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should detect a conflict and the resolution of a conflict", func() {
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-1",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			},
		}
		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-record-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.2"}),
			},
		}

		By("creating dnsrecord " + dnsRecord.Name + " with endpoint dnsName: " + testHostname + " and target: `127.0.0.1`")
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash()))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("creating dnsrecord " + dnsRecord2.Name + " with endpoint dnsName: " + testHostname + " and target: `127.0.0.2`")
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
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash(), dnsRecord2.GetUIDHash()))

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
			g.Expect(dnsRecord2.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash(), dnsRecord2.GetUIDHash()))
		}, TestTimeoutLong, time.Second).Should(Succeed())

		By("fixing conflict with dnsrecord " + dnsRecord2.Name + " with endpoint dnsName: " + testHostname + " and target: `127.0.0.1`")
		Eventually(func(g Gomega) {
			// refresh the second record CR
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
			g.Expect(err).NotTo(HaveOccurred())
			dnsRecord2.Spec.Endpoints = getTestEndpoints(testHostname, []string{"127.0.0.1"})
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
			g.Expect(dnsRecord.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash(), dnsRecord2.GetUIDHash()))

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
			g.Expect(dnsRecord2.Status.DomainOwners).To(ConsistOf(dnsRecord.GetUIDHash(), dnsRecord2.GetUIDHash()))
		}, TestTimeoutLong, time.Second).Should(Succeed())
	})

	It("should not allow second record to change the type", func() {
		//Create default test dnsrecord (foo.xyz.example.com)
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo.example.com-2",
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName: testHostname,
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
					"Message":            ContainSubstring(fmt.Sprintf("record type conflict, cannot update endpoint '%s' with record type 'CNAME' when endpoint already exists with record type 'A'", testHostname)),
					"ObservedGeneration": Equal(dnsRecord2.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("should not allow a record to have a target that matches the root host if an endpoint doesn't exist for the target dns name", func() {
		//Create default test dnsrecord (foo.xyz.example.com)
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
			g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID))
			g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// e.g. bar.zyz.example.com
		testHostname2 := strings.Join([]string{"bar", testZoneDomainName}, ".")
		// e.g. foo.bar.zyz.example.com
		testHostname3 := strings.Join([]string{"foo", testHostname2}, ".")

		dnsRecord2 = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testHostname2,
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testHostname2,
				ProviderRef: v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName: testHostname2,
						Targets: []string{
							testHostname3,
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
					"Message":            ContainSubstring(fmt.Sprintf("invalid target, endpoint '%s' has target '%s' that matches the root host filters '[%s]' but does not exist in the list of local or remote endpoints", testHostname2, testHostname3, testHostname2)),
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

		It("should assign the most suitable zone for the provider", func(ctx SpecContext) {
			// initialize two zones e.g xyz.example.com and foo.xyz.example.com
			testZoneDomainName2 := strings.Join([]string{"foo", testZoneDomainName}, ".")
			// testHostname better matches the new zone (bar.foo.xyz.example.com)
			testHostname2 := strings.Join([]string{"bar", testZoneDomainName2}, ".")
			pSecret = pBuilder.
				WithZonesInitialisedFor(testZoneDomainName, testZoneDomainName2).
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname2,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname2,
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints(testHostname2, []string{"127.0.0.1"}),
				},
			}
			Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneDomainName2))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName2))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should report an error when no suitable zone can be found for the provider", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor(testZoneDomainName).
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
					Endpoints: getTestEndpoints("foo.noexist.com", []string{"127.0.0.1"}),
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

		It("should report an error when an apex domain is used", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor(testZoneDomainName).
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testZoneDomainName,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testZoneDomainName,
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints(testZoneDomainName, []string{"127.0.0.1"}),
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
						"Message":            ContainSubstring("is an apex domain"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should update broken record when provider is updated", func(ctx SpecContext) {
			pSecret = pBuilder.
				WithZonesInitialisedFor(testZoneDomainName).
				Build()
			Expect(k8sClient.Create(ctx, pSecret)).To(Succeed())

			testZoneDomainName2 := strings.Join([]string{GenerateName(), "otherdomain.com"}, ".")
			testZoneID2 := testZoneDomainName2
			testHostname2 := strings.Join([]string{"foo", testZoneDomainName2}, ".")

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname2,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname2,
					ProviderRef: v1alpha1.ProviderRef{
						Name: pSecret.Name,
					},
					Endpoints: getTestEndpoints(testHostname2, []string{"127.0.0.1"}),
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
						"Message":            Equal(fmt.Sprintf("Unable to find suitable zone in provider: no valid zone found for host: %s", testHostname2)),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			pSecretUpdate := builder.NewProviderBuilder("inmemory-credentials-2", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor(testZoneDomainName, testZoneDomainName2).
				Build()

			// Update the provider secrets init zones to include the other domain, now matches for record with root host `foo.example.com`
			pSecret.StringData = pSecretUpdate.StringData
			Expect(k8sClient.Update(ctx, pSecret)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.ZoneID).To(Equal(testZoneID2))
				g.Expect(dnsRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName2))
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

	})

})
