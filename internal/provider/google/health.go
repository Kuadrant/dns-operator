package google

import (
	"context"

	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type GCPHealthCheckReconciler struct {
}

var _ provider.HealthCheckReconciler = &GCPHealthCheckReconciler{}

func NewGCPHealthCheckReconciler() *GCPHealthCheckReconciler {
	return &GCPHealthCheckReconciler{}
}

func (r *GCPHealthCheckReconciler) Reconcile(_ context.Context, _ provider.HealthCheckSpec, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe, _ string) (provider.HealthCheckResult, error) {
	return provider.HealthCheckResult{}, nil
}

func (r *GCPHealthCheckReconciler) Delete(_ context.Context, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe) (provider.HealthCheckResult, error) {
	return provider.HealthCheckResult{}, nil
}
