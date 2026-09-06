package chatcmd

import (
	"fmt"
	"log/slog"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
)

type Dependencies struct {
	Version                      string
	RegistryFromViper            func() *tools.Registry
	RegisterTriggeredStaticTools func(*tools.Registry, map[string]bool)
	GuardFromViper               func(*slog.Logger) (*guard.Guard, error)
}

func New(deps Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive chat session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := buildChatSession(cmd, deps)
			if err != nil {
				return fmt.Errorf("build chat session: %w", err)
			}
			defer sess.cleanup()
			return runREPL(sess)
		},
	}

	cmd.Flags().String("provider", "", "Override LLM provider.")
	cmd.Flags().String("endpoint", "", "Override LLM endpoint.")
	cmd.Flags().String("model", "", "Override LLM model.")
	cmd.Flags().String("api-key", "", "Override API key.")
	cmd.Flags().String("profile", "", "Named LLM profile from config (e.g., 'kimi', 'gemini-alt', 'zhipu'). Overrides provider/model/api-key from the profile.")
	cmd.Flags().Duration("llm-request-timeout", configdefaults.DefaultLLMRequestTimeout, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Default: file_state_dir/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-enabled", true, "Enable loading configured skills.")
	cmd.Flags().Int("max-steps", configdefaults.DefaultMaxSteps, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", configdefaults.DefaultParseRetries, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", configdefaults.DefaultMaxTokenBudget, "Max cumulative token budget (0 disables).")
	cmd.Flags().Int("tool-repeat-limit", configdefaults.DefaultToolRepeatLimit, "Force final when the same successful tool call repeats this many times.")
	cmd.Flags().Duration("timeout", configdefaults.DefaultTaskTimeout, "Overall timeout.")
	cmd.Flags().Bool("verbose", false, "Show info-level logs (default: only errors).")
	cmd.Flags().String("workspace", "", "Attach a workspace directory for this chat session.")
	cmd.Flags().Bool("no-workspace", false, "Start chat without a workspace attachment.")

	return cmd
}
