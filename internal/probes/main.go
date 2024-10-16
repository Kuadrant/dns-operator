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

func ProcessProbeJob(ctx context.Context, k8sClient client.Client, originalProbe *v1alpha1.DNSHealthCheckProbe) bool {
	logger := log.FromContext(ctx)
	probe := &v1alpha1.DNSHealthCheckProbe{}
	timeUntilProbe := time.Second

	for {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(originalProbe), probe)
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "error reading probe")
			return true
		} else if err != nil {
			logger.Info("could not find probe CR, killing related worker")
			return false
		}

		timeUntilProbe = time.Until(probe.Status.LastCheckedAt.Time.Add(probe.Spec.Interval.Duration))
		if timeUntilProbe <= 0 {
			logger.V(1).Info("probe ready to execute", "timeUntilProbe", timeUntilProbe, "lastChecked", probe.Status.LastCheckedAt, "interval", probe.Spec.Interval, "now", time.Now())
			break
		}

		logger.V(1).Info("sleeping until probe is ready", "timeUntilProbe", timeUntilProbe, "lastChecked", probe.Status.LastCheckedAt, "interval", probe.Spec.Interval, "now", time.Now())
		time.Sleep(timeUntilProbe)
	}

	//if we slept for a while, refresh the probe from the api
	if timeUntilProbe > time.Second*3 {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(originalProbe), probe)
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "error reading probe")
			return true
		} else if err != nil {
			logger.Info("could not find probe CR, killing related worker")
			return false
		}
	}

	result := performProbe(ctx, k8sClient, probe)

	patchFrom := client.MergeFrom(probe.DeepCopy())
	if !result.Healthy {
		probe.Status.ConsecutiveFailures += 1
	} else {
		probe.Status.ConsecutiveFailures = 0
	}
	probe.Status.Healthy = &result.Healthy
	probe.Status.LastCheckedAt = result.CheckedAt
	probe.Status.Reason = result.Reason

	err := k8sClient.Status().Patch(ctx, probe, patchFrom, &client.SubResourcePatchOptions{})
	if err != nil {
		logger.Error(err, "error patching probe status")
	}
	return true
}

func performProbe(ctx context.Context, k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) ProbeResult {
	logger := log.FromContext(ctx)
	logger.V(3).Info("performing health check", "request", probe)

	//if address is a CNAME, check all IP Addresses that it resolves to
	ips, err := net.LookupHost(probe.Spec.Address)
	if err != nil {
		logger.V(1).Error(err, "error looking up host", "host", probe.Spec.Address)
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
	}

	// get any user-defined additional headers
	headers, err := getAdditionalHeaders(ctx, k8sClient, probe)
	if err != nil {
		return ProbeResult{CheckedAt: metav1.Now(), Healthy: false, Reason: err.Error()}
	}

	for _, ip := range ips {
		result := performRequest(ctx, string(probe.Spec.Protocol), probe.Spec.Hostname, probe.Spec.Path, ip, probe.Spec.Port, probe.Spec.AllowInsecureCertificate, headers)
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

func performRequest(ctx context.Context, protocol, host, path, ip string, port int, allowInsecure bool, headers v1alpha1.AdditionalHeaders) ProbeResult {
	probeClient := metrics.NewInstrumentedClient("probe", &http.Client{
		Transport: TransportWithDNSResponse(map[string]string{host: ip}, allowInsecure),
	})

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
