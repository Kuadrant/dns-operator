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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/types"
)

var _ = Describe("DNSRecordReconciler", func() {
	// Runtime reconfiguration test cases - tests that verify controller behavior when
	// configuration options change at runtime (e.g., pod restart with different flags)
	Describe("Runtime Reconfiguration", Labels{"runtime-reconfiguration"}, func() {
		var (
			testEnv    *envtest.Environment
			restConfig *rest.Config
			k8sClient  client.Client

			stopMgr func()

			mockTXTResolver *MockTXTResolver

			testNamespace      string
			testZoneDomainName string
			testHostname       string
		)

		BeforeEach(func() {
			// Create a single API server that persists across manager restarts
			testEnv, restConfig = createTestEnv()

			mockTXTResolver = NewMockTXTResolver()

			testNamespace = generateTestNamespaceName()
			testZoneDomainName = strings.Join([]string{GenerateName(), "example.com"}, ".")
			testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")
		})

		AfterEach(func() {
			if stopMgr != nil {
				stopMgr()
			}
			if testEnv != nil {
				err := testEnv.Stop()
				Expect(err).NotTo(HaveOccurred())
			}
		})

		Describe("Group Change", Labels{"groups"}, func() {
			// Note: This test only passes because the test suite uses low values for
			// ValidityDuration (2s) and RequeueDuration (2s). With production values
			// (e.g., validFor=14m), the group change would be delayed by recordReceivedPrematurely
			// which skips status updates when the record is still within its validity window.
			// See https://github.com/Kuadrant/dns-operator/issues/664
			It("should update status.group when controller restarts with a different group", func(ctx SpecContext) {
				// Phase 1: Start controller with group1
				By("starting controller with group=group1")
				mgr1 := setupManager(ctx, restConfig, DelegationRoleSecondary, 10, "group1", mockTXTResolver)
				k8sClient = mgr1.GetClient()
				stopMgr = startManager(ctx, mgr1)

				By("creating test namespace")
				CreateNamespace(testNamespace, k8sClient)

				By("creating inmemory dns provider secret")
				createDefaultDNSProviderSecret(ctx, testNamespace, testZoneDomainName, k8sClient)

				By("setting group1 as active in DNS")
				setActiveGroupsInDNS(testZoneDomainName, types.Groups{types.Group("group1")}, mockTXTResolver)

				By("creating a DNSRecord")
				dnsRecord := &v1alpha1.DNSRecord{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testHostname,
						Namespace: testNamespace,
					},
					Spec: v1alpha1.DNSRecordSpec{
						RootHost: testHostname,
						Endpoints: []*externaldnsendpoint.Endpoint{
							{
								DNSName:    testHostname,
								Targets:    []string{"127.0.0.1"},
								RecordType: "A",
								RecordTTL:  60,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

				By("waiting for DNSRecord to be reconciled with group=group1")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
					g.Expect(dnsRecord.Status.Group).To(Equal(types.Group("group1")))
					g.Expect(dnsRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				// Phase 2: Stop the controller, restart with group2
				By("stopping the group1 controller")
				stopMgr()

				By("setting group2 as active in DNS")
				setActiveGroupsInDNS(testZoneDomainName, types.Groups{types.Group("group2")}, mockTXTResolver)

				By("starting controller with group=group2")
				mgr2 := setupManager(ctx, restConfig, DelegationRoleSecondary, 11, "group2", mockTXTResolver)
				k8sClient = mgr2.GetClient()
				stopMgr = startManager(ctx, mgr2)

				By("waiting for DNSRecord status.group to change to group2")
				Eventually(func(g Gomega) {
					updatedRecord := &v1alpha1.DNSRecord{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), updatedRecord)).To(Succeed())
					g.Expect(updatedRecord.Status.Group).To(Equal(types.Group("group2")))
					g.Expect(updatedRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
				}, TestTimeoutMedium, time.Second).Should(Succeed())

				// Clean up
				By("deleting the DNSRecord")
				Expect(k8sClient.Delete(ctx, dnsRecord)).To(Succeed())

				Eventually(func(g Gomega) {
					recordList := &v1alpha1.DNSRecordList{}
					g.Expect(k8sClient.List(ctx, recordList, client.InNamespace(testNamespace))).To(Succeed())
					g.Expect(recordList.Items).To(BeEmpty())
				}, TestTimeoutMedium, time.Second).Should(Succeed())
			})
		})
	})
})
