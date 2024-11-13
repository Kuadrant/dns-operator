//go:build integration

package controller

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/goombaio/namegenerator"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
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

func getTestEndpoints(dnsName string, ip []string) []*externaldnsendpoint.Endpoint {
	return []*externaldnsendpoint.Endpoint{
		{
			DNSName:          dnsName,
			Targets:          ip,
			RecordType:       "A",
			SetIdentifier:    "foo",
			RecordTTL:        60,
			Labels:           nil,
			ProviderSpecific: nil,
		},
	}
}

func getTestHealthCheckSpec() *v1alpha1.HealthCheckSpec {
	return &v1alpha1.HealthCheckSpec{
		Path:             "/healthz",
		Port:             443,
		Protocol:         "HTTPS",
		FailureThreshold: 5,
		Interval:         &metav1.Duration{Duration: time.Minute},
		AdditionalHeadersRef: &v1alpha1.AdditionalHeadersRef{
			Name: "headers",
		},
	}
}
