package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
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

var (
	ErrInvalidHeader = fmt.Errorf("invalid header format")
)

// DNSProbeReconciler reconciles a DNSRecord object
type DNSProbeReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ProbeManager *probes.ProbeManager
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
			return ctrl.Result{}, err
		} else {
			// not found error stop any exsting worker will create a new one if a new probe is created
			logger.V(1).Info("probe not found cleaning up any executing health checks")
			previous.Name = req.Name
			previous.Namespace = req.Namespace
			r.ProbeManager.StopProbeWorker(ctx, previous)
			return ctrl.Result{}, nil
		}
	}

	dnsProbe := previous.DeepCopy()
	ctx, _ = r.setLoggerValues(ctx, baseLogger, dnsProbe)

	if dnsProbe.DeletionTimestamp != nil && !dnsProbe.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(dnsProbe, DNSHealthCheckFinalizer)
		if err = r.Update(ctx, dnsProbe); client.IgnoreNotFound(err) != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		logger.Info("healthcheckprobe deleted cleaning up workers")
		r.ProbeManager.StopProbeWorker(ctx, dnsProbe)
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(dnsProbe, DNSHealthCheckFinalizer) {
		controllerutil.AddFinalizer(dnsProbe, DNSHealthCheckFinalizer)
		if err := r.Client.Update(ctx, dnsProbe); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// get any user-defined additional headers
	headers, err := getAdditionalHeaders(ctx, r.Client, dnsProbe)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "attempted to load headers secret for probe but failed")
	}

	r.ProbeManager.EnsureProbeWorker(ctx, r.Client, dnsProbe, headers)

	return ctrl.Result{}, nil
}

func getAdditionalHeaders(ctx context.Context, clt client.Client, probeObj *v1alpha1.DNSHealthCheckProbe) (v1alpha1.AdditionalHeaders, error) {
	additionalHeaders := v1alpha1.AdditionalHeaders{}

	if probeObj.Spec.AdditionalHeadersRef != nil {
		secretKey := client.ObjectKey{Name: probeObj.Spec.AdditionalHeadersRef.Name, Namespace: probeObj.Namespace}
		additionalHeadersSecret := &v1.Secret{}
		if err := clt.Get(ctx, secretKey, additionalHeadersSecret); client.IgnoreNotFound(err) != nil {
			return additionalHeaders, fmt.Errorf("error retrieving additional headers secret %v/%v: %w", secretKey.Namespace, secretKey.Name, err)
		} else if err != nil {
			probeError := fmt.Errorf("error retrieving additional headers secret %v/%v: %w", secretKey.Namespace, secretKey.Name, err)
			probeObj.Status.ConsecutiveFailures = 0
			probeObj.Status.Reason = "additional headers secret not found"
			return additionalHeaders, probeError
		}
		for k, v := range additionalHeadersSecret.Data {
			if strings.ContainsAny(strings.TrimSpace(k), " \t") {
				probeObj.Status.ConsecutiveFailures = 0
				probeObj.Status.Reason = "invalid header found: " + k
				return nil, fmt.Errorf("invalid header, must not contain whitespace '%v': %w", k, ErrInvalidHeader)
			}
			additionalHeaders = append(additionalHeaders, v1alpha1.AdditionalHeader{
				Name:  strings.TrimSpace(k),
				Value: string(v),
			})
		}
	}
	return additionalHeaders, nil
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSHealthCheckProbe.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *DNSProbeReconciler) setLoggerValues(ctx context.Context, logger logr.Logger, dnsProbe *v1alpha1.DNSHealthCheckProbe) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("URL", dnsProbe.ToString()).
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
