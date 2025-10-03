package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/coredns"
	"github.com/kuadrant/dns-operator/internal/provider/endpoint"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
)

var deleteOldTXTCMD = &cobra.Command{
	Use:  "delete-old-txt [provider-secret-name]",
	RunE: deleteOldTXT,
}

type DeleteOldTXTCMDFlags struct {
	ns     string
	domain string
}

var deleteOldTXTCMDFlags DeleteOldTXTCMDFlags

func init() {
	deleteOldTXTCMD.Flags().StringVarP(&deleteOldTXTCMDFlags.ns, "namespace", "n", "default", "namespace of a provider secret")
	deleteOldTXTCMD.Flags().StringVarP(&deleteOldTXTCMDFlags.domain, "domain", "d", "", "domain filter to appy to endpoints")

}

func deleteOldTXT(_ *cobra.Command, args []string) error {
	log = logf.Log.WithName("delete-old-txt")

	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	if len(args) > 1 {
		return fmt.Errorf("too many arguments (expected one secret name)")
	}

	// setup client
	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))

	config := controllerruntime.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		fmt.Printf("failed to create client: %v\n", err)
		return nil
	}

	secretList := &v1.SecretList{}
	secret := &v1.Secret{}

	// looking for a default secret
	if len(args) == 0 {
		err = k8sClient.List(ctx, secretList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				v1alpha1.DefaultProviderSecretLabel: "true",
			}),
			Namespace: deleteOldTXTCMDFlags.ns,
		})
		if err != nil {
			fmt.Printf("failed to list secrets: %v\n", err)
			return nil
		}

		if len(secretList.Items) != 1 {
			fmt.Printf("unexpected nmber of secrets: %d; expected 1 default secret\n", len(secretList.Items))
			return nil
		}
		secret = &secretList.Items[0]

	} else {
		err = k8sClient.Get(ctx, client.ObjectKey{Name: args[0], Namespace: deleteOldTXTCMDFlags.ns}, secret)
		if err != nil {
			fmt.Printf("failed to get secret: %v\n", err)
			return nil
		}
	}
	// factory to get a list of all endpoints
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Printf("failed to create dynamic client: %v\n", err)
		return nil
	}

	providerFactory, err := provider.NewFactory(k8sClient, dynClient, provider.RegisteredDefaultProviders(), endpoint.NewAuthoritativeDNSRecordProvider)
	if err != nil {
		fmt.Printf("failed to create provider factory: %v\n", err)
		return nil
	}

	// empty config to list all records
	endpointProvider, err := providerFactory.ProviderForSecret(ctx, secret, provider.Config{})
	if err != nil {
		fmt.Printf("failed to get provider: %v\n", err)
		return nil
	}

	endpoints, err := endpointProvider.Records(ctx)
	if err != nil {
		fmt.Printf("failed to get endpoints: %v\n", err)
		return nil
	}

	// only old TXTs created by us should be left
	endpoints = slice.Filter(endpoints, func(e *externaldns.Endpoint) bool {
		if e != nil &&
			e.RecordType == externaldns.RecordTypeTXT &&
			strings.HasSuffix(e.DNSName, deleteOldTXTCMDFlags.domain) {

			// we will always have at least one target
			if len(e.Targets) == 0 {
				return false
			}

			// two layers of checks to make sure we don't delete anything needed

			// owner and version in targets
			owner, epVersion, epLabels, err := registry.NewLabelsFromString(e.Targets[0], []byte{})
			if err != nil {
				fmt.Printf("failed to extract owner labels: %v\n", err)
				return false
			}

			// happens when there was only one owner
			if owner == "" {
				owner = epLabels[externaldns.OwnerLabelKey]
			}

			// it is old only if it has owner and doesn't have version
			if owner != "" && epVersion == "" {
				// this should be redundant,
				// but it might happen that they have TXTs from external dns that are not from our operator
				// make sure the name of endpoint matches our old pattern
				mapper := registry.NewExternalDNSAffixNameMapper("kuadrant-", "", "wildcard")
				epName, recordType := mapper.ToEndpointName(e.DNSName, epVersion)

				// if they aren't empty - it was our record
				return epName != "" && recordType != ""
			}
			return false
		}
		return false
	})

	fmt.Printf("filtered endpoints len %d\n", len(endpoints))
	for _, ep := range endpoints {
		fmt.Printf("ep: %s\n", ep.DNSName)
	}

	// display what is about to be deleted
	fmt.Printf("TXT records (%d) to be deleted:\n", len(endpoints))
	for _, txtRecord := range endpoints {
		fmt.Printf("%s\t IN\t %s\t %s\n", txtRecord.DNSName, txtRecord.RecordType, txtRecord.Targets)
	}
	fmt.Printf("Do you want to proceed? [Y/N]\n")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	switch answer {
	case "y":
		//delete
		fmt.Printf("deleting old TXT records...\n")
		err = endpointProvider.ApplyChanges(ctx, &plan.Changes{
			Delete: endpoints,
		})
		if err != nil {
			fmt.Printf("failed to delete old TXT records: %v\n", err)
		}
		return nil
	case "n":
		// do nothing
		return nil
	default:
		fmt.Printf("unknown answer %s\n", answer)
		return nil
	}
}
