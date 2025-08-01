/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsprovideraws "github.com/kuadrant/dns-operator/internal/external-dns/provider/aws"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	ProviderSpecificHealthCheckID            = "aws/health-check-id"
	providerSpecificWeight                   = "aws/weight"
	providerSpecificGeolocationCountryCode   = "aws/geolocation-country-code"
	providerSpecificGeolocationContinentCode = "aws/geolocation-continent-code"
	awsBatchChangeSize                       = 1000
	awsBatchChangeInterval                   = time.Second
	awsEvaluateTargetHealth                  = false
	awsPreferCNAME                           = true
	awsZoneCacheDuration                     = 0 * time.Second
	providerContinentPrefix                  = "GEO-"
)

type Route53DNSProvider struct {
	*externaldnsprovideraws.AWSProvider
	awsConfig     externaldnsprovideraws.AWSConfig
	logger        logr.Logger
	route53Client *route53.Route53
}

var p provider.Provider = &Route53DNSProvider{}

func (*Route53DNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderAWS
}

func NewProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	config := aws.NewConfig()

	config.WithHTTPClient(metrics.NewInstrumentedClient(provider.DNSProviderAWS.String(), config.HTTPClient))

	sessionOpts := session.Options{
		Config: *config,
	}
	if string(s.Data[v1alpha1.AWSAccessKeyIDKey]) == "" || string(s.Data[v1alpha1.AWSSecretAccessKeyKey]) == "" {
		return nil, fmt.Errorf("AWS Provider credentials is empty")
	}

	sessionOpts.Config.Credentials = credentials.NewStaticCredentials(string(s.Data[v1alpha1.AWSAccessKeyIDKey]), string(s.Data[v1alpha1.AWSSecretAccessKeyKey]), "")
	sessionOpts.SharedConfigState = session.SharedConfigDisable
	sess, err := session.NewSessionWithOptions(sessionOpts)
	if err != nil {
		return nil, fmt.Errorf("unable to create aws session: %s", err)
	}
	if string(s.Data[v1alpha1.AWSRegionKey]) != "" {
		sess.Config.WithRegion(string(s.Data[v1alpha1.AWSRegionKey]))
	}

	route53Client := route53.New(sess, config)

	awsConfig := externaldnsprovideraws.AWSConfig{
		DomainFilter:         c.DomainFilter,
		ZoneIDFilter:         c.ZoneIDFilter,
		ZoneTypeFilter:       c.ZoneTypeFilter,
		ZoneTagFilter:        externaldnsprovider.NewZoneTagFilter([]string{}),
		BatchChangeSize:      awsBatchChangeSize,
		BatchChangeInterval:  awsBatchChangeInterval,
		EvaluateTargetHealth: awsEvaluateTargetHealth,
		PreferCNAME:          awsPreferCNAME,
		DryRun:               false,
		ZoneCacheDuration:    awsZoneCacheDuration,
	}

	logger := log.FromContext(ctx).WithName("aws-dns").WithValues("region", config.Region)
	ctx = log.IntoContext(ctx, logger)

	awsProvider, err := externaldnsprovideraws.NewAWSProvider(ctx, awsConfig, route53Client)
	if err != nil {
		return nil, fmt.Errorf("unable to create aws provider: %s", err)
	}

	p := &Route53DNSProvider{
		AWSProvider:   awsProvider,
		awsConfig:     awsConfig,
		logger:        logger,
		route53Client: route53Client,
	}
	return p, nil
}

// #### External DNS Provider ####

func (p *Route53DNSProvider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	endpoints, err := p.AWSProvider.AdjustEndpoints(endpoints)
	if err != nil {
		return nil, err
	}
	p.logger.V(1).Info("adjusting aws endpoints")
	for _, ep := range endpoints {
		if prop, ok := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight); ok {
			ep.DeleteProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
			ep.WithProviderSpecific(providerSpecificWeight, prop)
			p.logger.V(1).Info("set provider specific weight", "endpoint", ep)
		}

		if prop, ok := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode); ok {
			ep.DeleteProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
			prop = strings.ToUpper(prop)
			if strings.HasPrefix(prop, providerContinentPrefix) {
				continent := strings.Replace(prop, providerContinentPrefix, "", -1)
				if !provider.IsContinentCode(continent) {
					return nil, fmt.Errorf("unexpected continent code. %s", continent)
				}
				ep.WithProviderSpecific(providerSpecificGeolocationContinentCode, continent)
				p.logger.V(1).Info("set provider specific continent code base GEO- prefix", "endpoint", ep)
				continue
			}

			if provider.IsISO3166Alpha2Code(prop) || prop == "*" {
				ep.WithProviderSpecific(providerSpecificGeolocationCountryCode, prop)
				p.logger.V(1).Info("set provider specific country code", "endpoint", ep)
				continue
			}
			//if we get to here there is a value we cannot use
			return nil, fmt.Errorf("unexpected geo code. Prefix with %s for continents or use ISO_3166 Alpha 2 supported code for countries", providerContinentPrefix)
		}
	}
	return endpoints, nil
}

// #### DNS Operator Provider ####

func (p *Route53DNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	var hzs []provider.DNSZone
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	for _, z := range zones {
		hz := provider.DNSZone{
			ID:      *z.Id,
			DNSName: strings.ToLower(strings.TrimSuffix(*z.Name, ".")),
		}
		hzs = append(hzs, hz)
	}
	return hzs, nil
}

func (p *Route53DNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	zones, err := p.DNSZones(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FindDNSZoneForHost(ctx, host, zones, true)
}

func (*Route53DNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{
		Weight:        providerSpecificWeight,
		HealthCheckID: ProviderSpecificHealthCheckID,
	}
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider(p.Name().String(), NewProviderFromSecret, true)
}
