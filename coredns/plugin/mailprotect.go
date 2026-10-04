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

// applyMailProtection publishes deny-all SPF/DKIM/DMARC when the zone's
// nullmail flag is on, and removes records that would bypass those policies.
//
// Enforcement is zone-wide, not three hardcoded names. An exact DKIM selector
// beats *._domainkey, a _dmarc record at a subdomain beats the organisational
// sp= tag, a permissive SPF at a subdomain can align and pass DMARC, and a
// CNAME at any of those names is followed before the TXT is read. Each refresh
// walks the merged tree and overwrites those names. Non-SPF TXT elsewhere is
// left alone. No-op when the flag is off.
func applyMailProtection(z *Zone) {
	if z == nil || !z.nullmail {
		return
	}
	origin := dns.Fqdn(z.origin)
	if origin == "" || origin == "." {
		return
	}

	spfAt := map[string]struct{}{origin: {}}
	mxAt := map[string]struct{}{origin: {}}
	dmarcAt := map[string]struct{}{dns.Fqdn("_dmarc." + origin): {}}
	dkimAt := map[string]struct{}{dns.Fqdn("*._domainkey." + origin): {}}

	for _, name := range zoneNames(z) {
		name = dns.Fqdn(name)
		if !dns.IsSubDomain(origin, name) {
			continue
		}
		switch {
		case isDMARCName(name, origin):
			dmarcAt[name] = struct{}{}
		case isDKIMName(name, origin):
			dkimAt[name] = struct{}{}
		default:
			if hasSPF(z, name) {
				spfAt[name] = struct{}{}
			}
			if len(recordsOfType(z, name, dns.TypeMX)) > 0 {
				mxAt[name] = struct{}{}
			}
		}
	}

	for name := range dmarcAt {
		replacePolicy(z, name, txtDMARCReject)
	}
	for name := range dkimAt {
		replacePolicy(z, name, txtDKIMEmpty)
	}
	for name := range spfAt {
		enforceSPF(z, name)
	}
	for name := range mxAt {
		replaceNullMX(z, name)
	}
}

func zoneNames(z *Zone) []string {
	if z == nil || z.Tree == nil {
		return nil
	}
	z.RLock()
	defer z.RUnlock()
	elems := z.Tree.All()
	names := make([]string, 0, len(elems))
	for _, elem := range elems {
		if elem == nil || elem.Name() == "" {
			continue
		}
		names = append(names, elem.Name())
	}
	return names
}

func isDMARCName(name, origin string) bool {
	name = strings.ToLower(dns.Fqdn(name))
	origin = strings.ToLower(dns.Fqdn(origin))
	if name == origin || !dns.IsSubDomain(origin, name) {
		return false
	}
	labels := dns.SplitDomainName(name)
	return len(labels) > 0 && labels[0] == "_dmarc"
}

func isDKIMName(name, origin string) bool {
	name = strings.ToLower(dns.Fqdn(name))
	origin = strings.ToLower(dns.Fqdn(origin))
	if name == origin || !dns.IsSubDomain(origin, name) {
		return false
	}
	if name == "_domainkey."+origin {
		return true
	}
	return strings.HasSuffix(name, "._domainkey."+origin)
}

func replacePolicy(z *Zone, name, want string) {
	name = dns.Fqdn(name)
	for _, rr := range recordsOfType(z, name, dns.TypeTXT) {
		txt, ok := rr.(*dns.TXT)
		if !ok {
			continue
		}
		if txtValue(txt) != want {
			log.Warningf("nullmail: replacing TXT at %s (%q) with %q", name, txtValue(txt), want)
		}
	}
	for _, qtype := range []uint16{dns.TypeCNAME, dns.TypeDNAME, dns.TypeNS} {
		for _, rr := range recordsOfType(z, name, qtype) {
			log.Warningf("nullmail: dropping %s at %s so it cannot bypass mail policy", dns.TypeToString[qtype], name)
			z.Delete(rr)
			break
		}
	}
	existing := recordsOfType(z, name, dns.TypeTXT)
	if len(existing) > 0 {
		z.Delete(existing[0])
	}
	_ = z.Insert(newTXT(name, want))
}

func replaceNullMX(z *Zone, name string) {
	name = dns.Fqdn(name)
	existing := recordsOfType(z, name, dns.TypeMX)
	alreadyNull := len(existing) == 1 && isNullMX(existing[0])
	if len(existing) > 0 && !alreadyNull {
		for _, rr := range existing {
			log.Warningf("nullmail: dropping MX %s from zone %s", rr.String(), z.origin)
		}
		z.Delete(existing[0])
	}
	if !alreadyNull {
		_ = z.Insert(&dns.MX{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeMX,
				Class:  dns.ClassINET,
				Ttl:    mailProtectTTL,
			},
			Preference: 0,
			Mx:         ".",
		})
	}
}

func isNullMX(rr dns.RR) bool {
	mx, ok := rr.(*dns.MX)
	if !ok {
		return false
	}
	return mx.Preference == 0 && (mx.Mx == "." || mx.Mx == "")
}

func hasSPF(z *Zone, name string) bool {
	for _, rr := range recordsOfType(z, name, dns.TypeTXT) {
		txt, ok := rr.(*dns.TXT)
		if ok && isSPF(txt) {
			return true
		}
	}
	return false
}

func enforceSPF(z *Zone, name string) {
	name = dns.Fqdn(name)
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
