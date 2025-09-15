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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsplan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

// RemoteDNSRecordReconciler reconciles a DNSRecord object on a remote cluster.
type RemoteDNSRecordReconciler struct {
	Scheme          *runtime.Scheme
	ProviderFactory provider.Factory
	DelegationRole  string

	mgr mcmanager.Manager
	gvk schema.GroupVersionKind
}

var _ reconcile.TypedReconciler[mcreconcile.Request] = &RemoteDNSRecordReconciler{}

func (r *RemoteDNSRecordReconciler) postReconcile(ctx context.Context, name, ns, cluster string) {
	log.FromContext(ctx).Info(fmt.Sprintf("Reconciled Remote DNSRecord %s from namespace %s on cluster %s", name, ns, cluster))
	remoteRecordsReconcileMetric := metrics.NewRemoteRecordReconcileMetric(name, ns, cluster)
	remoteRecordsReconcileMetric.Publish()
}

func (r *RemoteDNSRecordReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	baseLogger := log.FromContext(ctx).WithName("remote_dnsrecord_controller").WithValues(
		"cluster", req.ClusterName,
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

	previous := &v1alpha1.DNSRecord{}
	err = cl.GetClient().Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, previous)
	if err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	dnsRecord := previous.DeepCopy()

	// Update the logger with appropriate record/zone metadata from the dnsRecord
	ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)

	//Only remote records that are delegating should be processed
	if !dnsRecord.IsDelegating() {
		logger.V(1).Info("skipping reconciliation of remote record that is not delegating")
		return ctrl.Result{}, nil
	}

	if dnsRecord.IsDeleting() {
		logger.Info("Deleting DNSRecord")

		if !dnsRecord.Status.ProviderEndpointsDeletion() {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsDeletion), "DNS records are being deleted from provider")
			return r.updateStatusAndRequeue(ctx, cl.GetClient(), previous, dnsRecord, time.Second)
		}

		if dnsRecord.HasDNSZoneAssigned() {
			// Create a dns provider with config calculated for the current dns record status (Last successful)
			dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
			if err != nil {
				logger.Error(err, "Failed to load DNS Provider")
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
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

		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsRemoved), "DNS records removed from provider")
		dnsRecord.Status.ZoneEndpoints = nil
		dnsRecord.Status.ZoneDomainName = ""
		dnsRecord.Status.ZoneID = ""

		return r.updateStatusAndRequeue(ctx, cl.GetClient(), previous, dnsRecord, time.Second)
	}

	if !dnsRecord.Status.ReadyForDelegation() {
		logger.Info("remote record not ready for processing, skipping")
		return ctrl.Result{}, nil
	}

	// Ensure a DNS Zone has been assigned to the record (ZoneID and ZoneDomainName are set in the status)
	if !dnsRecord.HasDNSZoneAssigned() {
		logger.Info(fmt.Sprintf("provider zone not assigned for root host %s, finding suitable zone", dnsRecord.Spec.RootHost))

		// Create a dns provider with no config to list all potential zones available from the configured provider
		p, err := r.ProviderFactory.ProviderFor(ctx, dnsRecord, provider.Config{})
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
			return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
		}

		z, err := p.DNSZoneForHost(ctx, dnsRecord.Spec.RootHost)
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("Unable to find suitable zone in provider: %v", provider.SanitizeError(err)))
			return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
		}

		//Add zone id/domainName to status
		dnsRecord.Status.ZoneID = z.ID
		dnsRecord.Status.ZoneDomainName = z.DNSName

		//Update logger and context so it includes updated zone metadata
		ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)
	}

	// Create a dns provider for the current record, must have an owner and zone assigned or will throw an error
	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
		return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
	}

	// Publish the record
	_, err = r.publishRecord(ctx, dnsRecord, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to publish record")
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err)))
		return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, err)
	}

	setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")
	return r.updateStatus(ctx, cl.GetClient(), previous, dnsRecord, nil)
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSRecord.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *RemoteDNSRecordReconciler) setLogger(ctx context.Context, logger logr.Logger, dnsRecord *v1alpha1.DNSRecord) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("rootHost", dnsRecord.Spec.RootHost).
		WithValues("ownerID", dnsRecord.Status.OwnerID).
		WithValues("zoneID", dnsRecord.Status.ZoneID).
		WithValues("zoneDomainName", dnsRecord.Status.ZoneDomainName).
		WithValues("delegationRole", r.DelegationRole)
	return log.IntoContext(ctx, logger), logger
}

// getDNSProvider returns a Provider configured for the given DNSRecord
// If no zone/id/domain has been assigned to the given record, an error is thrown.
// If no owner has been assigned to the given record, an error is thrown.
// If the provider can't be initialised, an error is thrown.
func (r *RemoteDNSRecordReconciler) getDNSProvider(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (provider.Provider, error) {
	var err error
	if !dnsRecord.HasOwnerIDAssigned() {
		err = errors.Join(fmt.Errorf("has no ownerID assigned"))
	}
	if !dnsRecord.HasDNSZoneAssigned() {
		err = errors.Join(fmt.Errorf("has no DNSZone assigned"))
	}
	if err != nil {
		return nil, err
	}
	providerConfig := provider.Config{
		HostDomainFilter: externaldnsendpoint.NewDomainFilter([]string{dnsRecord.Spec.RootHost}),
		DomainFilter:     externaldnsendpoint.NewDomainFilter([]string{dnsRecord.Status.ZoneDomainName}),
		ZoneTypeFilter:   externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:     externaldnsprovider.NewZoneIDFilter([]string{dnsRecord.Status.ZoneID}),
	}
	return r.ProviderFactory.ProviderFor(ctx, dnsRecord, providerConfig)
}

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) zone (dnsRecord.Status.ZoneID).
func (r *RemoteDNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)

	hadChanges, err := r.applyChanges(ctx, dnsRecord, dnsProvider, true)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			logger.Info("Record not found in zone, continuing")
			return false, nil
		} else if strings.Contains(err.Error(), "no endpoints") {
			logger.Info("DNS record had no endpoint, continuing")
			return false, nil
		}
		return false, err
	}
	logger.Info("Deleted DNSRecord in zone")

	return hadChanges, nil
}

