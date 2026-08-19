package slackcmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/slackclient"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewCommand(d Dependencies) *cobra.Command {
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
			cronCfg := channelopts.CronConfigFromViper()
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
			deps := buildSlackRuntimeDeps(d, runtimeToolsConfig, viper.GetViper())
			awarenessDeps, awarenessOpts := buildAwarenessRuntime(d, cfg, hbCfg, cronCfg, botToken, runOpts.AllowedChannelIDs, runOpts.TaskTimeout, runOpts.BaseURL, runtimeToolsConfig, runOpts.InspectPrompt, runOpts.InspectRequest, deps.RuntimePaths, chatinfo.NewFetcher(chatinfo.FetcherOptionsFromReader(viper.GetViper())))
			awarenessDeps.DefaultWorkspaceDir = deps.DefaultWorkspaceDir
			return runSlackWithOptionalAwareness(cmd.Context(), deps, runOpts, awarenessDeps, awarenessOpts, (hbCfg.Enabled && hbCfg.Interval > 0) || cronCfg.Enabled)
		},
	}

	cmd.Flags().String("slack-bot-token", "", "Slack bot token (xoxb-...).")
	cmd.Flags().String("slack-app-token", "", "Slack app-level token for Socket Mode (xapp-...).")
	cmd.Flags().StringArray("slack-allowed-team-id", nil, "Allowed Slack team id(s). If empty, defaults to the bot's home team.")
	cmd.Flags().StringArray("slack-allowed-channel-id", nil, "Allowed Slack channel id(s). If empty, allows all channels in allowed teams.")
	cmd.Flags().String("slack-group-trigger-mode", configdefaults.DefaultGroupTriggerMode, "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("slack-addressing-confidence-threshold", configdefaults.DefaultAddressingThreshold, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("slack-addressing-interject-threshold", configdefaults.DefaultAddressingThreshold, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("slack-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("slack-max-concurrency", configdefaults.DefaultChannelMaxConcurrency, "Max number of Slack conversations processed concurrently.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_slack_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_slack_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildAwarenessRuntime(
	d Dependencies,
	slackCfg channelopts.SlackConfig,
	hbCfg channelopts.HeartbeatConfig,
	cronCfg channelopts.CronConfig,
	botToken string,
	allowedChannelIDs []string,
	taskTimeout time.Duration,
	baseURL string,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
	inspectPrompt bool,
	inspectRequest bool,
	paths runtimepaths.Paths,
	chatInfoRefresher chatinfo.Refresher,
) (awarenessruntime.Dependencies, awarenessruntime.RunOptions) {
	awarenessDeps := d.Dependencies
	awarenessDeps.RuntimeToolsConfig = runtimeToolsConfig
	awarenessDeps.RuntimePaths = paths
	awarenessOpts := awarenessruntime.RunOptions{
		Interval:          hbCfg.Interval,
		TaskTimeout:       taskTimeout,
		RequestTimeout:    slackCfg.RequestTimeout,
		AgentLimits:       slackCfg.AgentLimits,
		EngineToolsConfig: slackCfg.EngineToolsConfig,
		Source:            "slack",
		ChecklistPath:     paths.HeartbeatPath,
		DisableHeartbeat:  !hbCfg.Enabled || hbCfg.Interval <= 0,
		InspectPrompt:     inspectPrompt,
		InspectRequest:    inspectRequest,
		Notifier:          newSlackAwarenessNotifier(botToken, baseURL, allowedChannelIDs),
		CronEnabled:       cronCfg.Enabled,
		CronPath:          paths.CronPath,
		ChatInfoRefresher: chatInfoRefresher,
	}
	return awarenessDeps, awarenessOpts
}

func buildSlackRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
	reader *viper.Viper,
) slackruntime.Dependencies {
	return slackruntime.Dependencies{
		CommonDependencies: depsutil.ApplyRuntimeConfig(d.Dependencies, runtimeToolsConfig, reader),
		HandleModelCommand: d.HandleModelCommand,
		HandleSkillCommand: d.HandleSkillCommand,
	}
}

func runSlackWithOptionalAwareness(
	ctx context.Context,
	slackDeps slackruntime.Dependencies,
	slackOpts slackruntime.RunOptions,
	awarenessDeps awarenessruntime.Dependencies,
	awarenessOpts awarenessruntime.RunOptions,
	awarenessEnabled bool,
) error {
	if !awarenessEnabled {
		return slackruntime.Run(ctx, slackDeps, slackOpts)
	}
	if slackOpts.TaskStore == nil {
		taskStore, err := daemonruntime.NewTaskViewForTarget("slack", slackOpts.Server.MaxQueue, daemonruntime.TaskViewConfig{
			PersistenceTargets: slackDeps.TaskPersistenceTargets,
			TasksDir:           slackDeps.RuntimePaths.TasksDir,
			JournalDir:         slackDeps.RuntimePaths.JournalDir,
			RotateMaxBytes:     slackDeps.TaskRotateMaxBytes,
		})
		if err != nil {
			return err
		}
		slackOpts.TaskStore = taskStore
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attachSlackAwarenessTriggers(&slackOpts, &awarenessOpts)

	errCh := make(chan error, 2)
	go func() {
		errCh <- slackruntime.Run(runCtx, slackDeps, slackOpts)
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

func attachSlackAwarenessTriggers(slackOpts *slackruntime.RunOptions, awarenessOpts *awarenessruntime.RunOptions) {
	if slackOpts == nil || awarenessOpts == nil {
		return
	}
	awarenessOpts.TaskStore = slackOpts.TaskStore
	pokeRequests := make(chan awarenessruntime.PokeRequest)
	awarenessOpts.PokeRequests = pokeRequests
	slackOpts.Server.Poke = func(ctx context.Context, input awarenessdomain.PokeInput) error {
		return awarenessruntime.Trigger(ctx, pokeRequests, input)
	}
	if awarenessOpts.CronEnabled {
		cronRequests := make(chan awarenessruntime.CronRequest)
		awarenessOpts.CronRequests = cronRequests
		slackOpts.Server.CronRun = func(ctx context.Context, task cronstore.Task) error {
			return awarenessruntime.TriggerCron(ctx, cronRequests, task)
		}
	}
}

func newSlackAwarenessNotifier(botToken, baseURL string, channelIDs []string) awarenessruntime.Notifier {
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
	return awarenessruntime.NotifyFunc(func(ctx context.Context, text string) error {
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
