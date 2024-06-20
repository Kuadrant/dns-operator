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

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/metrics"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	ManagedZoneFinalizer = "kuadrant.io/managed-zone"
)

var (
	ErrProvider              = errors.New("ProviderError")
	ErrZoneValidation        = errors.New("ZoneValidationError")
	ErrProviderSecretMissing = errors.New("ProviderSecretMissing")
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
	logger := log.FromContext(ctx).WithName("managedzone_controller")
	ctx = log.IntoContext(ctx, logger)

	logger.V(1).Info("Reconciling ManagedZone")

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

	if managedZone.DeletionTimestamp != nil && !managedZone.DeletionTimestamp.IsZero() {
		if err := r.deleteParentZoneNSRecord(ctx, managedZone); err != nil {
			logger.Error(err, "Failed to delete parent Zone NS Record")
			return ctrl.Result{}, err
		}
		if err := r.deleteManagedZone(ctx, managedZone); err != nil {
			logger.Error(err, "Failed to delete ManagedZone")
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

	err = r.publishManagedZone(ctx, managedZone)
	if err != nil {
		reason := "UnknownError"
		message := fmt.Sprintf("unexpected error %v ", provider.SanitizeError(err))
		if errors.Is(err, ErrProvider) {
			reason = ErrProvider.Error()
			message = err.Error()
		}
		if errors.Is(err, ErrZoneValidation) {
			reason = ErrZoneValidation.Error()
			message = err.Error()
		}
		if errors.Is(err, ErrProviderSecretMissing) {
			metrics.SecretMissing.WithLabelValues(managedZone.Name, managedZone.Namespace, managedZone.Spec.SecretRef.Name).Set(1)
			reason = "DNSProviderSecretNotFound"
			message = fmt.Sprintf(
				"Could not find secret: %v/%v for managedzone: %v/%v",
				managedZone.Namespace, managedZone.Spec.SecretRef.Name,
				managedZone.Namespace, managedZone.Name)
		} else {
			metrics.SecretMissing.WithLabelValues(managedZone.Name, managedZone.Namespace, managedZone.Spec.SecretRef.Name).Set(0)
		}

		setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, reason, message)
		statusUpdateErr := r.Status().Update(ctx, managedZone)
		if statusUpdateErr != nil {
			return ctrl.Result{}, fmt.Errorf("zone error %v : status update failed error %v", err, statusUpdateErr)
		}
		return ctrl.Result{}, err
	}

	err = r.createParentZoneNSRecord(ctx, managedZone)
	if err != nil {
		message := fmt.Sprintf("Failed to create the NS record in the parent managed zone: %v", provider.SanitizeError(err))
		setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "ParentZoneNSRecordError", message)
		statusUpdateErr := r.Status().Update(ctx, managedZone)
		if statusUpdateErr != nil {
			// if we fail to update the status we want an immediate requeue to ensure the end user sees the error
			return ctrl.Result{}, fmt.Errorf("provider failed error %v : status update failed error %v", err, statusUpdateErr)
		}
		return ctrl.Result{}, err
	}

	// Check the parent zone NS record status
	err = r.parentZoneNSRecordReady(ctx, managedZone)
	if err != nil {
		message := fmt.Sprintf("NS Record ready status check failed: %v", provider.SanitizeError(err))
		setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, "ParentZoneNSRecordNotReady", message)
		statusUpdateErr := r.Status().Update(ctx, managedZone)
		if statusUpdateErr != nil {
			// if we fail to update the status we want an immediate requeue to ensure the end user sees the error
			return ctrl.Result{}, fmt.Errorf("provider failed error %v : status update failed error %v", err, statusUpdateErr)
		}
		return ctrl.Result{}, err
	}

	//We are all good set ready status true
	managedZone.Status.ObservedGeneration = managedZone.Generation
	setManagedZoneCondition(managedZone, string(v1alpha1.ConditionTypeReady), metav1.ConditionTrue, "ProviderSuccess", "Provider ensured the managed zone")
	err = r.Status().Update(ctx, managedZone)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("Reconciled ManagedZone")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedZoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ManagedZone{}).
		Owns(&v1alpha1.ManagedZone{}).
		Owns(&v1alpha1.DNSRecord{}).
		Watches(&v1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			logger := log.FromContext(ctx)
			var toReconcile []reconcile.Request

			zones := &v1alpha1.ManagedZoneList{}
			if err := mgr.GetClient().List(ctx, zones, &client.ListOptions{Namespace: o.GetNamespace()}); err != nil {
				logger.Error(err, "failed to list zones ", "namespace", o.GetNamespace())
				return toReconcile
			}
			for _, zone := range zones.Items {
				if zone.Spec.SecretRef.Name == o.GetName() {
					logger.Info("managed zone secret updated", "secret", o.GetNamespace()+"/"+o.GetName(), "enqueuing zone ", zone.GetName())
					toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&zone)})
				}
			}
			return toReconcile
		})).
		Complete(r)
}

func (r *ManagedZoneReconciler) publishManagedZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {

	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone, provider.Config{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("%w, the secret '%s/%s', referenced in the managedZone '%s/%s' does not exist",
				ErrProviderSecretMissing,
				managedZone.Namespace, managedZone.Spec.SecretRef.Name,
				managedZone.Namespace, managedZone.Name)
		}
		return fmt.Errorf("failed to get provider for the zone: %w", provider.SanitizeError(err))
	}

	mzResp, err := dnsProvider.EnsureManagedZone(ctx, managedZone)
	if err != nil {
		err = fmt.Errorf("%w, The DNS provider failed to ensure the managed zone: %v", ErrProvider, provider.SanitizeError(err))
	} else if managedZone.Spec.ID != "" && mzResp.DNSName != managedZone.Spec.DomainName {
		err = fmt.Errorf("%w, zone DNS name '%s' and managed zone domain name '%s' do not match for zone id '%s'", ErrZoneValidation, mzResp.DNSName, managedZone.Spec.DomainName, managedZone.Spec.ID)
	}

	if err != nil {
		managedZone.Status.ID = ""
		managedZone.Status.RecordCount = 0
		managedZone.Status.NameServers = nil
		return err
	}

	managedZone.Status.ID = mzResp.ID
	managedZone.Status.RecordCount = mzResp.RecordCount
	managedZone.Status.NameServers = mzResp.NameServers

	return nil
}

func (r *ManagedZoneReconciler) deleteManagedZone(ctx context.Context, managedZone *v1alpha1.ManagedZone) error {
	logger := log.FromContext(ctx)

	if managedZone.Spec.ID != "" {
		logger.Info("Skipping deletion of managed zone with provider ID specified in spec")
		return nil
	}

	dnsProvider, err := r.ProviderFactory.ProviderFor(ctx, managedZone, provider.Config{})
	if err != nil {
		return fmt.Errorf("failed to get DNS provider instance : %v", err)
	}
	err = dnsProvider.DeleteManagedZone(managedZone)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			logger.Info("ManagedZone was not found, continuing")
			return nil
		}
		return fmt.Errorf("%w, Failed to delete from provider. Provider Error: %v", ErrProvider, provider.SanitizeError(err))
	}
	logger.Info("Deleted ManagedZone")

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
