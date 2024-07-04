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
	dnsClusterContexts = "TEST_DNS_CLUSTER_CONTEXTS"
	deploymentCount    = "DEPLOYMENT_COUNT"
	clusterCount       = "CLUSTER_COUNT"
)

var (
	// testSuiteID is a randomly generated identifier for the test suite
	testSuiteID string
	// testZoneDomainName provided domain name for the testZoneID e.g. e2e.hcpapps.net
	testZoneDomainName  string
	testManagedZoneName string
	testNamespaces      []string
	testClusterContexts []string
	testDNSProvider     string
	testClusters        []testCluster
)

// testCluster represents a cluster under test and contains a reference to a configured k8client and all it's managed zones.
type testCluster struct {
	name             string
	testManagedZones []*v1alpha1.ManagedZone
	k8sClient        client.Client
}

// testDNSRecord encapsulates a v1alpha1.DNSRecord created in a test case, the v1alpha1.ManagedZone it was created in and the config used to create it.
// The testConfig is used when asserting the expected values set in the providers.
type testDNSRecord struct {
	cluster     *testCluster
	managedZone *v1alpha1.ManagedZone
	record      *v1alpha1.DNSRecord
	config      testConfig
}

type testConfig struct {
	testTargetIP       string
	testGeoCode        string
	testDefaultGeoCode string
	hostnames          testHostnames
}

type testHostnames struct {
	klb           string
	geoKlb        string
	defaultGeoKlb string
	clusterKlb    string
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

	loadClusters(ctx)
	Expect(testDNSProvider).NotTo(BeEmpty())
	Expect(testZoneDomainName).NotTo(BeEmpty())
	Expect(testClusters).NotTo(BeEmpty())
	for i := range testClusters {
		Expect(testClusters[i].testManagedZones).NotTo(BeEmpty())
	}

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
// dnsNamespaces=dns-operator deploymentCount=2 = dnsNamespaces=dns-operator-1,dns-operator-2
// dnsNamespaces=dns-operator-5,dns-operator-6 deploymentCount=1 = dnsNamespaces=dns-operator-5,dns-operator-6
//
// dnsClusterContexts test cluster contexts, comma seperated list (i.e. kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2),
// if unset the current context is used and a single cluster is assumed.
// clusterCount number of test clusters expected. Appends an index suffix to the dnsClusterContexts, only used if dnsClusterContexts is a single length array.
//
// Examples:
// dnsClusterContexts=kind-kuadrant-dns-local clusterCount=<unset> = dnsClusterContexts=kind-kuadrant-dns-local
// dnsClusterContexts=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2 clusterCount=<unset> = dnsClusterContexts=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2
// dnsClusterContexts=kind-kuadrant-dns-local clusterCount=1 = dnsClusterContexts=kind-kuadrant-dns-local-1
// dnsClusterContexts=kind-kuadrant-dns-local clusterCount=2 = dnsClusterContexts=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2
// dnsClusterContexts=my-cluster-1,my-cluster-2 clusterCount=1 = dnsClusterContexts=my-cluster-1,my-cluster-2

func setConfigFromEnvVars() error {
	// Load test suite configuration from the environment
	if testManagedZoneName = os.Getenv(dnsManagedZoneName); testManagedZoneName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsManagedZoneName)
	}

	namespacesStr := os.Getenv(dnsNamespaces)
	if namespacesStr == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsNamespaces)
	}

	namespaces := strings.Split(namespacesStr, ",")
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

	clusterContextsStr := os.Getenv(dnsClusterContexts)
	if clusterContextsStr == "" {
		testClusterContexts = []string{"current"}
		return nil
	}

	clusterContexts := strings.Split(clusterContextsStr, ",")
	if len(clusterContexts) == 1 {
		if dcStr := os.Getenv(clusterCount); dcStr != "" {
			dc, err := strconv.Atoi(dcStr)
			if err != nil {
				return fmt.Errorf("env variable '%s' must be an integar", clusterCount)
			}
			for i := 1; i <= dc; i++ {
				testClusterContexts = append(testClusterContexts, fmt.Sprintf("%s-%d", clusterContexts[0], i))
			}
		} else {
			testClusterContexts = clusterContexts
		}
	} else {
		testClusterContexts = clusterContexts
	}

	return nil
}

// loadClusters iterates each of the configured test clusters, configures a k8s client, loads test managed zones and creates a `testCluster` resource.
func loadClusters(ctx context.Context) {
	for _, c := range testClusterContexts {
		cfgOverrides := &clientcmd.ConfigOverrides{}
		if c != "current" {
			cfgOverrides = &clientcmd.ConfigOverrides{CurrentContext: c}
		}
		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			cfgOverrides,
		).ClientConfig()
		Expect(err).NotTo(HaveOccurred())

		k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		tc := &testCluster{
			name:      c,
			k8sClient: k8sClient,
		}

		loadManagedZones(ctx, tc)

		//Append the cluster to the list of test clusters
		testClusters = append(testClusters, *tc)
	}
}

// loadManagedZones iterates each of the configured test namespaces, loads the expected managed zone (TEST_DNS_MANAGED_ZONE_NAME), and asserts the configuration of each is compatible.
// Sets the test suite testDNSProvider and testZoneDomainName directly from the managed zone spec and provider secret.
// If the managed zone does not exist in the namespace, an error is thrown.
// If the managed zone has a different domain name from any previously loaded managed zones, an error is thrown.
// If the managed zone has a different dns provider from any previously loaded managed zones, an error is thrown.
func loadManagedZones(ctx context.Context, tc *testCluster) {
	for _, n := range testNamespaces {
		mz := &v1alpha1.ManagedZone{}

		// Ensure managed zone exists and is ready
		err := tc.k8sClient.Get(ctx, client.ObjectKey{Namespace: n, Name: testManagedZoneName}, mz)
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
		err = tc.k8sClient.Get(ctx, client.ObjectKey{Namespace: n, Name: mz.Spec.SecretRef.Name}, s)
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
		tc.testManagedZones = append(tc.testManagedZones, mz)
	}
}
