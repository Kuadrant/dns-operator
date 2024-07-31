package helpers

import (
	"context"
	"crypto/rand"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/goombaio/namegenerator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	TestTimeoutMedium = 10 * time.Second
	TestTimeoutLong   = 60 * time.Second
)

func GenerateName() string {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return namegenerator.NewNameGenerator(nBig.Int64()).Generate()
}

func ResolverForDomainName(domainName string) *net.Resolver {
	nameservers, err := net.LookupNS(domainName)
	Expect(err).ToNot(HaveOccurred())
	GinkgoWriter.Printf("[debug] authoritative nameserver used for DNS record resolution: %s\n", nameservers[0].Host)

	authoritativeResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(ctx, network, strings.Join([]string{nameservers[0].Host, "53"}, ":"))
		},
	}
	return authoritativeResolver
}

func EndpointsForHost(ctx context.Context, provider provider.Provider, host string) ([]*externaldnsendpoint.Endpoint, error) {
	filtered := []*externaldnsendpoint.Endpoint{}

	records, err := provider.Records(ctx)
	if err != nil {
		return nil, err
	}

	hostRegexp, err := regexp.Compile(host)
	if err != nil {
		return nil, err
	}

	domainFilter := externaldnsendpoint.NewRegexDomainFilter(hostRegexp, nil)

	for _, record := range records {
		// Ignore records that do not match the domain filter provided
		if !domainFilter.Match(record.DNSName) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}

func ProviderForManagedZone(ctx context.Context, mz *v1alpha1.ManagedZone, c client.Client) (provider.Provider, error) {
	//ToDo mnairn: We have a mismatch in naming GCP vs Google, we need to make this consistent one way or the other
	providerFactory, err := provider.NewFactory(c, []string{"aws", "google", "azure"})
	if err != nil {
		return nil, err
	}
	providerConfig := provider.Config{
		DomainFilter:   externaldnsendpoint.NewDomainFilter([]string{mz.Spec.DomainName}),
		ZoneTypeFilter: externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:   externaldnsprovider.NewZoneIDFilter([]string{mz.Status.ID}),
	}
	//Disable provider logging in test output
	return providerFactory.ProviderFor(logr.NewContext(ctx, logr.Discard()), mz, providerConfig)
}
