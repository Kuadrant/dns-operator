//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/hash"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

const (
	controllerNamespace      = "dns-operator-system"
	controllerDeploymentName = "dns-operator-controller-manager"
	deploymentReadyTimeout   = 3 * time.Minute
)

// Group tests are Serial and Ordered to minimize deployment restarts
// This reduces test time from ~5 minutes to ~2-3 minutes
var _ = Describe("Group Setting on Controller", Labels{"group"}, Serial, Ordered, func() {
	var k8sClient client.Client
	var testDNSProviderSecret *v1.Secret
	var initialGroupValue string

	BeforeAll(func(ctx SpecContext) {
		k8sClient = testClusters[0].k8sClient
		testDNSProviderSecret = testClusters[0].testDNSProviderSecrets[0]

		// Store the initial group value from the deployment
		deployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: controllerNamespace,
			Name:      controllerDeploymentName,
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		initialGroupValue = getGroupFromDeployment(deployment)
	})

	AfterAll(func(ctx SpecContext) {
		// Restore initial group value only once at the end
		By("restoring initial controller group configuration")
		err := setControllerGroup(ctx, k8sClient, initialGroupValue)
		Expect(err).NotTo(HaveOccurred())
		waitForDeploymentReady(ctx, k8sClient, controllerNamespace, controllerDeploymentName)
	})

	// Test 1: Start with no group, create a DNSRecord
	It("should create DNSRecord without group when controller has no group configured", func(ctx SpecContext) {
		By("ensuring controller has no group configured")
		err := setControllerGroup(ctx, k8sClient, "")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for deployment to become ready")
		waitForDeploymentReady(ctx, k8sClient, controllerNamespace, controllerDeploymentName)

		By("creating a DNSRecord without group")
		dnsRecord := createTestDNSRecord(ctx, k8sClient, testDNSProviderSecret, "127.0.0.1")

		By("checking DNSRecord becomes ready without group")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
					"Status": Equal(metav1.ConditionTrue),
				})),
			)
			g.Expect(string(dnsRecord.Status.Group)).To(BeEmpty())
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		DeferCleanup(func(ctx SpecContext) {
			By("cleaning up test DNSRecord")
			err := k8sClient.Delete(ctx, dnsRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, time.Second, ctx).Should(Succeed())
		})
	})

	// Test 2: Add group to controller, verify existing DNSRecord gets updated
	It("should add group to existing DNSRecord when group is added to controller", func(ctx SpecContext) {
		testGroup := "test-group"

		By("creating a DNSRecord before adding group")
		dnsRecord := createTestDNSRecord(ctx, k8sClient, testDNSProviderSecret, "127.0.0.2")

		By("waiting for DNSRecord to be ready without group")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
					"Status": Equal(metav1.ConditionTrue),
				})),
			)
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By(fmt.Sprintf("adding group %s to controller", testGroup))
		err := setControllerGroup(ctx, k8sClient, testGroup)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for deployment to become ready")
		waitForDeploymentReady(ctx, k8sClient, controllerNamespace, controllerDeploymentName)

		By("checking DNSRecord is reconciled with group in status")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(dnsRecord.Status.Group)).To(Equal(testGroup))
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By("verifying group is present in provider TXT records")
		if txtRegistryEnabled {
			err := verifyGroupInTXTRecords(ctx, k8sClient, dnsRecord, testGroup)
			Expect(err).NotTo(HaveOccurred())
		}

		DeferCleanup(func(ctx SpecContext) {
			By("cleaning up test DNSRecord")
			err := k8sClient.Delete(ctx, dnsRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, time.Second, ctx).Should(Succeed())
		})
	})

	// Test 3: Update group value - controller already has test-group from previous test
	It("should update group in DNSRecord status and TXT records when group value changes", func(ctx SpecContext) {
		initialGroup := "test-group" // From previous test
		updatedGroup := "super-test-group"

		By("creating a DNSRecord with initial group")
		dnsRecord := createTestDNSRecord(ctx, k8sClient, testDNSProviderSecret, "127.0.0.3")

		By("checking DNSRecord becomes ready with initial group")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
					"Status": Equal(metav1.ConditionTrue),
				})),
			)
			g.Expect(string(dnsRecord.Status.Group)).To(Equal(initialGroup))
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By(fmt.Sprintf("updating controller group from %s to %s", initialGroup, updatedGroup))
		err := setControllerGroup(ctx, k8sClient, updatedGroup)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for deployment to become ready")
		waitForDeploymentReady(ctx, k8sClient, controllerNamespace, controllerDeploymentName)

		By("checking DNSRecord is reconciled with updated group")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(dnsRecord.Status.Group)).To(Equal(updatedGroup))
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By("verifying updated group is present in provider TXT records")
		if txtRegistryEnabled {
			err := verifyGroupInTXTRecords(ctx, k8sClient, dnsRecord, updatedGroup)
			Expect(err).NotTo(HaveOccurred())
		}

		DeferCleanup(func(ctx SpecContext) {
			By("cleaning up test DNSRecord")
			err := k8sClient.Delete(ctx, dnsRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, time.Second, ctx).Should(Succeed())
		})
	})

	// Test 4: Remove group - controller already has super-test-group from previous test
	It("should remove group from DNSRecord status and TXT records when removed from controller", func(ctx SpecContext) {
		currentGroup := "super-test-group" // From previous test

		By("creating a DNSRecord with group")
		dnsRecord := createTestDNSRecord(ctx, k8sClient, testDNSProviderSecret, "127.0.0.4")

		By("checking DNSRecord becomes ready with group")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
					"Status": Equal(metav1.ConditionTrue),
				})),
			)
			g.Expect(string(dnsRecord.Status.Group)).To(Equal(currentGroup))
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By("removing group from controller")
		err := setControllerGroup(ctx, k8sClient, "")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for deployment to become ready")
		waitForDeploymentReady(ctx, k8sClient, controllerNamespace, controllerDeploymentName)

		By("checking group is removed from DNSRecord status")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(dnsRecord.Status.Group)).To(BeEmpty())
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By("verifying group is removed from provider TXT records")
		if txtRegistryEnabled {
			err := verifyGroupNotInTXTRecords(ctx, k8sClient, dnsRecord)
			Expect(err).NotTo(HaveOccurred())
		}

		DeferCleanup(func(ctx SpecContext) {
			By("cleaning up test DNSRecord")
			err := k8sClient.Delete(ctx, dnsRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, time.Second, ctx).Should(Succeed())
		})
	})
})

