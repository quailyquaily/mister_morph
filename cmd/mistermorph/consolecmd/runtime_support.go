package consolecmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func buildConsoleRegistriesFromReader(ctx context.Context, logger *slog.Logger, r *viper.Viper) (*tools.Registry, *tools.Registry, *mcphost.Host, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg, err := toolsutil.StaticRegistryConfigFromReader(r)
	if err != nil {
		return nil, nil, nil, err
	}
	baseRegistry := tools.NewRegistry()
	awarenessRegistry := tools.NewRegistry()
	toolsutil.RegisterStaticTools(baseRegistry, cfg, nil, nil)
	cfg.Common.Awareness = true
	toolsutil.RegisterStaticTools(awarenessRegistry, cfg, nil, nil)

	host, err := mcphost.RegisterTools(ctx, mcphost.MCPConfigFromReader(r), baseRegistry, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("register console MCP tools: %w", err)
	}
	if err := mcphost.RegisterHostTools(host, awarenessRegistry); err != nil {
		return nil, nil, nil, fmt.Errorf("register console awareness MCP tools: %w", err)
	}
	return baseRegistry, awarenessRegistry, host, nil
}

func consoleEngineToolsConfigFromReader(r interface {
	GetBool(string) bool
	GetStringSlice(string) []string
}) agent.EngineToolsConfig {
	if r == nil {
		return agent.EngineToolsConfig{}
	}
	return agent.EngineToolsConfig{
		SpawnEnabled:    r.GetBool("tools.spawn.enabled"),
		ACPSpawnEnabled: r.GetBool("tools.acp_spawn.enabled"),
		CoderEnabled:    r.GetBool("tools.coder.enabled"),
		CoderPathExtra:  append([]string(nil), r.GetStringSlice("tools.coder.path_extra")...),
	}
}

func consoleContactsFailureCooldownFromReader(r *viper.Viper) time.Duration {
	if r == nil {
		return 72 * time.Hour
	}
	if r.IsSet("contacts.proactive.failure_cooldown") {
		if v := r.GetDuration("contacts.proactive.failure_cooldown"); v > 0 {
			return v
		}
	}
	return 72 * time.Hour
}
