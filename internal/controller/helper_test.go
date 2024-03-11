//go:build integration

package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
)

const (
	TestTimeoutMedium       = time.Second * 10
	TestTimeoutLong         = time.Second * 30
	TestRetryIntervalMedium = time.Millisecond * 250
	defaultNS               = "default"
	providerCredential      = "secretname"
)

func testBuildManagedZone(name, ns, domainName string) *kuadrantdnsv1alpha1.ManagedZone {
	return &kuadrantdnsv1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantdnsv1alpha1.ManagedZoneSpec{
			ID:          "1234",
			DomainName:  domainName,
			Description: domainName,
			SecretRef: kuadrantdnsv1alpha1.ProviderRef{
				Name: "secretname",
			},
		},
	}
}
