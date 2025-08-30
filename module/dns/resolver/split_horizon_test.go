package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
)

// mockDNSHandler implements mightydns.DNSHandler for testing
type mockDNSHandler struct {
	name     string
	response *dns.Msg
	err      error
	called   bool
}

func (m *mockDNSHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	if m.response != nil {
		m.response.Id = r.Id
		return w.WriteMsg(m.response)
	}

	// Create a simple response if none provided
	response := &dns.Msg{}
	response.SetReply(r)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: "test.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	}}
	return w.WriteMsg(response)
}

// mockResponseWriter implements dns.ResponseWriter for testing
type mockResponseWriter struct {
	remoteAddr net.Addr
	response   *dns.Msg
}

func (m *mockResponseWriter) LocalAddr() net.Addr         { return nil }
func (m *mockResponseWriter) RemoteAddr() net.Addr        { return m.remoteAddr }
func (m *mockResponseWriter) WriteMsg(msg *dns.Msg) error { m.response = msg; return nil }
func (m *mockResponseWriter) Write([]byte) (int, error)   { return 0, nil }
func (m *mockResponseWriter) Close() error                { return nil }
func (m *mockResponseWriter) TsigStatus() error           { return nil }
func (m *mockResponseWriter) TsigTimersOnly(bool)         {}
func (m *mockResponseWriter) Hijack()                     {}

// mockSplitHorizonContext implements mightydns.Context for testing split-horizon
type mockSplitHorizonContext struct {
	handlers map[string]mightydns.DNSHandler
}

func (m *mockSplitHorizonContext) App(name string) (interface{}, error) {
	return nil, fmt.Errorf("app %s not found", name)
}

func (m *mockSplitHorizonContext) Logger() *slog.Logger {
	return slog.Default()
}

func (m *mockSplitHorizonContext) LoadModule(cfg interface{}, fieldName string) (interface{}, error) {
	// Parse the config to extract handler type
	configMap, ok := cfg.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("config is not a map")
	}

	handlerType, ok := configMap["handler"].(string)
	if !ok {
		return nil, fmt.Errorf("handler type not specified")
	}

	// Return a mock handler based on the type
	if handler, exists := m.handlers[handlerType]; exists {
		return handler, nil
	}

	// Create a default mock handler
	return &mockDNSHandler{
		name: handlerType,
	}, nil
}

func TestSplitHorizonResolver_MightyModule(t *testing.T) {
	s := &SplitHorizonResolver{}
	info := s.MightyModule()

	if info.ID != "dns.resolver.split_horizon" {
		t.Errorf("Expected module ID 'dns.resolver.split_horizon', got %s", info.ID)
	}

	if info.New == nil {
		t.Error("Expected New function to be set")
	}

	newModule := info.New()
	if _, ok := newModule.(*SplitHorizonResolver); !ok {
		t.Error("Expected New() to return *SplitHorizonResolver")
	}
}

func TestSplitHorizonResolver_parseSource(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantErr    bool
		expectCIDR bool
		expectIP   bool
	}{
		{
			name:       "valid CIDR IPv4",
			source:     "192.168.1.0/24",
			wantErr:    false,
			expectCIDR: true,
		},
		{
			name:       "valid CIDR IPv6",
			source:     "2001:db8::/32",
			wantErr:    false,
			expectCIDR: true,
		},
		{
			name:     "valid IP IPv4",
			source:   "192.168.1.1",
			wantErr:  false,
			expectIP: true,
		},
		{
			name:     "valid IP IPv6",
			source:   "2001:db8::1",
			wantErr:  false,
			expectIP: true,
		},
		{
			name:    "invalid CIDR",
			source:  "192.168.1.0/33",
			wantErr: true,
		},
		{
			name:    "invalid IP",
			source:  "999.999.999.999",
			wantErr: true,
		},
		{
			name:    "empty string",
			source:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SplitHorizonResolver{logger: slog.Default()}
			compiled := &compiledClientGroup{}

			err := s.parseSource(tt.source, compiled)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if tt.expectCIDR && len(compiled.networks) != 1 {
					t.Errorf("Expected 1 network, got %d", len(compiled.networks))
				}
				if tt.expectIP && len(compiled.ips) != 1 {
					t.Errorf("Expected 1 IP, got %d", len(compiled.ips))
				}
			}
		})
	}
}