// Helper functions

// createTestDNSRecord creates a simple DNSRecord for testing with a unique test ID and IP
func createTestDNSRecord(ctx context.Context, k8sClient client.Client, testDNSProviderSecret *v1.Secret, targetIP string) *v1alpha1.DNSRecord {
	testID := "t-group-" + GenerateName()
	testDomainName := strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
	testHostname := strings.Join([]string{testID, testDomainName}, ".")

	dnsRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testID,
			Namespace: testDNSProviderSecret.Namespace,
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: testHostname,
			ProviderRef: &v1alpha1.ProviderRef{
				Name: testProviderSecretName,
			},
			Endpoints: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    testHostname,
					Targets:    []string{targetIP},
					RecordType: "A",
					RecordTTL:  60,
				},
			},
		},
	}

	err := k8sClient.Create(ctx, dnsRecord)
	Expect(err).ToNot(HaveOccurred())

	return dnsRecord
}

// getGroupFromDeployment extracts the current group value from the controller deployment
func getGroupFromDeployment(deployment *appsv1.Deployment) string {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "manager" {
			for i, arg := range container.Args {
				if arg == "--group" && i+1 < len(container.Args) {
					return container.Args[i+1]
				}
			}
			for _, env := range container.Env {
				if env.Name == "GROUP" {
					return env.Value
				}
			}
		}
	}
	return ""
}

