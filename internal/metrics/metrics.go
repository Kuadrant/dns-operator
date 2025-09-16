package metrics

import (
	"context"
	"errors"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

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
	authoritativeRecordSpecInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_authoritative_record_spec_info",
			Help: "Provides information about authoritative DNS records, identified by label values.",
		},
		[]string{"root_host", "sha", dnsRecordNameLabel, dnsRecordNamespaceLabel},
	)
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
	activeCluster = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_active_multi_cluster_count",
			Help: "Reports the number of secrets configured for multi cluster configuration",
		},
		[]string{})
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

func NewActiveClustersMetric(ctx context.Context, cl client.Client, logger logr.Logger, ns string, label string) activeClusterMetric {
	return activeClusterMetric{
		ctx:       ctx,
		client:    cl,
		logger:    logger,
		namespace: ns,
		label:     label,
	}
}

type activeClusterMetric struct {
	ctx       context.Context
	client    client.Client
	logger    logr.Logger
	namespace string
	label     string
}

func (m *activeClusterMetric) Publish() {

	labelSelector := labels.SelectorFromSet(labels.Set{
		m.label: "true",
	})

	listOptions := &client.ListOptions{
		Namespace:     m.namespace,
		LabelSelector: labelSelector,
	}

	secretList := &corev1.SecretList{}

	err := m.client.List(m.ctx, secretList, listOptions)
	if err != nil {
		m.logger.Error(err, "unable to get list of secrets used for multi cluster setup, metric can not be published")
		return
	}

	secretCount := float64(len(secretList.Items))
	activeCluster.WithLabelValues().Set(secretCount)
}

func NewAuthoritativeRecordSpecInfoMetric(dnsRecord *v1alpha1.DNSRecord) (*authoritativeRecordSpecInfoMetric, error) {
	if dnsRecord == nil {
		return nil, errors.New("dnsRecord nil pointer")
	}

	metric := &authoritativeRecordSpecInfoMetric{
		RootHost:  dnsRecord.Spec.RootHost,
		Name:      dnsRecord.Name,
		NameSpace: dnsRecord.Namespace,
	}
	err := metric.calculateSha(dnsRecord)
	if err != nil {
		return nil, err
	}
	return metric, nil
}

type authoritativeRecordSpecInfoMetric struct {
	RootHost  string
	Sha       string
	Name      string
	NameSpace string
}

func (m *authoritativeRecordSpecInfoMetric) calculateSha(dnsRecord *v1alpha1.DNSRecord) error {
	if dnsRecord == nil {
		return errors.New("dnsRecord nil pointer")
	}

	specString, err := hash.GetCanonicalString(dnsRecord.Spec)
	if err != nil {
		return err
	}
	m.Sha = hash.ToBase36HashLen(specString, 6)
	return nil
}

func (m *authoritativeRecordSpecInfoMetric) Publish() {
	authoritativeRecordSpecInfo.WithLabelValues(m.RootHost, m.Sha, m.Name, m.NameSpace).Set(float64(1))
}

func init() {
	metrics.Registry.MustRegister(WriteCounter)
	metrics.Registry.MustRegister(SecretMissing)
	metrics.Registry.MustRegister(ProbeCounter)
	metrics.Registry.MustRegister(RecordReady)
	metrics.Registry.MustRegister(authoritativeRecordSpecInfo)
	metrics.Registry.MustRegister(activeCluster)
	metrics.Registry.MustRegister(remoteRecords)
	metrics.Registry.MustRegister(remoteRecordReconcile)
}
