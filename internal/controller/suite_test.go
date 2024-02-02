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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kuadrantiov1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-dns-operator/internal/health"
	"github.com/kuadrant/kuadrant-dns-operator/internal/provider"
	_ "github.com/kuadrant/kuadrant-dns-operator/internal/provider/aws"
	providerFake "github.com/kuadrant/kuadrant-dns-operator/internal/provider/fake"
	_ "github.com/kuadrant/kuadrant-dns-operator/internal/provider/google"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc
var dnsProviderFactory = &providerFake.Factory{
	ProviderForFunc: func(ctx context.Context, pa kuadrantiov1alpha1.ProviderAccessor) (provider.Provider, error) {
		return &providerFake.Provider{
			EnsureFunc: func(record *kuadrantiov1alpha1.DNSRecord, zone *kuadrantiov1alpha1.ManagedZone) error {
				return nil
			},
			DeleteFunc: func(record *kuadrantiov1alpha1.DNSRecord, zone *kuadrantiov1alpha1.ManagedZone) error {
				return nil
			},
			EnsureManagedZoneFunc: func(zone *kuadrantiov1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
				return provider.ManagedZoneOutput{}, nil
			},
			DeleteManagedZoneFunc: func(zone *kuadrantiov1alpha1.ManagedZone) error {
				return nil
			},
		}, nil
	},
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = kuadrantiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).ToNot(HaveOccurred())

	healthQueue := health.NewRequestQueue(1 * time.Second)
	err = mgr.Add(healthQueue)
	Expect(err).ToNot(HaveOccurred())

	monitor := health.NewMonitor()
	err = mgr.Add(monitor)
	Expect(err).ToNot(HaveOccurred())

	healthServer := &testHealthServer{
		Port: 3333,
	}
	err = mgr.Add(healthServer)
	Expect(err).ToNot(HaveOccurred())

	err = (&ManagedZoneReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: dnsProviderFactory,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	err = (&DNSRecordReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: dnsProviderFactory,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	err = (&DNSHealthCheckProbeReconciler{
		Client:        mgr.GetClient(),
		HealthMonitor: monitor,
		Queue:         healthQueue,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
