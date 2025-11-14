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
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	zap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	kubeconfigprovider "sigs.k8s.io/multicluster-runtime/providers/kubeconfig"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/controller"
	dnsMetrics "github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/probes"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/coredns"
	ep "github.com/kuadrant/dns-operator/internal/provider/endpoint"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
	_ "github.com/kuadrant/dns-operator/internal/provider/inmemory"
	"github.com/kuadrant/dns-operator/types"
	//+kubebuilder:scaffold:imports
)

var (
	setupLog        = ctrl.Log.WithName("setup")
	gitSHA          string // pass ldflag here to display gitSHA hash
	dirty           string // must be string as passed in by ldflag to determine display .
	version         string // must be string as passed in by ldflag to determine display .
	delegationRoles = []string{controller.DelegationRolePrimary, controller.DelegationRoleSecondary}

	// controller flags
	metricsAddr            string
	enableLeaderElection   bool
	probeAddr              string
	minRequeueTime         time.Duration
	validFor               time.Duration
	maxRequeueTime         time.Duration
	providers              stringSliceFlags
	dnsProbesEnabled       bool
	allowInsecureCerts     bool
	clusterSecretNamespace string
	clusterSecretLabel     string
	watchNamespaces        string
	delegationRole         string
	group                  types.Group
	logMode                string
	logLevel               string

	// represents booth flag and envar key
	metricsAddrKey            = variableKey("metrics-bind-address")
	enableLeaderElectionKey   = variableKey("leader-elect")
	probeAddrKey              = variableKey("health-probe-bind-address")
	minRequeueTimeKey         = variableKey("min-requeue-time")
	validForKey               = variableKey("valid-for")
	maxRequeueTimeKey         = variableKey("max-requeue-time")
	providersKey              = variableKey("provider")
	dnsProbesEnabledKey       = variableKey("enable-probes")
	allowInsecureCertsKey     = variableKey("insecure-health-checks")
	clusterSecretNamespaceKey = variableKey("cluster-secret-namespace")
	clusterSecretLabelKey     = variableKey("cluster-secret-label")
	watchNamespacesKey        = variableKey("watch-namespaces")
	delegationRoleKey         = variableKey("delegation-role")
	groupKey                  = variableKey("group")
	logModeKey                = variableKey("log-mode")
	logLevelKey               = variableKey("log-level")
)

const (
	RequeueDuration           = time.Minute * 15
	ValidityDuration          = time.Minute * 14
	DefaultValidationDuration = time.Second * 5

	DefaultClusterSecretNamespace = "dns-operator-system"
	DefaultClusterSecretLabel     = "kuadrant.io/multicluster-kubeconfig"

	DefaultLogLevel = zapcore.InfoLevel
)

func init() {
	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	//+kubebuilder:scaffold:scheme
}

