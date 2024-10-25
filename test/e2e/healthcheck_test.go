//go:build e2e_healthchecks

package e2e

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

var _ = Describe("Health Check Test", Serial, Labels{"health_checks"}, func() {
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var k8sClient client.Client
	var transportLayerConfig *v1.ConfigMap
	var testDNSProviderSecret *v1.Secret

	var dnsRecord *v1alpha1.DNSRecord

	BeforeEach(func() {
		testID = "t-health-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		k8sClient = testClusters[0].k8sClient
		testDNSProviderSecret = testClusters[0].testDNSProviderSecrets[0]
		SetTestEnv("testID", testID)
		SetTestEnv("testHostname", testHostname)
		SetTestEnv("testNamespace", testDNSProviderSecret.Namespace)

		transportLayerConfig = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mockhealthcheckresponses",
				Namespace: "dns-operator-system",
			},
			Data: map[string]string{
				"172.32.200.1": "400",
				"172.32.200.2": "400",
			},
		}
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}

		if transportLayerConfig != nil {
			err := k8sClient.Delete(ctx, transportLayerConfig,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	// single controller against host
	Context("Sole publisher", func() {
		It("Correctly react to healthchecks", func(ctx SpecContext) {

			By("overriding transport layer")
			Eventually(func(g Gomega) {
				err := k8sClient.Create(ctx, transportLayerConfig)
				g.Expect(err).ToNot(HaveOccurred())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("creating dnsrecord ")
			dnsRecord = &v1alpha1.DNSRecord{}
			err := ResourceFromFile("./fixtures/healthcheck_test/geo-dnsrecord-healthchecks.yaml", dnsRecord, GetTestEnv)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("checking " + dnsRecord.Name + " haven't published unhealthy endpoints")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(0))
				g.Expect(dnsRecord.Status.Conditions).To(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionFalse),
						"Reason":             Equal(string(v1alpha1.ConditionReasonUnhealthy)),
						"Message":            Equal("None of the healthchecks succeeded"),
						"ObservedGeneration": Equal(dnsRecord.Generation),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(v1alpha1.ConditionReasonUnhealthy)),
						"Message": And(ContainSubstring("Not healthy addresses:"), ContainSubstring("172.32.200.1"), ContainSubstring("172.32.200.2")),
					}),
				))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("making all addresses healthy")
			transportLayerConfig.Data["172.32.200.1"] = "200"
			transportLayerConfig.Data["172.32.200.2"] = "200"
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Update(ctx, transportLayerConfig)).To(Succeed())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("checking " + dnsRecord.Name + " published healthy endpoints")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(5))
				g.Expect(dnsRecord.Status.Endpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal("14byhk-2k52h1.klb." + testHostname),
						"Targets":    ConsistOf("172.32.200.1", "172.32.200.2"),
						"RecordType": Equal("A"),
					})),
				))
				g.Expect(dnsRecord.Status.Conditions).To(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(v1alpha1.ConditionReasonProviderSuccess)),
						"Message": Equal("Provider ensured the dns record"),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(v1alpha1.ConditionReasonHealthy)),
						"Message": Equal("All healthchecks succeeded"),
					}),
				))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("making 172.32.200.2 unhealthy")
			transportLayerConfig.Data["172.32.200.2"] = "400"
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Update(ctx, transportLayerConfig)).To(Succeed())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("ensure unhealthy address removed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(5))
				g.Expect(dnsRecord.Status.Endpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal("14byhk-2k52h1.klb." + testHostname),
						"Targets":    ConsistOf("172.32.200.1"),
						"RecordType": Equal("A"),
					})),
				))
				g.Expect(dnsRecord.Status.Conditions).To(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(v1alpha1.ConditionReasonProviderSuccess)),
						"Message": Equal("Provider ensured the dns record"),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(v1alpha1.ConditionReasonPartiallyHealthy)),
						"Message": Equal("Not healthy addresses: [172.32.200.2]"),
					}),
				))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("making all addresses unhealthy")
			transportLayerConfig.Data["172.32.200.1"] = "400"
			transportLayerConfig.Data["172.32.200.2"] = "400"
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Update(ctx, transportLayerConfig)).To(Succeed())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("ensure EPs are not removed and status updated")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(5))
				g.Expect(dnsRecord.Status.Endpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal("14byhk-2k52h1.klb." + testHostname),
						"Targets":    ConsistOf("172.32.200.1"),
						"RecordType": Equal("A"),
					})),
				))
				g.Expect(dnsRecord.Status.Conditions).To(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(v1alpha1.ConditionReasonUnhealthy)),
						"Message": Equal("None of the healthchecks succeeded"),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(v1alpha1.ConditionReasonUnhealthy)),
						"Message": And(ContainSubstring("Not healthy addresses:"), ContainSubstring("172.32.200.1"), ContainSubstring("172.32.200.2")),
					}),
				))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("removing healthchecks")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())

				dnsRecord.Spec.HealthCheck = nil

				g.Expect(k8sClient.Update(ctx, dnsRecord)).To(Succeed())
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("ensure EPs are published")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(5))
				g.Expect(dnsRecord.Status.Endpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal("14byhk-2k52h1.klb." + testHostname),
						"Targets":    ConsistOf("172.32.200.1", "172.32.200.2"),
						"RecordType": Equal("A"),
					})),
				))
				g.Expect(dnsRecord.Status.Conditions).To(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(v1alpha1.ConditionReasonProviderSuccess)),
						"Message": Equal("Provider ensured the dns record"),
					}),
				))
				// make sure healthcheck condition is gone
				g.Expect(dnsRecord.Status.Conditions).ToNot(ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(v1alpha1.ConditionTypeHealthy)),
					}),
				))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())
		})
	})
})
