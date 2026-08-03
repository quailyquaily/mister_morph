package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

// Runtime is the reusable wiring entrypoint for third-party embedding.
type Runtime struct {
	features         Features
	inspect          InspectOptions
	promptBlocks     []string
	builtinToolNames []string
	snap             runtimeSnapshot
	selection        *llmselect.Store
	buildDeps        runtimeBuildDependencies
}

type PreparedRun struct {
	Engine              *agent.Engine
	Model               string
	ContextWindowTokens int64
	Cleanup             func() error
}

// New constructs a Runtime while preserving the legacy no-error signature.
//
// Deprecated: use NewChecked so configuration errors are returned before the
// runtime is published.
func New(cfg Config) *Runtime {
	return newRuntime(cfg, runtimeBuildDependencies{})
}

// NewChecked constructs a Runtime and returns configuration errors before the
// runtime is published to the caller. New remains available for compatibility;
// callers that still use it should check Runtime.Err before using the runtime.
func NewChecked(cfg Config) (*Runtime, error) {
	rt := newRuntime(cfg, runtimeBuildDependencies{})
	if err := rt.Err(); err != nil {
		return nil, err
	}
	return rt, nil
}

// Err reports configuration errors captured while constructing the runtime.
func (rt *Runtime) Err() error {
	if rt == nil {
		return errRuntimeNil
	}
	return rt.snap.InitErr
}

func newRuntime(cfg Config, buildDeps runtimeBuildDependencies) *Runtime {
	cfg = normalizeConfig(cfg)
	return &Runtime{
		features:         cfg.Features,
		inspect:          cfg.Inspect,
		promptBlocks:     append([]string(nil), cfg.PromptBlocks...),
		builtinToolNames: append([]string(nil), cfg.BuiltinToolNames...),
		snap:             loadRuntimeSnapshot(cfg),
		selection:        llmselect.NewStore(),
		buildDeps:        normalizeRuntimeBuildDependencies(buildDeps),
	}
}

func normalizeConfig(cfg Config) Config {
	out := cfg
	out.BuiltinToolNames = normalizeToolNames(cfg.BuiltinToolNames)
	out.PromptBlocks = normalizePromptBlocks(cfg.PromptBlocks)
	out.Overrides = make(map[string]any, len(cfg.Overrides))
	for k, v := range cfg.Overrides {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out.Overrides[key] = v
	}
	return out
}

func normalizePromptBlocks(blocks []string) []string {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		out = append(out, block)
	}
	if len(out) == 0 {
		return nil
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
	registryConfig := snap.StaticRegistry
	registryConfig.Common.PathRoots = registryConfig.Common.PathRoots.WithWorkspaceDir(snap.DefaultWorkspaceDir)
	return rt.buildRegistry(registryConfig, snap.Logger)
}

func (rt *Runtime) NewRunEngine(ctx context.Context, task string) (*PreparedRun, error) {
	return rt.NewRunEngineWithRegistry(ctx, task, nil)
}

func (rt *Runtime) NewRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry) (*PreparedRun, error) {
	return rt.newRunEngineWithRegistry(ctx, task, baseReg, "")
}

func (rt *Runtime) resolveRunMainRoute(ctx context.Context, snap runtimeSnapshot, profile string) (llmutil.ResolvedRoute, error) {
	var (
		route llmutil.ResolvedRoute
		err   error
	)
	if profile = strings.TrimSpace(profile); profile != "" {
		route, err = llmutil.ResolveRouteWithProfileOverride(snap.LLMValues, llmutil.RoutePurposeMainLoop, profile)
	} else {
		route, err = llmselect.ResolveMainRoute(snap.LLMValues, rt.currentSelection())
	}
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
	selectionKey := strings.TrimSpace(llmstats.RunIDFromContext(ctx))
	if selectionKey == "" {
		selectionKey = strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx))
	}
	return llmutil.SelectRouteCandidate(route, selectionKey), nil
}

