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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider/inmemory"
	"github.com/kuadrant/dns-operator/types"
)

var _ = Describe("DNSRecordReconciler with Groups", func() {
	var (
		groupsCtx    context.Context
		groupsCancel context.CancelFunc

		// Primary 1 Group 1
		primary1Group1Env        *envtest.Environment
		primary1Group1Manager    ctrl.Manager
		primary1Group1K8sClient  client.Client
		primary1Group1Kubeconfig []byte

		// Secondary 1 Group 1
		secondary1Group1Env        *envtest.Environment
		secondary1Group1Manager    ctrl.Manager
		secondary1Group1K8sClient  client.Client
		secondary1Group1Kubeconfig []byte

		// Primary 2 Group 2
		primary2Group2Env        *envtest.Environment
		primary2Group2Manager    ctrl.Manager
		primary2Group2K8sClient  client.Client
		primary2Group2Kubeconfig []byte

		// Secondary 2 Group 2
		secondary2Group2Env        *envtest.Environment
		secondary2Group2Manager    ctrl.Manager
		secondary2Group2K8sClient  client.Client
		secondary2Group2Kubeconfig []byte

		// Ungrouped Primary
		ungroupedEnv        *envtest.Environment
		ungroupedManager    ctrl.Manager
		ungroupedK8sClient  client.Client
		ungroupedKubeconfig []byte
	)

	BeforeEach(func() {
		groupsCtx, groupsCancel = context.WithCancel(ctx)

		By("setting up primary-1 group-1 environment")
		primary1Group1Env, primary1Group1Manager = setupGroupEnv(groupsCtx, DelegationRolePrimary, types.Group("group1"), 1)
		primary1Group1K8sClient = primary1Group1Manager.GetClient()

		By("setting up secondary-1 group-1 environment")
		secondary1Group1Env, secondary1Group1Manager = setupGroupEnv(groupsCtx, DelegationRoleSecondary, types.Group("group1"), 1)
		secondary1Group1K8sClient = secondary1Group1Manager.GetClient()

		By("setting up primary-2 group-2 environment")
		primary2Group2Env, primary2Group2Manager = setupGroupEnv(groupsCtx, DelegationRolePrimary, types.Group("group2"), 2)
		primary2Group2K8sClient = primary2Group2Manager.GetClient()

		By("setting up secondary-2 group-2 environment")
		secondary2Group2Env, secondary2Group2Manager = setupGroupEnv(groupsCtx, DelegationRoleSecondary, types.Group("group2"), 2)
		secondary2Group2K8sClient = secondary2Group2Manager.GetClient()

		By("setting up ungrouped secondary environment")
		ungroupedEnv, ungroupedManager = setupGroupEnv(groupsCtx, DelegationRoleSecondary, types.Group(""), 3)
		ungroupedK8sClient = ungroupedManager.GetClient()

		// Start all managers
		go func() {
			defer GinkgoRecover()
			err := primary1Group1Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := secondary1Group1Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := primary2Group2Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := secondary2Group2Manager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		go func() {
			defer GinkgoRecover()
			err := ungroupedManager.Start(groupsCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		// Create multicluster namespaces on primaries
		By(fmt.Sprintf("creating namespace '%s' on primary-1 group-1", testDefaultClusterSecretNamespace))
		CreateNamespace(testDefaultClusterSecretNamespace, primary1Group1K8sClient)

		By(fmt.Sprintf("creating namespace '%s' on primary-2 group-2", testDefaultClusterSecretNamespace))
		CreateNamespace(testDefaultClusterSecretNamespace, primary2Group2K8sClient)

		By(fmt.Sprintf("creating namespace '%s' on ungrouped secondary", testDefaultClusterSecretNamespace))
		CreateNamespace(testDefaultClusterSecretNamespace, ungroupedK8sClient)

		// Create kubeconfig users for each cluster
		By("creating user 'kuadrant' in primary-1 group-1")
		primary1Group1Kubeconfig = createKuadrantUser(primary1Group1Env)
		Expect(primary1Group1Kubeconfig).ToNot(BeEmpty())

		By("creating user 'kuadrant' in secondary-1 group-1")
		secondary1Group1Kubeconfig = createKuadrantUser(secondary1Group1Env)
		Expect(secondary1Group1Kubeconfig).ToNot(BeEmpty())

		By("creating user 'kuadrant' in primary-2 group-2")
		primary2Group2Kubeconfig = createKuadrantUser(primary2Group2Env)
		Expect(primary2Group2Kubeconfig).ToNot(BeEmpty())

		By("creating user 'kuadrant' in secondary-2 group-2")
		secondary2Group2Kubeconfig = createKuadrantUser(secondary2Group2Env)
		Expect(secondary2Group2Kubeconfig).ToNot(BeEmpty())

		By("creating user 'kuadrant' in ungrouped secondary")
		ungroupedKubeconfig = createKuadrantUser(ungroupedEnv)
		Expect(ungroupedKubeconfig).ToNot(BeEmpty())
	})

	AfterEach(func() {
		By("tearing down the group test environments")
		groupsCancel()

		if primary1Group1Env != nil {
			err := primary1Group1Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if secondary1Group1Env != nil {
			err := secondary1Group1Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if primary2Group2Env != nil {
			err := primary2Group2Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if secondary2Group2Env != nil {
			err := secondary2Group2Env.Stop()
			Expect(err).NotTo(HaveOccurred())
		}

		if ungroupedEnv != nil {
			err := ungroupedEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Describe("Groups", Labels{"groups"}, func() {
		var (
			logBuffer *gbytes.Buffer

			// DNSRecords
			primary1Group1DNSRecord   *v1alpha1.DNSRecord
			secondary1Group1DNSRecord *v1alpha1.DNSRecord
			primary2Group2DNSRecord   *v1alpha1.DNSRecord
			secondary2Group2DNSRecord *v1alpha1.DNSRecord
			ungroupedDNSRecord        *v1alpha1.DNSRecord

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
			By(fmt.Sprintf("creating '%s' test namespace on primary-1 group-1", testNamespace))
			CreateNamespace(testNamespace, primary1Group1K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on secondary-1 group-1", testNamespace))
			CreateNamespace(testNamespace, secondary1Group1K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on primary-2 group-2", testNamespace))
			CreateNamespace(testNamespace, primary2Group2K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on secondary-2 group-2", testNamespace))
			CreateNamespace(testNamespace, secondary2Group2K8sClient)

			By(fmt.Sprintf("creating '%s' test namespace on ungrouped secondary", testNamespace))
			CreateNamespace(testNamespace, ungroupedK8sClient)

			// Create DNS provider secrets
			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on primary-1 group-1", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, primary1Group1K8sClient)

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on secondary-1 group-1", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, secondary1Group1K8sClient)

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on primary-2 group-2", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, primary2Group2K8sClient)

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on secondary-2 group-2", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, secondary2Group2K8sClient)

			By(fmt.Sprintf("creating inmemory dns provider in the '%s' test namespace on ungrouped secondary", testNamespace))
			createDefaultDNSProviderSecret(groupsCtx, testNamespace, testZoneDomainName, ungroupedK8sClient)

			// Create DNSRecords
			primary1Group1DNSRecord = createTestDNSRecord(testHostname+"-primary1-group1", testNamespace, testHostname, "127.0.0.1")
			secondary1Group1DNSRecord = createTestDNSRecord(testHostname+"-secondary1-group1", testNamespace, testHostname, "127.0.0.2")
			primary2Group2DNSRecord = createTestDNSRecord(testHostname+"-primary2-group2", testNamespace, testHostname, "127.0.0.3")
			secondary2Group2DNSRecord = createTestDNSRecord(testHostname+"-secondary2-group2", testNamespace, testHostname, "127.0.0.4")
			ungroupedDNSRecord = createTestDNSRecord(testHostname+"-ungrouped", testNamespace, testHostname, "127.0.0.5")
		})

		AfterEach(func() {
			GinkgoWriter.ClearTeeWriters()
		})

		It("should properly set group in status for grouped records", Labels{"groups"}, func(ctx SpecContext) {
			By("creating DNSRecords on all clusters")
			Expect(primary1Group1K8sClient.Create(ctx, primary1Group1DNSRecord)).To(Succeed())
			Expect(secondary1Group1K8sClient.Create(ctx, secondary1Group1DNSRecord)).To(Succeed())
			Expect(primary2Group2K8sClient.Create(ctx, primary2Group2DNSRecord)).To(Succeed())
			Expect(secondary2Group2K8sClient.Create(ctx, secondary2Group2DNSRecord)).To(Succeed())
			Expect(ungroupedK8sClient.Create(ctx, ungroupedDNSRecord)).To(Succeed())

			By("verifying group1 records have group1 set in status")
			Eventually(func(g Gomega) {
				g.Expect(primary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(primary1Group1DNSRecord), primary1Group1DNSRecord)).To(Succeed())
				g.Expect(primary1Group1DNSRecord.Status.Group).To(Equal(types.Group("group1")))
				g.Expect(primary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary1Group1DNSRecord), secondary1Group1DNSRecord)).To(Succeed())
				g.Expect(secondary1Group1DNSRecord.Status.Group).To(Equal(types.Group("group1")))
				g.Expect(secondary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying group2 records have group2 set in status")
			Eventually(func(g Gomega) {
				g.Expect(primary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2Group2DNSRecord), primary2Group2DNSRecord)).To(Succeed())
				g.Expect(primary2Group2DNSRecord.Status.Group).To(Equal(types.Group("group2")))
				g.Expect(primary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary2Group2DNSRecord), secondary2Group2DNSRecord)).To(Succeed())
				g.Expect(secondary2Group2DNSRecord.Status.Group).To(Equal(types.Group("group2")))
				g.Expect(secondary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying ungrouped record has no group set in status")
			Eventually(func(g Gomega) {
				g.Expect(ungroupedK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.Group).To(BeEmpty())
				g.Expect(ungroupedDNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying TXTResolver can read and update active groups")
			resolver := &InMemoryTXTResolver{client: inmemory.GetInMemoryClient()}
			activeGroupsHost := activeGroupsTXTRecordName + "." + testZoneDomainName

			values, err := resolver.LookupTXT(ctx, activeGroupsHost, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(BeEmpty())

			Expect(setActiveGroupsInDNS(ctx, testZoneDomainName, types.Groups{types.Group("group1")})).To(Succeed())
			Eventually(func(g Gomega) {
				values, err := resolver.LookupTXT(ctx, activeGroupsHost, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(values).To(ConsistOf("groups=group1"))
			}, TestTimeoutShort, time.Second).Should(Succeed())

			Expect(setActiveGroupsInDNS(ctx, testZoneDomainName, types.Groups{types.Group("group2")})).To(Succeed())
			Eventually(func(g Gomega) {
				values, err := resolver.LookupTXT(ctx, activeGroupsHost, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(values).To(ConsistOf("groups=group2"))
			}, TestTimeoutShort, time.Second).Should(Succeed())
		})

		It("should update zone records when active group changes", Labels{"groups"}, func(ctx SpecContext) {
			By("creating DNSRecords on all clusters")
			Expect(primary1Group1K8sClient.Create(ctx, primary1Group1DNSRecord)).To(Succeed())
			Expect(secondary1Group1K8sClient.Create(ctx, secondary1Group1DNSRecord)).To(Succeed())
			Expect(primary2Group2K8sClient.Create(ctx, primary2Group2DNSRecord)).To(Succeed())
			Expect(secondary2Group2K8sClient.Create(ctx, secondary2Group2DNSRecord)).To(Succeed())
			Expect(ungroupedK8sClient.Create(ctx, ungroupedDNSRecord)).To(Succeed())

			By("waiting for all DNSRecords to be ready")
			Eventually(func(g Gomega) {
				g.Expect(primary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(primary1Group1DNSRecord), primary1Group1DNSRecord)).To(Succeed())
				g.Expect(primary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary1Group1DNSRecord), secondary1Group1DNSRecord)).To(Succeed())
				g.Expect(secondary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(primary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2Group2DNSRecord), primary2Group2DNSRecord)).To(Succeed())
				g.Expect(primary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary2Group2DNSRecord), secondary2Group2DNSRecord)).To(Succeed())
				g.Expect(secondary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(ungroupedK8sClient.Get(ctx, client.ObjectKeyFromObject(ungroupedDNSRecord), ungroupedDNSRecord)).To(Succeed())
				g.Expect(ungroupedDNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying only ungrouped records are published in zone (no active groups set)")
			memClient := inmemory.GetInMemoryClient()
			Eventually(func(g Gomega) {
				records, err := memClient.Records(testZoneDomainName)
				g.Expect(err).NotTo(HaveOccurred())

				// Filter out TXT records to focus on actual DNS records
				var aRecords []*externaldnsendpoint.Endpoint
				for _, r := range records {
					if r.RecordType == "A" && r.DNSName == testHostname {
						aRecords = append(aRecords, r)
					}
				}

				// Should only have ungrouped (127.0.0.5) - grouped records are inactive
				g.Expect(aRecords).To(HaveLen(1))
				g.Expect(aRecords[0].Targets).To(ConsistOf("127.0.0.5"))
			}, TestTimeoutLong*3, time.Second).Should(Succeed())

			By("setting group1 as the active group")
			Expect(setActiveGroupsInDNS(ctx, testZoneDomainName, types.Groups{types.Group("group1")})).To(Succeed())

			By("waiting for group1 records to show Active condition")
			Eventually(func(g Gomega) {
				g.Expect(primary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(primary1Group1DNSRecord), primary1Group1DNSRecord)).To(Succeed())
				g.Expect(primary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary1Group1DNSRecord), secondary1Group1DNSRecord)).To(Succeed())
				g.Expect(secondary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("waiting for group2 records to show Inactive condition")
			Eventually(func(g Gomega) {
				g.Expect(primary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2Group2DNSRecord), primary2Group2DNSRecord)).To(Succeed())
				g.Expect(primary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
					})),
				)

				g.Expect(secondary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary2Group2DNSRecord), secondary2Group2DNSRecord)).To(Succeed())
				g.Expect(secondary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying zone contains only group1 and ungrouped records")
			// Allow extra time for ungrouped record to reconcile after group change
			Eventually(func(g Gomega) {
				records, err := memClient.Records(testZoneDomainName)
				g.Expect(err).NotTo(HaveOccurred())

				// Filter for A records matching our hostname
				var aRecords []*externaldnsendpoint.Endpoint
				for _, r := range records {
					if r.RecordType == "A" && r.DNSName == testHostname {
						aRecords = append(aRecords, r)
					}
				}

				// Should only have group1 (127.0.0.1, 127.0.0.2) and ungrouped (127.0.0.5)
				g.Expect(aRecords).To(HaveLen(1))
				g.Expect(aRecords[0].Targets).To(ConsistOf("127.0.0.1", "127.0.0.2", "127.0.0.5"))
			}, TestTimeoutLong*3, time.Second).Should(Succeed())

			By("switching to group2 as the active group")
			Expect(setActiveGroupsInDNS(ctx, testZoneDomainName, types.Groups{types.Group("group2")})).To(Succeed())

			By("waiting for group2 records to show Active condition")
			Eventually(func(g Gomega) {
				g.Expect(primary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(primary2Group2DNSRecord), primary2Group2DNSRecord)).To(Succeed())
				g.Expect(primary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)

				g.Expect(secondary2Group2K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary2Group2DNSRecord), secondary2Group2DNSRecord)).To(Succeed())
				g.Expect(secondary2Group2DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("waiting for group1 records to show Inactive condition")
			Eventually(func(g Gomega) {
				g.Expect(primary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(primary1Group1DNSRecord), primary1Group1DNSRecord)).To(Succeed())
				g.Expect(primary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
					})),
				)

				g.Expect(secondary1Group1K8sClient.Get(ctx, client.ObjectKeyFromObject(secondary1Group1DNSRecord), secondary1Group1DNSRecord)).To(Succeed())
				g.Expect(secondary1Group1DNSRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeActive)),
						"Status": Equal(metav1.ConditionFalse),
					})),
				)
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("verifying zone contains only group2 and ungrouped records")
			// Allow extra time for ungrouped record to reconcile after group change
			Eventually(func(g Gomega) {
				records, err := memClient.Records(testZoneDomainName)
				g.Expect(err).NotTo(HaveOccurred())

				// Filter for A records matching our hostname
				var aRecords []*externaldnsendpoint.Endpoint
				for _, r := range records {
					if r.RecordType == "A" && r.DNSName == testHostname {
						aRecords = append(aRecords, r)
					}
				}

				// Should only have group2 (127.0.0.3, 127.0.0.4) and ungrouped (127.0.0.5)
				g.Expect(aRecords).To(HaveLen(1))
				g.Expect(aRecords[0].Targets).To(ConsistOf("127.0.0.3", "127.0.0.4", "127.0.0.5"))
			}, TestTimeoutLong*3, time.Second).Should(Succeed())

			By("clearing active groups")
			Expect(setActiveGroupsInDNS(ctx, testZoneDomainName, types.Groups{})).To(Succeed())

			By("verifying only ungrouped records are published when no active groups")
			Eventually(func(g Gomega) {
				records, err := memClient.Records(testZoneDomainName)
				g.Expect(err).NotTo(HaveOccurred())

				// Filter for A records matching our hostname
				var aRecords []*externaldnsendpoint.Endpoint
				for _, r := range records {
					if r.RecordType == "A" && r.DNSName == testHostname {
						aRecords = append(aRecords, r)
					}
				}

				// Should only have ungrouped (127.0.0.5) - grouped records are inactive
				g.Expect(aRecords).To(HaveLen(1))
				g.Expect(aRecords[0].Targets).To(ConsistOf("127.0.0.5"))
			}, TestTimeoutLong*3, time.Second).Should(Succeed())
		})
	})
})
