package mightydns

import (
	"context"

	"github.com/miekg/dns"
)

type DNSHandler interface {
	ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error
}

type DNSMiddleware interface {
	ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, next DNSHandler) error
}
