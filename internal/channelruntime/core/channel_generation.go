package core

import (
	"context"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
)

func BootstrapRuntimeGenerationManager(ctx context.Context, dependencies depsutil.CommonDependencies, opts ChannelBootstrapOptions) (*RuntimeGenerationManager, error) {
	source := dependencies.RuntimeConfigSource
	if source == nil {
		bundle, err := BootstrapChannelRuntime(ctx, dependencies, opts)
		if err != nil {
			return nil, err
		}
		return NewStaticRuntimeGenerationManager(bundle, dependencies.AgentSettingsReader), nil
	}
	build := func(buildCtx context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
		generationDependencies, cleanupDependencies, err := depsutil.BuildGenerationDependencies(buildCtx, dependencies, reader)
		if err != nil {
			return ChannelRuntimeBundle{}, err
		}
		generationOptions := opts
		engineTools := engineToolsConfigFromReader(reader, opts.EngineToolsConfig)
		generationOptions.EngineToolsConfig = &engineTools
		bundle, err := BootstrapChannelRuntime(buildCtx, generationDependencies, generationOptions)
		if err != nil {
			cleanupDependencies()
			return ChannelRuntimeBundle{}, err
		}
		cleanupBundle := bundle.Cleanup
		var cleanupOnce sync.Once
		bundle.Cleanup = func() {
			cleanupOnce.Do(func() {
				if cleanupBundle != nil {
					cleanupBundle()
				}
				cleanupDependencies()
			})
		}
		return bundle, nil
	}
	return NewRuntimeGenerationManager(ctx, RuntimeGenerationManagerOptions{
		Source: source,
		Build:  build,
		Logger: opts.Logger,
	})
}

func engineToolsConfigFromReader(reader agentsettings.Reader, fallback *agent.EngineToolsConfig) agent.EngineToolsConfig {
	config := agent.DefaultEngineToolsConfig()
	if fallback != nil {
		config = *fallback
	}
	if reader == nil {
		return config
	}
	config.SpawnEnabled = reader.GetBool("tools.spawn.enabled")
	config.ACPSpawnEnabled = reader.GetBool("tools.acp_spawn.enabled")
	config.CoderEnabled = reader.GetBool("tools.coder.enabled")
	config.CoderPathExtra = append([]string(nil), reader.GetStringSlice("tools.coder.path_extra")...)
	return config
}
