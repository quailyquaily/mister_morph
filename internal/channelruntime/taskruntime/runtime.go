package taskruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
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

type runtimeClientOwners struct {
	clients []*ownedRuntimeClient
}

type ownedRuntimeClient struct {
	base      llm.Client
	closeOnce sync.Once
	closeErr  error
}

func (c *ownedRuntimeClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.base == nil {
		return llm.Result{}, io.ErrClosedPipe
	}
	return c.base.Chat(ctx, req)
}

func (c *ownedRuntimeClient) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if closer, ok := c.base.(io.Closer); ok {
			c.closeErr = closer.Close()
		}
	})
	return c.closeErr
}

func (owners *runtimeClientOwners) own(client llm.Client) llm.Client {
	if owners == nil || client == nil {
		return client
	}
	for _, owned := range owners.clients {
		if sameRuntimeClient(owned, client) || sameRuntimeClient(owned.base, client) {
			return owned
		}
	}
	owned := &ownedRuntimeClient{base: client}
	owners.clients = append(owners.clients, owned)
	return owned
}

func (owners *runtimeClientOwners) resources() []any {
	if owners == nil || len(owners.clients) == 0 {
		return nil
	}
	resources := make([]any, 0, len(owners.clients))
	for _, client := range owners.clients {
		resources = append(resources, client)
	}
	return resources
}

type Runtime struct {
	commonDeps            depsutil.CommonDependencies
	bootstrapClientOwners *runtimeClientOwners
	closeOnce             sync.Once
	closeErr              error

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

	ImageClient    llm.ImageClient
	ImageSession   *imagesession.Store
	imageRetention *toolsutil.ImageToolRetentionStore
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
	Task                     string
	Model                    string
	Route                    *llmutil.ResolvedRoute
	LLMProfile               string
	RoutePurpose             string
	ReasoningEffortOverride  string
	Scene                    string
	StickySkills             []string
	History                  []llm.Message
	CurrentMessage           *llm.Message
	Meta                     map[string]any
	Registry                 *tools.Registry
	DisableRuntimeTools      bool
	DisableTodoWorkflow      bool
	Hook                     agent.Hook
	PromptAugment            PromptAugmentFunc
	PlanStepUpdate           func(*agent.Context, agent.PlanStepUpdate)
	OnToolStart              func(*agent.Context, string)
	OnToolCallStart          func(*agent.Context, agent.ToolCall)
	OnToolCallDone           func(*agent.Context, agent.ToolCall, string, error)
	ReasoningDetails         bool
	OnStream                 llm.StreamHandler
	SteerSource              agent.SteerSource
	Memory                   MemoryHooks
	EngineToolsConfig        *agent.EngineToolsConfig
	ImageToolScope           string
	ImageToolRetention       toolsutil.ImageToolRetentionMode
	ContextCheckpointStore   agent.ContextCheckpointStore
	HistoryBoundaries        []string
	CurrentMessageBoundary   string
	DisableContextCompaction bool
	RuntimeToolsConfig       *toolsutil.RuntimeToolsRegisterConfig
	CreateImageClient        func() (llm.ImageClient, error)
}

type RunResult struct {
	Final        *agent.Final
	Context      *agent.Context
	LoadedSkills []string
}

type boundSubtaskRunner struct {
	runtime *Runtime
	route   llmutil.ResolvedRoute
}

func (r boundSubtaskRunner) RunSubtask(ctx context.Context, req agent.SubtaskRequest) (*agent.SubtaskResult, error) {
	route := r.route
	return r.runtime.runSubtask(ctx, req, &route)
}

func (rt *Runtime) PrepareContextHistory(ctx context.Context, conversationKey string, history []chathistory.ChatHistoryItem, current chathistory.ChatHistoryItem) (contextcheckpoint.PreparedHistory, error) {
	if rt == nil {
		return contextcheckpoint.PreparedHistory{}, fmt.Errorf("task runtime is nil")
	}
	return contextcheckpoint.PrepareHistory(ctx, rt.contextCheckpointRoot(), conversationKey, history, current)
}

