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
)

// Test Cases covering multiple DNSRecords updating a set of records in a zone
var _ = Describe("Multi Record Test", func() {
	// testID is a randomly generated identifier for the test
	// it is used to name resources and/or namespaces so different
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var dnsRecord1 *v1alpha1.DNSRecord
	var dnsRecord2 *v1alpha1.DNSRecord
	var geoCode1 string
	var geoCode2 string

	BeforeEach(func(ctx SpecContext) {
		testID = "t-multi-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")

		if testDNSProvider == "gcp" {
			geoCode1 = "us-east1"
			geoCode2 = "europe-west1"
		} else {
			geoCode1 = "US"
			geoCode2 = "EU"
		}
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord1 != nil {
			err := k8sClient.Delete(ctx, dnsRecord1,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if dnsRecord2 != nil {
			err := k8sClient.Delete(ctx, dnsRecord2,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	Context("simple", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating two dns records")
			testTargetIP1 := "127.0.0.1"
			testTargetIP2 := "127.0.0.2"

			dnsRecord1 = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testID + "-1",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: testHostname,
							Targets: []string{
								testTargetIP1,
							},
							RecordType: "A",
							RecordTTL:  60,
						},
					},
					HealthCheck: nil,
				},
			}

			dnsRecord2 = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testID + "-2",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: testHostname,
							Targets: []string{
								testTargetIP2,
							},
							RecordType: "A",
							RecordTTL:  60,
						},
					},
					HealthCheck: nil,
				},
			}

			By("creating dnsrecord " + dnsRecord1.Name)
			err := k8sClient.Create(ctx, dnsRecord1)
			Expect(err).ToNot(HaveOccurred())

			By("creating dnsrecord " + dnsRecord2.Name)
			err = k8sClient.Create(ctx, dnsRecord2)
			Expect(err).ToNot(HaveOccurred())

			By("checking dns records becomes ready")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord1.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord2.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, time.Minute, 10*time.Second, ctx).Should(Succeed())

			By("checking dns records ownerID is set correctly")
			Expect(dnsRecord1.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord2.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord1.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord2.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord1.Status.OwnerID).To(Equal(dnsRecord1.GetUIDHash()))
			Expect(dnsRecord2.Status.OwnerID).To(Equal(dnsRecord2.GetUIDHash()))

			testProvider, err := providerForManagedZone(ctx, testManagedZone)
			Expect(err).NotTo(HaveOccurred())

			By("ensuring zone records are created as expected")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(2))
				g.Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(testTargetIP1, testTargetIP2),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("kuadrant-a-" + testHostname),
						"Targets": ContainElement(And(
							ContainSubstring("heritage=external-dns,external-dns/owner="),
							ContainSubstring(dnsRecord1.Status.OwnerID),
							ContainSubstring(dnsRecord2.Status.OwnerID),
						)),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring the authoritative nameserver resolves the hostname")
			// speed up things by using the authoritative nameserver
			authoritativeResolver := ResolverForDomainName(testZoneDomainName)
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ips).To(ConsistOf(testTargetIP1, testTargetIP2))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("deleting dnsrecord " + dnsRecord2.Name)
			err = k8sClient.Delete(ctx, dnsRecord2,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By("checking dnsrecord " + dnsRecord2.Name + " is removed")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring zone records are updated as expected")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(2))
				g.Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(testTargetIP1),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-a-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				))
			}, 5*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring the authoritative nameserver resolves the hostname")
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ips).To(ConsistOf(testTargetIP1))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("deleting dnsrecord " + dnsRecord1.Name)
			err = k8sClient.Delete(ctx, dnsRecord1,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By("checking dnsrecord " + dnsRecord1.Name + " is removed")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring zone records are all removed as expected")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(0))
			}, 5*time.Second, 1*time.Second, ctx).Should(Succeed())
		})
	})

	Context("loadbalanced", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating two dns records")
			klbHostName := "klb." + testHostname

			testTargetIP1 := "127.0.0.1"
			geo1KlbHostName := strings.ToLower(geoCode1) + "." + klbHostName
			cluster1KlbHostName := "cluster1." + klbHostName

			testTargetIP2 := "127.0.0.2"
			geo2KlbHostName := strings.ToLower(geoCode2) + "." + klbHostName
			cluster2KlbHostName := "cluster2." + klbHostName

			dnsRecord1 = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testID + "-1",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: cluster1KlbHostName,
							Targets: []string{
								testTargetIP1,
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
							SetIdentifier: geoCode1,
							ProviderSpecific: externaldnsendpoint.ProviderSpecific{
								{
									Name:  "geo-code",
									Value: geoCode1,
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
					HealthCheck: nil,
				},
			}

			dnsRecord2 = &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testID + "-2",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: testHostname,
					ManagedZoneRef: &v1alpha1.ManagedZoneReference{
						Name: testManagedZoneName,
					},
					Endpoints: []*externaldnsendpoint.Endpoint{
						{
							DNSName: cluster2KlbHostName,
							Targets: []string{
								testTargetIP2,
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
							DNSName: geo2KlbHostName,
							Targets: []string{
								cluster2KlbHostName,
							},
							RecordType:    "CNAME",
							RecordTTL:     60,
							SetIdentifier: cluster2KlbHostName,
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
								geo2KlbHostName,
							},
							RecordType:    "CNAME",
							RecordTTL:     300,
							SetIdentifier: geoCode2,
							ProviderSpecific: externaldnsendpoint.ProviderSpecific{
								{
									Name:  "geo-code",
									Value: geoCode2,
								},
							},
						},
						//Note this dnsRecord has no default geo ....
					},
					HealthCheck: nil,
				},
			}

			By("creating dnsrecord " + dnsRecord1.Name)
			err := k8sClient.Create(ctx, dnsRecord1)
			Expect(err).ToNot(HaveOccurred())

			By("creating dnsrecord " + dnsRecord2.Name)
			err = k8sClient.Create(ctx, dnsRecord2)
			Expect(err).ToNot(HaveOccurred())

			By("checking the dns records become ready")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord1.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord2.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
			}, time.Minute, 10*time.Second, ctx).Should(Succeed())

			By("checking dns records ownerID is set correctly")
			Expect(dnsRecord1.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord2.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord1.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord2.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord1.Status.OwnerID).To(Equal(dnsRecord1.GetUIDHash()))
			Expect(dnsRecord2.Status.OwnerID).To(Equal(dnsRecord2.GetUIDHash()))

			testProvider, err := providerForManagedZone(ctx, testManagedZone)
			Expect(err).NotTo(HaveOccurred())

			By("ensuring zone records are created as expected")
			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			if testDNSProvider == "gcp" {
				Expect(zoneEndpoints).To(HaveLen(12))
				//Main Records
				By("checking endpoint " + testHostname)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(testHostname),
					"Targets":       ConsistOf(klbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				By("checking endpoint " + klbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(klbHostName),
					"Targets":       ConsistOf(geo1KlbHostName, geo2KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": ContainElements(
						externaldnsendpoint.ProviderSpecificProperty{Name: "routingpolicy", Value: "geo"},
						externaldnsendpoint.ProviderSpecificProperty{Name: geo1KlbHostName, Value: geoCode1},
						externaldnsendpoint.ProviderSpecificProperty{Name: geo2KlbHostName, Value: geoCode2},
					),
				}))))
				By("checking endpoint " + geo1KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(geo1KlbHostName),
					"Targets":       ConsistOf(cluster1KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					"ProviderSpecific": ContainElements(
						externaldnsendpoint.ProviderSpecificProperty{Name: "routingpolicy", Value: "weighted"},
						externaldnsendpoint.ProviderSpecificProperty{Name: cluster1KlbHostName, Value: "200"},
					),
				}))))
				By("checking endpoint " + geo2KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(geo2KlbHostName),
					"Targets":       ConsistOf(cluster2KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					"ProviderSpecific": ContainElements(
						externaldnsendpoint.ProviderSpecificProperty{Name: "routingpolicy", Value: "weighted"},
						externaldnsendpoint.ProviderSpecificProperty{Name: cluster2KlbHostName, Value: "200"},
					),
				}))))
				By("checking endpoint " + cluster1KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(cluster1KlbHostName),
					"Targets":       ConsistOf(testTargetIP1),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				}))))
				By("checking endpoint " + cluster2KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(cluster2KlbHostName),
					"Targets":       ConsistOf(testTargetIP2),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				}))))
				//Txt Records
				By("checking TXT owner endpoints")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal("kuadrant-cname-" + testHostname),
					"Targets": ContainElement(And(
						ContainSubstring("heritage=external-dns,external-dns/owner="),
						ContainSubstring(dnsRecord1.Status.OwnerID),
						ContainSubstring(dnsRecord2.Status.OwnerID),
					)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal("kuadrant-cname-" + klbHostName),
					"Targets": ContainElement(And(
						ContainSubstring("heritage=external-dns,external-dns/owner="),
						ContainSubstring(dnsRecord1.Status.OwnerID),
						ContainSubstring(dnsRecord2.Status.OwnerID),
					)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + geo1KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + geo2KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord2.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-a-" + cluster1KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-a-" + cluster2KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord2.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
			}
			if testDNSProvider == "aws" {
				Expect(zoneEndpoints).To(HaveLen(16))
				//Main Records
				By("checking endpoint " + testHostname)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(testHostname),
					"Targets":       ConsistOf(klbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				By("checking endpoint " + klbHostName + " - " + geoCode1)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(klbHostName),
					"Targets":       ConsistOf(geo1KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(geoCode1),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/geolocation-country-code", Value: "US"},
					}),
				}))))
				By("checking endpoint " + klbHostName + " - " + geoCode2)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(klbHostName),
					"Targets":       ConsistOf(geo2KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(geoCode2),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/geolocation-continent-code", Value: "EU"},
					}),
				}))))
				By("checking endpoint " + klbHostName + " - " + geoCode1)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(klbHostName),
					"Targets":       ConsistOf(geo1KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal("default"),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/geolocation-country-code", Value: "*"},
					}),
				}))))
				By("checking endpoint " + geo1KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(geo1KlbHostName),
					"Targets":       ConsistOf(cluster1KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(cluster1KlbHostName),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/weight", Value: "200"},
					}),
				}))))
				By("checking endpoint " + geo2KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(geo2KlbHostName),
					"Targets":       ConsistOf(cluster2KlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal(cluster2KlbHostName),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/weight", Value: "200"},
					}),
				}))))
				By("checking endpoint " + cluster1KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(cluster1KlbHostName),
					"Targets":       ConsistOf(testTargetIP1),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				}))))
				By("checking endpoint " + cluster2KlbHostName)
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(cluster2KlbHostName),
					"Targets":       ConsistOf(testTargetIP2),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				}))))
				//Txt Records
				By("checking TXT owner endpoints")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName": Equal("kuadrant-cname-" + testHostname),
					"Targets": ContainElement(And(
						ContainSubstring("heritage=external-dns,external-dns/owner="),
						ContainSubstring(dnsRecord1.Status.OwnerID),
						ContainSubstring(dnsRecord2.Status.OwnerID),
					)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(geoCode1),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/geolocation-country-code", Value: "US"},
					}),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord2.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(geoCode2),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/geolocation-continent-code", Value: "EU"},
					}),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal("default"),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/geolocation-country-code", Value: "*"},
					}),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + geo1KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(cluster1KlbHostName),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/weight", Value: "200"},
					}),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + geo2KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord2.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(cluster2KlbHostName),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/weight", Value: "200"},
					}),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-a-" + cluster1KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord1.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-a-" + cluster2KlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord2.Status.OwnerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
			}

			By("ensuring the authoritative nameserver resolves the hostname")
			// speed up things by using the authoritative nameserver
			authoritativeResolver := ResolverForDomainName(testZoneDomainName)
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("[debug] ips: %v\n", ips)
				g.Expect(ips).To(Or(ContainElement(testTargetIP1), ContainElement(testTargetIP2)))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("deleting dnsrecord " + dnsRecord2.Name)
			err = k8sClient.Delete(ctx, dnsRecord2,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By("checking dnsrecord " + dnsRecord2.Name + " is removed")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring the authoritative nameserver resolves the hostname")
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("[debug] ips: %v\n", ips)
				g.Expect(ips).To(ConsistOf(testTargetIP1))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			By("ensuring zone records are updated as expected")
			//ToDo mnairn Add more checks in here

			By("deleting dnsrecord " + dnsRecord1.Name)
			err = k8sClient.Delete(ctx, dnsRecord1,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By("checking dnsrecord " + dnsRecord1.Name + " is removed")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring zone records are all removed as expected")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(0))
			}, 5*time.Second, 1*time.Second, ctx).Should(Succeed())

		})
	})

})
