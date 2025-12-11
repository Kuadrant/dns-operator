package metrics

import (
	"context"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"k8s.io/apimachinery/pkg/api/meta"
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
	writeCounter = prometheus.NewDesc(
		"dns_provider_write_counter",
		"Counts DNS provider write operations for a current generation of the DNS record",
		[]string{dnsRecordNameLabel, dnsRecordNamespaceLabel},
		nil,
	)

	probeCounter = prometheus.NewDesc(
		"dns_health_probe_counter",
		"Count of active probes",
		[]string{dnsHealthCheckNameLabel, dnsHealthCheckNamespaceLabel, dnsHealthCheckHostLabel},
		nil,
	)
	// TODO: Move to collector pattern
	SecretMissing = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_provider_secret_absent",
			Help: "Emits one when provider secret is found to be absent, or zero when expected secrets exist",
		},
		[]string{mzRecordNameLabel, mzRecordNamespaceLabel, mzSecretNameLabel})

	recordReady = prometheus.NewDesc(
		"dns_provider_record_ready",
		"Reports the ready state of the dns record. 0 - not ready, 1 - ready. It also provides some metadata of the record in question",
		[]string{dnsRecordNameLabel, dnsRecordNamespaceLabel, dnsRecordRootHost, dnsRecordDelegating},
		nil,
	)

	remoteRecords = prometheus.NewDesc(
		"dns_provider_remote_records",
		"Reports the delegated dns records on a remote cluster",
		[]string{"cluster"},
		nil,
	)

	remoteRecordReconcile = prometheus.NewDesc(
		"dns_provider_remote_record_reconcile_count",
		"Reports the reconcile count for a remote DNSRecord (collector pattern)",
		[]string{"cluster", dnsRecordNameLabel, dnsRecordNamespaceLabel},
		nil,
	)

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

func writeCounterMetric(ch chan<- prometheus.Metric, record v1alpha1.DNSRecord) {
	ch <- prometheus.MustNewConstMetric(
		writeCounter,
		prometheus.GaugeValue,
		float64(record.Status.WriteCounter),
		record.Name,
		record.Namespace,
	)
}

func recordReadyMetric(ch chan<- prometheus.Metric, record v1alpha1.DNSRecord) {
	ready := meta.IsStatusConditionTrue(record.Status.Conditions, string(v1alpha1.ConditionTypeReady))
	var gauge float64
	if ready {
		gauge = 1
	}
	ch <- prometheus.MustNewConstMetric(
		recordReady,
		prometheus.GaugeValue,
		gauge,
		record.Name,
		record.Namespace,
		record.GetRootHost(),
		strconv.FormatBool(record.IsDelegating()),
	)
}

type RemoteClusterCollector struct {
	Provider                *kubeconfigprovider.Provider
	mutex                   sync.Mutex
	remoteReconcileCounters map[string]*remoteRecordCounter // key format: "cluster:namespace:name"
	remoteRecordsCounts     map[string]float64              // key format: "cluster"
}

type remoteRecordCounter struct {
	cluster   string
	namespace string
	name      string
	count     int64
}

func NewRemoteClusterCollector(provider *kubeconfigprovider.Provider) *RemoteClusterCollector {
	return &RemoteClusterCollector{
		Provider:                provider,
		remoteReconcileCounters: make(map[string]*remoteRecordCounter),
		remoteRecordsCounts:     make(map[string]float64),
	}
}

func (c *RemoteClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- multiClusterCount
	ch <- remoteRecords
	ch <- remoteRecordReconcile
}

func (c *RemoteClusterCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		multiClusterCount,
		prometheus.GaugeValue,
		float64(len(c.Provider.ListClusters())),
	)

	c.mutex.Lock()
	defer c.mutex.Unlock()

	for cluster, count := range c.remoteRecordsCounts {
		ch <- prometheus.MustNewConstMetric(
			remoteRecords,
			prometheus.GaugeValue,
			count,
			cluster,
		)
	}

	for _, counter := range c.remoteReconcileCounters {
		ch <- prometheus.MustNewConstMetric(
			remoteRecordReconcile,
			prometheus.CounterValue,
			float64(counter.count),
			counter.cluster,
			counter.name,
			counter.namespace,
		)
	}
}

// IncRemoteRecordReconcile increments the counter for a specific cluster/namespace/name combination
func (c *RemoteClusterCollector) IncRemoteRecordReconcile(cluster, namespace, name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	key := cluster + ":" + namespace + ":" + name
	if counter, exists := c.remoteReconcileCounters[key]; exists {
		counter.count++
	} else {
		c.remoteReconcileCounters[key] = &remoteRecordCounter{
			cluster:   cluster,
			namespace: namespace,
			name:      name,
			count:     1,
		}
	}
}

// RemoveRemoteRecordReconcile removes the counter for a specific cluster/namespace/name combination
func (c *RemoteClusterCollector) RemoveRemoteRecordReconcile(cluster, namespace, name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	key := cluster + ":" + namespace + ":" + name
	delete(c.remoteReconcileCounters, key)
}

// SetRemoteRecordsCount sets the count of delegated remote records for a specific cluster
func (c *RemoteClusterCollector) SetRemoteRecordsCount(cluster string, count float64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.remoteRecordsCounts[cluster] = count
}

type ProbeCollector struct {
	mutex         sync.Mutex
	probeCounters map[string]*probeCounterEntry // key format: "name:namespace:hostname"
}

type probeCounterEntry struct {
	name      string
	namespace string
	hostname  string
	count     int64
}

func NewProbeCollector() *ProbeCollector {
	return &ProbeCollector{
		probeCounters: make(map[string]*probeCounterEntry),
	}
}

func (c *ProbeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- probeCounter
}

func (c *ProbeCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, counter := range c.probeCounters {
		ch <- prometheus.MustNewConstMetric(
			probeCounter,
			prometheus.GaugeValue,
			float64(counter.count),
			counter.name,
			counter.namespace,
			counter.hostname,
		)
	}
}

// IncProbeCounter increments the counter for a specific probe
func (c *ProbeCollector) IncProbeCounter(name, namespace, hostname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	key := name + ":" + namespace + ":" + hostname
	if counter, exists := c.probeCounters[key]; exists {
		counter.count++
	} else {
		c.probeCounters[key] = &probeCounterEntry{
			name:      name,
			namespace: namespace,
			hostname:  hostname,
			count:     1,
		}
	}
}

// DecProbeCounter decrements the counter for a specific probe
func (c *ProbeCollector) DecProbeCounter(name, namespace, hostname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	key := name + ":" + namespace + ":" + hostname
	if counter, exists := c.probeCounters[key]; exists {
		counter.count--
		// Remove the entry if count reaches 0
		if counter.count <= 0 {
			delete(c.probeCounters, key)
		}
	}
}

type LocalCollector struct {
	Ctx context.Context
	Mgr manager.Manager
}

func (c *LocalCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- authoritativeRecordSpecInfo
	ch <- recordReady
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
		recordReadyMetric(ch, record)
		writeCounterMetric(ch, record)
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

func init() {
	metrics.Registry.MustRegister(SecretMissing)
}
