package resolver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
)

func init() {
	mightydns.RegisterModule(&UpstreamResolver{})
}

type UpstreamResolver struct {
	Upstreams []string `json:"upstreams,omitempty"`
	Timeout   string   `json:"timeout,omitempty"`
	Protocol  string   `json:"protocol,omitempty"`

	client   *dns.Client
	timeout  time.Duration
	protocol string
	logger   *slog.Logger
}

func (UpstreamResolver) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "dns.resolver.upstream",
		New: func() mightydns.Module { return new(UpstreamResolver) },
	}
}

func (u *UpstreamResolver) Provision(ctx mightydns.Context) error {
	u.logger = ctx.Logger().With("module", "dns.resolver.upstream")

	if len(u.Upstreams) == 0 {
		u.Upstreams = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}

	if u.Timeout == "" {
		u.timeout = 5 * time.Second
	} else {
		timeout, err := time.ParseDuration(u.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout duration: %w", err)
		}
		u.timeout = timeout
	}

	switch u.Protocol {
	case "tcp":
		u.protocol = "tcp"
	case "tcp-tls":
		u.protocol = "tcp-tls"
	case "udp", "":
		u.protocol = "udp"
	default:
		return fmt.Errorf("unsupported protocol: %s", u.Protocol)
	}

	u.client = &dns.Client{
		Net:     u.protocol,
		Timeout: u.timeout,
	}

	for _, upstream := range u.Upstreams {
		if _, _, err := net.SplitHostPort(upstream); err != nil {
			return fmt.Errorf("invalid upstream address %s: %w", upstream, err)
		}
	}

	return nil
}

func (u *UpstreamResolver) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	// Extract query details for logging
	var qname, qtype string
	if len(r.Question) > 0 {
		qname = r.Question[0].Name
		qtype = dns.TypeToString[r.Question[0].Qtype]
	}

	u.logger.Debug("starting DNS query resolution",
		"query_id", r.Id,
		"query_name", qname,
		"query_type", qtype,
		"upstreams", u.Upstreams,
		"protocol", u.protocol,
		"timeout", u.timeout)

	for i, upstream := range u.Upstreams {
		u.logger.Debug("attempting upstream resolver",
			"query_id", r.Id,
			"upstream", upstream,
			"attempt", i+1,
			"total_upstreams", len(u.Upstreams))

		resp, rtt, err := u.client.ExchangeContext(ctx, r, upstream)
		if err != nil {
			u.logger.Debug("upstream resolver failed",
				"query_id", r.Id,
				"upstream", upstream,
				"error", err,
				"rtt", rtt)
			continue
		}

		if resp != nil {
			u.logger.Debug("upstream resolver succeeded",
				"query_id", r.Id,
				"upstream", upstream,
				"rtt", rtt,
				"rcode", dns.RcodeToString[resp.Rcode],
				"answer_count", len(resp.Answer),
				"authority_count", len(resp.Ns),
				"additional_count", len(resp.Extra))

			resp.Id = r.Id
			return w.WriteMsg(resp)
		}

		u.logger.Debug("upstream resolver returned nil response",
			"query_id", r.Id,
			"upstream", upstream,
			"rtt", rtt)
	}

	u.logger.Debug("all upstream resolvers failed, returning SERVFAIL",
		"query_id", r.Id,
		"query_name", qname,
		"query_type", qtype,
		"tried_upstreams", len(u.Upstreams))

	m := new(dns.Msg)
	m.SetReply(r)
	m.SetRcode(r, dns.RcodeServerFailure)
	return w.WriteMsg(m)
}

func (u *UpstreamResolver) Cleanup() error {
	return nil
}
