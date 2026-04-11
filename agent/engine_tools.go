package agent

import (
	"context"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/tools"
)

type EngineToolsConfig struct {
	SpawnEnabled    bool
	ACPSpawnEnabled bool
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

func registerEngineTools(reg *tools.Registry, cfg EngineToolsConfig, spawnDeps spawnToolDeps, acpDeps acpSpawnToolDeps) {
	if reg == nil {
		return
	}
	if cfg.SpawnEnabled {
		reg.Register(newSpawnTool(spawnDeps))
	}
	if cfg.ACPSpawnEnabled {
		reg.Register(newACPSpawnTool(acpDeps))
	}
}

func lookupACPAgent(configs []acpclient.AgentConfig, name string) (acpclient.AgentConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return acpclient.AgentConfig{}, false
	}
	for _, cfg := range configs {
		if strings.ToLower(strings.TrimSpace(cfg.Name)) != name {
			continue
		}
		return cfg, true
	}
	return acpclient.AgentConfig{}, false
}
