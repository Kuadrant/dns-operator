//go:build integration

package controller

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/goombaio/namegenerator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"
	"github.com/kuadrant/dns-operator/types"
)

const (
	TestTimeoutShort          = time.Second * 5
	TestTimeoutMedium         = time.Second * 15
	TestTimeoutLong           = time.Second * 30
	TestRetryIntervalMedium   = time.Millisecond * 250
	RequeueDuration           = time.Second * 2
	DefaultValidationDuration = time.Second * 1
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

func (te *TestEndpoints) WithSetIdentifier(id string) *TestEndpoints {
	te.endpoints[0].SetIdentifier = id
	return te
}

func (te *TestEndpoints) WithTargets(targets []string) *TestEndpoints {
	te.endpoints[0].Targets = targets
	return te
}

func (te *TestEndpoints) WithTTL(ttl externaldnsendpoint.TTL) *TestEndpoints {
	te.endpoints[0].RecordTTL = ttl
	return te
}

func (te *TestEndpoints) WithRecordType(recordType string) *TestEndpoints {
	te.endpoints[0].RecordType = recordType
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

// setActiveGroupsInDNS sets active groups via the mock TXT resolver
func setActiveGroupsInDNS(zoneDomain string, groups types.Groups, resolver *MockTXTResolver) {
	activeGroupsHost := ActiveGroupsTXTRecordName + "." + zoneDomain

	// Create the active groups value
	groupsStr := ""
	if len(groups) > 0 {
		groupNames := []string{}
		for _, g := range groups {
			groupNames = append(groupNames, string(g))
		}
		groupsStr = "groups=" + strings.Join(groupNames, "&&")
	}

	if len(groups) == 0 {
		// Delete the active groups record
		resolver.DeleteTXTRecord(activeGroupsHost)
		fmt.Fprintf(GinkgoWriter, "DEBUG: Deleted active groups TXT record for host: %s\n", activeGroupsHost)
	} else {
		// Set the active groups record
		resolver.SetTXTRecord(activeGroupsHost, []string{groupsStr})
		fmt.Fprintf(GinkgoWriter, "DEBUG: Set active groups TXT record - host: %s, value: %s\n", activeGroupsHost, groupsStr)
	}
}

// createDefaultDNSProviderSecret creates an inmemory DNS provider secret with default provider label
func createDefaultDNSProviderSecret(ctx context.Context, namespace, zoneDomainName string, k8sClient client.Client) *v1.Secret {
	secret := builder.NewProviderBuilder("inmemory-credentials", namespace).
		For(v1alpha1.SecretTypeKuadrantInmemory).
		WithZonesInitialisedFor(zoneDomainName).
		Build()
	labels := secret.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[v1alpha1.DefaultProviderSecretLabel] = "true"
	secret.SetLabels(labels)
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	return secret
}

// createDNSRecord creates a delegated DNSRecord with weighted CNAME routing
func createDNSRecord(name, namespace, hostname, clusterTarget string) *v1alpha1.DNSRecord {
	eps := NewTestEndpoints(hostname)
	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost:  hostname,
			Delegate:  true,
			Endpoints: eps.WithRecordType("CNAME").WithTargets([]string{clusterTarget}).WithSetIdentifier("").Endpoints(),
		},
	}
}
