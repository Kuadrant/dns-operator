//go:build integration

/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	kubeconfigprovider "sigs.k8s.io/multicluster-runtime/providers/kubeconfig"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/endpoint"
	ep "github.com/kuadrant/dns-operator/internal/provider/endpoint"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	_ "github.com/kuadrant/dns-operator/internal/provider/inmemory"
	kuadrantTypes "github.com/kuadrant/dns-operator/types"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	testDefaultClusterSecretNamespace = "dns-operator-system"
	testDefaultClusterSecretLabel     = "kuadrant.io/multicluster-kubeconfig"
)

var (
	// Controller runtime env test environments for each delegation role
	primaryTestEnv   *envtest.Environment
	primary2TestEnv  *envtest.Environment
	secondaryTestEnv *envtest.Environment

	// Managers created for each environment
	primaryManager   ctrl.Manager
	primary2Manager  ctrl.Manager
	secondaryManager ctrl.Manager

	// Kubernetes clients created for each environment
	primaryK8sClient   client.Client
	primary2K8sClient  client.Client
	secondaryK8sClient client.Client

	// Dynamic Kubernetes client to use unstructured
	primaryDynamicClient *dynamic.DynamicClient

	// Kubeconfig data for 'kuadrant' user added to each environment
	primaryKubeconfig   []byte
	primary2Kubeconfig  []byte
	secondaryKubeconfig []byte

	// Cluster ID for each environment
	primary1ClusterID  string
	primary2ClusterID  string
	secondaryClusterID string

	ctx    context.Context
	cancel context.CancelFunc
)

type MockTXTResolver struct {
	response []string
	records  map[string][]string
}

func NewMockTXTResolver() *MockTXTResolver {
	return &MockTXTResolver{
		records: make(map[string][]string),
	}
}

func (m *MockTXTResolver) SetTXTRecord(host string, values []string) {
	if m.records == nil {
		m.records = make(map[string][]string)
	}
	m.records[host] = values
}

func (m *MockTXTResolver) DeleteTXTRecord(host string) {
	if m.records != nil {
		delete(m.records, host)
	}
}

