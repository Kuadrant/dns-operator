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
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// DNSRecordReconciler reconciles a DNSRecord object
type DNSRecordReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ProviderFactory provider.Factory
	DelegationRole  string
}

func postReconcile(ctx context.Context, name, ns string) {
	log.FromContext(ctx).Info(fmt.Sprintf("Reconciled DNSRecord %s from namespace %s in %s", name, ns, time.Since(reconcileStart.Time)))
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/finalizers,verbs=update

func (r *DNSRecordReconciler) IsPrimary() bool {
	return r.DelegationRole == DelegationRolePrimary
}

func (r *DNSRecordReconciler) IsSecondary() bool {
	return r.DelegationRole == DelegationRoleSecondary
}

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

	// Update the logger with appropriate record/zone metadata from the dnsRecord
	ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)

	if dnsRecord.IsDeleting() {
		logger.Info("Deleting DNSRecord")
		if dnsRecord.Status.ProviderEndpointsRemoved() {
			logger.V(1).Info("Status ProviderEndpointRemoved is true, finalizer can be removed")
			logger.Info("Removing Finalizer", "finalizer_name", DNSRecordFinalizer)
			controllerutil.RemoveFinalizer(dnsRecord, DNSRecordFinalizer)
			if err = r.Update(ctx, dnsRecord); client.IgnoreNotFound(err) != nil {
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

		if !dnsRecord.Status.ProviderEndpointsDeletion() {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsDeletion), "DNS records are being deleted from provider")
			result, err := r.updateStatus(ctx, previous, dnsRecord, probes, true, []string{}, err)
			return result, err
		}

		if dnsRecord.HasDNSZoneAssigned() {
			// Create a dns provider with config calculated for the current dns record status (Last successful)
			dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
			if err != nil {
				logger.Error(err, "Failed to load DNS Provider")
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
				return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
			}

			if probesEnabled {
				if err = r.DeleteHealthChecks(ctx, dnsRecord); client.IgnoreNotFound(err) != nil {
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

		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonProviderEndpointsRemoved), "DNS records removed from provider")
		dnsRecord.Status.ZoneEndpoints = nil
		dnsRecord.Status.ZoneDomainName = ""
		dnsRecord.Status.ZoneID = ""

		return r.updateStatusAndRequeue(ctx, previous, dnsRecord, time.Second)
	}

	if !controllerutil.ContainsFinalizer(dnsRecord, DNSRecordFinalizer) {
		logger.Info("Adding Finalizer", "finalizer_name", DNSRecordFinalizer)
		controllerutil.AddFinalizer(dnsRecord, DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	if probesEnabled {
		if err = r.ReconcileHealthChecks(ctx, dnsRecord, allowInsecureCert); err != nil {
			return ctrl.Result{}, err
		}
		// get all probes owned by this record
		if err := r.List(ctx, probes, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
			}),
			Namespace: dnsRecord.Namespace,
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	err = dnsRecord.Validate()
	if err != nil {
		logger.Error(err, "Failed to validate record")
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ValidationError", fmt.Sprintf("validation of DNSRecord failed: %v", err))
		return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
	}

	//Ensure an Owner ID has been assigned to the record (OwnerID set in the status)
	if !dnsRecord.HasOwnerIDAssigned() {
		if dnsRecord.Spec.OwnerID != "" {
			dnsRecord.Status.OwnerID = dnsRecord.Spec.OwnerID
		} else {
			dnsRecord.Status.OwnerID = dnsRecord.GetUIDHash()
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
		if !meta.IsStatusConditionPresentAndEqual(dnsRecord.Status.Conditions, string(v1alpha1.ConditionTypeReadyForDelegation), metav1.ConditionTrue) {
			meta.SetStatusCondition(&dnsRecord.Status.Conditions, metav1.Condition{Type: string(v1alpha1.ConditionTypeReadyForDelegation), Reason: string(v1alpha1.ConditionReasonFinalizersSet), Status: metav1.ConditionTrue})
			err := r.Status().Update(ctx, dnsRecord)
			if err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
		}

		if r.IsSecondary() {
			// Records that are delegating on secondary clusters should just set the ready status and return here
			// ToDo Should probably have a different condition reason and message
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")
			return r.updateStatusAndRequeue(ctx, previous, dnsRecord, 0)
		}
	}

	// Ensure we have provider secret
	if !dnsRecord.IsDelegating() && !dnsRecord.HasProviderSecretAssigned() {
		if dnsRecord.Spec.ProviderRef != nil && dnsRecord.Spec.ProviderRef.Name != "" {
			dnsRecord.Status.ProviderRef = *dnsRecord.Spec.ProviderRef
		} else {
			// try to find the default secret
			defaultSecretList := &v1.SecretList{}
			err = r.Client.List(ctx, defaultSecretList, &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					v1alpha1.DefaultProviderSecretLabel: "true",
				}),
				Namespace: dnsRecord.Namespace,
			})

			// failed to fetch
			if err != nil {
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", fmt.Sprintf("The default dns provider secret could not be loaded: %v", err))
				return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
			}

			// no secrets
			if len(defaultSecretList.Items) == 0 {
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", fmt.Sprintf("No default provider secret labeled %s was found", v1alpha1.DefaultProviderSecretLabel))
				return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, errors.New("no default secret found"))
			}

			// multiple defaults
			if len(defaultSecretList.Items) > 1 {
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
					"DNSProviderError", "Multiple default providers secrets found. Only one expected")
				return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, errors.New("multiple default provider secrets found"))
			}

			// set default secret as a provider secret to this record
			dnsRecord.Status.ProviderRef.Name = defaultSecretList.Items[0].Name
		}
	}

	// Ensure a DNS Zone has been assigned to the record (ZoneID and ZoneDomainName are set in the status)
	if !dnsRecord.HasDNSZoneAssigned() {
		logger.Info(fmt.Sprintf("provider zone not assigned for root host %s, finding suitable zone", dnsRecord.Spec.RootHost))

		// Create a dns provider with no config to list all potential zones available from the configured provider
		p, err := r.ProviderFactory.ProviderFor(ctx, dnsRecord, provider.Config{})
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
			return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
		}

		z, err := p.DNSZoneForHost(ctx, dnsRecord.Spec.RootHost)
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("Unable to find suitable zone in provider: %v", provider.SanitizeError(err)))
			return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
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
		return r.updateStatus(ctx, previous, dnsRecord, probes, false, []string{}, err)
	}

	// Ensure provider labels are added
	if !dnsRecord.IsDelegating() && common.MergeLabels(dnsRecord, dnsProvider.Labels()) {
		logger.Info("Adding provider labels")
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	// Publish the record
	hadChanges, notHealthyProbes, err := r.publishRecord(ctx, dnsRecord, probes, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to publish record")
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err)))
		return r.updateStatus(ctx, previous, dnsRecord, probes, hadChanges, notHealthyProbes, err)
	}

	if dnsRecord.IsAuthoritativeRecord() {
		metric, err := metrics.NewAuthoritativeRecordSpecInfoMetric(dnsRecord)
		if err != nil {
			logger.Error(err, "failed to create authoritativeRecordSpecInfo metric")
		} else {
			metric.Publish()
		}
	}
	return r.updateStatus(ctx, previous, dnsRecord, probes, hadChanges, notHealthyProbes, nil)
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSRecord.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *DNSRecordReconciler) setLogger(ctx context.Context, logger logr.Logger, dnsRecord *v1alpha1.DNSRecord) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("rootHost", dnsRecord.Spec.RootHost).
		WithValues("ownerID", dnsRecord.Status.OwnerID).
		WithValues("zoneID", dnsRecord.Status.ZoneID).
		WithValues("zoneDomainName", dnsRecord.Status.ZoneDomainName).
		WithValues("delegationRole", r.DelegationRole)
	return log.IntoContext(ctx, logger), logger
}

