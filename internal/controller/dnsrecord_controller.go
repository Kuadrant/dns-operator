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

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-dns-operator/internal/common/conditions"
	"github.com/kuadrant/kuadrant-dns-operator/internal/provider"
)

const (
	DNSRecordFinalizer = "kuadrant.io/dns-record"
)

var Clock clock.Clock = clock.RealClock{}

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
	_ = log.FromContext(ctx)

	previous := &v1alpha1.DNSRecord{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, previous)
	if err != nil {
		if err := client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	dnsRecord := previous.DeepCopy()

	log.Log.V(3).Info("DNSRecordReconciler Reconcile", "dnsRecord", dnsRecord)

	if dnsRecord.DeletionTimestamp != nil && !dnsRecord.DeletionTimestamp.IsZero() {
		if err := r.deleteRecord(ctx, dnsRecord); err != nil {
			log.Log.Error(err, "Failed to delete DNSRecord", "record", dnsRecord)
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(dnsRecord, DNSRecordFinalizer)

		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(dnsRecord, DNSRecordFinalizer) {
		controllerutil.AddFinalizer(dnsRecord, DNSRecordFinalizer)
		err = r.Update(ctx, dnsRecord)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	var reason, message string
	status := metav1.ConditionTrue
	reason = "ProviderSuccess"
	message = "Provider ensured the managed zone"

	// Publish the record
	err = r.publishRecord(ctx, dnsRecord)
	if err != nil {
		status = metav1.ConditionFalse
		reason = "ProviderError"
		message = fmt.Sprintf("The DNS provider failed to ensure the record: %v", provider.SanitizeError(err))
	} else {
		dnsRecord.Status.ObservedGeneration = dnsRecord.Generation
		dnsRecord.Status.Endpoints = dnsRecord.Spec.Endpoints
	}
	setDNSRecordCondition(dnsRecord, string(conditions.ConditionTypeReady), status, reason, message)

	if !equality.Semantic.DeepEqual(previous.Status, dnsRecord.Status) {
		updateErr := r.Status().Update(ctx, dnsRecord)
		if updateErr != nil {
			// Ignore conflicts, resource might just be outdated.
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, updateErr
		}
	}

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSRecord{}).
		Complete(r)
}

// deleteRecord deletes record(s) in the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) deleteRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) error {
	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		// If the Managed Zone isn't found, just continue
		return client.IgnoreNotFound(err)
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone)
	if err != nil {
		return err
	}

	err = dnsProvider.Delete(dnsRecord, managedZone)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			log.Log.Info("Record not found in managed zone, continuing", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
			return nil
		} else if strings.Contains(err.Error(), "no endpoints") {
			log.Log.Info("DNS record had no endpoint, continuing", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
			return nil
		}
		return err
	}
	log.Log.Info("Deleted DNSRecord in manage zone", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)

	return nil
}

// publishRecord publishes record(s) to the DNSPRovider(i.e. route53) configured by the ManagedZone assigned to this
// DNSRecord (dnsRecord.Status.ParentManagedZone).
func (r *DNSRecordReconciler) publishRecord(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) error {

	managedZone := &v1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.Spec.ManagedZoneRef.Name,
			Namespace: dnsRecord.Namespace,
		},
	}
	err := r.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone, &client.GetOptions{})
	if err != nil {
		return err
	}
	managedZoneReady := meta.IsStatusConditionTrue(managedZone.Status.Conditions, "Ready")

	if !managedZoneReady {
		return fmt.Errorf("the managed zone is not in a ready state : %s", managedZone.Name)
	}

	if dnsRecord.Generation == dnsRecord.Status.ObservedGeneration {
		log.Log.V(3).Info("Skipping managed zone to which the DNS dnsRecord is already published", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)
		return nil
	}
	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone)
	if err != nil {
		return err
	}

	err = dnsProvider.Ensure(dnsRecord, managedZone)
	if err != nil {
		return err
	}
	log.Log.Info("Published DNSRecord to manage zone", "dnsRecord", dnsRecord.Name, "managedZone", managedZone.Name)

	return nil
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
