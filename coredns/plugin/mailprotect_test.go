package kuadrant

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
)

type silentRW struct{}

func (silentRW) LocalAddr() net.Addr       { return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53} }
func (silentRW) RemoteAddr() net.Addr      { return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53} }
func (silentRW) WriteMsg(*dns.Msg) error   { return nil }
func (silentRW) Write([]byte) (int, error) { return 0, nil }
func (silentRW) Close() error              { return nil }
func (silentRW) TsigStatus() error         { return nil }
func (silentRW) TsigTimersOnly(bool)       {}
func (silentRW) Hijack()                   {}

func lookupRRs(t *testing.T, z *Zone, qname string, qtype uint16) ([]dns.RR, Result) {
	t.Helper()
	w := dnstest.NewRecorder(silentRW{})
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	state := request.Request{W: w, Req: m}
	ans, _, _, res := z.Lookup(context.Background(), state, dns.Fqdn(qname))
	return ans, res
}

func txtStrings(rrs []dns.RR) []string {
	var out []string
	for _, rr := range rrs {
		if txt, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(txt.Txt, ""))
		}
	}
	return out
}

func mustLookup(t *testing.T, z *Zone, qname string, qtype uint16) []dns.RR {
	t.Helper()
	ans, res := lookupRRs(t, z, qname, qtype)
	require.Equal(t, Success, res, "lookup %s %s", qname, dns.TypeToString[qtype])
	return ans
}

func TestParse_NullMailFlag(t *testing.T) {
	tests := []struct {
		name          string
		configuration string
		wantErr       bool
		wantNullMail  bool
	}{
		{name: "omitted defaults to off", configuration: `kuadrant example.com`, wantNullMail: false},
		{name: "empty block defaults to off", configuration: "kuadrant example.com {\n}", wantNullMail: false},
		{name: "bare nullmail enables protection", configuration: "kuadrant example.com {\n nullmail\n}", wantNullMail: true},
		{name: "nullmail with rname and kubeconfig", configuration: "kuadrant example.com {\n kubeconfig foo.kubeconfig\n rname admin@example.com\n nullmail\n}", wantNullMail: true},
		{name: "nullmail rejects extra args", configuration: "kuadrant example.com {\n nullmail on\n}", wantErr: true},
		{name: "unknown property still rejected", configuration: "kuadrant example.com {\n foo bar\n}", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", tt.configuration)
			k, err := parse(c)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNullMail, k.NullMail)
			z, ok := k.Zones.Z["example.com."]
			require.True(t, ok)
			assert.Equal(t, tt.wantNullMail, z.nullmail)
		})
	}
}

func TestMailProtection_DisabledByDefault(t *testing.T) {
	z := NewZone("example.com", "")
	applyMailProtection(z)
	ans, res := lookupRRs(t, z, "example.com", dns.TypeTXT)
	assert.Empty(t, txtStrings(ans))
	assert.NotEqual(t, Success, res)
}

func TestMailProtection_PublishesThreeRecords(t *testing.T) {
	z := NewZone("example.com", "")
	z.nullmail = true
	applyMailProtection(z)
	assert.Equal(t, []string{txtSPFDenyAll}, txtStrings(mustLookup(t, z, "example.com", dns.TypeTXT)))
	assert.Equal(t, []string{txtDMARCReject}, txtStrings(mustLookup(t, z, "_dmarc.example.com", dns.TypeTXT)))
	assert.Equal(t, []string{txtDKIMEmpty}, txtStrings(mustLookup(t, z, "*._domainkey.example.com", dns.TypeTXT)))
}

func TestMailProtection_WildcardDKIM(t *testing.T) {
	z := NewZone("example.com", "")
	z.nullmail = true
	applyMailProtection(z)
	assert.Equal(t, []string{txtDKIMEmpty}, txtStrings(mustLookup(t, z, "foo._domainkey.example.com", dns.TypeTXT)))
	assert.Equal(t, []string{txtDKIMEmpty}, txtStrings(mustLookup(t, z, "selector1._domainkey.example.com", dns.TypeTXT)))
}

