package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	clientCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_client_requests_total",
			Help: "A counter for requests from the wrapped client.",
		},
		[]string{"client", "code", "method"},
	)
	clientLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_client_request_latency_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"client", "code", "method"},
	)
	clientInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_client_in_flight_requests",
			Help: "A gauge of in-flight requests for the wrapped client.",
		},
		[]string{"client"},
	)
)

// NewInstrumentedClient creates an instrumented client by wrapping the given client with prometheus http middleware.
// The given name is added as a label to the metrics to distinguish between different client types (aws, google, azure etc..)
func NewInstrumentedClient(name string, client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}

	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}

	latency := clientLatency.MustCurryWith(prometheus.Labels{"client": name})
	counter := clientCounter.MustCurryWith(prometheus.Labels{"client": name})
	inFlight := clientInFlight.With(prometheus.Labels{"client": name})

	roundTripper := promhttp.InstrumentRoundTripperInFlight(inFlight,
		promhttp.InstrumentRoundTripperCounter(counter,
			promhttp.InstrumentRoundTripperDuration(latency, client.Transport),
		),
	)

	client.Transport = roundTripper
	return client
}

func init() {
	metrics.Registry.MustRegister(clientCounter, clientLatency, clientInFlight)
}
