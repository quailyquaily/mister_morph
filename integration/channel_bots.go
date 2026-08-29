package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	mixinruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/mixin"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

// BotRunner controls a long-running channel bot lifecycle.
type BotRunner interface {
	Run(ctx context.Context) error
	Close() error
}

type TelegramOptions struct {
	BotToken                      string
	AllowedChatIDs                []int64
	PollTimeout                   time.Duration
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	Hooks                         TelegramHooks
}

type SlackOptions struct {
	BotToken                      string
	AppToken                      string
	AllowedTeamIDs                []string
	AllowedChannelIDs             []string
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	Hooks                         SlackHooks
}

type MixinOptions struct {
	ClientID               string
	SessionID              string
	PrivateKey             string
	AllowedConversationIDs []string
	TaskTimeout            time.Duration
	MaxConcurrency         int
}

type TelegramHooks struct {
	OnInbound  func(TelegramInboundEvent)
	OnOutbound func(TelegramOutboundEvent)
	OnError    func(TelegramErrorEvent)
}

type TelegramInboundEvent = telegramruntime.InboundEvent
type TelegramOutboundEvent = telegramruntime.OutboundEvent
type TelegramErrorEvent = telegramruntime.ErrorEvent

type SlackHooks struct {
	OnInbound  func(SlackInboundEvent)
	OnOutbound func(SlackOutboundEvent)
	OnError    func(SlackErrorEvent)
}

type SlackInboundEvent = slackruntime.InboundEvent
type SlackOutboundEvent = slackruntime.OutboundEvent
type SlackErrorEvent = slackruntime.ErrorEvent

func (rt *Runtime) NewTelegramBot(opts TelegramOptions) (BotRunner, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is nil")
	}
	if err := rt.snapshot().InitErr; err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.BotToken) == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}
	return &telegramBotRunner{rt: rt, opts: opts}, nil
}

func (rt *Runtime) NewSlackBot(opts SlackOptions) (BotRunner, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is nil")
	}
	if err := rt.snapshot().InitErr; err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.BotToken) == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	if strings.TrimSpace(opts.AppToken) == "" {
		return nil, fmt.Errorf("slack app token is required")
	}
	return &slackBotRunner{rt: rt, opts: opts}, nil
}

func (rt *Runtime) NewMixinBot(opts MixinOptions) (BotRunner, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is nil")
	}
	if err := rt.snapshot().InitErr; err != nil {
		return nil, err
	}
	credentials, err := mixinapi.ParseCredentials(opts.ClientID, opts.SessionID, opts.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("mixin credentials: %w", err)
	}
	return &mixinBotRunner{rt: rt, opts: opts, credentials: credentials}, nil
}

type telegramBotRunner struct {
	rt    *Runtime
	opts  TelegramOptions
	state runState
}

func (r *telegramBotRunner) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("telegram runner is nil")
	}
	return runChannelLoop(ctx, &r.state, "telegram", r.rt, func(runCtx context.Context, snap runtimeSnapshot) (runErr error) {
		common, cleanup, err := r.rt.prepareChannelDependencies(runCtx, snap)
		if err != nil {
			return err
		}
		defer func() {
			runErr = errors.Join(runErr, cleanup())
		}()
		runOpts, err := channelopts.BuildTelegramRunOptions(snap.Telegram, channelopts.TelegramInput{
			BotToken:                      strings.TrimSpace(r.opts.BotToken),
			AllowedChatIDs:                append([]int64(nil), r.opts.AllowedChatIDs...),
			GroupTriggerMode:              strings.TrimSpace(r.opts.GroupTriggerMode),
			AddressingConfidenceThreshold: r.opts.AddressingConfidenceThreshold,
			AddressingInterjectThreshold:  r.opts.AddressingInterjectThreshold,
			PollTimeout:                   r.opts.PollTimeout,
			TaskTimeout:                   r.opts.TaskTimeout,
			MaxConcurrency:                r.opts.MaxConcurrency,
			Hooks:                         r.runtimeHooks(),
			InspectPrompt:                 r.rt.inspect.Prompt,
			InspectRequest:                r.rt.inspect.Request,
		})
		if err != nil {
			return err
		}
		runOpts.EngineToolsConfig.SpawnEnabled = runOpts.EngineToolsConfig.SpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinSpawn)
		runOpts.EngineToolsConfig.ACPSpawnEnabled = runOpts.EngineToolsConfig.ACPSpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinACPSpawn)
		runOpts.EngineToolsConfig.CoderEnabled = runOpts.EngineToolsConfig.CoderEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinCoder)
		deps := r.rt.telegramDependencies(snap)
		deps.CommonDependencies = common
		return telegramruntime.Run(runCtx, deps, runOpts)
	})
}

