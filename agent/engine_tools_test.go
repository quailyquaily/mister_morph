package agent

import (
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

func TestRegisterEngineToolsExplicitSpawn(t *testing.T) {
	reg := tools.NewRegistry()
	registerEngineTools(reg, EngineToolsConfig{
		SpawnEnabled: false,
		ToolTriggers: map[string]bool{"spawn": true},
	}, spawnToolDeps{}, acpSpawnToolDeps{}, coderToolDeps{})

	if _, ok := reg.Get("spawn"); !ok {
		t.Fatalf("spawn not registered")
	}
}

func TestRegisterEngineToolsCoderSwitch(t *testing.T) {
	reg := tools.NewRegistry()
	registerEngineTools(reg, EngineToolsConfig{}, spawnToolDeps{}, acpSpawnToolDeps{}, coderToolDeps{})
	if _, ok := reg.Get("coder"); ok {
		t.Fatal("coder should not be registered by default")
	}

	reg = tools.NewRegistry()
	registerEngineTools(reg, EngineToolsConfig{CoderEnabled: true}, spawnToolDeps{}, acpSpawnToolDeps{}, coderToolDeps{})
	if _, ok := reg.Get("coder"); !ok {
		t.Fatal("coder should be registered when enabled")
	}

	reg = tools.NewRegistry()
	registerEngineTools(reg, EngineToolsConfig{
		ToolTriggers: map[string]bool{"coder": true},
	}, spawnToolDeps{}, acpSpawnToolDeps{}, coderToolDeps{})
	if _, ok := reg.Get("coder"); !ok {
		t.Fatal("coder should be registered by explicit trigger")
	}
}