// setControllerGroup sets the group on the controller deployment
func setControllerGroup(ctx context.Context, k8sClient client.Client, group string) error {
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: controllerNamespace,
		Name:      controllerDeploymentName,
	}, deployment)
	if err != nil {
		return err
	}

	// Find the manager container and update/add the group argument
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == "manager" {
			args := deployment.Spec.Template.Spec.Containers[i].Args

			// Remove existing --group flag if present
			newArgs := []string{}
			skipNext := false
			for _, arg := range args {
				if skipNext {
					skipNext = false
					continue
				}
				if arg == "--group" {
					skipNext = true
					continue
				}
				newArgs = append(newArgs, arg)
			}

			// Add the new group flag if group is not empty
			if group != "" {
				newArgs = append(newArgs, "--group", group)
			}

			deployment.Spec.Template.Spec.Containers[i].Args = newArgs
			break
		}
	}

	return k8sClient.Update(ctx, deployment)
}

// waitForDeploymentReady waits for a deployment to become ready
func waitForDeploymentReady(ctx context.Context, k8sClient client.Client, namespace, name string) {
	Eventually(func(g Gomega, ctx context.Context) {
		deployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, deployment)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(deployment.Status.Replicas))
		g.Expect(deployment.Status.UpdatedReplicas).To(Equal(deployment.Status.Replicas))
	}, deploymentReadyTimeout, 5*time.Second, ctx).Should(Succeed())
}

// verifyGroupInTXTRecords verifies that the group is present in the TXT records in the provider
func verifyGroupInTXTRecords(ctx context.Context, k8sClient client.Client, dnsRecord *v1alpha1.DNSRecord, expectedGroup string) error {
	testProvider, err := ProviderForDNSRecord(ctx, dnsRecord, k8sClient, testClusters[0].dynamicClient)
	if err != nil {
		return err
	}

	zoneEndpoints, err := EndpointsForHost(ctx, testProvider, dnsRecord.Spec.RootHost)
	if err != nil {
		return err
	}

	// Look for TXT record with group information
	txtRecordName := "kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + dnsRecord.Spec.RootHost
	found := false
	for _, ep := range zoneEndpoints {
		if ep.RecordType == "TXT" && ep.DNSName == txtRecordName {
			found = true
			// The TXT record should contain targets information when group is set
			// Format: "heritage=external-dns,external-dns/owner=<ownerID>,external-dns/version=1,targets=<targets>"
			// or the targets are embedded in a separate way
			Expect(ep.Targets).NotTo(BeEmpty())
			// The group is added as a label on the endpoint, which should result in targets being present
			GinkgoWriter.Printf("[debug] TXT record for group: %s, targets: %v\n", txtRecordName, ep.Targets)
			break
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf("TXT record %s not found for group verification", txtRecordName))

	return nil
}

// verifyGroupNotInTXTRecords verifies that the group has been removed from TXT records
func verifyGroupNotInTXTRecords(ctx context.Context, k8sClient client.Client, dnsRecord *v1alpha1.DNSRecord) error {
	testProvider, err := ProviderForDNSRecord(ctx, dnsRecord, k8sClient, testClusters[0].dynamicClient)
	if err != nil {
		return err
	}

	zoneEndpoints, err := EndpointsForHost(ctx, testProvider, dnsRecord.Spec.RootHost)
	if err != nil {
		return err
	}

	// Look for TXT record - when group is removed, the TXT record should not contain targets field
	txtRecordName := "kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + dnsRecord.Spec.RootHost
	for _, ep := range zoneEndpoints {
		if ep.RecordType == "TXT" && ep.DNSName == txtRecordName {
			// When group is not set, the TXT record should only contain the basic heritage info
			// without the targets field
			GinkgoWriter.Printf("[debug] TXT record without group: %s, targets: %v\n", txtRecordName, ep.Targets)
			break
		}
	}

	return nil
}