func (rt *Runtime) ResetContextHistory(ctx context.Context, conversationKey string) error {
	if rt == nil {
		return fmt.Errorf("task runtime is nil")
	}
	return contextcheckpoint.Reset(ctx, rt.contextCheckpointRoot(), conversationKey)
}

func (rt *Runtime) contextCheckpointRoot() string {
	root := strings.TrimSpace(rt.commonDeps.RuntimePaths.CheckpointRoot)
	if root == "" {
		root = strings.TrimSpace(rt.EngineToolsConfig.PathRoots.FileStateDir)
	}
	if root == "" {
		root = strings.TrimSpace(rt.commonDeps.RuntimeToolsConfig.Image.FileStateDir)
	}
	return root
}

// NewRunPreparer creates the shared task preparation core without opening an
// LLM client. It is intended for entry points whose prepared result owns every
// client created for one run.
func NewRunPreparer(d depsutil.CommonDependencies, opts BootstrapOptions) (*Runtime, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	logger, err := d.Logger()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	logOpts := agent.LogOptions{}
	if d.LogOptions != nil {
		logOpts = d.LogOptions()
	}
	imageSession := imagesession.NewStore(d.RuntimeToolsConfig.Image.FileStateDir)
	var baseRegistry *tools.Registry
	if d.Registry != nil {
		baseRegistry = d.Registry()
	}
	if baseRegistry == nil {
		baseRegistry = tools.NewRegistry()
	}
	engineToolsConfig := agent.DefaultEngineToolsConfig()
	if opts.EngineToolsConfig != nil {
		engineToolsConfig = *opts.EngineToolsConfig
	}
	engineToolsConfig.PathRoots = pathroots.New(
		engineToolsConfig.PathRoots.WorkspaceDir,
		firstNonEmpty(engineToolsConfig.PathRoots.FileCacheDir, d.RuntimeToolsConfig.Image.FileCacheDir),
		firstNonEmpty(engineToolsConfig.PathRoots.FileStateDir, d.RuntimeToolsConfig.Image.FileStateDir),
	)
	var sharedGuard *guard.Guard
	if d.Guard != nil {
		sharedGuard, err = d.Guard(logger)
		if err != nil {
			var closeErr error
			if sharedGuard != nil {
				closeErr = sharedGuard.Close()
			}
			return nil, fmt.Errorf("initialize guard: %w", errors.Join(err, closeErr))
		}
	}
	var acpAgents []acpclient.AgentConfig
	if d.ACPAgents != nil {
		acpAgents = d.ACPAgents()
	}
	return &Runtime{
		commonDeps:            d,
		bootstrapClientOwners: &runtimeClientOwners{},
		Logger:                logger,
		LogOptions:            logOpts,
		AgentConfig:           opts.AgentConfig,
		EngineToolsConfig:     engineToolsConfig,
		ClientDecorator:       opts.ClientDecorator,
		BaseRegistry:          baseRegistry,
		SharedGuard:           sharedGuard,
		ACPAgents:             acpAgents,
		ImageSession:          imageSession,
		imageRetention:        toolsutil.NewImageToolRetentionStore(),
	}, nil
}

