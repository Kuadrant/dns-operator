//go:build e2e

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/conditions"
)

var _ = Describe("Single Cluster Record Test", func() {
	// testID is a randomly generated identifier for the test
	// it is used to name resources and/or namespaces so different
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var dnsRecord *v1alpha1.DNSRecord
	var geoCode string

	BeforeEach(func(ctx SpecContext) {
		testID = "t-single-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")

		if testDNSProvider == "gcp" {
			geoCode = "us-east1"
		} else {
			geoCode = "US"
		}
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	Context("simple", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating a dns record")
			testTargetIP := "127.0.0.1"
			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testID,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: testHostname,
							Targets: []string{
								testTargetIP,
							},
							RecordType: "A",
							RecordTTL:  60,
						},
					},
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(conditions.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("ensuring the authoritative nameserver resolves the hostname")
			// speed up things by using the authoritative nameserver
			authoritativeResolver := ResolverForDomainName(testZoneDomainName)
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ips).To(ContainElement(testTargetIP))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())
		})
	})

	Context("loadbalanced", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating a dns record")
			testTargetIP := "127.0.0.1"

			klbHostName := "klb." + testHostname
			geo1KlbHostName := geoCode + "." + klbHostName
			cluster1KlbHostName := "cluster1." + klbHostName

			dnsRecord = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-record",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: cluster1KlbHostName,
							Targets: []string{
								testTargetIP,
							},
							RecordType: "A",
							RecordTTL:  60,
						},
						{
							DNSName: testHostname,
							Targets: []string{
								klbHostName,
							},
							RecordType: "CNAME",
							RecordTTL:  300,
						},
						{
							DNSName: geo1KlbHostName,
							Targets: []string{
								cluster1KlbHostName,
							},
							RecordType:    "CNAME",
							RecordTTL:     60,
							SetIdentifier: cluster1KlbHostName,
							ProviderSpecific: externaldnsendpoint.ProviderSpecific{
								{
									Name:  "weight",
									Value: "200",
								},
							},
						},
						{
							DNSName: klbHostName,
							Targets: []string{
								geo1KlbHostName,
							},
							RecordType:    "CNAME",
							RecordTTL:     300,
							SetIdentifier: geoCode,
							ProviderSpecific: externaldnsendpoint.ProviderSpecific{
								{
									Name:  "geo-code",
									Value: geoCode,
								},
							},
						},
						{
							DNSName: klbHostName,
							Targets: []string{
								geo1KlbHostName,
							},
							RecordType:    "CNAME",
							RecordTTL:     300,
							SetIdentifier: "default",
							ProviderSpecific: externaldnsendpoint.ProviderSpecific{
								{
									Name:  "geo-code",
									Value: "*",
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(conditions.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("ensuring the authoritative nameserver resolves the hostname")
			// speed up things by using the authoritative nameserver
			authoritativeResolver := ResolverForDomainName(testZoneDomainName)
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ips).To(ContainElement(testTargetIP))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())
		})
	})

})