func (rt *Runtime) newRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry, profile string) (prepared *PreparedRun, err error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is nil")
	}
	snap := rt.snapshot()
	if snap.InitErr != nil {
		return nil, snap.InitErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = ensureIntegrationRunContext(ctx)
	ctx = pathroots.WithWorkspaceDir(ctx, snap.DefaultWorkspaceDir)
	task = strings.TrimSpace(task)

	logger := snap.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var requestInspector *llminspect.RequestInspector
	var promptInspector *llminspect.PromptInspector
	var mcp mcpRegistration
	var runPreparer *taskruntime.Runtime
	var taskPrepared *taskruntime.PreparedEngine
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			if taskPrepared != nil {
				cleanupErr = errors.Join(cleanupErr, taskPrepared.Cleanup())
			}
			if runPreparer != nil {
				cleanupErr = errors.Join(cleanupErr, runPreparer.Close())
			}
			if mcp.close != nil {
				cleanupErr = errors.Join(cleanupErr, mcp.close())
			}
			cleanupErr = errors.Join(cleanupErr, closeDistinctResources(promptInspector, requestInspector))
		})
		return cleanupErr
	}
	success := false
	defer func() {
		if !success {
			err = errors.Join(err, cleanup())
		}
	}()
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
			return nil, err
		}
	}
	clientWrap := inspectClientWrap(promptInspector, requestInspector)
	runStaticRegistry := snap.StaticRegistry
	runStaticRegistry.Common.PathRoots = runStaticRegistry.Common.PathRoots.WithWorkspaceDir(snap.DefaultWorkspaceDir)
	var reg *tools.Registry
	if baseReg == nil {
		reg = rt.buildRegistry(runStaticRegistry, logger)
	} else {
		reg = baseReg.Clone()
	}

	mcp, err = rt.buildDeps.connectMCP(ctx, snap.MCPServers, logger)
	if err != nil {
		return nil, fmt.Errorf("connect MCP servers: %w", err)
	}
	if err = registerIntegrationMCPTools(reg, mcp.tools); err != nil {
		return nil, fmt.Errorf("register MCP tools: %w", err)
	}

	common := rt.sharedDependencies(snap)
	common.Registry = func() *tools.Registry { return reg.Clone() }
	common.RegisterTriggeredStaticTools = func(reg *tools.Registry, triggers map[string]bool) {
		rt.registerStaticTools(reg, runStaticRegistry, logger, false, triggers)
	}
	basePromptAugment := common.PromptAugment
	common.PromptAugment = func(spec *agent.PromptSpec, reg *tools.Registry) {
		if block := workspace.PromptBlock(snap.DefaultWorkspaceDir); strings.TrimSpace(block.Content) != "" {
			spec.Blocks = append(spec.Blocks, block)
		}
		if basePromptAugment != nil {
			basePromptAugment(spec, reg)
		}
	}
	engineToolsConfig := agent.EngineToolsConfig{
		SpawnEnabled: snap.Registry.ToolsSpawnEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinSpawn),
		ACPSpawnEnabled: snap.Registry.ToolsACPSpawnEnabled &&
			rt.isBuiltinToolSelected(toolsutil.BuiltinACPSpawn),
		CoderEnabled:   snap.Registry.ToolsCoderEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinCoder),
		PathRoots:      runStaticRegistry.Common.PathRoots,
		CoderPathExtra: append([]string(nil), snap.Registry.ToolsCoderPathExtra...),
	}
	var clientDecorator taskruntime.ClientDecorator
	if clientWrap != nil {
		clientDecorator = func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
			return clientWrap(client, route.ClientConfig, route.Profile)
		}
	}
	runPreparer, err = taskruntime.NewRunPreparer(common, taskruntime.BootstrapOptions{
		AgentConfig:       snap.AgentLimits.ToConfig(),
		EngineToolsConfig: &engineToolsConfig,
		ClientDecorator:   clientDecorator,
	})
	if err != nil {
		return nil, err
	}
	taskPrepared, err = runPreparer.PrepareEngine(ctx, taskruntime.RunRequest{
		Task:       task,
		LLMProfile: strings.TrimSpace(profile),
	})
	if err != nil {
		return nil, err
	}
	prepared = &PreparedRun{
		Engine:              taskPrepared.Engine,
		Model:               taskPrepared.Model,
		ContextWindowTokens: taskPrepared.ContextWindowTokens,
		Cleanup:             cleanup,
	}
	success = true
	return prepared, nil
}

func (rt *Runtime) appendPromptBlocks(spec *agent.PromptSpec) {
	if rt == nil || spec == nil || len(rt.promptBlocks) == 0 {
		return
	}
	for _, content := range rt.promptBlocks {
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: content})
	}
}

func (rt *Runtime) RunTask(ctx context.Context, task string, opts agent.RunOptions) (*agent.Final, *agent.Context, error) {
	ctx = ensureIntegrationRunContext(ctx)
	if rt != nil {
		ctx = pathroots.WithWorkspaceDir(ctx, rt.snapshot().DefaultWorkspaceDir)
	}
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
	if opts.ContextWindowTokens <= 0 {
		opts.ContextWindowTokens = prepared.ContextWindowTokens
	}
	return prepared.Engine.Run(ctx, task, opts)
}

func ensureIntegrationRunContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(llmstats.RunIDFromContext(ctx)) != "" || strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx)) != "" {
		return ctx
	}
	return llmstats.WithRunID(ctx, llmstats.NewSyntheticRunID(defaultIntegrationTaskTarget))
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

func imageToolsRegisterConfigFromSnapshot(snap runtimeSnapshot, values llmutil.RuntimeValues, generateEnabled, editEnabled bool) toolsutil.ImageToolsRegisterConfig {
	return toolsutil.ApplyImageToolLLMConfig(toolsutil.ImageToolsRegisterConfig{
		GenerateEnabled: generateEnabled,
		EditEnabled:     editEnabled,
		FileCacheDir:    snap.Paths.CacheDir,
		FileStateDir:    snap.Paths.StateDir,
		Options:         snap.LLMValues.ImageOptions,
	}, toolsutil.ImageToolLLMConfig{
		Provider:            values.Provider,
		APIKey:              values.APIKey,
		Model:               values.Model,
		ImageProvider:       values.ImageProvider,
		ImageAPIKey:         values.ImageAPIKey,
		ImageModel:          values.ImageModel,
		CloudflareAccountID: values.CloudflareAccountID,
		CloudflareAPIToken:  values.CloudflareAPIToken,
	})
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
