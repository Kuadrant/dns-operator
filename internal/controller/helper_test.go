//go:build integration

package controller

import (
	"time"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
)

const (
	TestTimeoutShort          = time.Second * 5
	TestTimeoutMedium         = time.Second * 15
	TestTimeoutLong           = time.Second * 30
	TestRetryIntervalMedium   = time.Millisecond * 250
	RequeueDuration           = time.Second * 6
	ValidityDuration          = time.Second * 3
	DefaultValidationDuration = time.Millisecond * 500
)

func getDefaultTestEndpoints() []*externaldnsendpoint.Endpoint {
	return getTestEndpoints("foo.example.com", "127.0.0.1")
}

func getTestEndpoints(dnsName, ip string) []*externaldnsendpoint.Endpoint {
	return []*externaldnsendpoint.Endpoint{
		{
			DNSName: dnsName,
			Targets: []string{
				ip,
			},
			RecordType:       "A",
			SetIdentifier:    "foo",
			RecordTTL:        60,
			Labels:           nil,
			ProviderSpecific: nil,
		},
	}
}
