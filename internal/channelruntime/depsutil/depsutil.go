package depsutil

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type PromptSpecFunc func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error)

type CommonDependencies struct {
	Logger                       func() (*slog.Logger, error)
	LogOptions                   func() agent.LogOptions
	ResolveLLMRoute              func(purpose string) (llmutil.ResolvedRoute, error)
	CreateLLMClient              func(route llmutil.ResolvedRoute) (llm.Client, error)
	CreateImageClient            func() (llm.ImageClient, error)
	Registry                     func() *tools.Registry
	AwarenessRegistry            func() *tools.Registry
	ToolTriggers                 func(task string) map[string]bool
	RegisterTriggeredStaticTools func(*tools.Registry, map[string]bool)
	ACPAgents                    func() []acpclient.AgentConfig
	RuntimeToolsConfig           toolsutil.RuntimeToolsRegisterConfig
	Guard                        func(logger *slog.Logger) *guard.Guard
	PromptSpec                   PromptSpecFunc
	PromptAugment                func(spec *agent.PromptSpec, reg *tools.Registry)
}

func Logger(fn func() (*slog.Logger, error)) (*slog.Logger, error) {
	if fn == nil {
		return nil, fmt.Errorf("Logger dependency missing")
	}
	return fn()
}

func LogOptions(fn func() agent.LogOptions) agent.LogOptions {
	if fn == nil {
		return agent.LogOptions{}
	}
	return fn()
}

func CreateClient(fn func(route llmutil.ResolvedRoute) (llm.Client, error), route llmutil.ResolvedRoute) (llm.Client, error) {
	if fn == nil {
		return nil, fmt.Errorf("CreateLLMClient dependency missing")
	}
	return fn(route)
}

func ResolveLLMRoute(fn func(purpose string) (llmutil.ResolvedRoute, error), purpose string) (llmutil.ResolvedRoute, error) {
	if fn == nil {
		return llmutil.ResolvedRoute{}, fmt.Errorf("ResolveLLMRoute dependency missing")
	}
	return fn(purpose)
}

func Registry(fn func() *tools.Registry) *tools.Registry {
	if fn == nil {
		return nil
	}
	return fn()
}

func ACPAgents(fn func() []acpclient.AgentConfig) []acpclient.AgentConfig {
	if fn == nil {
		return nil
	}
	return fn()
}

func Guard(gf func(logger *slog.Logger) *guard.Guard, logger *slog.Logger) *guard.Guard {
	if gf == nil {
		return nil
	}
	return gf(logger)
}

func PromptSpec(fn PromptSpecFunc, ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
	if fn == nil {
		return agent.PromptSpec{}, nil, fmt.Errorf("PromptSpec dependency missing")
	}
	return fn(ctx, logger, logOpts, task, client, model, stickySkills)
}

func FormatFinalOutput(final *agent.Final) string {
	return outputfmt.FormatFinalOutput(final)
}

func FormatRuntimeError(err error) string {
	s := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
	if s != "" {
		return s
	}
	if err == nil {
		return "unknown error"
	}
	raw := strings.TrimSpace(err.Error())
	if raw == "" {
		return "unknown error"
	}
	return raw
}

func LoggerFromCommon(d CommonDependencies) (*slog.Logger, error) {
	return Logger(d.Logger)
}

func LogOptionsFromCommon(d CommonDependencies) agent.LogOptions {
	return LogOptions(d.LogOptions)
}

func CreateClientFromCommon(d CommonDependencies, route llmutil.ResolvedRoute) (llm.Client, error) {
	return CreateClient(d.CreateLLMClient, route)
}

func ResolveLLMRouteFromCommon(d CommonDependencies, purpose string) (llmutil.ResolvedRoute, error) {
	return ResolveLLMRoute(d.ResolveLLMRoute, purpose)
}

func RegistryFromCommon(d CommonDependencies) *tools.Registry {
	return Registry(d.Registry)
}

func ACPAgentsFromCommon(d CommonDependencies) []acpclient.AgentConfig {
	return ACPAgents(d.ACPAgents)
}

func GuardFromCommon(d CommonDependencies, logger *slog.Logger) *guard.Guard {
	return Guard(d.Guard, logger)
}

func PromptSpecFromCommon(d CommonDependencies, ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
	return PromptSpec(d.PromptSpec, ctx, logger, logOpts, task, client, model, stickySkills)
}

func PromptAugment(spec *agent.PromptSpec, reg *tools.Registry, fn func(spec *agent.PromptSpec, reg *tools.Registry)) {
	if fn == nil || spec == nil {
		return
	}
	fn(spec, reg)
}

func PromptAugmentFromCommon(d CommonDependencies, spec *agent.PromptSpec, reg *tools.Registry) {
	PromptAugment(spec, reg, d.PromptAugment)
}
