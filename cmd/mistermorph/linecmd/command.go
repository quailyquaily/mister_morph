package linecmd

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	lineruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/line"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
)

func NewCommand(d Dependencies) *cobra.Command {
	return newLineCmd(d)
}

func newLineCmd(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "line",
		Short: "Run a LINE bot with webhook ingress",
		RunE: func(cmd *cobra.Command, args []string) error {
			channelAccessToken := strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-channel-access-token", "line.channel_access_token"))
			if channelAccessToken == "" {
				return fmt.Errorf("missing line.channel_access_token (set via --line-channel-access-token or MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN)")
			}
			channelSecret := strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-channel-secret", "line.channel_secret"))
			if channelSecret == "" {
				return fmt.Errorf("missing line.channel_secret (set via --line-channel-secret or MISTER_MORPH_LINE_CHANNEL_SECRET)")
			}

			cfg := channelopts.LineConfigFromViper()
			runtimeToolsConfig := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			runOpts := channelopts.BuildLineRunOptions(cfg, channelopts.LineInput{
				ChannelAccessToken:            channelAccessToken,
				ChannelSecret:                 channelSecret,
				AllowedGroupIDs:               configutil.FlagOrViperStringArray(cmd, "line-allowed-group-id", "line.allowed_group_ids"),
				GroupTriggerMode:              strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-group-trigger-mode", "line.group_trigger_mode")),
				AddressingConfidenceThreshold: configutil.FlagOrViperFloat64(cmd, "line-addressing-confidence-threshold", "line.addressing_confidence_threshold"),
				AddressingInterjectThreshold:  configutil.FlagOrViperFloat64(cmd, "line-addressing-interject-threshold", "line.addressing_interject_threshold"),
				TaskTimeout:                   configutil.FlagOrViperDuration(cmd, "line-task-timeout", "line.task_timeout"),
				MaxConcurrency:                configutil.FlagOrViperInt(cmd, "line-max-concurrency", "line.max_concurrency"),
				BaseURL:                       strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-base-url", "line.base_url")),
				WebhookListen:                 strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-webhook-listen", "line.webhook_listen")),
				WebhookPath:                   strings.TrimSpace(configutil.FlagOrViperString(cmd, "line-webhook-path", "line.webhook_path")),
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			deps := buildLineRuntimeDeps(d, runtimeToolsConfig)
			return lineruntime.Run(cmd.Context(), deps, runOpts)
		},
	}

	cmd.Flags().String("line-channel-access-token", "", "LINE channel access token.")
	cmd.Flags().String("line-channel-secret", "", "LINE channel secret for webhook signature verification.")
	cmd.Flags().StringArray("line-allowed-group-id", nil, "Allowed LINE group id(s). If empty, allows all groups.")
	cmd.Flags().String("line-group-trigger-mode", "smart", "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("line-addressing-confidence-threshold", 0.6, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("line-addressing-interject-threshold", 0.6, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("line-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("line-max-concurrency", 3, "Max number of LINE conversations processed concurrently.")
	cmd.Flags().String("line-base-url", "https://api.line.me", "LINE API base URL.")
	cmd.Flags().String("line-webhook-listen", "127.0.0.1:18080", "Listen address for LINE webhook server.")
	cmd.Flags().String("line-webhook-path", "/line/webhook", "HTTP path for LINE webhook callback.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_line_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_line_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildLineRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
) lineruntime.Dependencies {
	return lineruntime.Dependencies{
		CommonDependencies: depsutil.CommonDependencies{
			Logger:             d.Logger,
			LogOptions:         d.LogOptions,
			ResolveLLMRoute:    d.ResolveLLMRoute,
			CreateLLMClient:    d.CreateLLMClient,
			Registry:           d.Registry,
			RuntimeToolsConfig: runtimeToolsConfig,
			Guard:              d.Guard,
			PromptSpec:         d.PromptSpec,
			PromptAugment:      d.PromptAugment,
		},
		HandleModelCommand: d.HandleModelCommand,
	}
}
