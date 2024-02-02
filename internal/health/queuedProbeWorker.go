package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/go-logr/logr"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

// QueuedProbeWorker funnels incoming health check requests from health probes,
// processing them one at a time and spacing them by a specified duration
type QueuedProbeWorker struct {
	Throttle time.Duration

	requests []HealthRequest
	logger   logr.Logger

	mux sync.Mutex
}

func NewRequestQueue(throttle time.Duration) *QueuedProbeWorker {
	return &QueuedProbeWorker{
		Throttle: throttle,
		requests: make([]HealthRequest, 0),
	}
}

type HealthRequest struct {
	Host, Path, Address      string
	Protocol                 v1alpha1.HealthProtocol
	Port                     int
	AdditionalHeaders        v1alpha1.AdditionalHeaders
	ExpectedResponses        []int
	AllowInsecureCertificate bool
	Notifier                 ProbeNotifier
}

func (q *QueuedProbeWorker) EnqueueCheck(req HealthRequest) {
	q.mux.Lock()
	defer q.mux.Unlock()

	q.logger.V(3).Info("enqueueing health check", "request", req)
	q.requests = append(q.requests, req)
}

// deqeue takes the next element of the queue and returns it. It blocks
// if the queue is empty, and returns false if the context is cancelled
func (q *QueuedProbeWorker) dequeue(ctx context.Context) (HealthRequest, bool) {
	reqChn := make(chan HealthRequest)

	go func() {
		for {
			select {
			case <-ctx.Done():
				close(reqChn)
				return
			default:
			}

			q.mux.Lock()
			if len(q.requests) == 0 {
				q.mux.Unlock()
				runtime.Gosched()
				continue
			}

			req := q.requests[0]
			q.requests = q.requests[1:]
			q.mux.Unlock()

			reqChn <- req
			return
		}

	}()

	req, ok := <-reqChn
	return req, ok
}

func (q *QueuedProbeWorker) Start(ctx context.Context) error {
	q.logger = log.FromContext(ctx)
	q.logger.V(3).Info("Starting health check queue")
	defer q.logger.Info("Stopping health check queue")

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				return ctx.Err()
			}
			return nil
		case <-time.After(q.Throttle):
			q.logger.V(3).Info("dequeing health check")
			req, ok := q.dequeue(ctx)
			if !ok {
				return nil
			}

			q.process(ctx, req)
		}
	}
}

func (q *QueuedProbeWorker) process(ctx context.Context, req HealthRequest) {
	go func() {
		result := q.performRequest(ctx, req)
		notificationResult, err := req.Notifier.Notify(ctx, result)
		if err != nil {
			q.logger.Error(err, "failed to notify health check result")
		}

		if notificationResult.Requeue {
			q.EnqueueCheck(req)
		}
	}()
}

func (q *QueuedProbeWorker) performRequest(ctx context.Context, req HealthRequest) ProbeResult {
	q.logger.V(3).Info("performing health check", "request", req)

	probeClient := &http.Client{
		Transport: TransportWithDNSResponse(map[string]string{req.Host: req.Address}),
	}

	if req.AllowInsecureCertificate {
		probeClient.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// Default port to 80
	port := 80
	if req.Port != 0 {
		port = req.Port
	}

	// Build the http request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://%s:%d%s", req.Protocol.ToScheme(), req.Host, port, req.Path), nil)
	if err != nil {
		return ProbeResult{CheckedAt: time.Now(), Healthy: false, Reason: err.Error()}
	}

	// add any user-defined additional headers
	for _, h := range req.AdditionalHeaders {
		httpReq.Header.Add(h.Name, h.Value)
	}

	// Send the request
	res, err := probeClient.Do(httpReq)
	if utilnet.IsConnectionReset(err) {
		res = &http.Response{StatusCode: 104}
	} else if err != nil {
		return ProbeResult{CheckedAt: time.Now(), Healthy: false, Reason: fmt.Sprintf("error: %s, response: %+v", err.Error(), res)}
	}

	// Create the result based on the response
	if req.ExpectedResponses == nil {
		req.ExpectedResponses = []int{200, 201}
	}
	healthy := true
	reason := ""

	if !checkResponse(res.StatusCode, req.ExpectedResponses) {
		healthy = false
		reason = fmt.Sprintf("Status code: %d", res.StatusCode)
	}

	return ProbeResult{
		CheckedAt: time.Now(),
		Healthy:   healthy,
		Status:    res.StatusCode,
		Reason:    reason,
	}
}

func checkResponse(response int, expected []int) bool {
	for _, i := range expected {
		if response == i {
			return true
		}
	}
	return false
}

// TransportWithDNSResponse creates a new transport which overrides hostnames.
func TransportWithDNSResponse(overrides map[string]string) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
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

	return transport
}
