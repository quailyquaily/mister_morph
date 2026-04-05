package agent

import "github.com/quailyquaily/mistermorph/tools"

type EngineToolsConfig struct {
	SpawnEnabled bool
}

func DefaultEngineToolsConfig() EngineToolsConfig {
	return EngineToolsConfig{
		SpawnEnabled: true,
	}
}

type spawnToolDeps struct {
	LookupTool   func(name string) (tools.Tool, bool)
	DefaultModel string
	Runner       SubtaskRunner
}

func registerEngineTools(reg *tools.Registry, cfg EngineToolsConfig, deps spawnToolDeps) {
	if reg == nil {
		return
	}
	if cfg.SpawnEnabled {
		reg.Register(newSpawnTool(deps))
	}
}
