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
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/route53"
)

type InstrumentedRoute53 struct {
	route53 *route53.Route53
}

func observe(operation string, f func() error) {
	start := time.Now()
	route53RequestCount.WithLabelValues(operation).Inc()
	defer route53RequestCount.WithLabelValues(operation).Dec()
	err := f()
	duration := time.Since(start).Seconds()
	code := returnCodeLabelDefault
	if err != nil {
		route53RequestErrors.WithLabelValues(operation, code).Inc()
		if awsErr, ok := err.(awserr.Error); ok {
			if reqErr, ok := err.(awserr.RequestFailure); ok {
				// A service error occurred
				code = strconv.Itoa(reqErr.StatusCode())
			} else {
				// Generic AWS Error with Code, Message, and original error (if any)
				code = awsErr.Code()
			}
		}
	}
	route53RequestDuration.WithLabelValues(operation, code).Observe(duration)
	route53RequestTotal.WithLabelValues(operation, code).Inc()
}

func (c *InstrumentedRoute53) ListHostedZones(input *route53.ListHostedZonesInput) (output *route53.ListHostedZonesOutput, err error) {
	observe("ListHostedZones", func() error {
		output, err = c.route53.ListHostedZones(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (output *route53.ChangeResourceRecordSetsOutput, err error) {
	observe("ChangeResourceRecordSets", func() error {
		output, err = c.route53.ChangeResourceRecordSets(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) CreateHealthCheck(input *route53.CreateHealthCheckInput) (output *route53.CreateHealthCheckOutput, err error) {
	observe("CreateHealthCheck", func() error {
		output, err = c.route53.CreateHealthCheck(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) GetHostedZone(input *route53.GetHostedZoneInput) (output *route53.GetHostedZoneOutput, err error) {
	observe("GetHostedZone", func() error {
		output, err = c.route53.GetHostedZone(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) UpdateHostedZoneComment(input *route53.UpdateHostedZoneCommentInput) (output *route53.UpdateHostedZoneCommentOutput, err error) {
	observe("UpdateHostedZoneComment", func() error {
		output, err = c.route53.UpdateHostedZoneComment(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) CreateHostedZone(input *route53.CreateHostedZoneInput) (output *route53.CreateHostedZoneOutput, err error) {
	observe("CreateHostedZone", func() error {
		output, err = c.route53.CreateHostedZone(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) DeleteHostedZone(input *route53.DeleteHostedZoneInput) (output *route53.DeleteHostedZoneOutput, err error) {
	observe("DeleteHostedZone", func() error {
		output, err = c.route53.DeleteHostedZone(input)
		return err
	})
	return
}

func (c *InstrumentedRoute53) ChangeTagsForResourceWithContext(ctx aws.Context, input *route53.ChangeTagsForResourceInput, opts ...request.Option) (output *route53.ChangeTagsForResourceOutput, err error) {
	observe("ChangeTagsForResourceWithContext", func() error {
		output, err = c.route53.ChangeTagsForResourceWithContext(ctx, input, opts...)
		return err
	})
	return
}
