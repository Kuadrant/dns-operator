package controller

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

// healthChecksConfig represents the user configuration for the health checks
type healthChecksConfig struct {
	Endpoint         string
	Port             *int64
	FailureThreshold *int64
	Protocol         *provider.HealthCheckProtocol
}

func (r *DNSRecordReconciler) ReconcileHealthChecks(ctx context.Context, dnsRecord *v1alpha1.DNSRecord) error {
	var results []provider.HealthCheckResult
	var err error

	dnsProvider, err := r.getDNSProvider(ctx, dnsRecord)
	if err != nil {
		return err
	}

	healthCheckReconciler := dnsProvider.HealthCheckReconciler()

	// Get the configuration for the health checks. If no configuration is
	// set, ensure that the health checks are deleted
	config := getHealthChecksConfig(dnsRecord)

	for _, dnsEndpoint := range dnsRecord.Spec.Endpoints {
		addresses := provider.GetExternalAddresses(dnsEndpoint, dnsRecord)
		for _, address := range addresses {
			probeStatus := r.getProbeStatus(address, dnsRecord)

			// no config means delete the health checks
			if config == nil {
				result, err := healthCheckReconciler.Delete(ctx, dnsEndpoint, probeStatus)
				if err != nil {
					return err
				}

				results = append(results, result)
				continue
			}

			// creating / updating health checks
			endpointId, err := idForEndpoint(dnsRecord, dnsEndpoint, address)
			if err != nil {
				return err
			}

			spec := provider.HealthCheckSpec{
				Id:               endpointId,
				Name:             fmt.Sprintf("%s-%s-%s", *dnsRecord.Spec.RootHost, dnsEndpoint.DNSName, address),
				Host:             dnsRecord.Spec.RootHost,
				Path:             config.Endpoint,
				Port:             config.Port,
				Protocol:         config.Protocol,
				FailureThreshold: config.FailureThreshold,
			}

			result, err := healthCheckReconciler.Reconcile(ctx, spec, dnsEndpoint, probeStatus, address)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
	}

	result := r.reconcileHealthCheckStatus(results, dnsRecord)
	return result
}

func (r *DNSRecordReconciler) getProbeStatus(address string, dnsRecord *v1alpha1.DNSRecord) *v1alpha1.HealthCheckStatusProbe {
	if dnsRecord.Status.HealthCheck == nil || dnsRecord.Status.HealthCheck.Probes == nil {
		return nil
	}
	for _, probeStatus := range dnsRecord.Status.HealthCheck.Probes {
		if probeStatus.IPAddress == address {
			return &probeStatus
		}
	}

	return nil
}

func (r *DNSRecordReconciler) reconcileHealthCheckStatus(results []provider.HealthCheckResult, dnsRecord *v1alpha1.DNSRecord) error {
	var previousCondition *metav1.Condition
	probesCondition := &metav1.Condition{
		Reason: "AllProbesSynced",
		Type:   "healthProbesSynced",
	}

	var allSynced = metav1.ConditionTrue

	if dnsRecord.Status.HealthCheck == nil {
		dnsRecord.Status.HealthCheck = &v1alpha1.HealthCheckStatus{
			Conditions: []metav1.Condition{},
			Probes:     []v1alpha1.HealthCheckStatusProbe{},
		}
	}

	previousCondition = meta.FindStatusCondition(dnsRecord.Status.HealthCheck.Conditions, "HealthProbesSynced")
	if previousCondition != nil {
		probesCondition = previousCondition
	}

	dnsRecord.Status.HealthCheck.Probes = []v1alpha1.HealthCheckStatusProbe{}

	for _, result := range results {
		if result.ID == "" {
			continue
		}
		status := true
		if result.Result == provider.HealthCheckFailed {
			status = false
			allSynced = metav1.ConditionFalse
		}

		dnsRecord.Status.HealthCheck.Probes = append(dnsRecord.Status.HealthCheck.Probes, v1alpha1.HealthCheckStatusProbe{
			ID:        result.ID,
			IPAddress: result.IPAddress,
			Host:      result.Host,
			Synced:    status,
		})
	}

	probesCondition.ObservedGeneration = dnsRecord.Generation
	probesCondition.Status = allSynced

	if allSynced == metav1.ConditionTrue {
		probesCondition.Message = fmt.Sprintf("all %v probes synced successfully", len(dnsRecord.Status.HealthCheck.Probes))
		probesCondition.Reason = "AllProbesSynced"
	} else {
		probesCondition.Reason = "UnsyncedProbes"
		probesCondition.Message = "some probes have not yet successfully synced to the DNS Provider"
	}

	//probe condition changed? - update transition time
	if !reflect.DeepEqual(previousCondition, probesCondition) {
		probesCondition.LastTransitionTime = metav1.Now()
	}

	dnsRecord.Status.HealthCheck.Conditions = []metav1.Condition{*probesCondition}

	return nil
}

func getHealthChecksConfig(dnsRecord *v1alpha1.DNSRecord) *healthChecksConfig {
	if dnsRecord.Spec.HealthCheck == nil || dnsRecord.DeletionTimestamp != nil {
		return nil
	}

	port := int64(*dnsRecord.Spec.HealthCheck.Port)
	failureThreshold := int64(*dnsRecord.Spec.HealthCheck.FailureThreshold)

	return &healthChecksConfig{
		Endpoint:         dnsRecord.Spec.HealthCheck.Endpoint,
		Port:             &port,
		FailureThreshold: &failureThreshold,
		Protocol:         (*provider.HealthCheckProtocol)(dnsRecord.Spec.HealthCheck.Protocol),
	}
}

// idForEndpoint returns a unique identifier for an endpoint
func idForEndpoint(dnsRecord *v1alpha1.DNSRecord, endpoint *externaldns.Endpoint, address string) (string, error) {
	hash := md5.New()
	if _, err := io.WriteString(hash, fmt.Sprintf("%s/%s@%s:%s", dnsRecord.Name, endpoint.SetIdentifier, endpoint.DNSName, address)); err != nil {
		return "", fmt.Errorf("unexpected error creating ID for endpoint %s", endpoint.SetIdentifier)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
