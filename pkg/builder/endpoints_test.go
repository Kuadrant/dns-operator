//go:build unit

package builder

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/external-dns/endpoint"
)

const (
	IPAddressOne = "127.0.0.1"
	IPAddressTwo = "127.0.0.2"
	TestHostname = "pat.the.cat"
)

var (
	TestHost          string
	TestTarget        TestTargetImpl
	TestLoadbalancing *LoadBalancingSpec

	TestName      = "TestName"
	TestNamespace = "TestNamespace"

	domain            = "example.com"
	clusterHash       = "2q5hyv"
	gwHash            = "a8xcra"
	defaultGeo        = "IE"
	defaultWeight     = 120
	clusterID         = "fbf71c44-6b37-4962-ace6-801912e769be"
	customWeightLabel = "kuadrant.io/my-custom-weight-attr"
)

type TestTargetImpl struct {
	metav1.Object
	addresses []TargetAddress
}

func (t TestTargetImpl) GetAddresses() []TargetAddress {
	return t.addresses
}

var _ = Describe("DnsrecordEndpoints", func() {
	BeforeEach(func() {
		// reset
		TestTarget = TestTargetImpl{
			Object: &metav1.ObjectMeta{
				Name:      TestName,
				Namespace: TestNamespace,
			},
		}

		TestLoadbalancing = &LoadBalancingSpec{}
	})
	Context("Success scenarios", func() {
		Context("Simple routing Strategy", func() {
			BeforeEach(func() {
				TestTarget.addresses = []TargetAddress{
					{
						Type:  IPAddressType,
						Value: IPAddressOne,
					},
					{
						Type:  IPAddressType,
						Value: IPAddressTwo,
					},
				}
			})
			It("Should generate endpoint", func() {
				TestHost = HostOne(domain)
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostOne(domain)),
						"Targets":       ContainElements(IPAddressOne, IPAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(60)),
					})),
				))
			})
			It("Should generate wildcard endpoint", func() {
				TestListener := HostWildcard(domain)
				endpoints, err := NewEndpointsBuilder(TestTarget, TestListener).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostWildcard(domain)),
						"Targets":       ContainElements(IPAddressOne, IPAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(60)),
					})),
				))
			})
			It("Should generate hostname endpoint", func() {
				TestTarget.addresses = []TargetAddress{{Type: HostnameAddressType, Value: TestHostname}}
				TestHost = HostOne(domain)
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostOne(domain)),
						"Targets":       ContainElement(TestHostname),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(60)),
					})),
				))

			})
		})
		Context("Load-balanced routing strategy", func() {
			BeforeEach(func() {
				TestTarget.addresses = []TargetAddress{
					{
						Type:  IPAddressType,
						Value: IPAddressOne,
					},
					{
						Type:  IPAddressType,
						Value: IPAddressTwo,
					},
				}
				TestTarget.SetLabels(map[string]string{
					LabelLBAttributeGeoCode: defaultGeo,
				})
				TestLoadbalancing = &LoadBalancingSpec{
					DefaultWeight: Weight(defaultWeight),
					DefaultGeo:    defaultGeo,
				}
			})

			Context("With matching geo", func() {
				It("Should generate endpoints", func() {
					TestHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb.test." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(defaultGeo),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: defaultGeo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					TestHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(defaultGeo),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: defaultGeo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))
				})
			})

			Context("Load-balanced routing strategy with non-matching geo", func() {
				BeforeEach(func() {
					TestTarget.SetLabels(map[string]string{
						LabelLBAttributeGeoCode: "CAD",
					})
				})
				It("Should generate endpoints", func() {
					TestHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("cad.klb.test." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("cad.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("CAD"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "CAD"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					TestHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("cad.klb." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("cad.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("CAD"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "CAD"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))
				})
			})

			Context("Load-balanced routing strategy with custom weights", func() {
				BeforeEach(func() {
					TestTarget.addresses = []TargetAddress{
						{
							Type:  IPAddressType,
							Value: IPAddressOne,
						},
						{
							Type:  IPAddressType,
							Value: IPAddressTwo,
						},
					}
					labels := TestTarget.GetLabels()
					labels[customWeightLabel] = "FOO"
					TestTarget.SetLabels(labels)
					TestLoadbalancing = &LoadBalancingSpec{
						DefaultWeight: Weight(defaultWeight),
						DefaultGeo:    defaultGeo,
						CustomWeights: []*CustomWeight{
							{
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										customWeightLabel: "FOO",
									},
								},
								Weight: 100,
							},
						},
					}

				})
				It("Should generate endpoints", func() {
					TestHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb.test." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "100"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(defaultGeo),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: defaultGeo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					TestHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{IPAddressOne, IPAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"Targets":       ConsistOf(IPAddressOne, IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "100"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(defaultGeo),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: defaultGeo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))
				})

			})

			Context("With missing geo label on Gateway and hostname address", func() {
				BeforeEach(func() {
					TestTarget.addresses = []TargetAddress{
						{
							Type:  HostnameAddressType,
							Value: TestHostname,
						},
					}
					TestTarget.SetLabels(map[string]string{})
					TestLoadbalancing = &LoadBalancingSpec{
						DefaultWeight: Weight(defaultWeight),
						DefaultGeo:    defaultGeo,
					}
				})

				It("Should generate endpoints", func() {
					TestHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{TestHostname})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("default.klb.test." + domain),
							"Targets":          ConsistOf(TestHostname),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(TestHostname),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("default.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": BeNil(),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					TestHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
						WithLoadBalancing(clusterID, TestLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{TestHostname})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("default.klb." + domain),
							"Targets":          ConsistOf(TestHostname),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(TestHostname),
							"RecordTTL":        Equal(endpoint.TTL(60)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("default.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(300)),
							"ProviderSpecific": BeNil(),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(300)),
						})),
					))
				})
			})

		})
	})

	Context("Failure scenarios", func() {
		BeforeEach(func() {
			// create valid set of inputs for lb strategy with custom weights.
			TestTarget.addresses = []TargetAddress{
				{
					Type:  IPAddressType,
					Value: IPAddressOne,
				},
				{
					Type:  IPAddressType,
					Value: IPAddressTwo,
				},
			}
			TestTarget.SetLabels(map[string]string{
				customWeightLabel: "FOO",
			})
			TestLoadbalancing = &LoadBalancingSpec{
				DefaultWeight: Weight(defaultWeight),
				DefaultGeo:    defaultGeo,
				CustomWeights: []*CustomWeight{
					{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								customWeightLabel: "FOO",
							},
						},
						Weight: 100,
					},
				},
			}
			TestHost = HostOne(domain)
		})

		It("Should return no endpoints if missing addresses", func() {
			TestTarget.addresses = []TargetAddress{}
			endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
				WithLoadBalancing(clusterID, TestLoadbalancing).
				Build()
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoints).To(BeEmpty())
		})
		Context("Should validate builder correctly", func() {
			It("Should not allow invalid hostname", func() {
				endpoints, err := NewEndpointsBuilder(TestTarget, "cat").Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid hostname"))
			})
			It("Should not allow nil target", func() {
				endpoints, err := NewEndpointsBuilder(nil, TestHost).Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("must provide target"))
			})
			It("Should not allow for nil addresses", func() {
				TestTarget.addresses = nil
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing(clusterID, TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("must provide addresses"))
			})
			It("Should not allow for empty clusterID", func() {
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing("", TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("cluster ID is required"))
			})
			It("Should not allow for invalid default weight", func() {
				TestLoadbalancing.DefaultWeight = -1
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing(clusterID, TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid default weight"))
			})
			It("Should not allow for invalid default geo", func() {
				TestLoadbalancing.DefaultGeo = ""
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing(clusterID, TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("default geocode is required"))
			})
			It("Should not allow invalid custom weight", func() {
				TestLoadbalancing.CustomWeights[0].Weight = -1
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing(clusterID, TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("custom weight cannot be negative"))
			})
			It("Should define selector for custom weight", func() {
				TestLoadbalancing.CustomWeights[0].Selector = nil
				endpoints, err := NewEndpointsBuilder(TestTarget, TestHost).
					WithLoadBalancing(clusterID, TestLoadbalancing).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("custom weight must define non-empty selector"))
			})
		})

	})
})
