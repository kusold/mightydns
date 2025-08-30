package mightydns

import (
	"testing"
)

func TestRegisterModule(t *testing.T) {
	testModule := &testModuleImpl{}

	defer func() {
		delete(modules, "test.module")
	}()

	RegisterModule(testModule)

	info, exists := GetModule("test.module")
	if !exists {
		t.Fatal("module was not registered")
	}

	if info.ID != "test.module" {
		t.Errorf("expected ID 'test.module', got '%s'", info.ID)
	}
}

func TestRegisterModulePanic(t *testing.T) {
	testModule := &testModuleImpl{}

	defer func() {
		delete(modules, "test.module")
	}()

	RegisterModule(testModule)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering duplicate module")
		}
	}()

	RegisterModule(testModule)
}

type testModuleImpl struct{}

func (t *testModuleImpl) MightyModule() ModuleInfo {
	return ModuleInfo{
		ID:  "test.module",
		New: func() Module { return new(testModuleImpl) },
	}
}
