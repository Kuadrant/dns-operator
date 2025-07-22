//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/hash"
	"github.com/kuadrant/dns-operator/internal/provider"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Test Cases covering a single DNSRecord updating a set of records in a zone
var _ = Describe("Single Record Test", Labels{"single_record"}, func() {
	// testID is a randomly generated identifier for the test
	// it is used to name resources and/or namespaces so different
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var k8sClient client.Client
	var testDNSProviderSecret *v1.Secret
	var geoCode string

	var dnsRecord *v1alpha1.DNSRecord
	var dnsRecords []*v1alpha1.DNSRecord

	var dynamicClient dynamic.Interface

	BeforeEach(func(ctx SpecContext) {
		testID = "t-single-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		k8sClient = testClusters[0].k8sClient
		testDNSProviderSecret = testClusters[0].testDNSProviderSecrets[0]
		if testDNSProvider == provider.DNSProviderGCP.String() {
			geoCode = "us-east1"
		} else if testDNSProvider == provider.DNSProviderAzure.String() {
			geoCode = "GEO-NA"
		} else {
			geoCode = "US"
		}
		config, err := rest.InClusterConfig()
		Expect(err).ToNot(HaveOccurred())

		dynamicClient, err = dynamic.NewForConfig(config)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			By("ensuring dns record is deleted")
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By("checking dns record is removed")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, time.Second, ctx).Should(Succeed())
		}
		if len(dnsRecords) > 0 {
			for _, record := range dnsRecords {
				err := k8sClient.Delete(ctx, record, client.PropagationPolicy(metav1.DeletePropagationForeground))
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}
		}
	})

	It("correctly handles wildcard rootHost values", func(ctx SpecContext) {
		if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			Skip("coredns does not currently handle wildcard rootHosts correctly!!")
		}
		testTargetIP := "127.0.0.1"
		testTargetIP2 := "127.0.0.2"
		testWCHostname := "*." + testHostname
		testHostname2 := "foo." + testHostname
		dnsRecord = &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testID,
				Namespace: testDNSProviderSecret.Namespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost: testWCHostname,
				ProviderRef: &v1alpha1.ProviderRef{
					Name: testProviderSecretName,
				},
				Endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName: testWCHostname,
						Targets: []string{
							testTargetIP,
						},
						RecordType: "A",
						RecordTTL:  60,
					},
					{
						DNSName: testHostname2,
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

		By("creating dnsrecord " + dnsRecord.Name)
		err := k8sClient.Create(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		By("checking " + dnsRecord.Name + " becomes ready")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
					"Status": Equal(metav1.ConditionTrue),
				})),
			)
			if txtRegistryEnabled {
				g.Expect(dnsRecord.Status.DomainOwners).NotTo(BeEmpty())
				g.Expect(dnsRecord.Status.DomainOwners).To(ContainElement(dnsRecord.GetUIDHash()))
			} else {
				g.Expect(dnsRecord.Status.DomainOwners).To(BeEmpty())
			}
		}, recordsReadyMaxDuration, 10*time.Second, ctx).Should(Succeed())

		By("checking " + dnsRecord.Name + " ownerID is set correctly")
		Expect(dnsRecord.Spec.OwnerID).To(BeEmpty())
		Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
		Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))

		By("ensuring zone records are created as expected")
		testProvider, err := ProviderForDNSRecord(ctx, dnsRecord, k8sClient, dynamicClient)
		Expect(err).NotTo(HaveOccurred())
		zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
		Expect(err).NotTo(HaveOccurred())

		expectedElementMatchers := []types.GomegaMatcher{
			PointTo(MatchFields(IgnoreExtras, Fields{
				"DNSName":       Equal(testWCHostname),
				"Targets":       ConsistOf(testTargetIP),
				"RecordType":    Equal("A"),
				"SetIdentifier": Equal(""),
				"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
			})),
			PointTo(MatchFields(IgnoreExtras, Fields{
				"DNSName":       Equal(testHostname2),
				"Targets":       ConsistOf(testTargetIP2),
				"RecordType":    Equal("A"),
				"SetIdentifier": Equal(""),
				"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
			})),
		}
		expectedDomainOwnersMatcher := BeEmpty()
		if txtRegistryEnabled {
			expectedElementMatchers = append(expectedElementMatchers,
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-wildcard." + testHostname),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + testHostname2),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				})),
			)
			expectedDomainOwnersMatcher = ConsistOf(dnsRecord.GetUIDHash())
		}

		By("ensuring zone records are created as expected and owners set as expected")
		Expect(zoneEndpoints).To(HaveLen(len(expectedElementMatchers)))
		Expect(zoneEndpoints).To(ContainElements(expectedElementMatchers))
		Expect(dnsRecord.Status.DomainOwners).To(expectedDomainOwnersMatcher)
	})

	Context("simple", Labels{"simple"}, func() {
		It("makes available a hostname that can be resolved", Labels{"happy"}, func(ctx SpecContext) {
			testTargetIP := "127.0.0.1"
			dnsRecord = &v1alpha1.DNSRecord{
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
							DNSName: testHostname,
							Targets: []string{
								testTargetIP,
							},
							RecordType: "A",
							RecordTTL:  60,
						},
					},
					HealthCheck: nil,
				},
			}
			By("creating dnsrecord " + dnsRecord.Name)
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("checking " + dnsRecord.Name + " becomes ready")
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

			By("checking " + dnsRecord.Name + " ownerID is set correctly")
			Expect(dnsRecord.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))

			By("ensuring zone records are created as expected")
			testProvider, err := ProviderForDNSRecord(ctx, dnsRecord, k8sClient, dynamicClient)
			Expect(err).NotTo(HaveOccurred())
			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())

			expectedElementMatchers := []types.GomegaMatcher{
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(testHostname),
					"Targets":       ConsistOf(testTargetIP),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				})),
			}
			expectedDomainOwnersMatcher := BeEmpty()
			if txtRegistryEnabled {
				expectedElementMatchers = append(expectedElementMatchers,
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				)
				expectedDomainOwnersMatcher = ConsistOf(dnsRecord.GetUIDHash())
			}

			Expect(zoneEndpoints).To(HaveLen(len(expectedElementMatchers)))
			Expect(zoneEndpoints).To(ContainElements(expectedElementMatchers))
			Expect(dnsRecord.Status.DomainOwners).To(expectedDomainOwnersMatcher)

			By("ensuring the authoritative nameserver resolves the hostname")
			resolvers, authoritative := ResolversForDomainNameAndProvider(testZoneDomainName, testDNSProviderSecret)
			Expect(resolvers).To(Not(BeEmpty()))

			if !authoritative {
				//we need to wait a minute to allow the records to propagate if not using an authoritative nameserver
				Consistently(func(g Gomega, ctx context.Context) {
					g.Expect(true).To(BeTrue())
				}, 1*time.Minute, 1*time.Minute, ctx).Should(Succeed())
			}

			Eventually(func(g Gomega, ctx context.Context) {
				var err error
				var ips []string
				for _, resolver := range resolvers {
					ips, err = resolver.LookupHost(ctx, testHostname)
					if err == nil {
						break
					}
				}
				g.Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("[debug] ips: %v\n", ips)
				g.Expect(ips).To(Or(ContainElements(testTargetIP)))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())
		})
	})

	Context("loadbalanced", Labels{"loadbalanced"}, func() {
		It("makes available a hostname that can be resolved", Labels{"happy"}, func(ctx SpecContext) {
			testTargetIP := "127.0.0.1"

			klbHostName := "klb." + testHostname
			geo1KlbHostName := strings.ToLower(geoCode) + "." + klbHostName
			cluster1KlbHostName := "cluster1." + klbHostName

			dnsRecord = &v1alpha1.DNSRecord{
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
					HealthCheck: nil,
				},
			}
			By("creating dnsrecord " + dnsRecord.Name)
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("checking " + dnsRecord.Name + " becomes ready")
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

			By("checking " + dnsRecord.Name + " ownerID is set correctly")
			Expect(dnsRecord.Spec.OwnerID).To(BeEmpty())
			Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
			Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))

			By("ensuring zone records are created as expected")
			testProvider, err := ProviderForDNSRecord(ctx, dnsRecord, k8sClient, dynamicClient)
			Expect(err).NotTo(HaveOccurred())
			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			if testProvider.Name() == provider.DNSProviderGCP {
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
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + cluster1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + geo1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + klbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				))
			}
			if testProvider.Name() == provider.DNSProviderAzure {
				Expect(zoneEndpoints).To(HaveLen(8))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(cluster1KlbHostName),
						"Targets":       ConsistOf(testTargetIP),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(testHostname),
						"Targets":       ConsistOf(klbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(geo1KlbHostName),
						"Targets":       ConsistOf(cluster1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "routingpolicy", Value: "Weighted"},
							{Name: cluster1KlbHostName, Value: "200"},
						}),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(klbHostName),
						"Targets":       ConsistOf(geo1KlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "routingpolicy", Value: "Geographic"},
							{Name: geo1KlbHostName, Value: "WORLD"},
						}),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + cluster1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + geo1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
				Expect(zoneEndpoints).To(ContainElement(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + klbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
			}
			if testProvider.Name() == provider.DNSProviderAWS {
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
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-a-" + cluster1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + testHostname),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + geo1KlbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(cluster1KlbHostName),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "aws/weight", Value: "200"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + klbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(geoCode),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "aws/geolocation-country-code", Value: "US"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-" + hash.ToBase36HashLen(dnsRecord.Status.OwnerID, 8) + "-cname-" + klbHostName),
						"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + dnsRecord.Status.OwnerID + ",external-dns/version=1\""),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal("default"),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "aws/geolocation-country-code", Value: "*"},
						}),
					})),
				))
			}
			if testProvider.Name() == provider.DNSProviderCoreDNS {
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
						"DNSName":    Equal(geo1KlbHostName),
						"Targets":    ConsistOf(cluster1KlbHostName),
						"RecordType": Equal("CNAME"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "weight", Value: "200"},
						}),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":    Equal(klbHostName),
						"Targets":    ConsistOf(geo1KlbHostName),
						"RecordType": Equal("CNAME"),
						"RecordTTL":  Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "geo-code", Value: "US"},
						}),
					})),
				))
			}

			By("ensuring the authoritative nameserver resolves the hostname")
			resolvers, authoritative := ResolversForDomainNameAndProvider(testZoneDomainName, testDNSProviderSecret)
			Expect(resolvers).To(Not(BeEmpty()))

			if !authoritative {
				//we need to wait a minute to allow the records to propagate if not using an authoritative nameserver
				Consistently(func(g Gomega, ctx context.Context) {
					g.Expect(true).To(BeTrue())
				}, 1*time.Minute, 1*time.Minute, ctx).Should(Succeed())
			}

			Eventually(func(g Gomega, ctx context.Context) {
				var err error
				var ips []string
				for _, resolver := range resolvers {
					ips, err = resolver.LookupHost(ctx, testHostname)
					if err == nil {
						break
					}
				}
				g.Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("[debug] ips: %v\n", ips)
				g.Expect(ips).To(Or(ContainElements(testTargetIP)))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())
		})

		It("handles multiple DNS Records for the same zone at the same time", func(ctx SpecContext) {
			By("creating " + strconv.Itoa(testConcurrentRecords) + " DNS Records")
			SetTestEnv("testNamespace", testDNSProviderSecret.Namespace)

			if testDNSProvider == provider.DNSProviderGCP.String() {
				SetTestEnv("testGeoCode", "europe-west1")
			} else if testDNSProvider == provider.DNSProviderAzure.String() {
				SetTestEnv("testGeoCode", "GEO-EU")
			} else {
				SetTestEnv("testGeoCode", "GEO-EU")
			}

			SetTestEnv("TEST_DNS_PROVIDER_SECRET_NAME", testDNSProviderSecret.Name)
			for i := 1; i <= testConcurrentRecords; i++ {
				testRecord := &v1alpha1.DNSRecord{}
				SetTestEnv("testHostname", strings.Join([]string{testID + "-" + strconv.Itoa(i), testDomainName}, "."))
				SetTestEnv("testID", testID+"-"+strconv.Itoa(i))

				err := ResourceFromFile("./fixtures/healthcheck_test/geo-dnsrecord-healthchecks.yaml", testRecord, GetTestEnv)
				By("creating DNS Record for host " + GetTestEnv("testHostname"))
				Expect(err).ToNot(HaveOccurred())
				err = k8sClient.Create(ctx, testRecord)
				Expect(err).ToNot(HaveOccurred())
				dnsRecords = append(dnsRecords, testRecord)
			}

			startedTime := time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, record := range dnsRecords {
					testRecord := &v1alpha1.DNSRecord{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(record), testRecord)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(testRecord.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
				}
			}, recordsReadyMaxDuration*time.Duration(testConcurrentRecords), 5*time.Second, ctx).Should(Succeed())
			By(fmt.Sprintf("Total time for %d records to become ready: %s", testConcurrentRecords, time.Now().Sub(startedTime)))
		})
	})

})