func (m *MockTXTResolver) LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error) {
	logger := ctrl.LoggerFrom(ctx)

	// If records map is set, use it for host-specific lookups
	if m.records != nil {
		if values, ok := m.records[host]; ok {
			logger.V(1).Info("MockTXTResolver.LookupTXT found record", "host", host, "values", values)
			return values, nil
		}
		availableHosts := []string{}
		for h := range m.records {
			availableHosts = append(availableHosts, h)
		}
		logger.V(1).Info("MockTXTResolver.LookupTXT no record found", "host", host, "available_hosts", availableHosts)
		return []string{}, nil
	}
	// Fall back to legacy single response behavior for backwards compatibility
	return m.response, nil
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.Level(zapcore.DebugLevel)))

	// Speed up inactive group requeuing for tests
	InactiveGroupRequeueTime = time.Millisecond * 500

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())
	By("bootstrapping test environment")

	primaryTestEnv, primaryManager = setupEnv(DelegationRolePrimary, 1, "", &MockTXTResolver{})
	primary2TestEnv, primary2Manager = setupEnv(DelegationRolePrimary, 2, "", &MockTXTResolver{})
	secondaryTestEnv, secondaryManager = setupEnv(DelegationRoleSecondary, 1, "", &MockTXTResolver{})

	primaryK8sClient = primaryManager.GetClient()
	primary2K8sClient = primary2Manager.GetClient()
	secondaryK8sClient = secondaryManager.GetClient()

	var err error
	primaryDynamicClient, err = dynamic.NewForConfig(primaryTestEnv.Config)
	Expect(err).ShouldNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := primaryManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	go func() {
		defer GinkgoRecover()
		err := primary2Manager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	go func() {
		defer GinkgoRecover()
		err := secondaryManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	//Create the namespace to hold multicluster secrets on the primaries
	By(fmt.Sprintf("creating namespace '%s' on primaries", testDefaultClusterSecretNamespace))
	CreateNamespace(testDefaultClusterSecretNamespace, primaryK8sClient)
	CreateNamespace(testDefaultClusterSecretNamespace, primary2K8sClient)

	//Create a 'kuadrant' user in the primary environments and store the kubeconfig
	By("creating user 'kuadrant' in primary environments")
	primaryKubeconfig = createKuadrantUser(primaryTestEnv)
	Expect(primaryKubeconfig).ToNot(BeEmpty())
	primary2Kubeconfig = createKuadrantUser(primary2TestEnv)
	Expect(primary2Kubeconfig).ToNot(BeEmpty())

	//Create a 'kuadrant' user in the secondary environments and store the kubeconfig
	By("creating user 'kuadrant' in secondary environments")
	secondaryKubeconfig = createKuadrantUser(secondaryTestEnv)
	Expect(secondaryKubeconfig).ToNot(BeEmpty())

	//Verify kubeconfigs are different
	Expect(primaryKubeconfig).ToNot(Or(Equal(secondaryKubeconfig), Equal(primary2Kubeconfig)))
	Expect(primary2Kubeconfig).ToNot(Or(Equal(secondaryKubeconfig), Equal(primaryKubeconfig)))
	Expect(secondaryKubeconfig).ToNot(Or(Equal(primaryKubeconfig), Equal(primary2Kubeconfig)))

	//Get the kube system namespace UID for each environment
	primary1ClusterID, err = getKubeSystemUID(ctx, primaryK8sClient)
	Expect(err).NotTo(HaveOccurred())
	Expect(primary1ClusterID).ToNot(BeEmpty())

	primary2ClusterID, err = getKubeSystemUID(ctx, primary2K8sClient)
	Expect(err).NotTo(HaveOccurred())
	Expect(primary2ClusterID).ToNot(BeEmpty())

	secondaryClusterID, err = getKubeSystemUID(ctx, secondaryK8sClient)
	Expect(err).NotTo(HaveOccurred())
	Expect(secondaryClusterID).ToNot(BeEmpty())

	//Verify IDs are different
	Expect(primary1ClusterID).ToNot(Or(Equal(primary2ClusterID), Equal(secondaryClusterID)))
	Expect(primary2ClusterID).ToNot(Or(Equal(primary1ClusterID), Equal(secondaryClusterID)))
	Expect(secondaryClusterID).ToNot(Or(Equal(primary1ClusterID), Equal(primary2ClusterID)))
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	if primaryTestEnv != nil {
		err := primaryTestEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}

	if primary2TestEnv != nil {
		err := primary2TestEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}

	if secondaryTestEnv != nil {
		err := secondaryTestEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

func CreateNamespace(name string, client client.Client) {
	nsObject := &v1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	err := client.Create(context.Background(), nsObject)
	Expect(err).ToNot(HaveOccurred())

	existingNamespace := &v1.Namespace{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Name: name}, existingNamespace)
	}, time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
}

// createTestEnv creates and starts a new controller runtime envTest environment.
// It returns the environment and the rest.Config for connecting to its API server.
// The caller is responsible for stopping the environment when done.
func createTestEnv() (*envtest.Environment, *rest.Config) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	return testEnv, cfg
}

