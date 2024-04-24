package provider

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type HealthCheckReconciler interface {
	Reconcile(ctx context.Context, spec HealthCheckSpec, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe, address string) HealthCheckResult
	Delete(ctx context.Context, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe) (HealthCheckResult, error)
	HealthCheckExists(ctx context.Context, probeStatus *v1alpha1.HealthCheckStatusProbe) (bool, error)
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
	Condition metav1.Condition
}

func NewHealthCheckResult(result HealthCheckReconciliationResult, id, ipaddress, host string, condition metav1.Condition) HealthCheckResult {
	return HealthCheckResult{
		Result:    result,
		Condition: condition,
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
