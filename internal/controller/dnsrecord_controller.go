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
	"errors"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	DNSRecordFinalizer = "kuadrant.io/dns-record"

	DelegationRolePrimary   = "primary"
	DelegationRoleSecondary = "secondary"

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

	probesEnabled     bool
	allowInsecureCert bool
)

// DNSRecordReconciler reconciles a DNSRecord object on a local cluster.
type DNSRecordReconciler struct {
	BaseDNSRecordReconciler

	client.Client
}

var _ reconcile.TypedReconciler[reconcile.Request] = &DNSRecordReconciler{}

func postReconcile(ctx context.Context, name, ns string) {
	log.FromContext(ctx).Info(fmt.Sprintf("Reconciled DNSRecord %s from namespace %s in %s", name, ns, time.Since(reconcileStart.Time)))
}

// postReconcileMetrics emits a metric after the reconciliation.
// separate from the postReconcile as it should be deferred only if we have the record (vs called all the time)
func postReconcileMetrics(record *v1alpha1.DNSRecord, ready bool) {
	metric := metrics.NewRecordReadyMetric(record, ready)
	metric.Publish()
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/finalizers,verbs=update

func (r *DNSRecordReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Keep a reference to the initial logger(baseLogger) so we can update it throughout the reconcile
	baseLogger := log.FromContext(ctx).WithName("dnsrecord_controller")
	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger

	logger.Info("Reconciling DNSRecord")

	reconcileStart = metav1.Now()
	probes := &v1alpha1.DNSHealthCheckProbeList{}

	defer postReconcile(ctx, req.Name, req.Namespace)

	// randomize validation reconcile delay
	randomizedValidationRequeue = common.RandomizeValidationDuration(validationRequeueVariance, defaultValidationRequeue)

	rec := &v1alpha1.DNSRecord{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, rec)
	if err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	var previous, dnsRecord DNSRecordAccessor
	previous = &DNSRecord{
		DNSRecord: rec,
	}
	dnsRecord = &DNSRecord{
		DNSRecord: rec.DeepCopy(),
	}

	defer postReconcileMetrics(dnsRecord.GetDNSRecord(), meta.IsStatusConditionTrue(dnsRecord.GetStatus().Conditions, string(v1alpha1.ConditionTypeReady)))

	// Update the logger with appropriate record/zone metadata from the dnsRecord
	ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)

	if dnsRecord.IsDeleting() {
		logger.Info("Deleting DNSRecord")
		if dnsRecord.GetStatus().ProviderEndpointsRemoved() {
			logger.V(1).Info("Status ProviderEndpointRemoved is true, finalizer can be removed")
			logger.Info("Removing Finalizer", "finalizer_name", DNSRecordFinalizer)
			controllerutil.RemoveFinalizer(dnsRecord.GetDNSRecord(), DNSRecordFinalizer)
			if err = r.Update(ctx, dnsRecord.GetDNSRecord()); client.IgnoreNotFound(err) != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
			// If the status is set there is no clean up work required.
			// This stops a requeue loop if there are other finalizers add to the resource.
			// i.e. user generated finalizers.
			return ctrl.Result{}, nil
		}

		if !dnsRecord.GetStatus().ProviderEndpointsDeletion() {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsDeletion), "DNS records are being deleted from provider")
			result, err := r.updateStatus(ctx, previous, dnsRecord, true, err)
			return result, err
		}

		if dnsRecord.HasDNSZoneAssigned() {
			// Create a dns provider with config calculated for the current dns record status (Last successful)
			dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
			if err != nil {
				logger.Error(err, "Failed to load DNS Provider")
				dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
				return r.updateStatus(ctx, previous, dnsRecord, false, err)
			}

			if probesEnabled {
				if err = r.DeleteHealthChecks(ctx, dnsRecord.GetDNSRecord()); client.IgnoreNotFound(err) != nil {
					return ctrl.Result{}, err
				}
			}
			hadChanges, err := r.deleteRecord(ctx, dnsRecord, dnsProvider)
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
		} else {
			logger.Info("dns zone was never assigned, skipping zone cleanup")
		}

		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsRemoved), "DNS records removed from provider")
		dnsRecord.SetStatusZoneDomainName("")
		dnsRecord.SetStatusZoneID("")
		dnsRecord.SetStatusDomainOwners(nil)

		return r.updateStatusAndRequeue(ctx, r.Client, previous, dnsRecord, time.Second)
	}

	if !controllerutil.ContainsFinalizer(dnsRecord.GetDNSRecord(), DNSRecordFinalizer) {
		logger.Info("Adding Finalizer", "finalizer_name", DNSRecordFinalizer)
		controllerutil.AddFinalizer(dnsRecord.GetDNSRecord(), DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord.GetDNSRecord())
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	if probesEnabled {
		if err = r.ReconcileHealthChecks(ctx, dnsRecord.GetDNSRecord(), allowInsecureCert); err != nil {
			return ctrl.Result{}, err
		}
		// get all probes owned by this record
		if err := r.List(ctx, probes, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord.GetDNSRecord()),
			}),
			Namespace: dnsRecord.GetNamespace(),
		}); err != nil {
			return ctrl.Result{}, err
		}
		dnsRecord = newHealthCheckAdapter(dnsRecord, probes)
	}

	err = dnsRecord.GetDNSRecord().Validate()
	if err != nil {
		logger.Error(err, "Failed to validate record")
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonValidationError), fmt.Sprintf("validation of DNSRecord failed: %v", err))
		return r.updateStatus(ctx, previous, dnsRecord, false, err)
	}

	//Ensure an Owner ID has been assigned to the record (OwnerID set in the status)
	if !dnsRecord.HasOwnerIDAssigned() {
		if dnsRecord.GetSpec().OwnerID != "" {
			dnsRecord.SetStatusOwnerID(dnsRecord.GetSpec().OwnerID)
		} else {
			dnsRecord.SetStatusOwnerID(dnsRecord.GetDNSRecord().GetUIDHash())
		}
		//Update logger and context so it includes updated owner metadata
		ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)
	}

	if dnsRecord.IsDelegating() {
		// ReadyForDelegation can be set to true once:
		// - finalizer is added
		// - ownerID is set
		// - record is validated
		// - health probes created
		if !meta.IsStatusConditionPresentAndEqual(dnsRecord.GetStatus().Conditions, string(v1alpha1.ConditionTypeReadyForDelegation), metav1.ConditionTrue) {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReadyForDelegation), metav1.ConditionTrue, string(v1alpha1.ConditionReasonFinalizersSet), "")
			return r.updateStatusAndRequeue(ctx, r.Client, previous, dnsRecord, randomizedValidationRequeue)
		}

		if r.IsSecondary() {
			// Records that are delegating on secondary clusters should just set the ready status and return here
			// ToDo Should probably have a different condition reason and message
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")
			return r.updateStatusAndRequeue(ctx, r.Client, previous, dnsRecord, 0)
		}
	}

	// Ensure we have provider secret
	if !dnsRecord.IsDelegating() && !dnsRecord.HasProviderSecretAssigned() {
		if dnsRecord.GetSpec().ProviderRef != nil && dnsRecord.GetSpec().ProviderRef.Name != "" {
			dnsRecord.GetStatus().ProviderRef = *dnsRecord.GetSpec().ProviderRef
		} else {
			// try to find the default secret
			defaultSecretList := &v1.SecretList{}
			err = r.Client.List(ctx, defaultSecretList, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					v1alpha1.DefaultProviderSecretLabel: "true",
				}),
				Namespace: dnsRecord.GetNamespace(),
			})

			// failed to fetch
			if err != nil {
				dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", fmt.Sprintf("The default dns provider secret could not be loaded: %v", err))
				return r.updateStatus(ctx, previous, dnsRecord, false, err)
			}

			// no secrets
			if len(defaultSecretList.Items) == 0 {
				dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", fmt.Sprintf("No default provider secret labeled %s was found", v1alpha1.DefaultProviderSecretLabel))
				return r.updateStatus(ctx, previous, dnsRecord, false, errors.New("no default secret found"))
			}

			// multiple defaults
			if len(defaultSecretList.Items) > 1 {
				dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", "Multiple default providers secrets found. Only one expected")
				return r.updateStatus(ctx, previous, dnsRecord, false, errors.New("multiple default provider secrets found"))
			}

			// set default secret as a provider secret to this record
			dnsRecord.GetStatus().ProviderRef.Name = defaultSecretList.Items[0].Name
		}
	}

	// Ensure a DNS Zone has been assigned to the record (ZoneID and ZoneDomainName are set in the status)
	if !dnsRecord.HasDNSZoneAssigned() {
		logger.Info(fmt.Sprintf("provider zone not assigned for root host %s, finding suitable zone", dnsRecord.GetRootHost()))

		// Create a dns provider with no config to list all potential zones available from the configured provider
		p, err := r.ProviderFactory.ProviderFor(ctx, dnsRecord.GetDNSRecord(), provider.Config{})
		if err != nil {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
			return r.updateStatus(ctx, previous, dnsRecord, false, err)
		}

		z, err := p.DNSZoneForHost(ctx, dnsRecord.GetSpec().RootHost)
		if err != nil {
			dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("Unable to find suitable zone in provider: %v", provider.SanitizeError(err)))
			return r.updateStatus(ctx, previous, dnsRecord, false, err)
		}

		//Add zone id/domainName to status
		dnsRecord.SetStatusZoneID(z.ID)
		dnsRecord.SetStatusZoneDomainName(z.DNSName)

		//Update logger and context so it includes updated zone metadata
		ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)
	}

	// Create a dns provider for the current record, must have an owner and zone assigned or will throw an error
	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			string(v1alpha1.ConditionReasonProviderError), fmt.Sprintf("The dns provider could not be loaded: %v", err))
		return r.updateStatus(ctx, previous, dnsRecord, false, err)
	}

	// Ensure provider labels are added
	if !dnsRecord.IsDelegating() && common.MergeLabels(dnsRecord.GetDNSRecord(), dnsProvider.Labels()) {
		logger.Info("Adding provider labels")
		err = r.Update(ctx, dnsRecord.GetDNSRecord())
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	// Publish the record
	hadChanges, err := r.publishRecord(ctx, dnsRecord, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to publish record")
		dnsRecord.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err)))
		return r.updateStatus(ctx, previous, dnsRecord, hadChanges, err)
	}

	return r.updateStatus(ctx, previous, dnsRecord, hadChanges, nil)
}

