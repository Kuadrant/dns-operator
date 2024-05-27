//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Test Cases covering multiple creation and deletion of health checks
var _ = Describe("Clean Up Test", Labels{"cleanup"}, func() {

	var testID string

	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string

	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var testDeletableNamespace string

	BeforeEach(func() {
		testID = "t-health-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		testDeletableNamespace = testNamespace + "-delete"
		helpers.SetTestEnv("testID", testID)
		helpers.SetTestEnv("testHostname", testHostname)
		helpers.SetTestEnv("testNamespace", testNamespace)
		helpers.SetTestEnv("testDeletableNamespace", testDeletableNamespace)
		helpers.SetTestEnv("testZoneDomainName", testZoneDomainName)

		helpers.SetTestEnv("awsTestZoneID", testAWSZoneID)
		helpers.SetTestEnv("awsAccessKey", testAWSAccessKey)
		helpers.SetTestEnv("awsSecretKey", testAWSSecretKey)

		helpers.SetTestEnv("gcpSecret", testGCPSecret)
		helpers.SetTestEnv("gcpProjectID", testGCPProjectID)
		helpers.SetTestEnv("gcpTestZoneID", testGCPZoneID)
	})

	AfterEach(func(ctx SpecContext) {
		deletableNS := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: helpers.GetTestEnv("testDeletableNamespace"),
			},
		}
		deletableSecret := &v1.Secret{}
		deletableMZ := &v1alpha1.ManagedZone{}
		deletableDNSRecord := &v1alpha1.DNSRecord{}

		err := k8sClient.Get(ctx, client.ObjectKey{Name: testDeletableNamespace}, deletableNS)
		// test if NS exists
		if !errors.IsNotFound(err) {
			// ensure objects are removed
			Expect(
				helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
			).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableMZ), deletableMZ)).To(BeNil())
			mzFrom := deletableMZ.DeepCopy()
			deletableMZ.Finalizers = nil
			patch := client.MergeFrom(mzFrom)
			Expect(k8sClient.Patch(ctx, deletableMZ, patch)).To(Succeed())

			Expect(
				helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/dnsrecord.yaml", testDNSProvider), deletableDNSRecord, helpers.GetTestEnv),
			).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), deletableDNSRecord)).To(BeNil())
			recordFrom := deletableDNSRecord.DeepCopy()
			deletableDNSRecord.Finalizers = nil
			patch = client.MergeFrom(recordFrom)
			Expect(k8sClient.Patch(ctx, deletableDNSRecord, patch)).To(Succeed())

			// ensure NS is removed
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), deletableNS)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		}

		// no we know all resources are gone, recreate them, so we can delete the gracefully and ensure the zone is empty
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, deletableNS))).To(Succeed())

		Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/secret.yaml", testDNSProvider), deletableSecret, helpers.GetTestEnv)).To(Succeed())
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, deletableSecret))).To(Succeed())

		Expect(
			helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
		).To(Succeed())
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, deletableMZ))).To(Succeed())

		Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/dnsrecord.yaml", testDNSProvider), deletableDNSRecord, helpers.GetTestEnv)).To(Succeed())
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, deletableDNSRecord))).To(Succeed())

		// wait for DNS Record to be ready
		Eventually(func(g Gomega) {
			dnsRecord := &v1alpha1.DNSRecord{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), dnsRecord)
			g.Expect(err).To(BeNil())

			ready := meta.FindStatusCondition(dnsRecord.Status.Conditions, string(v1alpha1.ConditionTypeReady))
			g.Expect(ready).ToNot(BeNil())
			g.Expect(ready.Status).To(BeEquivalentTo(v1.ConditionTrue))
		}, TestTimeoutLong, time.Second).Should(Succeed())

		// delete DNS Record first
		Expect(
			client.IgnoreNotFound(k8sClient.Delete(ctx, deletableDNSRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))),
		).Should(Succeed())

		// wait for it to go
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), deletableDNSRecord)
			return errors.IsNotFound(err)
		}, TestTimeoutMedium, time.Second).Should(BeTrue())

		// delete the zone
		deletableMZ = &v1alpha1.ManagedZone{}
		Expect(
			helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
		).To(Succeed())
		Expect(
			client.IgnoreNotFound(k8sClient.Delete(ctx, deletableMZ, client.PropagationPolicy(metav1.DeletePropagationForeground))),
		).Should(Succeed())

		// wait for it to go
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableMZ), deletableMZ)
			return errors.IsNotFound(err)
		}, TestTimeoutMedium, time.Second).Should(BeTrue())

		// now we can delete the NS
		deletableNS = &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: helpers.GetTestEnv("testDeletableNamespace"),
			},
		}
		Expect(
			client.IgnoreNotFound(k8sClient.Delete(ctx, deletableNS, client.PropagationPolicy(metav1.DeletePropagationForeground))),
		).Should(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), deletableNS)
			return errors.IsNotFound(err)
		}, TestTimeoutMedium, time.Second).Should(BeTrue())
	})

	Context("deleting resources that have interacted with the DNS Provider", func() {
		It("sets a status on the namespace for the failed deletion of DNS Records", func(ctx SpecContext) {
			By("creating a deletable namespace")
			deletableNS := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: helpers.GetTestEnv("testDeletableNamespace"),
				},
			}
			Expect(k8sClient.Create(ctx, deletableNS)).To(Succeed())

			By("creating a new managedZone and secret")
			deletableMZ := &v1alpha1.ManagedZone{}
			Expect(
				helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
			).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableMZ)).To(Succeed())

			deletableSecret := &v1.Secret{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/secret.yaml", testDNSProvider), deletableSecret, helpers.GetTestEnv)).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableSecret)).To(Succeed())

			deletableDNSRecord := &v1alpha1.DNSRecord{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/dnsrecord.yaml", testDNSProvider), deletableDNSRecord, helpers.GetTestEnv)).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableDNSRecord)).To(Succeed())

			By("waiting for DNSRecord status to be healthy")
			Eventually(func(g Gomega) {
				dnsRecord := &v1alpha1.DNSRecord{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), dnsRecord)
				g.Expect(err).To(BeNil())

				ready := meta.FindStatusCondition(dnsRecord.Status.Conditions, string(v1alpha1.ConditionTypeReady))
				g.Expect(ready).ToNot(BeNil())
				g.Expect(ready.Status).To(BeEquivalentTo(v1.ConditionTrue))
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("deleting Zone secret")
			Expect(k8sClient.Delete(ctx, deletableSecret)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableSecret), deletableSecret)
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("deleting Namespace")
			Expect(k8sClient.Delete(ctx, deletableNS)).To(Succeed())

			By("expecting bad deletion status on namespace")
			Eventually(func(g Gomega) {
				ns := &v1.Namespace{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), ns)).To(Succeed())
				g.Expect(ns.DeletionTimestamp).ToNot(BeNil())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			Consistently(func(g Gomega) {
				ns := &v1.Namespace{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), ns)).To(Succeed())
				for _, c := range ns.Status.Conditions {
					if c.Type == "NamespaceFinalizersRemaining" || c.Type == "NamespaceContentRemaining" {
						g.Expect(c.Status).To(BeEquivalentTo(v1.ConditionTrue))
					} else if c.Type == "NamespaceDeletionContentFailure" {
						g.Expect(c.Status).To(BeEquivalentTo(v1.ConditionFalse))
					}
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("removing finalizers on records and zone")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), deletableDNSRecord)).To(BeNil())
			recordFrom := deletableDNSRecord.DeepCopy()
			deletableDNSRecord.Finalizers = nil
			patch := client.MergeFrom(recordFrom)
			Expect(k8sClient.Patch(ctx, deletableDNSRecord, patch)).To(Succeed())

			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableMZ), deletableMZ)
			Expect(client.IgnoreNotFound(err)).To(BeNil())
			if !errors.IsNotFound(err) {
				mzFrom := deletableMZ.DeepCopy()
				deletableMZ.Finalizers = nil
				patch = client.MergeFrom(mzFrom)
				Expect(k8sClient.Patch(ctx, deletableMZ, patch)).To(Succeed())
			}

			By("expecting NS to delete")
			Eventually(func(g Gomega) {
				ns := &v1.Namespace{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), ns)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("recreating the test NS and test resources")
			deletableNS = &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: helpers.GetTestEnv("testDeletableNamespace"),
				},
			}
			Expect(k8sClient.Create(ctx, deletableNS)).To(Succeed())

			deletableMZ = &v1alpha1.ManagedZone{}
			Expect(
				helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
			).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableMZ)).To(Succeed())

			deletableSecret = &v1.Secret{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/secret.yaml", testDNSProvider), deletableSecret, helpers.GetTestEnv)).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableSecret)).To(Succeed())

			deletableDNSRecord = &v1alpha1.DNSRecord{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/dnsrecord.yaml", testDNSProvider), deletableDNSRecord, helpers.GetTestEnv)).To(Succeed())
			Expect(k8sClient.Create(ctx, deletableDNSRecord)).To(Succeed())

			By("waiting for DNSRecord status to be healthy")
			Eventually(func(g Gomega) {
				dnsRecord := &v1alpha1.DNSRecord{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), dnsRecord)
				g.Expect(err).To(BeNil())

				ready := meta.FindStatusCondition(dnsRecord.Status.Conditions, string(v1alpha1.ConditionTypeReady))
				g.Expect(ready).ToNot(BeNil())
				g.Expect(ready.Status).To(BeEquivalentTo(v1.ConditionTrue))
			}, TestTimeoutLong, time.Second).Should(Succeed())

			By("deleting the DNS Record and waiting for it to delete")
			deletableDNSRecord = &v1alpha1.DNSRecord{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/dnsrecord.yaml", testDNSProvider), deletableDNSRecord, helpers.GetTestEnv)).To(Succeed())
			Expect(
				client.IgnoreNotFound(k8sClient.Delete(ctx, deletableDNSRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))),
			).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableDNSRecord), deletableDNSRecord)
				return errors.IsNotFound(err)
			}, TestTimeoutMedium, time.Second).Should(BeTrue())

			By("deleting the Zone and waiting for it to delete")
			deletableMZ = &v1alpha1.ManagedZone{}
			Expect(
				helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/managedzone.yaml", testDNSProvider), deletableMZ, helpers.GetTestEnv),
			).To(Succeed())
			Expect(
				client.IgnoreNotFound(k8sClient.Delete(ctx, deletableMZ, client.PropagationPolicy(metav1.DeletePropagationForeground))),
			).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableMZ), deletableMZ)
				return errors.IsNotFound(err)
			}, TestTimeoutMedium, time.Second).Should(BeTrue())

			deletableSecret = &v1.Secret{}
			Expect(helpers.ResourceFromFile(fmt.Sprintf("./fixtures/cleanup_test/%v/secret.yaml", testDNSProvider), deletableSecret, helpers.GetTestEnv)).To(Succeed())
			Expect(
				client.IgnoreNotFound(k8sClient.Delete(ctx, deletableSecret, client.PropagationPolicy(metav1.DeletePropagationForeground))),
			).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableSecret), deletableSecret)
				return errors.IsNotFound(err)
			}, TestTimeoutMedium, time.Second).Should(BeTrue())

			By("deleting the namespace and waiting for it to delete")
			deletableNS = &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: helpers.GetTestEnv("testDeletableNamespace"),
				},
			}
			Expect(
				client.IgnoreNotFound(k8sClient.Delete(ctx, deletableNS, client.PropagationPolicy(metav1.DeletePropagationForeground))),
			).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deletableNS), deletableNS)
				return errors.IsNotFound(err)
			}, TestTimeoutMedium, time.Second).Should(BeTrue())

		})
	})
})
