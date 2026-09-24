package kuadrant

import (
	"strings"

	"github.com/miekg/dns"
)

// Deny-all mail-policy records from issue #575.
const (
	txtSPFDenyAll   = "v=spf1 -all"
	txtDKIMEmpty    = "v=DKIM1; p="
	txtDMARCReject  = "v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s"
	spfRecordPrefix = "v=spf1"
	mailProtectTTL  = 60
)

// applyMailProtection publishes the deny-all SPF/DKIM/DMARC records when the
// zone's nullmail flag is on. User mail-policy records are replaced; non-SPF
// apex TXT (site-verification) is left alone. MX records in the zone are
// removed. No-op when the flag is off.
func applyMailProtection(z *Zone) {
	if z == nil || !z.nullmail {
		return
	}

	origin := z.origin
	if origin == "" {
		return
	}

	dropMX(z, origin)
	enforceSPF(z, origin)
	enforceTXT(z, "_dmarc."+origin, txtDMARCReject)
	enforceTXT(z, "*._domainkey."+origin, txtDKIMEmpty)
}

func dropMX(z *Zone, name string) {
	existing := recordsOfType(z, name, dns.TypeMX)
	if len(existing) == 0 {
		return
	}
	for _, rr := range existing {
		log.Warningf("nullmail: dropping MX %s from zone %s", rr.String(), z.origin)
	}
	// Tree.Delete removes every RR of that type at the name, not just this RR.
	z.Delete(existing[0])
}

func enforceSPF(z *Zone, name string) {
	existing := recordsOfType(z, name, dns.TypeTXT)
	var keep []dns.RR
	for _, rr := range existing {
		txt, ok := rr.(*dns.TXT)
		if !ok {
			keep = append(keep, dns.Copy(rr))
			continue
		}
		if isSPF(txt) {
			if txtValue(txt) != txtSPFDenyAll {
				log.Warningf("nullmail: replacing SPF %q at %s with %q", txtValue(txt), name, txtSPFDenyAll)
			}
			continue
		}
		keep = append(keep, dns.Copy(rr))
	}
	if len(existing) > 0 {
		// Wipes the entire apex TXT RRset; non-SPF strings are re-inserted below.
		z.Delete(existing[0])
	}
	for _, rr := range keep {
		_ = z.Insert(rr)
	}
	_ = z.Insert(newTXT(name, txtSPFDenyAll))
}

func enforceTXT(z *Zone, name, want string) {
	existing := recordsOfType(z, name, dns.TypeTXT)
	for _, rr := range existing {
		txt, ok := rr.(*dns.TXT)
		if !ok {
			continue
		}
		if txtValue(txt) != want {
			log.Warningf("nullmail: replacing TXT at %s (%q) with %q", name, txtValue(txt), want)
		}
	}
	if len(existing) > 0 {
		z.Delete(existing[0])
	}
	_ = z.Insert(newTXT(name, want))
}

func newTXT(name, value string) *dns.TXT {
	return &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    mailProtectTTL,
		},
		Txt: []string{value},
	}
}

func isSPF(txt *dns.TXT) bool {
	v := strings.ToLower(strings.TrimSpace(txtValue(txt)))
	return v == spfRecordPrefix || strings.HasPrefix(v, spfRecordPrefix+" ")
}

func txtValue(txt *dns.TXT) string {
	if txt == nil {
		return ""
	}
	return strings.Join(txt.Txt, "")
}

func recordsOfType(z *Zone, name string, qtype uint16) []dns.RR {
	name = dns.Fqdn(name)
	z.RLock()
	defer z.RUnlock()

	var out []dns.RR
	if z.Tree != nil {
		if elem, ok := z.Tree.Search(name); ok && elem != nil {
			for _, rr := range elem.Type(qtype) {
				out = append(out, dns.Copy(rr))
			}
		}
	}
	return out
}