func (r *DNSRecordReconciler) updateStatus(ctx context.Context, previous, current *v1alpha1.DNSRecord, probes *v1alpha1.DNSHealthCheckProbeList, hadChanges bool, notHealthyProbes []string, specErr error) (reconcile.Result, error) {
	var requeueTime time.Duration
	logger := log.FromContext(ctx)

	// failure
	if specErr != nil {
		logger.Error(specErr, "Error reconciling DNS Record")
		var updateError error
		if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
			if updateError = r.Status().Update(ctx, current); updateError != nil && apierrors.IsConflict(updateError) {
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{Requeue: true}, updateError
	}

	// short loop. We don't publish anything so not changing status
	if prematurely, requeueIn := recordReceivedPrematurely(current, probes); prematurely {
		return reconcile.Result{RequeueAfter: requeueIn}, nil
	}

	// success
	if hadChanges {
		// generation has not changed but there are changes.
		// implies that they were overridden - bump write counter
		if !generationChanged(current) {
			current.Status.WriteCounter++
			metrics.WriteCounter.WithLabelValues(current.Name, current.Namespace).Inc()
			logger.V(1).Info("Changes needed on the same generation of record")
		}
		requeueTime = randomizedValidationRequeue
	} else {
		logger.Info("All records are already up to date")

		readyCond := meta.FindStatusCondition(current.Status.Conditions, string(v1alpha1.ConditionTypeReady))

		// this is the first reconciliation current.Status.ValidFor is not set
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
			requeueTime = exponentialRequeueTime(current.Status.ValidFor)

			// reset requeue time if we changed healthcheck spec but no updates were needed to the provider
			if generationChanged(current) {
				requeueTime = defaultValidationRequeue
			}
		}
	}

	setStatusConditions(current, hadChanges, notHealthyProbes)

	// valid for is always a requeue time
	current.Status.ValidFor = requeueTime.String()

	// reset the counter on the gen change regardless of having changes in the plan
	if generationChanged(current) {
		current.Status.WriteCounter = 0
		metrics.WriteCounter.WithLabelValues(current.Name, current.Namespace).Set(0)
		logger.V(1).Info("Resetting write counter on the generation change")
	}

	current.Status.ObservedGeneration = current.Generation
	current.Status.QueuedAt = reconcileStart

	return r.updateStatusAndRequeue(ctx, previous, current, requeueTime)
}

