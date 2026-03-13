package integration

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
)

// Runtime is the reusable wiring entrypoint for third-party embedding.
type Runtime struct {
	features         Features
	inspect          InspectOptions
	builtinToolNames []string
	snap             runtimeSnapshot
}

type PreparedRun struct {
	Engine  *agent.Engine
	Model   string
	Cleanup func() error
}

func New(cfg Config) *Runtime {
	cfg = normalizeConfig(cfg)
	return &Runtime{
		features:         cfg.Features,
		inspect:          cfg.Inspect,
		builtinToolNames: append([]string(nil), cfg.BuiltinToolNames...),
		snap:             loadRuntimeSnapshot(cfg),
	}
}

func normalizeConfig(cfg Config) Config {
	out := DefaultConfig()
	if cfg.Features != (Features{}) {
		out.Features = cfg.Features
	}
	if cfg.Inspect != (InspectOptions{}) {
		out.Inspect = cfg.Inspect
	}
	if len(cfg.BuiltinToolNames) > 0 {
		out.BuiltinToolNames = normalizeToolNames(cfg.BuiltinToolNames)
	}
	for k, v := range cfg.Overrides {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out.Overrides[key] = v
	}
	return out
}

func normalizeToolNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

func (rt *Runtime) NewRegistry() *tools.Registry {
	if rt == nil {
		return tools.NewRegistry()
	}
	snap := rt.snapshot()
	return rt.buildRegistry(snap.Registry, snap.Logger)
}

func (rt *Runtime) NewRunEngine(ctx context.Context, task string) (*PreparedRun, error) {
	return rt.NewRunEngineWithRegistry(ctx, task, nil)
}

