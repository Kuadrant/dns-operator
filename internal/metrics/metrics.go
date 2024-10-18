package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	dnsRecordNameLabel           = "dns_record_name"
	dnsRecordNamespaceLabel      = "dns_record_namespace"
	dnsHealthCheckNameLabel      = "dns_health_check_name"
	dnsHealthCheckNamespaceLabel = "dns_health_check_namespace"
	dnsHealthCheckHostLabel      = "dns_health_check_host"
	mzRecordNameLabel            = "managed_zone_name"
	mzRecordNamespaceLabel       = "managed_zone_namespace"
	mzSecretNameLabel            = "managed_zone_secret_name"
)

var (
	WriteCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_write_counter",
			Help: "Counts DNS provider write operations for a current generation of the DNS record",
		},
		[]string{dnsRecordNameLabel, dnsRecordNamespaceLabel})
	ProbeCounter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_health_probe_counter",
			Help: "Count of active probes",
		},
		[]string{dnsHealthCheckNameLabel, dnsHealthCheckNamespaceLabel, dnsHealthCheckHostLabel})
	SecretMissing = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_secret_absent",
			Help: "Emits one when provider secret is found to be absent, or zero when expected secrets exist",
		},
		[]string{mzRecordNameLabel, mzRecordNamespaceLabel, mzSecretNameLabel})
)

func init() {
	metrics.Registry.MustRegister(WriteCounter)
	metrics.Registry.MustRegister(SecretMissing)
	metrics.Registry.MustRegister(ProbeCounter)
}
