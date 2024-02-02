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
	"reflect"

	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	operationLabel  = "operation"
	returnCodeLabel = "code"
	// The default return code
	returnCodeLabelDefault = ""
)

var (
	// route53RequestCount is a prometheus metric which holds the number of
	// concurrent inflight requests to Route53.
	route53RequestCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mgc_aws_route53_inflight_request_count",
			Help: "MGC AWS Route53 inflight request count",
		},
		[]string{operationLabel},
	)

	// route53RequestTotal is a prometheus counter metrics which holds the total
	// number of requests to Route53.
	route53RequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mgc_aws_route53_request_total",
			Help: "MGC AWS Route53 total number of requests",
		},
		[]string{operationLabel, returnCodeLabel},
	)

	// route53RequestErrors is a prometheus counter metrics which holds the total
	// number of failed requests to Route53.
	route53RequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mgc_aws_route53_request_errors_total",
			Help: "MGC AWS Route53 total number of errors",
		},
		[]string{operationLabel, returnCodeLabel},
	)

	// route53RequestDuration is a prometheus metric which records the duration
	// of the requests to Route53.
	route53RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "mgc_aws_route53_request_duration_seconds",
			Help: "MGC AWS Route53 request duration",
			Buckets: []float64{
				0.005, 0.01, 0.025, 0.05, 0.1,
				0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45,
				0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
				1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5,
				5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60,
			},
		},
		[]string{operationLabel, returnCodeLabel},
	)
)

var operationLabelValues []string

func init() {
	// Register metrics into the global prometheus registry
	metrics.Registry.MustRegister(
		route53RequestCount,
		route53RequestTotal,
		route53RequestErrors,
		route53RequestDuration,
	)

	monitoredRoute53 := reflect.PtrTo(reflect.TypeOf(InstrumentedRoute53{}))
	for i := 0; i < monitoredRoute53.NumMethod(); i++ {
		operationLabelValues = append(operationLabelValues, monitoredRoute53.Method(i).Name)
	}

	// Initialize metrics
	for _, operation := range operationLabelValues {
		route53RequestCount.WithLabelValues(operation).Set(0)
		route53RequestTotal.WithLabelValues(operation, returnCodeLabelDefault).Add(0)
		route53RequestErrors.WithLabelValues(operation, returnCodeLabelDefault).Add(0)
	}
}