func printControllerMetaInfo() {
	setupLog.Info("build information", "version", version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	flag.BoolVar(&dnsProbesEnabled, dnsProbesEnabledKey.Flag(), true, "Enable DNSHealthProbes controller.")
	flag.BoolVar(&allowInsecureCerts, allowInsecureCertsKey.Flag(), true, "Allow DNSHealthProbes to use insecure certificates")

	flag.StringVar(&metricsAddr, metricsAddrKey.Flag(), ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, probeAddrKey.Flag(), ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, enableLeaderElectionKey.Flag(), false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&maxRequeueTime, maxRequeueTimeKey.Flag(), RequeueDuration,
		"The maximum times it takes between reconciliations of DNS Record "+
			"Controls how ofter record is reconciled")
	flag.DurationVar(&validFor, validForKey.Flag(), ValidityDuration,
		"Duration when the record is considered to hold valid information"+
			"Controls if we commit to the full reconcile loop")
	flag.DurationVar(&minRequeueTime, minRequeueTimeKey.Flag(), DefaultValidationDuration,
		"The minimal timeout between calls to the DNS Provider"+
			"Controls if we commit to the full reconcile loop")
	flag.Var(&providers, providersKey.Flag(), "DNS Provider(s) to enable. Can be passed multiple times e.g. --provider aws --provider google, or as a comma separated list e.g. --provider aws,gcp")

	flag.StringVar(&clusterSecretNamespace, clusterSecretNamespaceKey.Flag(), DefaultClusterSecretNamespace, "The Namespace to look for cluster secrets.")
	flag.StringVar(&clusterSecretLabel, clusterSecretLabelKey.Flag(), DefaultClusterSecretLabel, "The label that identifies a Secret resource as a cluster secret.")
	flag.StringVar(&watchNamespaces, watchNamespacesKey.Flag(), "", "Comma separated list of default namespaces.")
	flag.StringVar(&logLevel, logLevelKey.Flag(), "", "Log level")
	flag.StringVar(&logMode, logModeKey.Flag(), "", "Log mode")

	flag.Var(newDelegationRoleValue(controller.DelegationRolePrimary, &delegationRole), delegationRoleKey.Flag(), "The delegation role for this controller. Must be one of 'primary'(default), or 'secondary'")

	flag.Var(&group, groupKey.Flag(), "Set Group for dns-operator")

	flag.Parse()

	ctrl.SetLogger(zap.New(withLogLevel(logLevel), withLogMode(logMode)))

	printControllerMetaInfo()
	overrideControllerFlags()

	ctx := ctrl.SetupSignalHandler()

	defaultOptions := ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "a3f98d6c.kuadrant.io",
	}

	if watchNamespaces != "" {
		namespaces := strings.Split(watchNamespaces, ",")
		setupLog.Info("watching namespaces set ", watchNamespacesKey.Flag(), namespaces)
		cacheOpts := cache.Options{
			DefaultNamespaces: map[string]cache.Config{},
		}
		for _, ns := range namespaces {
			cacheOpts.DefaultNamespaces[ns] = cache.Config{}
		}
		defaultOptions.Cache = cacheOpts
	}

	var mgr ctrl.Manager
	var mcmgr mcmanager.Manager
	var err error
	setupLog.Info(fmt.Sprintf("using group: %s", group))
	setupLog.Info("using delegation role: ", "delegationRole", delegationRole)
	if delegationRole == controller.DelegationRoleSecondary {
		setupLog.Info("Creating manager")
		// Use the normal controller runtime manager when running with the secondary delegation role
		mgr, err = ctrl.NewManager(ctrl.GetConfigOrDie(), defaultOptions)
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}
	} else {
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
		mcmgr, err = mcmanager.New(ctrl.GetConfigOrDie(), clusterProvider, defaultOptions)
		if err != nil {
			setupLog.Error(err, "unable to start multi cluster manager")
			os.Exit(1)
		}

		// Setup provider controller with the manager.
		err = clusterProvider.SetupWithManager(ctx, mcmgr)
		if err != nil {
			setupLog.Error(err, "Unable to setup provider with manager")
			os.Exit(1)
		}

		mgr = mcmgr.GetLocalManager()
		metrics.Registry.MustRegister(&dnsMetrics.RemoteClusterCollector{Provider: clusterProvider})
	}

	metrics.Registry.MustRegister(&dnsMetrics.LocalCollector{Ctx: ctx, Mgr: mgr})

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
	providerFactory, err := provider.NewFactory(mgr.GetClient(), dynamicClient, providers, ep.NewAuthoritativeDNSRecordProvider)
	if err != nil {
		setupLog.Error(err, "unable to create provider factory")
		os.Exit(1)
	}

	dnsRecordController := &controller.DNSRecordReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		ProviderFactory: providerFactory,
		DelegationRole:  delegationRole,
		Group:           &group,
	}

	if err = dnsRecordController.SetupWithManager(mgr, maxRequeueTime, validFor, minRequeueTime, dnsProbesEnabled, allowInsecureCerts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSRecord")
		os.Exit(1)
	}

	if mcmgr != nil {
		remoteDNSRecordController := &controller.RemoteDNSRecordReconciler{
			Scheme:          mgr.GetScheme(),
			ProviderFactory: providerFactory,
			DelegationRole:  delegationRole,
			Group:           &group,
		}

		if err = remoteDNSRecordController.SetupWithManager(mcmgr, false); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "RemoteDNSRecord")
			os.Exit(1)
		}
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
	if err := mgr.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// overrideControllerFlags overrides "some-flag" with value of "SOME_FLAG" envar.
