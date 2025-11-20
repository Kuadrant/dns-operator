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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	"sigs.k8s.io/multicluster-runtime/pkg/controller"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

// RemoteDNSRecordReconciler reconciles a DNSRecord object on a remote cluster.
type RemoteDNSRecordReconciler struct {
	BaseDNSRecordReconciler

	clusterUID string
	mgr        mcmanager.Manager
	gvk        schema.GroupVersionKind
}

var _ reconcile.TypedReconciler[mcreconcile.Request] = &RemoteDNSRecordReconciler{}

func (r *RemoteDNSRecordReconciler) postReconcile(ctx context.Context, name, ns, cluster string) {
	log.FromContext(ctx).Info(fmt.Sprintf("Reconciled Remote DNSRecord %s from namespace %s on cluster %s", name, ns, cluster))
	remoteRecordsReconcileMetric := metrics.NewRemoteRecordReconcileMetric(name, ns, cluster)
	remoteRecordsReconcileMetric.Publish()
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

func (r *RemoteDNSRecordReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	clusterID, err := r.getClusterUID(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	baseLogger := log.FromContext(ctx).WithName("remote_dnsrecord_controller").WithValues(
		"cluster", req.ClusterName,
		"clusterID", clusterID,
		r.gvk.Kind, klog.KRef(req.Namespace, req.Name),
		"namespace", req.Namespace,
		"name", req.Name,
	)

	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger

	logger.Info("Remote Reconcile", "req", req.String())

	defer r.postReconcile(ctx, req.Name, req.Namespace, req.ClusterName)

	cl, err := r.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, err
	}

	remoteRecordsMetric := metrics.NewRemoteRecordsMetric(ctx, cl.GetClient(), logger, req.ClusterName)
	remoteRecordsMetric.Publish()

	rec := &v1alpha1.DNSRecord{}
	err = cl.GetClient().Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, rec)
	if err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	var previous, dnsRecord DNSRecordAccessor
	previous = &RemoteDNSRecord{
		DNSRecord: rec,
		ClusterID: clusterID,
	}
	dnsRecord = &RemoteDNSRecord{
		DNSRecord: rec.DeepCopy(),
		ClusterID: clusterID,
	}

	defer postReconcileMetrics(dnsRecord.GetDNSRecord(), meta.IsStatusConditionTrue(dnsRecord.GetStatus().Conditions, string(v1alpha1.ConditionTypeReady)))

	// Update the logger with appropriate record/zone metadata from the dnsRecord
	ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)

	//Only remote records that are delegating should be processed
	if !dnsRecord.IsDelegating() {
		logger.V(1).Info("skipping reconciliation of remote record that is not delegating")
		return ctrl.Result{}, nil
	}

	if dnsRecord.IsDeleting() {
		logger.Info("Deleting DNSRecord")

		if dnsRecord.GetStatus().ProviderEndpointsRemoved() {
			return ctrl.Result{}, nil
		}

		if !dnsRecord.GetStatus().ProviderEndpointsDeletion() {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsDeletion), "DNS records are being deleted from provider")
			return r.updateStatusAndRequeue(ctx, cl.GetClient(), previous, dnsRecord, time.Second)
		}

		if dnsRecord.HasDNSZoneAssigned() {
			// Create a dns provider with config calculated for the dnsRecord dns record status (Last successful)
			dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
			if err != nil {
				logger.Error(err, "Failed to load DNS Provider")
				dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
				return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
			}

			_, err = r.deleteRecord(ctx, dnsRecord, dnsProvider)
			if err != nil {
				logger.Error(err, "Failed to delete DNSRecord")
				return ctrl.Result{}, err
			}
		} else {
			logger.Info("dns zone was never assigned, skipping zone cleanup")
		}

		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsRemoved), "DNS records removed from provider")
		dnsRecord.SetStatusZoneDomainName("")
		dnsRecord.SetStatusZoneID("")
		dnsRecord.SetStatusDomainOwners(nil)

		return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, nil)
	}

	//Note: Can't use GetStatus() here since it's the remote embedded one returned and we need the higher level one for the ReadyForDelegation status
	if !dnsRecord.GetDNSRecord().Status.ReadyForDelegation() {
		logger.Info("remote record not ready for processing, skipping")
		return ctrl.Result{}, nil
	}

	// This needs to be called after the group is set/updated on the record (at least in memory)
	dnsRecord = newGroupAdapter(dnsRecord, r.getActiveGroups(ctx, r.mgr.GetLocalManager().GetClient(), dnsRecord))

	if !dnsRecord.IsActive() {
		_, err := r.updateStatus(ctx, r.mgr.GetLocalManager().GetClient(), previous, dnsRecord, nil)
		return reconcile.Result{RequeueAfter: InactiveGroupRequeueTime}, err
	}

	// Ensure a DNS Zone has been assigned to the record (ZoneID and ZoneDomainName are set in the status)
	if !dnsRecord.HasDNSZoneAssigned() {
		logger.Info(fmt.Sprintf("provider zone not assigned for root host %s, finding suitable zone", dnsRecord.GetRootHost()))

		// Create a dns provider with no config to list all potential zones available from the configured provider
		p, err := r.ProviderFactory.ProviderFor(ctx, dnsRecord.GetDNSRecord(), provider.Config{})
		if err != nil {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
			return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
		}

		z, err := p.DNSZoneForHost(ctx, dnsRecord.GetSpec().RootHost)
		if err != nil {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("Unable to find suitable zone in provider: %v", provider.SanitizeError(err)))
			return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
		}

		//Add zone id/domainName to status
		dnsRecord.SetStatusZoneID(z.ID)
		dnsRecord.SetStatusZoneDomainName(z.DNSName)

		//Update logger and context so it includes updated zone metadata
		ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)
	}

	// Create a dns provider for the dnsRecord record, must have an owner and zone assigned or will throw an error
	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
		return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
	}

	// Publish the record
	_, err = r.publishRecord(ctx, dnsRecord, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to publish record")
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err)))
		return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
	}

	dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")
	dnsRecord.SetStatusObservedGeneration(dnsRecord.GetDNSRecord().GetGeneration())

	err = r.unpublishInactiveGroups(ctx, r.mgr.GetLocalManager().GetClient(), dnsRecord, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to unpublish inactive groups")
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to unpublish inactive groups: %v", provider.SanitizeError(err)))

	}

	return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, nil)
}

func (r *RemoteDNSRecordReconciler) getClusterUID(ctx context.Context) (string, error) {
	if r.clusterUID != "" {
		return r.clusterUID, nil
	}

	ns := &corev1.Namespace{}
	err := r.mgr.GetLocalManager().GetClient().Get(ctx, client.ObjectKey{Name: "kube-system"}, ns)
	if err != nil {
		return "", err
	}
	r.clusterUID = string(ns.UID)
	return r.clusterUID, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteDNSRecordReconciler) SetupWithManager(mgr mcmanager.Manager, skipNameValidation bool) error {
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
		WithOptions(controller.Options{
			SkipNameValidation: &skipNameValidation,
		}).
		Complete(r)
}
