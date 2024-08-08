//go:build unit

package builder

import (
	"fmt"
	"slices"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/external-dns/endpoint"
)

func TestV1alpha1(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "API suite")
}

// misc
func HostWildcard(domain string) string {
	return fmt.Sprintf("*.%s", domain)
}

func HostOne(domain string) string {
	return fmt.Sprintf("%s.%s", "test", domain)
}

func HostTwo(domain string) string {
	return fmt.Sprintf("%s.%s", "other.test", domain)
}

// EndpointsTraversable consumes an array of endpoints and returns a boolean
// indicating presence of that path from host to all destinations
// this function DOES NOT report a presence of an endpoint with one of destinations DNSNames
func EndpointsTraversable(endpoints []*endpoint.Endpoint, host string, destinations []string) bool {
	allDestinationsFound := len(destinations) > 0
	for _, destination := range destinations {
		allTargetsFound := false
		for _, ep := range endpoints {
			// the host exists as a DNSName on an endpoint
			if ep.DNSName == host {
				// we found destination in the targets of the endpoint.
				if slices.Contains(ep.Targets, destination) {
					return true
				}
				// destination is not found on the endpoint. Use target as a host and check for existence of Endpoints with such a DNSName
				for _, target := range ep.Targets {
					// if at least one returns as true allTargetsFound will be locked in true
					// this means that at least one of the targets on the endpoint leads to the destination
					allTargetsFound = allTargetsFound || EndpointsTraversable(endpoints, target, []string{destination})
				}
			}
		}
		// we must match all destinations
		allDestinationsFound = allDestinationsFound && allTargetsFound
	}
	// there are no destinations to look for: len(destinations) == 0 locks allDestinationsFound into false
	// or every destination was matched to a target on the endpoint
	return allDestinationsFound
}
