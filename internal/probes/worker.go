package probes

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/metrics"
)

var (
	ExpectedResponses = []int{200, 201}
)

const (
	PROBE_TIMEOUT = 3 * time.Second
)

type ProbeResult struct {
	CheckedAt metav1.Time
	Healthy   bool
	Reason    string
	Status    int
}

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type Probe struct {
	probeConfig  *v1alpha1.DNSHealthCheckProbe
	Transport    RoundTripperFunc
	probeHeaders v1alpha1.AdditionalHeaders
}

func NewProbe(probeConfig *v1alpha1.DNSHealthCheckProbe, headers v1alpha1.AdditionalHeaders) *Probe {
	return &Probe{
		probeConfig:  probeConfig,
		probeHeaders: headers,
	}
}

// Start a worker in a separate gouroutine. Returns a cancel func to kill the worker.
// If a worker is nil, no routines will start and cancel could be ignored (still returning it to prevent panic)
func (w *Probe) Start(clientctx context.Context, k8sClient client.Client) context.CancelFunc {
	ctx, cancel := context.WithCancel(clientctx)
	logger := log.FromContext(ctx)
	logger.Info("health: starting new worker for", "probe ", keyForProbe(w.probeConfig))
	metrics.ProbeCounter.WithLabelValues(w.probeConfig.Name, w.probeConfig.Namespace, w.probeConfig.Spec.Hostname).Inc()
	go func() {
		for range w.ExecuteProbe(ctx, w.probeConfig) {
			// each time this is done it will send a signal
			logger.V(1).Info("health: probe finished ", "updating status for probe", keyForProbe(w.probeConfig), "status", w.probeConfig.Status)
			err := k8sClient.Status().Update(clientctx, w.probeConfig)
			if err != nil {
				logger.Error(err, "health: probe finished. error updating probe status", "probe", keyForProbe(w.probeConfig))
			}
		}
		logger.V(1).Info("health: stopped executing probe", "probe", keyForProbe(w.probeConfig))
		metrics.ProbeCounter.WithLabelValues(w.probeConfig.Name, w.probeConfig.Namespace, w.probeConfig.Spec.Hostname).Dec()
	}()
	return cancel
}

func (w *Probe) ExecuteProbe(ctx context.Context, probe *v1alpha1.DNSHealthCheckProbe) <-chan struct{} {
	sig := make(chan struct{})

	go func() {
		logger := log.FromContext(ctx)
		for {
			if sig == nil || probe == nil {
				logger.Error(fmt.Errorf("channel or probe nil "), "exiting probe")
				return
			}
			timer := time.NewTimer(executeAt(probe))
			select {
			case <-ctx.Done():
				logger.V(1).Info("health: context shutdown. Stopping", "probe", keyForProbe(probe))
				if sig != nil {
					timer.Stop()
					close(sig)
					sig = nil
					logger.V(1).Info("health: context shutdown. time stopped and channel closed", "probe", keyForProbe(probe))
				}
				return
			case <-timer.C:
				result := w.execute(ctx, probe)
				probe.Status.ObservedGeneration = probe.Generation
				if !result.Healthy {
					probe.Status.ConsecutiveFailures++
				} else {
					probe.Status.ConsecutiveFailures = 0
				}
				probe.Status.Healthy = &result.Healthy
				probe.Status.LastCheckedAt = result.CheckedAt
				probe.Status.Reason = result.Reason
				logger.V(1).Info("health: execution complete ", "probe", keyForProbe(probe), "result", result)
				sig <- struct{}{}
			}
		}
	}()
	return sig
}

func executeAt(probe *v1alpha1.DNSHealthCheckProbe) time.Duration {
	timeUntilProbe := time.
		Until(probe.Status.LastCheckedAt.
			Time.Add(probe.Spec.Interval.Duration))
	if timeUntilProbe <= 0 {
		return 0
	}
	return timeUntilProbe

}

func (w *Probe) execute(ctx context.Context, probe *v1alpha1.DNSHealthCheckProbe) ProbeResult {
	logger := log.FromContext(ctx)
	logger.V(1).Info("kinperforming health check")
	var ips []net.IP

	//if the address is a CNAME, check all IP Addresses that it resolves to
	logger.V(1).Info("looking up address ", "address", probe.Spec.Address)
	ip := net.ParseIP(probe.Spec.Address)

	if ip == nil {
		IPAddr, err := net.LookupIP(probe.Spec.Address)
		if err != nil {
			logger.V(1).Error(err, "error looking up address", "address", probe.Spec.Address)
			return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
		}
		ips = append(ips, IPAddr...)
	} else {
		ips = append(ips, ip)
	}

	for _, ip = range ips {
		result := w.performRequest(ctx, string(probe.Spec.Protocol), probe.Spec.Hostname, probe.Spec.Path, ip.String(), probe.Spec.Port, probe.Spec.AllowInsecureCertificate, w.probeHeaders)
		// if any IP in a CNAME fails, it is a failed CNAME
		if !result.Healthy {
			return result
		}
	}

	// all IPs returned a healthy result
	return ProbeResult{
		CheckedAt: metav1.Now(),
		Healthy:   true,
	}
}

