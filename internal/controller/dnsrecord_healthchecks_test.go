//go:build integration

package controller

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"
)

var _ = Describe("DNSRecordReconciler_HealthChecks", func() {
	var (
		dnsRecord *v1alpha1.DNSRecord

		testNamespace, testHostname string
	)

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		testZoneDomainName := strings.Join([]string{GenerateName(), "example.com"}, ".")
		testHostname = strings.Join([]string{"foo", testZoneDomainName}, ".")

		dnsProviderSecret := builder.NewProviderBuilder("inmemory-credentials", testNamespace).
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
				Endpoints:   getTestEndpoints(testHostname, []string{"172.32.200.1", "172.32.200.2"}),
				HealthCheck: getTestHealthCheckSpec(),
			},
		}
	})

	It("Should create valid probe CRs and remove them on DNSRecord deletion", func() {
		//Create default test dnsrecord
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		By("Validating created probes")
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())

			g.Expect(probes.Items).To(HaveLen(2))
			g.Expect(probes.Items).To(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":      Equal(fmt.Sprintf("%s-%s", dnsRecord.Name, "172.32.200.1")),
						"Namespace": Equal(testNamespace),
					}),
					"Spec": MatchFields(IgnoreExtras, Fields{
						"Port":     Equal(443),
						"Hostname": Equal(testHostname),
						"Address":  Equal("172.32.200.1"),
						"Path":     Equal("/healthz"),
						"Protocol": Equal(v1alpha1.Protocol("HTTPS")),
						"Interval": Equal(metav1.Duration{Duration: time.Minute}),
						"AdditionalHeadersRef": PointTo(MatchFields(IgnoreExtras, Fields{
							"Name": Equal("headers"),
						})),
						"FailureThreshold":         Equal(5),
						"AllowInsecureCertificate": Equal(true),
					}),
				}),
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":      Equal(fmt.Sprintf("%s-%s", dnsRecord.Name, "172.32.200.2")),
						"Namespace": Equal(testNamespace),
					}),
					"Spec": MatchFields(IgnoreExtras, Fields{
						"Port":     Equal(443),
						"Hostname": Equal(testHostname),
						"Address":  Equal("172.32.200.2"),
						"Path":     Equal("/healthz"),
						"Protocol": Equal(v1alpha1.Protocol("HTTPS")),
						"Interval": Equal(metav1.Duration{Duration: time.Minute}),
						"AdditionalHeadersRef": PointTo(MatchFields(IgnoreExtras, Fields{
							"Name": Equal("headers"),
						})),
						"FailureThreshold":         Equal(5),
						"AllowInsecureCertificate": Equal(true),
					}),
				}),
			))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("Ensuring probes block DNSRecord deletion and correctly removed")
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())

			g.Expect(probes.Items).To(HaveLen(2))
			g.Expect(probes.Items).To(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":      Equal(fmt.Sprintf("%s-%s", dnsRecord.Name, "172.32.200.1")),
						"Namespace": Equal(testNamespace),
						"OwnerReferences": ConsistOf(MatchFields(IgnoreExtras, Fields{
							"Name":               Equal(dnsRecord.Name),
							"UID":                Equal(dnsRecord.UID),
							"Controller":         PointTo(Equal(true)),
							"BlockOwnerDeletion": PointTo(Equal(true)),
						})),
					}),
				}),
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":      Equal(fmt.Sprintf("%s-%s", dnsRecord.Name, "172.32.200.2")),
						"Namespace": Equal(testNamespace),
						"OwnerReferences": ConsistOf(MatchFields(IgnoreExtras, Fields{
							"Name":               Equal(dnsRecord.Name),
							"UID":                Equal(dnsRecord.UID),
							"Controller":         PointTo(Equal(true)),
							"BlockOwnerDeletion": PointTo(Equal(true)),
						})),
					}),
				}),
			))

			g.Expect(k8sClient.Delete(ctx, dnsRecord)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
					LabelSelector: labels.SelectorFromSet(map[string]string{
						ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
					}),
					Namespace: dnsRecord.Namespace,
				})).To(Succeed())

				g.Expect(probes.Items).To(HaveLen(0))
			}, TestTimeoutShort, time.Second)

		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("Should not publish unhealthy enpoints", func() {
		//Create default test dnsrecord
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		// Until we mark probes as healthy there sohuld be no endpoints published
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.Status.Endpoints).To(HaveLen(0))
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":             Equal(metav1.ConditionFalse),
					"Reason":             Equal(string(v1alpha1.ConditionReasonUnhealthy)),
					"Message":            Equal("None of the healthchecks succeeded"),
					"ObservedGeneration": Equal(dnsRecord.Generation),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("Should create wildcard probes", func() {
		// make record a wildcard one
		dnsRecord.Spec.RootHost = v1alpha1.WildcardPrefix + dnsRecord.Spec.RootHost
		dnsRecord.Spec.Endpoints = getTestEndpoints(v1alpha1.WildcardPrefix+testHostname, []string{"172.32.200.1", "172.32.200.2"})
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		// make probes healthy
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())
			g.Expect(len(probes.Items)).To(Equal(2))

			for _, probe := range probes.Items {
				probe.Status.Healthy = ptr.To(true)
				probe.Status.LastCheckedAt = metav1.Now()
				probe.Status.ConsecutiveFailures = 0
				g.Expect(k8sClient.Status().Update(ctx, &probe)).To(Succeed())
			}
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// make sure we published endpoint
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.Status.Endpoints).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal(v1alpha1.WildcardPrefix + testHostname),
					"Targets": ConsistOf("172.32.200.1", "172.32.200.2"),
				})),
			))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

	It("Should remove unhealthy endpoints", func() {
		//Create default test dnsrecord
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())

		By("Marking all probes as healthy")
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())
			g.Expect(len(probes.Items)).To(Equal(2))

			// make probes healthy
			for _, probe := range probes.Items {
				probe.Status.Healthy = ptr.To(true)
				probe.Status.LastCheckedAt = metav1.Now()
				probe.Status.ConsecutiveFailures = 0
				g.Expect(k8sClient.Status().Update(ctx, &probe)).To(Succeed())
			}
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// make sure we published endpoint
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.Status.Endpoints).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal(testHostname),
					"Targets": ConsistOf("172.32.200.1", "172.32.200.2"),
				})),
			))
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("Making one of the probes unhealthy")
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())

			var updated bool
			// make one of the probes unhealthy
			for _, probe := range probes.Items {
				if probe.Spec.Address == "172.32.200.1" {
					probe.Status.Healthy = ptr.To(false)
					probe.Status.LastCheckedAt = metav1.Now()
					probe.Status.ConsecutiveFailures = dnsRecord.Spec.HealthCheck.FailureThreshold + 1
					g.Expect(k8sClient.Status().Update(ctx, &probe)).To(Succeed())
					updated = true
				}
			}
			// expect to have updated one of the probes
			g.Expect(updated).To(BeTrue())
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("Ensure unhealthy endpoints are gone and status is updated")
		Eventually(func(g Gomega) {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())

			g.Expect(dnsRecord.Status.Endpoints).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal(testHostname),
					"Targets": ConsistOf("172.32.200.2"),
				})),
			))
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal(string(v1alpha1.ConditionReasonPartiallyHealthy)),
					"Message": Equal("Not healthy addresses: [172.32.200.1]"),
				})),
			)

		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("Mark the second probe as unhealthy")
		Eventually(func(g Gomega) {
			probes := &v1alpha1.DNSHealthCheckProbeList{}

			g.Expect(k8sClient.List(ctx, probes, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
				}),
				Namespace: dnsRecord.Namespace,
			})).To(Succeed())

			var updated bool
			for _, probe := range probes.Items {
				if probe.Spec.Address == "172.32.200.2" {
					probe.Status.Healthy = ptr.To(false)
					probe.Status.LastCheckedAt = metav1.Now()
					probe.Status.ConsecutiveFailures = dnsRecord.Spec.HealthCheck.FailureThreshold + 1
					g.Expect(k8sClient.Status().Update(ctx, &probe)).To(Succeed())
					updated = true
				}
			}
			// expect to have updated one of the probes
			g.Expect(updated).To(BeTrue())
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// we don't remove EPs if this leads to empty EPs
		By("Ensure endpoints are not changed and status is updated")
		Eventually(func(g Gomega) {
			newRecord := &v1alpha1.DNSRecord{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), newRecord)).To(Succeed())
			g.Expect(dnsRecord.Status.Endpoints).To(BeEquivalentTo(newRecord.Status.Endpoints))

			g.Expect(newRecord.Status.Conditions).To(ContainElements(
				MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeHealthy)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal(string(v1alpha1.ConditionReasonUnhealthy)),
					"Message": Equal("Not healthy addresses: [172.32.200.1 172.32.200.2]"),
				}),
				MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal(string(v1alpha1.ConditionReasonUnhealthy)),
					"Message": Equal("None of the healthchecks succeeded"),
				}),
			))
		}, TestTimeoutMedium, time.Second).Should(Succeed())
	})

})
