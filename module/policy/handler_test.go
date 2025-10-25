package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
	"github.com/kusold/mightydns/module/client"
)

func init() {
	// Register mock handler once for all tests
	mightydns.RegisterModule(&mockDNSHandler{})
}

func TestPolicyHandler_ModuleInfo(t *testing.T) {
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

	// Test that the handler provisions correctly and has the expected policy trees
	if _, exists := handler.policyTrees["internal"]; !exists {
		t.Error("Expected policy tree for internal group")
	}

	if handler.baseHandler == nil {
		t.Error("Expected base handler to be set")
	}

	if handler.classifier == nil {
		t.Error("Expected classifier to be set")
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

func TestPolicyHandler_ValidateConfiguration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tests := []struct {
		name      string
		handler   *PolicyHandler
		wantError bool
		errorMsg  string
	}{
		{
			name:      "missing base handler",
			handler:   &PolicyHandler{},
			wantError: true,
			errorMsg:  "base_handler is required",
		},
		{
			name: "invalid base handler JSON",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"invalid": "json"`),
			},
			wantError: true,
			errorMsg:  "base_handler must be valid JSON",
		},
		{
			name: "base handler missing handler field",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"upstreams": ["8.8.8.8:53"]}`),
			},
			wantError: true,
			errorMsg:  "base_handler must specify a 'handler' field",
		},
		{
			name: "missing client groups",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
			},
			wantError: true,
			errorMsg:  "client_groups are required",
		},
		{
			name: "client group with empty sources",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"empty": {Sources: []string{}, Priority: 10},
				},
			},
			wantError: true,
			errorMsg:  "client group must have at least one source",
		},
		{
			name: "client group with invalid CIDR",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"invalid": {Sources: []string{"192.168.0.0/999"}, Priority: 10},
				},
			},
			wantError: true,
			errorMsg:  "invalid CIDR block",
		},
		{
			name: "client group with invalid IP",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"invalid": {Sources: []string{"999.999.999.999"}, Priority: 10},
				},
			},
			wantError: true,
			errorMsg:  "invalid IP address",
		},
		{
			name: "client group with negative priority",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"negative": {Sources: []string{"192.168.0.0/16"}, Priority: -1},
				},
			},
			wantError: true,
			errorMsg:  "priority must be non-negative",
		},
		{
			name: "policy referencing unknown client group",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
				},
				Policies: []*PolicyOverride{
					{
						Match: &PolicyMatch{ClientGroup: "unknown"},
					},
				},
			},
			wantError: true,
			errorMsg:  "references unknown client group: unknown",
		},
		{
			name: "policy with invalid override JSON",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
				},
				Policies: []*PolicyOverride{
					{
						Match: &PolicyMatch{ClientGroup: "internal"},
						Overrides: map[string]json.RawMessage{
							"test": json.RawMessage(`{"invalid": "json"`),
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "override configuration for handler 'test' must be valid JSON",
		},
		{
			name: "duplicate client group in policies",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
				},
				Policies: []*PolicyOverride{
					{Match: &PolicyMatch{ClientGroup: "internal"}},
					{Match: &PolicyMatch{ClientGroup: "internal"}},
				},
			},
			wantError: true,
			errorMsg:  "client group 'internal' is used by multiple policies",
		},
		{
			name: "valid configuration",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "test"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16", "127.0.0.1"}, Priority: 10},
					"external": {Sources: []string{"0.0.0.0/0"}, Priority: 100},
				},
				Policies: []*PolicyOverride{
					{
						Match: &PolicyMatch{ClientGroup: "internal"},
						Overrides: map[string]json.RawMessage{
							"test": json.RawMessage(`{"upstreams": ["8.8.8.8:53"]}`),
						},
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.handler.logger = logger
			err := tt.handler.validateConfiguration()

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPolicyHandler_SourceValidation(t *testing.T) {
	handler := &PolicyHandler{}

	tests := []struct {
		name      string
		source    string
		wantError bool
	}{
		{"empty source", "", true},
		{"valid IPv4", "192.168.1.1", false},
		{"valid IPv6", "2001:db8::1", false},
		{"valid IPv4 CIDR", "192.168.0.0/16", false},
		{"valid IPv6 CIDR", "2001:db8::/32", false},
		{"invalid IP", "999.999.999.999", true},
		{"invalid CIDR", "192.168.0.0/999", true},
		{"invalid format", "not-an-ip", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.validateSource(tt.source)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestPolicyHandler_EnhancedProvision(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	tests := []struct {
		name      string
		handler   *PolicyHandler
		wantError bool
		errorMsg  string
	}{
		{
			name: "comprehensive validation failure",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "mock_handler"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"invalid": {Sources: []string{"bad-ip"}, Priority: 10},
				},
			},
			wantError: true,
			errorMsg:  "configuration validation failed",
		},
		{
			name: "valid configuration with enhanced validation",
			handler: &PolicyHandler{
				BaseHandler: json.RawMessage(`{"handler": "mock_handler"}`),
				ClientGroups: map[string]*client.ClientGroup{
					"internal": {Sources: []string{"192.168.0.0/16", "127.0.0.1"}, Priority: 10},
					"vpn":      {Sources: []string{"10.200.0.0/16"}, Priority: 20},
					"external": {Sources: []string{"0.0.0.0/0", "::/0"}, Priority: 100},
				},
				Policies: []*PolicyOverride{
					{
						Match: &PolicyMatch{ClientGroup: "internal"},
						Overrides: map[string]json.RawMessage{
							"mock_handler": json.RawMessage(`{"name": "internal"}`),
						},
					},
					{
						Match: &PolicyMatch{ClientGroup: "vpn"},
						Overrides: map[string]json.RawMessage{
							"mock_handler": json.RawMessage(`{"name": "vpn"}`),
						},
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.handler.Provision(ctx)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				// Verify that the handler was properly provisioned
				if tt.handler.classifier == nil {
					t.Error("Classifier should be initialized")
				}
				if tt.handler.baseHandler == nil {
					t.Error("Base handler should be initialized")
				}
			}
		})
	}
}

func TestPolicyHandler_ZoneMerging(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := &PolicyHandler{logger: logger}

	tests := []struct {
		name          string
		baseZones     interface{}
		overrideZones interface{}
		expectedZones int
		expectedZone1 string
		expectedZone2 string
	}{
		{
			name: "merge base and override zones",
			baseZones: []interface{}{
				map[string]interface{}{
					"type": "forward",
					"zone": "rockymtn.org.",
					"records": map[string]interface{}{
						"test.ext": map[string]interface{}{
							"type":  "A",
							"value": "192.168.1.20",
							"ttl":   300,
						},
					},
				},
			},
			overrideZones: []interface{}{
				map[string]interface{}{
					"type": "forward",
					"zone": "internal.rockymtn.org.",
					"records": map[string]interface{}{
						"api": map[string]interface{}{
							"type":  "A",
							"value": "203.0.113.10",
							"ttl":   300,
						},
					},
				},
			},
			expectedZones: 2,
			expectedZone1: "rockymtn.org.",
			expectedZone2: "internal.rockymtn.org.",
		},
		{
			name: "override zone replaces base zone with same name",
			baseZones: []interface{}{
				map[string]interface{}{
					"type": "forward",
					"zone": "example.com.",
					"records": map[string]interface{}{
						"old": map[string]interface{}{
							"type":  "A",
							"value": "1.1.1.1",
						},
					},
				},
			},
			overrideZones: []interface{}{
				map[string]interface{}{
					"type": "forward",
					"zone": "example.com.",
					"records": map[string]interface{}{
						"new": map[string]interface{}{
							"type":  "A",
							"value": "2.2.2.2",
						},
					},
				},
			},
			expectedZones: 1,
			expectedZone1: "example.com.",
		},
		{
			name:          "base zones not a slice",
			baseZones:     "not-a-slice",
			overrideZones: []interface{}{},
			expectedZones: 0,
		},
		{
			name:          "override zones not a slice",
			baseZones:     []interface{}{},
			overrideZones: "not-a-slice",
			expectedZones: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.mergeZones(tt.baseZones, tt.overrideZones)

			if resultSlice, ok := result.([]interface{}); ok {
				if len(resultSlice) != tt.expectedZones {
					t.Errorf("Expected %d zones, got %d", tt.expectedZones, len(resultSlice))
				}

				// Check specific zones if expected
				if tt.expectedZone1 != "" || tt.expectedZone2 != "" {
					foundZones := make(map[string]bool)
					for _, zone := range resultSlice {
						if zoneConfig, ok := zone.(map[string]interface{}); ok {
							if zoneName, exists := zoneConfig["zone"].(string); exists {
								foundZones[zoneName] = true
							}
						}
					}

					if tt.expectedZone1 != "" && !foundZones[tt.expectedZone1] {
						t.Errorf("Expected zone '%s' not found", tt.expectedZone1)
					}
					if tt.expectedZone2 != "" && !foundZones[tt.expectedZone2] {
						t.Errorf("Expected zone '%s' not found", tt.expectedZone2)
					}
				}
			} else if tt.expectedZones > 0 {
				t.Error("Expected result to be a slice")
			}
		})
	}
}

func TestPolicyHandler_ZoneOverrideIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := &PolicyHandler{logger: logger}

	// Simulate the exact scenario from the user's config
	baseConfig := json.RawMessage(`{
		"handler": "dns.zone.manager",
		"zones": [
			{
				"type": "forward",
				"zone": "rockymtn.org.",
				"records": {
					"test.internal": {
						"type": "A",
						"value": "192.168.1.10",
						"ttl": 300
					},
					"test.ext": {
						"type": "A",
						"value": "192.168.1.20",
						"ttl": 300
					}
				}
			}
		],
		"default_upstream": {
			"upstreams": ["8.8.8.8:53", "1.1.1.1:53"],
			"timeout": "5s",
			"protocol": "udp"
		}
	}`)

	overrides := map[string]json.RawMessage{
		"dns.zone.manager": json.RawMessage(`{
			"zones": [
				{
					"type": "forward",
					"zone": "internal.rockymtn.org.",
					"records": {
						"api": {
							"type": "A",
							"value": "203.0.113.10",
							"ttl": 300
						},
						"test": {
							"type": "A",
							"value": "192.168.1.11",
							"ttl": 300
						}
					},
					"upstream": {
						"upstreams": ["9.9.9.9:53"],
						"timeout": "2s"
					}
				}
			]
		}`),
	}

	result, err := handler.applyOverrides(baseConfig, overrides)
	if err != nil {
		t.Fatalf("applyOverrides failed: %v", err)
	}

	// Parse the result to verify both zones are present
	var resultConfig map[string]interface{}
	if err := json.Unmarshal(result, &resultConfig); err != nil {
		t.Fatalf("Failed to parse result config: %v", err)
	}

	zones, ok := resultConfig["zones"].([]interface{})
	if !ok {
		t.Fatal("zones field should be an array")
	}

	if len(zones) != 2 {
		t.Fatalf("Expected 2 zones (base + override), got %d", len(zones))
	}

	// Verify both zones are present
	foundZones := make(map[string]bool)
	for _, zone := range zones {
		if zoneConfig, ok := zone.(map[string]interface{}); ok {
			if zoneName, exists := zoneConfig["zone"].(string); exists {
				foundZones[zoneName] = true
			}
		}
	}

	if !foundZones["rockymtn.org."] {
		t.Error("Base zone 'rockymtn.org.' should be preserved")
	}

	if !foundZones["internal.rockymtn.org."] {
		t.Error("Override zone 'internal.rockymtn.org.' should be added")
	}

	// Verify default_upstream is preserved from base config
	if defaultUpstream, exists := resultConfig["default_upstream"]; !exists {
		t.Error("default_upstream should be preserved from base config")
	} else if upstream, ok := defaultUpstream.(map[string]interface{}); ok {
		if upstreams, ok := upstream["upstreams"].([]interface{}); ok {
			if len(upstreams) != 2 || upstreams[0] != "8.8.8.8:53" {
				t.Error("default_upstream should preserve base configuration")
			}
		}
	}
}
