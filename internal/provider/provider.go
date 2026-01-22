package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"

	"sigs.k8s.io/controller-runtime/pkg/log"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"
)

type DNSProviderName string

var (
	statusCodeRegexp        = regexp.MustCompile(`status code: [^\s]+`)
	requestIDRegexp         = regexp.MustCompile(`request id: [^\s]+`)
	saxParseExceptionRegexp = regexp.MustCompile(`Invalid XML ; javax.xml.stream.XMLStreamException: org.xml.sax.SAXParseException; lineNumber: [^\s]+; columnNumber: [^\s]+`)

	// ErrNoZoneForHost is returned when no DNS zone can be found that matches the requested host.
	// This error occurs when:
	//   - No zones are available from the provider
	//   - The host is a top-level domain (TLD)
	//   - The host doesn't match any available zone domains
	//   - The host has invalid format (single label)
	//
	// Callers should use errors.Is(err, ErrNoZoneForHost) to check for this error.
	ErrNoZoneForHost = fmt.Errorf("no zone for host")

	// ErrApexDomainNotAllowed is returned when the requested host matches an apex domain
	// (zone root) and the provider does not support apex domains (denyApex=true).
	// Apex domains can only have A/AAAA records, not CNAME records, so some providers
	// restrict their usage.
	//
	// Example: For zone "example.com", the host "example.com" is the apex domain.
	//
	// Callers should use errors.Is(err, ErrApexDomainNotAllowed) to check for this error.
	ErrApexDomainNotAllowed = fmt.Errorf("apex domain not allowed")

	// ErrMultipleZonesFound is returned when multiple zones with the same DNS name exist,
	// making zone selection ambiguous. This typically indicates a configuration issue
	// where zone filters should be used to disambiguate.
	//
	// Example: Two zones both named "example.com" with different IDs.
	//
	// Callers should use errors.Is(err, ErrMultipleZonesFound) to check for this error.
	ErrMultipleZonesFound = fmt.Errorf("multiple zones found for host")

	DNSProviderCoreDNS  DNSProviderName = "coredns"
	DNSProviderAWS      DNSProviderName = "aws"
	DNSProviderAzure    DNSProviderName = "azure"
	DNSProviderGCP      DNSProviderName = "google"
	DNSProviderInMem    DNSProviderName = "inmemory"
	DNSProviderEndpoint DNSProviderName = "endpoint"

	DNSProviderLabel = "kuadrant.io/dns-provider-name"

	CoreDNSRecordZoneLabel = "kuadrant.io/coredns-zone-name"
)

func (dp DNSProviderName) String() string {
	return string(dp)
}

// Provider knows how to manage DNS zones only as pertains to routing.
type Provider interface {
	externaldnsprovider.Provider

	// DNSZones returns a list of dns zones accessible for this provider
	DNSZones(ctx context.Context) ([]DNSZone, error)

	// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
	DNSZoneForHost(ctx context.Context, host string) (*DNSZone, error)

	ProviderSpecific() ProviderSpecificLabels

	Name() DNSProviderName

	Labels() map[string]string
}

type Config struct {
	// filter for specifying a host domain for providers that require it
	HostDomainFilter externaldnsendpoint.DomainFilter
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

type DNSZone struct {
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

// FindDNSZoneForHost finds the most suitable zone for the given host in the given list of DNSZones
func FindDNSZoneForHost(ctx context.Context, host string, zones []DNSZone, denyApex bool) (*DNSZone, error) {
	log.FromContext(ctx).V(1).Info(fmt.Sprintf("finding most suitable zone for %s from %v possible zones %v", host, len(zones), zones))
	z, _, err := findDNSZoneForHost(host, host, zones, denyApex)
	return z, err
}

func IsApexDomain(host string, zones []DNSZone) (string, bool) {
	for _, z := range zones {
		if z.DNSName == host {
			return z.ID, true
		}
	}
	return "", false
}

func IsWildCardHost(host string) bool {
	return strings.HasPrefix(host, "*.")
}

// findDNSZoneForHost will take a host and look for a zone that patches the immediate parent of that host and will continue to step through parents until it either finds a zone  or fails. Example *.example.com will look for example.com and other.domain.example.com will step through each subdomain until it hits example.com.
func findDNSZoneForHost(originalHost, host string, zones []DNSZone, denyApex bool) (*DNSZone, string, error) {
	if len(zones) == 0 {
		return nil, "", fmt.Errorf("%w: %s", ErrNoZoneForHost, host)
	}
	host = strings.ToLower(host)
	//get the TLD from this host
	tld, _ := publicsuffix.PublicSuffix(host)

	//The host is a TLD, so we now know `originalHost` can't possibly have a valid `DNSZone` available.
	if host == tld {
		return nil, "", fmt.Errorf("%w: %s", ErrNoZoneForHost, originalHost)
	}

	// We do not currently support creating records for Apex domains, and a DNSZone represents an Apex domain we cannot setup dns for the host
	if _, is := IsApexDomain(originalHost, zones); is && denyApex {
		return nil, "", fmt.Errorf("%w: %s", ErrApexDomainNotAllowed, originalHost)
	}

	hostParts := strings.SplitN(host, ".", 2)
	if len(hostParts) < 2 {
		return nil, "", fmt.Errorf("%w: %s", ErrNoZoneForHost, originalHost)
	}
	parentDomain := hostParts[1]

	// When apex domains are denied and we're on the first iteration (host == originalHost),
	// skip checking if the host itself is a zone and immediately recurse to the parent domain.
	// This prevents matching the host as an apex domain, which would be denied by the check above
	// after matching. Instead, we proactively skip to the parent to find a valid parent zone.
	// Example: For "example.com" with denyApex=true, skip directly to "com" instead of
	// potentially matching "example.com" as a zone and then failing the apex check.
	if host == originalHost && denyApex {
		return findDNSZoneForHost(originalHost, parentDomain, zones, denyApex)
	}

	matches := slices.DeleteFunc(slices.Clone(zones), func(zone DNSZone) bool {
		return strings.ToLower(zone.DNSName) != host
	})
	if len(matches) > 0 {
		if len(matches) > 1 {
			return nil, "", fmt.Errorf("%w: %s", ErrMultipleZonesFound, originalHost)
		}
		// Calculate subdomain by removing the zone suffix
		// For apex domains (where host == zone), subdomain should be empty
		subdomain := ""
		if strings.ToLower(originalHost) != strings.ToLower(matches[0].DNSName) {
			subdomain = strings.Replace(strings.ToLower(originalHost), "."+strings.ToLower(matches[0].DNSName), "", 1)
		}
		return &matches[0], subdomain, nil
	}

	return findDNSZoneForHost(originalHost, parentDomain, zones, denyApex)
}