func TestMailProtection_ReplacesCaseInsensitiveSPF(t *testing.T) {
	z := NewZone("example.com", "")
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"google-site-verification=abc123"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"V=SPF1 include:_spf.google.com ~all"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	z.nullmail = true
	applyMailProtection(z)
	got := txtStrings(mustLookup(t, z, "example.com", dns.TypeTXT))
	assert.Contains(t, got, "google-site-verification=abc123")
	assert.Contains(t, got, txtSPFDenyAll)
	assert.NotContains(t, got, "V=SPF1 include:_spf.google.com ~all")
	assert.NotContains(t, got, "v=spf1 include:_spf.google.com ~all")
}

func TestMailProtection_KeepsVerificationAndReplacesSPF(t *testing.T) {
	z := NewZone("example.com", "")
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"google-site-verification=abc123"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"v=spf1 include:_spf.google.com ~all"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	z.nullmail = true
	applyMailProtection(z)
	got := txtStrings(mustLookup(t, z, "example.com", dns.TypeTXT))
	assert.Contains(t, got, "google-site-verification=abc123")
	assert.Contains(t, got, txtSPFDenyAll)
	assert.NotContains(t, got, "v=spf1 include:_spf.google.com ~all")
}

func TestMailProtection_OverwritesDMARC(t *testing.T) {
	z := NewZone("example.com", "")
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "_dmarc.example.com", Targets: []string{"v=DMARC1; p=none"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	z.nullmail = true
	applyMailProtection(z)
	assert.Equal(t, []string{txtDMARCReject}, txtStrings(mustLookup(t, z, "_dmarc.example.com", dns.TypeTXT)))
}

func TestMailProtection_FlagOffLeavesUserRecords(t *testing.T) {
	z := NewZone("example.com", "")
	require.NoError(t, z.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"v=spf1 include:_spf.google.com ~all"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	applyMailProtection(z)
	assert.Equal(t, []string{"v=spf1 include:_spf.google.com ~all"}, txtStrings(mustLookup(t, z, "example.com", dns.TypeTXT)))
}

func TestMailProtection_DropsMX(t *testing.T) {
	z := NewZone("example.com", "")
	require.NoError(t, z.Insert(&dns.MX{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 60}, Preference: 10, Mx: "mail.example.com."}))
	z.nullmail = true
	applyMailProtection(z)
	ans, _ := lookupRRs(t, z, "example.com", dns.TypeMX)
	assert.Empty(t, ans)
}

func TestMailProtection_SimulatedRefreshAfterCRs(t *testing.T) {
	live := NewZone("example.com", "admin@example.com")
	live.nullmail = true
	newZ := NewZone("example.com", live.rname)
	newZ.nullmail = live.nullmail
	require.NoError(t, newZ.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"google-site-verification=abc123"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	require.NoError(t, newZ.InsertEndpoint(&endpoint.Endpoint{DNSName: "example.com", Targets: []string{"v=spf1 include:_spf.google.com ~all"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	require.NoError(t, newZ.InsertEndpoint(&endpoint.Endpoint{DNSName: "_dmarc.example.com", Targets: []string{"v=DMARC1; p=none"}, RecordType: endpoint.RecordTypeTXT, RecordTTL: 60}))
	require.NoError(t, newZ.InsertEndpoint(&endpoint.Endpoint{DNSName: "api.example.com", Targets: []string{"1.1.1.1"}, RecordType: endpoint.RecordTypeA, RecordTTL: 60}))
	applyMailProtection(newZ)
	live.RefreshFrom(newZ)
	got := txtStrings(mustLookup(t, live, "example.com", dns.TypeTXT))
	assert.Contains(t, got, "google-site-verification=abc123")
	assert.Contains(t, got, txtSPFDenyAll)
	assert.NotContains(t, got, "v=spf1 include:_spf.google.com ~all")
	assert.Equal(t, []string{txtDMARCReject}, txtStrings(mustLookup(t, live, "_dmarc.example.com", dns.TypeTXT)))
	ans, res := lookupRRs(t, live, "api.example.com", dns.TypeA)
	require.Equal(t, Success, res)
	require.Equal(t, "1.1.1.1", ans[0].(*dns.A).A.String())
}
