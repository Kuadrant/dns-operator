//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Test Cases covering multiple creation and deletion of health checks
var _ = Describe("Health Check Test", Serial, Labels{"health_checks"}, func() {
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var dnsRecord *v1alpha1.DNSRecord

	BeforeEach(func() {
		testID = "t-health-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		helpers.SetTestEnv("testID", testID)
		helpers.SetTestEnv("testHostname", testHostname)
		helpers.SetTestEnv("testNamespace", testNamespace)
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	Context("DNS Provider health checks", func() {
		It("creates health checks for a health check spec", func(ctx SpecContext) {
			healthChecksSupported := false
			if slices.Contains(supportedHealthCheckProviders, strings.ToLower(testDNSProvider)) {
				healthChecksSupported = true
			}

			provider, err := providerForManagedZone(ctx, testManagedZone)
			Expect(err).To(BeNil())

			By("creating a DNS Record")
			dnsRecord = &v1alpha1.DNSRecord{}
			err = helpers.ResourceFromFile("./fixtures/healthcheck_test/geo-dnsrecord-healthchecks.yaml", dnsRecord, helpers.GetTestEnv)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("Confirming the DNS Record status")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).ToNot(HaveOccurred())

				if healthChecksSupported {
					g.Expect(dnsRecord.Status.HealthCheck).ToNot(BeNil())
					g.Expect(&dnsRecord.Status.HealthCheck.Probes).ToNot(BeNil())
					g.Expect(len(dnsRecord.Status.HealthCheck.Probes)).ToNot(BeZero())
					for _, condition := range dnsRecord.Status.HealthCheck.Conditions {
						if condition.Type == "healthProbesSynced" {
							g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
							g.Expect(condition.Reason).To(Equal("AllProbesSynced"))
						}
					}
				} else {
					g.Expect(dnsRecord.Status.HealthCheck).ToNot(BeNil())
					g.Expect(dnsRecord.Status.HealthCheck.Probes).To(BeNil())
				}

				for _, probe := range dnsRecord.Status.HealthCheck.Probes {
					g.Expect(probe.Host).To(Equal(testHostname))
					g.Expect(probe.IPAddress).To(Equal("172.32.200.1"))
					g.Expect(probe.ID).ToNot(Equal(""))

					for _, probeCondition := range probe.Conditions {
						g.Expect(probeCondition.Type).To(Equal("ProbeSynced"))
						g.Expect(probeCondition.Status).To(Equal(metav1.ConditionTrue))
						g.Expect(probeCondition.Message).To(ContainSubstring(fmt.Sprintf("id: %v, address: %v, host: %v", probe.ID, probe.IPAddress, probe.Host)))
					}
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("confirming the health checks exist in the provider")
			Eventually(func(g Gomega) {
				if !healthChecksSupported {
					g.Expect(len(dnsRecord.Status.HealthCheck.Probes)).To(BeZero())
				}
				for _, healthCheck := range dnsRecord.Status.HealthCheck.Probes {
					exists, err := provider.HealthCheckReconciler().HealthCheckExists(ctx, &healthCheck)
					g.Expect(err).To(BeNil())
					g.Expect(exists).To(BeTrue())
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("Removing the health check")
			oldHealthCheckStatus := dnsRecord.Status.HealthCheck.DeepCopy()
			Eventually(func(g Gomega) {
				patchFrom := client.MergeFrom(dnsRecord.DeepCopy())
				dnsRecord.Spec.HealthCheck = nil
				err := k8sClient.Patch(ctx, dnsRecord, patchFrom)
				g.Expect(err).To(BeNil())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("Confirming the DNS Record status")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dnsRecord.Status.HealthCheck).To(BeNil())
			})

			By("confirming the health checks were removed in the provider")
			Eventually(func(g Gomega) {
				for _, healthCheck := range oldHealthCheckStatus.Probes {
					exists, err := provider.HealthCheckReconciler().HealthCheckExists(ctx, &healthCheck)
					g.Expect(err).NotTo(BeNil())
					g.Expect(exists).To(BeFalse())
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("Adding a health check spec")
			Eventually(func(g Gomega) {
				patchFrom := client.MergeFrom(dnsRecord.DeepCopy())
				err = helpers.ResourceFromFile("./fixtures/healthcheck_test/geo-dnsrecord-healthchecks.yaml", dnsRecord, helpers.GetTestEnv)
				g.Expect(err).ToNot(HaveOccurred())
				err := k8sClient.Patch(ctx, dnsRecord, patchFrom)
				g.Expect(err).To(BeNil())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("Confirming the DNS Record status")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).ToNot(HaveOccurred())

				if healthChecksSupported {
					g.Expect(dnsRecord.Status.HealthCheck).ToNot(BeNil())
					g.Expect(&dnsRecord.Status.HealthCheck.Probes).ToNot(BeNil())
					g.Expect(len(dnsRecord.Status.HealthCheck.Probes)).ToNot(BeZero())
					for _, condition := range dnsRecord.Status.HealthCheck.Conditions {
						if condition.Type == "healthProbesSynced" {
							g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
							g.Expect(condition.Reason).To(Equal("AllProbesSynced"))
						}
					}
				} else {
					g.Expect(dnsRecord.Status.HealthCheck).ToNot(BeNil())
					g.Expect(dnsRecord.Status.HealthCheck.Probes).To(BeNil())
				}

				for _, probe := range dnsRecord.Status.HealthCheck.Probes {
					g.Expect(probe.Host).To(Equal(testHostname))
					g.Expect(probe.IPAddress).To(Equal("172.32.200.1"))
					g.Expect(probe.ID).ToNot(Equal(""))

					for _, probeCondition := range probe.Conditions {
						g.Expect(probeCondition.Type).To(Equal("ProbeSynced"))
						g.Expect(probeCondition.Status).To(Equal(metav1.ConditionTrue))
						g.Expect(probeCondition.Message).To(ContainSubstring(fmt.Sprintf("id: %v, address: %v, host: %v", probe.ID, probe.IPAddress, probe.Host)))
					}
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("confirming the health checks exist in the provider")
			Eventually(func(g Gomega) {
				if !healthChecksSupported {
					g.Expect(len(dnsRecord.Status.HealthCheck.Probes)).To(BeZero())
				}
				for _, healthCheck := range dnsRecord.Status.HealthCheck.Probes {
					exists, err := provider.HealthCheckReconciler().HealthCheckExists(ctx, &healthCheck)
					g.Expect(err).To(BeNil())
					g.Expect(exists).To(BeTrue())
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("Deleting the DNS Record")
			oldHealthCheckStatus = dnsRecord.Status.HealthCheck.DeepCopy()
			err = helpers.ResourceFromFile("./fixtures/healthcheck_test/geo-dnsrecord-healthchecks.yaml", dnsRecord, helpers.GetTestEnv)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err := k8sClient.Delete(ctx, dnsRecord)
				g.Expect(client.IgnoreNotFound(err)).To(BeNil())

				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(errors.IsNotFound(err)).Should(BeTrue())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("confirming the health checks were removed in the provider")
			Eventually(func(g Gomega) {
				for _, healthCheck := range oldHealthCheckStatus.Probes {
					exists, err := provider.HealthCheckReconciler().HealthCheckExists(ctx, &healthCheck)
					g.Expect(err).NotTo(BeNil())
					g.Expect(exists).To(BeFalse())
				}
			}, TestTimeoutMedium, time.Second).Should(Succeed())
			// test

		})
	})
})
