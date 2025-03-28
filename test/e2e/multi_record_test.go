//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Test Cases covering multiple DNSRecords updating a set of records in a zone
var _ = Describe("Multi Record Test", Labels{"multi_record"}, func() {
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
	var weighted string

	var testRecords []*testDNSRecord

	BeforeEach(func(ctx SpecContext) {
		testID = "t-multi-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		testRecords = []*testDNSRecord{}
		if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			Skip("skipping multi record for core dns")
		}
		if testDNSProvider == provider.DNSProviderGCP.String() {
			geoCode1 = "us-east1"
			geoCode2 = "europe-west1"
			weighted = "weighted"
		} else if testDNSProvider == provider.DNSProviderAzure.String() {
			geoCode1 = "GEO-NA"
			geoCode2 = "GEO-EU"
			weighted = "Weighted"
		} else {
			geoCode1 = "US"
			geoCode2 = "GEO-EU"
			weighted = "weighted"
		}
	})

	AfterEach(func(ctx SpecContext) {
		By("ensuring all dns records are deleted")
		for _, tr := range testRecords {
			err := tr.cluster.k8sClient.Delete(ctx, tr.record,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}

		By("checking all dns records are removed")
		Eventually(func(g Gomega, ctx context.Context) {
			for _, tr := range testRecords {
				err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}
		}, time.Minute, 10*time.Second, ctx).Should(Succeed())
	})

	Context("simple", Labels{"simple"}, func() {
		if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			Skip("skipping multi record for core dns")
		}
		It("creates and deletes distributed dns records", func(ctx SpecContext) {
			By(fmt.Sprintf("creating %d simple dnsrecords accross %d clusters", len(testNamespaces)*len(testClusters), len(testClusters)))
			for ci, tc := range testClusters {
				for si, s := range tc.testDNSProviderSecrets {
					config := testConfig{
						testTargetIP: fmt.Sprintf("127.0.%d.%d", ci+1, si+1),
					}
					record := &v1alpha1.DNSRecord{
						ObjectMeta: metav1.ObjectMeta{
							Name:      testID,
							Namespace: s.Namespace,
						},
						Spec: v1alpha1.DNSRecordSpec{
							RootHost: testHostname,
							ProviderRef: v1alpha1.ProviderRef{
								Name: s.Name,
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

					By(fmt.Sprintf("creating dns record [name: `%s`, namespace: `%s`, secret: `%s`, endpoint: [dnsname: `%s`, target: `%s`]] on cluster [name: `%s`]", record.Name, record.Namespace, s.Name, testHostname, config.testTargetIP, tc.name))
					err := tc.k8sClient.Create(ctx, record)
					Expect(err).ToNot(HaveOccurred())

					testRecords = append(testRecords, &testDNSRecord{
						cluster:           &testClusters[ci],
						dnsProviderSecret: s,
						record:            record,
						config:            config,
					})
				}
			}

			By(fmt.Sprintf("checking all dns records become ready within %s", recordsReadyMaxDuration))
			var allOwners = []string{}
			var allTargetIps = []string{}
			checkStarted := time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				allOwners = []string{}
				allTargetIps = []string{}
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					allOwners = append(allOwners, tr.record.GetUIDHash())
					allTargetIps = append(allTargetIps, tr.config.testTargetIP)
					g.Expect(tr.record.Status.DomainOwners).NotTo(BeEmpty())
					g.Expect(tr.record.Status.DomainOwners).To(ContainElement(tr.record.GetUIDHash()))
				}
				g.Expect(len(allOwners)).To(Equal(len(testRecords)))
			}, recordsReadyMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records became ready in %v\n", time.Since(checkStarted))

			By("checking provider zone records are created as expected")
			testProvider, err := ProviderForDNSRecord(ctx, testRecords[0].record, testClusters[0].k8sClient)
			Expect(err).NotTo(HaveOccurred())

			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			Expect(zoneEndpoints).To(HaveLen(2))

			By(fmt.Sprintf("checking each record has all(%v) domain owners present within %s", len(allOwners), recordsReadyMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.DomainOwners).To(ConsistOf(allOwners))
				}
			}, recordsReadyMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records updated in %v\n", time.Since(checkStarted))

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
			var resolver *net.Resolver
			if testDNSProvider == provider.DNSProviderAzure.String() {
				// cannot use authoratitive nameserver in Azure due to how traffic managers use CNAMEs on trafficmanager.net
				By("ensuring the hostname resolves")
				//we need to wait a minute to allow the records to propagate
				Consistently(func(g Gomega, ctx context.Context) {
					g.Expect(true).To(BeTrue())
				}, 1*time.Minute, 1*time.Minute, ctx).Should(Succeed())
			} else {
				By("ensuring the authoritative nameserver resolves the hostname")
				// speed up things by using the authoritative nameserver
				resolver = ResolverForDomainName(testZoneDomainName, "")
			}
			Eventually(func(g Gomega, ctx context.Context) {
				var err error
				var ips []string
				if resolver == nil {
					ips, err = net.LookupHost(testHostname)
				} else {
					ips, err = resolver.LookupHost(ctx, testHostname)
				}
				g.Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("[debug] ips: %v\n", ips)
				g.Expect(ips).To(Or(ContainElements(allTargetIps)))
			}, 300*time.Second, 10*time.Second, ctx).Should(Succeed())

			//Test Deletion of one of the records
			recordToDelete := testRecords[0]
			lastRecord := len(testRecords) == 1
			By(fmt.Sprintf("deleting dns record [name: `%s` namespace: `%s`]", recordToDelete.record.Name, recordToDelete.record.Namespace))
			err = recordToDelete.cluster.k8sClient.Delete(ctx, recordToDelete.record,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking dns record [name: `%s` namespace: `%s`] is removed within %s", recordToDelete.record.Name, recordToDelete.record.Namespace, recordsRemovedMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				err := recordToDelete.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(recordToDelete.record), recordToDelete.record)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("not found")))
			}, recordsRemovedMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] record removed in %v\n", time.Since(checkStarted))

			By("checking provider zone records are updated as expected")
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
			}, 5*time.Second, time.Second, ctx).Should(Succeed())

			//Remove deleted record owner from owners list
			allOwners = slices.DeleteFunc(allOwners, func(id string) bool {
				return id == recordToDelete.record.Status.OwnerID
			})
			//Remove deleted record from testRecords list
			testRecords = slices.DeleteFunc(testRecords, func(tr *testDNSRecord) bool {
				return tr.record.Status.OwnerID == recordToDelete.record.Status.OwnerID
			})

			By(fmt.Sprintf("checking remaining records have all(%v) domain owners updated within %s", len(allOwners), recordsReadyMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.DomainOwners).To(ConsistOf(allOwners))
				}
			}, recordsReadyMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] records updated in %v\n", time.Since(checkStarted))

			By("deleting all remaining dns records")
			for _, tr := range testRecords {
				err := tr.cluster.k8sClient.Delete(ctx, tr.record,
					client.PropagationPolicy(metav1.DeletePropagationForeground))
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}

			By(fmt.Sprintf("checking all dns records are removed within %s", recordsRemovedMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}
			}, recordsRemovedMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records removed in %v\n", time.Since(checkStarted))

			By("checking provider zone records are all removed")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(0))
			}, 5*time.Second, time.Second, ctx).Should(Succeed())

		})

	})

	Context("loadbalanced", Labels{"loadbalanced"}, func() {
		if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			Skip("skipping multi record for core dns")
		}
		It("creates and deletes distributed dns records", func(ctx SpecContext) {
			testGeoRecords := map[string][]testDNSRecord{}

			By(fmt.Sprintf("creating %d loadbalanced dnsrecords accross %d clusters", len(testNamespaces)*len(testClusters), len(testClusters)))
			for ci, tc := range testClusters {
				for mi, s := range tc.testDNSProviderSecrets {

					var geoCode string
					if (ci+mi)%2 == 0 {
						geoCode = geoCode1
					} else {
						geoCode = geoCode2
					}

					klbHostName := "klb." + testHostname
					geoKlbHostName := strings.ToLower(geoCode) + "." + klbHostName
					defaultGeoKlbHostName := strings.ToLower(geoCode1) + "." + klbHostName
					clusterKlbHostName := fmt.Sprintf("cluster%d-%d.%s", ci+1, mi+1, klbHostName)

					config := testConfig{
						testTargetIP:       fmt.Sprintf("127.0.%d.%d", ci+1, mi+1),
						testGeoCode:        geoCode,
						testDefaultGeoCode: geoCode1,
						hostnames: testHostnames{
							klb:           klbHostName,
							geoKlb:        geoKlbHostName,
							defaultGeoKlb: defaultGeoKlbHostName,
							clusterKlb:    clusterKlbHostName,
						},
					}

					record := &v1alpha1.DNSRecord{
						ObjectMeta: metav1.ObjectMeta{
							Name:      testID,
							Namespace: s.Namespace,
						},
						Spec: v1alpha1.DNSRecordSpec{
							RootHost: testHostname,
							ProviderRef: v1alpha1.ProviderRef{
								Name: s.Name,
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

					By(fmt.Sprintf("creating dns record [name: `%s`, namespace: `%s`, secret: `%s`, endpoint: [dnsname: `%s`, target: `%s`, geoCode: `%s`]] on cluster [name: `%s`]", record.Name, record.Namespace, s.Name, testHostname, config.testTargetIP, config.testGeoCode, tc.name))
					err := tc.k8sClient.Create(ctx, record)
					Expect(err).ToNot(HaveOccurred())
					tr := &testDNSRecord{
						cluster:           &testClusters[ci],
						dnsProviderSecret: s,
						record:            record,
						config:            config,
					}
					testRecords = append(testRecords, tr)
					testGeoRecords[config.testGeoCode] = append(testGeoRecords[config.testGeoCode], *tr)
				}
			}

			By(fmt.Sprintf("checking all dns records become ready within %s", recordsReadyMaxDuration))
			var allOwners = []string{}
			checkStarted := time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				allOwners = []string{}
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.Conditions).To(
						ContainElement(MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
					)
					allOwners = append(allOwners, tr.record.GetUIDHash())
					g.Expect(tr.record.Status.DomainOwners).To(Not(BeEmpty()))
				}
				g.Expect(len(allOwners)).To(Equal(len(testRecords)))
			}, recordsReadyMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records became ready in %v\n", time.Since(checkStarted))

			By("checking provider zone records are created as expected")
			testProvider, err := ProviderForDNSRecord(ctx, testRecords[0].record, testClusters[0].k8sClient)
			Expect(err).NotTo(HaveOccurred())

			zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
			Expect(err).NotTo(HaveOccurred())
			var expectedEndpointsLen int
			if testDNSProvider == provider.DNSProviderGCP.String() {
				expectedEndpointsLen = (2 + len(testGeoRecords) + len(testRecords)) * 2
				Expect(zoneEndpoints).To(HaveLen(expectedEndpointsLen))
			} else if testDNSProvider == provider.DNSProviderAzure.String() {
				expectedEndpointsLen = (2 + len(testGeoRecords) + len(testRecords)) * 2
				Expect(zoneEndpoints).To(HaveLen(expectedEndpointsLen))
			} else if testDNSProvider == provider.DNSProviderAWS.String() {
				expectedEndpointsLen = (2 + len(testGeoRecords) + (len(testRecords) * 2)) * 2
				Expect(zoneEndpoints).To(HaveLen(expectedEndpointsLen))
			}

			By(fmt.Sprintf("checking each record has all(%v) domain owners present within %s", len(allOwners), recordsReadyMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(tr.record.Status.DomainOwners).To(ConsistOf(allOwners))
				}
			}, recordsReadyMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records updated in %v\n", time.Since(checkStarted))

			var totalEndpointsChecked = 0

			var allOwnerMatcher = []types.GomegaMatcher{
				ContainSubstring("heritage=external-dns,external-dns/owner="),
			}
			var geoOwners = map[string][]string{}
			var geoKlbHostname = map[string]string{}
			var geoOwnerMatcher = map[string][]types.GomegaMatcher{}
			for i := range testRecords {
				underTest := testRecords[i]
				ownerID := underTest.record.Status.OwnerID
				allOwnerMatcher = append(allOwnerMatcher, ContainSubstring(ownerID))
				geoCode := testRecords[i].config.testGeoCode
				geoOwners[geoCode] = append(geoOwners[geoCode], ownerID)
				geoKlbHostname[geoCode] = testRecords[i].config.hostnames.geoKlb
				if _, ok := geoOwnerMatcher[geoCode]; !ok {
					geoOwnerMatcher[geoCode] = []types.GomegaMatcher{
						ContainSubstring("heritage=external-dns,external-dns/owner="),
					}
				}
				geoOwnerMatcher[geoCode] = append(geoOwnerMatcher[geoCode], ContainSubstring(ownerID))
			}

			By("[Common] checking common endpoints")
			// A CNAME record for testHostname should always exist and be owned by all endpoints
			By("[Common] checking " + testHostname + " endpoint")
			Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
				"DNSName":       Equal(testHostname),
				"Targets":       ConsistOf(testRecords[0].config.hostnames.klb),
				"RecordType":    Equal("CNAME"),
				"SetIdentifier": Equal(""),
				"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
			}))))
			totalEndpointsChecked++
			By("[Common] checking " + testHostname + " TXT owner endpoint")
			Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
				"DNSName":       Equal("kuadrant-cname-" + testHostname),
				"Targets":       ContainElement(And(allOwnerMatcher...)),
				"RecordType":    Equal("TXT"),
				"SetIdentifier": Equal(""),
				"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
			}))))
			totalEndpointsChecked++

			By("[Geo] checking geo endpoints")

			if testDNSProvider == provider.DNSProviderAzure.String() {

				defaultTarget := FindDefaultTarget(zoneEndpoints)
				// A CNAME record for klbHostName should always exist, be owned by all endpoints and target all geo hostnames
				klbHostName := testRecords[0].config.hostnames.klb

				allKlbGeoHostnames := []string{}
				gcpGeoProps := []externaldnsendpoint.ProviderSpecificProperty{
					{Name: "routingpolicy", Value: "Geographic"},
				}
				for g, h := range geoKlbHostname {
					allKlbGeoHostnames = append(allKlbGeoHostnames, h)
					if h == defaultTarget {
						g = "WORLD"
					}
					gcpGeoProps = append(gcpGeoProps, externaldnsendpoint.ProviderSpecificProperty{Name: h, Value: g})
				}

				By("[Geo] checking " + klbHostName + " endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":          Equal(klbHostName),
					"Targets":          ConsistOf(allKlbGeoHostnames),
					"RecordType":       Equal("CNAME"),
					"SetIdentifier":    Equal(""),
					"RecordTTL":        Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": ContainElements(gcpGeoProps),
				}))))
				totalEndpointsChecked++
				By("[Geo] checking " + klbHostName + " TXT owner endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ContainElement(And(allOwnerMatcher...)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				totalEndpointsChecked++
			}
			if testDNSProvider == provider.DNSProviderGCP.String() {
				// A CNAME record for klbHostName should always exist, be owned by all endpoints and target all geo hostnames
				klbHostName := testRecords[0].config.hostnames.klb

				allKlbGeoHostnames := []string{}
				gcpGeoProps := []externaldnsendpoint.ProviderSpecificProperty{
					{Name: "routingpolicy", Value: "geo"},
				}
				for g, h := range geoKlbHostname {
					allKlbGeoHostnames = append(allKlbGeoHostnames, h)
					gcpGeoProps = append(gcpGeoProps, externaldnsendpoint.ProviderSpecificProperty{Name: h, Value: g})
				}

				By("[Geo] checking " + klbHostName + " endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":          Equal(klbHostName),
					"Targets":          ConsistOf(allKlbGeoHostnames),
					"RecordType":       Equal("CNAME"),
					"SetIdentifier":    Equal(""),
					"RecordTTL":        Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": ContainElements(gcpGeoProps),
				}))))
				totalEndpointsChecked++
				By("[Geo] checking " + klbHostName + " TXT owner endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ContainElement(And(allOwnerMatcher...)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				totalEndpointsChecked++
			}
			if testDNSProvider == provider.DNSProviderAWS.String() {
				// A CNAME record for klbHostName should exist for each geo and be owned by all endpoints in that geo
				klbHostName := testRecords[0].config.hostnames.klb
				for geoCode, geoRecords := range testGeoRecords {
					geoKlbHostName := geoRecords[0].config.hostnames.geoKlb

					By("[Geo] checking " + klbHostName + " -> " + geoCode + " -> " + geoKlbHostName + " - endpoint")

					awsGeoCodeKey := "aws/geolocation-country-code"
					if !provider.IsISO3166Alpha2Code(geoCode) {
						awsGeoCodeKey = "aws/geolocation-continent-code"
					}
					awsGeoCodeValue := strings.TrimPrefix(geoCode, "GEO-")

					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(klbHostName),
						"Targets":       ConsistOf(geoKlbHostName),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(geoCode),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: "alias", Value: "false"},
							{Name: awsGeoCodeKey, Value: awsGeoCodeValue},
						}),
					}))))
					totalEndpointsChecked++
					By("[Geo] checking " + klbHostName + " -> " + geoCode + " - TXT owner endpoint")
					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-cname-" + klbHostName),
						"Targets":       ContainElement(And(geoOwnerMatcher[geoCode]...)),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(geoCode),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
						"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
							{Name: awsGeoCodeKey, Value: awsGeoCodeValue},
						}),
					}))))
					totalEndpointsChecked++
				}

				defaultGeoKlbHostName := testRecords[0].config.hostnames.defaultGeoKlb
				defaultGeoCode := testRecords[0].config.testDefaultGeoCode

				By("[Geo] checking endpoint " + klbHostName + " -> default")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(klbHostName),
					"Targets":       ConsistOf(defaultGeoKlbHostName),
					"RecordType":    Equal("CNAME"),
					"SetIdentifier": Equal("default"),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "alias", Value: "false"},
						{Name: "aws/geolocation-country-code", Value: "*"},
					}),
				}))))
				totalEndpointsChecked++
				By("[Geo] checking " + klbHostName + " -> default - TXT owner endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-cname-" + klbHostName),
					"Targets":       ContainElement(And(geoOwnerMatcher[defaultGeoCode]...)),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal("default"),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
						{Name: "aws/geolocation-country-code", Value: "*"},
					}),
				}))))
				totalEndpointsChecked++
			}

			By("[Weight] checking weighted endpoints")
			if testDNSProvider == provider.DNSProviderAzure.String() {
				// A weighted CNAME record should exist for each geo, be owned by all endpoints in that geo, and target the hostname of all clusters in that geo
				for geoCode, geoRecords := range testGeoRecords {
					geoKlbHostName := geoRecords[0].config.hostnames.geoKlb

					allGeoClusterHostnames := []string{}
					gcpWeightProps := []externaldnsendpoint.ProviderSpecificProperty{
						{Name: "routingpolicy", Value: weighted},
					}
					for i := range geoRecords {
						geoClusterHostname := geoRecords[i].config.hostnames.clusterKlb
						allGeoClusterHostnames = append(allGeoClusterHostnames, geoClusterHostname)
						gcpWeightProps = append(gcpWeightProps, externaldnsendpoint.ProviderSpecificProperty{Name: geoClusterHostname, Value: "200"})
					}

					By("[Weight] checking " + geoKlbHostName + " endpoint")
					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":          Equal(geoKlbHostName),
						"Targets":          ConsistOf(allGeoClusterHostnames),
						"RecordType":       Equal("CNAME"),
						"SetIdentifier":    Equal(""),
						"RecordTTL":        Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": ContainElements(gcpWeightProps),
					}))))
					totalEndpointsChecked++
					By("[Weight] checking " + geoKlbHostName + " TXT owner endpoint")
					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-cname-" + geoKlbHostName),
						"Targets":       ContainElement(And(geoOwnerMatcher[geoCode]...)),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
					totalEndpointsChecked++
				}
			}
			if testDNSProvider == provider.DNSProviderGCP.String() {
				// A weighted CNAME record should exist for each geo, be owned by all endpoints in that geo, and target the hostname of all clusters in that geo
				for geoCode, geoRecords := range testGeoRecords {
					geoKlbHostName := geoRecords[0].config.hostnames.geoKlb

					allGeoClusterHostnames := []string{}
					gcpWeightProps := []externaldnsendpoint.ProviderSpecificProperty{
						{Name: "routingpolicy", Value: weighted},
					}
					for i := range geoRecords {
						geoClusterHostname := geoRecords[i].config.hostnames.clusterKlb
						allGeoClusterHostnames = append(allGeoClusterHostnames, geoClusterHostname)
						gcpWeightProps = append(gcpWeightProps, externaldnsendpoint.ProviderSpecificProperty{Name: geoClusterHostname, Value: "200"})
					}

					By("[Weight] checking " + geoKlbHostName + " endpoint")
					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":          Equal(geoKlbHostName),
						"Targets":          ConsistOf(allGeoClusterHostnames),
						"RecordType":       Equal("CNAME"),
						"SetIdentifier":    Equal(""),
						"RecordTTL":        Equal(externaldnsendpoint.TTL(60)),
						"ProviderSpecific": ContainElements(gcpWeightProps),
					}))))
					totalEndpointsChecked++
					By("[Weight] checking " + geoKlbHostName + " TXT owner endpoint")
					Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal("kuadrant-cname-" + geoKlbHostName),
						"Targets":       ContainElement(And(geoOwnerMatcher[geoCode]...)),
						"RecordType":    Equal("TXT"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
					}))))
					totalEndpointsChecked++
				}
			}
			if testDNSProvider == provider.DNSProviderAWS.String() {
				// A weighted CNAME record should exist for each dns record in each geo and be owned only by that endpoint
				for _, geoRecords := range testGeoRecords {
					geoKlbHostName := geoRecords[0].config.hostnames.geoKlb
					for i := range geoRecords {
						clusterKlbHostName := geoRecords[i].config.hostnames.clusterKlb
						ownerID := geoRecords[i].record.Status.OwnerID
						By("[Weight] checking " + geoKlbHostName + " -> " + clusterKlbHostName + " - endpoint")
						Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(geoKlbHostName),
							"Targets":       ConsistOf(clusterKlbHostName),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(clusterKlbHostName),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "alias", Value: "false"},
								{Name: "aws/weight", Value: "200"},
							}),
						}))))
						totalEndpointsChecked++
						By("[Weight] checking " + geoKlbHostName + " -> " + clusterKlbHostName + " -> " + ownerID + " TXT owner endpoint")
						Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("kuadrant-cname-" + geoKlbHostName),
							"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + ownerID + "\""),
							"RecordType":    Equal("TXT"),
							"SetIdentifier": Equal(clusterKlbHostName),
							"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
							"ProviderSpecific": Equal(externaldnsendpoint.ProviderSpecific{
								{Name: "aws/weight", Value: "200"},
							}),
						}))))
						totalEndpointsChecked++
					}
				}
			}

			By("[Cluster] checking cluster endpoints")
			// An A record with the cluster target IP should exist for each dns record and owned only by that endpoint
			for i := range testRecords {
				clusterKlbHostName := testRecords[i].config.hostnames.clusterKlb
				clusterTargetIP := testRecords[i].config.testTargetIP
				ownerID := testRecords[i].record.Status.OwnerID
				By("[Cluster] checking " + clusterKlbHostName + " endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal(clusterKlbHostName),
					"Targets":       ConsistOf(clusterTargetIP),
					"RecordType":    Equal("A"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(60)),
				}))))
				totalEndpointsChecked++
				By("[Cluster] checking " + clusterKlbHostName + " TXT owner endpoint")
				Expect(zoneEndpoints).To(ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
					"DNSName":       Equal("kuadrant-a-" + clusterKlbHostName),
					"Targets":       ConsistOf("\"heritage=external-dns,external-dns/owner=" + ownerID + "\""),
					"RecordType":    Equal("TXT"),
					"SetIdentifier": Equal(""),
					"RecordTTL":     Equal(externaldnsendpoint.TTL(300)),
				}))))
				totalEndpointsChecked++
			}

			By("checking all endpoints were validated")
			Expect(totalEndpointsChecked).To(Equal(expectedEndpointsLen))

			By("deleting all remaining dns records")
			for _, tr := range testRecords {
				err := tr.cluster.k8sClient.Delete(ctx, tr.record,
					client.PropagationPolicy(metav1.DeletePropagationForeground))
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}

			By(fmt.Sprintf("checking all dns records are removed within %s", recordsRemovedMaxDuration))
			checkStarted = time.Now()
			Eventually(func(g Gomega, ctx context.Context) {
				for _, tr := range testRecords {
					err := tr.cluster.k8sClient.Get(ctx, client.ObjectKeyFromObject(tr.record), tr.record)
					g.Expect(err).To(MatchError(ContainSubstring("not found")))
				}
			}, recordsRemovedMaxDuration, 5*time.Second, ctx).Should(Succeed())
			GinkgoWriter.Printf("[debug] all records removed in %v\n", time.Since(checkStarted))

			By("checking provider zone records are all removed")
			Eventually(func(g Gomega, ctx context.Context) {
				zoneEndpoints, err := EndpointsForHost(ctx, testProvider, testHostname)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(zoneEndpoints).To(HaveLen(0))
			}, 5*time.Second, 1*time.Second, ctx).Should(Succeed())

		})
	})

})
