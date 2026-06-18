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

func TestRegisterEngineToolsPassesCoderPathExtra(t *testing.T) {
	reg := tools.NewRegistry()
	registerEngineTools(reg, EngineToolsConfig{
		CoderEnabled:   true,
		CoderPathExtra: []string{"/opt/coder/bin"},
	}, spawnToolDeps{}, acpSpawnToolDeps{}, coderToolDeps{})

	tool, ok := reg.Get("coder")
	if !ok {
		t.Fatal("coder should be registered")
	}
	coder, ok := tool.(*coderTool)
	if !ok {
		t.Fatalf("tool type = %T, want *coderTool", tool)
	}
	if len(coder.deps.PathExtra) != 1 || coder.deps.PathExtra[0] != "/opt/coder/bin" {
		t.Fatalf("PathExtra = %#v, want /opt/coder/bin", coder.deps.PathExtra)
	}
}
