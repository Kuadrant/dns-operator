package azure

import (
	"testing"

	. "github.com/onsi/gomega"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
)

func TestAzureProvider_AdjustEndpoints(t *testing.T) {
	RegisterTestingT(t)
	tests := []struct {
		name      string
		endpoints []*externaldnsendpoint.Endpoint
		Verify    func(endpoints []*externaldnsendpoint.Endpoint, err error)
	}{
		{
			name: "GEO endpoints",
			endpoints: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "app.testdomain.com",
					RecordTTL:  300,
					RecordType: "CNAME",
					Targets: []string{
						"klb.testdomain.com",
					},
				},
				{
					DNSName:    "ip1.testdomain.com",
					RecordTTL:  60,
					RecordType: "A",
					Targets: []string{
						"172.32.200.1",
					},
				},
				{
					DNSName:    "ip2.testdomain.com",
					RecordTTL:  60,
					RecordType: "A",
					Targets: []string{
						"172.32.200.2",
					},
				},
				{
					DNSName:    "eu.klb.testdomain.com",
					RecordTTL:  60,
					RecordType: "CNAME",
					Targets: []string{
						"ip2.testdomain.com",
					},
				},
				{
					DNSName:    "us.klb.testdomain.com",
					RecordTTL:  60,
					RecordType: "CNAME",
					Targets: []string{
						"ip1.testdomain.com",
					},
				},
				{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []externaldnsendpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "*",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				},
				{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []externaldnsendpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "GEO-NA",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"us.klb.testdomain.com",
					},
				},
				{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []externaldnsendpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				},
			},
			Verify: func(endpoints []*externaldnsendpoint.Endpoint, err error) {
				Expect(err).To(BeNil())

				Expect(endpoints).To(HaveLen(6))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "app.testdomain.com",
						RecordTTL:  300,
						RecordType: "CNAME",
						Targets:    []string{"klb.testdomain.com"},
					},
				))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "ip1.testdomain.com",
						RecordTTL:  60,
						RecordType: "A",
						Targets:    []string{"172.32.200.1"},
					},
				))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "ip2.testdomain.com",
						RecordTTL:  60,
						RecordType: "A",
						Targets:    []string{"172.32.200.2"},
					},
				))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "eu.klb.testdomain.com",
						RecordTTL:  60,
						RecordType: "CNAME",
						Targets:    []string{"ip2.testdomain.com"},
					},
				))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "us.klb.testdomain.com",
						RecordTTL:  60,
						RecordType: "CNAME",
						Targets:    []string{"ip1.testdomain.com"},
					},
				))
				Expect(endpoints).To(ContainElement(
					&externaldnsendpoint.Endpoint{
						DNSName:    "klb.testdomain.com",
						RecordTTL:  300,
						RecordType: "CNAME",
						Labels:     map[string]string{},
						Targets:    []string{"eu.klb.testdomain.com", "us.klb.testdomain.com"},
						ProviderSpecific: []externaldnsendpoint.ProviderSpecificProperty{
							{
								Name:  "routingpolicy",
								Value: "Geographic",
							},
							{
								Name:  "eu.klb.testdomain.com",
								Value: "WORLD",
							},
							{
								Name:  "us.klb.testdomain.com",
								Value: "GEO-NA",
							},
						},
					},
				))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &AzureProvider{}
			endpoints, err := p.AdjustEndpoints(tt.endpoints)
			tt.Verify(endpoints, err)

		})
	}
}