func (r *telegramBotRunner) Close() error {
	if r == nil {
		return nil
	}
	return r.state.close()
}

type slackBotRunner struct {
	rt    *Runtime
	opts  SlackOptions
	state runState
}

func (r *slackBotRunner) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("slack runner is nil")
	}
	return runChannelLoop(ctx, &r.state, "slack", r.rt, func(runCtx context.Context, snap runtimeSnapshot) (runErr error) {
		common, cleanup, err := r.rt.prepareChannelDependencies(runCtx, snap)
		if err != nil {
			return err
		}
		defer func() {
			runErr = errors.Join(runErr, cleanup())
		}()
		runOpts := channelopts.BuildSlackRunOptions(snap.Slack, channelopts.SlackInput{
			BotToken:                      strings.TrimSpace(r.opts.BotToken),
			AppToken:                      strings.TrimSpace(r.opts.AppToken),
			AllowedTeamIDs:                append([]string(nil), r.opts.AllowedTeamIDs...),
			AllowedChannelIDs:             append([]string(nil), r.opts.AllowedChannelIDs...),
			GroupTriggerMode:              strings.TrimSpace(r.opts.GroupTriggerMode),
			AddressingConfidenceThreshold: r.opts.AddressingConfidenceThreshold,
			AddressingInterjectThreshold:  r.opts.AddressingInterjectThreshold,
			TaskTimeout:                   r.opts.TaskTimeout,
			MaxConcurrency:                r.opts.MaxConcurrency,
			Hooks:                         r.runtimeHooks(),
			InspectPrompt:                 r.rt.inspect.Prompt,
			InspectRequest:                r.rt.inspect.Request,
		})
		runOpts.EngineToolsConfig.SpawnEnabled = runOpts.EngineToolsConfig.SpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinSpawn)
		runOpts.EngineToolsConfig.ACPSpawnEnabled = runOpts.EngineToolsConfig.ACPSpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinACPSpawn)
		runOpts.EngineToolsConfig.CoderEnabled = runOpts.EngineToolsConfig.CoderEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinCoder)
		deps := r.rt.slackDependencies(snap)
		deps.CommonDependencies = common
		return slackruntime.Run(runCtx, deps, runOpts)
	})
}

func runChannelLoop(ctx context.Context, state *runState, name string, rt *Runtime, run func(context.Context, runtimeSnapshot) error) error {
	name = strings.TrimSpace(name)
	if rt == nil {
		return fmt.Errorf("%s runner is nil", name)
	}
	runCtx, cancel, err := state.begin(ctx, name)
	if err != nil {
		return err
	}
	defer state.end(cancel)
	return run(runCtx, rt.snapshot())
}

func (r *slackBotRunner) Close() error {
	if r == nil {
		return nil
	}
	return r.state.close()
}

type mixinBotRunner struct {
	rt          *Runtime
	opts        MixinOptions
	credentials mixinapi.Credentials
	state       runState
}

func (r *mixinBotRunner) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("mixin runner is nil")
	}
	return runChannelLoop(ctx, &r.state, "mixin", r.rt, func(runCtx context.Context, snap runtimeSnapshot) (runErr error) {
		common, cleanup, err := r.rt.prepareChannelDependencies(runCtx, snap)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, cleanup()) }()
		runOpts := channelopts.BuildMixinRunOptions(snap.Mixin, channelopts.MixinInput{
			AllowedConversationIDs: append([]string(nil), r.opts.AllowedConversationIDs...),
			TaskTimeout:            r.opts.TaskTimeout,
			MaxConcurrency:         r.opts.MaxConcurrency,
			InspectPrompt:          r.rt.inspect.Prompt,
			InspectRequest:         r.rt.inspect.Request,
		})
		runOpts.Credentials = r.credentials
		runOpts.EngineToolsConfig.SpawnEnabled = runOpts.EngineToolsConfig.SpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinSpawn)
		runOpts.EngineToolsConfig.ACPSpawnEnabled = runOpts.EngineToolsConfig.ACPSpawnEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinACPSpawn)
		runOpts.EngineToolsConfig.CoderEnabled = runOpts.EngineToolsConfig.CoderEnabled && r.rt.isBuiltinToolSelected(toolsutil.BuiltinCoder)
		deps := r.rt.mixinDependencies(snap)
		deps.CommonDependencies = common
		return mixinruntime.Run(runCtx, deps, runOpts)
	})
}

