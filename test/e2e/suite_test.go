//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

const (
	// configuration environment variables
	dnsZoneDomainNameEnvvar     = "TEST_DNS_ZONE_DOMAIN_NAME"
	dnsProviderSecretNameEnvvar = "TEST_DNS_PROVIDER_SECRET_NAME"
	dnsNamespacesEnvvar         = "TEST_DNS_NAMESPACES"
	dnsClusterContextsEnvvar    = "TEST_DNS_CLUSTER_CONTEXTS"
	deploymentCountEnvvar       = "DEPLOYMENT_COUNT"
	clusterCountEnvvar          = "CLUSTER_COUNT"
)

var (
	// testSuiteID is a randomly generated identifier for the test suite
	testSuiteID string
	// testZoneDomainName provided domain name for the testZoneID e.g. e2e.hcpapps.net
	testZoneDomainName     string
	testProviderSecretName string
	testNamespaces         []string
	testClusterContexts    []string
	testDNSProvider        string
	testClusters           []testCluster

	supportedHealthCheckProviders = []string{"aws"}

	defaultRecordsReadyTimeout = time.Minute
	customRecordsReadyTimeout  = map[string]time.Duration{
		"azure": 5 * time.Minute,
	}

	defaultRecordsDeletedTimeout = time.Second * 90
	customRecordsDeletedTimeout  = map[string]time.Duration{
		"azure": 5 * time.Minute,
	}
	recordsReadyMaxDuration   time.Duration
	recordsRemovedMaxDuration time.Duration
)

// testCluster represents a cluster under test and contains a reference to a configured k8client and all it's dns provider secrets.
type testCluster struct {
	name                   string
	testDNSProviderSecrets []*v1.Secret
	k8sClient              client.Client
}

// testDNSRecord encapsulates a v1alpha1.DNSRecord created in a test case, the v1.Secret (DNS Provider Secret) it was created with and the config used to create it.
// The testConfig is used when asserting the expected values set in the providers.
type testDNSRecord struct {
	cluster           *testCluster
	dnsProviderSecret *v1.Secret
	record            *v1alpha1.DNSRecord
	config            testConfig
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
	RunSpecs(t, "E2E Tests Suite")
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
		Expect(testClusters[i].testDNSProviderSecrets).NotTo(BeEmpty())
	}

	recordsReadyMaxDuration = GetRecordsReadyTimeout(testDNSProvider)
	recordsRemovedMaxDuration = GetRecordsDeletedTimeout(testDNSProvider)

	testSuiteID = "dns-op-e2e-" + GenerateName()

	geoCode := "EU"
	if testDNSProvider == "google" {
		geoCode = "europe-west1"
	}
	SetTestEnv("testGeoCode", geoCode)
})

// setConfigFromEnvVars loads test suite runtime configurations from env vars.
//
// dnsProviderSecretNameEnvvar dns provider secret name expected to exist in each test namespace (i.e. dns-provider-credentials-aws).
// dnsZoneDomainNameEnvvar zone domain name accessible via the provider secret to use for testing (i.e. mn.hcpapps.net).
// dnsNamespacesEnvvar test namespaces, comma seperated list (i.e. dns-operator-1,dns-operator-2)
// deploymentCountEnvvar number of test namespaces expected. Appends an index suffix to the dnsNamespacesEnvvar, only used if dnsNamespacesEnvvar is a single length array.
//
// Examples:
// inputs: TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=<unset> configResult: dnsNamespaces=dns-operator
// inputs: TEST_DNS_NAMESPACES=dns-operator-1,dns-operator-2 DEPLOYMENT_COUNT=<unset> configResult: dnsNamespaces=dns-operator-1,dns-operator-2
// inputs: TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=1 configResult: dnsNamespaces=dns-operator-1
// inputs: TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=2 configResult: dnsNamespaces=dns-operator-1,dns-operator-2
// inputs: TEST_DNS_NAMESPACES=dns-operator-5,dns-operator-6 DEPLOYMENT_COUNT=1 configResult: dnsNamespaces=dns-operator-5,dns-operator-6
//
// dnsClusterContextsEnvvar test cluster contexts, comma seperated list (i.e. kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2),
// if unset the current context is used and a single cluster is assumed.
// clusterCountEnvvar number of test clusters expected. Appends an index suffix to the dnsClusterContextsEnvvar, only used if dnsClusterContextsEnvvar is a single length array.
//
// Examples:
// inputs: TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local CLUSTER_COUNT=<unset> configResult: dnsClusterContexts=kind-kuadrant-dns-local
// inputs: TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2 CLUSTER_COUNT=<unset> configResult: dnsClusterContexts=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2
// inputs: TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local CLUSTER_COUNT=1 configResult: dnsClusterContexts=kind-kuadrant-dns-local-1
// inputs: TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local CLUSTER_COUNT=2 configResult: dnsClusterContexts=kind-kuadrant-dns-local-1,kind-kuadrant-dns-local-2
// inputs: TEST_DNS_CLUSTER_CONTEXTS=my-cluster-1,my-cluster-2 CLUSTER_COUNT=1 configResult: dnsClusterContexts=my-cluster-1,my-cluster-2

