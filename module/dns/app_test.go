package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"testing"

	"github.com/miekg/dns"
)

type mockContext struct{}

func (mockContext) App(name string) (interface{}, error) { return nil, nil }
func (mockContext) Logger() *slog.Logger                 { return slog.Default() }
func (mockContext) LoadModule(cfg interface{}, fieldName string) (interface{}, error) {
	return nil, fmt.Errorf("module loading not supported in mock context")
}

type mockDNSHandler struct{}

func (mockDNSHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	m := new(dns.Msg)
	m.SetReply(r)
	m.SetRcode(r, dns.RcodeSuccess)
	return w.WriteMsg(m)
}

func TestDNSApp_ModuleInfo(t *testing.T) {
	app := &DNSApp{}
	info := app.MightyModule()

	if info.ID != "dns" {
		t.Errorf("Expected module ID 'dns', got %s", info.ID)
	}

	if info.New == nil {
		t.Error("Expected New function to be set")
	}

	newModule := info.New()
	if _, ok := newModule.(*DNSApp); !ok {
		t.Error("Expected New() to return *DNSApp")
	}
}

func TestDNSApp_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  *DNSApp
		wantErr bool
	}{
		{
			name:    "empty config",
			config:  &DNSApp{},
			wantErr: false,
		},
		{
			name: "simple server config",
			config: &DNSApp{
				Servers: map[string]*DNSServer{
					"test": {
						Listen:   []string{":5353"},
						Protocol: []string{"udp"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Provision(mockContext{})
			if (err != nil) != tt.wantErr {
				t.Errorf("DNSApp.Provision() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDNSServer_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  *DNSServer
		wantErr bool
	}{
		{
			name:    "default config",
			config:  &DNSServer{},
			wantErr: false,
		},
		{
			name: "custom listen and protocol",
			config: &DNSServer{
				Listen:   []string{":5353", ":8053"},
				Protocol: []string{"tcp"},
			},
			wantErr: false,
		},
		{
			name: "invalid handler config",
			config: &DNSServer{
				Handler: json.RawMessage(`{invalid json}`),
			},
			wantErr: true,
		},
		{
			name: "missing handler field",
			config: &DNSServer{
				Handler: json.RawMessage(`{"upstreams": ["8.8.8.8:53"]}`),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.provision(mockContext{}, slog.Default())
			if (err != nil) != tt.wantErr {
				t.Errorf("DNSServer.provision() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDNSServer_DefaultValues(t *testing.T) {
	server := &DNSServer{}
	err := server.provision(mockContext{}, slog.Default())
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	expectedListen := []string{":53"}
	if len(server.Listen) != len(expectedListen) {
		t.Errorf("Expected %d listen addresses, got %d", len(expectedListen), len(server.Listen))
	}
	for i, expected := range expectedListen {
		if server.Listen[i] != expected {
			t.Errorf("Expected listen address %d to be %s, got %s", i, expected, server.Listen[i])
		}
	}

	expectedProtocols := []string{"udp", "tcp"}
	if len(server.Protocol) != len(expectedProtocols) {
		t.Errorf("Expected %d protocols, got %d", len(expectedProtocols), len(server.Protocol))
	}
	for i, expected := range expectedProtocols {
		if server.Protocol[i] != expected {
			t.Errorf("Expected protocol %d to be %s, got %s", i, expected, server.Protocol[i])
		}
	}
}

func TestDNSServer_ServeDNS(t *testing.T) {
	server := &DNSServer{
		handler: &mockDNSHandler{},
		logger:  slog.Default(),
	}

	// Create a mock DNS request
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

	// Create a mock response writer
	mockWriter := &mockResponseWriter{}

	// Test ServeDNS
	server.ServeDNS(mockWriter, req)

	if !mockWriter.writeCalled {
		t.Error("Expected WriteMsg to be called")
	}
}

// Mock response writer for testing
type mockResponseWriter struct {
	writeCalled bool
	msg         *dns.Msg
}

func (m *mockResponseWriter) LocalAddr() net.Addr  { return nil }
func (m *mockResponseWriter) RemoteAddr() net.Addr { return nil }
func (m *mockResponseWriter) WriteMsg(msg *dns.Msg) error {
	m.writeCalled = true
	m.msg = msg
	return nil
}
func (m *mockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (m *mockResponseWriter) Close() error              { return nil }
func (m *mockResponseWriter) TsigStatus() error         { return nil }
func (m *mockResponseWriter) TsigTimersOnly(bool)       {}
func (m *mockResponseWriter) Hijack()                   {}
