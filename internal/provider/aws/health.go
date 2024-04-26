package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
	"github.com/rs/xid"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	idTag = "kuadrant.dev/healthcheck"

	defaultHealthCheckPath             = "/"
	defaultHealthCheckPort             = 80
	defaultHealthCheckFailureThreshold = 3
)

var (
	callerReference func(id string) *string
)

type Route53HealthCheckReconciler struct {
	client route53iface.Route53API
}

var _ provider.HealthCheckReconciler = &Route53HealthCheckReconciler{}

func NewRoute53HealthCheckReconciler(client route53iface.Route53API) *Route53HealthCheckReconciler {
	return &Route53HealthCheckReconciler{
		client: client,
	}
}

func getTransitionTime(probeConditions []metav1.Condition, conditionType string, conditionStatus metav1.ConditionStatus) metav1.Time {
	for _, c := range probeConditions {
		if c.Type == conditionType && c.Status == conditionStatus {
			return c.LastTransitionTime
		}
	}
	return metav1.Now()
}

func (r *Route53HealthCheckReconciler) Reconcile(ctx context.Context, spec provider.HealthCheckSpec, endpoint *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe, address string) provider.HealthCheckResult {
	healthCheck, exists, err := r.findHealthCheck(ctx, probeStatus)
	if err != nil {
		lastTransition := metav1.Now()
		if probeStatus != nil {
			lastTransition = getTransitionTime(probeStatus.Conditions, "ProbeSynced", "False")
		}
		return provider.HealthCheckResult{
			Result:    provider.HealthCheckFailed,
			ID:        "",
			IPAddress: address,
			Host:      *spec.Host,
			Condition: metav1.Condition{
				Type:               "ProbeSynced",
				Status:             "False",
				LastTransitionTime: lastTransition,
				Reason:             "DNSProviderError",
				Message:            fmt.Sprintf("probe (id: %v, address: %v, host: %v) error recovering existing health check: %v", "", address, *spec.Host, err),
			},
		}
	}

	if exists {
		status, err := r.updateHealthCheck(ctx, spec, endpoint, healthCheck, address)
		if err != nil {
			return provider.HealthCheckResult{
				Result:    provider.HealthCheckFailed,
				ID:        "",
				IPAddress: address,
				Host:      *spec.Host,
				Condition: metav1.Condition{
					Type:               "ProbeSynced",
					Status:             "False",
					LastTransitionTime: getTransitionTime(probeStatus.Conditions, "ProbeSynced", "False"),
					Reason:             "DNSProviderError",
					Message:            fmt.Sprintf("probe (id: %v, address: %v, host: %v) error updating existing health check: %v", "", address, *spec.Host, err),
				},
			}
		}

		return provider.NewHealthCheckResult(status, *healthCheck.Id, address, *spec.Host, metav1.Condition{
			Type:               "ProbeSynced",
			Status:             "True",
			LastTransitionTime: getTransitionTime(probeStatus.Conditions, "ProbeSynced", "True"),
			Reason:             "ProbeSyncSuccessful",
			Message:            fmt.Sprintf("probe (id: %v, address: %v, host: %v)  synced successfully", *healthCheck.Id, address, *spec.Host),
		})
	}

	healthCheck, err = r.createHealthCheck(ctx, spec, address)
	if err != nil {
		lastTransition := metav1.Now()
		if probeStatus != nil {
			lastTransition = getTransitionTime(probeStatus.Conditions, "ProbeSynced", "False")
		}
		return provider.HealthCheckResult{
			Result:    provider.HealthCheckFailed,
			ID:        "",
			IPAddress: address,
			Host:      *spec.Host,
			Condition: metav1.Condition{
				Type:               "ProbeSynced",
				Status:             "False",
				LastTransitionTime: lastTransition,
				Reason:             "DNSProviderError",
				Message:            fmt.Sprintf("probe (id: %v, address: %v, host: %v) error from DNS Provider: %v", "", address, *spec.Host, err),
			},
		}
	}
	lastTransition := metav1.Now()
	if probeStatus != nil {
		lastTransition = getTransitionTime(probeStatus.Conditions, "ProbeSynced", "True")
	}
	return provider.NewHealthCheckResult(provider.HealthCheckCreated, *healthCheck.Id, address, *spec.Host, metav1.Condition{
		Type:               "ProbeSynced",
		Status:             "True",
		LastTransitionTime: lastTransition,
		Reason:             "ProbeSyncSuccessful",
		Message:            fmt.Sprintf("probe (id: %v, address: %v, host: %v) synced successfully", *healthCheck.Id, address, *spec.Host),
	})
}

func (r *Route53HealthCheckReconciler) Delete(ctx context.Context, _ *externaldns.Endpoint, probeStatus *v1alpha1.HealthCheckStatusProbe) (provider.HealthCheckResult, error) {
	healthCheck, found, err := r.findHealthCheck(ctx, probeStatus)
	if err != nil {
		return provider.HealthCheckResult{}, err
	}
	if !found {
		return provider.NewHealthCheckResult(provider.HealthCheckNoop, "", "", "", metav1.Condition{}), nil
	}

	_, err = r.client.DeleteHealthCheckWithContext(ctx, &route53.DeleteHealthCheckInput{
		HealthCheckId: healthCheck.Id,
	})

	if err != nil {
		return provider.HealthCheckResult{}, err
	}

	return provider.NewHealthCheckResult(provider.HealthCheckDeleted, *healthCheck.Id, "", "", metav1.Condition{}), nil
}

