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

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/internal/metrics"
)

// RemoteClusterSecretReconciler reconciles a cluster secrets object related to remote clusters
type RemoteClusterSecretReconciler struct {
	Client       client.Client
	SecretConfig SecretConfig
}

func (r *RemoteClusterSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	baseLogger := log.FromContext(ctx)
	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger.WithName("remote_cluster_secret_controller")

	logger.Info("Cluster Secret Reconcile", "req", req.String())

	clusterMetric := metrics.NewActiveClustersMetric(ctx, r.Client, logger, r.SecretConfig.Namespace, r.SecretConfig.Label)
	clusterMetric.Publish()

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteClusterSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("remoteClusterSecret").
		For(&corev1.Secret{}).
		WithEventFilter(secretNamespaceFilter(r.SecretConfig.Namespace, r.SecretConfig.Label)).
		Complete(r)
}

func secretNamespaceFilter(ns, label string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == ns && obj.GetLabels()[label] == "true"
	})
}
