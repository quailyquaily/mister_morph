package mixincmd

import (
	"fmt"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	mixinruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/mixin"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewCommand(d Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mixin",
		Short: "Run a Mixin Messenger bot",
		RunE: func(cmd *cobra.Command, args []string) error {
			keystoreFile := pathutil.ResolveConfigRelativePath(
				configutil.FlagOrViperString(cmd, "mixin-keystore-file", "mixin.keystore_file"),
				viper.GetString("config"),
			)
			if keystoreFile == "" {
				return fmt.Errorf("missing mixin.keystore_file (set via --mixin-keystore-file or MISTER_MORPH_MIXIN_KEYSTORE_FILE)")
			}

			cfg := channelopts.MixinConfigFromViper()
			runtimeToolsConfig := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			runOpts := channelopts.BuildMixinRunOptions(cfg, channelopts.MixinInput{
				KeystoreFile:                  keystoreFile,
				AllowedConversationIDs:        configutil.FlagOrViperStringArray(cmd, "mixin-allowed-conversation-id", "mixin.allowed_conversation_ids"),
				GroupTriggerMode:              configutil.FlagOrViperString(cmd, "mixin-group-trigger-mode", "mixin.group_trigger_mode"),
				AddressingConfidenceThreshold: configutil.FlagOrViperFloat64(cmd, "mixin-addressing-confidence-threshold", "mixin.addressing_confidence_threshold"),
				AddressingInterjectThreshold:  configutil.FlagOrViperFloat64(cmd, "mixin-addressing-interject-threshold", "mixin.addressing_interject_threshold"),
				TaskTimeout:                   configutil.FlagOrViperDuration(cmd, "mixin-task-timeout", "mixin.task_timeout"),
				MaxConcurrency:                configutil.FlagOrViperInt(cmd, "mixin-max-concurrency", "mixin.max_concurrency"),
				InspectPrompt:                 configutil.FlagOrViperBool(cmd, "inspect-prompt", ""),
				InspectRequest:                configutil.FlagOrViperBool(cmd, "inspect-request", ""),
			})
			deps := buildMixinRuntimeDeps(d, runtimeToolsConfig, viper.GetViper())
			return mixinruntime.Run(cmd.Context(), deps, runOpts)
		},
	}

	cmd.Flags().String("mixin-keystore-file", "", "Mixin Messenger Ed25519 keystore file.")
	cmd.Flags().StringArray("mixin-allowed-conversation-id", nil, "Allowed Mixin conversation UUID(s). If empty, allows all conversations.")
	cmd.Flags().String("mixin-group-trigger-mode", "talkative", "Group trigger mode: strict|smart|talkative.")
	cmd.Flags().Float64("mixin-addressing-confidence-threshold", configdefaults.DefaultAddressingThreshold, "Minimum confidence (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Float64("mixin-addressing-interject-threshold", configdefaults.DefaultAddressingThreshold, "Minimum interject (0-1) required to accept an addressing LLM decision.")
	cmd.Flags().Duration("mixin-task-timeout", 0, "Per-message agent timeout (0 uses --timeout).")
	cmd.Flags().Int("mixin-max-concurrency", configdefaults.DefaultChannelMaxConcurrency, "Max number of Mixin conversations processed concurrently.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_mixin_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_mixin_YYYYMMDD_HHmmss.md.")

	return cmd
}

func buildMixinRuntimeDeps(d Dependencies, runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig, reader *viper.Viper) mixinruntime.Dependencies {
	return mixinruntime.Dependencies{
		CommonDependencies: depsutil.ApplyRuntimeConfig(d.Dependencies, runtimeToolsConfig, reader),
		HandleModelCommand: d.HandleModelCommand,
		HandleSkillCommand: d.HandleSkillCommand,
	}
}