// updateStatusAndRequeue will update the status of the record if the current and previous status is different
// and returns a reconcile.result that re-queues at the given time.
func (r *DNSRecordReconciler) updateStatusAndRequeue(ctx context.Context, previous, current *v1alpha1.DNSRecord, requeueTime time.Duration) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	// update the record after setting the status
	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		logger.V(1).Info("Updating status of DNSRecord")
		if updateError := r.Status().Update(ctx, current); updateError != nil {
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

	return ctrl.Result{RequeueAfter: requeueTime}, nil
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

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) zone (dnsRecord.Status.ZoneID).
func (r *DNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)

	hadChanges, _, err := r.applyChanges(ctx, dnsRecord, nil, dnsProvider, true)
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
func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, probes *v1alpha1.DNSHealthCheckProbeList, dnsProvider provider.Provider) (bool, []string, error) {
	logger := log.FromContext(ctx)
	if prematurely, _ := recordReceivedPrematurely(dnsRecord, probes); prematurely {
		logger.V(1).Info("Skipping DNSRecord - is still valid")
		return false, []string{}, nil
	}

	hadChanges, notHealthyProbes, err := r.applyChanges(ctx, dnsRecord, probes, dnsProvider, false)
	if err != nil {
		return hadChanges, notHealthyProbes, err
	}
	logger.Info("Published DNSRecord to zone")

	return hadChanges, notHealthyProbes, nil
}

