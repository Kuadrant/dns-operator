package failover

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/internal/provider"
)

var GetActiveGroupsCMD = &cobra.Command{
	Use:   "get-active-groups --providerRef <namespace>/<name> --domain <domain>",
	RunE:  getActiveGroups,
	Short: "list active groups",
	Long:  "Will list all active groups for provided domain. Groups will be grouped by the HostedZone they belong to",
}

func init() {
	GetActiveGroupsCMD.Flags().StringVar(&providerRef, "providerRef", "", "A provider reference to the secret with provider credentials. Format = '<namespace>/<name>'")
	GetActiveGroupsCMD.Flags().StringVarP(&domain, "domain", "d", "", "domain to which the group will belong")

}

func getActiveGroups(_ *cobra.Command, _ []string) error {
	log := logf.Log.WithName("get-active-groups").WithSink(logf.NullLogSink{})

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

	// get provider secret
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

	allZones, err := endpointProvider.DNSZones(ctx)
	if err != nil {
		common.PrintError(err, "failed to get DNS zones")
		return err
	}

	if len(allZones) == 0 {
		common.PrintOutput(fmt.Sprintf("No DNS zones found for domain %s", domain), false)
		return nil
	}

	// check each zone and write down active groups for it

	// dns zone is not comparable
	type ZoneRepresentation struct {
		name string
		id   string
	}
	allActiveGroups := make(map[ZoneRepresentation][]string)

	for _, zone := range allZones {
		providerForZone, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
			DomainFilter: externaldnsendpoint.DomainFilter{
				Filters: []string{domain},
			},
			ZoneIDFilter: externaldnsprovider.ZoneIDFilter{
				ZoneIDs: []string{zone.ID},
			},
		})
		if err != nil {
			common.PrintError(err, fmt.Sprintf("failed to create provider for zone %s (ID: %s)", zone.DNSName, zone.ID))
			continue
		}

		endpoints, err := providerForZone.Records(ctx)
		if err != nil {
			common.PrintError(err, "failed tp get endpoints")
			continue
		}

		// check for txt record to exist
		groupRecordName := TXTRecordPrefix + domain
		var groupTXTRecord *externaldnsendpoint.Endpoint

		for _, ep := range endpoints {
			if ep.DNSName == groupRecordName {
				groupTXTRecord = ep
				break
			}
		}

		// we found group record in the zone
		if groupTXTRecord != nil {
			activeGroups, isCurrentVersion := GetActiveGroupsFromTarget(groupTXTRecord.Targets[0])
			if isCurrentVersion {
				allActiveGroups[ZoneRepresentation{
					name: zone.DNSName,
					id:   zone.ID,
				}] = activeGroups
			}
		}
	}

	if len(allActiveGroups) == 0 {
		common.PrintOutput(fmt.Sprintf("No active groups found for domain %s", domain), false)
		return nil
	}

	for zone, groups := range allActiveGroups {
		common.PrintOutput(fmt.Sprintf("Zone %s (ID %s):", zone.name, zone.id), false)
		for _, group := range groups {
			common.PrintOutput(fmt.Sprintf("\t%s", group), false)
		}
	}

	return nil
}
