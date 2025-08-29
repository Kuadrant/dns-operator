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
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// RemoteDNSRecordReconciler reconciles a DNSRecord object on a remote cluster
type RemoteDNSRecordReconciler struct {
	DNSRecordReconciler
	mgr mcmanager.Manager
	gvk schema.GroupVersionKind
}

var _ reconcile.TypedReconciler[mcreconcile.Request] = &RemoteDNSRecordReconciler{}

func (r *RemoteDNSRecordReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	baseLogger := log.FromContext(ctx).WithValues("cluster", req.ClusterName)
	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger.WithName("remote_dnsrecord_controller")

	logger.Info("Remote Reconcile", "req", req.String())

	cl, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, err
	}

	reconciler := DNSRecordReconciler{
		Client:          cl.GetClient(),
		LocalClient:     r.mgr.GetLocalManager().GetClient(),
		Scheme:          r.Scheme,
		ProviderFactory: r.ProviderFactory,
		remoteClient:    true,
		DelegationRole:  r.DelegationRole,
	}

	//For consistency in log statements, we need to build up the correct logger and pass it to Reconcile via the context
	logger = r.mgr.GetLocalManager().GetLogger().WithValues(
		"controller", strings.ToLower(r.gvk.Kind),
		"controllerGroup", r.gvk.Group,
		"controllerKind", r.gvk.Kind,
		r.gvk.Kind, klog.KRef(req.Namespace, req.Name),
		"namespace", req.Namespace,
		"name", req.Name)

	return reconciler.Reconcile(log.IntoContext(ctx, logger), req.Request)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteDNSRecordReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	var gvk schema.GroupVersionKind
	var err error
	gvk, err = apiutil.GVKForObject(&v1alpha1.DNSRecord{}, mgr.GetLocalManager().GetScheme())
	if err != nil {
		return err
	}
	r.mgr = mgr
	r.gvk = gvk

	return mcbuilder.ControllerManagedBy(mgr).
		Named("remotednsrecord").
		For(&v1alpha1.DNSRecord{}).
		Complete(r)
}
