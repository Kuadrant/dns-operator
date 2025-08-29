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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/pkg/builder"
)

var _ = Describe("DNSRecordReconciler", func() {
	// Delegation specific test cases
	Describe("Delegation", Labels{"delegation"}, func() {
		var (
			// buffer containing all the log entries added during the current spec execution
			logBuffer *gbytes.Buffer

			primaryDNSRecord *v1alpha1.DNSRecord

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
			logBuffer = gbytes.NewBuffer()
			GinkgoWriter.TeeTo(logBuffer)

			testNamespace = generateTestNamespaceName()

			By(fmt.Sprintf("creating '%s' test namespace on the primary", testNamespace))
			CreateNamespace(testNamespace, primaryK8sClient)

			testZoneDomainName = strings.Join([]string{GenerateName(), "example.com"}, ".")
			testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")
			// In memory provider currently uses the same value for domain and id
			// Issue here to change this https://github.com/Kuadrant/dns-operator/issues/208
			testZoneID = testZoneDomainName

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on the primary", testNamespace))
			dnsProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor(testZoneDomainName).
				Build()
			Expect(primaryK8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())

			primaryDNSRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName:          testHostname,
							Targets:          []string{"127.0.0.1"},
							RecordType:       "A",
							RecordTTL:        60,
							Labels:           nil,
							ProviderSpecific: nil,
						},
					},
					Delegate: true,
				},
			}
		})

		It("should default to false if not specified", func(ctx SpecContext) {
			By("creating a dnsrecord with no delegate field")
			primaryDNSRecord.Spec = v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: &v1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				},
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			}
			By("verifying created record has delegating=false")
			Expect(primaryK8sClient.Create(ctx, primaryDNSRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primaryDNSRecord), primaryDNSRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(primaryDNSRecord.IsDelegating()).To(BeFalse())
				g.Expect(primaryDNSRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
					})),
				)
				g.Expect(primaryDNSRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should create authoritative record on the primary for delegating record on the primary", Labels{"primary"}, func(ctx SpecContext) {
			var authRecord *v1alpha1.DNSRecord

			By("creating delegating dnsrecord on the primary")
			Expect(primaryK8sClient.Create(ctx, primaryDNSRecord)).To(Succeed())

			By("verifying the status of the primary record")
			Eventually(func(g Gomega) {
				// Find the record
				g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primaryDNSRecord), primaryDNSRecord)).To(Succeed())
				// Verify the expected state of the record
				g.Expect(primaryDNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(primaryDNSRecord.Generation),
					})),
				)
				g.Expect(primaryDNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				g.Expect(primaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
				g.Expect(primaryDNSRecord.IsDelegating()).To(BeTrue())
				g.Expect(primaryDNSRecord.Status.DomainOwners).To(ConsistOf(primaryDNSRecord.GetUIDHash()))
				g.Expect(primaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the authoritative record exists and has the correct spec and status")
			Eventually(func(g Gomega) {
				// Find the authoritative record
				authRecords := &v1alpha1.DNSRecordList{}
				g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.DelegationAuthoritativeRecordLabel: common.HashRootHost(testHostname)})).To(Succeed())
				g.Expect(authRecords.Items).To(HaveLen(1))
				authRecord = &authRecords.Items[0]

				// Verify the expected state of the authoritative record
				g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("delegation-authoritative-record-%s", common.HashRootHost(testHostname))))
				g.Expect(authRecord.IsDelegating()).To(BeFalse())
				g.Expect(authRecord.Spec.RootHost).To(Equal(testHostname))
				// no default secret yet
				g.Expect(authRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
				g.Expect(authRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal("DNSProviderError"),
						"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
					})),
				)
				g.Expect(authRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
					})),
				)
				//authoritative record should contain the expected endpoint and registry record
				g.Expect(authRecord.Spec.Endpoints).To(HaveLen(2))
				g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal(testHostname),
						"Targets":    ConsistOf("127.0.0.1"),
						"RecordType": Equal("A"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    HaveSuffix(testHostname),
						"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType": Equal("TXT"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
					})),
				))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the primary record status references the authoritative record")
			// Verify record status has authoritative record referenced
			Expect(primaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
			Expect(primaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

			By(fmt.Sprintf("setting the inmemory dns provider as the default in the '%s' test namespace", testNamespace))
			// Set the default-provider label on the provider secret
			labels := dnsProviderSecret.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[v1alpha1.DefaultProviderSecretLabel] = "true"
			dnsProviderSecret.SetLabels(labels)
			Expect(primaryK8sClient.Update(ctx, dnsProviderSecret)).To(Succeed())

			By("verifying authoritative record becomes ready")
			Eventually(func(g Gomega) {
				// Get the authoritative record
				g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
				// Verify the authoritative record becomes ready
				g.Expect(authRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal("ProviderSuccess"),
						"Message": Equal("Provider ensured the dns record"),
					})),
				)
				// Verify the authoritative record has the expected provider label
				g.Expect(authRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		Context("with secondary", Labels{"multicluster"}, func() {
			var (
				secondaryDNSRecord     *v1alpha1.DNSRecord
				secondaryClusterSecret *v1.Secret
			)

			BeforeEach(func() {
				By("creating kubeconfig secret for secondary cluster on the primary")
				secondaryClusterSecret = &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secondary-cluster-1",
						Namespace: testDefaultClusterSecretNamespace,
						Labels: map[string]string{
							testDefaultClusterSecretLabel: "true",
						},
					},
					StringData: map[string]string{
						"kubeconfig": string(secondaryKubeconfig),
					},
				}
				createClusterKubeconfigSecret(primaryK8sClient, secondaryClusterSecret, logBuffer)

				By(fmt.Sprintf("creating '%s' test namespace on the secondary", testNamespace))
				CreateNamespace(testNamespace, secondaryK8sClient)

				secondaryDNSRecord = &v1alpha1.DNSRecord{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testHostname,
						Namespace: testNamespace,
					},
					Spec: v1alpha1.DNSRecordSpec{
						RootHost: testHostname,
						Endpoints: []*externaldnsendpoint.Endpoint{
							{
								DNSName:          testHostname,
								Targets:          []string{"127.0.0.2"},
								RecordType:       "A",
								RecordTTL:        60,
								Labels:           nil,
								ProviderSpecific: nil,
							},
						},
						Delegate: true,
					},
				}
			})

			AfterEach(func() {
				if secondaryClusterSecret != nil {
					Expect(client.IgnoreNotFound(primaryK8sClient.Delete(ctx, secondaryClusterSecret))).To(Succeed())
				}
				GinkgoWriter.ClearTeeWriters()
			})

			It("should add the correct status to a secondary cluster record that is not delegating", Labels{"secondary"}, func(ctx SpecContext) {
				By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on the secondary", testNamespace))
				dnsProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
					For(v1alpha1.SecretTypeKuadrantInmemory).
					WithZonesInitialisedFor(testZoneDomainName).
					Build()
				Expect(secondaryK8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())

				By("creating non delegating dnsrecord on the secondary")
				secondaryDNSRecord.Spec = v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ProviderRef: &v1alpha1.ProviderRef{
						Name: dnsProviderSecret.Name,
					},
					Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
					Delegate:  false,
				}
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
						})),
					)
					g.Expect(secondaryDNSRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					g.Expect(secondaryDNSRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
					g.Expect(secondaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(secondaryDNSRecord.Status.WriteCounter).To(BeZero())
					g.Expect(secondaryDNSRecord.Status.ZoneID).To(Equal(testZoneID))
					g.Expect(secondaryDNSRecord.Status.ZoneDomainName).To(Equal(testZoneDomainName))
					g.Expect(secondaryDNSRecord.Status.DomainOwners).To(ConsistOf(secondaryDNSRecord.GetUIDHash()))
				}, TestTimeoutMedium, time.Second).Should(Succeed())
			})

			It("primary cluster should skip reconciliation of a secondary cluster record that is not delegating", Labels{"primary", "secondary"}, func(ctx SpecContext) {
				secondaryDNSRecord.Spec = v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ProviderRef: &v1alpha1.ProviderRef{
						Name: dnsProviderSecret.Name,
					},
					Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
					Delegate:  false,
				}

				By("creating non delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying primary cluster skips the reconciliation of the secondary record")
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary.dnsrecord_controller\".+\"msg\":\"skipping reconciliation of remote record that is not delegating\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
			})

			It("should create authoritative record on the primary for delegating record on the secondary", Labels{"primary", "secondary"}, func(ctx SpecContext) {
				var authRecord *v1alpha1.DNSRecord

				By("creating delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying the status of the secondary record")
				Eventually(func(g Gomega) {
					// Find the record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					// Verify the expected state of the record
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
						})),
					)
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(secondaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(secondaryDNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(secondaryDNSRecord.Status.DomainOwners).To(ConsistOf(secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the authoritative record exists and has the correct spec and status")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.DelegationAuthoritativeRecordLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("delegation-authoritative-record-%s", common.HashRootHost(testHostname))))
					g.Expect(authRecord.IsDelegating()).To(BeFalse())
					g.Expect(authRecord.Spec.RootHost).To(Equal(testHostname))
					// no default secret yet
					g.Expect(authRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					g.Expect(authRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal("DNSProviderError"),
							"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
						})),
					)
					g.Expect(authRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(2))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the secondary record status references the authoritative record")
				// Verify record status has authoritative record referenced
				Expect(secondaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))
			})

			It("should create authoritative record on primary for delegating records on both the primary and secondary", Labels{"primary", "secondary"}, func(ctx SpecContext) {
				var authRecord *v1alpha1.DNSRecord

				By("creating delegating dnsrecord on the primary")
				Expect(primaryK8sClient.Create(ctx, primaryDNSRecord)).To(Succeed())

				By("creating delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying the status of the primary and secondary records")
				Eventually(func(g Gomega) {
					// Find the primary record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primaryDNSRecord), primaryDNSRecord)).To(Succeed())
					// Find the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					// Verify the expected state of the primary record
					g.Expect(primaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primaryDNSRecord.Generation),
						})),
					)
					g.Expect(primaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primaryDNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primaryDNSRecord.Status.OwnerID).To(Equal(primaryDNSRecord.GetUIDHash()))
					g.Expect(primaryDNSRecord.Status.DomainOwners).To(ConsistOf(primaryDNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(primaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))

					// Verify the expected state of the secondary record
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
						})),
					)
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(secondaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(secondaryDNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(secondaryDNSRecord.Status.OwnerID).To(Equal(secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Status.DomainOwners).To(ConsistOf(primaryDNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
				}, TestTimeoutLong, time.Second).Should(Succeed())

				By("verifying the authoritative record exists and has the correct spec and status")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.DelegationAuthoritativeRecordLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("delegation-authoritative-record-%s", common.HashRootHost(testHostname))))
					g.Expect(authRecord.IsDelegating()).To(BeFalse())
					g.Expect(authRecord.Spec.RootHost).To(Equal(testHostname))
					// no default secret yet
					g.Expect(authRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					g.Expect(authRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal("DNSProviderError"),
							"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
						})),
					)
					g.Expect(authRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(3))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.0.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary record status references the authoritative record")
				// Verify the primary record status has the authoritative record referenced
				Expect(primaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(primaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

				By("verifying the secondary record status references the authoritative record")
				// Verify the secondary record status has the authoritative record referenced
				Expect(secondaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))
			})

			// More comprehensive test that covers the following:
			// - auth record is created correctly and updated by both the primary and secondary delegating records
			// - auth record becomes ready when a default provider is added
			// - auth record is updated correctly on delegating record endpoint updates
			// - auth record is updated correctly on delegating record endpoint additions
			// - auth record is updated correctly on delegating record endpoint deletions
			// - auth record is updated correctly on delegating record deletion
			It("should handle create, update and deletion of delegating records", Labels{"primary", "secondary"}, func(ctx SpecContext) {
				var authRecord *v1alpha1.DNSRecord

				By("creating delegating dnsrecord on the primary")
				Expect(primaryK8sClient.Create(ctx, primaryDNSRecord)).To(Succeed())

				By("creating delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying the status of the primary and secondary records")
				Eventually(func(g Gomega) {
					// Find the primary record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primaryDNSRecord), primaryDNSRecord)).To(Succeed())
					// Find the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					// Verify the expected state of the primary record
					g.Expect(primaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primaryDNSRecord.Generation),
						})),
					)
					g.Expect(primaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primaryDNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primaryDNSRecord.Status.OwnerID).To(Equal(primaryDNSRecord.GetUIDHash()))
					g.Expect(primaryDNSRecord.Status.DomainOwners).To(ConsistOf(primaryDNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(primaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))

					// Verify the expected state of the secondary record
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
						})),
					)
					g.Expect(secondaryDNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(secondaryDNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(secondaryDNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(secondaryDNSRecord.Status.OwnerID).To(Equal(secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Status.DomainOwners).To(ConsistOf(primaryDNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
				}, TestTimeoutLong, time.Second).Should(Succeed())

				By("verifying an authoritative record exists for the test host")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.DelegationAuthoritativeRecordLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the authoritative has the correct spec and status")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("delegation-authoritative-record-%s", common.HashRootHost(testHostname))))
					g.Expect(authRecord.IsDelegating()).To(BeFalse())
					g.Expect(authRecord.Spec.RootHost).To(Equal(testHostname))
					// no default secret yet
					g.Expect(authRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					g.Expect(authRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal("DNSProviderError"),
							"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
						})),
					)
					g.Expect(authRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					g.Expect(authRecord.Status.OwnerID).To(Equal(authRecord.GetUIDHash()))
					//ToDo What should DomainOwners be on the authRecord?
					//g.Expect(authRecord.Status.DomainOwners).To(ConsistOf(authRecord.GetUIDHash()))
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(3))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.0.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary record status references the authoritative record")
				// Verify the primary record status has the authoritative record referenced
				Expect(primaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(primaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

				By("verifying the secondary record status references the authoritative record")
				// Verify the secondary record status has the authoritative record referenced
				Expect(secondaryDNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

				By(fmt.Sprintf("setting the inmemory dns provider as the default in the primary clusters '%s' test namespace", testNamespace))
				// Set the default-provider label on the provider secret
				labels := dnsProviderSecret.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels[v1alpha1.DefaultProviderSecretLabel] = "true"
				dnsProviderSecret.SetLabels(labels)
				Expect(primaryK8sClient.Update(ctx, dnsProviderSecret)).To(Succeed())

				By("verifying authoritative record becomes ready")
				Eventually(func(g Gomega) {
					// Get the authoritative record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					// Verify the authoritative record becomes ready
					g.Expect(authRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal("ProviderSuccess"),
							"Message": Equal("Provider ensured the dns record"),
						})),
					)
					// Verify the authoritative record has the expected provider label
					g.Expect(authRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("updating existing endpoint and adding additional endpoint to secondary record")
				Eventually(func(g Gomega) {
					// refresh the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					secondaryDNSRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{
						{
							DNSName:          testHostname,
							Targets:          []string{"127.0.1.2"},
							RecordType:       "A",
							RecordTTL:        60,
							Labels:           nil,
							ProviderSpecific: nil,
						},
						{
							DNSName:          "cname." + testHostname,
							Targets:          []string{testHostname},
							RecordType:       "CNAME",
							RecordTTL:        120,
							Labels:           nil,
							ProviderSpecific: nil,
						},
					}
					g.Expect(secondaryK8sClient.Update(ctx, secondaryDNSRecord)).To(Succeed())
				}, TestTimeoutShort, time.Second).Should(Succeed())

				By("verifying the authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(5))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.1.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal("cname." + testHostname),
							"Targets":    ConsistOf(testHostname),
							"RecordType": Equal("CNAME"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(120)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix("cname." + testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting endpoint from the secondary record")
				Eventually(func(g Gomega) {
					// refresh the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					secondaryDNSRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{
						{
							DNSName:          testHostname,
							Targets:          []string{"127.0.1.2"},
							RecordType:       "A",
							RecordTTL:        60,
							Labels:           nil,
							ProviderSpecific: nil,
						},
					}
					g.Expect(secondaryK8sClient.Update(ctx, secondaryDNSRecord)).To(Succeed())
				}, TestTimeoutShort, time.Second).Should(Succeed())

				By("verifying the authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(5))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.1.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + secondaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting the secondary record")
				Expect(secondaryK8sClient.Delete(ctx, secondaryDNSRecord)).To(Succeed())
				// both clusters should eventually see the delete event
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"secondary.dnsrecord_controller\".+\"msg\":\"Deleting DNSRecord\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary.dnsrecord_controller\".+\"msg\":\"Deleting DNSRecord\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				// primary should eventually say it's removed the records from the zone
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary.dnsrecord_controller\".+\"msg\":\"Deleted DNSRecord in zone\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				// secondary should eventually say it removed the finalizer, primary should not
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"secondary.dnsrecord_controller\".+\"msg\":\"Removing Finalizer\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				Consistently(logBuffer, TestTimeoutShort).Should(Not(gbytes.Say(fmt.Sprintf("\"logger\":\"primary.dnsrecord_controller\".+\"msg\":\"Removing Finalizer\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace))))
				// secondary record should be removed
				Eventually(func(g Gomega) {
					err := secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

				By("verifying the authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(authRecord.Spec.Endpoints).To(HaveLen(2))
					g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primaryDNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting the primary record")
				Expect(primaryK8sClient.Delete(ctx, primaryDNSRecord)).To(Succeed())
				// primary record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primaryDNSRecord), primaryDNSRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

				By("verifying the authoritative record endpoints are empty")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					//authoritative record should contain no endpoints
					g.Expect(authRecord.Spec.Endpoints).To(BeEmpty())
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting the authoritative record")
				Expect(primaryK8sClient.Delete(ctx, authRecord)).To(Succeed())
				// primary record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
			})
		})
	})

})
