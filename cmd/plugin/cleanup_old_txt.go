package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	_ "github.com/kuadrant/dns-operator/internal/provider/aws"
	_ "github.com/kuadrant/dns-operator/internal/provider/azure"
	_ "github.com/kuadrant/dns-operator/internal/provider/coredns"
	_ "github.com/kuadrant/dns-operator/internal/provider/google"
)

var cleanupOldTXTCMD = &cobra.Command{
	Use:   "prune-legacy-txt-records [provider-secret-name]",
	RunE:  deleteOldTXT,
	Short: "Remove TXT records from previous version of TXT registry",
	Long: "Retrieves the list of all the records from the zone based on the providers secret." +
		"Only the old format of TXT records are considered for a deletion." +
		"The old format of TXTs are only read only with precedence to new records. Once operator locates and old record " +
		"it will create a new one and effectively ignore the existence of the old.",
	Args: cobra.MaximumNArgs(1),
}

type CleanupOldTXTCMDFlags struct {
	ns        string
	domain    string
	assumeyes bool
}

var cleanupOldTXTCMDFlags CleanupOldTXTCMDFlags

func init() {
	cleanupOldTXTCMD.Flags().StringVarP(&cleanupOldTXTCMDFlags.ns, "namespace", "n", "default", "namespace of a provider secret")
	cleanupOldTXTCMD.Flags().StringVarP(&cleanupOldTXTCMDFlags.domain, "domain", "d", "", "domain filter to appy to endpoints. Allows only endpoints that end with specified domain")
	cleanupOldTXTCMD.Flags().BoolVarP(&cleanupOldTXTCMDFlags.assumeyes, "assumeyes", "y", false, "skip confirmation of deletion. Use at your own risk")

}

func deleteOldTXT(_ *cobra.Command, args []string) error {
	log = logf.Log.WithName("prune-legacy-txt")

	if cleanupOldTXTCMDFlags.domain == "" {
		return fmt.Errorf("domain is required")
	}

	// create regexp to filter zones
	// example.com will become ^example.com$ for an exact match
	// *.example.com will become ^.*example.com$ to search using wildcard domain
	domainRegexp, err := regexp.Compile(fmt.Sprintf("^%s$", strings.Replace(cleanupOldTXTCMDFlags.domain, "*.", ".*", 1)))
	if err != nil {
		return err
	}

	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	var secretName string
	if len(args) == 1 {
		secretName = args[0]
	}

	secretRef := &common.ResourceRef{
		Namespace: cleanupOldTXTCMDFlags.ns,
		Name:      secretName,
	}

	log.Info("obtaining list of endpoints from the provider")
	endpointProvider, err := common.GetProviderForConfig(ctx, secretRef, provider.Config{
		DomainFilter: externaldns.NewRegexDomainFilter(domainRegexp, nil),
	})
	if err != nil {
		log.Error(err, "failed to get provider")
		return err
	}

	endpoints, err := endpointProvider.Records(ctx)
	if err != nil {
		log.Error(err, "failed to get endpoints")
		return err
	}

	// only old TXTs created by us should be left
	endpoints = slice.Filter(endpoints, func(e *externaldns.Endpoint) bool {
		if e != nil &&
			e.RecordType == externaldns.RecordTypeTXT &&
			strings.HasSuffix(e.DNSName, cleanupOldTXTCMDFlags.domain) {

			// we will always have at least one target
			if len(e.Targets) == 0 {
				return false
			}

			// two layers of checks to make sure we don't delete anything needed

			// owner and version in targets
			owner, epVersion, epLabels, err := registry.NewLabelsFromString(e.Targets[0], []byte{})
			if err != nil {
				log.Info(fmt.Sprintf("failed to extract owner labels: %v\n ep name: %s, targets: %s", err, e.DNSName, e.Targets))
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

	if len(endpoints) == 0 {
		log.Info("no endpoints to be deleted found")
		return nil
	}

	// display what is about to be deleted
	log.Info(fmt.Sprintf("TXT records (%d) to be deleted:", len(endpoints)))
	for _, txtRecord := range endpoints {
		logf.Log.Info(fmt.Sprintf("%s\t IN\t %s\t %s", txtRecord.DNSName, txtRecord.RecordType, txtRecord.Targets))
	}
	log.Info(fmt.Sprintf("Do you want to proceed? [Y/N]"))
	reader := bufio.NewReader(os.Stdin)

	var answer string

	if !cleanupOldTXTCMDFlags.assumeyes {
		answer, err = reader.ReadString('\n')
		if err != nil {
			log.Error(err, "failed to read answer", "answer", answer)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
	}

	if answer == "y" || cleanupOldTXTCMDFlags.assumeyes {
		//delete
		log.Info("deleting old TXT records...")
		err = endpointProvider.ApplyChanges(ctx, &plan.Changes{
			Delete: endpoints,
		})
		if err != nil {
			log.Error(err, "failed to delete old TXT records")
			return err
		}
		log.Info("records are deleted")
		return nil
	}

	// do nothing
	log.Info("canceling")
	return nil

}
