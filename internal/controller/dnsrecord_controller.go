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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
	externaldnsplan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	DNSRecordFinalizer        = "kuadrant.io/dns-record"
	WriteCounterLimit         = 20
	validationRequeueVariance = 0.5
	DefaultValidationDuration = time.Second * 5
)

var (
	defaultRequeueTime    time.Duration
	validationRequeueTime time.Duration
	noRequeueDuration     = time.Duration(0)
	validFor              time.Duration
	reconcileStart                    = metav1.Time{}
	Clock                 clock.Clock = clock.RealClock{}
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
	logger := log.FromContext(ctx)

	reconcileStart = metav1.Now()

	// randomize validation reconcile delay
	validationRequeueTime = common.RandomizeDuration(validationRequeueVariance, DefaultValidationDuration)

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
		if err := r.ReconcileHealthChecks(ctx, dnsRecord); err != nil {
			return ctrl.Result{}, err
		}
		requeueTime, err := r.deleteRecord(ctx, dnsRecord)
		if err != nil {
			logger.Error(err, "Failed to delete DNSRecord")
			return ctrl.Result{RequeueAfter: requeueTime}, err
		}
		// if requeueTime returned  is the same as validationRequeueTime - the deleteRecord has successfully applied changes
		// in this case we need to queue for validation to ensure DNS Provider retained changes
		// before removing finalizer and deleting the DNS Record CR
		if requeueTime == validationRequeueTime {
			return ctrl.Result{RequeueAfter: requeueTime}, nil
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
		dnsRecord.Status.QueuedFor = metav1.NewTime(reconcileStart.Add(validationRequeueTime))
		logger.Info("Adding Finalizer", "name", DNSRecordFinalizer)
		controllerutil.AddFinalizer(dnsRecord, DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: validationRequeueTime}, nil
	}

	var reason, message string
	err = dnsRecord.Validate()
	if err != nil {
		reason = "ValidationError"
		message = fmt.Sprintf("validation of DNSRecord failed: %v", err)
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
		return r.updateStatus(ctx, previous, dnsRecord, noRequeueDuration)
	}

	// Publish the record
	requeueAfter, err := r.publishRecord(ctx, dnsRecord)
	if err != nil {
		reason = "ProviderError"
		message = fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err))
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
		return r.updateStatus(ctx, previous, dnsRecord, noRequeueDuration)
	}
	// success
	dnsRecord.Status.ObservedGeneration = dnsRecord.Generation
	dnsRecord.Status.Endpoints = dnsRecord.Spec.Endpoints

	if err := r.ReconcileHealthChecks(ctx, dnsRecord); err != nil {
		return ctrl.Result{}, err
	}

	return r.updateStatus(ctx, previous, dnsRecord, requeueAfter)
}

func (r *DNSRecordReconciler) updateStatus(ctx context.Context, previous, current *v1alpha1.DNSRecord, requeueAfter time.Duration) (reconcile.Result, error) {
	current.Status.QueuedFor = metav1.NewTime(reconcileStart.Add(requeueAfter))

	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		updateError := r.Status().Update(ctx, current)
		if apierrors.IsConflict(updateError) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, updateError
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager, requeueIn, valid time.Duration) error {
	defaultRequeueTime = requeueIn
	validFor = valid

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSRecord{}).
		Complete(r)
}

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (time.Duration, error) {
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
		return noRequeueDuration, client.IgnoreNotFound(err)
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return validationRequeueTime, fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	requeueTime, err := r.applyChanges(ctx, dnsRecord, managedZone, true)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			logger.Info("Record not found in managed zone, continuing", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
			return noRequeueDuration, nil
		} else if strings.Contains(err.Error(), "no endpoints") {
			logger.Info("DNS record had no endpoint, continuing", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
			return noRequeueDuration, nil
		}
		return noRequeueDuration, err
	}
	logger.Info("Deleted DNSRecord in manage zone", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)

	return requeueTime, nil
}

// publishRecord publishes record(s) to the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) (time.Duration, error) {
	logger := log.FromContext(ctx)
	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		return noRequeueDuration, err
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return noRequeueDuration, fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	// cut off here for the short reconcile loop
	requeueIn := validFor
	if dnsRecord.Status.ValidFor != "" {
		requeueIn, _ = time.ParseDuration(dnsRecord.Status.ValidFor)
	}
	expiryTime := metav1.NewTime(dnsRecord.Status.QueuedAt.Add(requeueIn))
	if !generationChanged(dnsRecord) && reconcileStart.Before(&expiryTime) {
		logger.V(3).Info("Skipping managed zone to which the DNS dnsRecord is already published and is still valid", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
		return requeueIn, nil
	}
	if generationChanged(dnsRecord) {
		dnsRecord.Status.WriteCounter = 0
		wrtiteCounter.WithLabelValues(dnsRecord.Name, dnsRecord.Namespace).Set(0)
	}

	requeueAfter, err := r.applyChanges(ctx, dnsRecord, managedZone, false)
	if err != nil {
		return noRequeueDuration, err
	}
	logger.Info("Published DNSRecord to manage zone", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)

	return requeueAfter, nil
}

