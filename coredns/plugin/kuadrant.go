package kuadrant

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/file"
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

type Zones struct {
	Z     map[string]*Zone // A map mapping zone (origin) to the Zone's data
	Names []string         // All the keys from the map Z as a string slice.
}

type Kuadrant struct {
	Next       plugin.Handler
	Controller *KubeController
	Zones
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
	log.Debugf("incoming query %s", state.QName())

	qname := state.QName()
	zone := plugin.Zones(k.Zones.Names).Matches(qname)
	if zone == "" {
		log.Debugf("request %s has not matched any zones %v", qname, k.Zones)
		return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
	}

	z, ok := k.Zones.Z[zone]
	if !ok || z == nil {
		return dns.RcodeServerFailure, nil
	}

	// If transfer is not loaded, we'll see these, answer with refused (no transfer allowed).
	if state.QType() == dns.TypeAXFR || state.QType() == dns.TypeIXFR {
		return dns.RcodeRefused, nil
	}

	z.file.RLock()
	exp := z.file.Expired
	z.file.RUnlock()
	if exp {
		log.Errorf("Zone %s is expired", zone)
		return dns.RcodeServerFailure, nil
	}

	answer, ns, extra, result := z.Lookup(ctx, state, qname)

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer, m.Ns, m.Extra = answer, ns, extra

	switch result {
	case file.Success:
	case file.NoData:
	case file.NameError:
		m.Rcode = dns.RcodeNameError
	case file.Delegation:
		m.Authoritative = false
	case file.ServerFailure:
		// If the result is SERVFAIL and the answer is non-empty, then the SERVFAIL came from an
		// external CNAME lookup and the answer contains the CNAME with no target record. We should
		// write the CNAME record to the client instead of sending an empty SERVFAIL response.
		if len(m.Answer) == 0 {
			return dns.RcodeServerFailure, nil
		}
		//  The rcode in the response should be the rcode received from the target lookup. RFC 6604 section 3
		m.Rcode = dns.RcodeServerFailure
	}

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (k *Kuadrant) Name() string {
	return pluginName
}

// Strips the closing dot unless it's "."
func stripClosingDot(s string) string {
	if len(s) > 1 {
		return strings.TrimSuffix(s, ".")
	}
	return s
}
