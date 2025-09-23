package zone

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestNormalizeQName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com."},
		{"example.com.", "example.com."},
		{"EXAMPLE.COM", "example.com."},
		{"EXAMPLE.COM.", "example.com."},
		{"", "."},
	}

	for _, test := range tests {
		result := normalizeQName(test.input)
		if result != test.expected {
			t.Errorf("normalizeQName(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestIsSubdomain(t *testing.T) {
	tests := []struct {
		qname    string
		zone     string
		expected bool
	}{
		{"example.com.", "example.com.", true},
		{"www.example.com.", "example.com.", true},
		{"api.www.example.com.", "example.com.", true},
		{"other.com.", "example.com.", false},
		{"example.org.", "example.com.", false},
		{"notexample.com.", "example.com.", false},
	}

	for _, test := range tests {
		result := isSubdomain(test.qname, test.zone)
		if result != test.expected {
			t.Errorf("isSubdomain(%q, %q) = %v, expected %v", test.qname, test.zone, result, test.expected)
		}
	}
}

func TestForwardZoneMatch(t *testing.T) {
	records := map[string]DNSRecord{
		"api.example.com.": {Type: "A", Value: "192.168.1.10"},
	}
	zone := NewForwardZone("example.com.", records, nil)

	tests := []struct {
		qname    string
		expected bool
	}{
		{"example.com.", true},
		{"api.example.com.", true},
		{"www.example.com.", true},
		{"other.com.", false},
		{"example.org.", false},
	}

	for _, test := range tests {
		result := zone.Match(test.qname)
		if result != test.expected {
			t.Errorf("zone.Match(%q) = %v, expected %v", test.qname, result, test.expected)
		}
	}
}

type mockResponseWriter struct {
	msg        *dns.Msg
	remoteAddr net.Addr
}

func (w *mockResponseWriter) LocalAddr() net.Addr  { return nil }
func (w *mockResponseWriter) RemoteAddr() net.Addr { return w.remoteAddr }
func (w *mockResponseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m
	return nil
}
func (w *mockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (w *mockResponseWriter) Close() error              { return nil }
func (w *mockResponseWriter) TsigStatus() error         { return nil }
func (w *mockResponseWriter) TsigTimersOnly(bool)       {}
func (w *mockResponseWriter) Hijack()                   {}

func TestForwardZoneResolveLocalRecord(t *testing.T) {
	records := map[string]DNSRecord{
		"api.example.com.": {Type: "A", Value: "192.168.1.10", TTL: 300},
	}
	zone := NewForwardZone("example.com.", records, nil)

	r := new(dns.Msg)
	r.Id = 1234
	r.Question = []dns.Question{
		{Name: "api.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
	}

	w := &mockResponseWriter{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345},
	}

	ctx := context.Background()
	resolved, err := zone.Resolve(ctx, w, r, "test")

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !resolved {
		t.Fatal("Expected query to be resolved")
	}

	if w.msg == nil {
		t.Fatal("No response message written")
	}

	if w.msg.Id != 1234 {
		t.Errorf("Response ID = %d, expected 1234", w.msg.Id)
	}

	if len(w.msg.Answer) != 1 {
		t.Fatalf("Expected 1 answer, got %d", len(w.msg.Answer))
	}

	if aRecord, ok := w.msg.Answer[0].(*dns.A); ok {
		if !aRecord.A.Equal(net.ParseIP("192.168.1.10")) {
			t.Errorf("A record IP = %v, expected 192.168.1.10", aRecord.A)
		}
		if aRecord.Hdr.Ttl != 300 {
			t.Errorf("A record TTL = %d, expected 300", aRecord.Hdr.Ttl)
		}
	} else {
		t.Errorf("Expected A record, got %T", w.msg.Answer[0])
	}
}

func TestForwardZoneResolveNonMatchingZone(t *testing.T) {
	records := map[string]DNSRecord{
		"api.example.com.": {Type: "A", Value: "192.168.1.10"},
	}
	zone := NewForwardZone("example.com.", records, nil)

	r := new(dns.Msg)
	r.Question = []dns.Question{
		{Name: "api.other.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
	}

	w := &mockResponseWriter{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345},
	}

	ctx := context.Background()
	resolved, err := zone.Resolve(ctx, w, r, "test")

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved {
		t.Fatal("Expected query not to be resolved by this zone")
	}
}

func TestForwardZoneMergeRecords(t *testing.T) {
	baseRecords := map[string]DNSRecord{
		"api.example.com.": {Type: "A", Value: "192.168.1.10"},
		"web.example.com.": {Type: "A", Value: "192.168.1.20"},
	}

	baseZone := NewForwardZone("example.com.", baseRecords, nil)

	overrideRecords := map[string]DNSRecord{
		"api.example.com.": {Type: "A", Value: "203.0.113.10"}, // Override existing
		"new.example.com.": {Type: "A", Value: "203.0.113.20"}, // Add new
	}

	mergedZone := baseZone.MergeRecords(overrideRecords)
	mergedRecords := mergedZone.GetRecords()

	if len(mergedRecords) != 3 {
		t.Fatalf("Expected 3 records, got %d", len(mergedRecords))
	}

	if mergedRecords["api.example.com."].Value != "203.0.113.10" {
		t.Errorf("api record not overridden correctly: %s", mergedRecords["api.example.com."].Value)
	}

	if mergedRecords["web.example.com."].Value != "192.168.1.20" {
		t.Errorf("web record should be preserved: %s", mergedRecords["web.example.com."].Value)
	}

	if mergedRecords["new.example.com."].Value != "203.0.113.20" {
		t.Errorf("new record not added correctly: %s", mergedRecords["new.example.com."].Value)
	}
}

func TestZoneManager_ExtractClientGroup(t *testing.T) {
	zm := &ZoneManager{}

	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "default context",
			ctx:      context.Background(),
			expected: "default",
		},
		{
			name:     "context with client group",
			ctx:      context.WithValue(context.Background(), ClientGroupKey{}, "internal"),
			expected: "internal",
		},
		{
			name:     "context with empty client group",
			ctx:      context.WithValue(context.Background(), ClientGroupKey{}, ""),
			expected: "default",
		},
		{
			name:     "context with non-string client group",
			ctx:      context.WithValue(context.Background(), ClientGroupKey{}, 123),
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := zm.extractClientGroup(tt.ctx)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
