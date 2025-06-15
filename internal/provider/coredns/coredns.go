// Core DNS provider is responsible for calling out to core dns instances via DNS for hosts it knows about to pull together the full set of "merged endpoints". This set of merged endpoints reprent the entire record set for a given host.

package coredns

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/miekg/dns"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/internal/provider"
)

type CoreDNSProvider struct {
	logger         logr.Logger
	nameservers    []*string
	availableZones []string
	DNSQueryFunc   QueryFunc
	hostFilter     endpoint.DomainFilter
}

type QueryFunc func(hosts []string, nameserver string) (map[string]*dns.Msg, error)

var p provider.Provider = &CoreDNSProvider{}

const (
	TxtGEOCodePrefix      = "geo="
	TxtDefaultGEOPrefix   = "default="
	TxtCodeTypeGEOPrefix  = "type="
	ProviderNameserverKey = "NAMESERVERS"
	ProviderZonesKey      = "ZONES"
)

// Register this Provider with the provider factory
func init() {
	provider.RegisterProviderFromSecret(p.Name().String()+"FromSecret", NewCoreDNSProviderFromSecret, true)
	provider.RegisterProvider(p.Name().String(), NewCoreDNSProvider, true)
}

func NewCoreDNSProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName(p.Name().String())

	if string(s.Data[ProviderNameserverKey]) == "" {
		return nil, fmt.Errorf("CoreDNS Provider credentials does not contain %s", ProviderNameserverKey)
	}

	if string(s.Data[ProviderZonesKey]) == "" {
		return nil, fmt.Errorf("CoreDNS Provider credentials does not contain %s", ProviderZonesKey)
	}

	p := &CoreDNSProvider{
		logger:     logger,
		hostFilter: c.HostDomainFilter,
	}
	if _, ok := s.Data[ProviderNameserverKey]; ok {
		nameservers := []*string{}
		nservers := strings.Split(strings.TrimSpace(string(s.Data[ProviderNameserverKey])), ",")
		for _, ns := range nservers {
			if err := validateNSAddress(ns); err != nil {
				return p, err
			}
			nameservers = append(nameservers, &ns)
		}
		p.nameservers = nameservers
	}
	if _, ok := s.Data[ProviderZonesKey]; ok {
		p.availableZones = strings.Split(strings.TrimSpace(string(s.Data[ProviderZonesKey])), ",")
	}
	p.availableZones = append(p.availableZones, provider.KuadrantTLD)
	p.DNSQueryFunc = p.dnsQuery
	return p, nil
}

func NewCoreDNSProvider(ctx context.Context, c provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName(p.Name().String())

	p := &CoreDNSProvider{
		logger:     logger,
		hostFilter: c.HostDomainFilter,
	}

	nameservers := []*string{}
	nservers := strings.Split(strings.TrimSpace(string(c.CoreDNSNameserver)), ",")
	for _, ns := range nservers {
		if err := validateNSAddress(ns); err != nil {
			return p, err
		}
		nameservers = append(nameservers, &ns)
	}
	p.nameservers = nameservers

	p.availableZones = strings.Split(strings.TrimSpace(string(c.CoreDNSZones)), ",")

	p.availableZones = append(p.availableZones, provider.KuadrantTLD)
	p.DNSQueryFunc = p.dnsQuery
	return p, nil
}

func validateNSAddress(address string) error {
	// currently we only can work with an IP and port number
	nsParts := strings.Split(address, ":")
	if len(nsParts) != 2 {
		return fmt.Errorf("expected an IP and Port number in format 1.2.4.5:53 got %s", address)
	}
	if nil == net.ParseIP(nsParts[0]) {
		return fmt.Errorf("expected an IPAddress as nameserver address but got %s", nsParts[0])
	}
	if _, err := strconv.ParseUint(nsParts[1], 10, 64); err != nil {
		return fmt.Errorf("port number expected at end of nameserver but got %s", nsParts[1])
	}
	return nil
}

func (p CoreDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderCoreDNS
}

// DNSZones returns a list of dns zones accessible for this provider
func (p *CoreDNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	zones := []provider.DNSZone{}
	for id, zone := range p.availableZones {
		zones = append(zones, provider.DNSZone{
			ID:          fmt.Sprintf("id-%d", id),
			DNSName:     zone,
			NameServers: p.nameservers,
		})
	}
	return zones, nil
}

// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
func (p *CoreDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	var assignedZone *provider.DNSZone
	zones, _ := p.DNSZones(ctx)
	for _, z := range zones {
		if strings.HasSuffix(host, z.DNSName) {
			if assignedZone == nil {
				assignedZone = &z
			}
			if len(assignedZone.DNSName) < len(z.DNSName) {
				assignedZone = &z
			}
		}
	}
	if assignedZone == nil {
		return nil, provider.ErrNoZoneForHost
	}
	return assignedZone, nil
}

func (p *CoreDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *CoreDNSProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	if !p.hostFilter.IsConfigured() {
		return nil, fmt.Errorf("no host domain filter specified for CoreDNS Provider")
	}
	return p.recordsForHost(p.hostFilter.Filters[0])
}

// recordsForHost returns the records each configured core dns has for a given host
func (p *CoreDNSProvider) recordsForHost(host string) ([]*endpoint.Endpoint, error) {
	// if host prefix matches our local prefix query nameservers to get other records and return merged endpoints
	// if host doesn't match prefix get the records from the resource directly.
	kudrantHost := fmt.Sprintf("%s.%s", host, provider.KuadrantTLD)
	noneWildCardHost := strings.Replace(kudrantHost, "*", "wildcard", -1)
	hosts := []string{kudrantHost, fmt.Sprintf("w.%s", noneWildCardHost), fmt.Sprintf("g.%s", noneWildCardHost)}
	var endpoints []*endpoint.Endpoint
	// do our queries and gather up the answers
	answers := map[string]map[string]*dns.Msg{}
	p.logger.Info("recordsForHost", "total nameservers", len(p.nameservers), "hosts", hosts)
	for _, nServer := range p.nameservers {
		p.logger.Info("checking nameserver ", "address", *nServer)
		nsAnswer, err := p.DNSQueryFunc(hosts, *nServer)
		if err != nil {
			//TODO prob need to handle dns errors better here
			return endpoints, err
		}
		answers[*nServer] = nsAnswer
	}
	//merge the answers into a single set of endpoints
	for server, answer := range answers {
		dns := answer["dns"]

		if dns == nil {
			return nil, fmt.Errorf("expected a dns response but got none from  %s", server)
		}
		if _, ok := answer["dns"]; ok && answer["dns"].Answer != nil {
			for _, rr := range answer["dns"].Answer {
				//need to remove duplicates where the dns name is the same and the targets are the same but keep duplicates where the targets are different
				endpoints = p.mergeDNSEndpoints(rr, endpoints)
			}
		}
		if _, ok := answer["weight"]; ok && answer["weight"].Answer != nil {
			for _, rr := range answer["weight"].Answer {
				// these will be unique dns names
				addWeight(rr, endpoints)
			}
		}
		if _, ok := answer["geo"]; ok && answer["geo"].Answer != nil {
			for _, rr := range answer["geo"].Answer {
				if err := p.populateGEO(rr, endpoints); err != nil {
					return endpoints, err
				}
			}
		}
	}

	return endpoints, nil
}

func sanitize(host string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(host, ".")), "."+provider.KuadrantTLD)
}

func (p *CoreDNSProvider) mergeDNSEndpoints(rr dns.RR, endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	ep := &endpoint.Endpoint{
		DNSName:   sanitize(rr.Header().Name),
		Targets:   []string{},
		RecordTTL: endpoint.TTL(rr.Header().Ttl),
	}
	switch rec := rr.(type) {
	case *dns.A:
		ep.RecordType = "A"
		ep.RecordTTL = endpoint.TTL(rec.Header().Ttl)
		ep.Targets = append(ep.Targets, rec.A.String())
	case *dns.CNAME:
		ep.RecordType = "CNAME"
		ep.Targets = append(ep.Targets, sanitize(rec.Target))
	case *dns.TXT:
		ep.RecordType = "TXT"
		ep.Targets = rec.Txt
	default:
		p.logger.Info("not handling record of type %v ", rec)
	}

	alreadyExists := false
	for _, existing := range endpoints {
		if existing.DNSName == ep.DNSName && existing.RecordType == ep.RecordType {
			if ep.RecordType == endpoint.RecordTypeA {
				alreadyExists = true
				existing.Targets = append(existing.Targets, ep.Targets...)
				break
			}
			if slices.Equal(existing.Targets, ep.Targets) {
				alreadyExists = true
				break
			}
		}
	}
	if !alreadyExists {
		endpoints = append(endpoints, ep)
	}

	return endpoints
}

