package telegramcmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
)

func NewCommand(d Dependencies) *cobra.Command {
	return newTelegramCmd(d)
}

func newTelegramCmd(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telegram",
		Short: "Run a Telegram bot that chats with the agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			token := strings.TrimSpace(configutil.FlagOrViperString(cmd, "telegram-bot-token", "telegram.bot_token"))
			if token == "" {
				return fmt.Errorf("missing telegram.bot_token (set via --telegram-bot-token or MISTER_MORPH_TELEGRAM_BOT_TOKEN)")
			}

			allowedIDsRaw := configutil.FlagOrViperStringArray(cmd, "telegram-allowed-chat-id", "telegram.allowed_chat_ids")
			allowedIDs := make([]int64, 0, len(allowedIDsRaw))
			parsedAllowedIDs, err := channelopts.ParseTelegramAllowedChatIDs(allowedIDsRaw)
			if err != nil {
				return err
			}
			allowedIDs = parsedAllowedIDs

			cfg := channelopts.TelegramConfigFromViper()
			hbCfg := channelopts.HeartbeatConfigFromViper()
			cronCfg := channelopts.CronConfigFromViper()
			runtimeToolsConfig := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			runOpts, err := channelopts.BuildTelegramRunOptions(cfg, channelopts.TelegramInput{
				BotToken:                      token,
				AllowedChatIDs:                allowedIDs,
				GroupTriggerMode:              strings.TrimSpace(configutil.FlagOrViperString(cmd, "telegram-group-trigger-mode", "telegram.group_trigger_mode")),
				AddressingConfidenceThreshold: configutil.FlagOrViperFloat64(cmd, "telegram-addressing-confidence-threshold", "telegram.addressing_confidence_threshold"),
				AddressingInterjectThreshold:  configutil.FlagOrViperFloat64(cmd, "telegram-addressing-interject-threshold", "telegram.addressing_interject_threshold"),
				PollTimeout:                   configutil.FlagOrViperDuration(cmd, "telegram-poll-timeout", "telegram.poll_timeout"),
				TaskTimeout:                   configutil.FlagOrViperDuration(cmd, "telegram-task-timeout", "telegram.task_timeout"),
				MaxConcurrency:                configutil.FlagOrViperInt(cmd, "telegram-max-concurrency", "telegram.max_concurrency"),
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			if err != nil {
				return err
			}
			deps := buildTelegramRuntimeDeps(d, runtimeToolsConfig)

			awarenessDeps, awarenessOpts := buildAwarenessRuntime(d, cfg, hbCfg, cronCfg, token, runOpts.AllowedChatIDs, runOpts.TaskTimeout, runtimeToolsConfig, runOpts.InspectPrompt, runOpts.InspectRequest)
			return runTelegramWithOptionalAwareness(cmd.Context(), deps, runOpts, awarenessDeps, awarenessOpts, (hbCfg.Enabled && hbCfg.Interval > 0) || cronCfg.Enabled)
		},
	}

	cmd.Flags().String("telegram-bot-token", "", "Telegram bot token.")
	cmd.Flags().StringArray("telegram-allowed-chat-id", nil, "Allowed chat id(s). If empty, allows all.")
	cmd.Flags().String("telegram-group-trigger-mode", "smart", "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("telegram-addressing-confidence-threshold", 0.6, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("telegram-addressing-interject-threshold", 0.6, "Minimum interject (0-1) allowed to accept an addressing LLM decision.")
	cmd.Flags().Duration("telegram-poll-timeout", 30*time.Second, "Long polling timeout for getUpdates.")
	cmd.Flags().Duration("telegram-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("telegram-max-concurrency", 3, "Max number of chats processed concurrently.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_telegram_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_telegram_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildAwarenessRuntime(
	d Dependencies,
	telegramCfg channelopts.TelegramConfig,
	hbCfg channelopts.HeartbeatConfig,
	cronCfg channelopts.CronConfig,
	telegramToken string,
	allowedChatIDs []int64,
	taskTimeout time.Duration,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
	inspectPrompt bool,
	inspectRequest bool,
) (awarenessruntime.Dependencies, awarenessruntime.RunOptions) {
	awarenessDeps := awarenessruntime.Dependencies{
		Logger:             d.Logger,
		LogOptions:         d.LogOptions,
		ResolveLLMRoute:    d.ResolveLLMRoute,
		CreateLLMClient:    d.CreateLLMClient,
		Registry:           d.Registry,
		RuntimeToolsConfig: runtimeToolsConfig,
		Guard:              d.Guard,
		PromptSpec:         d.PromptSpec,
	}
	awarenessOpts := awarenessruntime.RunOptions{
		Interval:                hbCfg.Interval,
		TaskTimeout:             taskTimeout,
		RequestTimeout:          telegramCfg.RequestTimeout,
		AgentLimits:             telegramCfg.AgentLimits,
		EngineToolsConfig:       telegramCfg.EngineToolsConfig,
		Source:                  "telegram",
		ChecklistPath:           statepaths.HeartbeatChecklistPath(),
		DisableHeartbeat:        !hbCfg.Enabled || hbCfg.Interval <= 0,
		MemoryEnabled:           telegramCfg.MemoryEnabled,
		MemoryShortTermDays:     telegramCfg.MemoryShortTermDays,
		MemoryInjectionEnabled:  telegramCfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: telegramCfg.MemoryInjectionMaxItems,
		InspectPrompt:           inspectPrompt,
		InspectRequest:          inspectRequest,
		// Keep heartbeat alerts in logs only; avoid pushing failure alerts into chats.
		Notifier:    nil,
		CronEnabled: cronCfg.Enabled,
		CronPath:    statepaths.CronPath(),
	}
	return awarenessDeps, awarenessOpts
}

func buildTelegramRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
) telegramruntime.Dependencies {
	return telegramruntime.Dependencies{
		CommonDependencies: depsutil.CommonDependencies{
			Logger:             d.Logger,
			LogOptions:         d.LogOptions,
			ResolveLLMRoute:    d.ResolveLLMRoute,
			CreateLLMClient:    d.CreateLLMClient,
			Registry:           d.Registry,
			RuntimeToolsConfig: runtimeToolsConfig,
			Guard:              d.Guard,
			PromptSpec:         d.PromptSpec,
		},
		HandleModelCommand: d.HandleModelCommand,
		HandleSkillCommand: d.HandleSkillCommand,
	}
}

func runTelegramWithOptionalAwareness(
	ctx context.Context,
	telegramDeps telegramruntime.Dependencies,
	telegramOpts telegramruntime.RunOptions,
	awarenessDeps awarenessruntime.Dependencies,
	awarenessOpts awarenessruntime.RunOptions,
	awarenessEnabled bool,
) error {
	if !awarenessEnabled {
		return telegramruntime.Run(ctx, telegramDeps, telegramOpts)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pokeRequests := make(chan awarenessruntime.PokeRequest)
	awarenessOpts.PokeRequests = pokeRequests
	telegramOpts.Server.Poke = func(ctx context.Context, input daemonruntime.PokeInput) error {
		return awarenessruntime.Trigger(ctx, pokeRequests, input)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- telegramruntime.Run(runCtx, telegramDeps, telegramOpts)
	}()
	go func() {
		errCh <- awarenessruntime.Run(runCtx, awarenessDeps, awarenessOpts)
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
			firstErr = err
		}
		cancel()
	}
	return firstErr
}

func newTelegramAwarenessNotifier(token string, chatIDs []int64) awarenessruntime.Notifier {
	filtered := make([]int64, 0, len(chatIDs))
	for _, id := range chatIDs {
		if id != 0 {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return awarenessruntime.NotifyFunc(func(ctx context.Context, text string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		for _, chatID := range filtered {
			if err := telegramruntime.SendMessageHTML(ctx, client, "https://api.telegram.org", token, chatID, text, true); err != nil {
				return err
			}
		}
		return nil
	})
}
