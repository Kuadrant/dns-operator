package probes

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/metrics"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type Worker struct {
	probe     *v1alpha1.DNSHealthCheckProbe
	client    client.Client
	Transport RoundTripperFunc
}

func NewWorker(k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) *Worker {
	w := &Worker{
		probe:  probe,
		client: k8sClient,
	}
	return w
}

func (w *Worker) Start(clientctx context.Context) context.CancelFunc {

	ctx, cancel := context.WithCancel(clientctx)
	logger := log.FromContext(ctx)
	logger.Info("health: starting new worker for", "probe ", keyForProbe(w.probe))

	go func() {
		for range w.ExecuteProbe(ctx, w.client, w.probe) {
			logger.Info("health: probe finished ", "patching status for probe", keyForProbe(w.probe), "status", w.probe.Status)
			// each time this is done it will send a signal
			//patchFrom := client.MergeFrom(w.probe.DeepCopy())
			err := w.client.Status().Update(clientctx, w.probe)
			//err := w.client.Status().Patch(clientctx, w.probe, patchFrom, &client.SubResourcePatchOptions{})
			if err != nil {
				logger.Error(err, "health: probe finished. error patching probe status", "probe", keyForProbe(w.probe))
			}
		}
		logger.Info("health: stopped executing probe", "probe", keyForProbe(w.probe))
	}()
	return cancel
}

var (
	ErrInvalidHeader  = fmt.Errorf("invalid header format")
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

func (w *Worker) ExecuteProbe(ctx context.Context, k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) <-chan struct{} {

	sig := make(chan struct{})
	go func() {
		logger := log.FromContext(ctx)

		for {
			timer := time.NewTimer(executeAt(probe))
			select {
			case <-ctx.Done():
				logger.Info("health: context shutdown. Stopping", "probe", keyForProbe(probe))
				if sig != nil {
					timer.Stop()
					close(sig)
					sig = nil
					logger.Info("health: context shutdown. time stopped and channel closed", "probe", keyForProbe(probe))
				}
				return
			case <-timer.C:
				result := w.execute(ctx, k8sClient, probe)
				logger.Info("health: executed ", "probe", keyForProbe(probe), "result", result)
				probe.Status.ObservedGeneration = probe.Generation
				if !result.Healthy {
					probe.Status.ConsecutiveFailures += 1
				} else {
					probe.Status.ConsecutiveFailures = 0
				}
				probe.Status.Healthy = &result.Healthy
				probe.Status.LastCheckedAt = result.CheckedAt
				probe.Status.Reason = result.Reason
				logger.Info("health: execution complete ", "probe", keyForProbe(probe), "result", result)
				sig <- struct{}{}
			}
		}
	}()
	return sig
}

func executeAt(probe *v1alpha1.DNSHealthCheckProbe) time.Duration {
	timeUntilProbe := time.Until(probe.Status.LastCheckedAt.Time.Add(probe.Spec.Interval.Duration))
	if timeUntilProbe <= 0 {
		return 0
	}
	return timeUntilProbe

}

func (w *Worker) execute(ctx context.Context, k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) ProbeResult {
	logger := log.FromContext(ctx)
	logger.V(3).Info("performing health check", "request", probe)
	ips := []net.IP{}
	//if address is a CNAME, check all IP Addresses that it resolves to
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

	// get any user-defined additional headers
	headers, err := getAdditionalHeaders(ctx, k8sClient, probe)
	if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
	}

	for _, ip := range ips {
		result := w.performRequest(ctx, string(probe.Spec.Protocol), probe.Spec.Hostname, probe.Spec.Path, ip.String(), probe.Spec.Port, probe.Spec.AllowInsecureCertificate, headers)
		// if any IP in a CNAME fails, it is a failed CNAME
		if !result.Healthy {
			return result
		}
	}

	return ProbeResult{
		CheckedAt: metav1.Now(),
		Healthy:   true,
	}
}

func (w *Worker) performRequest(ctx context.Context, protocol, host, path, ip string, port int, allowInsecure bool, headers v1alpha1.AdditionalHeaders) ProbeResult {
	probeClient := metrics.NewInstrumentedClient("probe", &http.Client{
		Transport: TransportWithDNSResponse(map[string]string{host: ip}, allowInsecure),
	})
	if w.Transport != nil {
		probeClient.Transport = w.Transport
	}

	// Build the http request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://%s:%d%s", protocol, host, port, path), nil)
	if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
	}

	for _, h := range headers {
		httpReq.Header.Add(h.Name, h.Value)
	}

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

