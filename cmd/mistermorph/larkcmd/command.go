package larkcmd

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewCommand(d Dependencies) *cobra.Command {
	return newLarkCmd(d)
}

func newLarkCmd(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lark",
		Short: "Run a Lark bot with WebSocket ingress",
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
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			deps := buildLarkRuntimeDeps(d, runtimeToolsConfig, viper.GetViper())
			return larkruntime.Run(cmd.Context(), deps, runOpts)
		},
	}

	cmd.Flags().String("lark-app-id", "", "Lark app id.")
	cmd.Flags().String("lark-app-secret", "", "Lark app secret.")
	cmd.Flags().StringArray("lark-allowed-chat-id", nil, "Allowed Lark chat id(s). If empty, allows all chats.")
	cmd.Flags().String("lark-group-trigger-mode", configdefaults.DefaultGroupTriggerMode, "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("lark-addressing-confidence-threshold", configdefaults.DefaultAddressingThreshold, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("lark-addressing-interject-threshold", configdefaults.DefaultAddressingThreshold, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("lark-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("lark-max-concurrency", configdefaults.DefaultChannelMaxConcurrency, "Max number of Lark conversations processed concurrently.")
	cmd.Flags().String("lark-base-url", configdefaults.DefaultLarkBaseURL, "Lark Open API base URL.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_lark_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_lark_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildLarkRuntimeDeps(
	d Dependencies,
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig,
	reader *viper.Viper,
) larkruntime.Dependencies {
	paths := runtimepaths.FromReader(reader)
	common := d.Dependencies
	common.RuntimeToolsConfig = runtimeToolsConfig
	common.RuntimePaths = paths
	common.DefaultWorkspaceDir = strings.TrimSpace(reader.GetString("workspace_dir"))
	settingsOwner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{Reader: reader})
	common.AgentSettingsOwner = settingsOwner
	common.RuntimeConfigSource = settingsOwner
	common.AgentSettingsReader = settingsOwner.CurrentReader()
	common.TaskPersistenceTargets = append([]string(nil), reader.GetStringSlice("tasks.persistence_targets")...)
	common.TaskRotateMaxBytes = reader.GetInt64("tasks.rotate_max_bytes")
	return larkruntime.Dependencies{
		CommonDependencies: common,
		HandleModelCommand: d.HandleModelCommand,
		HandleSkillCommand: d.HandleSkillCommand,
	}
}
