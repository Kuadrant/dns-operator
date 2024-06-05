/*
Copyright 2024.

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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	externaldnsplan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	DNSRecordFinalizer        = "kuadrant.io/dns-record"
	validationRequeueVariance = 0.5

	txtRegistryPrefix              = "kuadrant-"
	txtRegistrySuffix              = ""
	txtRegistryWildcardReplacement = "wildcard"
	txtRegistryEncryptEnabled      = false
	txtRegistryEncryptAESKey       = ""
	txtRegistryCacheInterval       = time.Duration(0)
)

var (
	defaultRequeueTime          time.Duration
	defaultValidationRequeue    time.Duration
	randomizedValidationRequeue time.Duration
	validFor                    time.Duration
	reconcileStart              metav1.Time
)

// DNSRecordReconciler reconciles a DNSRecord object
type DNSRecordReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ProviderFactory provider.Factory
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/finalizers,verbs=update

func (r *DNSRecordReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("dnsrecord_controller")
	ctx = log.IntoContext(ctx, logger)

	logger.V(1).Info("Reconciling DNSRecord")

	reconcileStart = metav1.Now()

	// randomize validation reconcile delay
	randomizedValidationRequeue = common.RandomizeDuration(validationRequeueVariance, defaultValidationRequeue)

	previous := &v1alpha1.DNSRecord{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, previous)
	if err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	dnsRecord := previous.DeepCopy()

	if dnsRecord.DeletionTimestamp != nil && !dnsRecord.DeletionTimestamp.IsZero() {
		if err = r.ReconcileHealthChecks(ctx, dnsRecord); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		hadChanges, err := r.deleteRecord(ctx, dnsRecord)
		if err != nil {
			logger.Error(err, "Failed to delete DNSRecord")
			return ctrl.Result{}, err
		}
		// if hadChanges - the deleteRecord has successfully applied changes
		// in this case we need to queue for validation to ensure DNS Provider retained changes
		// before removing finalizer and deleting the DNS Record CR
		if hadChanges {
			return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
		}

		logger.Info("Removing Finalizer", "name", DNSRecordFinalizer)
		controllerutil.RemoveFinalizer(dnsRecord, DNSRecordFinalizer)
		if err = r.Update(ctx, dnsRecord); client.IgnoreNotFound(err) != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(dnsRecord, DNSRecordFinalizer) {
		dnsRecord.Status.QueuedFor = metav1.NewTime(reconcileStart.Add(randomizedValidationRequeue))
		logger.Info("Adding Finalizer", "name", DNSRecordFinalizer)
		controllerutil.AddFinalizer(dnsRecord, DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	var reason, message string
	err = dnsRecord.Validate()
	if err != nil {
		reason = "ValidationError"
		message = fmt.Sprintf("validation of DNSRecord failed: %v", err)
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
		return r.updateStatus(ctx, previous, dnsRecord, false, err)
	}

	// Publish the record
	hadChanges, err := r.publishRecord(ctx, dnsRecord)
	if err != nil {
		reason = "ProviderError"
		message = fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err))
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
		return r.updateStatus(ctx, previous, dnsRecord, hadChanges, err)
	}

	if err = r.ReconcileHealthChecks(ctx, dnsRecord); err != nil {
		return ctrl.Result{}, err
	}

	return r.updateStatus(ctx, previous, dnsRecord, hadChanges, nil)
}

func (r *DNSRecordReconciler) updateStatus(ctx context.Context, previous, current *v1alpha1.DNSRecord, hadChanges bool, specErr error) (reconcile.Result, error) {
	var requeueTime time.Duration
	logger := log.FromContext(ctx)

	// short loop. We don't publish anything so not changing status
	if prematurely, requeueIn := recordReceivedPrematurely(current); prematurely {
		return reconcile.Result{RequeueAfter: requeueIn}, nil
	}

	// failure
	if specErr != nil {
		var updateError error
		if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
			if updateError = r.Status().Update(ctx, current); updateError != nil && apierrors.IsConflict(updateError) {
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{Requeue: true}, updateError
	}

	// success
	if hadChanges {
		// generation has not changed but there are changes.
		// implies that they were overridden - bump write counter
		if !generationChanged(current) {
			current.Status.WriteCounter++
			writeCounter.WithLabelValues(current.Name, current.Namespace).Inc()
			logger.V(1).Info("Changes needed on the same generation of record")
		}
		requeueTime = randomizedValidationRequeue
		setDNSRecordCondition(current, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "AwaitingValidation", "Awaiting validation")
	} else {
		logger.Info("All records are already up to date")
		// reset the valid for from randomized value to a fixed value once validation succeeds
		if !meta.IsStatusConditionTrue(current.Status.Conditions, string(v1alpha1.ConditionTypeReady)) {
			requeueTime = exponentialRequeueTime(defaultValidationRequeue.String())
		} else {
			// uses current.Status.ValidFor as the last requeue duration. Double it.
			requeueTime = exponentialRequeueTime(current.Status.ValidFor)
		}
		setDNSRecordCondition(current, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, "ProviderSuccess", "Provider ensured the dns record")
	}

	// valid for is always a requeue time
	current.Status.ValidFor = requeueTime.String()

	// reset the counter on the gen change regardless of having changes in the plan
	if generationChanged(current) {
		current.Status.WriteCounter = 0
		writeCounter.WithLabelValues(current.Name, current.Namespace).Set(0)
		logger.V(1).Info("Resetting write counter on the generation change")
	}

	current.Status.ObservedGeneration = current.Generation
	current.Status.Endpoints = current.Spec.Endpoints
	current.Status.QueuedAt = reconcileStart

	// update the record after setting the status
	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		if updateError := r.Status().Update(ctx, current); updateError != nil {
			if apierrors.IsConflict(updateError) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, updateError
		}
	}

	return ctrl.Result{RequeueAfter: requeueTime}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager, maxRequeue, validForDuration, minRequeue time.Duration) error {
	defaultRequeueTime = maxRequeue
	validFor = validForDuration
	defaultValidationRequeue = minRequeue

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSRecord{}).
		Watches(&v1alpha1.ManagedZone{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			logger := log.FromContext(ctx)
			toReconcile := []reconcile.Request{}
			// list dns records in the maanagedzone namespace as they will be in the same namespace as the zone
			records := &v1alpha1.DNSRecordList{}
			if err := mgr.GetClient().List(ctx, records, &client.ListOptions{Namespace: o.GetNamespace()}); err != nil {
				logger.Error(err, "failed to list dnsrecords ", "namespace", o.GetNamespace())
				return toReconcile
			}
			for _, record := range records.Items {
				if record.Spec.ManagedZoneRef.Name == o.GetName() {
					logger.Info("managed zone updated", "managedzone", o.GetNamespace()+"/"+o.GetName(), "enqueuing dnsrecord ", record.GetName())
					toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&record)})
				}
			}
			return toReconcile
		})).
		Complete(r)
}

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (bool, error) {
	logger := log.FromContext(ctx)

	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		// If the Managed Zone isn't found, just continue
		return false, client.IgnoreNotFound(err)
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return false, fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	hadChanges, err := r.applyChanges(ctx, dnsRecord, managedZone, true)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			logger.Info("Record not found in managed zone, continuing", "managedZone", managedZone.Name)
			return false, nil
		} else if strings.Contains(err.Error(), "no endpoints") {
			logger.Info("DNS record had no endpoint, continuing", "managedZone", managedZone.Name)
			return false, nil
		}
		return false, err
	}
	logger.Info("Deleted DNSRecord in manage zone", "managedZone", managedZone.Name)

	return hadChanges, nil
}

// publishRecord publishes record(s) to the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (bool, error) {
	logger := log.FromContext(ctx)
	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		return false, err
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return false, fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	if prematurely, _ := recordReceivedPrematurely(dnsRecord); prematurely {
		logger.V(1).Info("Skipping managed zone to which the DNS dnsRecord is already published and is still valid", "managedZone", managedZone.Name)
		return false, nil
	}

	hadChanges, err := r.applyChanges(ctx, dnsRecord, managedZone, false)
	if err != nil {
		return hadChanges, err
	}
	logger.Info("Published DNSRecord to manage zone", "managedZone", managedZone.Name)

	return hadChanges, nil
}

// recordReceivedPrematurely returns true if current reconciliation loop started before
// last loop plus validFor duration.
// It also returns a duration for which the record should have been requeued. Meaning that if the record was valid
// for 30 minutes and was received in 29 minutes the function will return (true, 30 min).
func recordReceivedPrematurely(record *v1alpha1.DNSRecord) (bool, time.Duration) {
	requeueIn := validFor
	if record.Status.ValidFor != "" {
		requeueIn, _ = time.ParseDuration(record.Status.ValidFor)
	}
	expiryTime := metav1.NewTime(record.Status.QueuedAt.Add(requeueIn))
	return !generationChanged(record) && reconcileStart.Before(&expiryTime), requeueIn
}

func generationChanged(record *v1alpha1.DNSRecord) bool {
	return record.Generation != record.Status.ObservedGeneration
}

// exponentialRequeueTime consumes the current time and doubles it until it reaches defaultRequeueTime
func exponentialRequeueTime(lastRequeueTime string) time.Duration {
	lastRequeue, err := time.ParseDuration(lastRequeueTime)
	// corrupted DNSRecord. This value naturally set only via time.Duration.String() call
	if err != nil {
		// default to the least confidence timeout
		return randomizedValidationRequeue
	}
	// double the duration. Return the max timeout if overshoot
	newReqeueue := lastRequeue * 2
	if newReqeueue > defaultRequeueTime {
		return defaultRequeueTime
	}
	return newReqeueue
}

// setDNSRecordCondition adds or updates a given condition in the DNSRecord status..
func setDNSRecordCondition(dnsRecord *v1alpha1.DNSRecord, conditionType string, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dnsRecord.Generation,
	}
	meta.SetStatusCondition(&dnsRecord.Status.Conditions, cond)
}

func (r *DNSRecordReconciler) getDNSProvider(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (provider.Provider, error) {
	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		return nil, err
	}

	providerConfig := provider.Config{
		DomainFilter:   externaldnsendpoint.NewDomainFilter([]string{managedZone.Spec.DomainName}),
		ZoneTypeFilter: externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:   externaldnsprovider.NewZoneIDFilter([]string{managedZone.Status.ID}),
	}

	return r.ProviderFactory.ProviderFor(ctx, managedZone, providerConfig)
}

// applyChanges creates the Plan and applies it to the registry. Returns true only if the Plan had no errors and there were changes to apply.
// The error is nil only if the changes were successfully applied or there were no changes to be made.
func (r *DNSRecordReconciler) applyChanges(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone, isDelete bool) (bool, error) {
	logger := log.FromContext(ctx)
	zoneDomainName, _ := strings.CutPrefix(managedZone.Spec.DomainName, v1alpha1.WildcardPrefix)
	rootDomainName, _ := strings.CutPrefix(dnsRecord.Spec.RootHost, v1alpha1.WildcardPrefix)
	zoneDomainFilter := externaldnsendpoint.NewDomainFilter([]string{zoneDomainName})
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	excludeDNSRecordTypes := []string{}

	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		return false, err
	}

	registry, err := externaldnsregistry.NewTXTRegistry(ctx, dnsProvider, txtRegistryPrefix, txtRegistrySuffix,
		dnsRecord.Spec.OwnerID, txtRegistryCacheInterval, txtRegistryWildcardReplacement, managedDNSRecordTypes,
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
	if plan.Changes.HasChanges() {
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		return true, err
	}
	return false, nil
}