func generationChanged(record *v1alpha1.DNSRecord) bool {
	return record.Generation != record.Status.ObservedGeneration
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

func (r *DNSRecordReconciler) applyChanges(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone, isDelete bool) (time.Duration, error) {
	logger := log.FromContext(ctx)
	filterDomain, _ := strings.CutPrefix(managedZone.Spec.DomainName, v1alpha1.WildcardPrefix)
	if dnsRecord.Spec.RootHost != nil {
		filterDomain = *dnsRecord.Spec.RootHost
	}
	rootDomainFilter := externaldnsendpoint.NewDomainFilter([]string{filterDomain})

	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		return noRequeueDuration, err
	}

	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	excludeDNSRecordTypes := []string{}

	registry, err := dnsRecord.GetRegistry(dnsProvider, managedDNSRecordTypes, excludeDNSRecordTypes)
	if err != nil {
		return noRequeueDuration, err
	}

	policyID := "sync"
	policy, exists := externaldnsplan.Policies[policyID]
	if !exists {
		return noRequeueDuration, fmt.Errorf("unknown policy: %s", policyID)
	}

	//If we are deleting set the expected endpoints to an empty array
	if isDelete {
		dnsRecord.Spec.Endpoints = []*externaldnsendpoint.Endpoint{}
	}

	//zoneEndpoints = Records in the current dns provider zone
	zoneEndpoints, err := registry.Records(ctx)
	if err != nil {
		return noRequeueDuration, err
	}

	//specEndpoints = Records that this DNSRecord expects to exist
	specEndpoints, err := registry.AdjustEndpoints(dnsRecord.Spec.Endpoints)
	if err != nil {
		return noRequeueDuration, fmt.Errorf("adjusting specEndpoints: %w", err)
	}

	//statusEndpoints = Records that were created/updated by this DNSRecord last
	statusEndpoints, err := registry.AdjustEndpoints(dnsRecord.Status.Endpoints)
	if err != nil {
		return noRequeueDuration, fmt.Errorf("adjusting statusEndpoints: %w", err)
	}

	//Note: All endpoint lists should be in the same provider specific format at this point
	logger.V(3).Info("applyChanges", "zoneEndpoints", zoneEndpoints)
	logger.V(3).Info("applyChanges", "specEndpoints", specEndpoints)
	logger.V(3).Info("applyChanges", "statusEndpoints", statusEndpoints)

	plan := &externaldnsplan.Plan{
		Policies: []externaldnsplan.Policy{policy},
		Current:  zoneEndpoints,
		Desired:  specEndpoints,
		//Note: We can't just filter domains by `managedZone.Spec.DomainName` it needs to be the exact root domain for this particular record
		DomainFilter:   externaldnsendpoint.MatchAllDomainFilters{&rootDomainFilter},
		ManagedRecords: managedDNSRecordTypes,
		ExcludeRecords: excludeDNSRecordTypes,
		OwnerID:        registry.OwnerID(),
	}

	plan = plan.Calculate()

	dnsRecord.Status.ValidFor = defaultRequeueTime.String()
	dnsRecord.Status.QueuedAt = reconcileStart
	if plan.Changes.HasChanges() {
		// generation has not changed but there are changes.
		// implies that they were overridden - bump write counter
		if !generationChanged(dnsRecord) {
			if dnsRecord.Status.WriteCounter < WriteCounterLimit {
				dnsRecord.Status.WriteCounter++
				wrtiteCounter.WithLabelValues(dnsRecord.Name, dnsRecord.Namespace).Inc()
				logger.V(3).Info("Changes needed on the same generation of record")
			} else {
				err = errors.New("reached write limit to the DNS provider for the same generation of record")
				logger.Error(err, "Giving up on trying to maintain desired state of the DNS record - changes are being overridden")
				return noRequeueDuration, err
			}

		}
		dnsRecord.Status.ValidFor = validationRequeueTime.String()
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "AwaitingValidation", "Awaiting validation")
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		if err != nil {
			return validationRequeueTime, err
		}
	} else {
		logger.Info("All records are already up to date")
		dnsRecord.Status.WriteCounter = 0
		wrtiteCounter.WithLabelValues(dnsRecord.Name, dnsRecord.Namespace).Set(0)
		setDNSRecordCondition(dnsRecord, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, "ProviderSuccess", "Provider ensured the dns record")
	}

	return defaultRequeueTime, nil
}
