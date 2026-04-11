package taskruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type ClientDecorator func(client llm.Client, route llmutil.ResolvedRoute) llm.Client

type BootstrapOptions struct {
	AgentConfig       agent.Config
	EngineToolsConfig *agent.EngineToolsConfig
	ClientDecorator   ClientDecorator
}

type Runtime struct {
	commonDeps depsutil.CommonDependencies

	Logger            *slog.Logger
	LogOptions        agent.LogOptions
	AgentConfig       agent.Config
	EngineToolsConfig agent.EngineToolsConfig
	ClientDecorator   ClientDecorator

	BaseRegistry *tools.Registry
	SharedGuard  *guard.Guard

	BootstrapMainRoute    llmutil.ResolvedRoute
	BootstrapMainClient   llm.Client
	BootstrapMainModel    string
	BootstrapMainProvider string

	PlanRoute  llmutil.ResolvedRoute
	PlanClient llm.Client
	PlanModel  string
	ACPAgents  []acpclient.AgentConfig
}

type MemoryHooks struct {
	Source            string
	SubjectID         string
	LogFields         map[string]any
	InjectionEnabled  bool
	InjectionMaxItems int
	PrepareInjection  func(maxItems int) (string, error)
	ShouldRecord      func(final *agent.Final) bool
	Record            func(final *agent.Final, finalOutput string) error
	NotifyRecorded    func()
}

type PromptAugmentFunc func(spec *agent.PromptSpec, reg *tools.Registry)

type RunRequest struct {
	Task                string
	Model               string
	Scene               string
	StickySkills        []string
	History             []llm.Message
	CurrentMessage      *llm.Message
	Meta                map[string]any
	Registry            *tools.Registry
	DisableRuntimeTools bool
	PromptAugment       PromptAugmentFunc
	PlanStepUpdate      func(*agent.Context, agent.PlanStepUpdate)
	OnStream            llm.StreamHandler
	Memory              MemoryHooks
	EngineToolsConfig   *agent.EngineToolsConfig
}

type RunResult struct {
	Final        *agent.Final
	Context      *agent.Context
	LoadedSkills []string
}

func Bootstrap(d depsutil.CommonDependencies, opts BootstrapOptions) (*Runtime, error) {
	logger, err := depsutil.LoggerFromCommon(d)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	logOpts := depsutil.LogOptionsFromCommon(d)
	mainRoute, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return nil, err
	}
	mainClient, err := depsutil.CreateClientFromCommon(d, mainRoute)
	if err != nil {
		return nil, err
	}
	if opts.ClientDecorator != nil {
		mainClient = opts.ClientDecorator(mainClient, mainRoute)
	}
	mainModel := strings.TrimSpace(mainRoute.ClientConfig.Model)

	planRoute, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposePlanCreate)
	if err != nil {
		return nil, err
	}
	planClient := mainClient
	if !planRoute.SameProfile(mainRoute) {
		planClient, err = depsutil.CreateClientFromCommon(d, planRoute)
		if err != nil {
			return nil, err
		}
		if opts.ClientDecorator != nil {
			planClient = opts.ClientDecorator(planClient, planRoute)
		}
	}
	baseRegistry := depsutil.RegistryFromCommon(d)
	if baseRegistry == nil {
		baseRegistry = tools.NewRegistry()
	}
	engineToolsConfig := agent.DefaultEngineToolsConfig()
	if opts.EngineToolsConfig != nil {
		engineToolsConfig = *opts.EngineToolsConfig
	}
	return &Runtime{
		commonDeps:            d,
		Logger:                logger,
		LogOptions:            logOpts,
		AgentConfig:           opts.AgentConfig,
		EngineToolsConfig:     engineToolsConfig,
		ClientDecorator:       opts.ClientDecorator,
		BaseRegistry:          baseRegistry,
		SharedGuard:           depsutil.GuardFromCommon(d, logger),
		BootstrapMainRoute:    mainRoute,
		BootstrapMainClient:   mainClient,
		BootstrapMainModel:    mainModel,
		BootstrapMainProvider: strings.TrimSpace(mainRoute.ClientConfig.Provider),
		PlanRoute:             planRoute,
		PlanClient:            planClient,
		PlanModel:             strings.TrimSpace(planRoute.ClientConfig.Model),
		ACPAgents:             depsutil.ACPAgentsFromCommon(d),
	}, nil
}

func CloneRegistry(base *tools.Registry) *tools.Registry {
	reg := tools.NewRegistry()
	if base == nil {
		return reg
	}
	for _, t := range base.All() {
		reg.Register(t)
	}
	return reg
}