func Bootstrap(d depsutil.CommonDependencies, opts BootstrapOptions) (*Runtime, error) {
	rt, err := NewRunPreparer(d, opts)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Runtime, error) {
		return nil, errors.Join(err, rt.Close())
	}
	mainRoute, err := d.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
	if err != nil {
		return fail(err)
	}
	bootstrapClientOwners := rt.bootstrapClientOwners
	mainClient, err := d.CreateLLMClient(mainRoute)
	mainClient = bootstrapClientOwners.own(mainClient)
	if err != nil {
		return fail(err)
	}
	mainClient = decorateOwnedRuntimeClient(bootstrapClientOwners, opts.ClientDecorator, mainClient, mainRoute)
	mainModel := strings.TrimSpace(mainRoute.ClientConfig.Model)

	planRoute, err := d.ResolveLLMRoute(llmutil.RoutePurposePlanCreate)
	if err != nil {
		return fail(err)
	}
	planClient := mainClient
	if !planRoute.SameProfile(mainRoute) {
		createdPlanClient, createErr := d.CreateLLMClient(planRoute)
		planClient = bootstrapClientOwners.own(createdPlanClient)
		err = createErr
		if err != nil {
			return fail(err)
		}
		planClient = decorateOwnedRuntimeClient(bootstrapClientOwners, opts.ClientDecorator, createdPlanClient, planRoute)
	}
	rt.BootstrapMainRoute = mainRoute
	rt.BootstrapMainClient = mainClient
	rt.BootstrapMainModel = mainModel
	rt.BootstrapMainProvider = strings.TrimSpace(mainRoute.ClientConfig.Provider)
	rt.PlanRoute = planRoute
	rt.PlanClient = planClient
	rt.PlanModel = strings.TrimSpace(planRoute.ClientConfig.Model)
	return rt, nil
}

func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	rt.closeOnce.Do(func() {
		clientErr := closeRuntimeClientScope(rt.Logger, rt.bootstrapClientOwners, rt.PlanClient, rt.BootstrapMainClient)
		var guardErr error
		if rt.SharedGuard != nil {
			guardErr = rt.SharedGuard.Close()
			if guardErr != nil && rt.Logger != nil {
				rt.Logger.Warn("task_runtime_guard_close_failed", "error", guardErr.Error())
			}
		}
		rt.closeErr = errors.Join(clientErr, guardErr)
	})
	return rt.closeErr
}

// OwnBootstrapClient puts a client under the runtime's bootstrap lifecycle.
// Callers must do this before applying a client decorator.
func (rt *Runtime) OwnBootstrapClient(client llm.Client) llm.Client {
	if rt == nil {
		return client
	}
	return rt.bootstrapClientOwners.own(client)
}

type preparedRuntimeRun struct {
	task                string
	model               string
	route               llmutil.ResolvedRoute
	scene               string
	memoryContext       string
	logger              *slog.Logger
	engine              *agent.Engine
	loadedSkills        []string
	cleanup             func() error
	contextWindowTokens int64
}

// PreparedEngine owns the Engine and all per-run clients created while
// preparing it. Cleanup is safe to call more than once.
type PreparedEngine struct {
	Engine              *agent.Engine
	Route               llmutil.ResolvedRoute
	Model               string
	LoadedSkills        []string
	ContextWindowTokens int64
	prepared            *preparedRuntimeRun
}

func (rt *Runtime) PrepareEngine(ctx context.Context, req RunRequest) (*PreparedEngine, error) {
	prepared, err := rt.prepareRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return &PreparedEngine{
		Engine:              prepared.engine,
		Route:               prepared.route,
		Model:               prepared.model,
		LoadedSkills:        append([]string(nil), prepared.loadedSkills...),
		ContextWindowTokens: prepared.contextWindowTokens,
		prepared:            &prepared,
	}, nil
}

func (p *PreparedEngine) Cleanup() error {
	if p == nil || p.prepared == nil {
		return nil
	}
	return p.prepared.close()
}

func (p preparedRuntimeRun) close() error {
	if p.cleanup != nil {
		return p.cleanup()
	}
	return nil
}

