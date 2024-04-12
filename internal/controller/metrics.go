package controller

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	dnsRecordNameLabel      = "dns_record_name"
	dnsRecordNamespaceLabel = "dns_record_namespace"
)

var (
	wrtiteCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_write_counter",
			Help: "Counts DNS provider write operations for a current generation of the DNS record",
		},
		[]string{dnsRecordNameLabel, dnsRecordNamespaceLabel})
)

func init() {
	metrics.Registry.MustRegister(wrtiteCounter)
}
