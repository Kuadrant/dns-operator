//go:build e2e_multi_instance

package multi_instance

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
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

	var geoCode1 string
	var geoCode2 string

	var testRecords []*testDNSRecord

	BeforeEach(func(ctx SpecContext) {
		testID = "t-multi-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		testRecords = []*testDNSRecord{}

		if testDNSProvider == "google" {
			geoCode1 = "us-east1"
			geoCode2 = "europe-west1"
		} else {
			geoCode1 = "US"
			geoCode2 = "EU"
		}
	})

	AfterEach(func(ctx SpecContext) {
		By("ensuring all dns records are deleted")
		for _, tr := range testRecords {
			err := k8sClient.Delete(ctx, tr.record,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}

		By("checking all dns records are removed")
		Eventually(func(g Gomega, ctx context.Context) {
			for _, tr := range testRecords {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}
		}, time.Minute, 10*time.Second, ctx).Should(Succeed())
	})

	Context("simple", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			By("creating a simple dnsrecord in each managed zone")
			for i, managedZone := range testManagedZones {
				config := testConfig{
					testTargetIP: fmt.Sprintf("127.0.0.%d", i+1),
				}
				record := &v1alpha1.DNSRecord{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testID,
						Namespace: managedZone.Namespace,
					},
					Spec: v1alpha1.DNSRecordSpec{
						RootHost: testHostname,
						ManagedZoneRef: &v1alpha1.ManagedZoneReference{
							Name: managedZone.Name,
						},
						Endpoints: []*externaldnsendpoint.Endpoint{
							{
								DNSName: testHostname,
								Targets: []string{
									config.testTargetIP,
								},
								RecordType: "A",
								RecordTTL:  60,
							},
						},
						HealthCheck: nil,
					},
				}

				By(fmt.Sprintf("creating dns record [name: `%s`, namespace: `%s`, managedZone: `%s`, endpoint: [dnsname: `%s`, target: `%s`]]", record.Name, record.Namespace, managedZone.Name, testHostname, config.testTargetIP))
				err := k8sClient.Create(ctx, record)
				Expect(err).ToNot(HaveOccurred())

				testRecords = append(testRecords, &testDNSRecord{
					managedZone: managedZone,
					record:      record,
					config:      config,
				})
			}
			Expect(testRecords).To(HaveLen(len(testManagedZones)))

			By("checking all dns records become ready")
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
				}
			}, time.Minute, 10*time.Second, ctx).Should(Succeed())

			By("ensuring managedZone records are created as expected")
			testProvider, err := ProviderForManagedZone(ctx, testRecords[0].managedZone, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			Expect(zoneEndpoints).To(HaveLen(2))

			var allOwners = []string{}
			var allTargetIps = []string{}
			for i := range testRecords {
				allOwners = append(allOwners, testRecords[i].record.Status.OwnerID)
				allTargetIps = append(allTargetIps, testRecords[i].config.testTargetIP)
			}

			By("checking all target ips are present")
			Expect(zoneEndpoints).To(ContainElements(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(testHostname),
					"Targets":       ConsistOf(allTargetIps),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				})),
			))

			By("checking all owner references are present")
			for _, owner := range allOwners {
				Expect(zoneEndpoints).To(ContainElements(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-a-" + testHostname),
						"Targets":       ContainElement(ContainSubstring(owner)),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					})),
				))
			}

			By("checking the authoritative nameserver resolves the hostname")
			// speed up things by using the authoritative nameserver
			authoritativeResolver := ResolverForDomainName(testZoneDomainName)
			Eventually(func(g Gomega, ctx context.Context) {
				ips, err := authoritativeResolver.LookupHost(ctx, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ips).To(ContainElements(allTargetIps))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			//Test Deletion of one of the records
			recordToDelete := testRecords[0]
			lastRecord := len(testRecords) == 1
			By(fmt.Sprintf("deleting dns record [name: `%s` namespace: `%s`]", recordToDelete.record.Name, recordToDelete.record.Namespace))
			err = k8sClient.Delete(ctx, recordToDelete.record,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking dns record [name: `%s` namespace: `%s`] is removed", recordToDelete.record.Name, recordToDelete.record.Namespace))
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(recordToDelete.record), recordToDelete.record)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, 10*time.Second, 1*time.Second, ctx).Should(Succeed())

			By("ensuring zone records are updated as expected")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				if lastRecord {
					g.Expect(zoneEndpoints).To(HaveLen(0))
				} else {
					g.Expect(zoneEndpoints).To(HaveLen(2))
					By(fmt.Sprintf("checking ip `%s` and owner `%s` are removed", recordToDelete.config.testTargetIP, recordToDelete.record.Status.OwnerID))
					g.Expect(zoneEndpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(testHostname),
							"Targets":       Not(ContainElement(recordToDelete.config.testTargetIP)),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-a-" + testHostname),
							"Targets":       Not(ContainElement(ContainSubstring(recordToDelete.record.Status.OwnerID))),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						})),
					))
				}
			}, 5*time.Second, 1*time.Second, ctx).Should(Succeed())

		})

	})

	Context("loadbalanced", func() {
		It("makes available a hostname that can be resolved", func(ctx SpecContext) {
			testGeoRecords := map[string][]*testDNSRecord{}

			By("creating a loadbalanced dnsrecord in each managed zone")
			for i, managedZone := range testManagedZones {

				var geoCode string
				if i%2 == 0 {
					geoCode = geoCode1
				} else {
					geoCode = geoCode2
				}

				klbHostName := "klb." + testHostname
				geoKlbHostName := strings.ToLower(geoCode) + "." + klbHostName
				defaultGeoKlbHostName := strings.ToLower(geoCode1) + "." + klbHostName
				clusterKlbHostName := fmt.Sprintf("cluster%d.%s", i+1, klbHostName)

				config := testConfig{
					testTargetIP:       fmt.Sprintf("127.0.0.%d", i+1),
					testGeoCode:        geoCode,
					testDefaultGeoCode: geoCode1,
				}

				record := &v1alpha1.DNSRecord{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testID,
						Namespace: managedZone.Namespace,
					},
					Spec: v1alpha1.DNSRecordSpec{
						RootHost: testHostname,
						ManagedZoneRef: &v1alpha1.ManagedZoneReference{
							Name: managedZone.Name,
						},
						Endpoints: []*externaldnsendpoint.Endpoint{
							{
								DNSName: clusterKlbHostName,
								Targets: []string{
									config.testTargetIP,
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
								DNSName: geoKlbHostName,
								Targets: []string{
									clusterKlbHostName,
								},
								RecordType:    "CNAME",
								RecordTTL:     60,
								SetIdentifier: clusterKlbHostName,
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
									geoKlbHostName,
								},
								RecordType:    "CNAME",
								RecordTTL:     300,
								SetIdentifier: config.testGeoCode,
								ProviderSpecific: externaldnsendpoint.ProviderSpecific{
									{
										Name:  "geo-code",
										Value: config.testGeoCode,
									},
								},
							},
							{
								DNSName: klbHostName,
								Targets: []string{
									defaultGeoKlbHostName,
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

				By(fmt.Sprintf("creating dns record [name: `%s`, namespace: `%s`, managedZone: `%s`, endpoint: [dnsname: `%s`, target: `%s`, geoCode: `%s`]]", record.Name, record.Namespace, managedZone.Name, testHostname, config.testTargetIP, config.testGeoCode))
				err := k8sClient.Create(ctx, record)
				Expect(err).ToNot(HaveOccurred())
				tr := &testDNSRecord{
					managedZone: managedZone,
					record:      record,
					config:      config,
				}
				testRecords = append(testRecords, tr)
				testGeoRecords[config.testGeoCode] = append(testGeoRecords[config.testGeoCode], tr)
			}
			Expect(testRecords).To(HaveLen(len(testManagedZones)))

			By("checking all dns records become ready")
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
				}
			}, time.Minute, 10*time.Second, ctx).Should(Succeed())

			By("ensuring managedZone records are created as expected")
			testProvider, err := ProviderForManagedZone(ctx, testRecords[0].managedZone, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			var expectedLen int
			if testDNSProvider == "google" {
				expectedLen = (2 + len(testGeoRecords) + len(testRecords)) * 2
				Expect(zoneEndpoints).To(HaveLen(expectedLen))
			} else if testDNSProvider == "aws" {
				expectedLen = (2 + len(testGeoRecords) + (len(testRecords) * 2)) * 2
				Expect(zoneEndpoints).To(HaveLen(expectedLen))
			}

		})
	})

})
