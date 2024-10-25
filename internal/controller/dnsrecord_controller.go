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

	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
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
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

type HealthCheckOption bool

const (
	DNSRecordFinalizer        = "kuadrant.io/dns-record"
	validationRequeueVariance = 0.5

	txtRegistryPrefix              = "kuadrant-"
	txtRegistrySuffix              = ""
	txtRegistryWildcardReplacement = "wildcard"
	txtRegistryEncryptEnabled      = false
	txtRegistryEncryptAESKey       = ""
	txtRegistryCacheInterval       = time.Duration(0)

	EnableHealthCheckProbes             HealthCheckOption = true
	DisableHealthCheckProbes            HealthCheckOption = false
	EnableHealthCheckInsecureEndpoints  HealthCheckOption = true
	DisableHealthCheckInsecureEndpoints HealthCheckOption = false
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
}

func postReconcile(ctx context.Context) {
	log.FromContext(ctx).Info(fmt.Sprintf("Reconciled DNSRecord in %s", time.Since(reconcileStart.Time)))
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

	defer postReconcile(ctx)

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

	// Update the logger with appropriate record/zone metadata from the dnsRecord
	ctx, logger = r.setLogger(ctx, baseLogger, dnsRecord)

	if dnsRecord.DeletionTimestamp != nil && !dnsRecord.DeletionTimestamp.IsZero() {
		logger.Info("Deleting DNSRecord")
		if dnsRecord.HasDNSZoneAssigned() {
			// Create a dns provider with config calculated for the current dns record status (Last successful)
			dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
			if err != nil {
				logger.Error(err, "Failed to load DNS Provider")
				reason := "DNSProviderError"
				message := fmt.Sprintf("The dns provider could not be loaded: %v", err)
				setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
				return r.updateStatus(ctx, previous, dnsRecord, false, err)
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

		logger.Info("Removing Finalizer", "finalizer_name", DNSRecordFinalizer)
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
		logger.Info("Adding Finalizer", "finalizer_name", DNSRecordFinalizer)
		controllerutil.AddFinalizer(dnsRecord, DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: randomizedValidationRequeue}, nil
	}

	err = dnsRecord.Validate()
	if err != nil {
		logger.Error(err, "Failed to validate record")
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ValidationError", fmt.Sprintf("validation of DNSRecord failed: %v", err))
		return r.updateStatus(ctx, previous, dnsRecord, false, err)
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

	// Ensure a DNS Zone has been assigned to the record (ZoneID and ZoneDomainName are set in the status)
	if !dnsRecord.HasDNSZoneAssigned() {
		logger.Info(fmt.Sprintf("provider zone not assigned for root host %s, finding suitable zone", dnsRecord.Spec.RootHost))

		// Create a dns provider with no config to list all potential zones available from the configured provider
		p, err := r.ProviderFactory.ProviderFor(ctx, dnsRecord, provider.Config{})
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("The dns provider could not be loaded: %v", err))
			return r.updateStatus(ctx, previous, dnsRecord, false, err)
		}

		z, err := p.DNSZoneForHost(ctx, dnsRecord.Spec.RootHost)
		if err != nil {
			setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
				"DNSProviderError", fmt.Sprintf("Unable to find suitable zone in provider: %v", provider.SanitizeError(err)))
			return r.updateStatus(ctx, previous, dnsRecord, false, err)
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
		return r.updateStatus(ctx, previous, dnsRecord, false, err)
	}

	// Publish the record
	hadChanges, err := r.publishRecord(ctx, dnsRecord, dnsProvider)
	if err != nil {
		logger.Error(err, "Failed to publish record")
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse,
			"ProviderError", fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err)))
		return r.updateStatus(ctx, previous, dnsRecord, hadChanges, err)
	}

	if probesEnabled {
		if err = r.ReconcileHealthChecks(ctx, dnsRecord, allowInsecureCert); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.updateStatus(ctx, previous, dnsRecord, hadChanges, nil)
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSRecord.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *DNSRecordReconciler) setLogger(ctx context.Context, logger logr.Logger, dnsRecord *v1alpha1.DNSRecord) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("rootHost", dnsRecord.Spec.RootHost).
		WithValues("ownerID", dnsRecord.Status.OwnerID).
		WithValues("zoneID", dnsRecord.Status.ZoneID).
		WithValues("zoneDomainName", dnsRecord.Status.ZoneDomainName)
	return log.IntoContext(ctx, logger), logger
}

