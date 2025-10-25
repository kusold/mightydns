package zone

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
)

// ClientGroupKey is the context key for storing client group information
type ClientGroupKey struct{}

func init() {
	mightydns.RegisterModule(&ZoneManager{})
}

type ZoneManager struct {
	Zones           []*ZoneConfig   `json:"zones,omitempty"`
	DefaultUpstream *UpstreamConfig `json:"default_upstream,omitempty"`

	baseZones map[string]Zone
	logger    *slog.Logger
	ctx       mightydns.Context
}

type ZoneManagerConfig struct {
	Zones           []*ZoneConfig   `json:"zones,omitempty"`
	DefaultUpstream *UpstreamConfig `json:"default_upstream,omitempty"`
}

func (ZoneManager) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "dns.zone.manager",
		New: func() mightydns.Module { return new(ZoneManager) },
	}
}

func (zm *ZoneManager) Provision(ctx mightydns.Context) error {
	zm.ctx = ctx
	zm.logger = ctx.Logger().With("module", "dns.zone.manager")
	zm.baseZones = make(map[string]Zone)

	if zm.DefaultUpstream == nil {
		zm.DefaultUpstream = &UpstreamConfig{
			Upstreams: []string{"8.8.8.8:53", "1.1.1.1:53"},
			Timeout:   "5s",
			Protocol:  "udp",
		}
	}

	for _, zoneConfig := range zm.Zones {
		zone, err := zm.createZone(zoneConfig)
		if err != nil {
			return fmt.Errorf("failed to create zone %s: %w", zoneConfig.Zone, err)
		}
		zm.baseZones[normalizeQName(zoneConfig.Zone)] = zone
	}

	zm.logger.Info("zone manager provisioned",
		"zones", len(zm.baseZones),
		"default_upstream", zm.DefaultUpstream.Upstreams)

	return nil
}

func (zm *ZoneManager) createZone(config *ZoneConfig) (Zone, error) {
	switch strings.ToLower(config.Type) {
	case "forward", "":
		upstream := config.Upstream
		if upstream == nil {
			upstream = zm.DefaultUpstream
		}

		zone := NewForwardZone(config.Zone, config.Records, upstream)
		zone.SetLogger(zm.logger.With("zone", config.Zone))
		return zone, nil
	default:
		return nil, fmt.Errorf("unsupported zone type: %s", config.Type)
	}
}

func (zm *ZoneManager) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	if len(r.Question) == 0 {
		return zm.sendErrorResponse(w, r, dns.RcodeFormatError)
	}

	question := r.Question[0]
	qname := normalizeQName(question.Name)
	qtype := dns.TypeToString[question.Qtype]

	clientGroup := zm.extractClientGroup(ctx)

	zm.logger.Debug("processing DNS query",
		"query_id", r.Id,
		"qname", qname,
		"qtype", qtype,
		"client_group", clientGroup)

	// Create a sorted list of zones by specificity (longest zone name first)
	type zoneEntry struct {
		name string
		zone Zone
	}

	var sortedZones []zoneEntry
	for zoneName, zone := range zm.baseZones {
		sortedZones = append(sortedZones, zoneEntry{name: zoneName, zone: zone})
	}

	// Sort by zone name length (longer = more specific)
	sort.Slice(sortedZones, func(i, j int) bool {
		return len(sortedZones[i].name) > len(sortedZones[j].name)
	})

	for _, entry := range sortedZones {
		if entry.zone.Match(qname) {
			resolved, err := entry.zone.Resolve(ctx, w, r, clientGroup)
			if err != nil {
				zm.logger.Error("zone resolution error",
					"zone", entry.name,
					"qname", qname,
					"client_group", clientGroup,
					"error", err)
				return zm.sendErrorResponse(w, r, dns.RcodeServerFailure)
			}

			if resolved {
				zm.logger.Debug("query resolved by zone",
					"zone", entry.name,
					"qname", qname,
					"client_group", clientGroup)
				return nil
			}
		}
	}

	if zm.DefaultUpstream != nil && len(zm.DefaultUpstream.Upstreams) > 0 {
		return zm.forwardToDefaultUpstream(ctx, w, r)
	}

	zm.logger.Debug("no zone matched, returning NXDOMAIN",
		"qname", qname,
		"client_group", clientGroup)
	return zm.sendErrorResponse(w, r, dns.RcodeNameError)
}

func (zm *ZoneManager) extractClientGroup(ctx context.Context) string {
	if clientGroup, ok := ctx.Value(ClientGroupKey{}).(string); ok && clientGroup != "" {
		return clientGroup
	}
	return "default"
}

func (zm *ZoneManager) forwardToDefaultUpstream(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	zm.logger.Debug("forwarding to default upstream",
		"upstreams", zm.DefaultUpstream.Upstreams)

	timeout := 5 * time.Second
	if zm.DefaultUpstream.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(zm.DefaultUpstream.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	protocol := "udp"
	if zm.DefaultUpstream.Protocol != "" {
		protocol = zm.DefaultUpstream.Protocol
	}

	client := &dns.Client{
		Net:     protocol,
		Timeout: timeout,
	}

	for _, upstream := range zm.DefaultUpstream.Upstreams {
		if _, _, err := net.SplitHostPort(upstream); err != nil {
			zm.logger.Warn("invalid upstream address", "upstream", upstream, "error", err)
			continue
		}

		resp, rtt, err := client.ExchangeContext(ctx, r, upstream)
		if err != nil {
			zm.logger.Debug("upstream query failed",
				"upstream", upstream,
				"error", err,
				"rtt", rtt)
			continue
		}

		if resp != nil {
			zm.logger.Debug("upstream query succeeded",
				"upstream", upstream,
				"rtt", rtt,
				"rcode", dns.RcodeToString[resp.Rcode])

			resp.Id = r.Id
			return w.WriteMsg(resp)
		}
	}

	zm.logger.Debug("all upstream resolvers failed")
	return zm.sendErrorResponse(w, r, dns.RcodeServerFailure)
}

func (zm *ZoneManager) sendErrorResponse(w dns.ResponseWriter, r *dns.Msg, rcode int) error {
	m := new(dns.Msg)
	m.SetReply(r)
	m.SetRcode(r, rcode)
	return w.WriteMsg(m)
}

func (zm *ZoneManager) Cleanup() error {
	zm.logger.Debug("cleaning up zone manager")
	return nil
}
