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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	ManagedZoneFinalizer = "kuadrant.io/managed-zone"
)

// ManagedZoneReconciler reconciles a ManagedZone object
type ManagedZoneReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ProviderFactory provider.Factory
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=managedzones,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=managedzones/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=managedzones/finalizers,verbs=update

func (r *ManagedZoneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	previous := &v1alpha1.ManagedZone{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, previous)
	if err != nil {
		if err := client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	managedZone := previous.DeepCopy()

	log.Log.V(3).Info("ManagedZoneReconciler Reconcile", "managedZone", managedZone)

	if managedZone.DeletionTimestamp != nil && !managedZone.DeletionTimestamp.IsZero() {
		if err := r.deleteParentZoneNSRecord(ctx, managedZone); err != nil {
			log.Log.Error(err, "Failed to delete parent Zone NS Record", "managedZone", managedZone)
			return ctrl.Result{}, err
		}
		if err := r.deleteManagedZone(ctx, managedZone); err != nil {
			log.Log.Error(err, "Failed to delete ManagedZone", "managedZone", managedZone)
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(managedZone, ManagedZoneFinalizer)

		err = r.Update(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(managedZone, ManagedZoneFinalizer) {

		controllerutil.AddFinalizer(managedZone, ManagedZoneFinalizer)

		err = r.setParentZoneOwner(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.Update(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	var reason, message string
	status := metav1.ConditionTrue
	reason = "ProviderSuccess"
	message = "Provider ensured the managed zone"

	// Publish the managed zone
	err = r.publishManagedZone(ctx, managedZone)
	if err != nil {
		status = metav1.ConditionFalse
		reason = "ProviderError"
		message = fmt.Sprintf("The DNS provider failed to ensure the managed zone: %v", err)

		err = r.Status().Update(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create the parent zone NS record
	err = r.createParentZoneNSRecord(ctx, managedZone)
	if err != nil {
		status = metav1.ConditionFalse
		reason = "ParentZoneNSRecordError"
		message = fmt.Sprintf("Failed to create the NS record in the parent managed zone: %v", err)

		err = r.Status().Update(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check the parent zone NS record status
	err = r.parentZoneNSRecordReady(ctx, managedZone)
	if err != nil {
		status = metav1.ConditionFalse
		reason = "ParentZoneNSRecordNotReady"
		message = fmt.Sprintf("NS Record ready status check failed: %v", err)

		err = r.Status().Update(ctx, managedZone)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	managedZone.Status.ObservedGeneration = managedZone.Generation
	setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), status, reason, message)
	err = r.Status().Update(ctx, managedZone)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Log.Info("Reconciled ManagedZone", "managedZone", managedZone.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedZoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ManagedZone{}).
		Owns(&v1alpha1.ManagedZone{}).
		Complete(r)
}

func (r *ManagedZoneReconciler) publishManagedZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {

	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone, provider.Config{})
	if err != nil {
		return err
	}
	mzResp, err := dnsProvider.EnsureManagedZone(managedZone)
	if err != nil {
		return err
	}

	managedZone.Status.ID = mzResp.ID
	managedZone.Status.RecordCount = mzResp.RecordCount
	managedZone.Status.NameServers = mzResp.NameServers

	return nil
}

func (r *ManagedZoneReconciler) deleteManagedZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	if managedZone.Spec.ID != "" {
		log.Log.Info("Skipping deletion of managed zone with provider ID specified in spec", "managedZone", managedZone.Name)
		return nil
	}

	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone, provider.Config{})
	if err != nil {
		var reason, message string
		status := metav1.ConditionFalse
		reason = "ProviderError"
		message = fmt.Sprintf("The DNS provider creation failed: %v", err)
		managedZone.Status.ObservedGeneration = managedZone.Generation
		setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), status, reason, message)
		return err
	}
	err = dnsProvider.DeleteManagedZone(managedZone)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			log.Log.Info("ManagedZone was not found, continuing", "managedZone", managedZone.Name)
			return nil
		}
		return err
	}
	log.Log.Info("Deleted ManagedZone", "managedZone", managedZone.Name)

	return nil
}

func (r *ManagedZoneReconciler) getParentZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) (*v1alpha1.ManagedZone, error) {
	if managedZone.Spec.ParentManagedZone == nil {
		return nil, nil
	}
	parentZone := &v1alpha1.ManagedZone{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: managedZone.Namespace, Name: managedZone.Spec.ParentManagedZone.Name}, parentZone)
	if err != nil {
		return parentZone, err
	}
	return parentZone, nil
}

func (r *ManagedZoneReconciler) setParentZoneOwner(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	parentZone, err := r.getParentZone(ctx, managedZone)
	if err != nil {
		return err
	}
	if parentZone == nil {
		return nil
	}

	err = controllerutil.SetControllerReference(parentZone, managedZone, r.Scheme)
	if err != nil {
		return err
	}

	return err
}

func (r *ManagedZoneReconciler) createParentZoneNSRecord(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	parentZone, err := r.getParentZone(ctx, managedZone)
	if err != nil {
		return err
	}
	if parentZone == nil {
		return nil
	}

	recordName := managedZone.Spec.DomainName
	//Ensure NS record is created in parent managed zone if one is set
	recordTargets := make([]string, len(managedZone.Status.NameServers))
	for index := range managedZone.Status.NameServers {
		recordTargets[index] = *managedZone.Status.NameServers[index]
	}
	recordType := string(v1alpha1.NSRecordType)

	nsRecord := &v1alpha1.DNSRecord{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      recordName,
			Namespace: parentZone.Namespace,
		},
		Spec: v1alpha1.DNSRecordSpec{
			ManagedZoneRef: &v1alpha1.ManagedZoneReference{
				Name: parentZone.Name,
			},
			Endpoints: []*externaldns.Endpoint{
				{
					DNSName:    recordName,
					Targets:    recordTargets,
					RecordType: recordType,
					RecordTTL:  172800,
				},
			},
		},
	}
	err = controllerutil.SetControllerReference(parentZone, nsRecord, r.Scheme)
	if err != nil {
		return err
	}
	err = r.Client.Create(ctx, nsRecord, &client.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (r *ManagedZoneReconciler) deleteParentZoneNSRecord(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	parentZone, err := r.getParentZone(ctx, managedZone)
	if err := client.IgnoreNotFound(err); err != nil {
		return err
	}
	if parentZone == nil {
		return nil
	}

	recordName := managedZone.Spec.DomainName

	nsRecord := &v1alpha1.DNSRecord{}
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: parentZone.Namespace, Name: recordName}, nsRecord)
	if err != nil {
		if err := client.IgnoreNotFound(err); err == nil {
			return nil
		} else {
			return err
		}
	}

	err = r.Client.Delete(ctx, nsRecord, &client.DeleteOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (r *ManagedZoneReconciler) parentZoneNSRecordReady(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	parentZone, err := r.getParentZone(ctx, managedZone)
	if err := client.IgnoreNotFound(err); err != nil {
		return err
	}
	if parentZone == nil {
		return nil
	}

	recordName := managedZone.Spec.DomainName

	nsRecord := &v1alpha1.DNSRecord{}
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: parentZone.Namespace, Name: recordName}, nsRecord)
	if err != nil {
		if err := client.IgnoreNotFound(err); err == nil {
			return nil
		} else {
			return err
		}
	}

	nsRecordReady := meta.IsStatusConditionTrue(nsRecord.Status.Conditions, string(v1alpha1.ConditionTypeReady))
	if !nsRecordReady {
		return fmt.Errorf("the ns record is not in a ready state : %s", nsRecord.Name)
	}
	return nil
}

// setManagedZoneCondition adds or updates a given condition in the ManagedZone status.
func setManagedZoneCondition(managedZone *v1alpha1.ManagedZone, conditionType string, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: managedZone.Generation,
	}
	meta.SetStatusCondition(&managedZone.Status.Conditions, cond)
}
