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
	ipAddressOne         = "127.0.0.1"
	ipAddressTwo         = "127.0.0.2"
	testHostname         = "pat.the.cat"
	testTTL              = 123
	testLoadBalancingTTL = 234
)

var (
	testHost          string
	testTarget        TestTargetImpl
	testLoadbalancing *LoadBalancing

	testName      = "TestName"
	testNamespace = "TestNamespace"

	domain     = "example.com"
	idHash     = "2q5hyv"
	targetHash = "a8xcra"
	geo        = "IE"
	weight     = 120
	id         = "fbf71c44-6b37-4962-ace6-801912e769be"
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
		testTarget = TestTargetImpl{
			Object: &metav1.ObjectMeta{
				Name:      testName,
				Namespace: testNamespace,
			},
		}

		testLoadbalancing = &LoadBalancing{}
	})
	Context("Success scenarios", func() {
		Context("Simple routing Strategy", func() {
			BeforeEach(func() {
				testTarget.addresses = []TargetAddress{
					{
						Type:  IPAddressType,
						Value: ipAddressOne,
					},
					{
						Type:  IPAddressType,
						Value: ipAddressTwo,
					},
				}
			})
			It("Should set default TTL", func() {
				testHost = HostOne(domain)
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).
					SetDefaultTTL(testTTL).
					Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":   Equal(HostOne(domain)),
						"RecordTTL": Equal(endpoint.TTL(testTTL)),
					})),
				))
			})
			It("Should generate endpoint", func() {
				testHost = HostOne(domain)
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostOne(domain)),
						"Targets":       ContainElements(ipAddressOne, ipAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
					})),
				))
			})
			It("Should generate wildcard endpoint", func() {
				TestListener := HostWildcard(domain)
				endpoints, err := NewEndpointsBuilder(testTarget, TestListener).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostWildcard(domain)),
						"Targets":       ContainElements(ipAddressOne, ipAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
					})),
				))
			})
			It("Should generate hostname endpoint", func() {
				testTarget.addresses = []TargetAddress{{Type: HostnameAddressType, Value: testHostname}}
				testHost = HostOne(domain)
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).Build()
				Expect(err).NotTo(HaveOccurred())
				Expect(endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(HostOne(domain)),
						"Targets":       ContainElement(testHostname),
						"RecordType":    Equal("CNAME"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
					})),
				))

			})
		})
		Context("Load-balanced routing strategy", func() {
			BeforeEach(func() {
				testTarget.addresses = []TargetAddress{
					{
						Type:  IPAddressType,
						Value: ipAddressOne,
					},
					{
						Type:  IPAddressType,
						Value: ipAddressTwo,
					},
				}
				testLoadbalancing = &LoadBalancing{
					Weight:       weight,
					Geo:          geo,
					IsDefaultGeo: true,
					Id:           id,
				}
			})

			Context("With default geo", func() {
				It("Should set default TTl and loadbalancing TTL", func() {
					testHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(testTarget, testHost).
						WithLoadBalancing(testLoadbalancing).
						SetDefaultTTL(testTTL).
						SetDefaultLoadBalancedTTL(testLoadBalancingTTL).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{ipAddressOne, ipAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":   Equal(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"RecordTTL": Equal(endpoint.TTL(testTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":   Equal("ie.klb.test." + domain),
							"RecordTTL": Equal(endpoint.TTL(testTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":   Equal("klb.test." + domain),
							"Targets":   ConsistOf("ie.klb.test." + domain),
							"RecordTTL": Equal(endpoint.TTL(testLoadBalancingTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal("klb.test." + domain),
							"Targets":       ConsistOf("ie.klb.test." + domain),
							"SetIdentifier": Equal("default"),
							"RecordTTL":     Equal(endpoint.TTL(testLoadBalancingTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":   Equal(HostOne(domain)),
							"RecordTTL": Equal(endpoint.TTL(testLoadBalancingTTL)),
						})),
					))
				})
				It("Should generate endpoints", func() {
					testHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(testTarget, testHost).
						WithLoadBalancing(testLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{ipAddressOne, ipAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(ipAddressOne, ipAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb.test." + domain),
							"Targets":          ConsistOf(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(endpoint.TTL(DefaultTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(geo),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: geo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					testHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(testTarget, testHost).
						WithLoadBalancing(testLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{ipAddressOne, ipAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(idHash + "-" + targetHash + "." + "klb." + domain),
							"Targets":       ConsistOf(ipAddressOne, ipAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb." + domain),
							"Targets":          ConsistOf(idHash + "-" + targetHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(idHash + "-" + targetHash + "." + "klb." + domain),
							"RecordTTL":        Equal(endpoint.TTL(DefaultTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(geo),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: geo}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
						})),
					))
				})
			})

			Context("With non-default geo", func() {
				BeforeEach(func() {
					testLoadbalancing.IsDefaultGeo = false
					testLoadbalancing.Geo = "CAD"
				})
				It("Should generate endpoints", func() {
					testHost = HostOne(domain)
					endpoints, err := NewEndpointsBuilder(testTarget, testHost).
						WithLoadBalancing(testLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostOne(domain), []string{ipAddressOne, ipAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(ipAddressOne, ipAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("cad.klb.test." + domain),
							"Targets":          ConsistOf(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(idHash + "-" + targetHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(endpoint.TTL(DefaultTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("cad.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("CAD"),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "CAD"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
						})),
					))

				})
				It("Should generate wildcard endpoints", func() {
					testHost = HostWildcard(domain)
					endpoints, err := NewEndpointsBuilder(testTarget, testHost).
						WithLoadBalancing(testLoadbalancing).
						Build()
					Expect(err).NotTo(HaveOccurred())
					Expect(EndpointsTraversable(endpoints, HostWildcard(domain), []string{ipAddressOne, ipAddressTwo})).To(BeTrue())
					Expect(endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(idHash + "-" + targetHash + "." + "klb." + domain),
							"Targets":       ConsistOf(ipAddressOne, ipAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultTTL)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("cad.klb." + domain),
							"Targets":          ConsistOf(idHash + "-" + targetHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(idHash + "-" + targetHash + "." + "klb." + domain),
							"RecordTTL":        Equal(endpoint.TTL(DefaultTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("cad.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("CAD"),
							"RecordTTL":        Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
							"ProviderSpecific": Equal(endpoint.ProviderSpecific{{Name: "geo-code", Value: "CAD"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(endpoint.TTL(DefaultLoadBalancedTTL)),
						})),
					))
				})
			})

		})
	})

	Context("Failure scenarios", func() {
		BeforeEach(func() {
			// create valid set of inputs for lb strategy
			testTarget.addresses = []TargetAddress{
				{
					Type:  IPAddressType,
					Value: ipAddressOne,
				},
				{
					Type:  IPAddressType,
					Value: ipAddressTwo,
				},
			}
			testHost = HostOne(domain)
		})

		It("Should return no endpoints if missing addresses", func() {
			testTarget.addresses = []TargetAddress{}
			endpoints, err := NewEndpointsBuilder(testTarget, testHost).
				WithLoadBalancingFor(id, weight, geo, true).
				Build()
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoints).To(BeEmpty())
		})
		Context("Should validate builder correctly", func() {
			It("Should not allow invalid hostname", func() {
				endpoints, err := NewEndpointsBuilder(testTarget, "cat").Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid hostname"))
			})
			It("Should not allow nil target", func() {
				endpoints, err := NewEndpointsBuilder(nil, testHost).Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("must provide target"))
			})
			It("Should not allow for nil addresses", func() {
				testTarget.addresses = nil
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).
					WithLoadBalancingFor(id, weight, geo, true).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("must provide addresses"))
			})
			It("Should not allow for empty id", func() {
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).
					WithLoadBalancingFor("", weight, geo, true).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("ID is required"))
			})
			It("Should not allow for invalid default weight", func() {
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).
					WithLoadBalancingFor(id, -1, geo, true).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid default weight"))
			})
			It("Should not allow for invalid default geo", func() {
				endpoints, err := NewEndpointsBuilder(testTarget, testHost).
					WithLoadBalancingFor(id, weight, "", true).
					Build()
				Expect(endpoints).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("default geocode is required"))
			})
		})

	})
})
