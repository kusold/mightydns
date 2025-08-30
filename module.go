package mightydns

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
}

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
