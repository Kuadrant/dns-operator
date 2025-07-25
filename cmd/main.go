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
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/controller"
	"github.com/kuadrant/dns-operator/internal/probes"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/coredns"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	_ "github.com/kuadrant/dns-operator/internal/provider/inmemory"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	gitSHA   string // pass ldflag here to display gitSHA hash
	dirty    string // must be string as passed in by ldflag to determine display .
	version  string // must be string as passed in by ldflag to determine display .

	// controller flags
	metricsAddr          string
	enableLeaderElection bool
	probeAddr            string
	minRequeueTime       time.Duration
	validFor             time.Duration
	maxRequeueTime       time.Duration
	providers            stringSliceFlags
	dnsProbesEnabled     bool
	allowInsecureCerts   bool
)

const (
	RequeueDuration           = time.Minute * 15
	ValidityDuration          = time.Minute * 14
	DefaultValidationDuration = time.Second * 5

	// controller flags keys
	metricsAddrKey          = "metrics-bind-address"
	enableLeaderElectionKey = "leader-elect"
	probeAddrKey            = "health-probe-bind-address"
	minRequeueTimeKey       = "min-requeue-time"
	validForKey             = "valid-for"
	maxRequeueTimeKey       = "max-requeue-time"
	providersKey            = "provider"
	dnsProbesEnabledKey     = "enable-probes"
	allowInsecureCertsKey   = "insecure-health-checks"

	// override flags map
	overrideMapName      = "dns-operator"
	overrideMapNamespace = "kuadrant-system"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func printControllerMetaInfo() {
	setupLog.Info("build information", "version", version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	flag.BoolVar(&dnsProbesEnabled, dnsProbesEnabledKey, true, "Enable DNSHealthProbes controller.")
	flag.BoolVar(&allowInsecureCerts, allowInsecureCertsKey, true, "Allow DNSHealthProbes to use insecure certificates")

	flag.StringVar(&metricsAddr, metricsAddrKey, ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, probeAddrKey, ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, enableLeaderElectionKey, false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&maxRequeueTime, maxRequeueTimeKey, RequeueDuration,
		"The maximum times it takes between reconciliations of DNS Record "+
			"Controls how ofter record is reconciled")
	flag.DurationVar(&validFor, validForKey, ValidityDuration,
		"Duration when the record is considered to hold valid information"+
			"Controls if we commit to the full reconcile loop")
	flag.DurationVar(&minRequeueTime, minRequeueTimeKey, DefaultValidationDuration,
		"The minimal timeout between calls to the DNS Provider"+
			"Controls if we commit to the full reconcile loop")
	flag.Var(&providers, providersKey, "DNS Provider(s) to enable. Can be passed multiple times e.g. --provider aws --provider google, or as a comma separated list e.g. --provider aws,gcp")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printControllerMetaInfo()

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

	overrideControllerFlags()

	if len(providers) == 0 {
		defaultProviders := provider.RegisteredDefaultProviders()
		fmt.Println("\n\n\n def providers \n " + strings.Join(defaultProviders, ""))
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

	if err = (&controller.DNSRecordReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: providerFactory,
	}).SetupWithManager(mgr, maxRequeueTime, validFor, minRequeueTime, dnsProbesEnabled, allowInsecureCerts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSRecord")
		os.Exit(1)
	}

	if dnsProbesEnabled {
		probeManager := probes.NewProbeManager()
		if err = (&controller.DNSProbeReconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			ProbeManager: probeManager,
		}).SetupWithManager(mgr, maxRequeueTime, validFor, minRequeueTime); err != nil {
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// overrideControllerFlags locates the "dns-operator" config map and overrides controller flags with values from it
func overrideControllerFlags() {
	k8sClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create client")
		os.Exit(1)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      overrideMapName,
			Namespace: overrideMapNamespace,
		},
	}

	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(configMap), configMap)
	if errors.IsNotFound(err) {
		return
	}
	if err != nil {
		setupLog.Error(err, "unable to get configmap with controller flags")
		os.Exit(1)
	}
	setupLog.Info(fmt.Sprintf("overriding controller flags with the %s ConfigMap", overrideMapName))
	for k, v := range configMap.Data {
		switch k {
		case dnsProbesEnabledKey:
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				dnsProbesEnabled = value
			}
		case allowInsecureCertsKey:
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				allowInsecureCerts = value
			}
		case metricsAddrKey:
			metricsAddr = v
		case probeAddrKey:
			probeAddr = v
		case enableLeaderElectionKey:
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				enableLeaderElection = value
			}
		case maxRequeueTimeKey:
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				maxRequeueTime = value
			}
		case validForKey:
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				validFor = value
			}
		case minRequeueTimeKey:
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				minRequeueTime = value
			}
		case providersKey:
			sliceFlag := stringSliceFlags{}
			parseErr := sliceFlag.Set(v)
			if parseErr == nil {
				providers = sliceFlag
			}
		}
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
