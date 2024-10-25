package controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
)

func (r *DNSRecordReconciler) ReconcileHealthChecks(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, allowInsecureCerts bool) error {
	logger := log.FromContext(ctx).WithName("healthchecks")
	logger.Info("Reconciling healthchecks")

	// Probes enabled but no health check spec yet. Nothing to do
	if dnsRecord.Spec.HealthCheck == nil {
		return nil
	}

	desiredProbes := buildDesiredProbes(dnsRecord, common.GetLeafsTargets(common.MakeTreeFromDNSRecord(dnsRecord), ptr.To([]string{})), allowInsecureCerts)

	for _, probe := range desiredProbes {
		// if one of them fails - health checks for this record are invalid anyway, so no sense to continue
		if err := controllerruntime.SetControllerReference(dnsRecord, probe, r.Scheme); err != nil {
			return err
		}
		if err := r.ensureProbe(ctx, probe, logger); err != nil {
			return err
		}
	}
	logger.Info("Healthecks reconciled")
	return nil
}

// DeleteHealthChecks deletes all v1alpha1.DNSHealthCheckProbe that have ProbeOwnerLabel of passed in DNSRecord
func (r *DNSRecordReconciler) DeleteHealthChecks(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) error {
	logger := log.FromContext(ctx).WithName("healthchecks")
	logger.Info("Deleting healthchecks")

	healthProbes := v1alpha1.DNSHealthCheckProbeList{}

	if err := r.List(ctx, &healthProbes, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord),
		}),
		Namespace: dnsRecord.Namespace,
	}); err != nil {
		return err
	}

	var deleteErrors error
	for _, probe := range healthProbes.Items {
		logger.V(1).Info(fmt.Sprintf("Deleting probe: %s", probe.Name))
		if err := r.Delete(ctx, &probe); err != nil {
			deleteErrors = multierror.Append(deleteErrors, err)
		}
	}
	return deleteErrors
}

func (r *DNSRecordReconciler) ensureProbe(ctx context.Context, generated *v1alpha1.DNSHealthCheckProbe, logger logr.Logger) error {
	current := &v1alpha1.DNSHealthCheckProbe{}

	if err := r.Get(ctx, client.ObjectKeyFromObject(generated), current); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info(fmt.Sprintf("Creating probe: %s", generated.Name))
			return r.Create(ctx, generated)
		}
		return err
	}

	desired := current.DeepCopy()
	desired.Spec = generated.Spec

	if !reflect.DeepEqual(current, desired) {
		logger.V(1).Info(fmt.Sprintf("Updating probe: %s", desired.Name))
		if err := r.Update(ctx, desired); err != nil {
			return err
		}
	}
	logger.V(1).Info(fmt.Sprintf("No updates needed for probe: %s", desired.Name))
	return nil
}

func buildDesiredProbes(dnsRecord *v1alpha1.DNSRecord, leafs *[]string, allowInsecureCerts bool) []*v1alpha1.DNSHealthCheckProbe {
	var probes []*v1alpha1.DNSHealthCheckProbe

	if leafs == nil {
		return probes
	}

	for _, leaf := range *leafs {
		probes = append(probes, &v1alpha1.DNSHealthCheckProbe{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", dnsRecord.Name, leaf),
				Namespace: dnsRecord.Namespace,
				Labels:    map[string]string{ProbeOwnerLabel: BuildOwnerLabelValue(dnsRecord)},
			},
			Spec: v1alpha1.DNSHealthCheckProbeSpec{
				Port:                     dnsRecord.Spec.HealthCheck.Port,
				Hostname:                 dnsRecord.Spec.RootHost,
				Address:                  leaf,
				Path:                     dnsRecord.Spec.HealthCheck.Path,
				Protocol:                 dnsRecord.Spec.HealthCheck.Protocol,
				Interval:                 dnsRecord.Spec.HealthCheck.Interval,
				AdditionalHeadersRef:     dnsRecord.Spec.HealthCheck.AdditionalHeadersRef,
				FailureThreshold:         dnsRecord.Spec.HealthCheck.FailureThreshold,
				AllowInsecureCertificate: allowInsecureCerts,
			},
		})
	}
	return probes
}

// BuildOwnerLabelValue ensures label value does not exceed the 63 char limit
// It uses the name of the record,
// if the resulting string longer than 63 chars, it will use UIDHash of the record
func BuildOwnerLabelValue(record *v1alpha1.DNSRecord) string {
	value := record.Name
	if len(value) > 63 {
		return record.GetUIDHash()
	}
	return value
}
