package mightydns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

type Config struct {
	Admin   *AdminConfig   `json:"admin,omitempty"`
	Logging *LoggingConfig `json:"logging,omitempty"`
	Apps    ModuleMap      `json:"apps,omitempty"`

	// Internal fields
	apps       map[string]App
	cancelFunc context.CancelFunc
	logger     *slog.Logger
}

type App interface {
	Start() error
	Stop() error
}

// Global state
var (
	currentConfig *Config
	configMu      sync.RWMutex
)

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
	// If no config provided, create a default DNS server config
	if len(cfgJSON) == 0 || string(cfgJSON) == "null" {
		defaultConfig := getDefaultConfig()
		return Run(defaultConfig)
	}

	// Parse the configuration
	var newCfg Config
	if err := json.Unmarshal(cfgJSON, &newCfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Stop any existing configuration
	configMu.Lock()
	defer configMu.Unlock()

	if currentConfig != nil {
		stopConfig(currentConfig)
	}

	// Start the new configuration
	err := startConfig(&newCfg)
	if err != nil {
		return fmt.Errorf("starting config: %w", err)
	}

	currentConfig = &newCfg
	return nil
}

// getDefaultConfig returns a default configuration with a basic DNS server
func getDefaultConfig() *Config {
	return &Config{
		Logging: &LoggingConfig{
			// Level:   "INFO",
			Level:   "DEBUG",
			Handler: "logger.text",
		},
		Apps: ModuleMap{
			"dns": json.RawMessage(`{
				"servers": {
					"main": {
						"listen": [":53"],
						"protocol": ["udp", "tcp"],
						"handler": {
							"handler": "dns.resolver.upstream"
						}
					}
				}
			}`),
		},
	}
}

// startConfig provisions and starts all apps in the configuration
func startConfig(cfg *Config) error {
	// Setup logging first
	if err := SetupLogging(cfg.Logging); err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}

	cfg.logger = Logger()
	cfg.apps = make(map[string]App)

	// Create a cancellable context for this config
	ctx, cancel := context.WithCancel(context.Background())
	cfg.cancelFunc = cancel

	// Create the main context for app provisioning
	appCtx := &appContext{
		config: cfg,
		logger: cfg.logger,
		ctx:    ctx,
	}

	// Load and provision each app
	for appName, appConfigRaw := range cfg.Apps {
		cfg.logger.Info("loading app", "name", appName)

		// Parse the app config to get the module type
		var appConfig map[string]interface{}
		if err := json.Unmarshal(appConfigRaw, &appConfig); err != nil {
			return fmt.Errorf("parsing app config for %s: %w", appName, err)
		}

		// Load the app module (app name is the module ID)
		appModule, err := LoadModule(appCtx, appConfig, "", appName)
		if err != nil {
			return fmt.Errorf("loading app %s: %w", appName, err)
		}

		app, ok := appModule.(App)
		if !ok {
			return fmt.Errorf("module %s does not implement App interface", appName)
		}

		cfg.apps[appName] = app
	}

	// Start all apps
	for appName, app := range cfg.apps {
		cfg.logger.Info("starting app", "name", appName)
		if err := app.Start(); err != nil {
			return fmt.Errorf("starting app %s: %w", appName, err)
		}
	}

	cfg.logger.Info("all apps started successfully")
	return nil
}

// stopConfig stops all apps and cleans up the configuration
func stopConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	if cfg.logger != nil {
		cfg.logger.Info("stopping configuration")
	}

	// Stop all apps
	for appName, app := range cfg.apps {
		if cfg.logger != nil {
			cfg.logger.Info("stopping app", "name", appName)
		}
		if err := app.Stop(); err != nil && cfg.logger != nil {
			cfg.logger.Error("error stopping app", "name", appName, "error", err)
		}
	}

	// Cancel the context to clean up modules
	if cfg.cancelFunc != nil {
		cfg.cancelFunc()
	}
}

// Stop stops the current configuration
func Stop() error {
	configMu.Lock()
	defer configMu.Unlock()

	if currentConfig != nil {
		stopConfig(currentConfig)
		currentConfig = nil
	}

	return nil
}

// appContext implements the Context interface for app provisioning
type appContext struct {
	config *Config
	logger *slog.Logger
	ctx    context.Context
}

func (c *appContext) App(name string) (interface{}, error) {
	if c.config == nil || c.config.apps == nil {
		return nil, fmt.Errorf("app %s not found", name)
	}

	app, exists := c.config.apps[name]
	if !exists {
		return nil, fmt.Errorf("app %s not found", name)
	}

	return app, nil
}

func (c *appContext) Logger() *slog.Logger {
	return c.logger
}

func (c *appContext) LoadModule(cfg interface{}, fieldName string) (interface{}, error) {
	// This is a simplified version - in a full implementation, you'd parse
	// the fieldName to extract the module ID from the configuration
	// For now, we'll assume the cfg contains a "handler" field with the module ID
	if configMap, ok := cfg.(map[string]interface{}); ok {
		if handlerID, ok := configMap["handler"].(string); ok {
			return LoadModule(c, cfg, fieldName, handlerID)
		}
	}
	return nil, fmt.Errorf("cannot determine module ID for field %s", fieldName)
}
