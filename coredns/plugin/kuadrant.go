package kuadrant

import (
	"context"
	"net"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

var (
	ttlDefault        = uint32(60)
	ttlSOA            = uint32(60)
	defaultApex       = "dns1"
	defaultHostmaster = "hostmaster"
	defaultSecondNS   = ""
)

type Kuadrant struct {
	Next          plugin.Handler
	Controller    *KubeController
	Zones         []string
	Filter        string
	ConfigFile    string
	ConfigContext string
	ttlLow        uint32
	ttlSOA        uint32
	apex          string
	hostmaster    string
	secondNS      string
}

func newKuadrant() *Kuadrant {
	return &Kuadrant{
		ttlLow:     ttlDefault,
		ttlSOA:     ttlSOA,
		apex:       defaultApex,
		secondNS:   defaultSecondNS,
		hostmaster: defaultHostmaster,
	}
}

func (k *Kuadrant) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	log.Debugf("Incoming query %s", state.QName())

	qname := state.QName()
	zone := plugin.Zones(k.Zones).Matches(qname)
	if zone == "" {
		log.Debugf("Request %s has not matched any zones %v", qname, k.Zones)
		return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
	}
	zone = qname[len(qname)-len(zone):] // maintain case of original query
	state.Zone = zone
	log.Debugf("matching zone %s", zone)

	// Computing keys to look up in cache
	indexKey := stripClosingDot(state.QName())

	log.Debugf("Computed Index Keys %v", indexKey)

	for _, z := range k.Zones {
		if state.Name() == z { // apex query
			ret := k.serveApex(state)
			return ret, nil
		}
		//ToDo Deal with sub apex?
		//if dns.IsSubDomain(gw.opts.apex+"."+z, state.Name()) {
		//	//dns subdomain test for ns. and dns. queries
		//gw.serveSubApex(state)
		//return 0, nil
		//}
	}

	_, ep := Resources.DNSRecord.Lookup(indexKey)

	m := new(dns.Msg)
	m.SetReply(state.Req)

	if ep == nil || len(ep.Targets) == 0 {
		m.Rcode = dns.RcodeNameError
		m.Ns = []dns.RR{k.soa(state)}
		if err := w.WriteMsg(m); err != nil {
			log.Errorf("Failed to send a response: %s", err)
		}
		return 0, nil
	}

	log.Debugf("Computed response addresses %v", ep.Targets)

	switch state.QType() {
	case dns.TypeA:
		m.Answer = k.A(state, targetToIP(ep.Targets), ep.RecordTTL)
	case dns.TypeTXT:
		m.Answer = k.TXT(state, ep.Targets, ep.RecordTTL)
	default:
		m.Ns = []dns.RR{k.soa(state)}
	}

	if len(m.Answer) == 0 {
		m.Ns = []dns.RR{k.soa(state)}
	}

	if err := w.WriteMsg(m); err != nil {
		log.Errorf("Failed to send a response: %s", err)
	}

	return dns.RcodeSuccess, nil
}

func (k *Kuadrant) Name() string {
	return pluginName
}

// serveApex serves request that hit the zone' apex. A reply is written back to the client.
func (k *Kuadrant) serveApex(state request.Request) int {
	m := new(dns.Msg)
	m.SetReply(state.Req)
	switch state.QType() {
	case dns.TypeSOA:
		m.Answer = []dns.RR{k.soa(state)}
		m.Ns = []dns.RR{k.ns(state)} // This fixes some of the picky DNS resolvers
	case dns.TypeNS:
		m.Answer = []dns.RR{k.ns(state)}

		//ToDo Add the ns addresses here
		//addr := gw.externalAddrFunc(state)
		//for _, rr := range addr {
		//	rr.Header().Ttl = gw.opts.ttlHigh
		//	rr.Header().Name = dnsutil.Join("ns1", k.apex, state.QName())
		//	m.Extra = append(m.Extra, rr)
		//}
	default:
		m.Ns = []dns.RR{k.soa(state)}
	}

	if err := state.W.WriteMsg(m); err != nil {
		log.Errorf("Failed to send a response: %s", err)
	}
	return 0
}

func (k *Kuadrant) soa(state request.Request) *dns.SOA {
	header := dns.RR_Header{Name: state.Zone, Rrtype: dns.TypeSOA, Ttl: k.ttlSOA, Class: dns.ClassINET}

	soa := &dns.SOA{Hdr: header,
		Mbox:    dnsutil.Join(k.hostmaster, k.apex, state.Zone),
		Ns:      dnsutil.Join(k.apex, state.Zone),
		Serial:  12345,
		Refresh: 7200,
		Retry:   1800,
		Expire:  86400,
		Minttl:  k.ttlSOA,
	}
	return soa
}

func (k *Kuadrant) ns(state request.Request) *dns.NS {
	header := dns.RR_Header{Name: state.Zone, Rrtype: dns.TypeNS, Ttl: k.ttlSOA, Class: dns.ClassINET}
	ns := &dns.NS{Hdr: header, Ns: dnsutil.Join("ns1", k.apex, state.Zone)}
	return ns
}

// A generates dns.RR for A record
func (k *Kuadrant) A(state request.Request, results []net.IP, ttl endpoint.TTL) (records []dns.RR) {
	dup := make(map[string]struct{})
	if !ttl.IsConfigured() {
		ttl = endpoint.TTL(k.ttlLow)
	}
	for _, result := range results {
		if _, ok := dup[result.String()]; !ok {
			dup[result.String()] = struct{}{}
			records = append(records, &dns.A{Hdr: dns.RR_Header{Name: state.Name(), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(ttl)}, A: result})
		}
	}
	return records
}

// TXT generates dns.RR for TXT record
func (k *Kuadrant) TXT(state request.Request, results []string, ttl endpoint.TTL) (records []dns.RR) {
	if !ttl.IsConfigured() {
		ttl = endpoint.TTL(k.ttlLow)
	}
	return append(records, &dns.TXT{Hdr: dns.RR_Header{Name: state.Name(), Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: uint32(ttl)}, Txt: results})
}

// Strips the closing dot unless it's "."
func stripClosingDot(s string) string {
	if len(s) > 1 {
		return strings.TrimSuffix(s, ".")
	}
	return s
}

func targetToIP(targets []string) (ips []net.IP) {
	for _, ip := range targets {
		ips = append(ips, net.ParseIP(ip))
	}
	return
}
