package dns

import (
	"encoding/json"
	"log/slog"
	"testing"

	// Import the upstream resolver module so it's registered
	_ "github.com/kusold/mightydns/module/dns/resolver"
)

func TestDNSServer_WithUpstreamHandler(t *testing.T) {
	// This test verifies that the DNS server can provision with an upstream handler
	server := &DNSServer{
		Listen:   []string{":5353"},
		Protocol: []string{"udp"},
		Handler: json.RawMessage(`{
			"handler": "dns.resolver.upstream",
			"upstreams": ["8.8.8.8:53"]
		}`),
	}

	err := server.provision(mockContext{}, slog.Default())
	if err != nil {
		t.Errorf("Expected no error with upstream handler, got: %v", err)
	}

	if server.handler == nil {
		t.Error("Expected handler to be set after provision")
	}
}
