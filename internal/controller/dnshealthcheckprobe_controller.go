package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/health"
)

//ToDO This should not require sigs.k8s.io/gateway-api/apis/v1

const (
	DNSHealthCheckProbeFinalizer = "kuadrant.io/dns-health-check-probe"
)

var (
	ErrInvalidHeader = fmt.Errorf("invalid header format")
)

type DNSHealthCheckProbeReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	HealthMonitor *health.Monitor
	Queue         *health.QueuedProbeWorker
}

// +kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes/finalizers,verbs=get;update;patch

func (r *DNSHealthCheckProbeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	previous := &v1alpha1.DNSHealthCheckProbe{}
	err := r.Client.Get(ctx, req.NamespacedName, previous)
	if err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	}

	logger.V(3).Info("DNSHealthCheckProbeReconciler Reconcile", "dnsHealthCheckProbe", previous)

	probeObj := previous.DeepCopy()

	if probeObj.DeletionTimestamp != nil && !probeObj.DeletionTimestamp.IsZero() {
		logger.Info("deleting probe", "probe", probeObj)

		r.deleteProbe(probeObj)
		controllerutil.RemoveFinalizer(probeObj, DNSHealthCheckProbeFinalizer)

		if err := r.Update(ctx, probeObj); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(probeObj, DNSHealthCheckProbeFinalizer) {
		controllerutil.AddFinalizer(probeObj, DNSHealthCheckProbeFinalizer)
		if err := r.Update(ctx, probeObj); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set the interval
	interval := probeObj.Spec.Interval.Duration

	// Set the protocol: default to HTTP is not defined
	protocol := probeObj.Spec.Protocol
	if protocol == "" {
		protocol = v1alpha1.HttpProtocol
	}

	protocol = v1alpha1.NewHealthProtocol(string(probeObj.Spec.Protocol))

	probeId := probeId(probeObj)

	additionalHeaders, err := getAdditionalHeaders(ctx, r.Client, probeObj)

	if err != nil {
		f := false
		logger.V(1).Info(
			"error getting additional headers for probe",
			"secret name", probeObj.Spec.AdditionalHeadersRef.Name,
			"error", err)
		//update probe status
		probeObj.Status.Healthy = &f
		probeObj.Status.LastCheckedAt = metav1.Now()
		updateErr := r.Client.Status().Update(ctx, probeObj)
		if updateErr != nil {
			logger.V(1).Info("error updating probe status", "error", updateErr)
		}
		return ctrl.Result{}, err
	}

	if r.HealthMonitor.HasProbe(probeId) {
		r.HealthMonitor.UpdateProbe(probeId, func(p *health.ProbeQueuer) {
			p.Interval = interval
			p.Host = probeObj.Spec.Host
			p.IPAddress = probeObj.Spec.Address
			p.Path = probeObj.Spec.Path
			p.Port = probeObj.Spec.Port
			p.Protocol = protocol
			p.AdditionalHeaders = additionalHeaders
			p.ExpectedResponses = probeObj.Spec.ExpectedResponses
			p.AllowInsecureCertificate = probeObj.Spec.AllowInsecureCertificate
		})
	} else {
		notifier, err := r.newProbeNotifierFor(ctx, logger, previous)
		if err != nil {
			return ctrl.Result{}, err
		}

		r.HealthMonitor.AddProbeQueuer(&health.ProbeQueuer{
			ID:                       probeId,
			Interval:                 interval,
			Host:                     probeObj.Spec.Host,
			Path:                     probeObj.Spec.Path,
			Port:                     probeObj.Spec.Port,
			Protocol:                 protocol,
			IPAddress:                probeObj.Spec.Address,
			AdditionalHeaders:        additionalHeaders,
			ExpectedResponses:        probeObj.Spec.ExpectedResponses,
			AllowInsecureCertificate: probeObj.Spec.AllowInsecureCertificate,
			Notifier:                 notifier,
			Queue:                    r.Queue,
		})
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the manager
func (r *DNSHealthCheckProbeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSHealthCheckProbe{}).
		Complete(r)
}

func (r *DNSHealthCheckProbeReconciler) deleteProbe(probeObj *v1alpha1.DNSHealthCheckProbe) {
	r.HealthMonitor.RemoveProbe(probeId(probeObj))
}

func probeId(probeObj *v1alpha1.DNSHealthCheckProbe) string {
	return fmt.Sprintf("%s/%s", probeObj.Namespace, probeObj.Name)
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
			probeObj.Status.Reason = fmt.Sprintf("additional headers secret '%v' not found", secretKey.Name)
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

func (r *DNSHealthCheckProbeReconciler) getGatewayFor(ctx context.Context, probe *v1alpha1.DNSHealthCheckProbe) (*gatewayapiv1.Gateway, bool, error) {
	if probe.Labels == nil {
		return nil, false, nil
	}

	name, nameOk := probe.Labels["kuadrant.io/gateway"]
	namespace, namespaceOk := probe.Labels["kuadrant.io/gateway-namespace"]

	if !nameOk || !namespaceOk {
		return nil, false, nil
	}

	objKey := client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client.Get(ctx, objKey, gw); err != nil {
		return nil, false, err
	}

	return gw, true, nil
}

func (r *DNSHealthCheckProbeReconciler) newProbeNotifierFor(ctx context.Context, logger logr.Logger, probe *v1alpha1.DNSHealthCheckProbe) (health.ProbeNotifier, error) {
	// Base notifier to update the probe CR
	notifier := health.NewStatusUpdateProbeNotifier(r.Client, probe)

	// Try to find the associated Gateway, if not fount, return the base
	// notifier
	gateway, ok, err := r.getGatewayFor(ctx, probe)
	if err != nil {
		return nil, err
	}
	if !ok {
		logger.V(3).Info("no gateway associated to probe. Creating status update notifier")
		return notifier, nil
	}

	// Try to find the associated DNSRecord, if not found, return the base
	// notifier
	dnsRecord, ok, err := getDNSRecord(ctx, r.Client, probe)
	if err != nil {
		return nil, err
	}
	if !ok {
		logger.V(3).Info("no DNSRecord associated to probe. Creating status update notifier")
		return notifier, nil
	}

	// Find the listener in the Gateway that matches the DNSRecord
	listener, ok := slice.Find(gateway.Spec.Listeners, func(listener gatewayapiv1.Listener) bool {
		dnsRecordName := fmt.Sprintf("%s-%s", gateway.Name, listener.Name)
		return dnsRecord.Name == dnsRecordName
	})
	if !ok {
		return notifier, nil
	}

	logger.V(3).Info("creating instrumented probe notifier for probe")

	// Wrap the base notifier with the instrumented one that updates metrics
	return health.NewInstrumentedProbeNotifier(
		gateway.Name, gateway.Namespace, string(listener.Name),
		notifier,
	), nil
}

func getDNSRecord(ctx context.Context, apiClient client.Client, obj metav1.Object) (*v1alpha1.DNSRecord, bool, error) {
	if obj.GetAnnotations() == nil {
		return nil, false, nil
	}

	name, nameOk := obj.GetAnnotations()["dnsrecord-name"]
	ns, nsOk := obj.GetAnnotations()["dnsrecord-namespace"]

	if !nameOk || !nsOk {
		return nil, false, nil
	}

	dnsRecord := &v1alpha1.DNSRecord{}
	if err := apiClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: ns,
	}, dnsRecord); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return dnsRecord, true, nil
}
