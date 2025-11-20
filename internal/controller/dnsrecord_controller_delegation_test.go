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

			primary1DNSRecord *v1alpha1.DNSRecord

			primary1DNSProviderSecret *v1.Secret

			testNamespace string
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

			By(fmt.Sprintf("creating '%s' test namespace on primary-1", testNamespace))
			CreateNamespace(testNamespace, primaryK8sClient)

			testZoneDomainName = strings.Join([]string{GenerateName(), "example.com"}, ".")
			testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")
			// In memory provider currently uses the same value for domain and id
			// Issue here to change this https://github.com/Kuadrant/dns-operator/issues/208
			testZoneID = testZoneDomainName

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on primary-1", testNamespace))
			primary1DNSProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
				For(v1alpha1.SecretTypeKuadrantInmemory).
				WithZonesInitialisedFor(testZoneDomainName).
				Build()
			Expect(primaryK8sClient.Create(ctx, primary1DNSProviderSecret)).To(Succeed())

			primary1DNSRecord = &v1alpha1.DNSRecord{
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
			primary1DNSRecord.Spec = v1alpha1.DNSRecordSpec{
				RootHost: testHostname,
				ProviderRef: &v1alpha1.ProviderRef{
					Name: primary1DNSProviderSecret.Name,
				},
				Endpoints: NewTestEndpoints(testHostname).Endpoints(),
			}
			By("verifying created record has delegating=false")
			Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(primary1DNSRecord.IsDelegating()).To(BeFalse())
				g.Expect(primary1DNSRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
					})),
				)
				g.Expect(primary1DNSRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should create authoritative record on the primary for delegating record on the primary", Labels{"primary"}, func(ctx SpecContext) {
			var authRecord *v1alpha1.DNSRecord

			By("creating delegating dnsrecord on the primary")
			Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())

			By("verifying the status of the primary record")
			Eventually(func(g Gomega) {
				// Find the record
				g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)).To(Succeed())
				// Verify the expected state of the record
				g.Expect(primary1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(primary1DNSRecord.Generation),
					})),
				)
				g.Expect(primary1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				g.Expect(primary1DNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
				g.Expect(primary1DNSRecord.IsDelegating()).To(BeTrue())
				g.Expect(primary1DNSRecord.Status.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash()))
				g.Expect(primary1DNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the authoritative record exists and has the correct spec and status")
			Eventually(func(g Gomega) {
				// Find the authoritative record
				authRecords := &v1alpha1.DNSRecordList{}
				g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
				g.Expect(authRecords.Items).To(HaveLen(1))
				authRecord = &authRecords.Items[0]

				// Verify the expected state of the authoritative record
				g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
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
				//domain owners won't be set until the dns provider is set
				g.Expect(authRecord.Status.DomainOwners).To(BeEmpty())
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
						"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType": Equal("TXT"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
					})),
				))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the primary record status references the authoritative record")
			// Verify record status has authoritative record referenced
			Expect(primary1DNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
			Expect(primary1DNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

			By(fmt.Sprintf("setting the inmemory dns provider as the default in the '%s' test namespace", testNamespace))
			// Set the default-provider label on the provider secret
			labels := primary1DNSProviderSecret.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[v1alpha1.DefaultProviderSecretLabel] = "true"
			primary1DNSProviderSecret.SetLabels(labels)
			Expect(primaryK8sClient.Update(ctx, primary1DNSProviderSecret)).To(Succeed())

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
				// Verify the authoritative record has the expected domain owners
				g.Expect(authRecord.Status.DomainOwners).To(ConsistOf(authRecord.GetUIDHash()))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		// The wildcard prefix "*." should be removed for any authoritative record being generated for a delegating record with a wildcard root host.
		// delegatingRecord.Spec.RootHost=*.foo.example.com -> authoritativeRecord.Spec.RootHost=foo.example.com
		It("should handle wildcard root hosts for delegating records", Labels{"primary"}, func(ctx SpecContext) {
			var authRecord *v1alpha1.DNSRecord

			testWildcardHostname := "*." + testHostname
			testFooHostname := "foo." + testHostname

			primary1DNSRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHostname,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testWildcardHostname,
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName:          testWildcardHostname,
							Targets:          []string{"127.0.0.1"},
							RecordType:       "A",
							RecordTTL:        60,
							Labels:           nil,
							ProviderSpecific: nil,
						},
						{
							DNSName:          testFooHostname,
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

			By("creating delegating dnsrecord on the primary")
			Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())

			By("verifying the status of the primary record")
			Eventually(func(g Gomega) {
				// Find the record
				g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)).To(Succeed())
				// Verify the expected state of the record
				g.Expect(primary1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(primary1DNSRecord.Generation),
					})),
				)
				g.Expect(primary1DNSRecord.IsDelegating()).To(BeTrue())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the authoritative record exists with the correct root host and endpoints")
			Eventually(func(g Gomega) {
				// Find the authoritative record
				authRecords := &v1alpha1.DNSRecordList{}
				g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(primary1DNSRecord.GetRootHost())})).To(Succeed())
				g.Expect(authRecords.Items).To(HaveLen(1))
				authRecord = &authRecords.Items[0]

				// Verify the expected state of the authoritative record
				g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(primary1DNSRecord.GetRootHost()))))
				g.Expect(authRecord.IsDelegating()).To(BeFalse())
				g.Expect(authRecord.Spec.RootHost).To(Equal(primary1DNSRecord.GetRootHost()))
				// Auth record Spec.RootHost should not contain the wildcard prefix
				g.Expect(authRecord.Spec.RootHost).ToNot(HavePrefix("*."))
				// authoritative record should contain the expected endpoints
				g.Expect(authRecord.Spec.Endpoints).To(HaveLen(4))
				g.Expect(authRecord.Spec.Endpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal(testWildcardHostname),
						"Targets":    ConsistOf("127.0.0.1"),
						"RecordType": Equal("A"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal(testFooHostname),
						"Targets":    ConsistOf("127.0.0.2"),
						"RecordType": Equal("A"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    HaveSuffix("wildcard." + testHostname),
						"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType": Equal("TXT"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    HaveSuffix("foo." + testHostname),
						"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType": Equal("TXT"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
					})),
				))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying the primary record status references the authoritative record")
			// Verify record status has authoritative record referenced
			Expect(primary1DNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
			Expect(primary1DNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))
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
				primary1DNSProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
					For(v1alpha1.SecretTypeKuadrantInmemory).
					WithZonesInitialisedFor(testZoneDomainName).
					Build()
				Expect(secondaryK8sClient.Create(ctx, primary1DNSProviderSecret)).To(Succeed())

				By("creating non delegating dnsrecord on the secondary")
				secondaryDNSRecord.Spec = v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ProviderRef: &v1alpha1.ProviderRef{
						Name: primary1DNSProviderSecret.Name,
					},
					Endpoints: NewTestEndpoints(testHostname).Endpoints(),
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
						Name: primary1DNSProviderSecret.Name,
					},
					Endpoints: NewTestEndpoints(testHostname).Endpoints(),
					Delegate:  false,
				}

				By("creating non delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying primary cluster skips the reconciliation of the secondary record")
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary-1.remote_dnsrecord_controller\".+\"msg\":\"skipping reconciliation of remote record that is not delegating\".+\"controller\":\"remotednsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
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
					g.Expect(secondaryDNSRecord.Status.OwnerID).To(Equal(secondaryDNSRecord.GetUIDHash()))
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					// Main Status has no zone status
					g.Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
					g.Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
					// Remote record status is set as expected
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveLen(1))
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
					remoteRecordStatus := secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)
					g.Expect(remoteRecordStatus).ToNot(BeNil())
					g.Expect(remoteRecordStatus.Conditions).To(ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
					})))
					g.Expect(remoteRecordStatus.Endpoints).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ObservedGeneration).To(Equal(secondaryDNSRecord.Generation))
					g.Expect(remoteRecordStatus.DomainOwners).To(ConsistOf(secondaryDNSRecord.GetUIDHash()))
					//These values are checked below after we get the auth record
					g.Expect(remoteRecordStatus.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ZoneID).ToNot(BeEmpty())
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the authoritative record exists and has the correct spec and status")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
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
					//domain owners won't be set until the dns provider is set
					g.Expect(authRecord.Status.DomainOwners).To(BeEmpty())
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
				Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
				Expect(secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)).To(MatchFields(IgnoreExtras, Fields{
					"ZoneID":         Equal(authRecord.Name),
					"ZoneDomainName": Equal(authRecord.Spec.RootHost),
				}))
			})

			It("should create authoritative record on primary for delegating records on both the primary and secondary", Labels{"primary", "secondary"}, func(ctx SpecContext) {
				var authRecord *v1alpha1.DNSRecord

				By("creating delegating dnsrecord on the primary")
				Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())

				By("creating delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying the status of the primary and secondary records")
				Eventually(func(g Gomega) {
					// Find the primary record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)).To(Succeed())
					// Find the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					// Verify the expected state of the primary record
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primary1DNSRecord.Generation),
						})),
					)
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primary1DNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primary1DNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primary1DNSRecord.Status.OwnerID).To(Equal(primary1DNSRecord.GetUIDHash()))
					g.Expect(primary1DNSRecord.Status.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(primary1DNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))

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
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					// Main Status has no zone status
					g.Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
					g.Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
					// Remote record status is set as expected
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveLen(1))
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
					remoteRecordStatus := secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)
					g.Expect(remoteRecordStatus).ToNot(BeNil())
					g.Expect(remoteRecordStatus.Conditions).To(ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
					})))
					g.Expect(remoteRecordStatus.Endpoints).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ObservedGeneration).To(Equal(secondaryDNSRecord.Generation))
					g.Expect(remoteRecordStatus.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					//These values are checked below after we get the auth record
					g.Expect(remoteRecordStatus.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ZoneID).ToNot(BeEmpty())
				}, TestTimeoutLong, time.Second).Should(Succeed())

				By("verifying the authoritative record exists and has the correct spec and status")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
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
					//domain owners won't be set until the dns provider is set
					g.Expect(authRecord.Status.DomainOwners).To(BeEmpty())
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
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
				Expect(primary1DNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(primary1DNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

				By("verifying the secondary record status references the authoritative record")
				// Verify the secondary record status has the authoritative record referenced
				Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
				Expect(secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)).To(MatchFields(IgnoreExtras, Fields{
					"ZoneID":         Equal(authRecord.Name),
					"ZoneDomainName": Equal(authRecord.Spec.RootHost),
				}))
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
				Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())

				By("creating delegating dnsrecord on the secondary")
				Expect(secondaryK8sClient.Create(ctx, secondaryDNSRecord)).To(Succeed())

				By("verifying the status of the primary and secondary records")
				Eventually(func(g Gomega) {
					// Find the primary record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)).To(Succeed())
					// Find the secondary record
					g.Expect(secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryDNSRecord), secondaryDNSRecord)).To(Succeed())
					// Verify the expected state of the primary record
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primary1DNSRecord.Generation),
						})),
					)
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primary1DNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primary1DNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primary1DNSRecord.Status.OwnerID).To(Equal(primary1DNSRecord.GetUIDHash()))
					g.Expect(primary1DNSRecord.Status.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					g.Expect(primary1DNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))

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
					g.Expect(secondaryDNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					// Main Status has no zone status
					g.Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
					g.Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
					// Remote record status is set as expected
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveLen(1))
					g.Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
					remoteRecordStatus := secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)
					g.Expect(remoteRecordStatus).ToNot(BeNil())
					g.Expect(remoteRecordStatus.Conditions).To(ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(secondaryDNSRecord.Generation),
					})))
					g.Expect(remoteRecordStatus.Endpoints).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ObservedGeneration).To(Equal(secondaryDNSRecord.Generation))
					g.Expect(remoteRecordStatus.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), secondaryDNSRecord.GetUIDHash()))
					//These values are checked below after we get the auth record
					g.Expect(remoteRecordStatus.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ZoneID).ToNot(BeEmpty())
				}, TestTimeoutLong, time.Second).Should(Succeed())

				By("verifying an authoritative record exists for the test host")
				Eventually(func(g Gomega) {
					// Find the authoritative record on the primary
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					authRecord = &authRecords.Items[0]
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the authoritative has the correct spec and status")
				Eventually(func(g Gomega) {
					// Get the authoritative record on the primary
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)).To(Succeed())
					// Verify the expected state of the authoritative record
					g.Expect(authRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
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
					//domain owners won't be set until the dns provider is set
					g.Expect(authRecord.Status.DomainOwners).To(BeEmpty())
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
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
				Expect(primary1DNSRecord.Status.ZoneID).To(Equal(authRecord.Name))
				Expect(primary1DNSRecord.Status.ZoneDomainName).To(Equal(authRecord.Spec.RootHost))

				By("verifying the secondary record status references the authoritative record")
				// Verify the secondary record status has the authoritative record referenced
				Expect(secondaryDNSRecord.Status.ZoneID).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.ZoneDomainName).To(BeEmpty())
				Expect(secondaryDNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
				Expect(secondaryDNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)).To(MatchFields(IgnoreExtras, Fields{
					"ZoneID":         Equal(authRecord.Name),
					"ZoneDomainName": Equal(authRecord.Spec.RootHost),
				}))

				By(fmt.Sprintf("setting the inmemory dns provider as the default in the primary clusters '%s' test namespace", testNamespace))
				// Set the default-provider label on the provider secret
				labels := primary1DNSProviderSecret.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels[v1alpha1.DefaultProviderSecretLabel] = "true"
				primary1DNSProviderSecret.SetLabels(labels)
				Expect(primaryK8sClient.Update(ctx, primary1DNSProviderSecret)).To(Succeed())

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
					// Verify the authoritative record has the expected domain owners
					g.Expect(authRecord.Status.DomainOwners).To(ConsistOf(authRecord.GetUIDHash()))
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
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
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"secondary-1.dnsrecord_controller\".+\"msg\":\"Deleting DNSRecord\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary-1.remote_dnsrecord_controller\".+\"msg\":\"Deleting DNSRecord\".+\"controller\":\"remotednsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				// primary should eventually say it's removed the records from the zone
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"primary-1.remote_dnsrecord_controller\".+\"msg\":\"Deleted DNSRecord in zone\".+\"controller\":\"remotednsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				// secondary should eventually say it removed the finalizer, primary should not
				Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("\"logger\":\"secondary-1.dnsrecord_controller\".+\"msg\":\"Removing Finalizer\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace)))
				Consistently(logBuffer, TestTimeoutShort).Should(Not(gbytes.Say(fmt.Sprintf("\"logger\":\"primary-1.dnsrecord_controller\".+\"msg\":\"Removing Finalizer\".+\"controller\":\"dnsrecord\".+\"name\":\"%s\".+\"namespace\":\"%s\"", secondaryDNSRecord.Name, secondaryDNSRecord.Namespace))))
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting the primary record")
				Expect(primaryK8sClient.Delete(ctx, primary1DNSRecord)).To(Succeed())
				// primary record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

				By("verifying the primary authoritative record is removed")
				// authoritative record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(authRecord), authRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
			})
		})

		Context("with multiple primaries", Labels{"multicluster"}, func() {
			var (
				primary2DNSRecord         *v1alpha1.DNSRecord
				primary1ClusterSecret     *v1.Secret
				primary2ClusterSecret     *v1.Secret
				primary2DNSProviderSecret *v1.Secret
			)

			BeforeEach(func() {
				By("creating kubeconfig secret for primary-2 on primary-1")
				primary2ClusterSecret = &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "primary-cluster-2",
						Namespace: testDefaultClusterSecretNamespace,
						Labels: map[string]string{
							testDefaultClusterSecretLabel: "true",
						},
					},
					StringData: map[string]string{
						"kubeconfig": string(primary2Kubeconfig),
					},
				}
				createClusterKubeconfigSecret(primaryK8sClient, primary2ClusterSecret, logBuffer)

				By("creating kubeconfig secret for primary-1 on primary-2")
				primary1ClusterSecret = &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "primary-cluster-1",
						Namespace: testDefaultClusterSecretNamespace,
						Labels: map[string]string{
							testDefaultClusterSecretLabel: "true",
						},
					},
					StringData: map[string]string{
						"kubeconfig": string(primaryKubeconfig),
					},
				}
				createClusterKubeconfigSecret(primary2K8sClient, primary1ClusterSecret, logBuffer)

				By(fmt.Sprintf("creating '%s' test namespace on primary-2", testNamespace))
				CreateNamespace(testNamespace, primary2K8sClient)

				By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on primary-2", testNamespace))
				primary2DNSProviderSecret = builder.NewProviderBuilder("inmemory-credentials", testNamespace).
					For(v1alpha1.SecretTypeKuadrantInmemory).
					WithZonesInitialisedFor(testZoneDomainName).
					Build()
				Expect(primary2K8sClient.Create(ctx, primary2DNSProviderSecret)).To(Succeed())

				primary2DNSRecord = &v1alpha1.DNSRecord{
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
				if primary1ClusterSecret != nil {
					Expect(client.IgnoreNotFound(primary2K8sClient.Delete(ctx, primary1ClusterSecret))).To(Succeed())
				}
				if primary2ClusterSecret != nil {
					Expect(client.IgnoreNotFound(primaryK8sClient.Delete(ctx, primary2ClusterSecret))).To(Succeed())
				}
				GinkgoWriter.ClearTeeWriters()
			})

			// Multi Primary test that covers the following:
			// - auth record is created correctly and updated on both primary clusters
			// - auth records become ready when a default provider is added on both primary clusters
			// - auth record is updated correctly on delegating record endpoint updates on both primary clusters
			// - auth record is updated correctly on delegating record endpoint additions on both primary clusters
			// - auth record is updated correctly on delegating record endpoint deletions on both primary clusters
			// - auth record is updated correctly on delegating record deletion on both primary clusters
			// - deletion of records is prevented if a primary cannot update a remote record (disconnected)
			// - auth records update correctly when primary/primary connections are re-established
			It("should handle create, update and deletion of delegating records on multiple primaries", Labels{"primary", "multi-primary"}, func(ctx SpecContext) {
				var primary1AuthRecord, primary2AuthRecord *v1alpha1.DNSRecord

				By("creating delegating dnsrecord on primary-1")
				Expect(primaryK8sClient.Create(ctx, primary1DNSRecord)).To(Succeed())

				By("creating delegating dnsrecord on primary-2")
				Expect(primary2K8sClient.Create(ctx, primary2DNSRecord)).To(Succeed())

				By("verifying the status of primary-1 and primary-2 records")
				Eventually(func(g Gomega) {
					// Find the primary-1 record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)).To(Succeed())
					// Find the primary-2 record
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)).To(Succeed())

					// Verify the expected state of the primary records
					// primary-1(primary1DNSRecord) should have status and remote record status for primary-2(primary2DNSRecord)
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primary1DNSRecord.Generation),
						})),
					)
					g.Expect(primary1DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primary1DNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primary1DNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primary1DNSRecord.Status.OwnerID).To(Equal(primary1DNSRecord.GetUIDHash()))
					// Main Status has zone status set, values are checked below after we get the auth record
					g.Expect(primary1DNSRecord.Status.ZoneID).ToNot(BeEmpty())
					g.Expect(primary1DNSRecord.Status.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(primary1DNSRecord.Status.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), primary2DNSRecord.GetUIDHash()))
					g.Expect(primary1DNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					// Remote record status is set as expected for primary-2 record
					g.Expect(primary1DNSRecord.Status.RemoteRecordStatuses).To(HaveLen(1))
					g.Expect(primary1DNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary2ClusterID))
					remoteRecordStatus := primary1DNSRecord.Status.GetRemoteRecordStatus(primary2ClusterID)
					g.Expect(remoteRecordStatus).ToNot(BeNil())
					g.Expect(remoteRecordStatus.Conditions).To(ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(primary1DNSRecord.Generation),
					})))
					g.Expect(remoteRecordStatus.Endpoints).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ObservedGeneration).To(Equal(primary1DNSRecord.Generation))
					g.Expect(remoteRecordStatus.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), primary2DNSRecord.GetUIDHash()))
					//These values are checked below after we get the auth record
					g.Expect(remoteRecordStatus.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ZoneID).ToNot(BeEmpty())

					// primary-2(primary2DNSRecord) should have status and remote record status for primary-1(primary1DNSRecord)
					g.Expect(primary2DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":             Equal(metav1.ConditionTrue),
							"Reason":             Equal("ProviderSuccess"),
							"Message":            Equal("Provider ensured the dns record"),
							"ObservedGeneration": Equal(primary2DNSRecord.Generation),
						})),
					)
					g.Expect(primary2DNSRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					g.Expect(primary2DNSRecord.Finalizers).To(ContainElement(DNSRecordFinalizer))
					g.Expect(primary2DNSRecord.IsDelegating()).To(BeTrue())
					g.Expect(primary2DNSRecord.Status.OwnerID).To(Equal(primary2DNSRecord.GetUIDHash()))
					// Main Status has zone status set, values are checked below after we get the auth record
					g.Expect(primary2DNSRecord.Status.ZoneID).ToNot(BeEmpty())
					g.Expect(primary2DNSRecord.Status.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(primary2DNSRecord.Status.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), primary2DNSRecord.GetUIDHash()))
					g.Expect(primary2DNSRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					// Remote record status is set as expected for primary-1 record
					g.Expect(primary2DNSRecord.Status.RemoteRecordStatuses).To(HaveLen(1))
					g.Expect(primary2DNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
					remoteRecordStatus = primary2DNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)
					g.Expect(remoteRecordStatus).ToNot(BeNil())
					g.Expect(remoteRecordStatus.Conditions).To(ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"Message":            Equal("Provider ensured the dns record"),
						"ObservedGeneration": Equal(primary2DNSRecord.Generation),
					})))
					g.Expect(remoteRecordStatus.Endpoints).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ObservedGeneration).To(Equal(primary2DNSRecord.Generation))
					g.Expect(remoteRecordStatus.DomainOwners).To(ConsistOf(primary1DNSRecord.GetUIDHash(), primary2DNSRecord.GetUIDHash()))
					//These values are checked below after we get the auth record
					g.Expect(remoteRecordStatus.ZoneDomainName).ToNot(BeEmpty())
					g.Expect(remoteRecordStatus.ZoneID).ToNot(BeEmpty())
				}, TestTimeoutLong, time.Second).Should(Succeed())

				By("verifying an authoritative record exists for the test host on both primary-1 and primary-2")
				Eventually(func(g Gomega) {
					// Find the authoritative record on primary-1
					authRecords := &v1alpha1.DNSRecordList{}
					g.Expect(primaryK8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					primary1AuthRecord = &authRecords.Items[0]

					// Find the authoritative record on primary-2
					g.Expect(primary2K8sClient.List(ctx, authRecords, client.InNamespace(testNamespace), client.MatchingLabels{v1alpha1.AuthoritativeRecordLabel: "true", v1alpha1.AuthoritativeRecordHashLabel: common.HashRootHost(testHostname)})).To(Succeed())
					g.Expect(authRecords.Items).To(HaveLen(1))
					primary2AuthRecord = &authRecords.Items[0]
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the authoritative records have the correct spec and status")
				Eventually(func(g Gomega) {
					// Get the authoritative record on primary-1
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)).To(Succeed())
					// Verify the expected state of the authoritative record
					g.Expect(primary1AuthRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
					g.Expect(primary1AuthRecord.IsDelegating()).To(BeFalse())
					g.Expect(primary1AuthRecord.Spec.RootHost).To(Equal(testHostname))
					// no default secret yet
					g.Expect(primary1AuthRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					g.Expect(primary1AuthRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal("DNSProviderError"),
							"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
						})),
					)
					g.Expect(primary1AuthRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					g.Expect(primary1AuthRecord.Status.OwnerID).To(Equal(primary1AuthRecord.GetUIDHash()))
					//domain owners won't be set until the dns provider is set
					g.Expect(primary1AuthRecord.Status.DomainOwners).To(BeEmpty())
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(HaveLen(3))
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.0.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))

					// Get the authoritative record on primary-2
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)).To(Succeed())
					// Verify the expected state of the authoritative record
					g.Expect(primary2AuthRecord.Name).To(Equal(fmt.Sprintf("authoritative-record-%s", common.HashRootHost(testHostname))))
					g.Expect(primary2AuthRecord.IsDelegating()).To(BeFalse())
					g.Expect(primary2AuthRecord.Spec.RootHost).To(Equal(testHostname))
					// no default secret yet
					g.Expect(primary2AuthRecord.Labels).Should(Not(HaveKey("kuadrant.io/dns-provider-name")))
					g.Expect(primary2AuthRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal("DNSProviderError"),
							"Message": Equal("No default provider secret labeled kuadrant.io/default-provider was found"),
						})),
					)
					g.Expect(primary2AuthRecord.Status.Conditions).ToNot(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type": Equal(string(v1alpha1.ConditionTypeReadyForDelegation)),
						})),
					)
					g.Expect(primary2AuthRecord.Status.OwnerID).To(Equal(primary2AuthRecord.GetUIDHash()))
					//domain owners won't be set until the dns provider is set
					g.Expect(primary2AuthRecord.Status.DomainOwners).To(BeEmpty())
					//authoritative record should contain the expected endpoint and registry record
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(HaveLen(3))
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.0.0.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary-1 record status references the primary-1 authoritative record")
				// Verify the primary records have the authoritative record referenced
				Expect(primary1DNSRecord.Status.ZoneID).To(Equal(primary1AuthRecord.Name))
				Expect(primary1DNSRecord.Status.ZoneDomainName).To(Equal(primary1AuthRecord.Spec.RootHost))
				Expect(primary1DNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary2ClusterID))
				Expect(primary1DNSRecord.Status.GetRemoteRecordStatus(primary2ClusterID)).To(MatchFields(IgnoreExtras, Fields{
					"ZoneID":         Equal(primary2AuthRecord.Name),
					"ZoneDomainName": Equal(primary2AuthRecord.Spec.RootHost),
				}))

				By("verifying the primary-2 record status references the primary-2 authoritative record")
				// Verify the primary records have the authoritative record referenced
				Expect(primary2DNSRecord.Status.ZoneID).To(Equal(primary2AuthRecord.Name))
				Expect(primary2DNSRecord.Status.ZoneDomainName).To(Equal(primary2AuthRecord.Spec.RootHost))
				Expect(primary2DNSRecord.Status.RemoteRecordStatuses).To(HaveKey(primary1ClusterID))
				Expect(primary2DNSRecord.Status.GetRemoteRecordStatus(primary1ClusterID)).To(MatchFields(IgnoreExtras, Fields{
					"ZoneID":         Equal(primary1AuthRecord.Name),
					"ZoneDomainName": Equal(primary1AuthRecord.Spec.RootHost),
				}))

				By(fmt.Sprintf("setting the inmemory dns provider as the default in the primary-1 clusters '%s' test namespace", testNamespace))
				// Set the default-provider label on the provider secret
				labels := primary1DNSProviderSecret.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels[v1alpha1.DefaultProviderSecretLabel] = "true"
				primary1DNSProviderSecret.SetLabels(labels)
				Expect(primaryK8sClient.Update(ctx, primary1DNSProviderSecret)).To(Succeed())

				By(fmt.Sprintf("setting the inmemory dns provider as the default in the primary-2 clusters '%s' test namespace", testNamespace))
				// Set the default-provider label on the provider secret
				labels = primary2DNSProviderSecret.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels[v1alpha1.DefaultProviderSecretLabel] = "true"
				primary2DNSProviderSecret.SetLabels(labels)
				Expect(primary2K8sClient.Update(ctx, primary2DNSProviderSecret)).To(Succeed())

				By("verifying authoritative records becomes ready")
				Eventually(func(g Gomega) {
					// Get the primary-1 authoritative record
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)).To(Succeed())
					// Verify the primary-1 authoritative record becomes ready
					g.Expect(primary1AuthRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal("ProviderSuccess"),
							"Message": Equal("Provider ensured the dns record"),
						})),
					)
					// Verify the primary-1 authoritative record has the expected provider label
					g.Expect(primary1AuthRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
					// Verify the authoritative record has the expected domain owners
					g.Expect(primary1AuthRecord.Status.DomainOwners).To(ConsistOf(primary1AuthRecord.GetUIDHash(), primary2AuthRecord.GetUIDHash()))

					// Get the primary-2 authoritative record
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)).To(Succeed())
					// Verify the primary-1 authoritative record becomes ready
					g.Expect(primary2AuthRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal("ProviderSuccess"),
							"Message": Equal("Provider ensured the dns record"),
						})),
					)
					// Verify the primary-1 authoritative record has the expected provider label
					g.Expect(primary2AuthRecord.Labels).Should(HaveKeyWithValue("kuadrant.io/dns-provider-name", "inmemory"))
					// Verify the authoritative record has the expected domain owners
					g.Expect(primary2AuthRecord.Status.DomainOwners).To(ConsistOf(primary1AuthRecord.GetUIDHash(), primary2AuthRecord.GetUIDHash()))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("updating existing endpoint and adding additional endpoint to primary-2 record")
				Eventually(func(g Gomega) {
					// refresh the primary-2 record
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)).To(Succeed())
					primary2DNSRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{
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
					g.Expect(primary2K8sClient.Update(ctx, primary2DNSRecord)).To(Succeed())
				}, TestTimeoutShort, time.Second).Should(Succeed())

				By("verifying the primary-1 and primary-2 authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on primary-1
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(HaveLen(5))
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(ContainElements(
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix("cname." + testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))

					// Get the authoritative record on primary-2
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(HaveLen(5))
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(ContainElements(
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix("cname." + testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				// The following is to check that primaries recover correctly if they miss events during disconnection/reconnection
				By("removing the primary-2 cluster secret from the primary-1 cluster")
				deleteClusterKubeconfigSecret(primaryK8sClient, primary2ClusterSecret, logBuffer)

				By("updating existing endpoint and removing endpoint from primary-2 record")
				Eventually(func(g Gomega) {
					// refresh the primary-2 record
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)).To(Succeed())
					primary2DNSRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{
						{
							DNSName:          testHostname,
							Targets:          []string{"127.1.1.2"},
							RecordType:       "A",
							RecordTTL:        60,
							Labels:           nil,
							ProviderSpecific: nil,
						},
					}
					g.Expect(primary2K8sClient.Update(ctx, primary2DNSRecord)).To(Succeed())
				}, TestTimeoutShort, time.Second).Should(Succeed())

				By("verifying the primary-2 authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on primary-2
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(HaveLen(3))
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1", "127.1.1.2"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary-1 authoritative record endpoints are not being updated")
				Consistently(func(g Gomega) {
					// Get the authoritative record on primary-1
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(HaveLen(5))
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(ContainElements(
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
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix("cname." + testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary2DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutShort).Should(Succeed())

				//Check deleting record on primary-2 does not get removed while primary-1 has no access to update it
				By("deleting the primary-2 record")
				Expect(primary2K8sClient.Delete(ctx, primary2DNSRecord)).To(Succeed())
				//  primary-2 record should not get removed, but be marked as deleting
				By("checking primary-2 record is not being removed")
				Eventually(func(g Gomega) {
					err := primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(primary2DNSRecord.IsDeleting()).To(BeTrue())
				}, TestTimeoutShort, time.Second).Should(Succeed())
				Consistently(func(g Gomega) {
					err := primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(primary2DNSRecord.IsDeleting()).To(BeTrue())
				}, TestTimeoutShort).Should(Succeed())

				By("creating kubeconfig secret for primary-2 on primary-1")
				primary2ClusterSecret = &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "primary-cluster-2",
						Namespace: testDefaultClusterSecretNamespace,
						Labels: map[string]string{
							testDefaultClusterSecretLabel: "true",
						},
					},
					StringData: map[string]string{
						"kubeconfig": string(primary2Kubeconfig),
					},
				}
				createClusterKubeconfigSecret(primaryK8sClient, primary2ClusterSecret, logBuffer)

				//  primary-2 record should now get removed
				By("checking primary-2 record gets removed")
				Eventually(func(g Gomega) {
					err := primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2DNSRecord), primary2DNSRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary-2 authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on primary-2
					g.Expect(primary2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(HaveLen(2))
					g.Expect(primary2AuthRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("verifying the primary-1 authoritative record endpoints are updated correctly")
				Eventually(func(g Gomega) {
					// Get the authoritative record on primary-1
					g.Expect(primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)).To(Succeed())
					// authoritative record should contain the expected endpoint and registry record
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(HaveLen(2))
					g.Expect(primary1AuthRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    Equal(testHostname),
							"Targets":    ConsistOf("127.0.0.1"),
							"RecordType": Equal("A"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":    HaveSuffix(testHostname),
							"Targets":    ConsistOf("\"heritage=external-dns,external-dns/owner=" + primary1DNSRecord.Status.OwnerID + ",external-dns/version=1\""),
							"RecordType": Equal("TXT"),
							"RecordTTL":  Equal(externaldnsendpoint.TTL(0)),
						})),
					))
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				By("deleting the primary-1 record")
				Expect(primaryK8sClient.Delete(ctx, primary1DNSRecord)).To(Succeed())
				// primary-1 record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1DNSRecord), primary1DNSRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

				By("verifying the primary-1 and primary-2 authoritative records are removed")
				// primary-1 and primary-2 auth record should be removed
				Eventually(func(g Gomega) {
					err := primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary1AuthRecord), primary1AuthRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))

					err = primaryK8sClient.Get(ctx, client.ObjectKeyFromObject(primary2AuthRecord), primary2AuthRecord)
					g.Expect(err).To(HaveOccurred())
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
			})
		})
	})

})