func (r *mixinBotRunner) Close() error {
	if r == nil {
		return nil
	}
	return r.state.close()
}

type runState struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func (s *runState) begin(ctx context.Context, name string) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		cancel()
		return nil, nil, fmt.Errorf("%s runner already running", strings.TrimSpace(name))
	}
	s.running = true
	s.cancel = cancel
	return runCtx, cancel, nil
}

func (s *runState) end(cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancel = nil
	s.running = false
	s.mu.Unlock()
	cancel()
}

func (s *runState) close() error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func mapRuntimeHook[T any](fn func(T)) func(context.Context, T) {
	if fn == nil {
		return nil
	}
	return func(_ context.Context, event T) {
		fn(event)
	}
}

func (r *telegramBotRunner) runtimeHooks() telegramruntime.Hooks {
	h := r.opts.Hooks
	return telegramruntime.Hooks{
		OnInbound:  mapRuntimeHook(h.OnInbound),
		OnOutbound: mapRuntimeHook(h.OnOutbound),
		OnError:    mapRuntimeHook(h.OnError),
	}
}

func (r *slackBotRunner) runtimeHooks() slackruntime.Hooks {
	h := r.opts.Hooks
	return slackruntime.Hooks{
		OnInbound:  mapRuntimeHook(h.OnInbound),
		OnOutbound: mapRuntimeHook(h.OnOutbound),
		OnError:    mapRuntimeHook(h.OnError),
	}
}

func (rt *Runtime) prepareChannelDependencies(ctx context.Context, snap runtimeSnapshot) (depsutil.CommonDependencies, func() error, error) {
	if snap.InitErr != nil {
		return depsutil.CommonDependencies{}, nil, snap.InitErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger := snap.Logger
	if logger == nil {
		logger = slog.Default()
	}
	baseRegistry := rt.buildRegistry(snap.StaticRegistry, logger)
	awarenessRegistry := rt.buildAwarenessRegistry(snap.StaticRegistry, logger)
	registration, err := rt.buildDeps.connectMCP(ctx, snap.MCPServers, logger)
	if err != nil {
		if registration.close != nil {
			err = errors.Join(err, registration.close())
		}
		return depsutil.CommonDependencies{}, nil, fmt.Errorf("connect MCP servers: %w", err)
	}
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			if registration.close != nil {
				cleanupErr = registration.close()
			}
		})
		return cleanupErr
	}
	if err := registerIntegrationMCPTools(baseRegistry, registration.tools); err != nil {
		return depsutil.CommonDependencies{}, nil, errors.Join(fmt.Errorf("register MCP tools: %w", err), cleanup())
	}
	if err := registerIntegrationMCPTools(awarenessRegistry, registration.tools); err != nil {
		return depsutil.CommonDependencies{}, nil, errors.Join(fmt.Errorf("register awareness MCP tools: %w", err), cleanup())
	}

	common := rt.sharedDependencies(snap)
	common.Registry = func() *tools.Registry { return baseRegistry.Clone() }
	common.AwarenessRegistry = func() *tools.Registry { return awarenessRegistry.Clone() }
	return common, cleanup, nil
}