func (rt *Runtime) NewRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry) (*PreparedRun, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is nil")
	}
	snap := rt.snapshot()
	if snap.LoggerInitErr != nil {
		return nil, snap.LoggerInitErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	task = strings.TrimSpace(task)

	logger := snap.Logger
	if logger == nil {
		logger = slog.Default()
	}
	slog.SetDefault(logger)
	logOpts := cloneLogOptions(snap.LogOptions)

	mainRoute, err := llmutil.ResolveRoute(snap.LLMValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return nil, err
	}
	client, err := llmutil.ClientFromConfigWithValues(mainRoute.ClientConfig, mainRoute.Values)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(mainRoute.ClientConfig.Model)
	var requestInspector *llminspect.RequestInspector
	var promptInspector *llminspect.PromptInspector
	inspectCleanup := func() error {
		var firstErr error
		if promptInspector != nil {
			if err := promptInspector.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if requestInspector != nil {
			if err := requestInspector.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	if rt.inspect.Request {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            strings.TrimSpace(rt.inspect.Mode),
			Task:            strings.TrimSpace(task),
			TimestampFormat: strings.TrimSpace(rt.inspect.TimestampFormat),
			DumpDir:         strings.TrimSpace(rt.inspect.DumpDir),
		})
		if err != nil {
			return nil, err
		}
	}
	if rt.inspect.Prompt {
		promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
			Mode:            strings.TrimSpace(rt.inspect.Mode),
			Task:            strings.TrimSpace(task),
			TimestampFormat: strings.TrimSpace(rt.inspect.TimestampFormat),
			DumpDir:         strings.TrimSpace(rt.inspect.DumpDir),
		})
		if err != nil {
			_ = inspectCleanup()
			return nil, err
		}
	}
	client = llminspect.WrapClient(client, llminspect.ClientOptions{
		PromptInspector:  promptInspector,
		RequestInspector: requestInspector,
		APIBase:          mainRoute.ClientConfig.Endpoint,
		Model:            model,
	})

	reg := cloneRegistry(baseReg)
	if reg == nil {
		reg = rt.buildRegistry(snap.Registry, logger)
	}

	var mcpCleanup func() error
	mh, err := mcphost.RegisterTools(ctx, snap.MCPServers, reg, logger)
	if err != nil {
		logger.Warn("mcp_init_failed", "err", err)
	}
	if mh != nil {
		mcpCleanup = mh.Close
	}

	planEnabled := rt.features.PlanTool && snap.Registry.ToolsPlanCreateEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinPlanCreate)
	todoEnabled := snap.Registry.ToolsTodoUpdateEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinTodoUpdate)
	planClient := client
	planModel := model
	planRoute, err := llmutil.ResolveRoute(snap.LLMValues, llmutil.RoutePurposePlanCreate)
	if err != nil {
		_ = inspectCleanup()
		return nil, err
	}
	if !planRoute.SameProfile(mainRoute) {
		planClient, err = llmutil.ClientFromConfigWithValues(planRoute.ClientConfig, planRoute.Values)
		if err != nil {
			_ = inspectCleanup()
			return nil, err
		}
		planClient = llminspect.WrapClient(planClient, llminspect.ClientOptions{
			PromptInspector:  promptInspector,
			RequestInspector: requestInspector,
			APIBase:          planRoute.ClientConfig.Endpoint,
			Model:            planRoute.ClientConfig.Model,
		})
	}
	planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
	toolsutil.RegisterRuntimeTools(reg, toolsutil.RuntimeToolsRegisterConfig{
		PlanCreate: toolsutil.BuildPlanCreateRegisterConfig(planEnabled, snap.Registry.ToolsPlanCreateMaxSteps),
		TodoUpdate: toolsutil.TodoUpdateRegisterConfig{
			Enabled:      todoEnabled,
			TODOPathWIP:  snap.Registry.TODOPathWIP,
			TODOPathDone: snap.Registry.TODOPathDone,
			ContactsDir:  snap.Registry.ContactsDir,
		},
	}, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    client,
		DefaultModel:     model,
		PlanCreateClient: planClient,
		PlanCreateModel:  planModel,
	})

	promptSpec := agent.DefaultPromptSpec()
	if rt.features.Skills {
		spec, _, err := rt.promptSpecWithSkillsFromConfig(ctx, logger, logOpts, task, client, model, snap.SkillsConfig, nil)
		if err != nil {
			_ = inspectCleanup()
			return nil, err
		}
		promptSpec = spec
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)

	opts := []agent.Option{
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
	}
	if g := rt.buildGuard(snap.Guard, logger); g != nil {
		opts = append(opts, agent.WithGuard(g))
	}

	engine := agent.New(
		client,
		reg,
		snap.AgentLimits.ToConfig(),
		promptSpec,
		opts...,
	)

	return &PreparedRun{
		Engine: engine,
		Model:  model,
		Cleanup: func() error {
			firstErr := inspectCleanup()
			if mcpCleanup != nil {
				if err := mcpCleanup(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			return firstErr
		},
	}, nil
}

func (rt *Runtime) RunTask(ctx context.Context, task string, opts agent.RunOptions) (*agent.Final, *agent.Context, error) {
	prepared, err := rt.NewRunEngine(ctx, task)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = prepared.Cleanup()
	}()

	if strings.TrimSpace(opts.Model) == "" {
		opts.Model = prepared.Model
	}
	return prepared.Engine.Run(ctx, task, opts)
}

func cloneRegistry(base *tools.Registry) *tools.Registry {
	if base == nil {
		return nil
	}
	out := tools.NewRegistry()
	for _, t := range base.All() {
		out.Register(t)
	}
	return out
}

func (rt *Runtime) isBuiltinToolSelected(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || rt == nil {
		return false
	}
	if len(rt.builtinToolNames) == 0 {
		return true
	}
	for _, item := range rt.builtinToolNames {
		if item == name {
			return true
		}
	}
	return false
}

func (rt *Runtime) RequestTimeout() time.Duration {
	if rt == nil {
		return 0
	}
	return rt.snapshot().LLMRequestTimeout
}

func (rt *Runtime) snapshot() runtimeSnapshot {
	if rt == nil {
		return runtimeSnapshot{}
	}
	return rt.snap
}
