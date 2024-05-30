package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/provider"
)

type zonesListCache struct {
	age      time.Time
	duration time.Duration
	zones    map[string]*route53.HostedZone
}

type ZonesAPI interface {
	GetZones(ctx context.Context) (map[string]*route53.HostedZone, error)
}

type ZonesAPIListAndFilter struct {
	client Route53API
	// only consider hosted zones managing domains ending in this suffix
	domainFilter endpoint.DomainFilter
	// filter hosted zones by id
	zoneIDFilter provider.ZoneIDFilter
	// filter hosted zones by type (e.g. private or public)
	zoneTypeFilter provider.ZoneTypeFilter
	// filter hosted zones by tags
	zoneTagFilter provider.ZoneTagFilter
	zonesCache    *zonesListCache
}

var _ ZonesAPI = &ZonesAPIListAndFilter{}

func NewZonesAPIListAndFilter(awsConfig AWSConfig, client Route53API) *ZonesAPIListAndFilter {
	return &ZonesAPIListAndFilter{
		client:         client,
		domainFilter:   awsConfig.DomainFilter,
		zoneIDFilter:   awsConfig.ZoneIDFilter,
		zoneTypeFilter: awsConfig.ZoneTypeFilter,
		zoneTagFilter:  awsConfig.ZoneTagFilter,
		zonesCache:     &zonesListCache{duration: awsConfig.ZoneCacheDuration},
	}
}

// Zones returns the list of hosted zones.
func (p ZonesAPIListAndFilter) GetZones(ctx context.Context) (map[string]*route53.HostedZone, error) {
	if p.zonesCache.zones != nil && time.Since(p.zonesCache.age) < p.zonesCache.duration {
		log.Debug("Using cached zones list")
		return p.zonesCache.zones, nil
	}
	log.Debug("Refreshing zones list cache")

	zones := make(map[string]*route53.HostedZone)

	var tagErr error
	f := func(resp *route53.ListHostedZonesOutput, lastPage bool) (shouldContinue bool) {
		for _, zone := range resp.HostedZones {
			if !p.zoneIDFilter.Match(aws.StringValue(zone.Id)) {
				continue
			}

			if !p.zoneTypeFilter.Match(zone) {
				continue
			}

			if !p.domainFilter.Match(aws.StringValue(zone.Name)) {
				continue
			}

			// Only fetch tags if a tag filter was specified
			if !p.zoneTagFilter.IsEmpty() {
				tags, err := p.tagsForZone(ctx, *zone.Id)
				if err != nil {
					tagErr = err
					return false
				}
				if !p.zoneTagFilter.Match(tags) {
					continue
				}
			}

			zones[aws.StringValue(zone.Id)] = zone
		}

		return true
	}

	err := p.client.ListHostedZonesPagesWithContext(ctx, &route53.ListHostedZonesInput{}, f)
	if err != nil {
		return nil, fmt.Errorf("failed to list hosted zones, %w", err)
	}
	if tagErr != nil {
		return nil, fmt.Errorf("failed to list zones tags, %w", tagErr)
	}

	for _, zone := range zones {
		log.Debugf("Considering zone: %s (domain: %s)", aws.StringValue(zone.Id), aws.StringValue(zone.Name))
	}

	if p.zonesCache.duration > time.Duration(0) {
		p.zonesCache.zones = zones
		p.zonesCache.age = time.Now()
	}

	return zones, nil
}

func (p *ZonesAPIListAndFilter) tagsForZone(ctx context.Context, zoneID string) (map[string]string, error) {
	response, err := p.client.ListTagsForResourceWithContext(ctx, &route53.ListTagsForResourceInput{
		ResourceType: aws.String("hostedzone"),
		ResourceId:   aws.String(zoneID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for zone %s, %w", zoneID, err)
	}
	tagMap := map[string]string{}
	for _, tag := range response.ResourceTagSet.Tags {
		tagMap[*tag.Key] = *tag.Value
	}
	return tagMap, nil
}

type ZonesAPISingleByID struct {
	client Route53API
	// filter hosted zones by id
	zoneIDFilter provider.ZoneIDFilter
}

var _ ZonesAPI = &ZonesAPISingleByID{}

func NewZonesAPISingleByID(awsConfig AWSConfig, client Route53API) *ZonesAPISingleByID {
	return &ZonesAPISingleByID{
		client:       client,
		zoneIDFilter: awsConfig.ZoneIDFilter,
	}
}

func (p ZonesAPISingleByID) GetZones(ctx context.Context) (map[string]*route53.HostedZone, error) {
	if !p.zoneIDFilter.IsConfigured() && len(p.zoneIDFilter.ZoneIDs) != 1 {
		return nil, fmt.Errorf("invalid zone id filter configuration %s", p.zoneIDFilter)
	}
	zoneID := p.zoneIDFilter.ZoneIDs[0]

	getResp, err := p.client.GetHostedZoneWithContext(ctx, &route53.GetHostedZoneInput{
		Id: &zoneID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get hosted zone %s, %w", zoneID, err)
	}
	if getResp.HostedZone == nil {
		return nil, fmt.Errorf("aws zone issue. No hosted zone info in response")
	}

	zone := getResp.HostedZone
	zones := make(map[string]*route53.HostedZone)
	zones[aws.StringValue(zone.Id)] = zone

	return zones, nil
}
