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
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/controller"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	_ "github.com/kuadrant/dns-operator/internal/provider/inmemory"
	"github.com/kuadrant/dns-operator/internal/version"
	"github.com/kuadrant/dns-operator/pkg/log"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = k8sruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	logLevel = env.GetString("LOG_LEVEL", "info")
	logMode  = env.GetString("LOG_MODE", "production")
	gitSHA   string // pass ldflag here to display gitSHA hash
	dirty    string // must be string as passed in by ldflag to determine display .
)

const (
	RequeueDuration           = time.Minute * 15
	ValidityDuration          = time.Minute * 14
	DefaultValidationDuration = time.Second * 5
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	logger := log.NewLogger(
		log.SetLevel(log.ToLevel(logLevel)),
		log.SetMode(log.ToMode(logMode)),
		log.WriteTo(os.Stdout),
	)
	log.SetLogger(logger)
}

func printControllerMetaInfo() {
	setupLog.Info(fmt.Sprintf("go version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("go os/arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	setupLog.Info("base logger", "log level", logLevel, "log mode", logMode)
	setupLog.Info("", "version", version.Version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	printControllerMetaInfo()
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var minRequeueTime time.Duration
	var validFor time.Duration
	var maxRequeueTime time.Duration
	var providers stringSliceFlags

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
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	var watchNamespaces = "WATCH_NAMESPACES"
	defaultOptions := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "a3f98d6c.kuadrant.io",
	}

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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), defaultOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
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

	setupLog.Info("init provider factory", "providers", providers)
	providerFactory, err := provider.NewFactory(mgr.GetClient(), providers)
	if err != nil {
		setupLog.Error(err, "unable to create provider factory")
		os.Exit(1)
	}

	if err = (&controller.ManagedZoneReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: providerFactory,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ManagedZone")
		os.Exit(1)
	}
	if err = (&controller.DNSRecordReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: providerFactory,
	}).SetupWithManager(mgr, maxRequeueTime, validFor, minRequeueTime); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSRecord")
		os.Exit(1)
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
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
