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

func getTypeMeta() metav1.TypeMeta {
	gvk := v1alpha1.GroupVersion.WithKind("DNSRecord")
	return metav1.TypeMeta{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
	}
}

type TestEndpoints struct {
	endpoints []*externaldnsendpoint.Endpoint
}

func NewTestEndpoints(dnsName string) *TestEndpoints {
	return &TestEndpoints{
		endpoints: []*externaldnsendpoint.Endpoint{
			{
				DNSName:          dnsName,
				Targets:          []string{"127.0.0.1"},
				RecordType:       "A",
				SetIdentifier:    "foo",
				RecordTTL:        60,
				Labels:           nil,
				ProviderSpecific: nil,
			},
		},
	}
}

func (te *TestEndpoints) WithTargets(targets []string) *TestEndpoints {
	te.endpoints[0].Targets = targets
	return te
}

func (te *TestEndpoints) WithTTL(ttl externaldnsendpoint.TTL) *TestEndpoints {
	te.endpoints[0].RecordTTL = ttl
	return te
}

func (te *TestEndpoints) Endpoints() []*externaldnsendpoint.Endpoint {
	return te.endpoints
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
