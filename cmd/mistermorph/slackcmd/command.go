package slackcmd

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
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/slackclient"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
)

func NewCommand(d Dependencies) *cobra.Command {
	return newSlackCmd(d)
}

func newSlackCmd(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "Run a Slack bot with Socket Mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			botToken := strings.TrimSpace(configutil.FlagOrViperString(cmd, "slack-bot-token", "slack.bot_token"))
			if botToken == "" {
				return fmt.Errorf("missing slack.bot_token (set via --slack-bot-token or MISTER_MORPH_SLACK_BOT_TOKEN)")
			}
			appToken := strings.TrimSpace(configutil.FlagOrViperString(cmd, "slack-app-token", "slack.app_token"))
			if appToken == "" {
				return fmt.Errorf("missing slack.app_token (set via --slack-app-token or MISTER_MORPH_SLACK_APP_TOKEN)")
			}

			cfg := channelopts.SlackConfigFromViper()
			hbCfg := channelopts.HeartbeatConfigFromViper()
			runtimeToolsConfig := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			runOpts := channelopts.BuildSlackRunOptions(cfg, channelopts.SlackInput{
				BotToken:                      botToken,
				AppToken:                      appToken,
				AllowedTeamIDs:                configutil.FlagOrViperStringArray(cmd, "slack-allowed-team-id", "slack.allowed_team_ids"),
				AllowedChannelIDs:             configutil.FlagOrViperStringArray(cmd, "slack-allowed-channel-id", "slack.allowed_channel_ids"),
				GroupTriggerMode:              strings.TrimSpace(configutil.FlagOrViperString(cmd, "slack-group-trigger-mode", "slack.group_trigger_mode")),
				AddressingConfidenceThreshold: configutil.FlagOrViperFloat64(cmd, "slack-addressing-confidence-threshold", "slack.addressing_confidence_threshold"),
				AddressingInterjectThreshold:  configutil.FlagOrViperFloat64(cmd, "slack-addressing-interject-threshold", "slack.addressing_interject_threshold"),
				TaskTimeout:                   configutil.FlagOrViperDuration(cmd, "slack-task-timeout", "slack.task_timeout"),
				MaxConcurrency:                configutil.FlagOrViperInt(cmd, "slack-max-concurrency", "slack.max_concurrency"),
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			deps := buildSlackRuntimeDeps(d, runtimeToolsConfig)
			hbDeps, hbOpts := buildHeartbeatRuntime(d, cfg, hbCfg, botToken, runOpts.AllowedChannelIDs, runOpts.TaskTimeout, runOpts.BaseURL, runtimeToolsConfig, runOpts.InspectPrompt, runOpts.InspectRequest)
			return runSlackWithOptionalHeartbeat(cmd.Context(), deps, runOpts, hbDeps, hbOpts, hbCfg.Enabled)
		},
	}

	cmd.Flags().String("slack-bot-token", "", "Slack bot token (xoxb-...).")
	cmd.Flags().String("slack-app-token", "", "Slack app-level token for Socket Mode (xapp-...).")
	cmd.Flags().StringArray("slack-allowed-team-id", nil, "Allowed Slack team id(s). If empty, defaults to the bot's home team.")
	cmd.Flags().StringArray("slack-allowed-channel-id", nil, "Allowed Slack channel id(s). If empty, allows all channels in allowed teams.")
	cmd.Flags().String("slack-group-trigger-mode", "smart", "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("slack-addressing-confidence-threshold", 0.6, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("slack-addressing-interject-threshold", 0.6, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("slack-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("slack-max-concurrency", 3, "Max number of Slack conversations processed concurrently.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_slack_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_slack_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildHeartbeatRuntime(
	d Dependencies,
	slackCfg channelopts.SlackConfig,
	hbCfg channelopts.HeartbeatConfig,
	botToken string,
	allowedChannelIDs []string,
	taskTimeout time.Duration,
	baseURL string,
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
		RequestTimeout:          slackCfg.RequestTimeout,
		AgentLimits:             slackCfg.AgentLimits,
		EngineToolsConfig:       slackCfg.EngineToolsConfig,
		Source:                  "slack",
		ChecklistPath:           statepaths.HeartbeatChecklistPath(),
		MemoryEnabled:           slackCfg.MemoryEnabled,
		MemoryShortTermDays:     slackCfg.MemoryShortTermDays,
		MemoryInjectionEnabled:  slackCfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: slackCfg.MemoryInjectionMaxItems,
		InspectPrompt:           inspectPrompt,
		InspectRequest:          inspectRequest,
		Notifier:                newSlackHeartbeatNotifier(botToken, baseURL, allowedChannelIDs),
	}
	return hbDeps, hbOpts
}

func buildSlackRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
) slackruntime.Dependencies {
	return slackruntime.Dependencies{
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

func runSlackWithOptionalHeartbeat(
	ctx context.Context,
	slackDeps slackruntime.Dependencies,
	slackOpts slackruntime.RunOptions,
	hbDeps heartbeatruntime.Dependencies,
	hbOpts heartbeatruntime.RunOptions,
	hbEnabled bool,
) error {
	if !hbEnabled || hbOpts.Interval <= 0 {
		return slackruntime.Run(ctx, slackDeps, slackOpts)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pokeRequests := make(chan heartbeatruntime.PokeRequest)
	hbOpts.PokeRequests = pokeRequests
	slackOpts.Server.Poke = func(ctx context.Context, input daemonruntime.PokeInput) error {
		return heartbeatruntime.Trigger(ctx, pokeRequests, input)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- slackruntime.Run(runCtx, slackDeps, slackOpts)
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

func newSlackHeartbeatNotifier(botToken, baseURL string, channelIDs []string) heartbeatruntime.Notifier {
	filtered := make([]string, 0, len(channelIDs))
	seen := make(map[string]bool, len(channelIDs))
	for _, raw := range channelIDs {
		channelID := strings.TrimSpace(raw)
		if channelID == "" || seen[channelID] {
			continue
		}
		seen[channelID] = true
		filtered = append(filtered, channelID)
	}
	if len(filtered) == 0 {
		return nil
	}
	client := slackclient.New(&http.Client{Timeout: 30 * time.Second}, baseURL, botToken)
	return heartbeatruntime.NotifyFunc(func(ctx context.Context, text string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		for _, channelID := range filtered {
			if err := client.PostMessage(ctx, channelID, text, ""); err != nil {
				return err
			}
		}
		return nil
	})
}
