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
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/metrics"
)

var (
	ExpectedResponses = []int{200, 201}

	ProbeDelay = float64(time.Second.Milliseconds())
)

const (
	PROBE_TIMEOUT = 3 * time.Second

	ProbeDelayVariance = 0.5
)

type ProbeResult struct {
	// CheckedAt the current helath check time
	CheckedAt metav1.Time
	// PreviousCheck the time it was checked before the current check
	PreviousCheck metav1.Time
	Healthy       bool
	Reason        string
	Status        int
}

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type Probe struct {
	Transport    RoundTripperFunc
	probeHeaders v1alpha1.AdditionalHeaders
}

func NewProbe(headers v1alpha1.AdditionalHeaders) *Probe {
	return &Probe{
		probeHeaders: headers,
	}
}

// ExecuteProbe executes the health check request on a background routine at the correct interval. It returns a channel on which it will send each probe result
func (w *Probe) ExecuteProbe(ctx context.Context, probe *v1alpha1.DNSHealthCheckProbe) <-chan ProbeResult {
	sig := make(chan ProbeResult)
	localProbe := probe.DeepCopy()
	go func() {
		logger := log.FromContext(ctx).WithValues("health probe worker:", keyForProbe(localProbe))
		for {
			if sig == nil || localProbe == nil {
				logger.Error(fmt.Errorf("channel or probe nil "), "exiting probe")
				return
			}
			timer := time.NewTimer(executeAt(localProbe))
			select {
			case <-ctx.Done():
				logger.V(2).Info("health probe worker: context shutdown. Stopping")
				if sig != nil {
					timer.Stop()
					close(sig)
					sig = nil
					logger.V(2).Info("health probe worker: context shutdown. time stopped and channel closed")
				}
				return
			case <-timer.C:
				logger.V(2).Info("health probe worker: executing")
				result := w.execute(ctx, localProbe)
				// set the previous check time from the exsting probe
				result.PreviousCheck = localProbe.Status.LastCheckedAt
				// as this routine is just executing the local config it only cares about when it should execute again
				// set the lastCheck based on the result
				localProbe.Status.LastCheckedAt = result.CheckedAt
				sig <- result
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
	logger := log.FromContext(ctx).WithValues("health probe worker:", keyForProbe(probe))
	logger.V(2).Info("performing health check")
	var ips []net.IP

	//if the address is a CNAME, check all IP Addresses that it resolves to
	logger.V(2).Info("looking up address ", "address", probe.Spec.Address)
	ip := net.ParseIP(probe.Spec.Address)

	if ip == nil {
		IPAddr, err := net.LookupIP(probe.Spec.Address)
		if err != nil {
			logger.Error(err, "error looking up address", "address", probe.Spec.Address)
			return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
		}
		ips = append(ips, IPAddr...)
	} else {
		ips = append(ips, ip)
	}
	var result ProbeResult
	for _, ip = range ips {
		result = w.performRequest(ctx, string(probe.Spec.Protocol), probe.Spec.Hostname, probe.Spec.Path, ip.String(), probe.Spec.Port, probe.Spec.AllowInsecureCertificate, w.probeHeaders)
		// return as any healthy IP is a good result (multiple can only really happen with a CNAME)
		if result.Healthy {
			return result
		}
	}
	//TODO deal with multiple results for a CNAME better and don't just rely on last result
	// all IPs returned an unhealthy. Result will be the last ProbeResult so return this to have some status set
	return result
}

func (w *Probe) performRequest(ctx context.Context, protocol, host, path, ip string, port int, allowInsecure bool, headers v1alpha1.AdditionalHeaders) ProbeResult {
	logger := log.FromContext(ctx).WithValues("health probe worker:", "preforming request")
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

	logger.V(2).Info("health: probe executing against ", "url", httpReq.URL)

	// Send the request
	res, err := probeClient.Do(httpReq)
	if utilnet.IsConnectionReset(err) {
		res = &http.Response{StatusCode: 104}
	} else if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: fmt.Sprintf("error: %s, response: %+v", err.Error(), res)}
	}
	logger.V(2).Info("health: probe execution complete against ", "url", httpReq.URL, "status code", res.StatusCode)
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
		Status:    res.StatusCode,
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
	logger := log.FromContext(ctx).WithValues("health probe worker:", keyForProbe(probeCR))
	if stop, ok := m.probes[keyForProbe(probeCR)]; ok {
		logger.V(2).Info("Stopping existing worker", "probe", keyForProbe(probeCR))
		stop()
		delete(m.probes, keyForProbe(probeCR))
	}
}

// Start a worker in a separate gouroutine. Returns a cancel func to kill the worker.
// If a worker is nil, no routines will start and cancel could be ignored (still returning it to prevent panic)
func (w *Probe) Start(clientctx context.Context, k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) context.CancelFunc {

	ctx, cancel := context.WithCancel(clientctx)
	logger := log.FromContext(ctx).WithValues("probe", keyForProbe(probe))
	logger.V(1).Info("health: starting new worker for probe")

	go func() {
		// jitter probe execution so we aren't starting at the same time
		time.Sleep(common.RandomizeDuration(ProbeDelayVariance, ProbeDelay))

		metrics.ProbeCounter.WithLabelValues(probe.Name, probe.Namespace, probe.Spec.Hostname).Inc()
		//each time the probe executes it will send a result on the channel returned by ExecuteProbe until the probe is cancelled. The probe can be cancelled by a new spec being created for the healthcheck or on shutdown
		for probeResult := range w.ExecuteProbe(ctx, probe) {
			freshProbe := &v1alpha1.DNSHealthCheckProbe{}
			if err := k8sClient.Get(clientctx, client.ObjectKeyFromObject(probe), freshProbe); err != nil {
				// if we hit an error here we cancel and return as it is an unusual state
				logger.Error(err, "health: probe finished. error getting upto date probe. Cancelling")
				cancel()
				return
			}
			freshProbe.Status.ObservedGeneration = freshProbe.Generation
			if !probeResult.Healthy {
				freshProbe.Status.ConsecutiveFailures++
				if freshProbe.Status.ConsecutiveFailures > freshProbe.Spec.FailureThreshold {
					freshProbe.Status.Healthy = &probeResult.Healthy
				}
			} else {
				freshProbe.Status.ConsecutiveFailures = 0
				freshProbe.Status.Healthy = &probeResult.Healthy
			}
			logger.V(1).Info("health: execution complete ", "result", probeResult, "checked at", probeResult.CheckedAt.String(), "previoud check at ", probeResult.PreviousCheck)
			freshProbe.Status.LastCheckedAt = probeResult.CheckedAt
			freshProbe.Status.Reason = probeResult.Reason
			freshProbe.Status.Status = probeResult.Status

			logger.V(2).Info("health: probe finished updating status for probe", "status", freshProbe)
			err := k8sClient.Status().Update(clientctx, freshProbe)
			if err != nil {
				logger.Error(err, "health: probe finished. error updating probe status")
			}
		}

		logger.V(1).Info("health: stopped executing probe", "probe", keyForProbe(probe))
		metrics.ProbeCounter.WithLabelValues(probe.Name, probe.Namespace, probe.Spec.Hostname).Dec()
	}()
	return cancel
}

// EnsureProbeWorker ensures a new worker per generation of the probe.
// New generation of probe - new worker.
// If the generation has not changed, it will re-create a worker. If context is done (we are deleting) that worker will die immediately.
func (m *ProbeManager) EnsureProbeWorker(ctx context.Context, k8sClient client.Client, probeCR *v1alpha1.DNSHealthCheckProbe, headers v1alpha1.AdditionalHeaders) {
	logger := log.FromContext(ctx).WithValues("health probe worker:", keyForProbe(probeCR))
	logger.Info("ensure probe")
	// if worker exists
	if stop, ok := m.probes[keyForProbe(probeCR)]; ok {
		// gen has not changed (spec has not changed) - nothing to do,
		// or first reconcile of the probe but worker already in place
		if probeCR.Status.ObservedGeneration == probeCR.Generation || probeCR.Status.ObservedGeneration == 0 {
			logger.V(2).Info("already exists for generation and status. Continuing", "probe", keyForProbe(probeCR))
			return
		}
		logger.V(2).Info("old worker exists. New generation of the probe found: stopping existing worker", "probe", keyForProbe(probeCR))
		stop()
	}
	// Either worker does not exist, or gen changed and old worker got killed. Creating a new one.
	logger.V(2).Info("health: starting fresh worker for", "generation", probeCR.Generation, "probe", keyForProbe(probeCR))
	probe := NewProbe(headers)
	m.probes[keyForProbe(probeCR)] = probe.Start(ctx, k8sClient, probeCR)
}

func keyForProbe(probe *v1alpha1.DNSHealthCheckProbe) string {
	return fmt.Sprintf("%s/%s", probe.Name, probe.Namespace)
}
