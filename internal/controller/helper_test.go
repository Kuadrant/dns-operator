//go:build integration

package controller

import (
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
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

func testBuildManagedZone(name, ns, domainName, secretName string) *kuadrantdnsv1alpha1.ManagedZone {
	return &kuadrantdnsv1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantdnsv1alpha1.ManagedZoneSpec{
			DomainName:  domainName,
			Description: domainName,
			SecretRef: kuadrantdnsv1alpha1.ProviderRef{
				Name: secretName,
			},
		},
	}
}

func testBuildInMemoryCredentialsSecret(name, ns string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: map[string][]byte{},
		Type: "kuadrant.io/inmemory",
	}
}

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
