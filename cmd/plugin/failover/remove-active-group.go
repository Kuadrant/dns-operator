package failover

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/internal/provider"
)

var RemoveActiveGroupCMD = &cobra.Command{
	Use:   "remove-active-group <groupName> --providerRef <namespace>/<name> --domain <domain>",
	RunE:  removeActiveGroup,
	Short: "removes group from active groups",
	Long:  "Will remove group from active groups. If this was the last group will remove the TXT record",
	Args:  cobra.ExactArgs(1),
}

func init() {
	RemoveActiveGroupCMD.Flags().StringVar(&providerRef, "providerRef", "", "A provider reference to the secret with provider credentials. Format = '<namespace>/<name>'")
	RemoveActiveGroupCMD.Flags().StringVarP(&domain, "domain", "d", "", "domain to which the group will belong")
	RemoveActiveGroupCMD.Flags().BoolVarP(&assumeYes, "assumeyes", "y", false, "skip confirmation. Use at your own risk")

}

func removeActiveGroup(_ *cobra.Command, args []string) error {
	log := logf.Log.WithName("remove-active-group")

	groupName := args[0]

	if groupName == "" {
		return fmt.Errorf("groupName is required")
	}

	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	// create regexp to filter zones
	domainRegexp, err := GetDomainRegexp(domain)
	if err != nil {
		return err
	}

	resourceRef, err = common.ParseProviderRef(providerRef)
	if err != nil {
		log.Error(err, "failed to parse provider ref")
		return err
	}

	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	// get all the zones
	endpointProvider, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
		DomainFilter: externaldnsendpoint.NewRegexDomainFilter(domainRegexp, nil),
	})
	if err != nil {
		log.Error(err, "failed to create provider for secret")
		return err
	}

	// note down zones we want to work with
	allZones, err := endpointProvider.DNSZones(ctx)
	if err != nil {
		log.Error(err, "failed to get DNS zones")
		return err
	}

	if len(allZones) == 0 {
		log.Info(fmt.Sprintf("No DNS zones found for domain %s", domain))
		return nil
	}

	for _, zone := range allZones {
		providerForZone, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
			DomainFilter: externaldnsendpoint.NewRegexDomainFilter(domainRegexp, nil),
			ZoneIDFilter: externaldnsprovider.NewZoneIDFilter([]string{zone.ID}),
		})
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to create provider for zone %s (ID: %s)", zone.DNSName, zone.ID))
			continue
		}

		// fetch all records from the zone
		endpoints, err := providerForZone.Records(ctx)
		if err != nil {
			log.Error(err, "failed to get endpoints")
			return err
		}

		// check for txt record to exist
		groupRecordName := TXTRecordPrefix + zone.DNSName
		var groupTXTRecord *externaldnsendpoint.Endpoint

		for _, ep := range endpoints {
			if ep.DNSName == groupRecordName {
				groupTXTRecord = ep
				break
			}
		}

		if groupTXTRecord == nil {
			log.V(1).Info(fmt.Sprintf("Found no TXT record for domain in zone %s (ID %s)", zone.DNSName, zone.ID), "domain", domain, "record", groupRecordName)
			continue
		}

		if !strings.Contains(groupTXTRecord.Targets[0], groupName) {
			log.V(1).Info(fmt.Sprintf("TXT record does not contain group %s. Groups: %s. Skipping", groupTXTRecord.Targets[0], groupName))
			continue
		}

		log.Info(fmt.Sprintf("Removing active group %s from the record: %s", groupName, groupTXTRecord.DNSName))
		log.V(1).Info(fmt.Sprintf("Selected zone: %s, (ID: %s)", zone.DNSName, zone.ID))

		var answer string
		if !assumeYes {
			log.Info("Do you want to proceed? [Y/N]")

			reader := bufio.NewReader(os.Stdin)
			answer, err = reader.ReadString('\n')
			if err != nil {
				log.Error(err, "failed to read answer", "answer", answer)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
		}

		// delete group
		if answer == "y" || assumeYes {
			changes := &plan.Changes{}

			oldGroupRecord := groupTXTRecord.DeepCopy()
			groupTXTRecord = RemoveGroupFromActiveGroups(groupName, groupTXTRecord)

			activeGroups, isCurrentVersion := GetActiveGroupsFromTarget(groupTXTRecord.Targets[0])
			if !isCurrentVersion {
				log.V(1).Info(fmt.Sprintf("Skipping removal of active group %s; This is a legacy record: %s", groupName, groupTXTRecord.Targets[0]))
			}

			if len(activeGroups) == 0 {
				changes.Delete = append(changes.Delete, oldGroupRecord)
			} else {
				changes.UpdateOld = append(changes.UpdateOld, oldGroupRecord)
				changes.UpdateNew = append(changes.UpdateNew, groupTXTRecord)
			}

			// apply changes via provider bypassing registry - we don't want ownership TXT records for this
			err = providerForZone.ApplyChanges(ctx, changes)
			if err != nil {
				log.Error(err, "failed to apply changes")
				return err
			}
		}

	}

	return nil
}
