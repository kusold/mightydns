package mightydns

import (
	"encoding/json"
)

type AdminConfig struct {
	Listen string `json:"listen,omitempty"`
}

type LoggingConfig struct {
	Level   string `json:"level,omitempty"`
	Handler string `json:"handler,omitempty"`
}

func (c *Config) Validate() error {
	return nil
}

func LoadConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, cfg.Validate()
}