// setupManager creates a ctrl.Manager with the required controllers registered for the given delegation role,
// using the provided rest.Config (typically from an existing envtest environment).
// The manager is NOT started - the caller must start it (e.g., via startManager).
//
// The setup of controllers here should be the same as how they are configured in the main application.
//
// Primary:
//   - create multicluster-controller-runtime manager
//   - setup kubeconfig provider
//   - setup DNSRecordReconciler
//   - setup RemoteDNSRecordReconciler
//
// Secondary:
//   - create controller-runtime manager
//   - setup DNSRecordReconciler
func setupManager(ctx context.Context, cfg *rest.Config, delegationRole string, count int, group string, txtResolver TXTResolver) ctrl.Manager {
	dynClient, err := dynamic.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(dynClient).NotTo(BeNil())

	var mgr ctrl.Manager
	var mcmgr mcmanager.Manager

	defaultOptions := ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
		Logger: ctrl.LoggerFrom(ctx).WithName(fmt.Sprintf("%s-%v", delegationRole, count)),
	}

	if delegationRole == DelegationRoleSecondary {
		// Use the normal controller runtime manager when running with the secondary delegation role
		mgr, err = ctrl.NewManager(cfg, defaultOptions)
		Expect(err).ToNot(HaveOccurred())
	} else {
		// Create the kubeconfig provider with options
		clusterProviderOpts := kubeconfigprovider.Options{
			Namespace:             testDefaultClusterSecretNamespace,
			KubeconfigSecretLabel: testDefaultClusterSecretLabel,
			KubeconfigSecretKey:   "kubeconfig",
			Scheme:                scheme.Scheme,
		}

		// Create the provider first, then the manager with the provider
		clusterProvider := kubeconfigprovider.New(clusterProviderOpts)

		// Set up a cluster-aware Manager, with the provider to lookup clusters.
		mcmgr, err = mcmanager.New(cfg, clusterProvider, defaultOptions)
		Expect(err).ToNot(HaveOccurred())

		// Set up provider controller with the manager.
		err = clusterProvider.SetupWithManager(ctx, mcmgr)
		Expect(err).ToNot(HaveOccurred())

		mgr = mcmgr.GetLocalManager()
	}
	Expect(mgr).ToNot(BeNil())

	providerFactory, err := provider.NewFactory(mgr.GetClient(), dynClient, []string{provider.DNSProviderInMem.String(), provider.DNSProviderEndpoint.String()}, ep.NewAuthoritativeDNSRecordProvider)
	Expect(err).ToNot(HaveOccurred())
	Expect(providerFactory).ToNot(BeNil())

	dnsRecordController := &DNSRecordReconciler{
		Client: mgr.GetClient(),
		BaseDNSRecordReconciler: BaseDNSRecordReconciler{
			Scheme:          mgr.GetScheme(),
			ProviderFactory: providerFactory,
			DelegationRole:  delegationRole,
			Group:           kuadrantTypes.Group(group),
			TXTResolver:     txtResolver,
		},
	}

	err = dnsRecordController.SetupWithManager(mgr, RequeueDuration, DefaultValidationDuration, true, true)
	Expect(err).ToNot(HaveOccurred())

	if delegationRole == DelegationRolePrimary {
		Expect(mcmgr).ToNot(BeNil())

		remoteDNSRecordController := &RemoteDNSRecordReconciler{
			BaseDNSRecordReconciler: BaseDNSRecordReconciler{
				Scheme:          mgr.GetScheme(),
				ProviderFactory: providerFactory,
				DelegationRole:  delegationRole,
				TXTResolver:     txtResolver,
			},
		}

		err = remoteDNSRecordController.SetupWithManager(mcmgr, RequeueDuration, true)
		Expect(err).ToNot(HaveOccurred())
	}

	return mgr
}

// startManager starts the given manager in a goroutine with a new child context.
// Returns a stop function that cancels the manager context and waits for the
// manager to fully shut down. The stop function is safe to call multiple times.
func startManager(parentCtx context.Context, mgr ctrl.Manager) func() {
	mgrCtx, mgrCancel := context.WithCancel(parentCtx)
	done := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(done)
		err := mgr.Start(mgrCtx)
		Expect(err).ToNot(HaveOccurred())
	}()
	return func() {
		mgrCancel()
		<-done
	}
}

// setupEnv creates a new controller runtime envTest environment with the required controllers running for the given delegation role.
// This is a convenience wrapper that combines createTestEnv and setupManager.
func setupEnv(delegationRole string, count int, group string, txtResolver TXTResolver) (*envtest.Environment, ctrl.Manager) {
	testEnv, cfg := createTestEnv()
	mgr := setupManager(ctx, cfg, delegationRole, count, group, txtResolver)
	return testEnv, mgr
}

// createKuadrantUser creates a new user 'kuadrant' in the given envTest Environment and returns the kubeconfig data for that user.
func createKuadrantUser(testEnv *envtest.Environment) (kubeconfig []byte) {
	user, err := testEnv.AddUser(envtest.User{Name: "kuadrant", Groups: []string{"system:masters"}}, &rest.Config{})
	Expect(err).ToNot(HaveOccurred())

	kubeconfig, err = user.KubeConfig()
	Expect(err).ToNot(HaveOccurred())
	Expect(kubeconfig).ToNot(BeEmpty())

	return kubeconfig
}

func generateTestNamespaceName() string {
	return "test-namespace-" + uuid.New().String()
}

// returns the `kube-system` namespace UID as a string
func getKubeSystemUID(ctx context.Context, c client.Client) (string, error) {
	ns := &v1.Namespace{}
	err := c.Get(ctx, client.ObjectKey{Name: "kube-system"}, ns)
	if err != nil {
		return "", err
	}
	return string(ns.UID), nil
}
