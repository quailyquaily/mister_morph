package consolecmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
	mixinruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/mixin"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

const (
	managedRuntimeTelegram = "telegram"
	managedRuntimeSlack    = "slack"
	managedRuntimeLark     = "lark"
	managedRuntimeMixin    = "mixin"
)

type managedRuntimeSupervisor struct {
	transitionMu    sync.Mutex
	mu              sync.Mutex
	kinds           []string
	configReader    *viper.Viper
	pendingPrepared *managedRuntimePrepared
	inspectPrompt   bool
	inspectRequest  bool
	localRuntime    *consoleLocalRuntime
	parentCtx       context.Context
	active          *managedRuntimeExecution
	onFatal         func(error)
	generation      uint64
}

type managedRuntimeExecution struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type managedRuntimePrepared struct {
	reader   *viper.Viper
	kinds    []string
	children []managedPreparedRuntime
}

type managedPreparedRuntime struct {
	kind    string
	run     func(context.Context) error
	cleanup func()
}

func normalizeManagedRuntimeKinds(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		kind := strings.ToLower(strings.TrimSpace(item))
		if kind == "" {
			continue
		}
		switch kind {
		case managedRuntimeTelegram, managedRuntimeSlack, managedRuntimeLark, managedRuntimeMixin:
		default:
			return nil, fmt.Errorf("unsupported console.managed_runtimes entry %q", item)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out, nil
}

func newManagedRuntimeSupervisor(localRuntime *consoleLocalRuntime, inspectPrompt bool, inspectRequest bool) *managedRuntimeSupervisor {
	return &managedRuntimeSupervisor{
		inspectPrompt:  inspectPrompt,
		inspectRequest: inspectRequest,
		localRuntime:   localRuntime,
	}
}

func (s *managedRuntimeSupervisor) Start(ctx context.Context, onFatal func(error)) error {
	if s == nil {
		return nil
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.Lock()
	if ctx == nil {
		ctx = context.Background()
	}
	s.parentCtx = ctx
	s.onFatal = onFatal
	if s.pendingPrepared == nil && s.configReader != nil {
		prepared, err := s.prepareReloadLocked(s.configReader)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.pendingPrepared = prepared
	}
	prepared := s.pendingPrepared
	s.mu.Unlock()
	return s.applyPrepared(prepared)
}

func (s *managedRuntimeSupervisor) ReloadConfig(reader *viper.Viper) error {
	if s == nil {
		return nil
	}
	prepared, err := s.PrepareReload(reader)
	if err != nil {
		return err
	}
	return s.ApplyPrepared(prepared)
}

func (s *managedRuntimeSupervisor) PrepareReload(reader *viper.Viper) (*managedRuntimePrepared, error) {
	if s == nil {
		return &managedRuntimePrepared{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prepareReloadLocked(reader)
}

func (s *managedRuntimeSupervisor) prepareReloadLocked(reader *viper.Viper) (*managedRuntimePrepared, error) {
	if reader == nil {
		return nil, fmt.Errorf("managed runtime config reader is required")
	}
	kinds, err := managedRuntimeKindsFromReader(reader)
	if err != nil {
		return nil, err
	}
	prepared := &managedRuntimePrepared{
		reader: reader,
	}
	for _, kind := range kinds {
		if field, hint, missing := managedRuntimeMissingCredential(kind, reader); missing {
			s.logger().Warn("managed_runtime_channel_disabled", "channel", kind, "missing", field, "hint", hint)
			continue
		}
		run, cleanup, err := s.buildRuntime(kind, reader)
		if err != nil {
			prepared.cleanup()
			return nil, err
		}
		if run == nil {
			if cleanup != nil {
				cleanup()
			}
			continue
		}
		prepared.kinds = append(prepared.kinds, kind)
		prepared.children = append(prepared.children, managedPreparedRuntime{
			kind:    kind,
			run:     run,
			cleanup: cleanup,
		})
	}
	return prepared, nil
}

func (p *managedRuntimePrepared) cleanup() {
	if p == nil {
		return
	}
	for _, child := range p.children {
		if child.cleanup != nil {
			child.cleanup()
		}
	}
}

func (s *managedRuntimeSupervisor) ApplyPrepared(prepared *managedRuntimePrepared) error {
	if s == nil {
		if prepared != nil {
			prepared.cleanup()
		}
		return nil
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	return s.applyPrepared(prepared)
}

func (s *managedRuntimeSupervisor) applyPrepared(prepared *managedRuntimePrepared) error {
	if prepared == nil {
		prepared = &managedRuntimePrepared{reader: viper.New()}
	}
	s.mu.Lock()
	var replacedPending *managedRuntimePrepared
	if s.pendingPrepared != nil && s.pendingPrepared != prepared {
		replacedPending = s.pendingPrepared
	}
	s.pendingPrepared = nil
	if s.parentCtx == nil {
		s.configReader = prepared.reader
		s.kinds = append([]string(nil), prepared.kinds...)
		s.pendingPrepared = prepared
		s.mu.Unlock()
		if replacedPending != nil {
			replacedPending.cleanup()
		}
		return nil
	}
	active, oldKinds := s.detachActiveLocked()
	parentCtx := s.parentCtx
	localRuntime := s.localRuntime
	s.mu.Unlock()

	if replacedPending != nil {
		replacedPending.cleanup()
	}
	stopManagedRuntimeExecution(active, localRuntime, oldKinds)

	s.mu.Lock()
	s.configReader = prepared.reader
	s.kinds = append([]string(nil), prepared.kinds...)
	if len(prepared.children) == 0 {
		s.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(parentCtx)
	active = &managedRuntimeExecution{cancel: cancel}
	active.wg.Add(len(prepared.children))
	s.active = active
	s.generation++
	generation := s.generation
	children := append([]managedPreparedRuntime(nil), prepared.children...)
	s.mu.Unlock()
	for _, child := range children {
		if localRuntime != nil {
			localRuntime.SetManagedRuntimeRunning(child.kind, true)
		}
		go func(child managedPreparedRuntime) {
			defer active.wg.Done()
			s.runManagedRuntime(runCtx, generation, child.kind, child.run, child.cleanup)
		}(child)
	}
	return nil
}

func (s *managedRuntimeSupervisor) Close() {
	if s == nil {
		return
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.Lock()
	active, oldKinds := s.detachActiveLocked()
	pendingPrepared := s.pendingPrepared
	s.pendingPrepared = nil
	s.parentCtx = nil
	s.onFatal = nil
	localRuntime := s.localRuntime
	s.mu.Unlock()

	stopManagedRuntimeExecution(active, localRuntime, oldKinds)
	if pendingPrepared != nil {
		pendingPrepared.cleanup()
	}
}

func (s *managedRuntimeSupervisor) detachActiveLocked() (*managedRuntimeExecution, []string) {
	active := s.active
	s.active = nil
	oldKinds := append([]string(nil), s.kinds...)
	if active != nil {
		// Make the departing children stale before cancellation. They can then
		// finish without racing a replacement generation's state callbacks.
		s.generation++
	}
	return active, oldKinds
}

func stopManagedRuntimeExecution(active *managedRuntimeExecution, localRuntime *consoleLocalRuntime, kinds []string) {
	if active != nil {
		active.cancel()
		active.wg.Wait()
	}
	if localRuntime != nil {
		for _, kind := range kinds {
			localRuntime.SetManagedRuntimeRunning(kind, false)
			if kind == managedRuntimeMixin {
				localRuntime.mixinConnected.Store(false)
			}
		}
	}
}

func (s *managedRuntimeSupervisor) buildRuntime(kind string, reader *viper.Viper) (func(context.Context) error, func(), error) {
	if reader == nil {
		return nil, nil, fmt.Errorf("managed runtime config reader is required")
	}
	runtimeValues, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return nil, nil, err
	}
	switch kind {
	case managedRuntimeTelegram:
		botToken := strings.TrimSpace(reader.GetString("telegram.bot_token"))
		deps, cleanup, err := buildManagedRuntimeDepsFromReader(s.logger(), reader)
		if err != nil {
			return nil, nil, err
		}
		cfg := channelopts.TelegramConfigFromReader(reader)
		runOpts, err := channelopts.BuildTelegramRunOptions(cfg, channelopts.TelegramInput{
			BotToken:       botToken,
			InspectPrompt:  s.inspectPrompt,
			InspectRequest: s.inspectRequest,
		})
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runOpts.Server.Listen = ""
		runOpts.Server.AuthToken = ""
		runOpts.Server.Poke = nil
		runOpts.TaskStore, err = newManagedRuntimeTaskStore(kind, runOpts.Server.MaxQueue, deps)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runtimeDeps := telegramruntime.Dependencies{
			CommonDependencies: deps,
			HandleModelCommand: func(text string) (string, bool, error) {
				return llmselect.ExecuteCommandText(runtimeValues, llmselect.ProcessStore(), text)
			},
			HandleSkillCommand: func(currentLoaded []string) (string, error) {
				return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromReader(reader), currentLoaded)
			},
		}
		return func(ctx context.Context) error {
			return telegramruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	case managedRuntimeSlack:
		botToken := strings.TrimSpace(reader.GetString("slack.bot_token"))
		appToken := strings.TrimSpace(reader.GetString("slack.app_token"))
		deps, cleanup, err := buildManagedRuntimeDepsFromReader(s.logger(), reader)
		if err != nil {
			return nil, nil, err
		}
		cfg := channelopts.SlackConfigFromReader(reader)
		runOpts := channelopts.BuildSlackRunOptions(cfg, channelopts.SlackInput{
			BotToken:       botToken,
			AppToken:       appToken,
			InspectPrompt:  s.inspectPrompt,
			InspectRequest: s.inspectRequest,
		})
		runOpts.Server.Listen = ""
		runOpts.Server.AuthToken = ""
		runOpts.Server.Poke = nil
		taskStore, err := newManagedRuntimeTaskStore(kind, runOpts.Server.MaxQueue, deps)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runOpts.TaskStore = taskStore
		runtimeDeps := slackruntime.Dependencies{
			CommonDependencies: deps,
			HandleModelCommand: func(text string) (string, bool, error) {
				return llmselect.ExecuteCommandText(runtimeValues, llmselect.ProcessStore(), text)
			},
			HandleSkillCommand: func(currentLoaded []string) (string, error) {
				return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromReader(reader), currentLoaded)
			},
		}
		return func(ctx context.Context) error {
			return slackruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	case managedRuntimeLark:
		appID := strings.TrimSpace(reader.GetString("lark.app_id"))
		appSecret := strings.TrimSpace(reader.GetString("lark.app_secret"))
		deps, cleanup, err := buildManagedRuntimeDepsFromReader(s.logger(), reader)
		if err != nil {
			return nil, nil, err
		}
		cfg := channelopts.LarkConfigFromReader(reader)
		runOpts := channelopts.BuildLarkRunOptions(cfg, channelopts.LarkInput{
			AppID:          appID,
			AppSecret:      appSecret,
			InspectPrompt:  s.inspectPrompt,
			InspectRequest: s.inspectRequest,
		})
		runOpts.ServerListen = ""
		runOpts.ServerAuthToken = ""
		runOpts.TaskStore, err = newManagedRuntimeTaskStore(kind, runOpts.ServerMaxQueue, deps)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runtimeDeps := larkruntime.Dependencies{
			CommonDependencies: deps,
			HandleModelCommand: func(text string) (string, bool, error) {
				return llmselect.ExecuteCommandText(runtimeValues, llmselect.ProcessStore(), text)
			},
			HandleSkillCommand: func(currentLoaded []string) (string, error) {
				return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromReader(reader), currentLoaded)
			},
		}
		return func(ctx context.Context) error {
			return larkruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	case managedRuntimeMixin:
		keystoreFile := pathutil.ResolveConfigRelativePath(reader.GetString("mixin.keystore_file"), reader.GetString("config"))
		deps, cleanup, err := buildManagedRuntimeDepsFromReader(s.logger(), reader)
		if err != nil {
			return nil, nil, err
		}
		cfg := channelopts.MixinConfigFromReader(reader)
		runOpts := channelopts.BuildMixinRunOptions(cfg, channelopts.MixinInput{
			KeystoreFile:  keystoreFile,
			InspectPrompt: s.inspectPrompt, InspectRequest: s.inspectRequest,
		})
		runOpts.ServerListen = ""
		runOpts.ServerAuthToken = ""
		runOpts.OnConnectionChange = func(connected bool) {
			if s.localRuntime != nil {
				s.localRuntime.mixinConnected.Store(connected)
			}
		}
		runOpts.TaskStore, err = newManagedRuntimeTaskStore(kind, runOpts.ServerMaxQueue, deps)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runtimeDeps := mixinruntime.Dependencies{
			CommonDependencies: deps,
			HandleModelCommand: func(text string) (string, bool, error) {
				return llmselect.ExecuteCommandText(runtimeValues, llmselect.ProcessStore(), text)
			},
			HandleSkillCommand: func(currentLoaded []string) (string, error) {
				return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromReader(reader), currentLoaded)
			},
		}
		return func(ctx context.Context) error {
			return mixinruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	default:
		return nil, nil, fmt.Errorf("unsupported managed runtime %q", kind)
	}
}

func managedRuntimeMissingCredential(kind string, reader *viper.Viper) (string, string, bool) {
	if reader == nil {
		return "runtime config", "runtime config reader is required", true
	}
	switch kind {
	case managedRuntimeTelegram:
		if strings.TrimSpace(reader.GetString("telegram.bot_token")) == "" {
			return "telegram.bot_token", "set MISTER_MORPH_TELEGRAM_BOT_TOKEN or telegram.bot_token", true
		}
	case managedRuntimeSlack:
		if strings.TrimSpace(reader.GetString("slack.bot_token")) == "" {
			return "slack.bot_token", "set MISTER_MORPH_SLACK_BOT_TOKEN or slack.bot_token", true
		}
		if strings.TrimSpace(reader.GetString("slack.app_token")) == "" {
			return "slack.app_token", "set MISTER_MORPH_SLACK_APP_TOKEN or slack.app_token", true
		}
	case managedRuntimeLark:
		if strings.TrimSpace(reader.GetString("lark.app_id")) == "" {
			return "lark.app_id", "set MISTER_MORPH_LARK_APP_ID or lark.app_id", true
		}
		if strings.TrimSpace(reader.GetString("lark.app_secret")) == "" {
			return "lark.app_secret", "set MISTER_MORPH_LARK_APP_SECRET or lark.app_secret", true
		}
	case managedRuntimeMixin:
		if strings.TrimSpace(reader.GetString("mixin.keystore_file")) == "" {
			return "mixin.keystore_file", "set MISTER_MORPH_MIXIN_KEYSTORE_FILE or mixin.keystore_file", true
		}
	}
	return "", "", false
}

func (s *managedRuntimeSupervisor) logger() *slog.Logger {
	if s != nil && s.localRuntime != nil {
		return s.localRuntime.currentLogger()
	}
	return slog.Default()
}

func managedRuntimeKindsFromReader(r interface {
	GetStringSlice(string) []string
}) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	return normalizeManagedRuntimeKinds(r.GetStringSlice("console.managed_runtimes"))
}

func newManagedRuntimeTaskStore(kind string, maxItems int, deps depsutil.CommonDependencies) (daemonruntime.TaskView, error) {
	switch kind {
	case managedRuntimeTelegram, managedRuntimeSlack, managedRuntimeLark, managedRuntimeMixin:
		return daemonruntime.NewTaskViewForTarget(kind, maxItems, daemonruntime.TaskViewConfig{
			PersistenceTargets: deps.TaskPersistenceTargets,
			TasksDir:           deps.RuntimePaths.TasksDir,
			JournalDir:         deps.RuntimePaths.JournalDir,
			RotateMaxBytes:     deps.TaskRotateMaxBytes,
		})
	default:
		return nil, fmt.Errorf("unsupported managed runtime %q", kind)
	}
}

func (s *managedRuntimeSupervisor) runManagedRuntime(ctx context.Context, generation uint64, kind string, run func(context.Context) error, cleanup func()) {
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	err := run(ctx)
	localRuntime, onFatal, current := s.currentGenerationCallbacks(generation)
	if !current {
		return
	}
	if localRuntime != nil {
		localRuntime.SetManagedRuntimeRunning(kind, false)
	}
	if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return
	}
	if onFatal != nil {
		onFatal(fmt.Errorf("managed runtime %s failed: %w", kind, err))
	}
}

func (s *managedRuntimeSupervisor) currentGenerationCallbacks(generation uint64) (*consoleLocalRuntime, func(error), bool) {
	if s == nil {
		return nil, nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localRuntime, s.onFatal, s.generation == generation
}

func buildManagedRuntimeDepsFromReader(logger *slog.Logger, reader *viper.Viper) (depsutil.CommonDependencies, func(), error) {
	if logger == nil {
		logger = slog.Default()
	}
	if reader == nil {
		return depsutil.CommonDependencies{}, nil, fmt.Errorf("managed runtime config reader is required")
	}
	runtimeValues, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return depsutil.CommonDependencies{}, nil, err
	}
	staticRegistryConfig, err := toolsutil.StaticRegistryConfigFromReader(reader)
	if err != nil {
		return depsutil.CommonDependencies{}, nil, err
	}
	logOpts := logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(reader))
	baseRegistry, awarenessRegistry, mcpHost, err := buildConsoleRegistriesFromReader(context.Background(), logger, reader)
	if err != nil {
		return depsutil.CommonDependencies{}, nil, err
	}
	guardSnapshot, err := guard.SnapshotFromReader(reader)
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return depsutil.CommonDependencies{}, nil, err
	}
	paths := runtimepaths.FromReader(reader)
	deps := depsutil.CommonDependencies{
		DefaultWorkspaceDir: strings.TrimSpace(reader.GetString("workspace_dir")),
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return logOpts
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
		RuntimeToolsConfig:     toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
		RuntimePaths:           paths,
		AgentSettingsReader:    agentsettings.NewReaderSnapshot(reader),
		TaskPersistenceTargets: append([]string(nil), reader.GetStringSlice("tasks.persistence_targets")...),
		TaskRotateMaxBytes:     reader.GetInt64("tasks.rotate_max_bytes"),
		ToolTriggers: func(task string) map[string]bool {
			cfg := skillsutil.SkillsConfigFromReader(reader)
			refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, cfg))
			if len(acpclient.AgentsFromReader(reader)) == 0 {
				delete(refs, toolsutil.BuiltinACPSpawn)
			}
			return refs
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			toolsutil.RegisterStaticTools(reg, staticRegistryConfig, nil, triggers)
		},
		ACPAgents: func() []acpclient.AgentConfig {
			return acpclient.AgentsFromReader(reader)
		},
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		AwarenessRegistry: func() *tools.Registry {
			return awarenessRegistry
		},
		Guard: func(guardLogger *slog.Logger) (*guard.Guard, error) {
			if guardLogger == nil {
				guardLogger = logger
			}
			return guard.NewChecked(guardSnapshot, guardLogger)
		},
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := skillsutil.SkillsConfigFromReader(reader)
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
	}
	return deps, func() {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
	}, nil
}
