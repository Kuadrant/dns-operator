/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// MultClusterDNSRecordReconciler reconciles a DNSRecord object on a remote cluster
type MultClusterDNSRecordReconciler struct {
	DNSRecordReconciler
	mgr mcmanager.Manager
}

var _ reconcile.TypedReconciler[mcreconcile.Request] = &MultClusterDNSRecordReconciler{}

func (r *MultClusterDNSRecordReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	baseLogger := log.FromContext(ctx).WithValues("cluster", req.ClusterName)
	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger

	logger.Info("Reconciling Remote DNSRecord")

	cl, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, err
	}

	reconciler := DNSRecordReconciler{
		Client:          cl.GetClient(),
		Scheme:          r.Scheme,
		ProviderFactory: r.ProviderFactory,
	}

	return reconciler.Reconcile(ctx, req.Request)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultClusterDNSRecordReconciler) SetupWithManager(mgr mcmanager.Manager, maxRequeue, validForDuration, minRequeue time.Duration, healthProbesEnabled, allowInsecureHealthCert bool) error {
	defaultRequeueTime = maxRequeue
	validFor = validForDuration
	defaultValidationRequeue = minRequeue
	probesEnabled = healthProbesEnabled
	allowInsecureCert = allowInsecureHealthCert

	r.mgr = mgr

	return mcbuilder.ControllerManagedBy(mgr).
		Named("multicluster-dnsrecord-controller").
		For(&v1alpha1.DNSRecord{}).
		Complete(r)
}
