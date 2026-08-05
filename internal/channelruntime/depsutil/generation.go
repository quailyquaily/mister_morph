package depsutil

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func BuildGenerationDependencies(ctx context.Context, base CommonDependencies, reader agentsettings.Reader) (CommonDependencies, func(), error) {
	if reader == nil {
		return CommonDependencies{}, nil, fmt.Errorf("runtime generation config reader is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger := slog.Default()
	if base.Logger != nil {
		resolved, err := base.Logger()
		if err != nil {
			return CommonDependencies{}, nil, err
		}
		if resolved != nil {
			logger = resolved
		}
	}
	runtimeValues, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return CommonDependencies{}, nil, err
	}
	readerViper := viper.New()
	configdefaults.Apply(readerViper)
	_ = readerViper.MergeConfigMap(reader.AllSettings())
	registryConfig, err := toolsutil.StaticRegistryConfigFromReader(readerViper)
	if err != nil {
		return CommonDependencies{}, nil, err
	}
	guardSnapshot, err := guard.SnapshotFromReader(reader)
	if err != nil {
		return CommonDependencies{}, nil, err
	}
	paths := runtimepaths.FromReader(reader)
	baseRegistry := tools.NewRegistry()
	toolsutil.RegisterStaticTools(baseRegistry, registryConfig, nil, nil)
	awarenessConfig := registryConfig
	awarenessConfig.Common.Awareness = true
	awarenessRegistry := tools.NewRegistry()
	toolsutil.RegisterStaticTools(awarenessRegistry, awarenessConfig, nil, nil)
	mcpHost, err := mcphost.RegisterTools(ctx, mcphost.MCPConfigFromReader(readerViper), baseRegistry, logger)
	if err != nil {
		return CommonDependencies{}, nil, err
	}
	if err := mcphost.RegisterHostTools(mcpHost, awarenessRegistry); err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return CommonDependencies{}, nil, err
	}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if mcpHost != nil {
				_ = mcpHost.Close()
			}
		})
	}
	logOptions := logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(reader))
	acpAgents := acpclient.AgentsFromReader(readerViper)
	deps := CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return logOptions
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
				return llmselect.ResolveMainRoute(runtimeValues, llmselect.ProcessStore().Get())
			}
			return llmutil.ResolveRoute(runtimeValues, purpose)
		},
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRouteWithProfileOverride(runtimeValues, purpose, profile)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return llmutil.BuildRouteClient(
				route,
				nil,
				llmutil.ClientFromConfigWithValues,
				func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
					return llmstats.WrapClient(client, llmstats.ClientOptions{
						Provider:            cfg.Provider,
						APIBase:             cfg.Endpoint,
						DefaultModel:        cfg.Model,
						ContextWindowTokens: cfg.ContextWindowTokens,
						JournalDir:          paths.LLMUsageJournalDir,
						TopicContextStore:   topiccontext.NewStore(paths.TopicContextPath),
						Logger:              logger,
					})
				},
				logger,
			)
		},
		CreateImageClient: func() (llm.ImageClient, error) {
			client, err := llmutil.ImageClientFromValues(runtimeValues)
			if err != nil {
				return nil, err
			}
			meta := llmutil.ResolveImageClientMetadata(runtimeValues)
			return llmstats.WrapImageClient(client, llmstats.ClientOptions{
				Provider:     meta.Provider,
				APIBase:      meta.Endpoint,
				DefaultModel: meta.Model,
				JournalDir:   paths.LLMUsageJournalDir,
				Logger:       logger,
			}), nil
		},
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		AwarenessRegistry: func() *tools.Registry {
			return awarenessRegistry
		},
		ToolTriggers: func(task string) map[string]bool {
			cfg := skillsutil.SkillsConfigFromReader(reader)
			refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, cfg))
			if len(acpAgents) == 0 {
				delete(refs, toolsutil.BuiltinACPSpawn)
			}
			return refs
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			toolsutil.RegisterStaticTools(reg, registryConfig, nil, triggers)
		},
		ACPAgents: func() []acpclient.AgentConfig {
			return acpclient.CloneAgents(acpAgents)
		},
		RuntimeToolsConfig:     toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
		RuntimePaths:           paths,
		DefaultWorkspaceDir:    strings.TrimSpace(reader.GetString("workspace_dir")),
		AgentSettingsOwner:     base.AgentSettingsOwner,
		RuntimeConfigSource:    base.RuntimeConfigSource,
		AgentSettingsReader:    agentsettings.NewReaderSnapshot(reader),
		TaskPersistenceTargets: append([]string(nil), reader.GetStringSlice("tasks.persistence_targets")...),
		TaskRotateMaxBytes:     reader.GetInt64("tasks.rotate_max_bytes"),
		Guard: func(guardLogger *slog.Logger) (*guard.Guard, error) {
			if guardLogger == nil {
				guardLogger = logger
			}
			return guard.NewChecked(guardSnapshot, guardLogger)
		},
		PromptSpec: func(ctx context.Context, promptLogger *slog.Logger, opts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := skillsutil.SkillsConfigFromReader(reader)
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, promptLogger, opts, task, client, model, cfg)
		},
		PromptAugment: base.PromptAugment,
	}
	return deps, cleanup, nil
}
