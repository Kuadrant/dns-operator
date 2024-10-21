package aws

import (
	"testing"

	"sigs.k8s.io/external-dns/endpoint"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

const recordTTL = 300

func TestAWSAdjustEndpoints(t *testing.T) {
	testCases := []struct {
		Name      string
		Endpoints []*externaldnsendpoint.Endpoint
		Validate  func(t *testing.T, eps []*externaldnsendpoint.Endpoint, err error)
	}{
		{
			Name: "test geo prefix continent code endpoint success",
			Endpoints: []*externaldnsendpoint.Endpoint{
				endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(v1alpha1.ProviderSpecificGeoCode, "GEO-EU"),
			},
			Validate: func(t *testing.T, eps []*externaldnsendpoint.Endpoint, err error) {
				if err != nil {
					t.Fatalf("did not expect an error but got %s", err)
				}
				if len(eps) != 1 {
					t.Fatalf("expected 1 endpoint but got %v", len(eps))
					val, ok := eps[0].GetProviderSpecificProperty(providerSpecificGeolocationContinentCode)
					if !ok {
						t.Fatalf("exected a provider specific contintinent code to be set but got none")
					}
					if val != "EU" {
						t.Fatalf("continent code set but expected the EU got %s ", val)
					}
				}

			},
		},
		{
			Name: "test none geo prefixed continent value and none ISO_3166 to return error",
			Endpoints: []*externaldnsendpoint.Endpoint{
				endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(v1alpha1.ProviderSpecificGeoCode, "EU"),
			},
			Validate: func(t *testing.T, eps []*externaldnsendpoint.Endpoint, err error) {
				if err == nil {
					t.Fatalf("expected an error but got none")
				}
				if len(eps) != 0 {
					t.Fatalf("expected no endpoints but got %v", len(eps))
				}

			},
		},
		{
			Name: "test valid ISO_3166 success for country code",
			Validate: func(t *testing.T, eps []*externaldnsendpoint.Endpoint, err error) {
				if err != nil {
					t.Fatalf("did not expect an error but got %s", err)
				}
				if len(eps) != 1 {
					t.Fatalf("expected 1 endpoint but got %v", len(eps))
				}
				val, ok := eps[0].GetProviderSpecificProperty(providerSpecificGeolocationCountryCode)
				if !ok {
					t.Fatalf("exected a provider specific country code to be set but got none")
				}
				if val != "IE" {
					t.Fatalf("continent code set but expected the IE got %s ", val)
				}
			},
			Endpoints: []*externaldnsendpoint.Endpoint{
				endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(v1alpha1.ProviderSpecificGeoCode, "IE"),
			},
		},
		{
			Name: "test geo prefix lower case continent code endpoint success",
			Endpoints: []*externaldnsendpoint.Endpoint{
				endpoint.NewEndpointWithTTL("geolocation-test.zone-1.ext-dns-test-2.teapot.zalan.do", endpoint.RecordTypeA, endpoint.TTL(recordTTL), "1.2.3.4").WithSetIdentifier("test-set-1").WithProviderSpecific(v1alpha1.ProviderSpecificGeoCode, "geo-eu"),
			},
			Validate: func(t *testing.T, eps []*externaldnsendpoint.Endpoint, err error) {
				if err != nil {
					t.Fatalf("did not expect an error but got %s", err)
				}

				if len(eps) != 1 {
					t.Fatalf("expected 1 endpoint but got %v", len(eps))
					val, ok := eps[0].GetProviderSpecificProperty(providerSpecificGeolocationContinentCode)
					if !ok {
						t.Fatalf("exected a provider specific contintinent code to be set but got none")
					}
					if val != "EU" {
						t.Fatalf("continent code set but expected the EU got %s ", val)
					}
				}

			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			r53Prov := &Route53DNSProvider{}
			adjusted, err := r53Prov.AdjustEndpoints(testCase.Endpoints)
			testCase.Validate(t, adjusted, err)

		})
	}
}
