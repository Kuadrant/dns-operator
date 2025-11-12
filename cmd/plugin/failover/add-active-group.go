package failover

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/endpoint"
)

const (
	groupLabel = "group"
)

var AddActiveGroupCMD = &cobra.Command{
	Use:   "add-active-group <groupName> --providerRef <namespace>/<name> --domain <domain>",
	RunE:  addActiveGroup,
	Short: "add group to active groups",
	Long: "Will ensure existence of a TXT record of active groups for provided host. Also will ensure provided group name" +
		"is an active group for tat domain. This action will trigger publishing of all records associated with the group",
	Args: cobra.ExactArgs(1),
}

func init() {
	AddActiveGroupCMD.Flags().StringVar(&providerRef, "providerRef", "", "A provider reference to the secret with provider credentials. Format = '<namespace>/<name>'")
	AddActiveGroupCMD.Flags().StringVarP(&domain, "domain", "d", "", "domain to which the group will belong")
	AddActiveGroupCMD.Flags().BoolVarP(&assumeYes, "assumeyes", "y", false, "skip confirmation. Use at your own risk")

}

func addActiveGroup(_ *cobra.Command, args []string) error {
	log := logf.Log.WithName("add-active-group")

	// TODO validate group
	groupName := args[0]

	// TODO validate domain against regex

	var err error
	resourceRef, err = common.ParseProviderRef(providerRef)
	if err != nil {
		log.Error(err, "failed to parse provider ref")
		return err
	}

	// get provider secret
	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	// setup client
	k8sConfig := controllerruntime.GetConfigOrDie()

	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	k8sClient, err := client.New(k8sConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		log.Error(err, "failed to create client")
		return err
	}

	secret := &v1.Secret{}

	log.V(1).Info("looking for a specific provider secret", "secretName", resourceRef.Name, "secretNamespace", resourceRef.Namespace)
	err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceRef.Name, Namespace: resourceRef.Namespace}, secret)
	if err != nil {
		log.Error(err, "failed to get secret")
		return err
	}

	// init provider

	dynClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		log.Error(err, "failed to create dynamic client")
		return err
	}

	providerFactory, err := provider.NewFactory(k8sClient, dynClient, provider.RegisteredDefaultProviders(), endpoint.NewAuthoritativeDNSRecordProvider)
	if err != nil {
		log.Error(err, "failed to create provider factory")
		return err
	}

	// isolate zone
	endpointProvider, err := providerFactory.ProviderForSecret(ctx, secret, provider.Config{
		DomainFilter: externaldnsendpoint.DomainFilter{
			Filters: []string{domain},
		},
	})
	if err != nil {
		log.Error(err, "failed to create provider for secret")
		return err
	}

	// we are assuming empty owner's identity - group ID should be merged across owners
	txtRegistry, err := common.GetDefaultTXTRegistry(ctx, endpointProvider, "empty")
	if err != nil {
		log.Error(err, "failed to get TXT registry")
		return err
	}

	// fetch all records from the zone
	endpoints, err := txtRegistry.Records(ctx)
	if err != nil {
		log.Error(err, "failed to get endpoints")
		return err
	}

	// write down all the records for the group
	affectedEndpoints := slice.Filter(endpoints, func(e *externaldnsendpoint.Endpoint) bool {
		return strings.Contains(e.Labels[groupLabel], groupName)
	})

	// check for txt record to exist
	groupRecordName := TXTRecordPrefix + domain
	var groupTXTRecord *externaldnsendpoint.Endpoint

	for _, ep := range endpoints {
		if ep.DNSName == groupRecordName {
			groupTXTRecord = ep
			break
		}
	}
	if groupTXTRecord != nil && strings.Contains(groupTXTRecord.Targets[0], groupName) {
		log.Info("Found existing TXT record for domain that already contains group name.", "domain", domain, "record", groupName)
		log.Info("Nothing to do")
		log.V(1).Info(fmt.Sprintf("existing record name: %s, targets: %s", groupRecordName, groupTXTRecord.Targets))
		return nil

	}

	// ask for confirmation while listing affected records (the ones with the group)
	log.Info(fmt.Sprintf("Setting group %s as active group", groupName))
	log.Info(fmt.Sprintf("Affected records: %d", len(affectedEndpoints)))
	if len(affectedEndpoints) != 0 {
		common.RenderEndpoints(affectedEndpoints)
	}

	log.Info("Do you want to proceed? [Y/N]")
	reader := bufio.NewReader(os.Stdin)

	var answer string

	if !assumeYes {
		answer, err = reader.ReadString('\n')
		if err != nil {
			log.Error(err, "failed to read answer", "answer", answer)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
	}

	// write txt record
	if answer == "y" || assumeYes {
		changes := &plan.Changes{}

		if groupTXTRecord == nil {
			changes.Create = append(changes.Create, GenerateGroupTXTRecord(domain, groupName))
		} else {
			changes.UpdateOld = append(changes.UpdateOld, groupTXTRecord.DeepCopy())
			changes.UpdateNew = append(changes.UpdateNew, EnsureGroupTXTRecord(groupName, groupTXTRecord))
		}

		// apply changes via provider bypassing registry - we don't want ownership TXT records for this
		err = endpointProvider.ApplyChanges(ctx, changes)
		if err != nil {
			log.Error(err, "failed to apply changes")
			return err
		}
	}

	log.Info(fmt.Sprintf("added group \"%s\" to active groups of \"%s\" domain", args[0], domain))
	return nil
}