func (rt *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if rt == nil {
		return RunResult{}, fmt.Errorf("task runtime is nil")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return RunResult{}, fmt.Errorf("empty task")
	}
	logger := rt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	scene := strings.TrimSpace(req.Scene)
	if scene == "" {
		scene = "runtime.loop"
	}
	mainRoute, err := rt.ResolveMainRouteForRun()
	if err != nil {
		return RunResult{}, err
	}
	mainClient, err := rt.CreateClientForRoute(mainRoute)
	if err != nil {
		return RunResult{}, err
	}
	defer closeRuntimeClient(logger, mainClient)
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(mainRoute.ClientConfig.Model)
	}

	reg := req.Registry
	if reg == nil {
		reg = CloneRegistry(rt.BaseRegistry)
	}
	if !req.DisableRuntimeTools {
		toolsutil.RegisterRuntimeTools(reg, rt.commonDeps.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
			DefaultClient:    mainClient,
			DefaultModel:     model,
			PlanCreateClient: rt.PlanClient,
			PlanCreateModel:  rt.PlanModel,
		})
	}

	_ = mainRoute
	promptSpec, loadedSkills, err := depsutil.PromptSpecFromCommon(rt.commonDeps, ctx, logger, rt.LogOptions, task, mainClient, model, req.StickySkills)
	if err != nil {
		return RunResult{}, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	if req.PromptAugment != nil {
		req.PromptAugment(&promptSpec, reg)
	}
	if err := rt.applyMemoryInjection(logger, &promptSpec, req.Memory); err != nil {
		return RunResult{}, err
	}
	depsutil.PromptAugmentFromCommon(rt.commonDeps, &promptSpec, reg)

	agentCfg := rt.AgentConfig
	agentCfg.DefaultModel = model
	engineToolsConfig := rt.EngineToolsConfig
	if req.EngineToolsConfig != nil {
		engineToolsConfig = *req.EngineToolsConfig
	}

	engineOpts := []agent.Option{
		agent.WithLogger(logger),
		agent.WithLogOptions(rt.LogOptions),
		agent.WithSubtaskRunner(rt),
		agent.WithEngineToolsConfig(engineToolsConfig),
		agent.WithACPAgents(rt.ACPAgents),
	}
	if rt.SharedGuard != nil {
		engineOpts = append(engineOpts, agent.WithGuard(rt.SharedGuard))
	}
	if req.PlanStepUpdate != nil {
		engineOpts = append(engineOpts, agent.WithPlanStepUpdate(req.PlanStepUpdate))
	}
	engine := agent.New(
		mainClient,
		reg,
		agentCfg,
		promptSpec,
		engineOpts...,
	)
	final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{
		Model:          model,
		Scene:          scene,
		History:        append([]llm.Message(nil), req.History...),
		Meta:           cloneMeta(req.Meta),
		CurrentMessage: req.CurrentMessage,
		OnStream:       req.OnStream,
	})
	if err != nil {
		return RunResult{Final: final, Context: runCtx, LoadedSkills: loadedSkills}, err
	}
	if err := rt.recordMemory(logger, final, req.Memory); err != nil {
		return RunResult{Final: final, Context: runCtx, LoadedSkills: loadedSkills}, err
	}
	return RunResult{
		Final:        final,
		Context:      runCtx,
		LoadedSkills: loadedSkills,
	}, nil
}

