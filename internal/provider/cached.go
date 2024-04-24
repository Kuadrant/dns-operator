package provider

import (
	"context"
	"reflect"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type CachedHealthCheckReconciler struct {
	reconciler HealthCheckReconciler
	provider   Provider

	syncCache *sync.Map
}

var _ HealthCheckReconciler = &CachedHealthCheckReconciler{}

func NewCachedHealthCheckReconciler(provider Provider, reconciler HealthCheckReconciler) *CachedHealthCheckReconciler {
	return &CachedHealthCheckReconciler{
		reconciler: reconciler,
		provider:   provider,
		syncCache:  &sync.Map{},
	}
}

func (r *CachedHealthCheckReconciler) HealthCheckExists(ctx context.Context, probeStatus *v1alpha1.HealthCheckStatusProbe) (bool, error) {
	return r.reconciler.HealthCheckExists(ctx, probeStatus)
}

// Delete implements HealthCheckReconciler
func (r *CachedHealthCheckReconciler) Delete(ctx context.Context, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe) (HealthCheckResult, error) {
	id, ok := r.getHealthCheckID(endpoint)
	if !ok {
		return NewHealthCheckResult(HealthCheckNoop, "", "", "", metav1.Condition{}), nil
	}

	defer r.syncCache.Delete(id)
	return r.reconciler.Delete(ctx, endpoint, probeStatus)
}

// Reconcile implements HealthCheckReconciler
func (r *CachedHealthCheckReconciler) Reconcile(ctx context.Context, spec HealthCheckSpec, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe, address string) HealthCheckResult {
	id, ok := r.getHealthCheckID(endpoint)
	if !ok {
		return r.reconciler.Reconcile(ctx, spec, endpoint, probeStatus, address)
	}

	// Update the cache with the new spec
	defer r.syncCache.Store(id, spec)

	// If the health heck is not cached, delegate the reconciliation
	existingSpec, ok := r.syncCache.Load(id)
	if !ok {
		return r.reconciler.Reconcile(ctx, spec, endpoint, probeStatus, address)
	}

	// If the spec is unchanged, return Noop
	if reflect.DeepEqual(spec, existingSpec.(HealthCheckSpec)) {
		return NewHealthCheckResult(HealthCheckNoop, id, "", "", metav1.Condition{Message: "spec unchanged"})
	}

	// Otherwise, delegate the reconciliation
	return r.reconciler.Reconcile(ctx, spec, endpoint, probeStatus, address)
}

func (r *CachedHealthCheckReconciler) getHealthCheckID(endpoint *externaldns.Endpoint) (string, bool) {
	return endpoint.GetProviderSpecificProperty(r.provider.ProviderSpecific().HealthCheckID)
}
