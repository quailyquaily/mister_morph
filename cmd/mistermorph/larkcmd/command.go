package larkcmd

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
)

func NewCommand(d Dependencies) *cobra.Command {
	return newLarkCmd(d)
}

func newLarkCmd(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lark",
		Short: "Run a Lark bot with webhook ingress",
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-app-id", "lark.app_id"))
			if appID == "" {
				return fmt.Errorf("missing lark.app_id (set via --lark-app-id or MISTER_MORPH_LARK_APP_ID)")
			}
			appSecret := strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-app-secret", "lark.app_secret"))
			if appSecret == "" {
				return fmt.Errorf("missing lark.app_secret (set via --lark-app-secret or MISTER_MORPH_LARK_APP_SECRET)")
			}

			cfg := channelopts.LarkConfigFromViper()
			runtimeToolsConfig := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			runOpts := channelopts.BuildLarkRunOptions(cfg, channelopts.LarkInput{
				AppID:                         appID,
				AppSecret:                     appSecret,
				AllowedChatIDs:                configutil.FlagOrViperStringArray(cmd, "lark-allowed-chat-id", "lark.allowed_chat_ids"),
				GroupTriggerMode:              strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-group-trigger-mode", "lark.group_trigger_mode")),
				AddressingConfidenceThreshold: configutil.FlagOrViperFloat64(cmd, "lark-addressing-confidence-threshold", "lark.addressing_confidence_threshold"),
				AddressingInterjectThreshold:  configutil.FlagOrViperFloat64(cmd, "lark-addressing-interject-threshold", "lark.addressing_interject_threshold"),
				TaskTimeout:                   configutil.FlagOrViperDuration(cmd, "lark-task-timeout", "lark.task_timeout"),
				MaxConcurrency:                configutil.FlagOrViperInt(cmd, "lark-max-concurrency", "lark.max_concurrency"),
				BaseURL:                       strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-base-url", "lark.base_url")),
				WebhookListen:                 strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-webhook-listen", "lark.webhook_listen")),
				WebhookPath:                   strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-webhook-path", "lark.webhook_path")),
				VerificationToken:             strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-verification-token", "lark.verification_token")),
				EncryptKey:                    strings.TrimSpace(configutil.FlagOrViperString(cmd, "lark-encrypt-key", "lark.encrypt_key")),
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			deps := buildLarkRuntimeDeps(d, runtimeToolsConfig)
			return larkruntime.Run(cmd.Context(), deps, runOpts)
		},
	}

	cmd.Flags().String("lark-app-id", "", "Lark app id.")
	cmd.Flags().String("lark-app-secret", "", "Lark app secret.")
	cmd.Flags().StringArray("lark-allowed-chat-id", nil, "Allowed Lark chat id(s). If empty, allows all chats.")
	cmd.Flags().String("lark-group-trigger-mode", "smart", "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("lark-addressing-confidence-threshold", 0.6, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("lark-addressing-interject-threshold", 0.6, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("lark-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("lark-max-concurrency", 3, "Max number of Lark conversations processed concurrently.")
	cmd.Flags().String("lark-base-url", "https://open.feishu.cn/open-apis", "Lark Open API base URL.")
	cmd.Flags().String("lark-webhook-listen", "127.0.0.1:18081", "Listen address for Lark webhook server.")
	cmd.Flags().String("lark-webhook-path", "/lark/webhook", "HTTP path for Lark webhook callback.")
	cmd.Flags().String("lark-verification-token", "", "Lark event subscription verification token.")
	cmd.Flags().String("lark-encrypt-key", "", "Lark event subscription encrypt key.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_lark_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_lark_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildLarkRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
) larkruntime.Dependencies {
	return larkruntime.Dependencies{
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