func (rt *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	prepared, err := rt.prepareRun(ctx, req)
	if err != nil {
		return RunResult{}, err
	}
	defer prepared.close()

	contextCompactionOnly := chatcommands.IsContextCompactCommand(prepared.task)
	final, runCtx, err := prepared.engine.Run(ctx, prepared.task, agent.RunOptions{
		Model:                    prepared.model,
		Scene:                    prepared.scene,
		History:                  append([]llm.Message(nil), req.History...),
		Meta:                     cloneMeta(req.Meta),
		MemoryContext:            prepared.memoryContext,
		CurrentMessage:           req.CurrentMessage,
		ReasoningDetails:         req.ReasoningDetails,
		OnStream:                 req.OnStream,
		SteerSource:              req.SteerSource,
		ContextWindowTokens:      prepared.contextWindowTokens,
		ContextCheckpointStore:   req.ContextCheckpointStore,
		HistoryBoundaries:        append([]string(nil), req.HistoryBoundaries...),
		CurrentMessageBoundary:   req.CurrentMessageBoundary,
		DisableContextCompaction: req.DisableContextCompaction,
		ContextCompactionOnly:    contextCompactionOnly,
	})
	if err != nil {
		return RunResult{Final: final, Context: runCtx, LoadedSkills: prepared.loadedSkills}, err
	}
	if !contextCompactionOnly {
		if err := rt.recordMemory(prepared.logger, final, req.Memory); err != nil {
			return RunResult{Final: final, Context: runCtx, LoadedSkills: prepared.loadedSkills}, err
		}
	}
	return RunResult{
		Final:        final,
		Context:      runCtx,
		LoadedSkills: prepared.loadedSkills,
	}, nil
}

func (rt *Runtime) Resume(ctx context.Context, approvalRequestID string, req RunRequest) (RunResult, error) {
	prepared, err := rt.prepareRun(ctx, req)
	if err != nil {
		return RunResult{}, err
	}
	defer prepared.close()

	final, runCtx, err := prepared.engine.ResumeWithOptions(ctx, approvalRequestID, agent.RunOptions{
		Model:                    prepared.model,
		Scene:                    prepared.scene,
		ReasoningDetails:         req.ReasoningDetails,
		OnStream:                 req.OnStream,
		SteerSource:              req.SteerSource,
		ContextWindowTokens:      prepared.contextWindowTokens,
		ContextCheckpointStore:   req.ContextCheckpointStore,
		DisableContextCompaction: req.DisableContextCompaction,
	})
	if err != nil {
		return RunResult{Final: final, Context: runCtx, LoadedSkills: prepared.loadedSkills}, err
	}
	if err := rt.recordMemory(prepared.logger, final, req.Memory); err != nil {
		return RunResult{Final: final, Context: runCtx, LoadedSkills: prepared.loadedSkills}, err
	}
	return RunResult{
		Final:        final,
		Context:      runCtx,
		LoadedSkills: prepared.loadedSkills,
	}, nil
}

