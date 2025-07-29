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

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
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

type NameServerNone string

// ResolversForDomainNameAndProvider returns a list of resolvers for the given domain and provider secret and if they
// are configured with authoritative nameservers or not.
func ResolversForDomainNameAndProvider(domainName string, providerSecret *v1.Secret) (resolvers []*net.Resolver, authoritative bool) {
	var nameServers []string
	authoritative = true

	if providerSecret.Type == v1alpha1.SecretTypeKuadrantCoreDNS {
		coreDNSNS := providerSecret.Data["NAMESERVERS"]
		Expect(coreDNSNS).NotTo(BeEmpty())
		nameServers = strings.Split(string(coreDNSNS), ",")
	} else if providerSecret.Type != v1alpha1.SecretTypeKuadrantAzure {
		// speed up things by using an authoritative nameserver
		nss, err := net.LookupNS(domainName)
		Expect(err).ToNot(HaveOccurred())
		nameServers = append(nameServers, strings.Join([]string{nss[0].Host, "53"}, ":"))
	}

	if nameServers == nil {
		resolvers = append(resolvers, net.DefaultResolver)
		authoritative = false
	} else {
		for _, nameServer := range nameServers {
			resolvers = append(resolvers, ResolverForNameServer(nameServer))
		}
	}

	return resolvers, authoritative
}

func ResolverForNameServer(nameserver string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(ctx, network, nameserver)
		},
	}
}

func EndpointsForHost(ctx context.Context, p provider.Provider, host string) ([]*externaldnsendpoint.Endpoint, error) {
	filtered := []*externaldnsendpoint.Endpoint{}

	records, err := p.Records(ctx)
	if err != nil {
		return nil, err
	}

	GinkgoWriter.Printf("[debug] records from zone count: %d\n", len(records))

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

func ProviderForDNSRecord(ctx context.Context, record *v1alpha1.DNSRecord, c client.Client, d dynamic.Interface) (provider.Provider, error) {
	providerFactory, err := provider.NewFactory(c, d, []string{provider.DNSProviderAWS.String(), provider.DNSProviderGCP.String(), provider.DNSProviderAzure.String(), provider.DNSProviderCoreDNS.String(), string(provider.DNSProviderEndpoint)})
	if err != nil {
		return nil, err
	}
	providerConfig := provider.Config{
		HostDomainFilter: externaldnsendpoint.NewDomainFilter([]string{record.Spec.RootHost}),
		DomainFilter:     externaldnsendpoint.NewDomainFilter([]string{record.Status.ZoneDomainName}),
		ZoneTypeFilter:   externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:     externaldnsprovider.NewZoneIDFilter([]string{record.Status.ZoneID}),
	}
	//Disable provider logging in test output
	return providerFactory.ProviderFor(logr.NewContext(ctx, logr.Discard()), record, providerConfig)
}

func FindDefaultTarget(eps []*externaldnsendpoint.Endpoint) string {
	for _, ep := range eps {
		for _, ps := range ep.ProviderSpecific {
			if ps.Value == "WORLD" {
				return ps.Name
			}
		}
	}
	return ""
}
