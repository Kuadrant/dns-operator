package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common"
)

func (r *DNSRecordReconciler) ReconcileHealthChecks(ctx context.Context, dnsRecord *v1alpha1.DNSRecord, allowInsecureCerts bool) error {
	logger := log.FromContext(ctx).WithName("healthchecks")
	logger.Info("Reconciling healthchecks")

	// Probes enabled but no health check spec yet. Nothing to do
	if dnsRecord.Spec.HealthCheck == nil {
		return r.DeleteHealthChecks(ctx, dnsRecord)
	}

	// we don't support probes for wildcard hosts
	if strings.HasPrefix(dnsRecord.Spec.RootHost, v1alpha1.WildcardPrefix) {
		return nil
	}

	desiredProbes := buildDesiredProbes(dnsRecord, common.GetLeafsTargets(common.MakeTreeFromDNSRecord(dnsRecord), ptr.To([]string{})), allowInsecureCerts)

	for _, probe := range desiredProbes {
		// if one of them fails - health checks for this record are invalid anyway, so no sense to continue
		if err := controllerruntime.SetControllerReference(dnsRecord, probe, r.BaseDNSRecordReconciler.Scheme); err != nil {
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

// removeUnhealthyEndpoints fetches all probes associated with this record and uses the following criteria while removing endpoints:
//   - If the Leaf Address has no health check CR - it is healthy
//   - If the health check CR has insufficient failures - it is healthy
//   - If the health check CR is deleting - it is healthy
//   - If the health check is a CNAME and any IP is healthy - the CNAME is healthy
//
// If this leads to an empty array of endpoints it:
//   - Does nothing (prevents NXDomain response) if we already published
//   - Returns empty array if nothing is published (prevent from publishing unhealthy EPs)
//
// it returns the list of healthy endpoints, an array of unhealthy addresses and an error
func removeUnhealthyEndpoints(specEndpoints []*endpoint.Endpoint, dnsRecord *v1alpha1.DNSRecord, probes *v1alpha1.DNSHealthCheckProbeList) ([]*endpoint.Endpoint, []string, error) {

	// we are deleting or don't have health checks - don't bother
	if (dnsRecord.DeletionTimestamp != nil && !dnsRecord.DeletionTimestamp.IsZero()) || dnsRecord.Spec.HealthCheck == nil || !probesEnabled {
		return specEndpoints, []string{}, nil
	}

	// we have wildcard record - healthchecks not supported
	if strings.HasPrefix(dnsRecord.Spec.RootHost, v1alpha1.WildcardPrefix) {
		return specEndpoints, []string{}, nil
	}

	unhealthyAddresses := make([]string, 0, len(probes.Items))

	// use adjusted endpoints instead of spec ones
	tree := common.MakeTreeFromDNSRecord(&v1alpha1.DNSRecord{
		Spec: v1alpha1.DNSRecordSpec{
			RootHost:  dnsRecord.Spec.RootHost,
			Endpoints: specEndpoints,
		},
	})

	var haveHealthyProbes bool
	for _, probe := range probes.Items {
		// if the probe is healthy, continue to the next probe
		if probe.Status.Healthy != nil && *probe.Status.Healthy {
			haveHealthyProbes = true
			continue
		}

		// if unhealthy or we haven't probed yet
		//delete bad endpoint from all endpoints targets
		tree.RemoveNode(&common.DNSTreeNode{
			Name: probe.Spec.Address,
		})
		unhealthyAddresses = append(unhealthyAddresses, probe.Spec.Address)
	}

	// if at least one of the leaf probes was healthy return healthy probes
	if haveHealthyProbes {
		return *common.ToEndpoints(tree, ptr.To([]*endpoint.Endpoint{})), unhealthyAddresses, nil
	}
	// if none of the probes are healthy or probes don't exist - don't modify endpoints
	return dnsRecord.Status.Endpoints, unhealthyAddresses, nil
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

// GetOwnerFromLabel returns a name or UID of probe owner
// A reverse to BuildOwnerLabelValue
func GetOwnerFromLabel(probe *v1alpha1.DNSHealthCheckProbe) string {
	return probe.GetLabels()[ProbeOwnerLabel]
}

type healthCheckAdapter struct {
	DNSRecordAccessor
	probes               *v1alpha1.DNSHealthCheckProbeList
	healthySpecEndpoints []*endpoint.Endpoint
	notHealthyProbes     []string
}

func newHealthCheckAdapter(accessor DNSRecordAccessor, probes *v1alpha1.DNSHealthCheckProbeList) *healthCheckAdapter {
	hca := &healthCheckAdapter{
		DNSRecordAccessor: accessor,
		probes:            probes,
	}
	return hca
}

func (s *healthCheckAdapter) removeUnhealthyEndpoints() {
	//ToDo removeUnhealthyEndpoints is manipulating the record spec and producing incorrect spec data with duplicate target values.
	// Current workaround is to pass in a copy of the record since we only care about the return values anyway.
	recCopy := s.GetDNSRecord().DeepCopy()
	specEndpoints := s.DNSRecordAccessor.GetEndpoints()
	// healthySpecEndpoints = Records that this DNSRecord expects to exist, that do not have matching unhealthy probes
	// Note: Error is ignored because one is never returned from `removeUnhealthyEndpoints`
	healthySpecEndpoints, notHealthyProbes, _ := removeUnhealthyEndpoints(specEndpoints, recCopy, s.probes)
	s.healthySpecEndpoints = healthySpecEndpoints
	s.notHealthyProbes = notHealthyProbes
}

func (s *healthCheckAdapter) GetEndpoints() []*endpoint.Endpoint {
	s.removeUnhealthyEndpoints()
	return s.healthySpecEndpoints
}

func (s *healthCheckAdapter) SetStatusConditions(hadChanges bool) {
	s.DNSRecordAccessor.SetStatusConditions(hadChanges)

	// we don't have probes yet
	if cap(s.notHealthyProbes) == 0 {
		s.SetStatusCondition(string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), "Probes are creating")
		return
	}

	// we have healthy probes
	if len(s.notHealthyProbes) < cap(s.notHealthyProbes) {
		if len(s.notHealthyProbes) == 0 {
			// all probes are healthy
			s.SetStatusCondition(string(v1alpha1.ConditionTypeHealthy), metav1.ConditionTrue, string(v1alpha1.ConditionReasonHealthy), "All healthchecks succeeded")
		} else {
			// at least one of the probes is healthy
			s.SetStatusCondition(string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonPartiallyHealthy), fmt.Sprintf("Not healthy addresses: %s", s.notHealthyProbes))
		}
		return
	}
	// none of the probes is healthy
	s.SetStatusCondition(string(v1alpha1.ConditionTypeHealthy), metav1.ConditionFalse, string(v1alpha1.ConditionReasonUnhealthy), fmt.Sprintf("Not healthy addresses: %s", s.notHealthyProbes))
	return
}
