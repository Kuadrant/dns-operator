package coredns_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/coredns"
)

var (
	nameserver1  = "1.1.1.1:53"
	nameserver2  = "2.2.2.2:53"
	gateway1IP   = "18.17.16.15"
	gateway2IP   = "19.18.17.16"
	gateway1Name = "gateway1"
	gateway2Name = "gateway2"
)

func TestRecordsForHost(t *testing.T) {

	testCases := []struct {
		Name              string
		Host              string
		Secret            *v1.Secret
		QueryResponse     coredns.QueryFunc
		ExpectedEndPoints []*endpoint.Endpoint
		ExpectErr         bool
	}{
		{
			Name: "test merged endpoints with weight and geo",
			Host: "k.example.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES":       []byte("k.example.com"),
					"NAMESERVERS": []byte(fmt.Sprintf("%s,%s", nameserver1, nameserver2)),
				},
			},
			ExpectedEndPoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("k.example.com", "CNAME", "lb.k.example.com"),
				endpoint.NewEndpoint("lb.k.example.com", "CNAME", "geo-na.lb.k.example.com").WithProviderSpecific("geo-code", "GEO-NA").WithSetIdentifier("default"),
				endpoint.NewEndpoint("lb.k.example.com", "CNAME", "geo-eu.lb.k.example.com").WithProviderSpecific("geo-code", "GEO-EU"),
				endpoint.NewEndpoint("geo-na.lb.k.example.com", "CNAME", gateway1Name+".lb.k.example.com").WithProviderSpecific("weight", "200"),
				endpoint.NewEndpoint("geo-eu.lb.k.example.com", "CNAME", gateway2Name+".lb.k.example.com").WithProviderSpecific("weight", "100"),
				endpoint.NewEndpoint(gateway1Name+".lb.k.example.com", "A", gateway1IP),
				endpoint.NewEndpoint(gateway2Name+".lb.k.example.com", "A", gateway2IP),
			},
			QueryResponse: func(hosts []string, nameServer string) (map[string]*dns.Msg, error) {
				geo := "geo-na"
				weight := "200"
				gatewayIP := gateway1IP
				gatewayName := gateway1Name
				isDefault := true
				if nameServer == nameserver1 {
					geo = "geo-eu"
					isDefault = false
					weight = "100"
					gatewayIP = gateway2IP
					gatewayName = gateway2Name
				}
				answers := map[string]*dns.Msg{}
				for _, host := range hosts {
					if strings.HasPrefix(host, "w.") {
						answers["weight"] = &dns.Msg{
							Answer: []dns.RR{
								&dns.TXT{
									Hdr: dns.RR_Header{
										Name: host,
									},
									Txt: []string{fmt.Sprintf("%s,%s.lb.k.example.com", weight, geo)},
								},
							},
						}

					} else if strings.HasPrefix(host, "g.") {
						answers["geo"] = &dns.Msg{
							Answer: []dns.RR{
								&dns.TXT{
									Hdr: dns.RR_Header{
										Name: host,
									},
									Txt: []string{fmt.Sprintf("geo=%s", strings.ToUpper(geo)), "type=continent", fmt.Sprintf("default=%t", isDefault)},
								},
							},
						}
					} else {
						answers["dns"] = &dns.Msg{
							Answer: []dns.RR{
								&dns.CNAME{
									Hdr: dns.RR_Header{
										Name: host,
									},
									Target: fmt.Sprintf("lb.%s", host),
								},
								&dns.CNAME{
									Hdr: dns.RR_Header{
										Name: fmt.Sprintf("lb.%s", host),
									},
									Target: fmt.Sprintf("%s.lb.%s", geo, host),
								},
								&dns.CNAME{
									Hdr: dns.RR_Header{
										Name: fmt.Sprintf("%s.lb.%s", geo, host),
									},
									Target: fmt.Sprintf("%s.lb.%s", gatewayName, host),
								},
								&dns.A{
									Hdr: dns.RR_Header{
										Name: fmt.Sprintf("%s.lb.%s", gatewayName, host),
									},
									A: net.ParseIP(gatewayIP),
								},
							},
						}
					}
				}
				return answers, nil
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			p, err := coredns.NewCoreDNSProviderFromSecret(context.Background(), testCase.Secret, provider.Config{})
			if err != nil {
				t.Fatalf("failed to create new core dns provider %s", err)
			}
			p.(*coredns.CoreDNSProvider).DNSQueryFunc = testCase.QueryResponse

			endpoints, _ := p.RecordsForHost(context.Background(), testCase.Host)

			equal := endpointsEqual(endpoints, testCase.ExpectedEndPoints)
			if !equal {
				t.Fatalf("endpoints \n %v \n should match expected endpoints \n %v ", endpoints, testCase.ExpectedEndPoints)
			}
		})
	}
}

func endpointsEqual(eps1, eps2 []*endpoint.Endpoint) bool {
	if len(eps1) != len(eps2) {
		return false
	}
	for _, ep1 := range eps1 {
		found := false
		for _, ep2 := range eps2 {
			if ep1.Key() == ep2.Key() {
				if slices.Equal(ep1.Targets, ep2.Targets) {
					fmt.Println("found endpoint with same targets ", ep1, ep2)
					if equality.Semantic.DeepEqual(ep1.ProviderSpecific, ep2.ProviderSpecific) {
						found = true
						break
					}
				}

			}
		}
		if !found {
			return found
		}
	}
	return true
}

func TestDNSZoneForHost(t *testing.T) {
	testCases := []struct {
		Name             string
		Secret           *v1.Secret
		Host             string
		ExpectedZoneRoot string
		ExpectedErr      func(err error) bool
	}{
		{
			Name: "test correct zone returned",
			Host: "api.k.example.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES":       []byte("k.example.com,example.com"),
					"NAMESERVERS": []byte(fmt.Sprintf("%s,%s", nameserver1, nameserver2)),
				},
			},
			ExpectedZoneRoot: "k.example.com",
			ExpectedErr: func(err error) bool {
				return err == nil
			},
		},
		{
			Name: "test correct zone returned",
			Host: "api.k.example.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES":       []byte("example.com,k.other.com"),
					"NAMESERVERS": []byte(fmt.Sprintf("%s,%s", nameserver1, nameserver2)),
				},
			},
			ExpectedZoneRoot: "example.com",
			ExpectedErr: func(err error) bool {
				return err == nil
			},
		},
		{
			Name: "test error when no zone found",
			Host: "api.k.other.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES":       []byte("k.example.com,example.com"),
					"NAMESERVERS": []byte(fmt.Sprintf("%s,%s", nameserver1, nameserver2)),
				},
			},
			ExpectedZoneRoot: "",
			ExpectedErr: func(err error) bool {
				return err == provider.ErrNoZoneForHost
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			p, err := coredns.NewCoreDNSProviderFromSecret(context.Background(), testCase.Secret, provider.Config{})
			if err != nil {
				t.Fatalf("failed to create new core dns provider %s", err)
			}
			z, err := p.DNSZoneForHost(context.Background(), testCase.Host)
			if err != nil && !testCase.ExpectedErr(err) {
				t.Fatalf("error was not as expected %s ", err)
			}
			if err == nil && z.DNSName != testCase.ExpectedZoneRoot {
				t.Fatalf("expected the zone to be %s but got %s", testCase.ExpectedZoneRoot, z.DNSName)
			}

		})
	}
}