func TestSplitHorizonResolver_compileClientGroups(t *testing.T) {
	tests := []struct {
		name         string
		clientGroups map[string]*ClientGroup
		wantErr      bool
	}{
		{
			name: "valid client groups",
			clientGroups: map[string]*ClientGroup{
				"internal": {
					Sources:  []string{"192.168.0.0/16", "10.0.0.1"},
					Priority: 10,
				},
				"external": {
					Sources:  []string{"0.0.0.0/0"},
					Priority: 100,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid CIDR in client group",
			clientGroups: map[string]*ClientGroup{
				"bad": {
					Sources: []string{"invalid/cidr"},
				},
			},
			wantErr: true,
		},
		{
			name:         "no client groups",
			clientGroups: map[string]*ClientGroup{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SplitHorizonResolver{
				ClientGroups:   tt.clientGroups,
				compiledGroups: make(map[string]*compiledClientGroup),
				logger:         slog.Default(),
			}

			err := s.compileClientGroups()
			if (err != nil) != tt.wantErr {
				t.Errorf("compileClientGroups() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if len(s.compiledGroups) != len(tt.clientGroups) {
					t.Errorf("Expected %d compiled groups, got %d", len(tt.clientGroups), len(s.compiledGroups))
				}
			}
		})
	}
}

func TestSplitHorizonResolver_matchClientGroup(t *testing.T) {
	s := &SplitHorizonResolver{
		logger: slog.Default(),
		compiledGroups: map[string]*compiledClientGroup{
			"internal": {
				name:     "internal",
				priority: 10,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("192.168.0.0/16"); return n }(),
				},
				ips: []net.IP{net.ParseIP("127.0.0.1")},
			},
			"vpn": {
				name:     "vpn",
				priority: 20,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("10.200.0.0/16"); return n }(),
				},
			},
			"private": {
				name:     "private",
				priority: 30,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("10.0.0.0/8"); return n }(),
				},
			},
			"external": {
				name:     "external",
				priority: 100,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("0.0.0.0/0"); return n }(),
				},
			},
		},
	}

	tests := []struct {
		name          string
		clientIP      string
		expectedGroup string
	}{
		{
			name:          "localhost matches internal via IP",
			clientIP:      "127.0.0.1",
			expectedGroup: "internal",
		},
		{
			name:          "private network matches internal",
			clientIP:      "192.168.1.100",
			expectedGroup: "internal",
		},
		{
			name:          "VPN network matches vpn (more specific than private)",
			clientIP:      "10.200.1.1",
			expectedGroup: "vpn",
		},
		{
			name:          "other 10.x network matches private",
			clientIP:      "10.50.1.1",
			expectedGroup: "private",
		},
		{
			name:          "public IP matches external",
			clientIP:      "8.8.8.8",
			expectedGroup: "external",
		},
		{
			name:          "invalid IP returns empty",
			clientIP:      "",
			expectedGroup: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clientIP net.IP
			if tt.clientIP != "" {
				clientIP = net.ParseIP(tt.clientIP)
			}

			result := s.matchClientGroup(clientIP)
			if result != tt.expectedGroup {
				t.Errorf("matchClientGroup(%s) = %s, want %s", tt.clientIP, result, tt.expectedGroup)
			}
		})
	}
}

func TestSplitHorizonResolver_getClientIP(t *testing.T) {
	s := &SplitHorizonResolver{logger: slog.Default()}

	tests := []struct {
		name       string
		remoteAddr net.Addr
		expectedIP string
	}{
		{
			name:       "UDP address",
			remoteAddr: &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "TCP address",
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 54321},
			expectedIP: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &mockResponseWriter{remoteAddr: tt.remoteAddr}
			result := s.getClientIP(w)

			if result.String() != tt.expectedIP {
				t.Errorf("getClientIP() = %s, want %s", result.String(), tt.expectedIP)
			}
		})
	}
}

