package provider

import (
	"context"

	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type HealthCheckReconciler interface {
	Reconcile(ctx context.Context, spec HealthCheckSpec, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe, address string) (HealthCheckResult, error)

	Delete(ctx context.Context, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe) (HealthCheckResult, error)
}

type HealthCheckSpec struct {
	Id               string
	Name             string
	Port             *int64
	FailureThreshold *int64
	Protocol         *HealthCheckProtocol
	Host             *string
	Path             string
}

type HealthCheckResult struct {
	Result    HealthCheckReconciliationResult
	ID        string
	IPAddress string
	Host      string
	Message   string
}

func NewHealthCheckResult(result HealthCheckReconciliationResult, id, ipaddress, host, message string) HealthCheckResult {
	return HealthCheckResult{
		Result:    result,
		Message:   message,
		ID:        id,
		IPAddress: ipaddress,
		Host:      host,
	}
}

type HealthCheckReconciliationResult string

const (
	HealthCheckCreated HealthCheckReconciliationResult = "Created"
	HealthCheckUpdated HealthCheckReconciliationResult = "Updated"
	HealthCheckDeleted HealthCheckReconciliationResult = "Deleted"
	HealthCheckNoop    HealthCheckReconciliationResult = "Noop"
	HealthCheckFailed  HealthCheckReconciliationResult = "Failed"
)

type HealthCheckProtocol string

const HealthCheckProtocolHTTP HealthCheckProtocol = "HTTP"
const HealthCheckProtocolHTTPS HealthCheckProtocol = "HTTPS"
