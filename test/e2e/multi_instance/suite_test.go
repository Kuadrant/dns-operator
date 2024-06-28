//go:build e2e_multi_instance

package multi_instance

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

const (
	// configuration environment variables
	dnsManagedZoneName = "TEST_DNS_MANAGED_ZONE_NAME"
	dnsNamespaces      = "TEST_DNS_NAMESPACES"
	deploymentCount    = "DEPLOYMENT_COUNT"
)

var (
	k8sClient client.Client
	// testSuiteID is a randomly generated identifier for the test suite
	testSuiteID string
	// testZoneDomainName provided domain name for the testZoneID e.g. e2e.hcpapps.net
	testZoneDomainName  string
	testManagedZoneName string
	testNamespaces      []string
	testDNSProvider     string
	testManagedZones    []*v1alpha1.ManagedZone
)

// testDNSRecord encapsulates a v1alpha1.DNSRecord created in a test case, the v1alpha1.ManagedZone it was created in and the config used to create it.
// The testConfig is used when asserting the expected values set in the providers.
type testDNSRecord struct {
	managedZone *v1alpha1.ManagedZone
	record      *v1alpha1.DNSRecord
	config      testConfig
}

type testConfig struct {
	testTargetIP       string
	testGeoCode        string
	testDefaultGeoCode string
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Multi Instance Tests Suite")
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

	loadManagedZones(ctx)
	Expect(testDNSProvider).NotTo(BeEmpty())
	Expect(testZoneDomainName).NotTo(BeEmpty())
	Expect(testManagedZones).NotTo(BeEmpty())

	testSuiteID = "dns-op-e2e-multi-" + GenerateName()

	geoCode := "EU"
	if testDNSProvider == "gcp" {
		geoCode = "europe-west1"
	}
	SetTestEnv("testGeoCode", geoCode)
})

// setConfigFromEnvVars loads test suite runtime configurations from env vars.
// dnsManagedZoneName managed zone name expected to exist in each test namespace (i.e. dev-mz-aws).
// dnsNamespaces test namespaces, comma seperated list (i.e. dns-operator-1,dns-operator-2)
// deploymentCount number of test namespaces expected. Appends an index suffix to the dnsNamespaces, only used if dnsNamespaces is a single length array.
//
// Examples:
// dnsNamespaces=dns-operator deploymentCount=<unset> = dnsNamespaces=dns-operator
// dnsNamespaces=dns-operator-1,dns-operator-2 deploymentCount=<unset> = dnsNamespaces=dns-operator-1,dns-operator-2
// dnsNamespaces=dns-operator deploymentCount=1 = dnsNamespaces=dns-operator-1
// dnsNamespaces=dns-operator deploymentCount=2 = dnsNamespaces=dns-operator-1,dns-operator-1
// dnsNamespaces=dns-operator-5,dns-operator-6 deploymentCount=1 = dnsNamespaces=dns-operator-5,dns-operator-6
func setConfigFromEnvVars() error {
	// Load test suite configuration from the environment
	if testManagedZoneName = os.Getenv(dnsManagedZoneName); testManagedZoneName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsManagedZoneName)
	}

	namespaces := strings.Split(os.Getenv(dnsNamespaces), ",")
	if len(namespaces) == 0 {
		return fmt.Errorf("env variable '%s' must be set", dnsNamespaces)
	}

	if len(namespaces) == 1 {
		if dcStr := os.Getenv(deploymentCount); dcStr != "" {
			dc, err := strconv.Atoi(dcStr)
			if err != nil {
				return fmt.Errorf("env variable '%s' must be an integar", deploymentCount)
			}
			for i := 1; i <= dc; i++ {
				testNamespaces = append(testNamespaces, fmt.Sprintf("%s-%d", namespaces[0], i))
			}
		} else {
			testNamespaces = namespaces
		}
	} else {
		testNamespaces = namespaces
	}

	return nil
}

// loadManagedZones iterates each of the configured test namespaces, loads the expected managed zone (TEST_DNS_MANAGED_ZONE_NAME), and asserts the configuration of each is compatible.
// Sets the test suite testDNSProvider and testZoneDomainName directly from the managed zone spec and provider secret.
// If the managed zone does not exist in the namespace, an error is thrown.
// If the managed zone has a different domain name from any previously loaded managed zones, an error is thrown.
// If the managed zone has a different dns provider from any previously loaded managed zones, an error is thrown.
func loadManagedZones(ctx context.Context) {
	for _, n := range testNamespaces {
		mz := &v1alpha1.ManagedZone{}

		// Ensure managed zone exists and is ready
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: n, Name: testManagedZoneName}, mz)
		Expect(err).NotTo(HaveOccurred())
		Expect(mz.Status.Conditions).To(
			ContainElement(MatchFields(IgnoreExtras, Fields{
				"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
				"Status":             Equal(metav1.ConditionTrue),
				"ObservedGeneration": Equal(mz.Generation),
			})),
		)

		// Ensure all managed zone names match
		if testZoneDomainName == "" {
			testZoneDomainName = mz.Spec.DomainName
		} else {
			Expect(mz.Spec.DomainName).To(Equal(testZoneDomainName))
		}

		s := &v1.Secret{}
		err = k8sClient.Get(ctx, client.ObjectKey{Namespace: n, Name: mz.Spec.SecretRef.Name}, s)
		Expect(err).NotTo(HaveOccurred())

		p, err := provider.NameForProviderSecret(s)
		Expect(err).NotTo(HaveOccurred())

		// Ensure all managed zone are suing the same provider
		if testDNSProvider == "" {
			testDNSProvider = p
		} else {
			Expect(p).To(Equal(testDNSProvider))
		}

		//Append the managed zone to the list of test zones
		testManagedZones = append(testManagedZones, mz)
	}
}