func (r *Route53HealthCheckReconciler) findHealthCheck(ctx context.Context, probeStatus *v1alpha1.HealthCheckStatusProbe) (*route53.HealthCheck, bool, error) {
	if probeStatus == nil || probeStatus.ID == "" {
		return nil, false, nil
	}

	response, err := r.client.GetHealthCheckWithContext(ctx, &route53.GetHealthCheckInput{
		HealthCheckId: &probeStatus.ID,
	})
	if err != nil {
		return nil, false, err
	}

	return response.HealthCheck, true, nil

}

func (r *Route53HealthCheckReconciler) createHealthCheck(ctx context.Context, spec provider.HealthCheckSpec, address string) (*route53.HealthCheck, error) {
	// Create the health check
	output, err := r.client.CreateHealthCheck(&route53.CreateHealthCheckInput{

		CallerReference: callerReference(spec.Id),

		HealthCheckConfig: &route53.HealthCheckConfig{
			IPAddress:                &address,
			FullyQualifiedDomainName: spec.Host,
			Port:                     spec.Port,
			ResourcePath:             &spec.Path,
			Type:                     healthCheckType(spec.Protocol),
			FailureThreshold:         spec.FailureThreshold,
		},
	})
	if err != nil {
		return nil, removeMetaData(err)
	}

	// Add the tag to identify it
	_, err = r.client.ChangeTagsForResourceWithContext(ctx, &route53.ChangeTagsForResourceInput{
		AddTags: []*route53.Tag{
			{
				Key:   aws.String(idTag),
				Value: aws.String(spec.Id),
			},
			{
				Key:   aws.String("Name"),
				Value: &spec.Name,
			},
		},
		ResourceId:   output.HealthCheck.Id,
		ResourceType: aws.String(route53.TagResourceTypeHealthcheck),
	})
	if err != nil {
		return nil, removeMetaData(err)
	}

	return output.HealthCheck, nil
}

func (r *Route53HealthCheckReconciler) updateHealthCheck(ctx context.Context, spec provider.HealthCheckSpec, endpoint *externaldns.Endpoint, healthCheck *route53.HealthCheck, address string) (provider.HealthCheckReconciliationResult, error) {
	diff := healthCheckDiff(healthCheck, spec, endpoint, address)
	if diff == nil {
		return provider.HealthCheckNoop, nil
	}

	_, err := r.client.UpdateHealthCheckWithContext(ctx, diff)
	if err != nil {
		return provider.HealthCheckFailed, removeMetaData(err)
	}

	return provider.HealthCheckUpdated, nil
}

// removeMetaData from the error responses to the API
// this is janky, but without removing this data, the constantly
// changing request ID and other data in the response causes
// our controller to never stop writing the status update
func removeMetaData(originalError error) error {
	if awsErr, ok := originalError.(awserr.Error); ok {
		chunks := strings.Split(awsErr.Message(), ":")
		newErr := strings.TrimSpace(strings.ReplaceAll(chunks[len(chunks)-1], "'", ""))
		return errors.New(newErr)
	}
	return originalError
}

// healthCheckDiff creates a `UpdateHealthCheckInput` object with the fields to
// update on healthCheck based on the given spec.
// If the health check matches the spec, returns `nil`
func healthCheckDiff(healthCheck *route53.HealthCheck, spec provider.HealthCheckSpec, endpoint *externaldns.Endpoint, address string) *route53.UpdateHealthCheckInput {
	var result *route53.UpdateHealthCheckInput

	// "Lazily" set the value for result only once and only when there is
	// a change, to ensure that it's nil if there's no change
	diff := func() *route53.UpdateHealthCheckInput {
		if result == nil {
			result = &route53.UpdateHealthCheckInput{
				HealthCheckId: healthCheck.Id,
			}
		}

		return result
	}

	if !valuesEqual(&endpoint.DNSName, healthCheck.HealthCheckConfig.FullyQualifiedDomainName) {
		diff().FullyQualifiedDomainName = spec.Host
	}

	if !valuesEqual(&address, healthCheck.HealthCheckConfig.IPAddress) {
		diff().IPAddress = &address
	}
	if !valuesEqualWithDefault(&spec.Path, healthCheck.HealthCheckConfig.ResourcePath, defaultHealthCheckPath) {
		diff().ResourcePath = &spec.Path
	}
	if !valuesEqualWithDefault(spec.Port, healthCheck.HealthCheckConfig.Port, defaultHealthCheckPort) {
		diff().Port = spec.Port
	}
	if !valuesEqualWithDefault(spec.FailureThreshold, healthCheck.HealthCheckConfig.FailureThreshold, defaultHealthCheckFailureThreshold) {
		diff().FailureThreshold = spec.FailureThreshold
	}

	return result
}

func init() {
	sid := xid.New()
	callerReference = func(s string) *string {
		return aws.String(fmt.Sprintf("%s.%s", s, sid))
	}
}

func healthCheckType(protocol *provider.HealthCheckProtocol) *string {
	if protocol == nil {
		return nil
	}

	switch *protocol {
	case provider.HealthCheckProtocolHTTP:
		return aws.String(route53.HealthCheckTypeHttp)

	case provider.HealthCheckProtocolHTTPS:
		return aws.String(route53.HealthCheckTypeHttps)
	}

	return nil
}

func valuesEqual[T comparable](ptr1, ptr2 *T) bool {
	if (ptr1 == nil && ptr2 != nil) || (ptr1 != nil && ptr2 == nil) {
		return false
	}
	if ptr1 == nil && ptr2 == nil {
		return true
	}

	return *ptr1 == *ptr2
}

func valuesEqualWithDefault[T comparable](ptr1, ptr2 *T, defaultValue T) bool {
	value1 := defaultValue
	if ptr1 != nil {
		value1 = *ptr1
	}

	value2 := defaultValue
	if ptr2 != nil {
		value2 = *ptr2
	}

	return value1 == value2
}