// recordReceivedPrematurely returns true if the current reconciliation loop started before
// the last loop plus validFor duration.
// It also returns a duration for which the record should have been requeued. Meaning that if the record was valid
// for 30 minutes and was received in 29 minutes, the function will return (true, 30 min).
// It will make an exception and will let through premature records if healthcheck probes change their health status
func recordReceivedPrematurely(record *v1alpha1.DNSRecord, probes *v1alpha1.DNSHealthCheckProbeList) (bool, time.Duration) {
	var prematurely bool

	requeueIn := validFor
	if record.Status.ValidFor != "" {
		requeueIn, _ = time.ParseDuration(record.Status.ValidFor)
	}
	expiryTime := metav1.NewTime(record.Status.QueuedAt.Add(requeueIn))
	prematurely = !generationChanged(record) && reconcileStart.Before(&expiryTime)

	// Check for the exception if we are received prematurely.
	// This cuts off all the cases when we are creating.
	// If this evaluates to true, we must have created probes and must have healthy condition
	if prematurely && probesEnabled && record.Spec.HealthCheck != nil {
		healthyCond := meta.FindStatusCondition(record.Status.Conditions, string(v1alpha1.ConditionTypeHealthy))
		// this is caused only by an error during reconciliation
		if healthyCond == nil {
			return false, requeueIn
		}
		// healthy is true only if we have probes and they are all healthy
		isHealthy := healthyCond.Status == metav1.ConditionTrue

		// if at least one is healthy - this will lock in true
		allProbesHealthy := false
		for _, probe := range probes.Items {
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
func setStatusConditions(record *v1alpha1.DNSRecord, hadChanges bool, notHealthyProbes []string) {
	// we get here only when spec err is nil - can trust hadChanges bool

	readyCond := meta.FindStatusCondition(record.Status.Conditions, string(v1alpha1.ConditionTypeReady))
	if readyCond != nil && (readyCond.Reason == string(v1alpha1.ConditionReasonProviderEndpointsRemoved) || readyCond.Reason == string(v1alpha1.ConditionReasonProviderEndpointsDeletion)) {
		// status already set to the expected value
		return
	}

	// give precedence to AwaitingValidation condition
	if hadChanges {
		setDNSRecordCondition(record, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonAwaitingValidation), "Awaiting validation")
		return
	}

	setDNSRecordCondition(record, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, string(v1alpha1.ConditionReasonProviderSuccess), "Provider ensured the dns record")

	// probes are disabled or not defined, or this is a wildcard record
	if record.Spec.HealthCheck == nil || strings.HasPrefix(record.Spec.RootHost, v1alpha1.WildcardPrefix) || !probesEnabled {
		meta.RemoveStatusCondition(&record.Status.Conditions, string(v1alpha1.ConditionTypeHealthy))
		return
	}

	// if we haven't published because of the health failure, we won't have changes but the spec endpoints will be empty
	if len(record.Status.Endpoints) == 0 {
		setDNSRecordCondition(record, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), "Not publishing unhealthy records")
	}

	// we don't have probes yet
	if cap(notHealthyProbes) == 0 {
		setDNSRecordCondition(record, string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), "Probes are creating")
		return
	}

	// we have healthy probes
	if len(notHealthyProbes) < cap(notHealthyProbes) {
		if len(notHealthyProbes) == 0 {
			// all probes are healthy
			setDNSRecordCondition(record, string(v1alpha1.ConditionTypeHealthy), metav1.ConditionTrue, string(v1alpha1.ConditionReasonHealthy), "All healthchecks succeeded")
		} else {
			// at least one of the probes is healthy
			setDNSRecordCondition(record, string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonPartiallyHealthy), fmt.Sprintf("Not healthy addresses: %s", notHealthyProbes))
		}
		return
	}
	// none of the probes is healthy
	setDNSRecordCondition(record, string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), fmt.Sprintf("Not healthy addresses: %s", notHealthyProbes))
}

// setDNSRecordCondition adds or updates a given condition in the DNSRecord status.
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

