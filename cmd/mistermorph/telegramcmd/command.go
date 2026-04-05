package telegramcmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	heartbeatruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
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

			hbDeps, hbOpts := buildHeartbeatRuntime(d, cfg, hbCfg, token, runOpts.AllowedChatIDs, runOpts.TaskTimeout, runtimeToolsConfig, runOpts.InspectPrompt, runOpts.InspectRequest)
			return runTelegramWithOptionalHeartbeat(cmd.Context(), deps, runOpts, hbDeps, hbOpts, hbCfg.Enabled)
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

func buildHeartbeatRuntime(
	d Dependencies,
	telegramCfg channelopts.TelegramConfig,
	hbCfg channelopts.HeartbeatConfig,
	telegramToken string,
	allowedChatIDs []int64,
	taskTimeout time.Duration,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
	inspectPrompt bool,
	inspectRequest bool,
) (heartbeatruntime.Dependencies, heartbeatruntime.RunOptions) {
	hbDeps := heartbeatruntime.Dependencies{
		Logger:             d.Logger,
		LogOptions:         d.LogOptions,
		ResolveLLMRoute:    d.ResolveLLMRoute,
		CreateLLMClient:    d.CreateLLMClient,
		Registry:           d.Registry,
		RuntimeToolsConfig: runtimeToolsConfig,
		Guard:              d.Guard,
		PromptSpec:         d.PromptSpec,
		BuildHeartbeatTask: d.BuildHeartbeatTask,
		BuildHeartbeatMeta: d.BuildHeartbeatMeta,
	}
	hbOpts := heartbeatruntime.RunOptions{
		Interval:                hbCfg.Interval,
		TaskTimeout:             taskTimeout,
		RequestTimeout:          telegramCfg.RequestTimeout,
		AgentLimits:             telegramCfg.AgentLimits,
		EngineToolsConfig:       telegramCfg.EngineToolsConfig,
		Source:                  "telegram",
		ChecklistPath:           statepaths.HeartbeatChecklistPath(),
		MemoryEnabled:           telegramCfg.MemoryEnabled,
		MemoryShortTermDays:     telegramCfg.MemoryShortTermDays,
		MemoryInjectionEnabled:  telegramCfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: telegramCfg.MemoryInjectionMaxItems,
		InspectPrompt:           inspectPrompt,
		InspectRequest:          inspectRequest,
		// Keep heartbeat alerts in logs only; avoid pushing failure alerts into chats.
		Notifier: nil,
	}
	return hbDeps, hbOpts
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
	}
}

func runTelegramWithOptionalHeartbeat(
	ctx context.Context,
	telegramDeps telegramruntime.Dependencies,
	telegramOpts telegramruntime.RunOptions,
	hbDeps heartbeatruntime.Dependencies,
	hbOpts heartbeatruntime.RunOptions,
	hbEnabled bool,
) error {
	if !hbEnabled || hbOpts.Interval <= 0 {
		return telegramruntime.Run(ctx, telegramDeps, telegramOpts)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pokeRequests := make(chan heartbeatruntime.PokeRequest)
	hbOpts.PokeRequests = pokeRequests
	telegramOpts.Server.Poke = func(ctx context.Context, input daemonruntime.PokeInput) error {
		return heartbeatruntime.Trigger(ctx, pokeRequests, input)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- telegramruntime.Run(runCtx, telegramDeps, telegramOpts)
	}()
	go func() {
		errCh <- heartbeatruntime.Run(runCtx, hbDeps, hbOpts)
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

func newTelegramHeartbeatNotifier(token string, chatIDs []int64) heartbeatruntime.Notifier {
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
	return heartbeatruntime.NotifyFunc(func(ctx context.Context, text string) error {
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