func overrideControllerFlags() {
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		k := pair[0]
		v := pair[1]
		switch k {
		case dnsProbesEnabledKey.Envar():
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				dnsProbesEnabled = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", dnsProbesEnabledKey.Flag(), v))
			}
		case allowInsecureCertsKey.Envar():
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				allowInsecureCerts = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", allowInsecureCertsKey.Flag(), v))
			}
		case metricsAddrKey.Envar():
			metricsAddr = v
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", metricsAddrKey.Flag(), v))
		case probeAddrKey.Envar():
			probeAddr = v
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", probeAddrKey.Flag(), v))
		case enableLeaderElectionKey.Envar():
			value, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				enableLeaderElection = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", enableLeaderElectionKey.Flag(), v))
			}
		case maxRequeueTimeKey.Envar():
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				maxRequeueTime = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", maxRequeueTimeKey.Flag(), v))
			}
		case validForKey.Envar():
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				validFor = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", validForKey.Flag(), v))
			}
		case minRequeueTimeKey.Envar():
			value, parseErr := time.ParseDuration(v)
			if parseErr == nil {
				minRequeueTime = value
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", minRequeueTimeKey.Flag(), v))
			}
		case providersKey.Envar():
			sliceFlag := stringSliceFlags{}
			parseErr := sliceFlag.Set(v)
			if parseErr == nil {
				providers = sliceFlag
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", providersKey.Flag(), v))
			}
		case clusterSecretNamespaceKey.Envar():
			clusterSecretNamespace = v
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", clusterSecretNamespaceKey.Flag(), v))
		case clusterSecretLabelKey.Envar():
			clusterSecretLabel = v
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", clusterSecretLabelKey.Flag(), v))
		case watchNamespacesKey.Envar():
			watchNamespaces = v
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", watchNamespacesKey.Flag(), v))
		case delegationRoleKey.Envar():
			role := newDelegationRoleValue(delegationRole, &delegationRole)
			parseErr := role.Set(v)
			if parseErr == nil {
				setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", delegationRoleKey.Flag(), v))
			}
		case groupKey.Envar():
			parseErr := group.Set(v)
			if parseErr != nil {
				setupLog.Info("unable to parse group type from configmap", "value", v, "error", parseErr)
				os.Exit(1)
			}
			setupLog.Info(fmt.Sprintf("overriding %s flag with \"%s\" value", groupKey.Flag(), v))
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

// represents a variableKey of a variable that can be passed to the controller as flag or envar
type variableKey string

// Flag returns string of format "some-value"
func (v *variableKey) Flag() string {
	s := strings.ToLower(string(*v))
	return strings.ReplaceAll(s, "_", "-")
}

// Envar returns a string of format "SOME_VALUE"
func (v *variableKey) Envar() string {
	s := strings.ToUpper(string(*v))
	return strings.ReplaceAll(s, "-", "_")
}

type delegationRoleValue string

func (f *delegationRoleValue) String() string { return string(*f) }

func (f *delegationRoleValue) Set(val string) error {
	if !slices.Contains(delegationRoles, val) {
		return fmt.Errorf("must be one of %v", delegationRoles)
	}
	*f = delegationRoleValue(val)
	return nil
}

func newDelegationRoleValue(val string, p *string) *delegationRoleValue {
	*p = val
	return (*delegationRoleValue)(p)
}

func withLogLevel(logLevel string) func(*zap.Options) {
	lvlEnvRaw, ok := os.LookupEnv(logLevelKey.Envar())
	if ok {
		logLevel = lvlEnvRaw
	}

	lvl, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		// If unable to parse the log level, set default
		lvl = DefaultLogLevel
	}

	return func(options *zap.Options) {
		options.Level = lvl
	}
}

func withLogMode(logMode string) func(*zap.Options) {
	logModeRaw, ok := os.LookupEnv(logModeKey.Envar())
	if ok {
		logMode = logModeRaw
	}

	devel := logMode == "development"

	return func(options *zap.Options) {
		options.Development = devel
	}
}
