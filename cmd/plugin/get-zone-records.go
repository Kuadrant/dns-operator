package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/cmd/plugin/output"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/endpoint"
)

const (
	host      = "host"
	DNSRecord = "dnsrecord"
)

var getZoneRecordsCMD = &cobra.Command{
	Use:     "list-records --type <type> --name <name> [ --namespace <namespace> | --provideRef <namespace>/<name> ]",
	PreRunE: flagValidate,
	RunE:    listZoneRecords,
	Short:   "Get all zone records for a host, or DNSrecord.",
}
var (
	name                 string
	namespace            string
	resourceType         string
	providerRef          string
	allowedResourceTypes = []string{host, DNSRecord}
)

func flagValidate(_ *cobra.Command, _ []string) error {
	if !slices.Contains(allowedResourceTypes, strings.ToLower(resourceType)) {
		return fmt.Errorf("Invalid type given. Acceptable types are: %s", strings.Join(allowedResourceTypes, ", "))
	}

	if resourceType == DNSRecord && providerRef != "" {
		return fmt.Errorf("type value of %s and the use of --providerRef are mutually exclusive", DNSRecord)
	}

	if resourceType == host && providerRef == "" {
		return fmt.Errorf("type value of %s requires --providerRef to be provided", host)
	}

	parts := strings.Split(providerRef, "/")
	if providerRef != "" && len(parts) != 2 {
		return errors.New("providerRef most be in the format of '<namespace>/<name>'")
	}

	return nil
}

func init() {
	noDefault := ""

	getZoneRecordsCMD.Flags().StringVar(&name, "name", noDefault, "name for resource")
	if err := getZoneRecordsCMD.MarkFlagRequired("name"); err != nil {
		panic(err)
	}

	getZoneRecordsCMD.Flags().StringVarP(&resourceType, "type", "t", noDefault, fmt.Sprintf("Type of resource being passed. (%s)", strings.Join(allowedResourceTypes, ", ")))
	if err := getZoneRecordsCMD.MarkFlagRequired("type"); err != nil {
		panic(err)
	}

	getZoneRecordsCMD.Flags().StringVar(&providerRef, "providerRef", noDefault,
		fmt.Sprintf("A provider reference to the secert to use when querying. This can only be used with the type of %s. Format = '<namespace>/<name>'", host))

	getZoneRecordsCMD.Flags().StringVarP(&namespace, "namespace", "n", "dns-operator-system", "namespace where resources exist")
}

func listZoneRecords(_ *cobra.Command, _ []string) error {
	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	output.Formatter.Debug(fmt.Sprintf("Getting zone records; Name: %s; Namespace: %s; resourceType: %s; providerRef: %s", name, namespace, resourceType, providerRef))

	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	config := controllerruntime.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		output.Formatter.Error(err, "failed to create client")
		return err
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		output.Formatter.Error(err, "failed to create dynamic client")
		return err
	}

	switch strings.ToLower(resourceType) {
	case host:
		return hostWorkFlow(ctx, k8sClient, dynClient)
	case DNSRecord:
		return dnsRecordWorkFlow(ctx, k8sClient, dynClient)
	default:
		return fmt.Errorf("no workflow found for type: %s, this should be set to one of host of dnsrecord", resourceType)
	}
}

func hostWorkFlow(ctx context.Context, k8sClient client.Client, dynClient *dynamic.DynamicClient) error {
	output.Formatter.Debug("Get secret from cluster based on the providerRef.")
	secretRef, err := common.ParseProviderRef(providerRef)
	if err != nil {
		return err
	}

	output.Formatter.Debug(fmt.Sprintf("secretRef; name: %s; namespace: %s", secretRef.Name, secretRef.Namespace))

	secret := &corev1.Secret{}
	err = k8sClient.Get(ctx, client.ObjectKey{Name: secretRef.Name, Namespace: secretRef.Namespace}, secret)
	if err != nil {
		output.Formatter.Error(err, "failed to get secret")
		return err
	}

	output.Formatter.Debug(fmt.Sprintf("found secret: %s", secret))

	p, err := getProviderFromSecret(ctx, k8sClient, dynClient, secret, name)
	if err != nil {
		output.Formatter.Error(err, "unable to get configure provider")
		return err
	}

	endpoints, err := getEndpoints(ctx, p, name)
	if err != nil {
		output.Formatter.Error(err, "unable to get endpoints from provider")
		return err
	}

	output.Formatter.RenderEndpoints(endpoints)

	return err
}

