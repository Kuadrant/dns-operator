//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goombaio/namegenerator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	"github.com/kuadrant/dns-operator/test/e2e/helpers"
)

const (
	// configuration environment variables
	dnsZoneDomainNameEnvvar = "TEST_DNS_ZONE_DOMAIN_NAME"
	dnsManagedZoneName      = "TEST_DNS_MANAGED_ZONE_NAME"
	dnsNamespace            = "TEST_DNS_NAMESPACE"
	dnsProvider             = "TEST_DNS_PROVIDER"
	TestTimeoutMedium       = 10 * time.Second
	TestTimeoutLong         = 60 * time.Second
)

var (
	k8sClient client.Client
	// testSuiteID is a randomly generated identifier for the test suite
	testSuiteID string
	// testZoneDomainName provided domain name for the testZoneID e.g. e2e.hcpapps.net
	testZoneDomainName            string
	testManagedZoneName           string
	testNamespace                 string
	testDNSProvider               string
	supportedProviders            = []string{"aws", "gcp"}
	supportedHealthCheckProviders = []string{"aws"}
	testManagedZone               *v1alpha1.ManagedZone
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Tests Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	//Disable default External DNS logger
	logrus.SetOutput(io.Discard)

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

	testManagedZone = &v1alpha1.ManagedZone{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testManagedZoneName}, testManagedZone)
	Expect(err).NotTo(HaveOccurred())

	testSuiteID = "dns-op-e2e-" + GenerateName()

	geoCode := "EU"
	if testDNSProvider == "gcp" {
		geoCode = "europe-west1"
	}
	helpers.SetTestEnv("testGeoCode", geoCode)
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

func EndpointsForHost(ctx context.Context, provider provider.Provider, host string) ([]*externaldnsendpoint.Endpoint, error) {
	filtered := []*externaldnsendpoint.Endpoint{}

	records, err := provider.Records(ctx)
	if err != nil {
		return nil, err
	}

	hostRegexp, err := regexp.Compile(host)
	if err != nil {
		return nil, err
	}

	domainFilter := externaldnsendpoint.NewRegexDomainFilter(hostRegexp, nil)

	for _, record := range records {
		// Ignore records that do not match the domain filter provided
		if !domainFilter.Match(record.DNSName) {
			GinkgoWriter.Printf("[debug] ignoring record %s that does not match domain filter %s\n", record.DNSName, host)
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}

func providerForManagedZone(ctx context.Context, mz *v1alpha1.ManagedZone) (provider.Provider, error) {
	providerFactory := provider.NewFactory(k8sClient)
	providerConfig := provider.Config{
		DomainFilter:   externaldnsendpoint.NewDomainFilter([]string{mz.Spec.DomainName}),
		ZoneTypeFilter: externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:   externaldnsprovider.NewZoneIDFilter([]string{mz.Status.ID}),
	}
	return providerFactory.ProviderFor(ctx, mz, providerConfig)
}