func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)
	if prematurely, _ := recordReceivedPrematurely(dnsRecord); prematurely {
		logger.V(1).Info("Skipping DNSRecord - is still valid")
		return false, nil
	}
	return r.BaseDNSRecordReconciler.publishRecord(ctx, dnsRecord, dnsProvider)
}

func (r *DNSRecordReconciler) updateStatus(ctx context.Context, previous, current DNSRecordAccessor, hadChanges bool, specErr error) (reconcile.Result, error) {
	var requeueTime time.Duration
	logger := log.FromContext(ctx)

	// failure
	if specErr != nil {
		logger.Error(specErr, "Error reconciling DNS Record")
		var updateError error
		if !equality.Semantic.DeepEqual(previous.GetStatus(), current.GetStatus()) {
			if updateError = r.Status().Update(ctx, current.GetDNSRecord()); updateError != nil && apierrors.IsConflict(updateError) {
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{Requeue: true}, updateError
	}

	// short loop. We don't publish anything so not changing status
	if prematurely, requeueIn := recordReceivedPrematurely(current); prematurely {
		return reconcile.Result{RequeueAfter: requeueIn}, nil
	}

	// success
	if hadChanges {
		// generation has not changed but there are changes.
		// implies that they were overridden - bump write counter
		if !generationChanged(current.GetDNSRecord()) {
			current.GetStatus().WriteCounter++
			metrics.WriteCounter.WithLabelValues(current.GetDNSRecord().GetName(), current.GetDNSRecord().GetNamespace()).Inc()
			logger.V(1).Info("Changes needed on the same generation of record")
		}
		requeueTime = randomizedValidationRequeue
	} else {
		logger.Info("All records are already up to date")

		readyCond := meta.FindStatusCondition(current.GetStatus().Conditions, string(v1alpha1.ConditionTypeReady))

		// this is the first reconciliation current.GetStatus().ValidFor is not set
		if readyCond == nil {
			requeueTime = defaultValidationRequeue
		} else if readyCond.Status == metav1.ConditionFalse && readyCond.Reason == string(v1alpha1.ConditionReasonAwaitingValidation) {
			// no changes and we are awaiting validation - validation succeeded
			// reset to a fixed value from a randomized one
			requeueTime = exponentialRequeueTime(defaultValidationRequeue.String())
		} else {
			// ready or not publishing unhealthy endpoints,
			// we are giving precedence to AwaitingValidation
			// meaning we are doubling not randomized value
			requeueTime = exponentialRequeueTime(current.GetStatus().ValidFor)

			// reset requeue time if we changed healthcheck spec but no updates were needed to the provider
			if generationChanged(current.GetDNSRecord()) {
				requeueTime = defaultValidationRequeue
			}
		}
	}

	setStatusConditions(current, hadChanges)

	// valid for is always a requeue time
	current.GetStatus().ValidFor = requeueTime.String()

	// reset the counter on the gen change regardless of having changes in the plan
	if generationChanged(current.GetDNSRecord()) {
		current.GetStatus().WriteCounter = 0
		metrics.WriteCounter.WithLabelValues(current.GetDNSRecord().GetName(), current.GetNamespace()).Set(0)
		logger.V(1).Info("Resetting write counter on the generation change")
	}

	current.GetStatus().ObservedGeneration = current.GetDNSRecord().GetGeneration()
	current.GetStatus().QueuedAt = reconcileStart

	return r.updateStatusAndRequeue(ctx, r.Client, previous, current, requeueTime)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager, maxRequeue, validForDuration, minRequeue time.Duration, healthProbesEnabled, allowInsecureHealthCert bool) error {
	defaultRequeueTime = maxRequeue
	validFor = validForDuration
	defaultValidationRequeue = minRequeue
	probesEnabled = healthProbesEnabled
	allowInsecureCert = allowInsecureHealthCert

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSRecord{}).
		Watches(&v1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			logger := log.FromContext(ctx)
			s, ok := o.(*v1.Secret)
			if !ok {
				logger.V(1).Info("unexpected object type", "error", fmt.Sprintf("%T is not a *v1.Secret", o))
				return nil
			}
			if !strings.HasPrefix(string(s.Type), "kuadrant.io") {
				return nil
			}
			var toReconcile []reconcile.Request
			// list dns records in the secret namespace as they will be in the same namespace as the secret
			records := &v1alpha1.DNSRecordList{}
			if err := mgr.GetClient().List(ctx, records, &client.ListOptions{Namespace: o.GetNamespace()}); err != nil {
				logger.Error(err, "failed to list dnsrecords ", "namespace", o.GetNamespace())
				return toReconcile
			}

			isDefaultSecret := s.Labels[v1alpha1.DefaultProviderSecretLabel] == "true"

			for _, record := range records.Items {
				if record.Status.ProviderRef.Name == o.GetName() {
					logger.Info("secret updated", "secret", o.GetNamespace()+"/"+o.GetName(), "enqueuing dnsrecord ", record.GetName())
					toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&record)})
				}

				// if this is a default and we don't have secret in spec (we need a default) and we haven't assigned a secret yet
				// (note that the status ref is not a pointer so no need for a nil check) add a records to the queue.
				// the secret that just got updated should be assigned to such a record
				if isDefaultSecret && (record.Spec.ProviderRef == nil || record.Spec.ProviderRef.Name == "") && record.Status.ProviderRef.Name == "" {
					toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&record)})
				}
			}
			return toReconcile
		})).
		Watches(&v1alpha1.DNSHealthCheckProbe{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			logger := log.FromContext(ctx)
			probe, ok := o.(*v1alpha1.DNSHealthCheckProbe)
			if !ok {
				logger.V(1).Info("unexpected object type", "error", fmt.Sprintf("%T is not a *v1alpha1.DNSHealthCheckProbe", o))
				return []reconcile.Request{}
			}

			// haven't probed yet or deleting - nothing to do
			if probe.Status.Healthy == nil {
				return []reconcile.Request{}
			}

			record := &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{}}
			for _, ro := range probe.GetOwnerReferences() {
				if ro.Kind == "DNSRecord" {
					record.Name = ro.Name
					record.Namespace = probe.Namespace
					break
				}
			}

			if err := mgr.GetClient().Get(ctx, client.ObjectKeyFromObject(record), record); client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to get record")
				return []reconcile.Request{}
			}

			condition := meta.FindStatusCondition(record.Status.Conditions, string(v1alpha1.ConditionTypeHealthy))
			// no condition - record is not precessed yet
			if condition == nil {
				return []reconcile.Request{}
			}

			isHealthy := condition.Status == metav1.ConditionTrue

			// record and probe disagree on health - requeue
			if *probe.Status.Healthy != isHealthy {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(record)}}
			}
			// nothing to do
			return []reconcile.Request{}
		})).
		Complete(r)
}

