package zone

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"
)

type ForwardZone struct {
	zoneName       string
	records        map[string]DNSRecord
	upstreamConfig *UpstreamConfig
	upstreamClient *dns.Client
	logger         *slog.Logger
}

func NewForwardZone(zoneName string, records map[string]DNSRecord, upstream *UpstreamConfig) *ForwardZone {
	fz := &ForwardZone{
		zoneName:       normalizeQName(zoneName),
		records:        make(map[string]DNSRecord),
		upstreamConfig: upstream,
	}

	for name, record := range records {
		fz.records[normalizeQName(name)] = record
	}

	fz.setupUpstreamClient()

	return fz
}

func (fz *ForwardZone) setupUpstreamClient() {
	if fz.upstreamConfig == nil {
		return
	}

	timeout := 5 * time.Second
	if fz.upstreamConfig.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(fz.upstreamConfig.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	protocol := "udp"
	if fz.upstreamConfig.Protocol != "" {
		protocol = fz.upstreamConfig.Protocol
	}

	fz.upstreamClient = &dns.Client{
		Net:     protocol,
		Timeout: timeout,
	}
}

func (fz *ForwardZone) Name() string {
	return fz.zoneName
}

func (fz *ForwardZone) Match(qname string) bool {
	return isSubdomain(qname, fz.zoneName)
}

func (fz *ForwardZone) GetRecords() map[string]DNSRecord {
	result := make(map[string]DNSRecord)
	for name, record := range fz.records {
		result[name] = record
	}
	return result
}

func (fz *ForwardZone) GetUpstream() *UpstreamConfig {
	return fz.upstreamConfig
}

func (fz *ForwardZone) SetLogger(logger *slog.Logger) {
	fz.logger = logger
}

func (fz *ForwardZone) Resolve(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, clientGroup string) (bool, error) {
	if len(r.Question) == 0 {
		return false, fmt.Errorf("no question in DNS request")
	}

	question := r.Question[0]
	qname := normalizeQName(question.Name)
	qtype := question.Qtype

	if fz.logger != nil {
		fz.logger.Debug("forward zone resolving query",
			"zone", fz.zoneName,
			"qname", qname,
			"qtype", dns.TypeToString[qtype],
			"client_group", clientGroup)
	}

	if !fz.Match(qname) {
		return false, nil
	}

	if record, exists := fz.records[qname]; exists && fz.matchesQType(record, qtype) {
		if fz.logger != nil {
			fz.logger.Debug("found local record",
				"zone", fz.zoneName,
				"qname", qname,
				"record_type", record.Type,
				"record_value", record.Value)
		}

		response := createDNSResponse(r, record, qname)
		return true, w.WriteMsg(response)
	}

	return fz.forwardToUpstream(ctx, w, r)
}

func (fz *ForwardZone) matchesQType(record DNSRecord, qtype uint16) bool {
	switch qtype {
	case dns.TypeA:
		return record.Type == "A"
	case dns.TypeAAAA:
		return record.Type == "AAAA"
	case dns.TypeCNAME:
		return record.Type == "CNAME"
	case dns.TypeTXT:
		return record.Type == "TXT"
	case dns.TypeANY:
		return true
	default:
		return false
	}
}

func (fz *ForwardZone) forwardToUpstream(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (bool, error) {
	if fz.upstreamConfig == nil || len(fz.upstreamConfig.Upstreams) == 0 {
		return false, nil
	}

	if fz.logger != nil {
		fz.logger.Debug("forwarding to upstream",
			"zone", fz.zoneName,
			"upstreams", fz.upstreamConfig.Upstreams)
	}

	for _, upstream := range fz.upstreamConfig.Upstreams {
		if _, _, err := net.SplitHostPort(upstream); err != nil {
			if fz.logger != nil {
				fz.logger.Warn("invalid upstream address", "upstream", upstream, "error", err)
			}
			continue
		}

		resp, rtt, err := fz.upstreamClient.ExchangeContext(ctx, r, upstream)
		if err != nil {
			if fz.logger != nil {
				fz.logger.Debug("upstream query failed",
					"upstream", upstream,
					"error", err,
					"rtt", rtt)
			}
			continue
		}

		if resp != nil {
			if fz.logger != nil {
				fz.logger.Debug("upstream query succeeded",
					"upstream", upstream,
					"rtt", rtt,
					"rcode", dns.RcodeToString[resp.Rcode])
			}

			resp.Id = r.Id
			return true, w.WriteMsg(resp)
		}
	}

	return false, nil
}

func (fz *ForwardZone) UpdateRecords(records map[string]DNSRecord) {
	fz.records = make(map[string]DNSRecord)
	for name, record := range records {
		fz.records[normalizeQName(name)] = record
	}
}

func (fz *ForwardZone) UpdateUpstream(upstream *UpstreamConfig) {
	fz.upstreamConfig = upstream
	fz.setupUpstreamClient()
}

func (fz *ForwardZone) MergeRecords(overrideRecords map[string]DNSRecord) *ForwardZone {
	mergedRecords := make(map[string]DNSRecord)

	for name, record := range fz.records {
		mergedRecords[name] = record
	}

	for name, record := range overrideRecords {
		mergedRecords[normalizeQName(name)] = record
	}

	newZone := NewForwardZone(fz.zoneName, mergedRecords, fz.upstreamConfig)
	newZone.SetLogger(fz.logger)
	return newZone
}
