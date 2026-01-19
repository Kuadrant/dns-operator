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
	"github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/types"
)

var _ = Describe("DNSRecordReconciler with Groups", func() {
	var (
		groupsCtx    context.Context
		groupsCancel context.CancelFunc

		// Shared mock TXT resolver for all environments
		mockTXTResolver *MockTXTResolver

		// cluster 1 Group 1
		cluster1Group1Env       *envtest.Environment
		cluster1Group1Manager   ctrl.Manager
		cluster1Group1K8sClient client.Client

		// cluster 1 Group 2
		cluster1Group2Env       *envtest.Environment
		cluster1Group2Manager   ctrl.Manager
		cluster1Group2K8sClient client.Client

		// Ungrouped Primary (only primary cluster in the test setup)
		ungroupedPrimaryEnv       *envtest.Environment
		ungroupedPrimaryManager   ctrl.Manager
		ungroupedPrimaryK8sClient client.Client
		ungroupedPrimaryClusterID string
	)

	// confirmRecordTargets returns a function that verifies the authoritative record
	// contains exactly the expected targets for the given hostname
	confirmRecordTargets := func(ctx context.Context, k8sClient client.Client, namespace, hostname string, expectedTargets []string) func(g Gomega) {
		return func(g Gomega) {
			var targets []string

			authRecordList := &v1alpha1.DNSRecordList{}
			g.Expect(k8sClient.List(ctx, authRecordList, client.InNamespace(namespace), client.MatchingLabels{
				v1alpha1.AuthoritativeRecordLabel: "true",
			})).To(Succeed())

			g.Expect(len(authRecordList.Items)).To(Equal(1))

			authRecord := authRecordList.Items[0]

			g.Expect(authRecord.Spec.RootHost).To(Equal(hostname))

			for _, endpoint := range authRecord.Spec.Endpoints {
				if endpoint.RecordType == "CNAME" && endpoint.DNSName == hostname {
					targets = append(targets, endpoint.Targets...)
				}
			}

			g.Expect(targets).To(ConsistOf(expectedTargets), fmt.Sprintf("Expected targets: %v", expectedTargets))
		}
	}

	BeforeEach(func() {
		var err error
		groupsCtx, groupsCancel = context.WithCancel(ctx)

		// Create a shared mock TXT resolver for all environments
		By("creating shared mock TXT resolver")
		mockTXTResolver = NewMockTXTResolver()

		By("setting up cluster1 group1 environment")
		cluster1Group1Env, cluster1Group1Manager = setupEnv(DelegationRoleSecondary, 1, "group1", mockTXTResolver)
		cluster1Group1K8sClient = cluster1Group1Manager.GetClient()

		By("setting up cluster1 group2 environment")
		cluster1Group2Env, cluster1Group2Manager = setupEnv(DelegationRoleSecondary, 3, "group2", mockTXTResolver)
		cluster1Group2K8sClient = cluster1Group2Manager.GetClient()

		By("setting up ungrouped primary environment")
		ungroupedPrimaryEnv, ungroupedPrimaryManager = setupEnv(DelegationRolePrimary, 5, "", mockTXTResolver)
		ungroupedPrimaryK8sClient = ungroupedPrimaryManager.GetClient()

		// Start all managers
		go func() {
			defer GinkgoRecover()
			err := cluster1Group1Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := cluster1Group2Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := ungroupedPrimaryManager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		By(fmt.Sprintf("creating namespace '%s' on ungrouped primary", testDefaultClusterSecretNamespace))
		CreateNamespace(testDefaultClusterSecretNamespace, ungroupedPrimaryK8sClient)

		k8sConfigs := map[string]string{}
		// Create kubeconfig users for each cluster
		By("creating user 'kuadrant' in cluster1 group1")
		k8sConfigs["cluster1-group1"] = string(createKuadrantUser(cluster1Group1Env))
		Expect(k8sConfigs["cluster1-group1"]).ToNot(BeEmpty())

		By("creating user 'kuadrant' in cluster1 group2")
		k8sConfigs["cluster1-group2"] = string(createKuadrantUser(cluster1Group2Env))
		Expect(k8sConfigs["cluster1-group2"]).ToNot(BeEmpty())

		// Create cluster connection secrets on primary cluster
		By("creating cluster connection secrets on ungrouped primary")
		for name, secret := range k8sConfigs {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testDefaultClusterSecretNamespace,
					Labels: map[string]string{
						testDefaultClusterSecretLabel: "true",
					},
				},
				StringData: map[string]string{
					"kubeconfig": secret,
				},
			}
			Expect(ungroupedPrimaryK8sClient.Create(ctx, secret)).To(Succeed())
		}

		ungroupedPrimaryClusterID, err = getKubeSystemUID(ctx, ungroupedPrimaryK8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(ungroupedPrimaryClusterID).ToNot(BeEmpty())

	})

	AfterEach(func() {
		By("tearing down the group test environments")
		groupsCancel()

		if cluster1Group1Env != nil {
			err := cluster1Group1Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if cluster1Group2Env != nil {
			err := cluster1Group2Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if ungroupedPrimaryEnv != nil {
			err := ungroupedPrimaryEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Describe("Groups", Labels{"groups"}, func() {
		var (
			logBuffer *gbytes.Buffer

			testNamespace      string
			testZoneDomainName string
			testHostname       string
		)

		BeforeEach(func() {
			logBuffer = gbytes.NewBuffer()
			GinkgoWriter.TeeTo(logBuffer)

			testNamespace = generateTestNamespaceName()
			testZoneDomainName = strings.Join([]string{GenerateName(), "example.com"}, ".")
			testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")

			// Create test namespaces on all clusters
			By(fmt.Sprintf("creating '%s' test namespace on cluster1 group1", testNamespace))
			CreateNamespace(testNamespace, cluster1Group1K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on cluster1 group2", testNamespace))
			CreateNamespace(testNamespace, cluster1Group2K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on ungrouped primary", testNamespace))
			CreateNamespace(testNamespace, ungroupedPrimaryK8sClient)

			// Create DNS provider secret on the primary cluster only
			// All secondary clusters use delegation and don't need provider secrets
			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on ungrouped primary", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, ungroupedPrimaryK8sClient)
		})

		AfterEach(func(ctx SpecContext) {
			GinkgoWriter.ClearTeeWriters()
		})

		It("should handle group transitions and record lifecycle", Labels{"groups"}, func(ctx SpecContext) {
			// Create DNS records for all clusters
			cluster1Group1DNSRecord := createDNSRecord(testHostname+"-cluster1-group1", testNamespace, testHostname, "cluster1-group1.example.com")
			cluster1Group2DNSRecord := createDNSRecord(testHostname+"-cluster1-group2", testNamespace, testHostname, "cluster1-group2.example.com")
			ungroupedDNSRecord := createDNSRecord(testHostname+"-ungrouped", testNamespace, testHostname, "cluster-ungrouped.example.com")

			By("creating DNSRecords on all clusters")
			Expect(cluster1Group1K8sClient.Create(ctx, cluster1Group1DNSRecord)).To(Succeed())
			Expect(cluster1Group2K8sClient.Create(ctx, cluster1Group2DNSRecord)).To(Succeed())
			Expect(ungroupedPrimaryK8sClient.Create(ctx, ungroupedDNSRecord)).To(Succeed())

			// Step 1: No active groups - only ungrouped should be published
			By("STEP 1: verifying only ungrouped records are published when no active groups are set")
			Eventually(func(g Gomega) {
				g.Expect(ungroupedPrimaryK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				// Ungrouped record should NOT have an Active condition
				g.Expect(ungroupedDNSRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeActive)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying grouped records have Active=False when no groups are active")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group1DNSRecord), cluster1Group1DNSRecord)).To(Succeed())
				g.Expect(cluster1Group1DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElements(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
						"Reason": Equal(string(v1alpha1.ConditionReasonNotInActiveGroup)),
					}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal(string(v1alpha1.ConditionReasonInInactiveGroup)),
						})),
				)

				g.Expect(cluster1Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group2DNSRecord), cluster1Group2DNSRecord)).To(Succeed())
				g.Expect(cluster1Group2DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElements(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
						"Reason": Equal(string(v1alpha1.ConditionReasonNotInActiveGroup)),
					}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal(string(v1alpha1.ConditionReasonInInactiveGroup)),
						})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			Eventually(confirmRecordTargets(ctx, ungroupedPrimaryK8sClient, testNamespace, testHostname, []string{"cluster-ungrouped.example.com"}), TestTimeoutMedium, time.Second*5).Should(Succeed())

			// Step 2: Set active group to group1 - group1 + ungrouped should be published
			By("STEP 2: setting group1 as the active group")
			setActiveGroupsInDNS(testZoneDomainName, types.Groups{types.Group("group1")}, mockTXTResolver)
			setActiveGroupsInDNS(testHostname, types.Groups{types.Group("group1")}, mockTXTResolver)

			By("waiting for group1 DNSRecord to be ready")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group1DNSRecord), cluster1Group1DNSRecord)).To(Succeed())
				g.Expect(cluster1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				g.Expect(cluster1Group1DNSRecord.Status.Group).To(Equal(types.Group("group1")))
				// group1 should have Active=True since it's in the active groups
				g.Expect(cluster1Group1DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
						"Reason": Equal(string(v1alpha1.ConditionReasonInActiveGroup)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying group2 has Active=False and ungrouped has no Active condition")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group2DNSRecord), cluster1Group2DNSRecord)).To(Succeed())
				g.Expect(cluster1Group2DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElements(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
						"Reason": Equal(string(v1alpha1.ConditionReasonNotInActiveGroup)),
					}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal(string(v1alpha1.ConditionReasonInInactiveGroup)),
						})),
				)

				g.Expect(ungroupedPrimaryK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeActive)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying group1 and ungrouped records are published via authoritative records")
			Eventually(confirmRecordTargets(ctx, ungroupedPrimaryK8sClient, testNamespace, testHostname, []string{"cluster1-group1.example.com", "cluster-ungrouped.example.com"}), TestTimeoutMedium, time.Second*5).Should(Succeed())

			// Step 3: Set active groups to group1 and group2 - all three should be published
			By("STEP 3: setting both group1 and group2 as active groups")
			setActiveGroupsInDNS(testZoneDomainName, types.Groups{types.Group("group1"), types.Group("group2")}, mockTXTResolver)
			setActiveGroupsInDNS(testHostname, types.Groups{types.Group("group1"), types.Group("group2")}, mockTXTResolver)

			By("waiting for group2 DNSRecord to be ready")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group2DNSRecord), cluster1Group2DNSRecord)).To(Succeed())
				g.Expect(cluster1Group2DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				g.Expect(cluster1Group2DNSRecord.Status.Group).To(Equal(types.Group("group2")))
				// group2 should have Active=True since it's now in the active groups
				g.Expect(cluster1Group2DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
						"Reason": Equal(string(v1alpha1.ConditionReasonInActiveGroup)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying both group1 and group2 have Active=True and ungrouped has no Active condition")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group1DNSRecord), cluster1Group1DNSRecord)).To(Succeed())
				g.Expect(cluster1Group1DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
						"Reason": Equal(string(v1alpha1.ConditionReasonInActiveGroup)),
					})),
				)

				g.Expect(ungroupedPrimaryK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeActive)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying group1, group2, and ungrouped records are published via authoritative records")
			Eventually(confirmRecordTargets(ctx, ungroupedPrimaryK8sClient, testNamespace, testHostname, []string{"cluster1-group1.example.com", "cluster1-group2.example.com", "cluster-ungrouped.example.com"}), TestTimeoutMedium, time.Second*5).Should(Succeed())

			// Step 4: Remove group1 (only group2 active) - group2 + ungrouped should be published
			By("STEP 4: removing group1 from active groups (only group2 active)")
			setActiveGroupsInDNS(testZoneDomainName, types.Groups{types.Group("group2")}, mockTXTResolver)
			setActiveGroupsInDNS(testHostname, types.Groups{types.Group("group2")}, mockTXTResolver)

			By("verifying group1 has Active=False, group2 has Active=True, and ungrouped has no Active condition")
			Eventually(func(g Gomega) {
				g.Expect(cluster1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group1DNSRecord), cluster1Group1DNSRecord)).To(Succeed())
				g.Expect(cluster1Group1DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElements(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
						"Reason": Equal(string(v1alpha1.ConditionReasonNotInActiveGroup)),
					}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal(string(v1alpha1.ConditionReasonInInactiveGroup)),
						})),
				)

				g.Expect(cluster1Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(cluster1Group2DNSRecord), cluster1Group2DNSRecord)).To(Succeed())
				g.Expect(cluster1Group2DNSRecord.Status.GetRemoteRecordStatus(ungroupedPrimaryClusterID).Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
						"Reason": Equal(string(v1alpha1.ConditionReasonInActiveGroup)),
					})),
				)

				g.Expect(ungroupedPrimaryK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeActive)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("verifying group2 and ungrouped records are published via authoritative records")
			Eventually(confirmRecordTargets(ctx, ungroupedPrimaryK8sClient, testNamespace, testHostname, []string{"cluster1-group2.example.com", "cluster-ungrouped.example.com"}), TestTimeoutMedium, time.Second*5).Should(Succeed())

			// Step 5: Delete all records
			By("STEP 5: deleting all DNSRecords")
			Expect(cluster1Group1K8sClient.Delete(ctx, cluster1Group1DNSRecord)).To(Succeed())
			Expect(cluster1Group2K8sClient.Delete(ctx, cluster1Group2DNSRecord)).To(Succeed())
			Expect(ungroupedPrimaryK8sClient.Delete(ctx, ungroupedDNSRecord)).To(Succeed())

			By("confirming all DNSRecords are removed")
			Eventually(func(g Gomega) {
				recordList := &v1alpha1.DNSRecordList{}
				g.Expect(cluster1Group1K8sClient.List(ctx, recordList, client.InNamespace(testNamespace))).To(Succeed())
				g.Expect(len(recordList.Items)).To(Equal(0))

				g.Expect(cluster1Group2K8sClient.List(ctx, recordList, client.InNamespace(testNamespace))).To(Succeed())
				g.Expect(len(recordList.Items)).To(Equal(0))

				g.Expect(ungroupedPrimaryK8sClient.List(ctx, recordList, client.InNamespace(testNamespace))).To(Succeed())
				g.Expect(len(recordList.Items)).To(Equal(0))

			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("clearing active groups")
			setActiveGroupsInDNS(testZoneDomainName, types.Groups{}, mockTXTResolver)
			setActiveGroupsInDNS(testHostname, types.Groups{}, mockTXTResolver)
		})
	})
})