// recordReceivedPrematurely returns true if the current reconciliation loop started before
// the last loop plus validFor duration.
// It also returns a duration for which the record should have been requeued. Meaning that if the record was valid
// for 30 minutes and was received in 29 minutes, the function will return (true, 30 min).
// It will make an exception and will let through premature records if healthcheck probes change their health status
func recordReceivedPrematurely(record DNSRecordAccessor) (bool, time.Duration) {
	var prematurely bool

	requeueIn := validFor
	if record.GetStatus().ValidFor != "" {
		requeueIn, _ = time.ParseDuration(record.GetStatus().ValidFor)
	}
	expiryTime := metav1.NewTime(record.GetStatus().QueuedAt.Add(requeueIn))
	prematurely = !generationChanged(record.GetDNSRecord()) && reconcileStart.Before(&expiryTime)

	hca, hasHealthChecks := record.(*healthCheckAdapter)

	// Check for the exception if we are received prematurely.
	// This cuts off all the cases when we are creating.
	// If this evaluates to true, we must have created probes and must have healthy condition
	if prematurely && hasHealthChecks && record.GetSpec().HealthCheck != nil {
		healthyCond := meta.FindStatusCondition(record.GetStatus().Conditions, string(v1alpha1.ConditionTypeHealthy))
		// this is caused only by an error during reconciliation
		if healthyCond == nil {
			return false, requeueIn
		}
		// healthy is true only if we have probes and they are all healthy
		isHealthy := healthyCond.Status == metav1.ConditionTrue

		// if at least one is healthy - this will lock in true
		allProbesHealthy := false
		for _, probe := range hca.probes.Items {
			if probe.Status.Healthy != nil {
				allProbesHealthy = allProbesHealthy || *probe.Status.Healthy
			}
		}
		// prematurely is true here. return false in case we need full reconcile
		return isHealthy == allProbesHealthy, requeueIn
	}

	return prematurely, requeueIn
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
	newRequeue := lastRequeue * 2
	if newRequeue > defaultRequeueTime {
		return defaultRequeueTime
	}
	return newRequeue
}

