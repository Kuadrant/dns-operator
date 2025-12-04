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
	internal "github.com/kuadrant/dns-operator/internal/controller"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"
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
	log := logf.Log.WithName("remove-active-group").WithSink(logf.NullLogSink{})

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
		common.PrintError(err, "failed to parse provider ref")
		return err
	}

	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	ctx = logf.IntoContext(ctx, log)
	defer cancel()

	// get all the zones
	endpointProvider, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
		DomainFilter: externaldnsendpoint.NewRegexDomainFilter(domainRegexp, nil),
	})
	if err != nil {
		common.PrintError(err, "failed to create provider for secret")
		return err
	}

	// note down zones we want to work with
	allZones, err := endpointProvider.DNSZones(ctx)
	if err != nil {
		common.PrintError(err, "failed to get DNS zones")
		return err
	}

	if len(allZones) == 0 {
		common.PrintOutput(fmt.Sprintf("No DNS zones found for domain %s", domain), false)
		return nil
	}

	for _, zone := range allZones {
		providerForZone, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
			DomainFilter: externaldnsendpoint.NewRegexDomainFilter(domainRegexp, nil),
			ZoneIDFilter: externaldnsprovider.NewZoneIDFilter([]string{zone.ID}),
		})
		if err != nil {
			common.PrintError(err, fmt.Sprintf("failed to create provider for zone %s (ID: %s)", zone.DNSName, zone.ID))
			continue
		}

		// fetch all records from the zone
		endpoints, err := providerForZone.Records(ctx)
		if err != nil {
			common.PrintError(err, "failed to get endpoints")
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
			common.PrintOutput(fmt.Sprintf("Found no TXT record for domain in zone %s (ID %s)", zone.DNSName, zone.ID), true)
			continue
		}

		if !strings.Contains(groupTXTRecord.Targets[0], groupName) {
			common.PrintOutput(fmt.Sprintf("TXT record does not contain group %s. Groups: %s. Skipping", groupTXTRecord.Targets[0], groupName), true)
			continue
		}

		// write down affected records
		registryMap := externaldnsregistry.TxtRecordsToRegistryMap(endpoints, internal.TXTRegistryPrefix, internal.TXTRegistrySuffix, internal.TXTRegistryWildcardReplacement, []byte(internal.TXTRegistryEncryptAESKey))

		//fmt.Println("CAT")
		//for key, value := range registryMap.Hosts {
		//	fmt.Printf("key: %s, host.host: %s\n", key, value.Host)
		//
		//	for group, registryGgroup := range value.Groups {
		//		fmt.Printf("group: %s\n", group)
		//		fmt.Printf("groupID: %s\n", registryGgroup.GroupID)
		//
		//		for ownerID, registryOwner := range registryGgroup.Owners {
		//			fmt.Printf("ownerID: %s\n", ownerID)
		//			fmt.Printf("registryOwner.OwnerID: %s\n", registryOwner.OwnerID)
		//
		//			for labelKey, labelValue := range registryOwner.Labels {
		//				fmt.Printf("labelKey: %s, labelValue: %s\n", labelKey, labelValue)
		//			}
		//		}
		//	}
		//
		//}

		affectedEndpoints := make([]*externaldnsendpoint.Endpoint, 0)
		for _, ep := range endpoints {
			// locate host in the host map
			registryHost, hostExists := registryMap.Hosts[strings.Replace(ep.DNSName, "*", internal.TXTRegistryWildcardReplacement, 1)]
			if hostExists {
				// check if we have deleting group associated with this host
				_, groupExists := registryHost.Groups[types.Group(groupName)]
				if groupExists {
					affectedEndpoints = append(affectedEndpoints, ep)
				}
			}
		}

		common.PrintOutput(fmt.Sprintf("Removing active group %s from the record: %s", groupName, groupTXTRecord.DNSName), false)

		fmt.Printf("Removal of this group will affect %d endpoints\n", len(affectedEndpoints))

		if len(affectedEndpoints) != 0 {
			common.RenderEndpoints(affectedEndpoints)
		}

		common.PrintOutput(fmt.Sprintf("Selected zone: %s, (ID: %s)", zone.DNSName, zone.ID), true)

		var answer string
		if !assumeYes {
			common.PrintOutput("Do you want to proceed? [Y/N]", false)

			reader := bufio.NewReader(os.Stdin)
			answer, err = reader.ReadString('\n')
			if err != nil {
				common.PrintError(err, fmt.Sprintf("failed to read answer: %s", answer))
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
				common.PrintOutput(fmt.Sprintf("Skipping removal of active group %s; This is a legacy record: %s", groupName, groupTXTRecord.Targets[0]), true)
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
				common.PrintError(err, "failed to apply changes")
				return err
			}
		}

	}

	return nil
}
