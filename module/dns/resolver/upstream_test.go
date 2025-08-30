package resolver

import (
	"fmt"
	"log/slog"
	"testing"
	"time"
)

type mockContext struct{}

func (mockContext) App(name string) (interface{}, error) { return nil, nil }
func (mockContext) Logger() *slog.Logger                 { return slog.Default() }
func (mockContext) LoadModule(cfg interface{}, fieldName string) (interface{}, error) {
	return nil, fmt.Errorf("module loading not supported in mock context")
}

func TestUpstreamResolver_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  UpstreamResolver
		wantErr bool
	}{
		{
			name:    "default config",
			config:  UpstreamResolver{},
			wantErr: false,
		},
		{
			name: "custom upstreams",
			config: UpstreamResolver{
				Upstreams: []string{"8.8.8.8:53", "1.1.1.1:53"},
				Timeout:   "10s",
				Protocol:  "tcp",
			},
			wantErr: false,
		},
		{
			name: "invalid timeout",
			config: UpstreamResolver{
				Timeout: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			config: UpstreamResolver{
				Protocol: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid upstream address",
			config: UpstreamResolver{
				Upstreams: []string{"invalid address"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &tt.config
			err := u.Provision(mockContext{})
			if (err != nil) != tt.wantErr {
				t.Errorf("UpstreamResolver.Provision() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpstreamResolver_ModuleInfo(t *testing.T) {
	u := &UpstreamResolver{}
	info := u.MightyModule()

	if info.ID != "dns.resolver.upstream" {
		t.Errorf("Expected module ID 'dns.resolver.upstream', got %s", info.ID)
	}

	if info.New == nil {
		t.Error("Expected New function to be set")
	}

	newModule := info.New()
	if _, ok := newModule.(*UpstreamResolver); !ok {
		t.Error("Expected New() to return *UpstreamResolver")
	}
}

func TestUpstreamResolver_DefaultValues(t *testing.T) {
	u := &UpstreamResolver{}
	err := u.Provision(mockContext{})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if len(u.Upstreams) != 2 {
		t.Errorf("Expected 2 default upstreams, got %d", len(u.Upstreams))
	}

	expectedUpstreams := []string{"8.8.8.8:53", "1.1.1.1:53"}
	for i, expected := range expectedUpstreams {
		if u.Upstreams[i] != expected {
			t.Errorf("Expected upstream %d to be %s, got %s", i, expected, u.Upstreams[i])
		}
	}

	if u.timeout != 5*time.Second {
		t.Errorf("Expected default timeout to be 5s, got %v", u.timeout)
	}

	if u.protocol != "udp" {
		t.Errorf("Expected default protocol to be udp, got %s", u.protocol)
	}
}
