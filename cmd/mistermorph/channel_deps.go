package main

import (
	"context"
	"log/slog"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type channelCommandRuntime struct {
	llm    *llmRuntimeResolver
	skills *skillsRuntimeResolver
}

func newChannelCommandRuntime() channelCommandRuntime {
	return channelCommandRuntime{
		llm:    newLLMRuntimeResolver(),
		skills: newSkillsRuntimeResolver(),
	}
}

func (r channelCommandRuntime) Dependencies(registry *registryRuntimeResolver, guard *guardRuntimeResolver) awarenessruntime.Dependencies {
	return awarenessruntime.Dependencies{
		Logger:          logutil.LoggerFromViper,
		LogOptions:      logutil.LogOptionsFromViper,
		ResolveLLMRoute: r.llm.ResolveRoute,
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			values, err := r.llm.Values()
			if err != nil {
				return llmutil.ResolvedRoute{}, err
			}
			return llmutil.ResolveRouteWithProfileOverride(values, purpose, profile)
		},
		CreateLLMClient:   r.llm.CreateClient,
		CreateImageClient: r.llm.CreateImageClient,
		Registry:          registry.Registry,
		AwarenessRegistry: registry.AwarenessRegistry,
		ToolTriggers: func(task string) map[string]bool {
			return explicitBuiltinToolsForTask(task, r.skills.Config())
		},
		RegisterTriggeredStaticTools: registry.RegisterTriggeredStaticTools,
		ACPAgents:                    acpclient.AgentsFromViper,
		Guard:                        guard.Guard,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := r.skills.Config()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
	}
}

func (r channelCommandRuntime) HandleModelCommand(text string) (string, bool, error) {
	values, err := r.llm.Values()
	if err != nil {
		return "", false, err
	}
	return llmselect.ExecuteCommandText(values, llmselect.ProcessStore(), text)
}

func (r channelCommandRuntime) HandleSkillCommand(currentLoaded []string) (string, error) {
	return r.skills.Status(currentLoaded)
}
