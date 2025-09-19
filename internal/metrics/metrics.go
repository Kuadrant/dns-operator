package metrics

import (
	"context"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	kubeconfigprovider "sigs.k8s.io/multicluster-runtime/providers/kubeconfig"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/hash"
)

const (
	dnsRecordNameLabel           = "dns_record_name"
	dnsRecordNamespaceLabel      = "dns_record_namespace"
	dnsRecordRootHost            = "dns_record_root_host"
	dnsRecordDelegating          = "dns_record_is_delegating"
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
	RecordReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_record_ready",
			Help: "Reports the ready state of the dns record. 0 - not ready or deleted, 1 - ready. It also provides some metadata of the record in question",
		},
		[]string{dnsRecordNameLabel, dnsRecordNamespaceLabel, dnsRecordRootHost, dnsRecordDelegating})
	remoteRecords = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_remote_records",
			Help: "Reports the delegated dns records on a remote cluster",
		},
		[]string{"cluster"})
	remoteRecordReconcile = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dns_provider_remote_record_reconcile_count",
			Help: "Reports the reconcile count for a remote DNSRecord",
		},
		[]string{"cluster", dnsRecordNameLabel, dnsRecordNamespaceLabel})

	authoritativeRecordSpecInfo = prometheus.NewDesc(
		"dns_provider_authoritative_record_spec_info",
		"Provides intformation aboutauthoritative DNS records, indentiffied by label values.",
		[]string{"root_host", "sha", dnsRecordNameLabel, dnsRecordNamespaceLabel},
		nil,
	)
	multiClusterCount = prometheus.NewDesc(
		"dns_provider_active_multi_cluster_count",
		"Reports the number of secrets configured for multi cluster configuration",
		nil,
		nil,
	)
)

func NewRecordReadyMetric(record *v1alpha1.DNSRecord, ready bool) recordReadyMetric {
	return recordReadyMetric{
		name:       record.GetName(),
		namespace:  record.GetNamespace(),
		rootHost:   record.GetRootHost(),
		delegating: record.IsDelegating(),
		ready:      ready,
	}
}

type recordReadyMetric struct {
	name       string
	namespace  string
	rootHost   string
	delegating bool
	ready      bool
}

func (m *recordReadyMetric) Publish() {
	var gauge float64
	if m.ready {
		gauge = 1
	}
	RecordReady.WithLabelValues(m.name, m.namespace, m.rootHost, strconv.FormatBool(m.delegating)).Set(gauge)
}

type RemoteClusterCollector struct {
	Provider *kubeconfigprovider.Provider
}

func (c *RemoteClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- multiClusterCount
}

func (c *RemoteClusterCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		multiClusterCount,
		prometheus.GaugeValue,
		float64(len(c.Provider.ListClusters())),
	)
}

type LocalCollector struct {
	Ctx context.Context
	Mgr manager.Manager
}

func (c *LocalCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- authoritativeRecordSpecInfo
}

func (c *LocalCollector) Collect(ch chan<- prometheus.Metric) {

	cl := c.Mgr.GetClient()
	logger := c.Mgr.GetLogger()

	listOptions := &client.ListOptions{}
	dnsRecordList := &v1alpha1.DNSRecordList{}

	err := cl.List(c.Ctx, dnsRecordList, listOptions)
	if err != nil {
		logger.Info("failed to list dns records", "status", "error")
	}

	for _, record := range dnsRecordList.Items {
		if record.IsAuthoritativeRecord() {
			specString, err := hash.GetCanonicalString(record.Spec)
			if err != nil {
				logger.Info("failed to list dns records", "status", "error")
				continue
			}
			value := hash.ToBase36HashLen(specString, 6)
			ch <- prometheus.MustNewConstMetric(
				authoritativeRecordSpecInfo,
				prometheus.GaugeValue,
				1,
				record.GetRootHost(),
				value,
				record.Name,
				record.Namespace,
			)
		}
	}
}

func NewRemoteRecordReconcileMetric(name, namespace, cluster string) remoteRecordReconcileMetric {
	return remoteRecordReconcileMetric{
		name:      name,
		namespace: namespace,
		cluster:   cluster,
	}
}

type remoteRecordReconcileMetric struct {
	name      string
	namespace string
	cluster   string
}

func (m *remoteRecordReconcileMetric) Publish() {
	remoteRecordReconcile.WithLabelValues(m.cluster, m.name, m.namespace).Inc()
}

func NewRemoteRecordsMetric(ctx context.Context, cl client.Client, logger logr.Logger, cluster string) remoteRecordsMetric {
	return remoteRecordsMetric{
		ctx:     ctx,
		client:  cl,
		logger:  logger,
		cluster: cluster,
	}
}

type remoteRecordsMetric struct {
	ctx     context.Context
	client  client.Client
	logger  logr.Logger
	cluster string
}

func (m *remoteRecordsMetric) Publish() {

	listOptions := &client.ListOptions{}
	dnsRecordList := &v1alpha1.DNSRecordList{}

	err := m.client.List(m.ctx, dnsRecordList, listOptions)
	if err != nil {
		m.logger.Error(err, "unable to get list of dnsRecords on secondary cluster, metric can not be published")
		return
	}

	count := float64(0)
	for _, record := range dnsRecordList.Items {
		if record.IsDelegating() {
			count += 1
		}
	}

	remoteRecords.WithLabelValues(m.cluster).Set(count)
}

func init() {
	metrics.Registry.MustRegister(WriteCounter)
	metrics.Registry.MustRegister(SecretMissing)
	metrics.Registry.MustRegister(ProbeCounter)
	metrics.Registry.MustRegister(RecordReady)
	metrics.Registry.MustRegister(remoteRecords)
	metrics.Registry.MustRegister(remoteRecordReconcile)
}