func (rt *Runtime) prepareRun(ctx context.Context, req RunRequest) (preparedRuntimeRun, error) {
	if rt == nil {
		return preparedRuntimeRun{}, fmt.Errorf("task runtime is nil")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return preparedRuntimeRun{}, fmt.Errorf("empty task")
	}
	routePurpose := strings.ToLower(strings.TrimSpace(req.RoutePurpose))
	reasoningEffort := strings.TrimSpace(req.ReasoningEffortOverride)
	if thinkTask, ok := chatcommands.ExtractThinkTask(task); ok {
		task = thinkTask
		routePurpose = llmutil.RoutePurposeThink
		reasoningEffort = llmutil.ReasoningEffortXHigh
		if task == "" {
			return preparedRuntimeRun{}, fmt.Errorf("empty task")
		}
	}
	if routePurpose == llmutil.RoutePurposeThink && reasoningEffort == "" {
		reasoningEffort = llmutil.ReasoningEffortXHigh
	}
	logger := rt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	scene := strings.TrimSpace(req.Scene)
	if scene == "" {
		scene = "runtime.loop"
	}
	if routePurpose == "" {
		routePurpose = llmutil.RoutePurposeMainLoop
	}
	var mainRoute llmutil.ResolvedRoute
	var err error
	profile := strings.TrimSpace(req.LLMProfile)
	if req.Route != nil {
		mainRoute = *req.Route
	} else if profile != "" {
		if rt.commonDeps.ResolveLLMRouteWithProfile == nil {
			return preparedRuntimeRun{}, fmt.Errorf("resolve LLM route with profile dependency is missing")
		}
		mainRoute, err = rt.commonDeps.ResolveLLMRouteWithProfile(routePurpose, profile)
	} else {
		mainRoute, err = rt.ResolveRouteForRun(ctx, routePurpose)
	}
	if err != nil {
		return preparedRuntimeRun{}, err
	}
	mainRoute = llmutil.SelectRouteCandidate(mainRoute, routeSelectionKey(ctx))
	if reasoningEffort != "" {
		mainRoute = llmutil.ResolvedRouteWithReasoningEffort(mainRoute, reasoningEffort)
	}
	runClientOwners := &runtimeClientOwners{}
	mainClient, err := rt.createClientForRoute(mainRoute, runClientOwners)
	if err != nil {
		return preparedRuntimeRun{}, err
	}
	planClient := mainClient
	planModel := strings.TrimSpace(mainRoute.ClientConfig.Model)
	var ownedPlanClient llm.Client
	var ownedImageClient llm.ImageClient
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			cleanupErr = closeRuntimeClientScope(logger, runClientOwners, ownedImageClient, ownedPlanClient, mainClient)
		})
		return cleanupErr
	}
	success := false
	defer func() {
		if !success {
			_ = cleanup()
		}
	}()
	if !req.DisableRuntimeTools {
		planRoute, routeErr := rt.ResolveRouteForRun(ctx, llmutil.RoutePurposePlanCreate)
		if routeErr != nil {
			return preparedRuntimeRun{}, routeErr
		}
		planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
		if !planRoute.SameProfile(mainRoute) {
			planClient, err = rt.createClientForRoute(planRoute, runClientOwners)
			if err != nil {
				return preparedRuntimeRun{}, err
			}
			ownedPlanClient = planClient
		}
	}
	systemPromptCacheControl, err := llmutil.SystemPromptCacheControl(mainRoute.Values.CacheTTL)
	if err != nil {
		return preparedRuntimeRun{}, err
	}
	model := strings.TrimSpace(req.Model)
	if model == "" || strings.TrimSpace(routePurpose) == llmutil.RoutePurposeThink {
		model = strings.TrimSpace(mainRoute.ClientConfig.Model)
	}

	reg := req.Registry
	if reg == nil {
		reg = rt.BaseRegistry.Clone()
	}
	var toolTriggers map[string]bool
	runtimeToolsConfig := rt.commonDeps.RuntimeToolsConfig
	if req.RuntimeToolsConfig != nil {
		runtimeToolsConfig = *req.RuntimeToolsConfig
	}
	createImageClient := rt.commonDeps.CreateImageClient
	if req.CreateImageClient != nil {
		createImageClient = req.CreateImageClient
	}
	imageTask := imageToolRegistrationTask(task, req.CurrentMessage)
	imageRetained := false
	imageScope := imagesession.NewScope(req.ImageToolScope)
	imageClient := rt.ImageClient
	if req.CreateImageClient != nil {
		imageClient = nil
	}
	if !req.DisableRuntimeTools {
		if rt.commonDeps.ToolTriggers != nil {
			toolTriggers = rt.commonDeps.ToolTriggers(task)
		}
		if len(rt.ACPAgents) == 0 {
			delete(toolTriggers, toolsutil.BuiltinACPSpawn)
		}
		if rt.commonDeps.RegisterTriggeredStaticTools != nil {
			reg.Remove(toolsutil.BuiltinAgentSend)
			if toolTriggers == nil {
				toolTriggers = make(map[string]bool)
			}
			toolTriggers[toolsutil.BuiltinAgentSend] = true
		}
		if rt.commonDeps.RegisterTriggeredStaticTools != nil && len(toolTriggers) > 0 {
			rt.commonDeps.RegisterTriggeredStaticTools(reg, toolTriggers)
		}
		activeImage := rt.imageSessionHasActive(ctx, logger, imageScope)
		toolTriggers = toolsutil.AddImageToolIntentTriggers(toolTriggers, imageTask, activeImage)
		imageToolTriggered := toolTriggers[toolsutil.BuiltinImageGenerate] || toolTriggers[toolsutil.BuiltinImageEdit]
		if rt.imageRetention != nil {
			imageRetained = rt.imageRetention.Resolve(req.ImageToolScope, req.ImageToolRetention, imageToolTriggered)
		}
		if runtimeToolsConfig.Image.Configured && (imageRetained || imageToolTriggered) && imageClient == nil {
			if createImageClient != nil {
				createdImageClient, imageErr := createImageClient()
				if imageErr != nil && logger != nil {
					logger.Warn("image_client_create_failed", "error", imageErr.Error())
				}
				if imageErr != nil {
					_ = closeRuntimeResources(logger, createdImageClient)
				} else {
					imageClient = createdImageClient
					ownedImageClient = createdImageClient
				}
			}
		}
		toolsutil.RegisterRuntimeTools(reg, runtimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
			DefaultClient:    mainClient,
			DefaultModel:     model,
			PlanCreateClient: planClient,
			PlanCreateModel:  planModel,
			ImageClient:      imageClient,
			ImageSession:     rt.ImageSession,
			ImageScope:       imageScope,
			ImageRetained:    imageRetained,
			ToolTriggers:     toolTriggers,
			PersonaDir:       rt.commonDeps.RuntimePaths.PersonaDir,
		})
	}

	_ = mainRoute
	promptSpec, loadedSkills, err := rt.commonDeps.PromptSpec(ctx, logger, rt.LogOptions, task, mainClient, model, req.StickySkills)
	if err != nil {
		return preparedRuntimeRun{}, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger, rt.commonDeps.RuntimePaths.PersonaDir)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	if !req.DisableTodoWorkflow {
		promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	}
	if req.PromptAugment != nil {
		req.PromptAugment(&promptSpec, reg)
	}
	if imageRetained {
		rt.appendImageSessionBlock(ctx, logger, req.ImageToolScope, &promptSpec)
	}
	memoryContext, err := rt.prepareMemoryContext(logger, req.Memory)
	if err != nil {
		return preparedRuntimeRun{}, err
	}
	if rt.commonDeps.PromptAugment != nil {
		rt.commonDeps.PromptAugment(&promptSpec, reg)
	}
	promptprofile.AppendModelPromptPatches(&promptSpec, model, logger)

	agentCfg := rt.AgentConfig
	agentCfg.DefaultModel = model
	engineToolsConfig := rt.EngineToolsConfig
	if req.EngineToolsConfig != nil {
		engineToolsConfig = *req.EngineToolsConfig
	}
	engineToolsConfig.ToolTriggers = toolTriggers

	engineOpts := []agent.Option{
		agent.WithLogger(logger),
		agent.WithLogOptions(rt.LogOptions),
		agent.WithSubtaskRunner(boundSubtaskRunner{runtime: rt, route: mainRoute}),
		agent.WithEngineToolsConfig(engineToolsConfig),
		agent.WithACPAgents(rt.ACPAgents),
	}
	if systemPromptCacheControl != nil {
		engineOpts = append(engineOpts, agent.WithSystemPromptCacheControl(systemPromptCacheControl))
	}
	if rt.SharedGuard != nil {
		engineOpts = append(engineOpts, agent.WithGuard(rt.SharedGuard))
	}
	if req.Hook != nil {
		engineOpts = append(engineOpts, agent.WithHook(req.Hook))
	}
	if req.PlanStepUpdate != nil {
		engineOpts = append(engineOpts, agent.WithPlanStepUpdate(req.PlanStepUpdate))
	}
	if req.OnToolStart != nil {
		engineOpts = append(engineOpts, agent.WithOnToolStart(req.OnToolStart))
	}
	if req.OnToolCallStart != nil {
		engineOpts = append(engineOpts, agent.WithOnToolCallStart(req.OnToolCallStart))
	}
	if req.OnToolCallDone != nil {
		engineOpts = append(engineOpts, agent.WithOnToolCallDone(req.OnToolCallDone))
	}
	engine := agent.New(
		mainClient,
		reg,
		agentCfg,
		promptSpec,
		engineOpts...,
	)
	success = true
	return preparedRuntimeRun{
		task:                task,
		model:               model,
		route:               mainRoute,
		scene:               scene,
		memoryContext:       memoryContext,
		logger:              logger,
		engine:              engine,
		loadedSkills:        loadedSkills,
		cleanup:             cleanup,
		contextWindowTokens: mainRoute.ClientConfig.ContextWindowTokens,
	}, nil
}

