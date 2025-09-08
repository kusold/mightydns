package client

import (
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/miekg/dns"
)

func TestNewClientClassifier(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	groups := map[string]*ClientGroup{
		"internal": {
			Sources:  []string{"192.168.0.0/16", "10.0.0.0/8"},
			Priority: 10,
		},
	}

	classifier := NewClientClassifier(groups, logger)
	if classifier == nil {
		t.Fatal("NewClientClassifier returned nil")
	}
	if len(classifier.Groups) != len(groups) {
		t.Error("Groups not set correctly")
	}
	if classifier.logger == nil {
		t.Error("Logger not set")
	}
}

func TestClientClassifier_Provision(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tests := []struct {
		name      string
		groups    map[string]*ClientGroup
		wantError bool
	}{
		{
			name:      "empty groups",
			groups:    map[string]*ClientGroup{},
			wantError: true,
		},
		{
			name: "valid CIDR and IP",
			groups: map[string]*ClientGroup{
				"internal": {
					Sources:  []string{"192.168.0.0/16", "127.0.0.1"},
					Priority: 10,
				},
			},
			wantError: false,
		},
		{
			name: "invalid CIDR",
			groups: map[string]*ClientGroup{
				"bad": {
					Sources:  []string{"192.168.0.0/99"},
					Priority: 10,
				},
			},
			wantError: true,
		},
		{
			name: "invalid IP",
			groups: map[string]*ClientGroup{
				"bad": {
					Sources:  []string{"not.an.ip"},
					Priority: 10,
				},
			},
			wantError: true,
		},
		{
			name: "multiple groups with priorities",
			groups: map[string]*ClientGroup{
				"internal": {
					Sources:  []string{"192.168.0.0/16"},
					Priority: 10,
				},
				"external": {
					Sources:  []string{"0.0.0.0/0"},
					Priority: 100,
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifier := NewClientClassifier(tt.groups, logger)
			err := classifier.Provision()

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.wantError {
				// Verify compiled groups were created
				if len(classifier.compiled) != len(tt.groups) {
					t.Errorf("Expected %d compiled groups, got %d", len(tt.groups), len(classifier.compiled))
				}
			}
		})
	}
}

func TestClientClassifier_ClassifyIP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	groups := map[string]*ClientGroup{
		"internal": {
			Sources:  []string{"192.168.0.0/16", "10.0.0.0/8", "127.0.0.1"},
			Priority: 10,
		},
		"dmz": {
			Sources:  []string{"172.16.0.0/12"},
			Priority: 20,
		},
		"external": {
			Sources:  []string{"0.0.0.0/0"},
			Priority: 100,
		},
	}

	classifier := NewClientClassifier(groups, logger)
	if err := classifier.Provision(); err != nil {
		t.Fatalf("Failed to provision classifier: %v", err)
	}

	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{
			name:     "localhost",
			ip:       "127.0.0.1",
			expected: "internal",
		},
		{
			name:     "private network 192.168",
			ip:       "192.168.1.100",
			expected: "internal",
		},
		{
			name:     "private network 10.x",
			ip:       "10.0.0.1",
			expected: "internal",
		},
		{
			name:     "DMZ network",
			ip:       "172.16.5.10",
			expected: "dmz",
		},
		{
			name:     "external IP",
			ip:       "8.8.8.8",
			expected: "external",
		},
		{
			name:     "IPv6 loopback",
			ip:       "::1",
			expected: "", // IPv6 doesn't match IPv4 CIDR blocks
		},
		{
			name:     "nil IP",
			ip:       "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ip net.IP
			if tt.ip != "" {
				ip = net.ParseIP(tt.ip)
				if ip == nil {
					t.Fatalf("Failed to parse test IP: %s", tt.ip)
				}
			}

			result := classifier.ClassifyIP(ip)
			if result != tt.expected {
				t.Errorf("ClassifyIP(%s) = %q, want %q", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestClientClassifier_PriorityOrdering(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Test that priority ordering works correctly
	groups := map[string]*ClientGroup{
		"specific": {
			Sources:  []string{"192.168.1.0/24"},
			Priority: 5, // Higher priority (lower number)
		},
		"general": {
			Sources:  []string{"192.168.0.0/16"},
			Priority: 10, // Lower priority (higher number)
		},
	}

	classifier := NewClientClassifier(groups, logger)
	if err := classifier.Provision(); err != nil {
		t.Fatalf("Failed to provision classifier: %v", err)
	}

	// IP that matches both groups should match the higher priority one
	ip := net.ParseIP("192.168.1.50")
	result := classifier.ClassifyIP(ip)

	if result != "specific" {
		t.Errorf("Expected IP 192.168.1.50 to match 'specific' group (priority 5), got '%s'", result)
	}
}

func TestClientClassifier_ExtractClientIP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	classifier := NewClientClassifier(map[string]*ClientGroup{}, logger)

	tests := []struct {
		name     string
		addr     net.Addr
		expected string
	}{
		{
			name:     "UDP address",
			addr:     &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345},
			expected: "192.168.1.1",
		},
		{
			name:     "TCP address",
			addr:     &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 54321},
			expected: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock DNS ResponseWriter
			w := &mockResponseWriter{addr: tt.addr}

			ip := classifier.ExtractClientIP(w)
			if ip == nil {
				t.Fatal("ExtractClientIP returned nil")
			}

			if ip.String() != tt.expected {
				t.Errorf("ExtractClientIP() = %s, want %s", ip.String(), tt.expected)
			}
		})
	}
}

func TestClientClassifier_ClassifyDNSRequest(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	groups := map[string]*ClientGroup{
		"internal": {
			Sources:  []string{"192.168.0.0/16"},
			Priority: 10,
		},
	}

	classifier := NewClientClassifier(groups, logger)
	if err := classifier.Provision(); err != nil {
		t.Fatalf("Failed to provision classifier: %v", err)
	}

	w := &mockResponseWriter{addr: &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}}
	result := classifier.ClassifyDNSRequest(w)

	if result != "internal" {
		t.Errorf("ClassifyDNSRequest() = %q, want %q", result, "internal")
	}
}

func TestClientClassifier_GetGroupNames(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	groups := map[string]*ClientGroup{
		"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
		"external": {Sources: []string{"0.0.0.0/0"}, Priority: 100},
	}

	classifier := NewClientClassifier(groups, logger)
	names := classifier.GetGroupNames()

	if len(names) != 2 {
		t.Errorf("Expected 2 group names, got %d", len(names))
	}

	// Check that both names are present (order doesn't matter)
	found := make(map[string]bool)
	for _, name := range names {
		found[name] = true
	}

	if !found["internal"] || !found["external"] {
		t.Errorf("Missing expected group names. Got: %v", names)
	}
}

func TestClientClassifier_GetGroupPriority(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	groups := map[string]*ClientGroup{
		"internal": {Sources: []string{"192.168.0.0/16"}, Priority: 10},
	}

	classifier := NewClientClassifier(groups, logger)
	if err := classifier.Provision(); err != nil {
		t.Fatalf("Failed to provision classifier: %v", err)
	}

	// Test existing group
	priority := classifier.GetGroupPriority("internal")
	if priority != 10 {
		t.Errorf("GetGroupPriority('internal') = %d, want 10", priority)
	}

	// Test non-existing group
	priority = classifier.GetGroupPriority("nonexistent")
	if priority != -1 {
		t.Errorf("GetGroupPriority('nonexistent') = %d, want -1", priority)
	}
}

// mockResponseWriter implements dns.ResponseWriter for testing
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
