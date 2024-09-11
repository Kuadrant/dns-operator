//go:build integration

package controller

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/goombaio/namegenerator"

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

func GenerateName() string {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return namegenerator.NewNameGenerator(nBig.Int64()).Generate()
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