func (rt *Runtime) appendImageSessionBlock(ctx context.Context, logger *slog.Logger, scopeKey string, spec *agent.PromptSpec) {
	if rt == nil || rt.ImageSession == nil || spec == nil {
		return
	}
	scope := imagesession.NewScope(scopeKey)
	if scope.Empty() {
		return
	}
	roots := rt.imageSessionRoots(ctx)
	block, err := rt.ImageSession.PromptBlock(scope, roots, 3)
	if err != nil {
		if logger != nil {
			logger.Warn("image_session_prompt_failed", "error", err.Error())
		}
		return
	}
	if strings.TrimSpace(block.Content) == "" {
		return
	}
	spec.Blocks = append(spec.Blocks, block)
}

func (rt *Runtime) imageSessionHasActive(ctx context.Context, logger *slog.Logger, scope imagesession.Scope) bool {
	if rt == nil || rt.ImageSession == nil || scope.Empty() {
		return false
	}
	active, err := rt.ImageSession.Active(scope, rt.imageSessionRoots(ctx))
	if err != nil {
		if logger != nil && !errors.Is(err, imagesession.ErrActiveImageMissing) {
			logger.Warn("image_session_active_check_failed", "error", err.Error())
		}
		return false
	}
	return active != nil && strings.TrimSpace(active.Path) != ""
}

