package azure

import (
	"fmt"
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

func TestCleanAzureError(t *testing.T) {
	RegisterTestingT(t)
	tests := []struct {
		name   string
		err    error
		Verify func(err error)
	}{
		{
			name: "cleans up bad GEO error",
			err:  fmt.Errorf("\n\t* PUT https://management.azure.com/subscriptions/6a87facd-e4e1-4738-a497-fb325344c3d1/resourceGroups/pbKuadrant/providers/Microsoft.Network/trafficmanagerprofiles/pbKuadrant-742d6572726f7273\n--------------------------------------------------------------------------------\nRESPONSE 400: 400 Bad Request\nERROR CODE: BadRequest\n--------------------------------------------------------------------------------\n{\n  \"error\": {\n    \"code\": \"BadRequest\",\n    \"message\": \"The following locations specified in the geoMapping property for endpoint ‘foo-example-com’ are not supported: NOTAGEOCODE. For a list of supported locations, see the Traffic Manager documentation.\"\n  }\n}\n--------------------------------------------------------------------------------\n\n\n"),
			Verify: func(err error) {
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(Equal("The following locations specified in the geoMapping property for endpoint ‘foo-example-com’ are not supported: NOTAGEOCODE. For a list of supported locations, see the Traffic Manager documentation."))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cleanAzureError(tt.err)
			tt.Verify(err)

		})
	}
}
