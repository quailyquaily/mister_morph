package agent

import (
	"context"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

type engineRegistryOwnerTool struct {
	name string
}

func (t *engineRegistryOwnerTool) Name() string          { return t.name }
func (*engineRegistryOwnerTool) Description() string     { return "host-owned tool" }
func (*engineRegistryOwnerTool) ParameterSchema() string { return `{}` }
func (*engineRegistryOwnerTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestNewDoesNotModifyCallerRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	hostTool := &engineRegistryOwnerTool{name: "host_owned"}
	if err := reg.Register(hostTool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	hostSpawnTool := &engineRegistryOwnerTool{name: spawnToolName}
	if err := reg.Register(hostSpawnTool); err != nil {
		t.Fatalf("Register(host spawn) error = %v", err)
	}

	engine := New(nil, reg, Config{}, DefaultPromptSpec())

	gotSpawn, ok := reg.Get(spawnToolName)
	if !ok || gotSpawn != hostSpawnTool {
		t.Fatal("New() replaced a caller-owned tool")
	}
	got, ok := reg.Get(hostTool.Name())
	if !ok || got != hostTool {
		t.Fatalf("caller registry tool = %v, want original tool", got)
	}
	engineSpawn, ok := engine.registry.Get(spawnToolName)
	if !ok {
		t.Fatal("engine-private spawn tool was not registered in engine registry")
	}
	if engineSpawn == hostSpawnTool {
		t.Fatal("engine registry retained caller spawn tool instead of its private tool")
	}
}

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
