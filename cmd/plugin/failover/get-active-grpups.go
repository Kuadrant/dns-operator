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
	log := logf.Log.WithName("get-active-groups")

	var err error
	resourceRef, err = common.ParseProviderRef(providerRef)
	if err != nil {
		log.Error(err, "failed to parse provider ref")
		return err
	}
	fmt.Println("test creds")
	fmt.Println(resourceRef.Name + "/" + resourceRef.Namespace)

	// get provider secret
	d := time.Now().Add(time.Minute * 5)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()

	// get all the zones
	endpointProvider, err := common.GetProviderForConfig(ctx, resourceRef, provider.Config{
		DomainFilter: externaldnsendpoint.DomainFilter{
			Filters: []string{domain},
		},
	})
	if err != nil {
		log.Error(err, "failed to create provider for secret")
		return err
	}

	allZones, err := endpointProvider.DNSZones(ctx)
	if err != nil {
		log.Error(err, "failed to get DNS zones")
		return err
	}

	if len(allZones) == 0 {
		log.Info(fmt.Sprintf("No DNS zones found for domain %s", domain))
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
			log.Error(err, fmt.Sprintf("failed to create provider for zone %s (ID: %s)", zone.DNSName, zone.ID))
			continue
		}

		endpoints, err := providerForZone.Records(ctx)
		if err != nil {
			log.Error(err, "failed tp get endpoints")
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
		log.Info(fmt.Sprintf("No active groups found for domain %s", domain))
		return nil
	}

	for zone, groups := range allActiveGroups {
		log.Info(fmt.Sprintf("Zone %s (ID %s):", zone.name, zone.id))
		for _, group := range groups {
			log.Info(fmt.Sprintf("\t%s", group))
		}
	}

	return nil
}
