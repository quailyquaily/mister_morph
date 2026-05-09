package depsutil

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type PromptSpecFunc func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error)

type CommonDependencies struct {
	Logger             func() (*slog.Logger, error)
	LogOptions         func() agent.LogOptions
	ResolveLLMRoute    func(purpose string) (llmutil.ResolvedRoute, error)
	CreateLLMClient    func(route llmutil.ResolvedRoute) (llm.Client, error)
	Registry           func() *tools.Registry
	ACPAgents          func() []acpclient.AgentConfig
	RuntimeToolsConfig toolsutil.RuntimeToolsRegisterConfig
	Guard              func(logger *slog.Logger) *guard.Guard
	PromptSpec         PromptSpecFunc
	PromptAugment      func(spec *agent.PromptSpec, reg *tools.Registry)
}

type AwarenessDependencies struct {
	Logger             func() (*slog.Logger, error)
	LogOptions         func() agent.LogOptions
	ResolveLLMRoute    func(purpose string) (llmutil.ResolvedRoute, error)
	CreateLLMClient    func(route llmutil.ResolvedRoute) (llm.Client, error)
	Registry           func() *tools.Registry
	ACPAgents          func() []acpclient.AgentConfig
	RuntimeToolsConfig toolsutil.RuntimeToolsRegisterConfig
	Guard              func(logger *slog.Logger) *guard.Guard
	PromptSpec         PromptSpecFunc
	PromptAugment      func(spec *agent.PromptSpec, reg *tools.Registry)
	BuildAwarenessTask func(behavior awarenessutil.Behavior, checklistPath string, input daemonruntime.PokeInput) (string, bool, error)
	BuildAwarenessMeta func(behavior awarenessutil.Behavior, source string, interval time.Duration, checklistPath string, taskEmpty bool, input daemonruntime.PokeInput, extra map[string]any) map[string]any
}

func CommonFromAwareness(d AwarenessDependencies) CommonDependencies {
	return CommonDependencies{
		Logger:             d.Logger,
		LogOptions:         d.LogOptions,
		ResolveLLMRoute:    d.ResolveLLMRoute,
		CreateLLMClient:    d.CreateLLMClient,
		Registry:           d.Registry,
		ACPAgents:          d.ACPAgents,
		RuntimeToolsConfig: d.RuntimeToolsConfig,
		Guard:              d.Guard,
		PromptSpec:         d.PromptSpec,
		PromptAugment:      d.PromptAugment,
	}
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

func BuildAwarenessTask(fn func(behavior awarenessutil.Behavior, checklistPath string, input daemonruntime.PokeInput) (string, bool, error), behavior awarenessutil.Behavior, checklistPath string, input daemonruntime.PokeInput) (string, bool, error) {
	if fn == nil {
		return "", true, fmt.Errorf("BuildAwarenessTask dependency missing")
	}
	return fn(behavior, checklistPath, input)
}

func BuildAwarenessMeta(fn func(behavior awarenessutil.Behavior, source string, interval time.Duration, checklistPath string, taskEmpty bool, input daemonruntime.PokeInput, extra map[string]any) map[string]any, behavior awarenessutil.Behavior, source string, interval time.Duration, checklistPath string, taskEmpty bool, input daemonruntime.PokeInput, extra map[string]any) map[string]any {
	if fn == nil {
		return awarenessutil.BuildAwarenessMeta(behavior, source, interval, checklistPath, taskEmpty, nil, input, extra)
	}
	return fn(behavior, source, interval, checklistPath, taskEmpty, input, extra)
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

func BuildAwarenessTaskFromDeps(d AwarenessDependencies, behavior awarenessutil.Behavior, checklistPath string, input daemonruntime.PokeInput) (string, bool, error) {
	return BuildAwarenessTask(d.BuildAwarenessTask, behavior, checklistPath, input)
}

func BuildAwarenessMetaFromDeps(d AwarenessDependencies, behavior awarenessutil.Behavior, source string, interval time.Duration, checklistPath string, taskEmpty bool, input daemonruntime.PokeInput, extra map[string]any) map[string]any {
	return BuildAwarenessMeta(d.BuildAwarenessMeta, behavior, source, interval, checklistPath, taskEmpty, input, extra)
}
