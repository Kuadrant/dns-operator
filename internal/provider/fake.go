package provider

import (
	"context"

	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type FakeHealthCheckReconciler struct{}

func (*FakeHealthCheckReconciler) Reconcile(_ context.Context, _ HealthCheckSpec, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe, _ string) (HealthCheckResult, error) {
	return HealthCheckResult{HealthCheckCreated, "fakeID", "", "", ""}, nil
}

func (*FakeHealthCheckReconciler) Delete(_ context.Context, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe) (HealthCheckResult, error) {
	return HealthCheckResult{HealthCheckDeleted, "fakeID", "", "", ""}, nil
}

var _ HealthCheckReconciler = &FakeHealthCheckReconciler{}
