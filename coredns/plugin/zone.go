package kuadrant

import (
	"context"
	"net"
	"strconv"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/coredns/coredns/plugin/file"
	"github.com/coredns/coredns/plugin/metadata"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

const continentGEOPrefix = "GEO-"

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

	z.file.RRResolver = func(ctx context.Context, state request.Request, rrs []dns.RR) dns.RR {
		log.Debugf("resolving %s in zone %s", rrs[0].Header().Name, name)
		rrMeta := z.rrData[rrs[0]]
		if rrMeta.geo != nil {
			rrs = z.parseGeoAnswers(ctx, state, rrs)
		} else if rrMeta.weight != nil {
			rrs = z.parseWeightedAnswers(ctx, state, rrs)
		} else {
			//Take the first answer in the default case, not geo or weighted
			rrs = []dns.RR{rrs[0]}
		}
		return rrs[0]
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

	if ep.RecordType == endpoint.RecordTypeNS {
		for _, t := range ep.Targets {
			ns := &dns.NS{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
				Ns: dns.Fqdn(t)}
			rrs = append(rrs, ns)
		}
	}

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
	return z.file.Lookup(ctx, state, qname)
}

func (z *Zone) RefreshFrom(newZ *Zone) {
	// copy elements we need
	z.file.Lock()
	z.file.Apex = newZ.file.Apex
	z.file.Tree = newZ.file.Tree
	z.rrData = newZ.rrData
	z.file.Unlock()
}

// parseWeightedAnswers takes a slice of answers for a dns name and reduces it down to a single answer based on weight.
func (z *Zone) parseWeightedAnswers(_ context.Context, _ request.Request, wrrs []dns.RR) []dns.RR {
	log.Debugf("parsing weighted answers for %s", wrrs[0].Header().Name)
	var answer *dns.RR
	var weightedRRs []weightedRR

	for _, r := range wrrs {
		if w := z.rrData[r].weight; w != nil {
			weightedRRs = append(weightedRRs, weightedRR{r, *w})
		}
	}

	if weightedRRs != nil {
		wRRSet := newWeightedRRSet(weightedRRs)
		answer = wRRSet.getTopRR()
	}

	if answer == nil {
		answer = &wrrs[0]
	}

	return []dns.RR{*answer}
}

// parseGeoAnswers takes a slice of answers for a dns name and reduces it down to a single answer based on geo.
func (z *Zone) parseGeoAnswers(ctx context.Context, request request.Request, grrs []dns.RR) []dns.RR {
	log.Debugf("parsing geo answers for %s", grrs[0].Header().Name)
	var answer *dns.RR
	var geoRRs []geodRR

	roundRobinShuffle(grrs)

	for _, r := range grrs {
		if geo := z.rrData[r].geo; geo != nil {
			geoRRs = append(geoRRs, geodRR{r, *geo})
		}
	}

	if geoRRs != nil {
		geoCountryCode := metadata.ValueFunc(ctx, "geoip/country/code")
		geoContinetCode := metadata.ValueFunc(ctx, "geoip/continent/code")
		if geoCountryCode != nil && geoContinetCode != nil {
			for _, geoRR := range geoRRs {
				recordGeoCode := geoRR.geo
				sourceGeoCode := geoCountryCode()
				if strings.HasPrefix(recordGeoCode, continentGEOPrefix) {
					recordGeoCode = strings.TrimPrefix(recordGeoCode, continentGEOPrefix)
					sourceGeoCode = geoContinetCode()
				}
				if recordGeoCode == sourceGeoCode {
					answer = &geoRR.RR
				}
			}
		} else {
			log.Debugf("no geo metadata available for %s", request.IP())
		}
	}

	if answer == nil {
		if geoRRs != nil {
			answer = &geoRRs[0].RR
		}
		answer = &grrs[0]
	}

	return []dns.RR{*answer}
}

// Taken from https://github.com/coredns/coredns/blob/master/plugin/loadbalance/loadbalance.go
func roundRobinShuffle(records []dns.RR) {
	switch l := len(records); l {
	case 0, 1:
		break
	case 2:
		if dns.Id()%2 == 0 {
			records[0], records[1] = records[1], records[0]
		}
	default:
		for j := 0; j < l; j++ {
			p := j + (int(dns.Id()) % (l - j))
			if j == p {
				continue
			}
			records[j], records[p] = records[p], records[j]
		}
	}
}