func (r *DNSRecordReconciler) updateStatus(ctx context.Context, previous, current *v1alpha1.DNSRecord, hadChanges bool, specErr error) (reconcile.Result, error) {
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
	if prematurely, requeueIn := recordReceivedPrematurely(current); prematurely {
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
		metrics.WriteCounter.WithLabelValues(current.Name, current.Namespace).Set(0)
		logger.V(1).Info("Resetting write counter on the generation change")
	}

	current.Status.ObservedGeneration = current.Generation
	current.Status.Endpoints = current.Spec.Endpoints
	current.Status.QueuedAt = reconcileStart

	// update the record after setting the status
	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		logger.V(1).Info("Updating status of DNSRecord")
		if updateError := r.Status().Update(ctx, current); updateError != nil {
			if apierrors.IsConflict(updateError) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, updateError
		}
	}
	logger.V(1).Info(fmt.Sprintf("Requeue in %s", requeueTime.String()))
	return ctrl.Result{RequeueAfter: requeueTime}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager, maxRequeue, validForDuration, minRequeue time.Duration, healthProbesEnabled, allowInsecureHealthCert HealthCheckOption) error {
	defaultRequeueTime = maxRequeue
	validFor = validForDuration
	defaultValidationRequeue = minRequeue
	probesEnabled = bool(healthProbesEnabled)
	allowInsecureCert = bool(allowInsecureHealthCert)

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
			for _, record := range records.Items {
				if record.Spec.ProviderRef.Name == o.GetName() {
					logger.Info("secret updated", "secret", o.GetNamespace()+"/"+o.GetName(), "enqueuing dnsrecord ", record.GetName())
					toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&record)})
				}
			}
			return toReconcile
		})).
		Complete(r)
}

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) zone (dnsRecord.Status.ZoneID).
func (r *DNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider) (bool, error) {
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
func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)

	if prematurely, _ := recordReceivedPrematurely(dnsRecord); prematurely {
		logger.V(1).Info("Skipping DNSRecord - is still valid")
		return false, nil
	}

	hadChanges, err := r.applyChanges(ctx, dnsRecord, dnsProvider, false)
	if err != nil {
		return hadChanges, err
	}
	logger.Info("Published DNSRecord to zone")

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
	newRequeue := lastRequeue * 2
	if newRequeue > defaultRequeueTime {
		return defaultRequeueTime
	}
	return newRequeue
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
		DomainFilter:   externaldnsendpoint.NewDomainFilter([]string{dnsRecord.Status.ZoneDomainName}),
		ZoneTypeFilter: externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:   externaldnsprovider.NewZoneIDFilter([]string{dnsRecord.Status.ZoneID}),
	}
	return r.ProviderFactory.ProviderFor(ctx, dnsRecord, providerConfig)
}

// applyChanges creates the Plan and applies it to the registry. Returns true only if the Plan had no errors and there were changes to apply.
// The error is nil only if the changes were successfully applied or there were no changes to be made.
func (r *DNSRecordReconciler) applyChanges(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, dnsProvider provider.Provider, isDelete bool) (bool, error) {
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

	// add related endpoints to the record
	dnsRecord.Status.ZoneEndpoints = mergeZoneEndpoints(
		dnsRecord.Status.ZoneEndpoints,
		filterEndpoints(rootDomainName, zoneEndpoints))

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
	if plan.Changes.HasChanges() {
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		return true, err
	}
	return false, nil
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