func (rt *Runtime) imageSessionRoots(ctx context.Context) pathroots.PathRoots {
	if rt == nil {
		return pathroots.Resolve(ctx, pathroots.PathRoots{})
	}
	return pathroots.Resolve(ctx, pathroots.New(
		"",
		rt.commonDeps.RuntimeToolsConfig.Image.FileCacheDir,
		rt.commonDeps.RuntimeToolsConfig.Image.FileStateDir,
	))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (rt *Runtime) ResolveMainRouteForRun(ctx context.Context) (llmutil.ResolvedRoute, error) {
	return rt.ResolveRouteForRun(ctx, llmutil.RoutePurposeMainLoop)
}

func (rt *Runtime) ResolveTaskRouteForRun(ctx context.Context, task string) (llmutil.ResolvedRoute, error) {
	purpose := llmutil.RoutePurposeMainLoop
	think := false
	if _, ok := chatcommands.ExtractThinkTask(task); ok {
		purpose = llmutil.RoutePurposeThink
		think = true
	}
	route, err := rt.ResolveRouteForRun(ctx, purpose)
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
	if think {
		route = llmutil.ResolvedRouteWithReasoningEffort(route, llmutil.ReasoningEffortXHigh)
	}
	return route, nil
}

func (rt *Runtime) ResolveRouteForRun(ctx context.Context, purpose string) (llmutil.ResolvedRoute, error) {
	if rt == nil {
		return llmutil.ResolvedRoute{}, fmt.Errorf("task runtime is nil")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = llmutil.RoutePurposeMainLoop
	}
	route, err := rt.commonDeps.ResolveLLMRoute(purpose)
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
	return llmutil.SelectRouteCandidate(route, routeSelectionKey(ctx)), nil
}

func routeSelectionKey(ctx context.Context) string {
	if runID := strings.TrimSpace(llmstats.RunIDFromContext(ctx)); runID != "" {
		return runID
	}
	return strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx))
}

func (rt *Runtime) RunSubtask(ctx context.Context, req agent.SubtaskRequest) (*agent.SubtaskResult, error) {
	return rt.runSubtask(ctx, req, nil)
}

