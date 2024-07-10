package azure

import (
	"context"

	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type AzureHealthCheckReconciler struct {
}

var _ provider.HealthCheckReconciler = &AzureHealthCheckReconciler{}

func NewAzureHealthCheckReconciler() *AzureHealthCheckReconciler {
	return &AzureHealthCheckReconciler{}
}

func (r *AzureHealthCheckReconciler) HealthCheckExists(_ context.Context, _ *v1alpha1.HealthCheckStatusProbe) (bool, error) {
	return true, nil
}

func (r *AzureHealthCheckReconciler) Reconcile(_ context.Context, _ provider.HealthCheckSpec, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe, _ string) provider.HealthCheckResult {
	return provider.HealthCheckResult{}
}

func (r *AzureHealthCheckReconciler) Delete(_ context.Context, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe) (provider.HealthCheckResult, error) {
	return provider.HealthCheckResult{}, nil
}
