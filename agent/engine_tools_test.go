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
	}, spawnToolDeps{}, acpSpawnToolDeps{})

	if _, ok := reg.Get("spawn"); !ok {
		t.Fatalf("spawn not registered")
	}
}
