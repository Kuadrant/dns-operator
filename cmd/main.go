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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	kubeconfigprovider "sigs.k8s.io/multicluster-runtime/providers/kubeconfig"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/controller"
	"github.com/kuadrant/dns-operator/internal/probes"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/coredns"
	_ "github.com/kuadrant/dns-operator/internal/provider/endpoint"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	_ "github.com/kuadrant/dns-operator/internal/provider/inmemory"
	//+kubebuilder:scaffold:imports
)

var (
	setupLog = ctrl.Log.WithName("setup")
	gitSHA   string // pass ldflag here to display gitSHA hash
	dirty    string // must be string as passed in by ldflag to determine display .
	version  string // must be string as passed in by ldflag to determine display .
)

const (
	RequeueDuration           = time.Minute * 15
	ValidityDuration          = time.Minute * 14
	DefaultValidationDuration = time.Second * 5
)

func init() {
	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	//+kubebuilder:scaffold:scheme
}

func printControllerMetaInfo() {
	setupLog.Info("build information", "version", version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var minRequeueTime time.Duration
	var validFor time.Duration
	var maxRequeueTime time.Duration
	var providers stringSliceFlags
	var dnsProbesEnabled bool
	var allowInsecureCerts bool

	var clusterSecretNamespace string
	var clusterSecretLabel string

	flag.BoolVar(&dnsProbesEnabled, "enable-probes", true, "Enable DNSHealthProbes controller.")
	flag.BoolVar(&allowInsecureCerts, "insecure-health-checks", true, "Allow DNSHealthProbes to use insecure certificates")

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&maxRequeueTime, "max-requeue-time", RequeueDuration,
		"The maximum times it takes between reconciliations of DNS Record "+
			"Controls how ofter record is reconciled")
	flag.DurationVar(&validFor, "valid-for", ValidityDuration,
		"Duration when the record is considered to hold valid information"+
			"Controls if we commit to the full reconcile loop")
	flag.DurationVar(&minRequeueTime, "min-requeue-time", DefaultValidationDuration,
		"The minimal timeout between calls to the DNS Provider"+
			"Controls if we commit to the full reconcile loop")
	flag.Var(&providers, "provider", "DNS Provider(s) to enable. Can be passed multiple times e.g. --provider aws --provider google, or as a comma separated list e.g. --provider aws,gcp")

	flag.StringVar(&clusterSecretNamespace, "cluster-secret-namespace", "dns-operator-system", "The Namespace to look for cluster secrets.")
	flag.StringVar(&clusterSecretLabel, "cluster-secret-label", "kuadrant.io/multicluster-kubeconfig", "The label that identifies a Secret resource as a cluster secret.")

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	//ToDo Replace this with generic logic to parse flags from env vars
	var csNamespaceEnvVarName = "CLUSTER_SECRET_NAMESPACE"
	if ns := os.Getenv(csNamespaceEnvVarName); ns != "" {
		clusterSecretNamespace = ns
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printControllerMetaInfo()

	ctx := ctrl.SetupSignalHandler()

	defaultOptions := ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "a3f98d6c.kuadrant.io",
	}

	//ToDo Replace this with generic logic to parse flags from env vars, also add `--watch-namespaces` as a flag
	var watchNamespaces = "WATCH_NAMESPACES"
	if watch := os.Getenv(watchNamespaces); watch != "" {
		namespaces := strings.Split(watch, ",")
		setupLog.Info("watching namespaces set ", watchNamespaces, namespaces)
		cacheOpts := cache.Options{
			DefaultNamespaces: map[string]cache.Config{},
		}
		for _, ns := range namespaces {
			cacheOpts.DefaultNamespaces[ns] = cache.Config{}
		}
		defaultOptions.Cache = cacheOpts
	}

	// Create the kubeconfig provider with options
	clusterProviderOpts := kubeconfigprovider.Options{
		Namespace:             clusterSecretNamespace,
		KubeconfigSecretLabel: clusterSecretLabel,
		KubeconfigSecretKey:   "kubeconfig",
	}

	// Create the provider first, then the manager with the provider
	setupLog.Info("Creating cluster provider")
	clusterProvider := kubeconfigprovider.New(clusterProviderOpts)

	// Setup a cluster-aware Manager, with the provider to lookup clusters.
	setupLog.Info("Creating cluster aware manager")
	mgr, err := mcmanager.New(ctrl.GetConfigOrDie(), clusterProvider, defaultOptions)
	if err != nil {
		setupLog.Error(err, "unable to start multi cluster manager")
		os.Exit(1)
	}

	// Setup provider controller with the manager.
	err = clusterProvider.SetupWithManager(ctx, mgr)
	if err != nil {
		setupLog.Error(err, "Unable to setup provider with manager")
		os.Exit(1)
	}

	if len(providers) == 0 {
		defaultProviders := provider.RegisteredDefaultProviders()
		if defaultProviders == nil {
			setupLog.Error(fmt.Errorf("no default providers registered"), "unable to set providers")
			os.Exit(1)
		}
		providers = defaultProviders
	}

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create dynamic client for cluster")
		os.Exit(1)
	}

	setupLog.Info("init provider factory", "providers", providers)
	providerFactory, err := provider.NewFactory(mgr.GetLocalManager().GetClient(), providers)
	if err != nil {
		setupLog.Error(err, "unable to create provider factory")
		os.Exit(1)
	}

	dnsRecordController := &controller.DNSRecordReconciler{
		Client:          mgr.GetLocalManager().GetClient(),
		Scheme:          mgr.GetLocalManager().GetScheme(),
		ProviderFactory: providerFactory,
	}

	remoteDNSRecordController := &controller.RemoteDNSRecordReconciler{
		DNSRecordReconciler: *dnsRecordController,
	}

	if err = dnsRecordController.SetupWithManager(mgr.GetLocalManager(), maxRequeueTime, validFor, minRequeueTime, dnsProbesEnabled, allowInsecureCerts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSRecord")
		os.Exit(1)
	}

	if err = remoteDNSRecordController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RemoteDNSRecord")
		os.Exit(1)
	}

	if dnsProbesEnabled {
		probeManager := probes.NewProbeManager()
		if err = (&controller.DNSProbeReconciler{
			Client:       mgr.GetLocalManager().GetClient(),
			Scheme:       mgr.GetLocalManager().GetScheme(),
			ProbeManager: probeManager,
		}).SetupWithManager(mgr.GetLocalManager(), maxRequeueTime, validFor, minRequeueTime); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DNSProbe")
			os.Exit(1)
		}
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

type stringSliceFlags []string

func (n *stringSliceFlags) String() string {
	return strings.Join(*n, ",")
}

func (n *stringSliceFlags) Set(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("cannot be empty")
	}
	for _, strVal := range strings.Split(s, ",") {
		for _, v := range *n {
			if v == strVal {
				return nil
			}
		}
		*n = append(*n, strVal)
	}
	return nil
}
