package provider

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type FakeHealthCheckReconciler struct{}

func (*FakeHealthCheckReconciler) Reconcile(_ context.Context, _ HealthCheckSpec, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe, _ string) HealthCheckResult {
	return HealthCheckResult{HealthCheckCreated, "fakeID", "", "", metav1.Condition{}}
}

func (*FakeHealthCheckReconciler) Delete(_ context.Context, _ *externaldns.Endpoint, _ *v1alpha1.HealthCheckStatusProbe) (HealthCheckResult, error) {
	return HealthCheckResult{HealthCheckDeleted, "fakeID", "", "", metav1.Condition{}}, nil
}

var _ HealthCheckReconciler = &FakeHealthCheckReconciler{}
