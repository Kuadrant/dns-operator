package kuadrant

import (
	"context"
	"net"
	"strconv"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/coredns/coredns/plugin/file"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type weightedRR struct {
	dns.RR
	weight int64
}

type geodRR struct {
	dns.RR
	geo string
}

type rrData struct {
	weight *int64
	geo    *string
}

type Zone struct {
	file   *file.Zone
	rrData map[dns.RR]rrData
}

func NewZone(name string) *Zone {
	z := &Zone{
		file.NewZone(name, ""),
		map[dns.RR]rrData{},
	}

	ns := &dns.NS{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeNS, Ttl: ttlSOA, Class: dns.ClassINET},
		Ns: dnsutil.Join("ns1", name),
	}
	z.file.Insert(ns)

	soa := &dns.SOA{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeSOA, Ttl: ttlSOA, Class: dns.ClassINET},
		Mbox:    dnsutil.Join("hostmaster", name),
		Ns:      dnsutil.Join("ns1", name),
		Serial:  12345,
		Refresh: 7200,
		Retry:   1800,
		Expire:  86400,
		Minttl:  ttlSOA,
	}
	z.file.Insert(soa)

	return z
}

func (z *Zone) InsertEndpoint(ep *endpoint.Endpoint) error {
	rrs := []dns.RR{}

	if ep.RecordType == endpoint.RecordTypeA {
		for _, t := range ep.Targets {
			a := &dns.A{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
				A: net.ParseIP(t)}
			rrs = append(rrs, a)
		}
	}

	if ep.RecordType == endpoint.RecordTypeAAAA {
		for _, t := range ep.Targets {
			aaaa := &dns.AAAA{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
				AAAA: net.ParseIP(t)}
			rrs = append(rrs, aaaa)
		}
	}

	if ep.RecordType == endpoint.RecordTypeTXT {
		txt := &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
			Txt: ep.Targets}
		rrs = append(rrs, txt)
	}

	if ep.RecordType == endpoint.RecordTypeCNAME {
		cname := &dns.CNAME{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
			Target: dns.Fqdn(ep.Targets[0])}
		rrs = append(rrs, cname)
	}

	for i, _ := range rrs {
		rrd := rrData{}
		if wProp, wExists := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight); wExists {
			weight, err := strconv.ParseInt(wProp, 10, 64)
			if err != nil {
				weight = 0
			}
			rrd.weight = &weight
		} else if gProp, gExists := ep.GetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode); gExists {
			rrd.geo = &gProp
		}
		z.rrData[rrs[i]] = rrd
		z.file.Insert(rrs[i])
	}

	return nil
}

func (z *Zone) Lookup(ctx context.Context, state request.Request, qname string) ([]dns.RR, []dns.RR, []dns.RR, file.Result) {
	answer, ns, extra, result := z.file.Lookup(ctx, state, qname)
	return z.parseAnswers(ctx, state, qname, answer), ns, extra, result
}

func (z *Zone) RefreshFrom(newZ *Zone) {
	// copy elements we need
	z.file.Lock()
	z.file.Apex = newZ.file.Apex
	z.file.Tree = newZ.file.Tree
	z.rrData = newZ.rrData
	z.file.Unlock()
}

// parseAnswers takes a slice of answers(RRs), groups them by name and reduces each group down to a single answer.
func (z *Zone) parseAnswers(_ context.Context, state request.Request, _ string, answers []dns.RR) []dns.RR {
	var dnsNames []string
	rrSets := map[string][]dns.RR{}

	for _, rr := range answers {
		//Group rrs with the same name but maintain the order of the  answers
		dnsName := rr.Header().Name
		if _, ok := rrSets[dnsName]; !ok {
			dnsNames = append(dnsNames, dnsName)
		}
		rrSets[dnsName] = append(rrSets[dnsName], rr)
	}

	var rrs []dns.RR
	for _, dnsName := range dnsNames {
		if len(rrSets[dnsName]) == 1 {
			// If there is only one answer for the dnsName just return it
			rrs = append(rrs, rrSets[dnsName]...)
			continue
		}
		rrMeta := z.rrData[rrSets[dnsName][0]]
		if rrMeta.geo != nil {
			rrs = append(rrs, z.parseGeoAnswers(state, rrSets[dnsName])...)
		} else if rrMeta.weight != nil {
			rrs = append(rrs, z.parseWeightedAnswers(state, rrSets[dnsName])...)
		} else {
			//Take the first answer in the default case, not geo or weighted
			rrs = append(rrs, rrSets[dnsName][0])
		}
	}

	return rrs
}

// parseWeightedAnswers takes a slice of answers for a dns name and reduces it down to a single answer based on weight.
func (z *Zone) parseWeightedAnswers(state request.Request, wrrs []dns.RR) []dns.RR {
	log.Debugf("parsing weighted answers for %s", state.QName())
	var answer *dns.RR
	var weightedRRs []weightedRR

	for _, r := range wrrs {
		if w := z.rrData[r].weight; w != nil {
			weightedRRs = append(weightedRRs, weightedRR{r, *w})
		}
	}

	if weightedRRs != nil {
		// ToDo calculate answer here!!
		answer = &weightedRRs[0].RR
	}

	if answer == nil {
		answer = &wrrs[0]
	}

	return []dns.RR{*answer}
}

// parseGeoAnswers takes a slice of answers for a dns name and reduces it down to a single answer based on geo.
func (z *Zone) parseGeoAnswers(state request.Request, grrs []dns.RR) []dns.RR {
	log.Debugf("parsing geo answers for %s", state.QName())
	var answer *dns.RR
	var geoRRs []geodRR

	for _, r := range grrs {
		if geo := z.rrData[r].geo; geo != nil {
			geoRRs = append(geoRRs, geodRR{r, *geo})
		}
	}

	if geoRRs != nil {
		// ToDo calculate answer here!!
		answer = &geoRRs[0].RR
	}

	if answer == nil {
		answer = &grrs[0]
	}

	return []dns.RR{*answer}
}