func dnsRecordWorkFlow(ctx context.Context, k8sClient client.Client, dynClient *dynamic.DynamicClient) error {
	output.Formatter.Debug("Get DNSRecord from the cluster based on the name and namespace provided")
	dnsRecord := &v1alpha1.DNSRecord{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, dnsRecord)
	if err != nil {
		output.Formatter.Error(err, fmt.Sprintf("Unable to get DNSRecord; DNSREcord name: %s", name))
		return err
	}

	// get the provider factory
	providerFactory, err := provider.NewFactory(k8sClient, dynClient, provider.RegisteredDefaultProviders(), endpoint.NewAuthoritativeDNSRecordProvider)
	if err != nil {
		output.Formatter.Error(err, "failed to create provider factory")
		return err
	}

	// the zone filter should be zone.com
	// root host of dns record is always record.zone.com - we need to cut it up a bit
	_, zoneHost, found := strings.Cut(dnsRecord.GetRootHost(), ".")
	if !found {
		err = errors.New("invalid dns record hostname: " + dnsRecord.GetRootHost())
		output.Formatter.Error(err, "failed to prepare zone filter regexp")
		return err
	}

	p, err := providerFactory.ProviderFor(ctx, dnsRecord, provider.Config{
		DomainFilter: externaldns.NewDomainFilter([]string{zoneHost}),
	})
	if err != nil {
		output.Formatter.Error(err, "unable to get configure provider")
		return err
	}

	endpoints, err := getEndpoints(ctx, p, dnsRecord.GetRootHost())
	if err != nil {
		output.Formatter.Error(err, "unable to get endpoints from provider")
		return err
	}

	output.Formatter.RenderEndpoints(endpoints)

	return err
}

func getProviderFromSecret(ctx context.Context, k8sClient client.Client, dynClient *dynamic.DynamicClient, secret *corev1.Secret, host string) (provider.Provider, error) {
	if secret == nil {
		err := errors.New("secret can not be nil")

		output.Formatter.Error(err, "please check configuration")
		return nil, err
	}

	output.Formatter.Debug(fmt.Sprintf("secret passed: %s", secret))

	providerFactory, err := provider.NewFactory(k8sClient, dynClient, provider.RegisteredDefaultProviders(), endpoint.NewAuthoritativeDNSRecordProvider)
	if err != nil {
		output.Formatter.Error(err, "failed to create provider factory")
		return nil, err
	}

	// empty config to list all records
	output.Formatter.Debug("obtaining list of endpoints from the provider")
	endpointProvider, err := providerFactory.ProviderForSecret(ctx, secret, provider.Config{
		DomainFilter: externaldns.NewDomainFilter([]string{host}),
	})
	if err != nil {
		output.Formatter.Error(err, "failed to get provider")
		return nil, err
	}

	return endpointProvider, nil

}

func getEndpoints(ctx context.Context, p provider.Provider, rootHost string) ([]*externaldns.Endpoint, error) {
	output.Formatter.Debug(fmt.Sprintf("get records from provider; provider.name: %s; rootHost: %s", p.Name(), rootHost))

	endpoints, err := p.Records(ctx)
	if err != nil {
		output.Formatter.Error(err, "unable to get records from provider")
		return nil, err
	}

	endpoints = slice.Filter(endpoints, func(e *externaldns.Endpoint) bool {
		if e == nil {
			return false
		}

		if strings.HasSuffix(e.DNSName, rootHost) {
			return true
		}
		return false
	},
	)

	return endpoints, nil
}
