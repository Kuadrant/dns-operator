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
				Endpoints:   getTestLBEndpoints(testHostname),
				HealthCheck: getTestHealthCheckSpec(),
			},
		}
	})

	It("Should create valid probe CRs and remove them on DNSRecord deletion", func() {
		//Create default test dnsrecord
		Expect(k8sClient.Create(ctx, dnsRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionTrue),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("Provider ensured the dns record"),
				})),
			)
		}, TestTimeoutMedium, time.Second).Should(Succeed())

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

})
