package zone

import (
	"context"
	"net"
	"strings"

	"github.com/miekg/dns"
)

type Zone interface {
	Name() string
	Match(qname string) bool
	Resolve(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, clientGroup string) (bool, error)
	GetRecords() map[string]DNSRecord
	GetUpstream() *UpstreamConfig
}

type DNSRecord struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   uint32 `json:"ttl,omitempty"`
}

type UpstreamConfig struct {
	Upstreams []string `json:"upstreams,omitempty"`
	Timeout   string   `json:"timeout,omitempty"`
	Protocol  string   `json:"protocol,omitempty"`
}

type ZoneConfig struct {
	Type     string               `json:"type"`
	Zone     string               `json:"zone"`
	Records  map[string]DNSRecord `json:"records,omitempty"`
	Upstream *UpstreamConfig      `json:"upstream,omitempty"`
}

func normalizeQName(qname string) string {
	qname = strings.ToLower(qname)
	if !strings.HasSuffix(qname, ".") {
		qname += "."
	}
	return qname
}

// makeAbsolute converts a relative name to FQDN within a zone
// If name is already absolute (ends with .), returns it normalized
// If name is relative, appends the zone name
// Special case: "@" represents the zone apex
func makeAbsolute(name, zoneName string) string {
	name = strings.TrimSpace(name)
	zoneName = normalizeQName(zoneName)

	// Handle zone apex
	if name == "@" || name == "" {
		return zoneName
	}

	// If already absolute, just normalize
	if strings.HasSuffix(name, ".") {
		return normalizeQName(name)
	}

	// Make relative name absolute by appending zone
	return normalizeQName(name + "." + zoneName)
}

func isSubdomain(qname, zone string) bool {
	qname = normalizeQName(qname)
	zone = normalizeQName(zone)

	if qname == zone {
		return true
	}

	return strings.HasSuffix(qname, "."+zone)
}

func createDNSResponse(r *dns.Msg, record DNSRecord, qname string) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(r)

	ttl := record.TTL
	if ttl == 0 {
		ttl = 300
	}

	var rr dns.RR

	switch strings.ToUpper(record.Type) {
	case "A":
		ip := net.ParseIP(record.Value)
		if ip == nil || ip.To4() == nil {
			m.SetRcode(r, dns.RcodeServerFailure)
			return m
		}
		rr = &dns.A{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   ip.To4(),
		}
	case "AAAA":
		ip := net.ParseIP(record.Value)
		if ip == nil || ip.To16() == nil {
			m.SetRcode(r, dns.RcodeServerFailure)
			return m
		}
		rr = &dns.AAAA{
			Hdr:  dns.RR_Header{Name: qname, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
			AAAA: ip.To16(),
		}
	case "CNAME":
		rr = &dns.CNAME{
			Hdr:    dns.RR_Header{Name: qname, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
			Target: normalizeQName(record.Value),
		}
	case "TXT":
		rr = &dns.TXT{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
			Txt: []string{record.Value},
		}
	default:
		m.SetRcode(r, dns.RcodeServerFailure)
		return m
	}

	if rr != nil {
		m.Answer = append(m.Answer, rr)
	} else {
		m.SetRcode(r, dns.RcodeServerFailure)
	}

	return m
}
