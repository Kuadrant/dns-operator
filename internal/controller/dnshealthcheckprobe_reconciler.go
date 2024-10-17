package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/probes"
)

const (
	DNSHealthCheckFinalizer = "kuadrant.io/dns-health-check-probe"
)

// DNSProbeReconciler reconciles a DNSRecord object
type DNSProbeReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	WorkerManager *probes.WorkerManager
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes/finalizers,verbs=update

func (r *DNSProbeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	baseLogger := log.FromContext(ctx).WithName("dnsprobe_controller")
	ctx = log.IntoContext(ctx, baseLogger)
	logger := baseLogger

	logger.Info("Reconciling DNSHealthCheckProbe")

	previous := &v1alpha1.DNSHealthCheckProbe{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, previous)
	if err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		} else {
			// not found error stop worker will create a new one if a new probe is created
			previous.Name = req.Name
			previous.Namespace = req.Namespace
			r.WorkerManager.StopProbeWorker(ctx, previous)
			return ctrl.Result{}, err
		}
	}

	dnsProbe := previous.DeepCopy()
	ctx, _ = r.setLoggerValues(ctx, baseLogger, dnsProbe)

	if dnsProbe.DeletionTimestamp != nil && !dnsProbe.DeletionTimestamp.IsZero() {
		logger.Info("healthcheckprobe deleted cleaning up workers")
		r.WorkerManager.StopProbeWorker(ctx, dnsProbe)
		controllerutil.RemoveFinalizer(dnsProbe, DNSHealthCheckFinalizer)
		if err = r.Update(ctx, dnsProbe); client.IgnoreNotFound(err) != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(dnsProbe, DNSHealthCheckFinalizer) {
		controllerutil.AddFinalizer(dnsProbe, DNSHealthCheckFinalizer)
		if err := r.Client.Update(ctx, dnsProbe); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	r.WorkerManager.EnsureProbeWorker(ctx, r.Client, dnsProbe)

	return ctrl.Result{}, nil
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSHealthCheckProbe.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *DNSProbeReconciler) setLoggerValues(ctx context.Context, logger logr.Logger, dnsProbe *v1alpha1.DNSHealthCheckProbe) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("Address", dnsProbe.ToString()).
		WithValues("Allow Insecure Certs", dnsProbe.Spec.AllowInsecureCertificate).
		WithValues("Failure Threshold", dnsProbe.Spec.FailureThreshold)
	return log.IntoContext(ctx, logger), logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSProbeReconciler) SetupWithManager(mgr ctrl.Manager, maxRequeue, validForDuration, minRequeue time.Duration) error {
	defaultRequeueTime = maxRequeue
	validFor = validForDuration
	defaultValidationRequeue = minRequeue

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSHealthCheckProbe{}).
		Complete(r)
}
