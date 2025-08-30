package policy

import (
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/miekg/dns"

	_ "github.com/kusold/mightydns/module/dns/resolver" // Import upstream resolver
)

func TestPolicyHandler_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	// Test the complete policy configuration with upstream resolver
	configJSON := `{
		"handler": "policy",
		"base_handler": {
			"handler": "dns.resolver.upstream",
			"upstreams": ["1.1.1.1:53"],
			"timeout": "5s"
		},
		"client_groups": {
			"internal": {
				"sources": ["192.168.0.0/16", "10.0.0.0/8", "127.0.0.1"],
				"priority": 10
			},
			"external": {
				"sources": ["0.0.0.0/0"],
				"priority": 100
			}
		},
		"policies": [
			{
				"match": {"client_group": "internal"},
				"overrides": {
					"dns.resolver.upstream": {
						"upstreams": ["8.8.8.8:53"],
						"timeout": "2s"
					}
				}
			}
		]
	}`

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Create and provision the policy handler
	handler := &PolicyHandler{}
	if err := json.Unmarshal([]byte(configJSON), handler); err != nil {
		t.Fatalf("Failed to unmarshal handler config: %v", err)
	}

	if err := handler.Provision(ctx); err != nil {
		t.Fatalf("Failed to provision handler: %v", err)
	}

	// Test internal client
	t.Run("internal client classification", func(t *testing.T) {
		w := &mockResponseWriter{
			addr: &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345},
		}

		req := new(dns.Msg)
		req.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

		// This should route to the internal policy (overridden upstream)
		// We can't actually test the upstream without a real server, but we can verify routing
		clientGroup := handler.classifier.ClassifyDNSRequest(w)
		if clientGroup != "internal" {
			t.Errorf("Expected internal client group, got %s", clientGroup)
		}

		// Verify policy handler exists for internal group
		if _, exists := handler.policyTrees["internal"]; !exists {
			t.Error("Expected policy handler for internal group")
		}
	})

	// Test external client
	t.Run("external client classification", func(t *testing.T) {
		w := &mockResponseWriter{
			addr: &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 12345},
		}

		clientGroup := handler.classifier.ClassifyDNSRequest(w)
		if clientGroup != "external" {
			t.Errorf("Expected external client group, got %s", clientGroup)
		}

		// External client should use base handler (no policy override)
		if _, exists := handler.policyTrees["external"]; exists {
			t.Error("External client should not have a policy override")
		}
	})

	// Test priority ordering
	t.Run("priority ordering", func(t *testing.T) {
		// Test IP that could match both internal (10.0.0.0/8) and catch-all (0.0.0.0/0)
		w := &mockResponseWriter{
			addr: &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345},
		}

		clientGroup := handler.classifier.ClassifyDNSRequest(w)
		if clientGroup != "internal" {
			t.Errorf("Expected higher priority 'internal' group, got %s", clientGroup)
		}
	})

	// Test cleanup
	t.Run("cleanup", func(t *testing.T) {
		if err := handler.Cleanup(); err != nil {
			t.Errorf("Cleanup failed: %v", err)
		}
	})
}

func TestPolicyHandler_ConfigValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := &mockContext{logger: logger}

	tests := []struct {
		name      string
		config    string
		wantError bool
		errorMsg  string
	}{
		{
			name: "missing client group reference",
			config: `{
				"handler": "policy",
				"base_handler": {"handler": "mock_handler"},
				"client_groups": {
					"internal": {"sources": ["192.168.0.0/16"], "priority": 10}
				},
				"policies": [
					{
						"match": {"client_group": "nonexistent"},
						"overrides": {"mock_handler": {"name": "test"}}
					}
				]
			}`,
			wantError: true,
			errorMsg:  "unknown client group",
		},
		{
			name: "policy without match",
			config: `{
				"handler": "policy",
				"base_handler": {"handler": "mock_handler"},
				"client_groups": {
					"internal": {"sources": ["192.168.0.0/16"], "priority": 10}
				},
				"policies": [
					{
						"overrides": {"mock_handler": {"name": "test"}}
					}
				]
			}`,
			wantError: true,
			errorMsg:  "must specify a client_group",
		},
		{
			name: "valid config with no overrides",
			config: `{
				"handler": "policy",
				"base_handler": {"handler": "mock_handler"},
				"client_groups": {
					"internal": {"sources": ["192.168.0.0/16"], "priority": 10}
				},
				"policies": [
					{
						"match": {"client_group": "internal"},
						"overrides": {}
					}
				]
			}`,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &PolicyHandler{}
			if err := json.Unmarshal([]byte(tt.config), handler); err != nil {
				t.Fatalf("Failed to unmarshal config: %v", err)
			}

			err := handler.Provision(ctx)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !stringContains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
