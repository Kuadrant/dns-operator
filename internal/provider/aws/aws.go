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
	externaldnsprovideraws "sigs.k8s.io/external-dns/provider/aws"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
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
)

type Route53DNSProvider struct {
	*externaldnsprovideraws.AWSProvider
	awsConfig             externaldnsprovideraws.AWSConfig
	logger                logr.Logger
	route53Client         *route53.Route53
	ctx                   context.Context
	healthCheckReconciler provider.HealthCheckReconciler
}

var _ provider.Provider = &Route53DNSProvider{}

func NewProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	config := aws.NewConfig()
	sessionOpts := session.Options{
		Config: *config,
	}
	if string(s.Data["AWS_ACCESS_KEY_ID"]) == "" || string(s.Data["AWS_SECRET_ACCESS_KEY"]) == "" {
		return nil, fmt.Errorf("AWS Provider credentials is empty")
	}

	sessionOpts.Config.Credentials = credentials.NewStaticCredentials(string(s.Data["AWS_ACCESS_KEY_ID"]), string(s.Data["AWS_SECRET_ACCESS_KEY"]), "")
	sessionOpts.SharedConfigState = session.SharedConfigDisable
	sess, err := session.NewSessionWithOptions(sessionOpts)
	if err != nil {
		return nil, fmt.Errorf("unable to create aws session: %s", err)
	}
	if string(s.Data["REGION"]) != "" {
		sess.Config.WithRegion(string(s.Data["REGION"]))
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

	awsProvider, err := externaldnsprovideraws.NewAWSProvider(awsConfig, route53Client)
	if err != nil {
		return nil, fmt.Errorf("unable to create aws provider: %s", err)
	}

	p := &Route53DNSProvider{
		AWSProvider:   awsProvider,
		awsConfig:     awsConfig,
		logger:        log.Log.WithName("aws-route53").WithValues("region", config.Region),
		route53Client: route53Client,
		ctx:           ctx,
	}
	return p, nil
}

// #### External DNS Provider ####
func (p *Route53DNSProvider) HealthCheckReconciler() provider.HealthCheckReconciler {
	if p.healthCheckReconciler == nil {
		p.healthCheckReconciler = NewRoute53HealthCheckReconciler(p.route53Client)
	}

	return p.healthCheckReconciler
}

func (p *Route53DNSProvider) AdjustEndpoints(endpoints []*externaldnsendpoint.Endpoint) ([]*externaldnsendpoint.Endpoint, error) {
	endpoints, err := p.AWSProvider.AdjustEndpoints(endpoints)
	if err != nil {
		return nil, err
	}

	for _, ep := range endpoints {
		if prop, ok := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight); ok {
			ep.DeleteProviderSpecificProperty(v1alpha1.ProviderSpecificWeight)
			ep.WithProviderSpecific(providerSpecificWeight, prop)
		}

		if prop, ok := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode); ok {
			ep.DeleteProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode)
			if provider.IsISO3166Alpha2Code(prop) || prop == "*" {
				ep.WithProviderSpecific(providerSpecificGeolocationCountryCode, prop)
			} else {
				ep.WithProviderSpecific(providerSpecificGeolocationContinentCode, prop)
			}
		}
	}
	return endpoints, nil
}

// #### DNS Operator Provider ####

func (p *Route53DNSProvider) EnsureManagedZone(zone *v1alpha1.ManagedZone) (provider.ManagedZoneOutput, error) {
	var zoneID string
	if zone.Spec.ID != "" {
		zoneID = zone.Spec.ID
	} else {
		zoneID = zone.Status.ID
	}

	var managedZoneOutput provider.ManagedZoneOutput

	if zoneID != "" {
		getResp, err := p.route53Client.GetHostedZone(&route53.GetHostedZoneInput{
			Id: &zoneID,
		})
		if err != nil {
			log.Log.Error(err, "failed to get hosted zone")
			return managedZoneOutput, err
		}

		_, err = p.route53Client.UpdateHostedZoneComment(&route53.UpdateHostedZoneCommentInput{
			Comment: &zone.Spec.Description,
			Id:      &zoneID,
		})
		if err != nil {
			log.Log.Error(err, "failed to update hosted zone comment")
		}
		if getResp.HostedZone == nil {
			err = fmt.Errorf("aws zone issue. No hosted zone info in response")
			log.Log.Error(err, "unexpected response")
			return managedZoneOutput, err
		}
		if getResp.HostedZone.Id == nil {
			err = fmt.Errorf("aws zone issue. No hosted zone id in response")
			return managedZoneOutput, err
		}

		managedZoneOutput.ID = *getResp.HostedZone.Id
		if getResp.HostedZone.ResourceRecordSetCount != nil {
			managedZoneOutput.RecordCount = *getResp.HostedZone.ResourceRecordSetCount
		}

		managedZoneOutput.NameServers = []*string{}
		if getResp.DelegationSet != nil {
			managedZoneOutput.NameServers = getResp.DelegationSet.NameServers
		}

		return managedZoneOutput, nil
	}
	//ToDo callerRef must be unique, but this can cause duplicates if the status can't be written back during a
	//reconciliation that successfully created a new hosted zone i.e. the object has been modified; please apply your
	//changes to the latest version and try again
	callerRef := time.Now().Format("20060102150405")
	// Create the hosted zone
	createResp, err := p.route53Client.CreateHostedZone(&route53.CreateHostedZoneInput{
		CallerReference: &callerRef,
		Name:            &zone.Spec.DomainName,
		HostedZoneConfig: &route53.HostedZoneConfig{
			Comment:     &zone.Spec.Description,
			PrivateZone: aws.Bool(false),
		},
	})
	if err != nil {
		log.Log.Error(err, "failed to create hosted zone")
		return managedZoneOutput, err
	}
	if createResp.HostedZone == nil {
		err = fmt.Errorf("aws zone creation issue. No hosted zone info in response")
		log.Log.Error(err, "unexpected response")
		return managedZoneOutput, err
	}
	if createResp.HostedZone.Id == nil {
		err = fmt.Errorf("aws zone creation issue. No hosted zone id in response")
		return managedZoneOutput, err
	}
	managedZoneOutput.ID = *createResp.HostedZone.Id

	if createResp.HostedZone.ResourceRecordSetCount != nil {
		managedZoneOutput.RecordCount = *createResp.HostedZone.ResourceRecordSetCount
	}
	if createResp.DelegationSet != nil {
		managedZoneOutput.NameServers = createResp.DelegationSet.NameServers
	}
	return managedZoneOutput, nil
}

func (p *Route53DNSProvider) DeleteManagedZone(zone *v1alpha1.ManagedZone) error {
	_, err := p.route53Client.DeleteHostedZone(&route53.DeleteHostedZoneInput{
		Id: &zone.Status.ID,
	})
	if err != nil {
		log.Log.Error(err, "failed to delete hosted zone")
		return err
	}
	return nil
}

func (*Route53DNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{
		Weight:        providerSpecificWeight,
		HealthCheckID: ProviderSpecificHealthCheckID,
	}
}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider("aws", NewProviderFromSecret)
}
