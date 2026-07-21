package agent

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/tools"
)

type EngineToolsConfig struct {
	SpawnEnabled    bool
	ACPSpawnEnabled bool
	CoderEnabled    bool
	ToolTriggers    map[string]bool
	PathRoots       pathroots.PathRoots
	CoderPathExtra  []string
}

func DefaultEngineToolsConfig() EngineToolsConfig {
	return EngineToolsConfig{
		SpawnEnabled:    true,
		ACPSpawnEnabled: false,
	}
}

type spawnToolDeps struct {
	LookupTool   func(name string) (tools.Tool, bool)
	DefaultModel string
	Runner       SubtaskRunner
}

type acpSpawnToolDeps struct {
	LookupAgent func(name string) (acpclient.AgentConfig, bool)
	Runner      SubtaskRunner
	RunPrompt   func(ctx context.Context, cfg acpclient.PreparedAgentConfig, req acpclient.RunRequest) (acpclient.RunResult, error)
}

type coderToolDeps struct {
	Runner    SubtaskRunner
	RunCLI    coderCLIRunFunc
	Roots     pathroots.PathRoots
	PathExtra []string
}

func registerEngineTools(reg *tools.Registry, cfg EngineToolsConfig, spawnDeps spawnToolDeps, acpDeps acpSpawnToolDeps, coderDeps coderToolDeps) {
	if reg == nil {
		return
	}
	if cfg.SpawnEnabled || cfg.ToolTriggers[spawnToolName] {
		if err := reg.Replace(newSpawnTool(spawnDeps)); err != nil {
			panic(err)
		}
	}
	if cfg.ACPSpawnEnabled || cfg.ToolTriggers[acpSpawnToolName] {
		if err := reg.Replace(newACPSpawnTool(acpDeps)); err != nil {
			panic(err)
		}
	}
	if cfg.CoderEnabled || cfg.ToolTriggers[coderToolName] {
		coderDeps.Roots = cfg.PathRoots
		coderDeps.PathExtra = append([]string(nil), cfg.CoderPathExtra...)
		if err := reg.Replace(newCoderTool(coderDeps)); err != nil {
			panic(err)
		}
	}
}
