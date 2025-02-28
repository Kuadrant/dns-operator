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

	ErrNoZoneForHost = fmt.Errorf("no zone for host")

	DNSProviderCoreDNS DNSProviderName = "coredns"
	DNSProviderAWS     DNSProviderName = "aws"
	DNSProviderAzure   DNSProviderName = "azure"
	DNSProviderGCP     DNSProviderName = "google"
	DNSProviderInMem   DNSProviderName = "inmemory"

	KuadrantTLD = "kdrnt"
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

	RecordsForHost(ctx context.Context, host string) ([]*externaldnsendpoint.Endpoint, error)
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
func FindDNSZoneForHost(ctx context.Context, host string, zones []DNSZone) (*DNSZone, error) {
	log.FromContext(ctx).V(1).Info(fmt.Sprintf("finding most suitable zone for %s from %v possible zones %v", host, len(zones), zones))
	z, _, err := findDNSZoneForHost(host, host, zones)
	return z, err
}

func isApexDomain(host string, zones []DNSZone) (string, bool) {
	for _, z := range zones {
		if z.DNSName == host {
			return z.ID, true
		}
	}
	return "", false
}

// findDNSZoneForHost will take a host and look for a zone that patches the immediate parent of that host and will continue to step through parents until it either finds a zone  or fails. Example *.example.com will look for example.com and other.domain.example.com will step through each subdomain until it hits example.com.
func findDNSZoneForHost(originalHost, host string, zones []DNSZone) (*DNSZone, string, error) {
	if len(zones) == 0 {
		return nil, "", fmt.Errorf("%w : %s", ErrNoZoneForHost, host)
	}
	host = strings.ToLower(host)
	//get the TLD from this host
	tld, _ := publicsuffix.PublicSuffix(host)

	//The host is a TLD, so we now know `originalHost` can't possibly have a valid `DNSZone` available.
	if host == tld {
		return nil, "", fmt.Errorf("no valid zone found for host: %v", originalHost)
	}

	// We do not currently support creating records for Apex domains, and a DNSZone represents an Apex domain we cannot setup dns for the host
	if id, is := isApexDomain(originalHost, zones); is {
		return nil, "", fmt.Errorf("host %s is an apex domain with zone id %s. Cannot configure DNS for apex domain as apex domains only support A records", originalHost, id)
	}

	hostParts := strings.SplitN(host, ".", 2)
	if len(hostParts) < 2 {
		return nil, "", fmt.Errorf("no valid zone found for host: %s", originalHost)
	}
	parentDomain := hostParts[1]
	// We do not currently support creating records for Apex domains, and a DNSZone represents an Apex domain, as such
	// we should never be trying to find a zone that matches the `originalHost` exactly. Instead, we just continue
	// on to the next possible valid host to try i.e. the parent domain.
	if host == originalHost {
		return findDNSZoneForHost(originalHost, parentDomain, zones)
	}

	matches := slices.DeleteFunc(slices.Clone(zones), func(zone DNSZone) bool {
		return strings.ToLower(zone.DNSName) != host
	})
	if len(matches) > 0 {
		if len(matches) > 1 {
			return nil, "", fmt.Errorf("multiple zones found for host: %s", originalHost)
		}
		subdomain := strings.Replace(strings.ToLower(originalHost), "."+strings.ToLower(matches[0].DNSName), "", 1)
		return &matches[0], subdomain, nil
	}

	return findDNSZoneForHost(originalHost, parentDomain, zones)
}
