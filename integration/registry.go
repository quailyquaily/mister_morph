package integration

import (
	"log/slog"

	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
)

func (rt *Runtime) buildRegistry(cfg toolsutil.StaticRegistryConfig, logger *slog.Logger) *tools.Registry {
	r := tools.NewRegistry()
	rt.registerStaticTools(r, cfg, logger, false, nil)
	return r
}

func (rt *Runtime) buildAwarenessRegistry(cfg toolsutil.StaticRegistryConfig, logger *slog.Logger) *tools.Registry {
	r := tools.NewRegistry()
	rt.registerStaticTools(r, cfg, logger, true, nil)
	return r
}

func (rt *Runtime) registerStaticTools(registry *tools.Registry, cfg toolsutil.StaticRegistryConfig, logger *slog.Logger, awareness bool, triggers map[string]bool) {
	if rt == nil || registry == nil {
		return
	}
	selectedBuiltinTools := make(map[string]bool, len(rt.builtinToolNames))
	for _, name := range rt.builtinToolNames {
		selectedBuiltinTools[name] = true
		if !toolsutil.IsKnownBuiltinToolName(name) && logger != nil {
			logger.Warn("unknown_builtin_tool_name", "name", name)
		}
	}
	cfg.Common.Awareness = awareness
	toolsutil.RegisterStaticTools(registry, cfg, selectedBuiltinTools, triggers)
}
