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
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

const (
	managedRuntimeTelegram = "telegram"
	managedRuntimeSlack    = "slack"
)

type managedRuntimeSupervisor struct {
	mu              sync.Mutex
	kinds           []string
	configReader    *viper.Viper
	pendingPrepared *managedRuntimePrepared
	inspectPrompt   bool
	inspectRequest  bool
	localRuntime    *consoleLocalRuntime
	parentCtx       context.Context
	cancel          context.CancelFunc
	onFatal         func(error)
	generation      uint64
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

type managedRuntimeConfigError struct {
	err error
}

func (e managedRuntimeConfigError) Error() string {
	if e.err == nil {
		return "invalid managed runtime config"
	}
	return e.err.Error()
}

func (e managedRuntimeConfigError) Unwrap() error {
	return e.err
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
		case managedRuntimeTelegram, managedRuntimeSlack:
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	s.parentCtx = ctx
	s.onFatal = onFatal
	if s.pendingPrepared == nil && s.configReader != nil {
		prepared, err := s.prepareReloadLocked(s.configReader)
		if err != nil {
			return err
		}
		s.pendingPrepared = prepared
	}
	return s.applyPreparedLocked(s.pendingPrepared)
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
		reader = viper.GetViper()
	}
	kinds, err := managedRuntimeKindsFromReader(reader)
	if err != nil {
		return nil, err
	}
	prepared := &managedRuntimePrepared{
		reader: reader,
		kinds:  append([]string(nil), kinds...),
	}
	for _, kind := range kinds {
		run, cleanup, err := s.buildRuntime(kind, reader)
		if err != nil {
			prepared.cleanup()
			return nil, err
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyPreparedLocked(prepared)
}

func (s *managedRuntimeSupervisor) applyPreparedLocked(prepared *managedRuntimePrepared) error {
	if prepared == nil {
		prepared = &managedRuntimePrepared{reader: viper.New()}
	}
	if s.pendingPrepared != nil && s.pendingPrepared != prepared {
		s.pendingPrepared.cleanup()
	}
	s.pendingPrepared = nil
	if s.parentCtx == nil {
		s.configReader = prepared.reader
		s.kinds = append([]string(nil), prepared.kinds...)
		s.pendingPrepared = prepared
		return nil
	}
	s.stopLocked()
	s.configReader = prepared.reader
	s.kinds = append([]string(nil), prepared.kinds...)
	if len(prepared.children) == 0 {
		return nil
	}
	runCtx, cancel := context.WithCancel(s.parentCtx)
	s.cancel = cancel
	s.generation++
	generation := s.generation
	for _, child := range prepared.children {
		if s.localRuntime != nil {
			s.localRuntime.SetManagedRuntimeRunning(child.kind, true)
		}
		go s.runManagedRuntime(runCtx, generation, child.kind, child.run, child.cleanup)
	}
	return nil
}

func (s *managedRuntimeSupervisor) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	if s.pendingPrepared != nil {
		s.pendingPrepared.cleanup()
		s.pendingPrepared = nil
	}
	s.parentCtx = nil
	s.onFatal = nil
}

func (s *managedRuntimeSupervisor) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.localRuntime != nil {
		for _, kind := range s.kinds {
			s.localRuntime.SetManagedRuntimeRunning(kind, false)
		}
	}
}

func (s *managedRuntimeSupervisor) buildRuntime(kind string, reader *viper.Viper) (func(context.Context) error, func(), error) {
	if reader == nil {
		reader = viper.GetViper()
	}
	runtimeValues := llmutil.RuntimeValuesFromReader(reader)
	switch kind {
	case managedRuntimeTelegram:
		botToken := strings.TrimSpace(reader.GetString("telegram.bot_token"))
		if botToken == "" {
			return nil, nil, managedRuntimeConfigError{err: fmt.Errorf("missing telegram.bot_token (set via --telegram-bot-token or MISTER_MORPH_TELEGRAM_BOT_TOKEN)")}
		}
		deps, cleanup := buildManagedRuntimeDepsFromReader(s.logger(), reader)
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
		runOpts.TaskStore, err = newManagedRuntimeTaskStore(kind, runOpts.Server.MaxQueue)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		runtimeDeps := telegramruntime.Dependencies{
			CommonDependencies: deps,
			HandleModelCommand: func(text string) (string, bool, error) {
				return llmselect.ExecuteCommandText(runtimeValues, llmselect.ProcessStore(), text)
			},
		}
		return func(ctx context.Context) error {
			return telegramruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	case managedRuntimeSlack:
		botToken := strings.TrimSpace(reader.GetString("slack.bot_token"))
		if botToken == "" {
			return nil, nil, managedRuntimeConfigError{err: fmt.Errorf("missing slack.bot_token (set via --slack-bot-token or MISTER_MORPH_SLACK_BOT_TOKEN)")}
		}
		appToken := strings.TrimSpace(reader.GetString("slack.app_token"))
		if appToken == "" {
			return nil, nil, managedRuntimeConfigError{err: fmt.Errorf("missing slack.app_token (set via --slack-app-token or MISTER_MORPH_SLACK_APP_TOKEN)")}
		}
		deps, cleanup := buildManagedRuntimeDepsFromReader(s.logger(), reader)
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
		taskStore, err := newManagedRuntimeTaskStore(kind, runOpts.Server.MaxQueue)
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
		}
		return func(ctx context.Context) error {
			return slackruntime.Run(ctx, runtimeDeps, runOpts)
		}, cleanup, nil
	default:
		return nil, nil, fmt.Errorf("unsupported managed runtime %q", kind)
	}
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

func newManagedRuntimeTaskStore(kind string, maxItems int) (daemonruntime.TaskView, error) {
	switch kind {
	case managedRuntimeTelegram, managedRuntimeSlack:
		return daemonruntime.NewTaskViewForTarget(kind, maxItems)
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
	if !s.isCurrentGeneration(generation) {
		return
	}
	if s.localRuntime != nil {
		s.localRuntime.SetManagedRuntimeRunning(kind, false)
	}
	if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return
	}
	if s.onFatal != nil {
		s.onFatal(fmt.Errorf("managed runtime %s failed: %w", kind, err))
	}
}

func (s *managedRuntimeSupervisor) isCurrentGeneration(generation uint64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation == generation
}

func buildManagedRuntimeDepsFromReader(logger *slog.Logger, reader *viper.Viper) (depsutil.CommonDependencies, func()) {
	if logger == nil {
		logger = slog.Default()
	}
	if reader == nil {
		reader = viper.GetViper()
	}
	logOpts := logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(reader))
	baseRegistry, mcpHost := buildConsoleBaseRegistryFromReader(context.Background(), logger, reader)
	sharedGuard := buildConsoleGuardFromReader(logger, reader)
	deps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return logOpts
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			values := llmutil.RuntimeValuesFromReader(reader)
			if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
				return llmselect.ResolveMainRoute(values, llmselect.ProcessStore().Get())
			}
			return llmutil.ResolveRoute(values, purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return llmutil.BuildRouteClient(
				route,
				nil,
				llmutil.ClientFromConfigWithValues,
				func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
					return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
				},
				logger,
			)
		},
		RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		Guard: func(_ *slog.Logger) *guard.Guard {
			return sharedGuard
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
	}
}