func (p *CoreDNSProvider) populateGEO(rr dns.RR, endpoints []*endpoint.Endpoint) error {
	txt := rr.(*dns.TXT)
	p.logger.Info("populateGEO ", "txt ", txt.Txt)
	if len(txt.Txt) != 3 {
		// return an error we expect 3 pieces of info
		return fmt.Errorf("expected 3 lines in the geo txt record but got %d", len(txt.Txt))
	}
	geo := newGeoData(txt)
	for _, ep := range endpoints {
		for _, target := range ep.Targets {
			p.logger.V(1).Info(fmt.Sprintf("populateGEO looking for geo subdomain %s %s", target, geo.subdomain()))
			//todo this is not matching things like us.
			if strings.HasPrefix(target, geo.subdomain()) {
				p.logger.Info("populateGEO found geo", "subdomain", geo.subdomain())
				if geo.Default {
					ep.SetIdentifier = "default"
				}
				if len(ep.ProviderSpecific) == 0 {
					ep.ProviderSpecific = append(ep.ProviderSpecific, endpoint.ProviderSpecificProperty{
						Name:  "geo-code",
						Value: geo.Code,
					})
				}
			}
		}
	}
	return nil
}

type geoData struct {
	Code      string
	Default   bool
	Continent bool
}

func (g *geoData) subdomain() string {

	return strings.ToLower(g.Code)

}

func newGeoData(txt *dns.TXT) geoData {
	gd := geoData{}
	for _, val := range txt.Txt {
		if strings.HasPrefix(val, TxtGEOCodePrefix) {
			gd.Code = strings.Replace(val, TxtGEOCodePrefix, "", -1)
		}
		if strings.HasPrefix(val, TxtDefaultGEOPrefix) {
			val = strings.Replace(val, TxtDefaultGEOPrefix, "", -1)
			gd.Default = strings.ToLower(strings.TrimSpace(val)) == "true"
		}
		if strings.HasPrefix(val, TxtCodeTypeGEOPrefix) {
			val = strings.Replace(val, TxtCodeTypeGEOPrefix, "", -1)
			if val == "continent" {
				gd.Continent = true
			}
		}
	}
	return gd
}

func addWeight(rr dns.RR, endpoints []*endpoint.Endpoint) {
	txt := rr.(*dns.TXT)

	if len(txt.Txt) == 1 {
		values := strings.Split(txt.Txt[0], ",")
		weight := values[0]
		dnsName := sanitize(values[1])

		for _, ep := range endpoints {
			if sanitize(ep.DNSName) == sanitize(dnsName) {
				ep.ProviderSpecific = []endpoint.ProviderSpecificProperty{
					{
						Name:  "weight",
						Value: weight,
					},
				}
			}
		}
	}
}

func (p *CoreDNSProvider) dnsQuery(hosts []string, nameserver string) (map[string]*dns.Msg, error) {
	p.logger.Info("doing dns query", "hosts", hosts)
	queryType := dns.TypeA
	answers := map[string]*dns.Msg{}
	key := "dns"
	for _, host := range hosts {
		if strings.HasPrefix(host, "g.") {
			queryType = dns.TypeTXT
			key = "geo"
		}
		if strings.HasPrefix(host, "w.") {
			queryType = dns.TypeTXT
			key = "weight"
		}
		if strings.HasPrefix(host, "d.") {
			queryType = dns.TypeTXT
			key = "defaultGeo"
		}

		dnsMsg := new(dns.Msg)
		fqdn := fmt.Sprintf("%s.", host) // Convert to true FQDN with dot at the end
		dnsMsg.SetQuestion(fqdn, queryType)
		msg, err := dns.Exchange(dnsMsg, nameserver)
		if err != nil {
			return answers, fmt.Errorf("%w failed to do dns exchange with nameserver %s ", err, nameserver)
		}
		p.logger.Info("got answer for dns query ", "fqdn", fqdn, "nameserver", nameserver, "answer", msg, "err", err)
		answers[key] = msg

	}
	return answers, nil
}

func (p *CoreDNSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	return fmt.Errorf("core dns does not use a plan")
}

// AdjustEndpoints canonicalizes a set of candidate endpoints.
// It is called with a set of candidate endpoints obtained from the various sources.
// It returns a set modified as required by the provider. The provider is responsible for
// adding, removing, and modifying the ProviderSpecific properties to match
// the endpoints that the provider returns in `Records` so that the change plan will not have
// unnecessary (potentially failing) changes. It may also modify other fields, add, or remove
// Endpoints. It is permitted to modify the supplied endpoints.
func (p *CoreDNSProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}
func (p *CoreDNSProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}
