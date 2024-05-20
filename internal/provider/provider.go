package provider

import (
	"errors"
	"regexp"
	"strings"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

var (
	statusCodeRegexp        = regexp.MustCompile(`status code: [^\s]+`)
	requestIDRegexp         = regexp.MustCompile(`request id: [^\s]+`)
	saxParseExceptionRegexp = regexp.MustCompile(`Invalid XML ; javax.xml.stream.XMLStreamException: org.xml.sax.SAXParseException; lineNumber: [^\s]+; columnNumber: [^\s]+`)
)

// Provider knows how to manage DNS zones only as pertains to routing.
type Provider interface {
	externaldnsprovider.Provider

	// Ensure will create or update a managed zone, returns an array of NameServers for that zone.
	EnsureManagedZone(managedZone *v1alpha1.ManagedZone) (ManagedZoneOutput, error)

	// Delete will delete a managed zone.
	DeleteManagedZone(managedZone *v1alpha1.ManagedZone) error

	// Get an instance of HealthCheckReconciler for this provider
	HealthCheckReconciler() HealthCheckReconciler

	ProviderSpecific() ProviderSpecificLabels
}

type Config struct {
	// only consider hosted zones managing domains ending in this suffix
	DomainFilter externaldnsendpoint.DomainFilter
	// filter for zones based on visibility
	ZoneTypeFilter externaldnsprovider.ZoneTypeFilter
	// only consider hosted zones ending with this zone id
	ZoneIDFilter externaldnsprovider.ZoneIDFilter
}

type ProviderSpecificLabels struct {
	Weight        string
	HealthCheckID string
}

type ManagedZoneOutput struct {
	ID          string
	DNSName     string
	NameServers []*string
	RecordCount int64
}

// SanitizeError removes request specific data from error messages in order to make them consistent across multiple similar requests to the provider.  e.g AWS SDK Request ids `request id: 051c860b-9b30-4c19-be1a-1280c3e9fdc4`
func SanitizeError(err error) error {
	sanitizedErr := err.Error()
	sanitizedErr = strings.ReplaceAll(sanitizedErr, "\n", " ")
	sanitizedErr = strings.ReplaceAll(sanitizedErr, "\t", " ")
	sanitizedErr = statusCodeRegexp.ReplaceAllString(sanitizedErr, "")
	sanitizedErr = requestIDRegexp.ReplaceAllString(sanitizedErr, "")
	sanitizedErr = saxParseExceptionRegexp.ReplaceAllString(sanitizedErr, "")
	sanitizedErr = strings.TrimSpace(sanitizedErr)
	return errors.New(sanitizedErr)
}