// setStatusConditions sets healthy and ready condition on given DNSRecord
func setStatusConditions(record DNSRecordAccessor, hadChanges bool) {
	// we get here only when spec err is nil - can trust hadChanges bool

	readyCond := meta.FindStatusCondition(record.GetStatus().Conditions, string(v1alpha1.ConditionTypeReady))
	if readyCond != nil && (readyCond.Reason == string(v1alpha1.ConditionReasonProviderEndpointsRemoved) || readyCond.Reason == string(v1alpha1.ConditionReasonProviderEndpointsDeletion)) {
		// status already set to the expected value
		return
	}

	// give precedence to AwaitingValidation condition
	if hadChanges {
		record.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonAwaitingValidation), "Awaiting validation")
		return
	}

	record.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")

	// probes are disabled or not defined, or this is a wildcard record
	if record.GetSpec().HealthCheck == nil || strings.HasPrefix(record.GetSpec().RootHost, v1alpha1.WildcardPrefix) || !probesEnabled {
		meta.RemoveStatusCondition(&record.GetStatus().Conditions, string(v1alpha1.ConditionTypeHealthy))
		return
	}

	// if we haven't published because of the health failure, we won't have changes but the spec endpoints will be empty
	if len(record.GetStatus().Endpoints) == 0 {
		record.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), "Not publishing unhealthy records")
	}

	record.SetStatusConditions(hadChanges)
}