func setConfigFromEnvVars() error {
	// Load test suite configuration from the environment
	if testZoneDomainName = os.Getenv(dnsZoneDomainNameEnvvar); testZoneDomainName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsZoneDomainNameEnvvar)
	}
	if testProviderSecretName = os.Getenv(dnsProviderSecretNameEnvvar); testProviderSecretName == "" {
		return fmt.Errorf("env variable '%s' must be set", dnsProviderSecretNameEnvvar)
	}

	namespacesStr := os.Getenv(dnsNamespacesEnvvar)
	if namespacesStr == "" {
		//ToDo mnairn: Temporarily keeping a check for "TEST_DNS_NAMESPACE" to allow PR e2e test to work. Remove later.
		namespacesStr = os.Getenv("TEST_DNS_NAMESPACE")
		if namespacesStr == "" {
			return fmt.Errorf("env variable '%s' must be set", dnsNamespacesEnvvar)
		}
	}

	namespaces := strings.Split(namespacesStr, ",")
	if len(namespaces) == 1 {
		if dcStr := os.Getenv(deploymentCountEnvvar); dcStr != "" {
			dc, err := strconv.Atoi(dcStr)
			if err != nil {
				return fmt.Errorf("env variable '%s' must be an integar", deploymentCountEnvvar)
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

	clusterContextsStr := os.Getenv(dnsClusterContextsEnvvar)
	if clusterContextsStr == "" {
		testClusterContexts = []string{"current"}
		return nil
	}

	clusterContexts := strings.Split(clusterContextsStr, ",")
	if len(clusterContexts) == 1 {
		if dcStr := os.Getenv(clusterCountEnvvar); dcStr != "" {
			dc, err := strconv.Atoi(dcStr)
			if err != nil {
				return fmt.Errorf("env variable '%s' must be an integar", clusterCountEnvvar)
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

// loadClusters iterates each of the configured test clusters, configures a k8s client, loads dns provider secrets and creates a `testCluster` resource.
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

		loadProviderSecrets(ctx, tc)

		//Append the cluster to the list of test clusters
		testClusters = append(testClusters, *tc)
	}
}

// loadProviderSecrets iterates each of the configured test namespaces, loads the expected provider secret (TEST_DNS_PROVIDER_SECRET_NAME), and asserts the configuration of each is compatible.
// Sets the test suite testDNSProvider directly from the provider secret.
// If the provider secret does not exist in the namespace, an error is thrown.
// If the provider secret has a different dns provider name from any previously loaded provider secret, an error is thrown.
func loadProviderSecrets(ctx context.Context, tc *testCluster) {
	for _, n := range testNamespaces {
		// Ensure provider secret exists
		s := &v1.Secret{}
		err := tc.k8sClient.Get(ctx, client.ObjectKey{Namespace: n, Name: testProviderSecretName}, s)
		Expect(err).NotTo(HaveOccurred())

		p, err := provider.NameForProviderSecret(s)
		Expect(err).NotTo(HaveOccurred())

		// Ensure all provider secrets are for the same provider
		if testDNSProvider == "" {
			testDNSProvider = p
		} else {
			Expect(p).To(Equal(testDNSProvider))
		}

		//Append the provider secret to the list of test provider secrets
		tc.testDNSProviderSecrets = append(tc.testDNSProviderSecrets, s)
	}
}

func GetRecordsReadyTimeout(provider string) time.Duration {
	if v, ok := customRecordsReadyTimeout[provider]; ok {
		return v
	}
	return defaultRecordsReadyTimeout
}

func GetRecordsDeletedTimeout(provider string) time.Duration {
	if v, ok := customRecordsDeletedTimeout[provider]; ok {
		return v
	}
	return defaultRecordsDeletedTimeout
}