func getAdditionalHeaders(ctx context.Context, clt client.Client, probeObj *v1alpha1.DNSHealthCheckProbe) (v1alpha1.AdditionalHeaders, error) {
	additionalHeaders := v1alpha1.AdditionalHeaders{}

	if probeObj.Spec.AdditionalHeadersRef != nil {
		secretKey := client.ObjectKey{Name: probeObj.Spec.AdditionalHeadersRef.Name, Namespace: probeObj.Namespace}
		additionalHeadersSecret := &v1.Secret{}
		if err := clt.Get(ctx, secretKey, additionalHeadersSecret); client.IgnoreNotFound(err) != nil {
			return additionalHeaders, fmt.Errorf("error retrieving additional headers secret %v/%v: %w", secretKey.Namespace, secretKey.Name, err)
		} else if err != nil {
			probeError := fmt.Errorf("error retrieving additional headers secret %v/%v: %w", secretKey.Namespace, secretKey.Name, err)
			probeObj.Status.ConsecutiveFailures = 0
			probeObj.Status.Reason = "additional headers secret not found"
			return additionalHeaders, probeError
		}
		for k, v := range additionalHeadersSecret.Data {
			if strings.ContainsAny(strings.TrimSpace(k), " \t") {
				probeObj.Status.ConsecutiveFailures = 0
				probeObj.Status.Reason = "invalid header found: " + k
				return nil, fmt.Errorf("invalid header, must not contain whitespace '%v': %w", k, ErrInvalidHeader)
			}
			additionalHeaders = append(additionalHeaders, v1alpha1.AdditionalHeader{
				Name:  strings.TrimSpace(k),
				Value: string(v),
			})
		}
	}
	return additionalHeaders, nil
}

type WorkerManager struct {
	workers map[string]context.CancelFunc
}

func NewWorkerManager() *WorkerManager {
	return &WorkerManager{
		workers: map[string]context.CancelFunc{},
	}
}

func (m *WorkerManager) StopProbeWorker(ctx context.Context, probeCR *v1alpha1.DNSHealthCheckProbe) {
	logger := log.FromContext(ctx)
	if stop, ok := m.workers[keyForProbe(probeCR)]; ok {
		logger.V(1).Info("health: Stopping existing", "probe", keyForProbe(probeCR))
		stop()
		delete(m.workers, keyForProbe(probeCR))
	}
}

func (m *WorkerManager) EnsureProbeWorker(ctx context.Context, k8sClient client.Client, probeCR *v1alpha1.DNSHealthCheckProbe) {
	logger := log.FromContext(ctx)
	logger.Info("ensure probe")
	if probeCR.Status.ObservedGeneration == 0 {
		if _, ok := m.workers[keyForProbe(probeCR)]; ok {
			logger.V(1).Info("health: probe worker exists for generation. Continuing", "probe", keyForProbe(probeCR))
			return
		}
	}
	if probeCR.Generation == probeCR.Status.ObservedGeneration {
		//no spec change if we already have a worker stop
		logger.V(1).Info("helath: probe generation has not changed. Ensuring probe worker exists", "probe", keyForProbe(probeCR))
		if _, ok := m.workers[keyForProbe(probeCR)]; ok {
			logger.V(1).Info("health: probe worker exists for generation no need for a new one", "probe", keyForProbe(probeCR))
			return
		}
	}
	// new generation stop existing worker and restart with new CR generation
	if stop, ok := m.workers[keyForProbe(probeCR)]; ok {
		logger.V(1).Info("health: worker already exists. new generation of probe stopping existing", "probe", keyForProbe(probeCR))
		stop()
	}
	logger.V(1).Info("health: starting fresh worker for", "generation", probeCR.Generation, "probe", keyForProbe(probeCR))

	worker := NewWorker(k8sClient, probeCR)
	go func() {
		stop := worker.Start(ctx)
		m.workers[keyForProbe(probeCR)] = stop
	}()

}

func keyForProbe(probe *v1alpha1.DNSHealthCheckProbe) string {
	return fmt.Sprintf("%s/%s", probe.Name, probe.Namespace)
}
