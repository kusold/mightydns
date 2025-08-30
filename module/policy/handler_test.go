package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
	"github.com/kusold/mightydns/module/client"
)

func init() {
	// Register mock handler once for all tests
	mightydns.RegisterModule(&mockDNSHandler{})
}

func TestPolicyHandler_MightyModule(t *testing.T) {
	handler := &PolicyHandler{}
	info := handler.MightyModule()

	if info.ID != "policy" {
		t.Errorf("Expected ID 'policy', got '%s'", info.ID)
	}

	if info.New == nil {
		t.Error("New function should not be nil")
	}

	module := info.New()
	if _, ok := module.(*PolicyHandler); !ok {
		t.Error("New() should return a PolicyHandler")
	}
}

func TestPolicyHandler_Provision(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	tests := []struct {
		name      string
		handler   *PolicyHandler
		wantError bool
	}{
		{
			name:      "missing base handler",
			handler:   &PolicyHandler{},
			wantError: true,
		},
		{
			name: "missing client groups",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
			},
			wantError: true,
		},
		{
			name: "invalid base handler",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"invalid": "json"`),
				ClientGroups: map[string]*client.ClientGroup{
					"test": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
				},
			},
			wantError: true,
		},
		{
			name: "valid minimal config",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "mock_handler"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.handler.Provision(ctx)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestPolicyHandler_ApplyOverrides(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	handler := &PolicyHandler{}
	handler.ctx = ctx
	handler.logger = logger

	baseConfig := json.RawMessage(`{
		"handler": "dns.resolver.chain",
		"handlers": [
			{
				"handler": "dns.resolver.cache",
				"size": 1000,
				"ttl": "300s"
			},
			{
				"handler": "dns.resolver.upstream",
				"upstreams": ["1.1.1.1:53"],
				"timeout": "5s"
			}
		]
	}`)

	overrides := map[string]json.RawMessage{
		"dns.resolver.upstream": json.RawMessage(`{
			"upstreams": ["8.8.8.8:53"],
			"timeout": "2s"
		}`),
	}

	result, err := handler.applyOverrides(baseConfig, overrides)
	if err != nil {
		t.Fatalf("applyOverrides failed: %v", err)
	}

	// Parse the result to verify overrides were applied
	var resultConfig map[string]interface{}
	if err := json.Unmarshal(result, &resultConfig); err != nil {
		t.Fatalf("Failed to parse result config: %v", err)
	}

	// Check that the handlers array exists
	handlers, ok := resultConfig["handlers"].([]interface{})
	if !ok {
		t.Fatal("handlers field should be an array")
	}

	if len(handlers) != 2 {
		t.Fatalf("Expected 2 handlers, got %d", len(handlers))
	}

	// Check that the upstream handler was modified
	upstreamHandler := handlers[1].(map[string]interface{})
	if upstreamHandler["handler"] != "dns.resolver.upstream" {
		t.Error("Second handler should be dns.resolver.upstream")
	}

	// Check that overrides were applied
	upstreams := upstreamHandler["upstreams"].([]interface{})
	if len(upstreams) != 1 || upstreams[0] != "8.8.8.8:53" {
		t.Errorf("Expected upstreams to be overridden to ['8.8.8.8:53'], got %v", upstreams)
	}

	if upstreamHandler["timeout"] != "2s" {
		t.Errorf("Expected timeout to be overridden to '2s', got %v", upstreamHandler["timeout"])
	}

	// Check that cache handler was not modified
	cacheHandler := handlers[0].(map[string]interface{})
	if cacheHandler["size"] != float64(1000) { // JSON numbers are float64
		t.Errorf("Cache handler size should remain 1000, got %v", cacheHandler["size"])
	}
}

func TestPolicyHandler_ServeDNS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	handler := &PolicyHandler{
		BaseHandler: json.RawMessage(`{"handler": "mock_handler", "name": "base"}`),
		ClientGroups: map[string]*client.ClientGroup{
			"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
			"external": {Sources: []string{"0.0.0.0/0"}, Priority: 100},
		},
		Policies: []*PolicyOverride{
			{
				Match: &PolicyMatch{ClientGroup: "internal"},
				Overrides: map[string]json.RawMessage{
					"mock_handler": json.RawMessage(`{"name": "internal_override"}`),
				},
			},
		},
	}

	if err := handler.Provision(ctx); err != nil {
		t.Fatalf("Failed to provision handler: %v", err)
	}

	tests := []struct {
		name           string
		clientIP       string
		expectedName   string
		expectedCalled bool
	}{
		{
			name:           "internal client gets policy handler",
			clientIP:       "192.168.1.100",
			expectedName:   "internal_override",
			expectedCalled: true,
		},
		{
			name:           "external client gets base handler",
			clientIP:       "8.8.8.8",
			expectedName:   "base",
			expectedCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock state
			mockHandlerCalled = false
			mockHandlerName = ""

			w := &mockResponseWriter{
				addr: &net.UDPAddr{IP: net.ParseIP(tt.clientIP), Port: 12345},
			}

			req := new(dns.Msg)
			req.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

			err := handler.ServeDNS(context.Background(), w, req)
			if err != nil {
				t.Errorf("ServeDNS failed: %v", err)
			}

			if mockHandlerCalled != tt.expectedCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectedCalled, mockHandlerCalled)
			}

			if tt.expectedCalled && mockHandlerName != tt.expectedName {
				t.Errorf("Expected handler name=%s, got %s", tt.expectedName, mockHandlerName)
			}
		})
	}
}

func TestPolicyHandler_DeepCopy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := &PolicyHandler{logger: logger}

	original := map[string]interface{}{
		"string": "value",
		"number": 42,
		"nested": map[string]interface{}{
			"inner": "value",
		},
		"array": []interface{}{
			"item1",
			map[string]interface{}{
				"nested": "in_array",
			},
		},
	}

	copy := handler.deepCopyMap(original)

	// Modify the original
	original["string"] = "modified"
	original["nested"].(map[string]interface{})["inner"] = "modified"
	original["array"].([]interface{})[0] = "modified"

	// Check that copy was not affected
	if copy["string"] != "value" {
		t.Error("String value should not be modified in copy")
	}

	if copy["nested"].(map[string]interface{})["inner"] != "value" {
		t.Error("Nested value should not be modified in copy")
	}

	if copy["array"].([]interface{})[0] != "item1" {
		t.Error("Array value should not be modified in copy")
	}
}

// Mock implementations for testing

var (
	mockHandlerCalled bool
	mockHandlerName   string
)

type mockDNSHandler struct {
	Name string `json:"name,omitempty"`
}

func (m *mockDNSHandler) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "mock_handler",
		New: func() mightydns.Module { return new(mockDNSHandler) },
	}
}

func (m *mockDNSHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	mockHandlerCalled = true
	mockHandlerName = m.Name

	// Create a simple response
	msg := new(dns.Msg)
	msg.SetReply(r)
	return w.WriteMsg(msg)
}

func (m *mockDNSHandler) Provision(ctx mightydns.Context) error {
	return nil
}

type mockContext struct {
	logger *slog.Logger
}

func (m *mockContext) Logger() *slog.Logger {
	return m.logger
}

func (m *mockContext) App(name string) (interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockContext) LoadModule(cfg interface{}, fieldName string) (interface{}, error) {
	// Simple implementation for testing - just find and instantiate the module
	if configMap, ok := cfg.(map[string]interface{}); ok {
		if handlerType, exists := configMap["handler"].(string); exists {
			moduleInfo, exists := mightydns.GetModule(handlerType)
			if !exists {
				return nil, fmt.Errorf("unknown module: %s", handlerType)
			}

			instance := moduleInfo.New()

			// Try to unmarshal the config into the instance
			cfgJSON, err := json.Marshal(cfg)
			if err != nil {
				return nil, err
			}

			if err := json.Unmarshal(cfgJSON, instance); err != nil {
				return nil, err
			}

			// Provision if possible
			if provisioner, ok := instance.(mightydns.Provisioner); ok {
				if err := provisioner.Provision(m); err != nil {
					return nil, err
				}
			}

			return instance, nil
		}
	}
	return nil, fmt.Errorf("invalid config")
}

type mockResponseWriter struct {
	addr net.Addr
}

func (m *mockResponseWriter) LocalAddr() net.Addr       { return m.addr }
func (m *mockResponseWriter) RemoteAddr() net.Addr      { return m.addr }
func (m *mockResponseWriter) WriteMsg(*dns.Msg) error   { return nil }
func (m *mockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (m *mockResponseWriter) Close() error              { return nil }
func (m *mockResponseWriter) TsigStatus() error         { return nil }
func (m *mockResponseWriter) TsigTimersOnly(bool)       {}
func (m *mockResponseWriter) Hijack()                   {}