func TestSplitHorizonResolver_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  SplitHorizonResolver
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: SplitHorizonResolver{
				ClientGroups: map[string]*ClientGroup{
					"internal": {
						Sources:  []string{"192.168.0.0/16"},
						Priority: 10,
					},
				},
				Policies: []*Policy{
					{
						Match: &PolicyMatch{ClientGroup: "internal"},
						Upstream: json.RawMessage(`{
							"handler": "dns.resolver.upstream",
							"upstreams": ["8.8.8.8:53"]
						}`),
					},
				},
				DefaultPolicy: &Policy{
					Upstream: json.RawMessage(`{
						"handler": "dns.resolver.upstream",
						"upstreams": ["1.1.1.1:53"]
					}`),
				},
			},
			wantErr: false,
		},
		{
			name: "no client groups",
			config: SplitHorizonResolver{
				Policies: []*Policy{},
			},
			wantErr: true,
		},
		{
			name: "no policies",
			config: SplitHorizonResolver{
				ClientGroups: map[string]*ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}},
				},
				Policies: []*Policy{},
			},
			wantErr: true,
		},
		{
			name: "policy references non-existent client group",
			config: SplitHorizonResolver{
				ClientGroups: map[string]*ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}},
				},
				Policies: []*Policy{
					{
						Match: &PolicyMatch{ClientGroup: "nonexistent"},
						Upstream: json.RawMessage(`{
							"handler": "dns.resolver.upstream",
							"upstreams": ["8.8.8.8:53"]
						}`),
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &tt.config
			ctx := &mockSplitHorizonContext{
				handlers: make(map[string]mightydns.DNSHandler),
			}

			err := s.Provision(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Provision() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSplitHorizonResolver_ServeDNS(t *testing.T) {
	// Create handlers for different policies
	internalHandler := &mockDNSHandler{name: "internal"}
	externalHandler := &mockDNSHandler{name: "external"}

	s := &SplitHorizonResolver{
		ClientGroups: map[string]*ClientGroup{
			"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
			"external": {Sources: []string{"0.0.0.0/0"}, Priority: 100},
		},
		Policies: []*Policy{
			{
				Match:   &PolicyMatch{ClientGroup: "internal"},
				handler: internalHandler,
			},
			{
				Match:   &PolicyMatch{ClientGroup: "external"},
				handler: externalHandler,
			},
		},
		logger: slog.Default(),
		compiledGroups: map[string]*compiledClientGroup{
			"internal": {
				name:     "internal",
				priority: 10,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("192.168.0.0/16"); return n }(),
				},
			},
			"external": {
				name:     "external",
				priority: 100,
				networks: []*net.IPNet{
					func() *net.IPNet { _, n, _ := net.ParseCIDR("0.0.0.0/0"); return n }(),
				},
			},
		},
	}

	tests := []struct {
		name            string
		clientIP        string
		expectedHandler string
	}{
		{
			name:            "internal IP routes to internal handler",
			clientIP:        "192.168.1.100",
			expectedHandler: "internal",
		},
		{
			name:            "external IP routes to external handler",
			clientIP:        "8.8.8.8",
			expectedHandler: "external",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset handlers
			internalHandler.called = false
			externalHandler.called = false

			w := &mockResponseWriter{
				remoteAddr: &net.UDPAddr{
					IP:   net.ParseIP(tt.clientIP),
					Port: 12345,
				},
			}

			req := &dns.Msg{
				Question: []dns.Question{
					{Name: "test.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
				},
			}

			err := s.ServeDNS(context.Background(), w, req)
			if err != nil {
				t.Errorf("ServeDNS() error = %v", err)
			}

			// Check that the correct handler was called
			switch tt.expectedHandler {
			case "internal":
				if !internalHandler.called {
					t.Error("Expected internal handler to be called")
				}
				if externalHandler.called {
					t.Error("External handler should not have been called")
				}
			case "external":
				if !externalHandler.called {
					t.Error("Expected external handler to be called")
				}
				if internalHandler.called {
					t.Error("Internal handler should not have been called")
				}
			}
		})
	}
}

func TestSplitHorizonResolver_ServeDNS_DefaultFallback(t *testing.T) {
	defaultHandler := &mockDNSHandler{name: "default"}

	s := &SplitHorizonResolver{
		DefaultPolicy: &Policy{
			handler: defaultHandler,
		},
		logger:         slog.Default(),
		compiledGroups: make(map[string]*compiledClientGroup), // No groups, should fall back to default
	}

	w := &mockResponseWriter{
		remoteAddr: &net.UDPAddr{
			IP:   net.ParseIP("1.2.3.4"),
			Port: 12345,
		},
	}

	req := &dns.Msg{
		Question: []dns.Question{
			{Name: "test.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
		},
	}

	err := s.ServeDNS(context.Background(), w, req)
	if err != nil {
		t.Errorf("ServeDNS() error = %v", err)
	}

	if !defaultHandler.called {
		t.Error("Expected default handler to be called")
	}

	if w.response == nil {
		t.Error("Expected a response from default handler")
	}
}
