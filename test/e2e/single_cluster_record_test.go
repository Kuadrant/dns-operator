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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
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
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
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

			By("ensuring zone records are created as expected")
			testProvider, err := providerForManagedZone(ctx, testManagedZone)
			Expect(err).NotTo(HaveOccurred())
			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			Expect(zoneEndpoints).To(HaveLen(1))
			Expect(zoneEndpoints).To(ContainElements(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(testHostname),
					"Targets":       ConsistOf(testTargetIP),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				})),
			))
		})
	})

	Context("loadbalanced", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating a dns record")
			testTargetIP := "127.0.0.1"

			klbHostName := "klb." + testHostname
			geo1KlbHostName := strings.ToLower(geoCode) + "." + klbHostName
			cluster1KlbHostName := "cluster1." + klbHostName

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
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
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

			By("ensuring zone records are created as expected")
			testProvider, err := providerForManagedZone(ctx, testManagedZone)
			Expect(err).NotTo(HaveOccurred())
			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			if testDNSProvider == "gcp" {
				Expect(zoneEndpoints).To(HaveLen(4))
				Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(cluster1KlbHostName),
						"Targets":       ConsistOf(testTargetIP),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(klbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(geo1KlbHostName),
						"Targets":       ConsistOf(cluster1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "routingpolicy", Value: "weighted"},
							{Name: cluster1KlbHostName, Value: "200"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(klbHostName),
						"Targets":       ConsistOf(geo1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "routingpolicy", Value: "geo"},
							{Name: geo1KlbHostName, Value: geoCode},
						}),
					})),
				))
			}
			if testDNSProvider == "aws" {
				Expect(zoneEndpoints).To(HaveLen(5))
				Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(cluster1KlbHostName),
						"Targets":       ConsistOf(testTargetIP),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(klbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(geo1KlbHostName),
						"Targets":       ConsistOf(cluster1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(cluster1KlbHostName),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "alias", Value: "false"},
							{Name: "aws/weight", Value: "200"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(klbHostName),
						"Targets":       ConsistOf(geo1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(geoCode),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "alias", Value: "false"},
							{Name: "aws/geolocation-country-code", Value: "US"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(klbHostName),
						"Targets":       ConsistOf(geo1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal("default"),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "alias", Value: "false"},
							{Name: "aws/geolocation-country-code", Value: "*"},
						}),
					})),
				))
			}

		})
	})

	Context("with ownerID", func() {
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
						OwnerID: ptr.To("test-owner"),
					},
				}
				err := k8sClient.Create(ctx, dnsRecord)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func(g Gomega, ctx context.Context) {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(dnsRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
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

				By("ensuring zone records are created as expected")
				testProvider, err := providerForManagedZone(ctx, testManagedZone)
				Expect(err).NotTo(HaveOccurred())
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				Expect(err).NotTo(HaveOccurred())

				Expect(zoneEndpoints).To(HaveLen(2))
				Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(testTargetIP),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-a-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				))
			})
		})

		Context("loadbalanced", func() {
			It("makes available a hostname that can be resolved", func(ctx SpecContext) {
				By("creating a dns record")
				testTargetIP := "127.0.0.1"

				klbHostName := "klb." + testHostname
				geo1KlbHostName := strings.ToLower(geoCode) + "." + klbHostName
				cluster1KlbHostName := "cluster1." + klbHostName

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
						OwnerID: ptr.To("test-owner"),
					},
				}
				err := k8sClient.Create(ctx, dnsRecord)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func(g Gomega, ctx context.Context) {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(dnsRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
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

				By("ensuring zone records are created as expected")
				testProvider, err := providerForManagedZone(ctx, testManagedZone)
				Expect(err).NotTo(HaveOccurred())
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				Expect(err).NotTo(HaveOccurred())
				if testDNSProvider == "gcp" {
					Expect(zoneEndpoints).To(HaveLen(8))
					Expect(zoneEndpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(cluster1KlbHostName),
							"Targets":       ConsistOf(testTargetIP),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(testHostname),
							"Targets":       ConsistOf(klbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(geo1KlbHostName),
							"Targets":       ConsistOf(cluster1KlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "routingpolicy", Value: "weighted"},
								{Name: cluster1KlbHostName, Value: "200"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(klbHostName),
							"Targets":       ConsistOf(geo1KlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "routingpolicy", Value: "geo"},
								{Name: geo1KlbHostName, Value: geoCode},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-a-" + cluster1KlbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + testHostname),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + geo1KlbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + klbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
					))
				}
				if testDNSProvider == "aws" {
					Expect(zoneEndpoints).To(HaveLen(10))
					Expect(zoneEndpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(cluster1KlbHostName),
							"Targets":       ConsistOf(testTargetIP),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(testHostname),
							"Targets":       ConsistOf(klbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(geo1KlbHostName),
							"Targets":       ConsistOf(cluster1KlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(cluster1KlbHostName),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "alias", Value: "false"},
								{Name: "aws/weight", Value: "200"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(klbHostName),
							"Targets":       ConsistOf(geo1KlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(geoCode),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "alias", Value: "false"},
								{Name: "aws/geolocation-country-code", Value: "US"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(klbHostName),
							"Targets":       ConsistOf(geo1KlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal("default"),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "alias", Value: "false"},
								{Name: "aws/geolocation-country-code", Value: "*"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-a-" + cluster1KlbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + testHostname),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + geo1KlbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(cluster1KlbHostName),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "aws/weight", Value: "200"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + klbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(geoCode),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "aws/geolocation-country-code", Value: "US"},
							}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + klbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=test-owner\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal("default"),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "aws/geolocation-country-code", Value: "*"},
							}),
						})),
					))
				}

			})
		})
	})

})
