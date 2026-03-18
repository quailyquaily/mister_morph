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
	mu           sync.Mutex
	kinds        []string
	localRuntime *consoleLocalRuntime
	parentCtx    context.Context
	cancel       context.CancelFunc
	onFatal      func(error)
	generation   uint64
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

func newManagedRuntimeSupervisor(localRuntime *consoleLocalRuntime, kinds []string) *managedRuntimeSupervisor {
	return &managedRuntimeSupervisor{
		kinds:        append([]string(nil), kinds...),
		localRuntime: localRuntime,
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
	return s.startLocked()
}

func (s *managedRuntimeSupervisor) Restart() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parentCtx == nil {
		return nil
	}
	s.stopLocked()
	return s.startLocked()
}

func (s *managedRuntimeSupervisor) UpdateKinds(kinds []string) error {
	if s == nil {
		return nil
	}
	normalized, err := normalizeManagedRuntimeKinds(kinds)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.kinds = append([]string(nil), normalized...)
	if s.parentCtx == nil {
		return nil
	}
	return s.startLocked()
}

func (s *managedRuntimeSupervisor) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.parentCtx = nil
	s.onFatal = nil
}

func (s *managedRuntimeSupervisor) startLocked() error {
	if len(s.kinds) == 0 {
		return nil
	}
	if s.parentCtx == nil {
		return fmt.Errorf("managed runtime supervisor parent context is not set")
	}
	runCtx, cancel := context.WithCancel(s.parentCtx)
	s.cancel = cancel
	s.generation++
	generation := s.generation
	for _, kind := range s.kinds {
		run, cleanup, err := s.buildRuntimeLocked(kind)
		if err != nil {
			cancel()
			s.cancel = nil
			for _, item := range s.kinds {
				s.localRuntime.SetManagedRuntimeRunning(item, false)
			}
			return err
		}
		s.localRuntime.SetManagedRuntimeRunning(kind, true)
		go s.runManagedRuntime(runCtx, generation, kind, run, cleanup)
	}
	return nil
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

func (s *managedRuntimeSupervisor) buildRuntimeLocked(kind string) (func(context.Context) error, func(), error) {
	deps, cleanup := buildManagedRuntimeDeps(s.localRuntime.logger)
	switch kind {
	case managedRuntimeTelegram:
		cfg := channelopts.TelegramConfigFromViper()
		runOpts, err := channelopts.BuildTelegramRunOptions(cfg, channelopts.TelegramInput{
			BotToken: strings.TrimSpace(viper.GetString("telegram.bot_token")),
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
		return func(ctx context.Context) error {
			return telegramruntime.Run(ctx, deps, runOpts)
		}, cleanup, nil
	case managedRuntimeSlack:
		cfg := channelopts.SlackConfigFromViper()
		runOpts := channelopts.BuildSlackRunOptions(cfg, channelopts.SlackInput{
			BotToken: strings.TrimSpace(viper.GetString("slack.bot_token")),
			AppToken: strings.TrimSpace(viper.GetString("slack.app_token")),
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
		return func(ctx context.Context) error {
			return slackruntime.Run(ctx, deps, runOpts)
		}, cleanup, nil
	default:
		cleanup()
		return nil, nil, fmt.Errorf("unsupported managed runtime %q", kind)
	}
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

func buildManagedRuntimeDeps(logger *slog.Logger) (depsutil.CommonDependencies, func()) {
	if logger == nil {
		logger = slog.Default()
	}
	logOpts := logutil.LogOptionsFromViper()
	baseRegistry, mcpHost := buildConsoleBaseRegistry(context.Background(), logger)
	sharedGuard := buildConsoleGuardFromViper(logger)
	deps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return logOpts
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRoute(llmutil.RuntimeValuesFromViper(), purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			base, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
			if err != nil {
				return nil, err
			}
			return llmstats.WrapRuntimeClient(base, route.ClientConfig.Provider, route.ClientConfig.Endpoint, route.ClientConfig.Model, logger), nil
		},
		RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromViper(),
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		Guard: func(_ *slog.Logger) *guard.Guard {
			return sharedGuard
		},
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := skillsutil.SkillsConfigFromViper()
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
