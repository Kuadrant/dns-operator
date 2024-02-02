package health

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	healthCheckAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mgc_dns_health_check_attempts_total",
			Help: "MGC DNS Health Check Probe total number of attempts",
		},
		[]string{"gateway_name", "gateway_namespace", "listener"},
	)

	healthCheckFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mgc_dns_health_check_failures_total",
			Help: "MGC DNS Health Check Probe total number of failures",
		},
		[]string{"gateway_name", "gateway_namespace", "listener"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		healthCheckAttempts,
		healthCheckFailures,
	)
}

// InstrumentedProbeNotifier wraps a notifier by incrementing the failure counter
// when the result is unhealthy
type InstrumentedProbeNotifier struct {
	gatewayName, gatewayNamespace, listener string
	notifier                                ProbeNotifier
}

func NewInstrumentedProbeNotifier(gatewayName, gatewayNamespace, listener string, notifier ProbeNotifier) *InstrumentedProbeNotifier {
	return &InstrumentedProbeNotifier{
		gatewayName:      gatewayName,
		gatewayNamespace: gatewayNamespace,
		listener:         listener,
		notifier:         notifier,
	}
}

func (n *InstrumentedProbeNotifier) Notify(ctx context.Context, result ProbeResult) (NotificationResult, error) {
	healthCheckAttempts.WithLabelValues(n.gatewayName, n.gatewayNamespace, n.listener).Inc()
	if !result.Healthy {
		healthCheckFailures.WithLabelValues(n.gatewayName, n.gatewayNamespace, n.listener).Inc()
	}

	return n.notifier.Notify(ctx, result)
}

var _ ProbeNotifier = &InstrumentedProbeNotifier{}