func (rt *Runtime) runSubtask(ctx context.Context, req agent.SubtaskRequest, route *llmutil.ResolvedRoute) (*agent.SubtaskResult, error) {
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
		Route:               route,
		Scene:               "spawn.subtask",
		Registry:            req.Registry,
		DisableRuntimeTools: true,
		EngineToolsConfig: &agent.EngineToolsConfig{
			SpawnEnabled:    false,
			ACPSpawnEnabled: false,
			CoderEnabled:    false,
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
	return rt.createClientForRoute(route, &runtimeClientOwners{})
}

func (rt *Runtime) createClientForRoute(route llmutil.ResolvedRoute, owners *runtimeClientOwners) (llm.Client, error) {
	if rt == nil {
		return nil, fmt.Errorf("task runtime is nil")
	}
	client, err := rt.commonDeps.CreateLLMClient(route)
	client = owners.own(client)
	if err != nil {
		_ = closeRuntimeResources(rt.Logger, client)
		return nil, err
	}
	if rt.ClientDecorator != nil {
		client = rt.ClientDecorator(client, route)
	}
	return client, nil
}

func decorateOwnedRuntimeClient(owners *runtimeClientOwners, decorator ClientDecorator, client llm.Client, route llmutil.ResolvedRoute) llm.Client {
	client = owners.own(client)
	if decorator != nil && client != nil {
		client = decorator(client, route)
	}
	return client
}

func sameRuntimeClient(a, b llm.Client) bool {
	if a == nil || b == nil {
		return false
	}
	aType := reflect.TypeOf(a)
	if aType != reflect.TypeOf(b) || !aType.Comparable() {
		return false
	}
	return a == b
}

func closeRuntimeClientScope(logger *slog.Logger, owners *runtimeClientOwners, resources ...any) error {
	resources = append(resources, owners.resources()...)
	return closeRuntimeResources(logger, resources...)
}

func closeRuntimeResources(logger *slog.Logger, resources ...any) error {
	seen := make(map[io.Closer]struct{}, len(resources))
	var errs []error
	for _, resource := range resources {
		closer, ok := resource.(io.Closer)
		if !ok || closer == nil {
			continue
		}
		closerType := reflect.TypeOf(closer)
		if closerType != nil && closerType.Comparable() {
			if _, exists := seen[closer]; exists {
				continue
			}
			seen[closer] = struct{}{}
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	err := errors.Join(errs...)
	if err != nil && logger != nil {
		logger.Warn("task_runtime_client_close_failed", "error", err.Error())
	}
	return err
}

func (rt *Runtime) prepareMemoryContext(logger *slog.Logger, hooks MemoryHooks) (string, error) {
	if hooks.PrepareInjection == nil || !hooks.InjectionEnabled || strings.TrimSpace(hooks.SubjectID) == "" {
		return "", nil
	}
	snap, err := hooks.PrepareInjection(hooks.InjectionMaxItems)
	if err != nil {
		logger.Warn("memory_injection_error", memoryLogArgs(hooks, "error", err.Error())...)
		return "", nil
	}
	if strings.TrimSpace(snap) == "" {
		logger.Debug("memory_injection_skipped", memoryLogArgs(hooks, "reason", "empty_snapshot")...)
		return "", nil
	}
	logger.Info("memory_injection_applied", memoryLogArgs(hooks, "snapshot_len", len(snap))...)
	return snap, nil
}

func (rt *Runtime) recordMemory(logger *slog.Logger, final *agent.Final, hooks MemoryHooks) error {
	if hooks.Record == nil || strings.TrimSpace(hooks.SubjectID) == "" {
		return nil
	}
	if hooks.ShouldRecord != nil && !hooks.ShouldRecord(final) {
		return nil
	}
	finalOutput := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
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

func imageToolRegistrationTask(task string, current *llm.Message) string {
	parts := []string{strings.TrimSpace(task)}
	if current != nil {
		if content := strings.TrimSpace(current.Content); content != "" {
			parts = append(parts, content)
		}
		for _, part := range current.Parts {
			if part.Type != llm.PartTypeText {
				continue
			}
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
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
