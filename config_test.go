package mightydns

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	configJSON := `{
		"admin": {
			"listen": ":2019"
		},
		"logging": {
			"level": "info",
			"format": "json"
		},
		"apps": {
			"dns": {
				"servers": {}
			}
		}
	}`

	cfg, err := LoadConfig([]byte(configJSON))
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Admin == nil {
		t.Error("admin config is nil")
	}

	if cfg.Admin.Listen != ":2019" {
		t.Errorf("expected admin listen ':2019', got '%s'", cfg.Admin.Listen)
	}

	if cfg.Logging == nil {
		t.Error("logging config is nil")
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("expected logging level 'info', got '%s'", cfg.Logging.Level)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	invalidJSON := `{invalid json`

	_, err := LoadConfig([]byte(invalidJSON))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
