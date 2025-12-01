package failover

import (
	"context"
	"fmt"
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
	AddActiveGroupCMD.Flags().StringVarP(&domain, "domain", "d", "", "root domain of the zone to add the group to")
	AddActiveGroupCMD.Flags().BoolVarP(&assumeYes, "assumeyes", "y", false, "skip confirmation. Use at your own risk")

}

func addActiveGroup(_ *cobra.Command, args []string) error {
	log := logf.Log.WithName("add-active-group")

	groupName := args[0]

	if groupName == "" {
		return fmt.Errorf("groupName is required")
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

	// setup context
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

	var selectedZones []provider.DNSZone

	if len(allZones) == 0 {
		log.Info(fmt.Sprintf("No DNS zones found for domain %s", domain))
		log.V(1).Info(fmt.Sprintf("Regexp string: %s", domainRegexp.String()))
		return nil
	} else if len(allZones) == 1 {
		selectedZones = allZones
	} else {
		log.Info(fmt.Sprintf("Multiple DNS zones (%d) found for domain %s", len(allZones), domain))
		for _, zone := range allZones {
			if !assumeYes {
				log.Info(fmt.Sprintf("Add group to zone %s (ID: %s)? [Y/N]", zone.DNSName, zone.ID))
			}

			if assumeYes || inputYes(log) {
				if assumeYes {
					log.V(1).Info(fmt.Sprintf("Selected zone %s (ID: %s)", domain, zone.ID))
				}
				selectedZones = append(selectedZones, zone)
			}

		}
	}

	for _, zone := range selectedZones {
		var providerForZone provider.Provider
		var endpoints []*externaldnsendpoint.Endpoint

		providerForZone, err = common.GetProviderForConfig(ctx, resourceRef, provider.Config{
			DomainFilter: externaldnsendpoint.NewRegexDomainFilter(domainRegexp, nil),
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{zone.ID},
			},
		})
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to create provider for zone %s (ID: %s)", zone.DNSName, zone.ID))
			continue
		}

		endpoints, err = providerForZone.Records(ctx)
		if err != nil {
			log.Error(err, "failed tp get endpoints")
			continue
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
		if groupTXTRecord != nil && strings.Contains(groupTXTRecord.Targets[0], groupName) {
			log.Info("Found existing TXT record for domain that already contains group name.", "zone DNS Name", zone.DNSName, "record", groupName)
			log.Info("Nothing to do")
			log.V(1).Info(fmt.Sprintf("existing record name: %s, targets: %s", groupRecordName, groupTXTRecord.Targets))
			continue

		}

		log.Info(fmt.Sprintf("Setting group %s as active group", groupName))

		// write txt record
		changes := &plan.Changes{}

		if groupTXTRecord == nil {
			changes.Create = append(changes.Create, GenerateGroupTXTRecord(zone.DNSName, groupName))
		} else {
			changes.UpdateOld = append(changes.UpdateOld, groupTXTRecord.DeepCopy())
			changes.UpdateNew = append(changes.UpdateNew, EnsureGroupTXTRecord(groupName, groupTXTRecord))
		}

		// apply changes via provider bypassing registry - we don't want ownership TXT records for this
		err = providerForZone.ApplyChanges(ctx, changes)
		if err != nil {
			log.Error(err, "failed to apply changes")
			continue
		}

		log.Info(fmt.Sprintf("added group \"%s\" to active groups of \"%s\" zone", args[0], zone.DNSName))
	}
	return nil
}