func (w *Probe) performRequest(ctx context.Context, protocol, host, path, ip string, port int, allowInsecure bool, headers v1alpha1.AdditionalHeaders) ProbeResult {
	logger := log.FromContext(ctx)
	probeClient := metrics.NewInstrumentedClient("probe", &http.Client{
		Transport: TransportWithDNSResponse(map[string]string{host: ip}, allowInsecure),
	})
	if w.Transport != nil {
		probeClient.Transport = w.Transport
	}
	url := fmt.Sprintf("%s://%s:%v%s", protocol, host, port, path)
	if port == 0 {
		url = fmt.Sprintf("%s://%s%s", protocol, host, path)
	}

	// Build the http request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
	}

	for _, h := range headers {
		httpReq.Header.Add(h.Name, h.Value)
	}

	logger.V(1).Info("health: probe executing against ", "url", httpReq.URL)

	// Send the request
	res, err := probeClient.Do(httpReq)
	if utilnet.IsConnectionReset(err) {
		res = &http.Response{StatusCode: 104}
	} else if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: fmt.Sprintf("error: %s, response: %+v", err.Error(), res)}
	}

	if !slice.Contains[int](ExpectedResponses, func(i int) bool { return i == res.StatusCode }) {
		return ProbeResult{
			CheckedAt: metav1.Now(),
			Healthy:   false,
			Status:    res.StatusCode,
			Reason:    fmt.Sprintf("Status code: %d", res.StatusCode),
		}
	}

	return ProbeResult{
		CheckedAt: metav1.Now(),
		Healthy:   true,
	}
}

// TransportWithDNSResponse creates a new transport which overrides hostnames.
func TransportWithDNSResponse(overrides map[string]string, allowInsecureCertificates bool) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{
		Timeout:   PROBE_TIMEOUT,
		KeepAlive: PROBE_TIMEOUT,
	}

	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		newHost, ok := overrides[host]
		if !ok {
			return dialer.DialContext(ctx, network, address)
		}
		overrideAddress := net.JoinHostPort(newHost, port)
		return dialer.DialContext(ctx, network, overrideAddress)
	}

	transport.TLSClientConfig.InsecureSkipVerify = allowInsecureCertificates

	return transport
}

type ProbeManager struct {
	probes map[string]context.CancelFunc
}

func NewProbeManager() *ProbeManager {
	return &ProbeManager{
		probes: map[string]context.CancelFunc{},
	}
}

// StopProbeWorker stops the worker and removes it from the WorkerManager
func (m *ProbeManager) StopProbeWorker(ctx context.Context, probeCR *v1alpha1.DNSHealthCheckProbe) {
	logger := log.FromContext(ctx)
	if stop, ok := m.probes[keyForProbe(probeCR)]; ok {
		logger.V(1).Info("health: Stopping existing worker", "probe", keyForProbe(probeCR))
		stop()
		delete(m.probes, keyForProbe(probeCR))
	}
}

// EnsureProbeWorker ensures a new worker per generation of the probe.
// New generation of probe - new worker.
// If the generation has not changed, it will re-create a worker. If context is done (we are deleting) that worker will die immediately.
func (m *ProbeManager) EnsureProbeWorker(ctx context.Context, k8sClient client.Client, probeCR *v1alpha1.DNSHealthCheckProbe, headers v1alpha1.AdditionalHeaders) {
	logger := log.FromContext(ctx)
	logger.Info("ensure probe")

	// if worker exists
	if stop, ok := m.probes[keyForProbe(probeCR)]; ok {
		// gen has not changed (spec has not changed) - nothing to do,
		// or first reconcile of the probe but worker already in place
		if probeCR.Status.ObservedGeneration == probeCR.Generation || probeCR.Status.ObservedGeneration == 0 {
			logger.V(1).Info("health: probe worker exists for generation. Continuing", "probe", keyForProbe(probeCR))
			return
		}
		logger.V(1).Info("health: worker already exists. New generation of the probe: stopping existing worker", "probe", keyForProbe(probeCR))
		stop()
	}
	// Either worker does not exist, or gen changed and old worker got killed. Creating a new one.
	logger.V(1).Info("health: starting fresh worker for", "generation", probeCR.Generation, "probe", keyForProbe(probeCR))
	probe := NewProbe(probeCR, headers)
	m.probes[keyForProbe(probeCR)] = probe.Start(ctx, k8sClient)
}

func keyForProbe(probe *v1alpha1.DNSHealthCheckProbe) string {
	return fmt.Sprintf("%s/%s", probe.Name, probe.Namespace)
}
