package mightydns

import (
	"encoding/json"
)

type Config struct {
	Admin   *AdminConfig           `json:"admin,omitempty"`
	Logging *LoggingConfig         `json:"logging,omitempty"`
	Apps    map[string]interface{} `json:"apps,omitempty"`
}

type App interface {
	Start() error
	Stop() error
}

// Run runs the given config, replacing any existing config.
func Run(cfg *Config) error {
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return Load(cfgJSON, true)
}

// Load loads the given config JSON and runs it only
// if it is different from the current config or
// forceReload is true.
func Load(cfgJSON []byte, forceReload bool) error {
	return nil
}