func (rt *Runtime) sharedDependencies(snap runtimeSnapshot) depsutil.CommonDependencies {
	planEnabled := rt.features.PlanTool && snap.Registry.ToolsPlanCreateEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinPlanCreate)
	todoEnabled := snap.Registry.ToolsTodoUpdateEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinTodoUpdate)
	imageGenerateEnabled := snap.Registry.ToolsImageGenerateEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinImageGenerate)
	imageEditEnabled := snap.Registry.ToolsImageEditEnabled && rt.isBuiltinToolSelected(toolsutil.BuiltinImageEdit)
	usageClientWrap := integrationUsageClientWrap(snap.Paths, snap.Logger)
	return depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			if snap.Logger != nil {
				return snap.Logger, nil
			}
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions { return cloneLogOptions(snap.LogOptions) },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
				return llmselect.ResolveMainRoute(snap.LLMValues, rt.currentSelection())
			}
			return llmutil.ResolveRoute(snap.LLMValues, purpose)
		},
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRouteWithProfileOverride(snap.LLMValues, purpose, profile)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return rt.buildLLMClient(route, snap.Logger, usageClientWrap)
		},
		CreateImageClient: func() (llm.ImageClient, error) {
			client, err := rt.buildDeps.buildImageClient(snap.LLMValues, snap.Logger)
			if err != nil {
				return client, err
			}
			meta := llmutil.ResolveImageClientMetadata(snap.LLMValues)
			return llmstats.WrapImageClient(client, llmstats.ClientOptions{
				Provider:     meta.Provider,
				APIBase:      meta.Endpoint,
				DefaultModel: meta.Model,
				JournalDir:   snap.Paths.LLMUsageJournalDir,
				Logger:       snap.Logger,
			}), nil
		},
		Registry:          func() *tools.Registry { return rt.buildRegistry(snap.StaticRegistry, snap.Logger) },
		AwarenessRegistry: func() *tools.Registry { return rt.buildAwarenessRegistry(snap.StaticRegistry, snap.Logger) },
		ToolTriggers: func(task string) map[string]bool {
			refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, snap.SkillsConfig))
			for name := range refs {
				if !rt.isBuiltinToolSelected(name) {
					delete(refs, name)
				}
			}
			if !rt.features.PlanTool {
				delete(refs, toolsutil.BuiltinPlanCreate)
			}
			if len(snap.ACPAgents) == 0 {
				delete(refs, toolsutil.BuiltinACPSpawn)
			}
			return refs
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			rt.registerStaticTools(reg, snap.StaticRegistry, snap.Logger, false, triggers)
		},
		ACPAgents: func() []acpclient.AgentConfig {
			return acpclient.CloneAgents(snap.ACPAgents)
		},
		RuntimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{
			PlanCreate: toolsutil.BuildPlanCreateRegisterConfig(planEnabled, snap.Registry.ToolsPlanCreateMaxSteps),
			TodoUpdate: toolsutil.TodoUpdateRegisterConfig{
				Enabled:     todoEnabled,
				CronPath:    snap.Paths.CronPath,
				ContactsDir: snap.Paths.ContactsDir,
			},
			Image: imageToolsRegisterConfigFromSnapshot(snap, snap.LLMValues, imageGenerateEnabled, imageEditEnabled),
		},
		RuntimePaths:           snap.Paths,
		DefaultWorkspaceDir:    snap.DefaultWorkspaceDir,
		AgentSettingsReader:    snap.AgentSettings,
		TaskPersistenceTargets: append([]string(nil), snap.Registry.TaskPersistenceTargets...),
		TaskRotateMaxBytes:     snap.Registry.TasksRotateMaxBytes,
		Guard:                  func(logger *slog.Logger) (*guard.Guard, error) { return rt.buildGuard(snap.Guard, logger) },
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			return rt.promptSpecWithSkillsFromConfig(ctx, logger, logOpts, task, client, model, snap.SkillsConfig, stickySkills)
		},
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			_ = reg
			rt.appendPromptBlocks(spec)
		},
	}
}

func (rt *Runtime) telegramDependencies(snap runtimeSnapshot) telegramruntime.Dependencies {
	base := rt.sharedDependencies(snap)
	return telegramruntime.Dependencies{
		CommonDependencies: base,
		HandleModelCommand: func(text string) (string, bool, error) {
			return llmselect.ExecuteCommandText(snap.LLMValues, rt.selection, text)
		},
	}
}

func (rt *Runtime) slackDependencies(snap runtimeSnapshot) slackruntime.Dependencies {
	base := rt.sharedDependencies(snap)
	return slackruntime.Dependencies{
		CommonDependencies: base,
		HandleModelCommand: func(text string) (string, bool, error) {
			return llmselect.ExecuteCommandText(snap.LLMValues, rt.selection, text)
		},
	}
}

func (rt *Runtime) mixinDependencies(snap runtimeSnapshot) mixinruntime.Dependencies {
	base := rt.sharedDependencies(snap)
	return mixinruntime.Dependencies{
		CommonDependencies: base,
		HandleModelCommand: func(text string) (string, bool, error) {
			return llmselect.ExecuteCommandText(snap.LLMValues, rt.selection, text)
		},
		HandleSkillCommand: func(currentLoaded []string) (string, error) {
			return skillsutil.RenderSkillStatus(snap.SkillsConfig, currentLoaded)
		},
	}
}

func (rt *Runtime) promptSpecWithSkillsFromConfig(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, base skillsutil.SkillsConfig, stickySkills []string) (agent.PromptSpec, []string, error) {
	if rt == nil {
		return agent.PromptSpec{}, nil, fmt.Errorf("runtime is nil")
	}
	if !rt.features.Skills {
		return agent.DefaultPromptSpec(), nil, nil
	}
	cfg := cloneSkillsConfig(base)
	if len(stickySkills) > 0 {
		cfg.Requested = append(cfg.Requested, stickySkills...)
	}
	return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
}