// publishRecord publishes record(s) to the DNSPRovider(i.e. route53) zone (dnsRecord.Status.ZoneID).
// returns if it had changes, if record is healthy and an error. If had no changes - the healthy bool can be ignored
func (r *RemoteDNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)
	hadChanges, err := r.applyChanges(ctx, dnsRecord, dnsProvider, false)
	if err != nil {
		return hadChanges, err
	}
	logger.Info("Published DNSRecord to zone")

	return hadChanges, nil
}

// applyChanges creates the Plan and applies it to the registry. This is used only for external cloud provider DNS. Returns true only if the Plan had no errors and there were changes to apply.
// The error is nil only if the changes were successfully applied or there were no changes to be made.
func (r *RemoteDNSRecordReconciler) applyChanges(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider, isDelete bool) (bool, error) {
	logger := log.FromContext(ctx)
	rootDomainName := dnsRecord.Spec.RootHost
	zoneDomainFilter := externaldnsendpoint.NewDomainFilter([]string{dnsRecord.Status.ZoneDomainName})
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	var excludeDNSRecordTypes []string

	registry, err := externaldnsregistry.NewTXTRegistry(ctx, dnsProvider, txtRegistryPrefix, txtRegistrySuffix,
		dnsRecord.Status.OwnerID, txtRegistryCacheInterval, txtRegistryWildcardReplacement, managedDNSRecordTypes,
		excludeDNSRecordTypes, txtRegistryEncryptEnabled, []byte(txtRegistryEncryptAESKey))
	if err != nil {
		return false, err
	}

	policyID := "sync"
	policy, exists := externaldnsplan.Policies[policyID]
	if !exists {
		return false, fmt.Errorf("unknown policy: %s", policyID)
	}

	//If we are deleting set the expected endpoints to an empty array
	if isDelete {
		dnsRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{}
	}

	//zoneEndpoints = Records in the current dns provider zone
	zoneEndpoints, err := registry.Records(ctx)
	if err != nil {
		return false, err
	}

	//specEndpoints = Records that this DNSRecord expects to exist
	specEndpoints, err := registry.AdjustEndpoints(dnsRecord.Spec.Endpoints)
	if err != nil {
		return false, fmt.Errorf("adjusting specEndpoints: %w", err)
	}

	//statusEndpoints = Records that were created/updated by this DNSRecord last
	statusEndpoints, err := registry.AdjustEndpoints(dnsRecord.Status.Endpoints)
	if err != nil {
		return false, fmt.Errorf("adjusting statusEndpoints: %w", err)
	}

	//Note: All endpoint lists should be in the same provider specific format at this point
	logger.V(1).Info("applyChanges", "zoneEndpoints", zoneEndpoints,
		"specEndpoints", specEndpoints, "statusEndpoints", statusEndpoints)

	plan := externaldnsplan.NewPlan(ctx, zoneEndpoints, statusEndpoints, specEndpoints, []externaldnsplan.Policy{policy},
		externaldnsendpoint.MatchAllDomainFilters{&zoneDomainFilter}, managedDNSRecordTypes, excludeDNSRecordTypes,
		registry.OwnerID(), &rootDomainName,
	)

	plan = plan.Calculate()
	if err = plan.Error(); err != nil {
		return false, err
	}
	dnsRecord.Status.DomainOwners = plan.Owners
	dnsRecord.Status.Endpoints = specEndpoints
	if plan.Changes.HasChanges() {
		//ToDo (mnairn) CoreDNS will always think it has changes as long as provider.Records() returns an empty slice
		// Figure out a better way of doing this that avoids the check for a specific provider here
		hasChanges := dnsProvider.Name() != provider.DNSProviderCoreDNS
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		return hasChanges, err
	}
	return false, nil
}

func (r *RemoteDNSRecordReconciler) updateStatus(ctx context.Context, client client.Client, previous, current *v1alpha1.DNSRecord, err error) (reconcile.Result, error) {
	result, uErr := r.updateStatusAndRequeue(ctx, client, previous, current, 0)
	if uErr != nil {
		err = uErr
	}
	return result, err
}

// updateStatusAndRequeue will update the status of the record if the current and previous status is different
// and returns a reconcile.result that re-queues at the given time.
func (r *RemoteDNSRecordReconciler) updateStatusAndRequeue(ctx context.Context, client client.Client, previous, current *v1alpha1.DNSRecord, requeueTime time.Duration) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	// update the record after setting the status
	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		logger.V(1).Info("Updating status of DNSRecord")
		if updateError := client.Status().Update(ctx, current); updateError != nil {
			if apierrors.IsConflict(updateError) {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			return ctrl.Result{}, updateError
		}
	}
	logger.V(1).Info(fmt.Sprintf("Requeue in %s", requeueTime.String()))

	var gauge float64
	if meta.IsStatusConditionTrue(current.Status.Conditions, string(v1alpha1.ConditionTypeReady)) {
		gauge = 1
	}
	metrics.RecordReady.WithLabelValues(current.Name, current.Namespace, current.Spec.RootHost, strconv.FormatBool(current.IsDelegating())).Set(gauge)

	return ctrl.Result{}, nil
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

type SecretConfig struct {
	Namespace string
	Label     string
}
