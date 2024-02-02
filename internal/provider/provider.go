package provider

import (
	"errors"
	"regexp"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

const (
	ProviderSpecificWeight  = "weight"
	ProviderSpecificGeoCode = "geo-code"
)

// Provider knows how to manage DNS zones only as pertains to routing.
type Provider interface {

	// Ensure will create or update record.
	Ensure(record *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone) error

	// Delete will delete record.
	Delete(record *v1alpha1.DNSRecord, managedZone *v1alpha1.ManagedZone) error

	// Ensure will create or update a managed zone, returns an array of NameServers for that zone.
	EnsureManagedZone(managedZone *v1alpha1.ManagedZone) (ManagedZoneOutput, error)

	// Delete will delete a managed zone.
	DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error
}

type ProviderSpecificLabels struct {
	Weight        string
	HealthCheckID string
}

type ManagedZoneOutput struct {
	ID          string
	NameServers []*string
	RecordCount int64
}

// SanitizeError removes request specific data from error messages in order to make them consistent across multiple similar requests to the provider.  e.g AWS SDK Request ids `request id: 051c860b-9b30-4c19-be1a-1280c3e9fdc4`
func SanitizeError(err error) error {
	re := regexp.MustCompile(`request id: [^\s]+`)
	sanitizedErr := re.ReplaceAllString(err.Error(), "")
	return errors.New(sanitizedErr)
}