// getDNSProvider returns a Provider configured for the given DNSRecord
// If no zone/id/domain has been assigned to the given record, an error is thrown.
// If no owner has been assigned to the given record, an error is thrown.
// If the provider can't be initialised, an error is thrown.
func (r *DNSRecordReconciler) getDNSProvider(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (provider.Provider, error) {
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

// applyChanges creates the Plan and applies it to the registry. This is used only for external cloud provider DNS. Returns true only if the Plan had no errors and there were changes to apply.
// The error is nil only if the changes were successfully applied or there were no changes to be made.
func (r *DNSRecordReconciler) applyChanges(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, probes *v1alpha1.DNSHealthCheckProbeList, dnsProvider provider.Provider, isDelete bool) (bool, []string, error) {
	logger := log.FromContext(ctx)
	rootDomainName := dnsRecord.Spec.RootHost
	zoneDomainFilter := externaldnsendpoint.NewDomainFilter([]string{dnsRecord.Status.ZoneDomainName})
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	var excludeDNSRecordTypes []string

	registry, err := externaldnsregistry.NewTXTRegistry(ctx, dnsProvider, txtRegistryPrefix, txtRegistrySuffix,
		dnsRecord.Status.OwnerID, txtRegistryCacheInterval, txtRegistryWildcardReplacement, managedDNSRecordTypes,
		excludeDNSRecordTypes, txtRegistryEncryptEnabled, []byte(txtRegistryEncryptAESKey))
	if err != nil {
		return false, []string{}, err
	}

	policyID := "sync"
	policy, exists := externaldnsplan.Policies[policyID]
	if !exists {
		return false, []string{}, fmt.Errorf("unknown policy: %s", policyID)
	}

	//If we are deleting set the expected endpoints to an empty array
	if isDelete {
		dnsRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{}
	}

	//zoneEndpoints = Records in the current dns provider zone
	zoneEndpoints, err := registry.Records(ctx)
	if err != nil {
		return false, []string{}, err
	}

	//specEndpoints = Records that this DNSRecord expects to exist
	specEndpoints, err := registry.AdjustEndpoints(dnsRecord.Spec.Endpoints)
	if err != nil {
		return false, []string{}, fmt.Errorf("adjusting specEndpoints: %w", err)
	}

	// healthySpecEndpoints = Records that this DNSRecord expects to exist, that do not have matching unhealthy probes
	healthySpecEndpoints, notHealthyProbes, err := removeUnhealthyEndpoints(specEndpoints, dnsRecord, probes)
	if err != nil {
		return false, []string{}, fmt.Errorf("removing unhealthy specEndpoints: %w", err)
	}

	//statusEndpoints = Records that were created/updated by this DNSRecord last
	statusEndpoints, err := registry.AdjustEndpoints(dnsRecord.Status.Endpoints)
	if err != nil {
		return false, []string{}, fmt.Errorf("adjusting statusEndpoints: %w", err)
	}

	// add related endpoints to the record
	dnsRecord.Status.ZoneEndpoints = mergeZoneEndpoints(
		dnsRecord.Status.ZoneEndpoints,
		filterEndpoints(rootDomainName, zoneEndpoints))

	//Note: All endpoint lists should be in the same provider specific format at this point
	logger.V(1).Info("applyChanges", "zoneEndpoints", zoneEndpoints,
		"specEndpoints", healthySpecEndpoints, "statusEndpoints", statusEndpoints)

	plan := externaldnsplan.NewPlan(ctx, zoneEndpoints, statusEndpoints, healthySpecEndpoints, []externaldnsplan.Policy{policy},
		externaldnsendpoint.MatchAllDomainFilters{&zoneDomainFilter}, managedDNSRecordTypes, excludeDNSRecordTypes,
		registry.OwnerID(), &rootDomainName,
	)

	plan = plan.Calculate()
	if err = plan.Error(); err != nil {
		return false, notHealthyProbes, err
	}
	dnsRecord.Status.DomainOwners = plan.Owners
	dnsRecord.Status.Endpoints = healthySpecEndpoints
	if plan.Changes.HasChanges() {
		//ToDo (mnairn) CoreDNS will always think it has changes as long as provider.Records() returns an empty slice
		// Figure out a better way of doing this that avoids the check for a specific provider here
		hasChanges := dnsProvider.Name() != provider.DNSProviderCoreDNS
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		return hasChanges, notHealthyProbes, err
	}
	return false, notHealthyProbes, nil
}

// filterEndpoints takes a list of zoneEndpoints and removes from it all endpoints
// that do not belong to the rootDomainName (some.example.com does belong to the example.com domain).
// it is not using ownerID of this record as well as domainOwners from the status for filtering
func filterEndpoints(rootDomainName string, zoneEndpoints []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	// these are records that share domain but are not defined in the spec of DNSRecord
	var filteredEndpoints []*externaldnsendpoint.Endpoint

	// setup domain filter since we can't be sure that zone records are sharing domain with DNSRecord
	rootDomain, _ := strings.CutPrefix(rootDomainName, v1alpha1.WildcardPrefix)
	rootDomainFilter := externaldnsendpoint.NewDomainFilter([]string{rootDomain})

	// go through all EPs in the zone
	for _, zoneEndpoint := range zoneEndpoints {
		// if zoneEndpoint matches domain filter, it must be added to related EPs
		if rootDomainFilter.Match(zoneEndpoint.DNSName) {
			filteredEndpoints = append(filteredEndpoints, zoneEndpoint)
		}
	}
	return filteredEndpoints
}

// mergeZoneEndpoints merges existing endpoints with new and ensures there are no duplicates
func mergeZoneEndpoints(currentEndpoints, newEndpoints []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	// map to use as filter
	combinedMap := make(map[string]*externaldnsendpoint.Endpoint)
	// return struct
	var combinedEndpoints []*externaldnsendpoint.Endpoint

	// Use DNSName of EP as unique key. Ensures no duplicates
	for _, endpoint := range currentEndpoints {
		combinedMap[endpoint.DNSName] = endpoint
	}
	for _, endpoint := range newEndpoints {
		combinedMap[endpoint.DNSName] = endpoint
	}

	// Convert a map into an array
	for _, endpoint := range combinedMap {
		combinedEndpoints = append(combinedEndpoints, endpoint)
	}
	return combinedEndpoints
}