func (rt *Runtime) ResolveMainRouteForRun() (llmutil.ResolvedRoute, error) {
	if rt == nil {
		return llmutil.ResolvedRoute{}, fmt.Errorf("task runtime is nil")
	}
	route, err := depsutil.ResolveLLMRouteFromCommon(rt.commonDeps, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
	return route, nil
}

func (rt *Runtime) RunSubtask(ctx context.Context, req agent.SubtaskRequest) (*agent.SubtaskResult, error) {
	if rt == nil {
		return nil, fmt.Errorf("task runtime is nil")
	}
	if err := agent.ValidateSubtaskStart(ctx); err != nil {
		return agent.FailedSubtaskResult("", err), nil
	}
	task := strings.TrimSpace(req.Task)
	if task == "" && req.RunFunc == nil {
		return nil, fmt.Errorf("empty subtask")
	}

	taskID, runCtx, meta := agent.PrepareSubtaskContext(ctx, req.Meta)
	logger := rt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mode := "agent"
	if req.RunFunc != nil {
		mode = "direct"
	}
	agent.EmitEvent(ctx, nil, agent.Event{
		Kind:    agent.EventKindSubtaskStart,
		TaskID:  taskID,
		Mode:    mode,
		Profile: string(agent.NormalizeObserveProfile(string(req.ObserveProfile))),
		Status:  "running",
	})
	logger.Info("subtask_start", "task_id", taskID, "mode", mode, "output_schema", strings.TrimSpace(req.OutputSchema))

	if req.RunFunc != nil {
		directResult, err := req.RunFunc(runCtx)
		if err != nil {
			result := agent.FailedSubtaskResult(taskID, err)
			logger.Info("subtask_done", "task_id", taskID, "status", result.Status, "output_kind", result.OutputKind)
			agent.EmitEvent(ctx, nil, agent.Event{
				Kind:       agent.EventKindSubtaskDone,
				TaskID:     taskID,
				Status:     strings.TrimSpace(result.Status),
				Summary:    strings.TrimSpace(result.Summary),
				OutputKind: strings.TrimSpace(result.OutputKind),
				Error:      strings.TrimSpace(result.Error),
			})
			return result, nil
		}
		result := agent.NormalizeDirectSubtaskResult(taskID, req.OutputSchema, directResult)
		logger.Info("subtask_done", "task_id", taskID, "status", result.Status, "output_kind", result.OutputKind)
		agent.EmitEvent(ctx, nil, agent.Event{
			Kind:       agent.EventKindSubtaskDone,
			TaskID:     taskID,
			Status:     strings.TrimSpace(result.Status),
			Summary:    strings.TrimSpace(result.Summary),
			OutputKind: strings.TrimSpace(result.OutputKind),
			Error:      strings.TrimSpace(result.Error),
		})
		return result, nil
	}

	result, err := rt.Run(runCtx, RunRequest{
		Task:                agent.BuildSubtaskTask(task, req.OutputSchema),
		Model:               strings.TrimSpace(req.Model),
		Scene:               "spawn.subtask",
		Registry:            req.Registry,
		DisableRuntimeTools: true,
		EngineToolsConfig: &agent.EngineToolsConfig{
			SpawnEnabled:    false,
			ACPSpawnEnabled: false,
		},
		Meta: meta,
	})
	if err != nil {
		failed := agent.FailedSubtaskResult(taskID, err)
		logger.Info("subtask_done", "task_id", taskID, "status", failed.Status, "output_kind", failed.OutputKind)
		agent.EmitEvent(ctx, nil, agent.Event{
			Kind:       agent.EventKindSubtaskDone,
			TaskID:     taskID,
			Status:     strings.TrimSpace(failed.Status),
			Summary:    strings.TrimSpace(failed.Summary),
			OutputKind: strings.TrimSpace(failed.OutputKind),
			Error:      strings.TrimSpace(failed.Error),
		})
		return failed, nil
	}
	final := agent.SubtaskResultFromFinal(taskID, req.OutputSchema, result.Final)
	logger.Info("subtask_done", "task_id", taskID, "status", final.Status, "output_kind", final.OutputKind)
	agent.EmitEvent(ctx, nil, agent.Event{
		Kind:       agent.EventKindSubtaskDone,
		TaskID:     taskID,
		Status:     strings.TrimSpace(final.Status),
		Summary:    strings.TrimSpace(final.Summary),
		OutputKind: strings.TrimSpace(final.OutputKind),
		Error:      strings.TrimSpace(final.Error),
	})
	return final, nil
}

func (rt *Runtime) CreateClientForRoute(route llmutil.ResolvedRoute) (llm.Client, error) {
	if rt == nil {
		return nil, fmt.Errorf("task runtime is nil")
	}
	client, err := depsutil.CreateClientFromCommon(rt.commonDeps, route)
	if err != nil {
		return nil, err
	}
	if rt.ClientDecorator != nil {
		client = rt.ClientDecorator(client, route)
	}
	return client, nil
}

func closeRuntimeClient(logger *slog.Logger, client llm.Client) {
	if client == nil {
		return
	}
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil && logger != nil {
		logger.Warn("task_runtime_client_close_failed", "error", err.Error())
	}
}

func (rt *Runtime) applyMemoryInjection(logger *slog.Logger, promptSpec *agent.PromptSpec, hooks MemoryHooks) error {
	if promptSpec == nil || hooks.PrepareInjection == nil || !hooks.InjectionEnabled || strings.TrimSpace(hooks.SubjectID) == "" {
		return nil
	}
	snap, err := hooks.PrepareInjection(hooks.InjectionMaxItems)
	if err != nil {
		logger.Warn("memory_injection_error", memoryLogArgs(hooks, "error", err.Error())...)
		return nil
	}
	if strings.TrimSpace(snap) == "" {
		logger.Debug("memory_injection_skipped", memoryLogArgs(hooks, "reason", "empty_snapshot")...)
		return nil
	}
	promptprofile.AppendMemorySummariesBlock(promptSpec, snap)
	logger.Info("memory_injection_applied", memoryLogArgs(hooks, "snapshot_len", len(snap))...)
	return nil
}

func (rt *Runtime) recordMemory(logger *slog.Logger, final *agent.Final, hooks MemoryHooks) error {
	if hooks.Record == nil || strings.TrimSpace(hooks.SubjectID) == "" {
		return nil
	}
	if hooks.ShouldRecord != nil && !hooks.ShouldRecord(final) {
		return nil
	}
	finalOutput := strings.TrimSpace(depsutil.FormatFinalOutput(final))
	if finalOutput == "" {
		return nil
	}
	if err := hooks.Record(final, finalOutput); err != nil {
		logger.Warn("memory_record_error", memoryLogArgs(hooks, "error", err.Error())...)
		return nil
	}
	logger.Debug("memory_record_ok", memoryLogArgs(hooks)...)
	if hooks.NotifyRecorded != nil {
		hooks.NotifyRecorded()
	}
	return nil
}

func cloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func memoryLogArgs(hooks MemoryHooks, extra ...any) []any {
	args := make([]any, 0, 4+len(hooks.LogFields)*2+len(extra))
	args = append(args, "source", strings.TrimSpace(hooks.Source))
	args = append(args, "subject_id", strings.TrimSpace(hooks.SubjectID))
	for k, v := range hooks.LogFields {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		args = append(args, key, v)
	}
	args = append(args, extra...)
	return args
}
