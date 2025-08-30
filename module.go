package mightydns

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

type ModuleInfo struct {
	ID  string
	New func() Module
}

type Module interface {
	MightyModule() ModuleInfo
}

type Provisioner interface {
	Provision(ctx Context) error
}

type CleanerUpper interface {
	Cleanup() error
}

type Context interface {
	App(name string) (interface{}, error)
	Logger() *slog.Logger
	LoadModule(cfg interface{}, fieldName string) (interface{}, error)
}

// ModuleMap is a map that can unmarshal JSON into modules
type ModuleMap map[string]json.RawMessage

var modules = make(map[string]ModuleInfo)

func RegisterModule(mod Module) {
	info := mod.MightyModule()
	if _, exists := modules[info.ID]; exists {
		panic("module already registered: " + info.ID)
	}
	modules[info.ID] = info
}

func GetModule(id string) (ModuleInfo, bool) {
	info, exists := modules[id]
	return info, exists
}

func GetModules() map[string]ModuleInfo {
	result := make(map[string]ModuleInfo)
	for k, v := range modules {
		result[k] = v
	}
	return result
}

// LoadModule loads a module by ID from the given configuration
func LoadModule(ctx Context, cfg interface{}, fieldName string, moduleID string) (interface{}, error) {
	moduleInfo, exists := GetModule(moduleID)
	if !exists {
		return nil, fmt.Errorf("unknown module: %s", moduleID)
	}

	// Create new instance
	instance := moduleInfo.New()

	// If we have configuration data, unmarshal it into the instance
	if cfg != nil {
		// Get the field value using reflection-like approach
		// For now, we'll assume cfg is already the right data for the module
		cfgJSON, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("marshaling config for module %s: %w", moduleID, err)
		}

		err = json.Unmarshal(cfgJSON, instance)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling config for module %s: %w", moduleID, err)
		}
	}

	// Provision the module if it implements Provisioner
	if provisioner, ok := instance.(Provisioner); ok {
		err := provisioner.Provision(ctx)
		if err != nil {
			return nil, fmt.Errorf("provisioning module %s: %w", moduleID, err)
		}
	}

	return instance, nil
}
