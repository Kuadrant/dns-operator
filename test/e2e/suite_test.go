//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goombaio/namegenerator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

const (
	// configuration environment variables
	dnsZoneDomainNameEnvvar = "TEST_DNS_ZONE_DOMAIN_NAME"
	dnsManagedZoneName      = "TEST_DNS_MANAGED_ZONE_NAME"
	dnsNamespace            = "TEST_DNS_NAMESPACE"
	dnsProvider             = "TEST_DNS_PROVIDER"
)

var (
	k8sClient client.Client
	// testSuiteID is a randomly generated identifier for the test suite
	testSuiteID string
	// testZoneDomainName provided domain name for the testZoneID e.g. e2e.hcpapps.net
	testZoneDomainName  string
	testManagedZoneName string
	testNamespace       string
	testDNSProvider     string
	supportedProviders  = []string{"aws", "gcp"}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Tests Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	err := setConfigFromEnvVars()
	Expect(err).NotTo(HaveOccurred())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	testSuiteID = "dns-op-e2e-" + GenerateName()
})

func ResolverForDomainName(domainName string) *net.Resolver {
	nameservers, err := net.LookupNS(domainName)
	Expect(err).ToNot(HaveOccurred())
	GinkgoWriter.Printf("[debug] authoritative nameserver used for DNS record resolution: %s\n", nameservers[0].Host)

	authoritativeResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(ctx, network, strings.Join([]string{nameservers[0].Host, "53"}, ":"))
		},
	}
	return authoritativeResolver
}

func setConfigFromEnvVars() error {
	// Load test suite configuration from the environment
	if testZoneDomainName = os.Getenv(dnsZoneDomainNameEnvvar); testZoneDomainName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsZoneDomainNameEnvvar)
	}
	if testManagedZoneName = os.Getenv(dnsManagedZoneName); testManagedZoneName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsManagedZoneName)
	}
	if testNamespace = os.Getenv(dnsNamespace); testNamespace == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsNamespace)
	}
	if testDNSProvider = os.Getenv(dnsProvider); testDNSProvider == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsProvider)
	}
	if !slices.Contains(supportedProviders, testDNSProvider) {
		return fmt.Errorf("unsupported provider '%s' must be one of '%s'", testDNSProvider, supportedProviders)
	}
	return nil
}

func GenerateName() string {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return namegenerator.NewNameGenerator(nBig.Int64()).Generate()
}
