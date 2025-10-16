package kuadrant

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/transfer"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

var (
	ttlSOA = uint32(60)
)

type Zones struct {
	Z     map[string]*Zone // A map mapping zone (origin) to the Zone's data
	Names []string         // All the keys from the map Z as a string slice.
}

type Kuadrant struct {
	Next       plugin.Handler
	Controller *KubeController
	Zones
	ConfigFile    string
	ConfigContext string
}

func newKuadrant() *Kuadrant {
	return &Kuadrant{}
}

// ServeDNS implements the plugin.Handle interface.
// Based on the file plugin ServeDNS implementation that the main zone lookup adapts https://github.com/coredns/coredns/blob/master/plugin/file/file.go
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

	z.RLock()
	exp := z.Expired
	z.RUnlock()
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
	case Success:
	case NoData:
	case NameError:
		m.Rcode = dns.RcodeNameError
	case Delegation:
		m.Authoritative = false
	case ServerFailure:
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

// Transfer implements the transfer.Transfer interface.
func (k *Kuadrant) Transfer(zone string, serial uint32) (<-chan []dns.RR, error) {
	z, ok := k.Z[zone]
	if !ok || z == nil {
		return nil, transfer.ErrNotAuthoritative
	}
	return z.Transfer(serial)
}

// Strips the closing dot unless it's "."
func stripClosingDot(s string) string {
	if len(s) > 1 {
		return strings.TrimSuffix(s, ".")
	}
	return s
}
