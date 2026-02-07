package runcmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	telegramcmd "github.com/quailyquaily/mistermorph/cmd/mistermorph/telegramcmd"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/retryutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Dependencies struct {
	RegistryFromViper func() *tools.Registry
	GuardFromViper    func(*slog.Logger) *guard.Guard
	RegisterPlanTool  func(*tools.Registry, llm.Client, string)
}

func New(deps Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent task",
		RunE: func(cmd *cobra.Command, args []string) error {
			isHeartbeat := configutil.FlagOrViperBool(cmd, "heartbeat", "")
			task := ""
			var runMeta map[string]any
			if isHeartbeat {
				hbChecklist := statepaths.HeartbeatChecklistPath()
				var hbSnapshot string
				if viper.GetBool("memory.enabled") {
					hbMgr := memory.NewManager(statepaths.MemoryDir(), viper.GetInt("memory.short_term_days"))
					snap, err := heartbeatutil.BuildHeartbeatProgressSnapshot(hbMgr, viper.GetInt("memory.injection.max_items"))
					if err != nil {
						return err
					}
					hbSnapshot = snap
				}
				hbTask, checklistEmpty, err := heartbeatutil.BuildHeartbeatTask(hbChecklist, hbSnapshot)
				if err != nil {
					return err
				}
				task = hbTask
				runMeta = map[string]any{
					"trigger": "heartbeat",
					"heartbeat": heartbeatutil.BuildHeartbeatMeta(
						"cli",
						viper.GetDuration("heartbeat.interval"),
						hbChecklist,
						checklistEmpty,
						nil,
						nil,
					),
				}
			} else {
				task = strings.TrimSpace(configutil.FlagOrViperString(cmd, "task", "task"))
				if task == "" {
					data, err := os.ReadFile("/dev/stdin")
					if err == nil {
						task = strings.TrimSpace(string(data))
					}
				}
				if task == "" {
					return fmt.Errorf("missing --task (or stdin)")
				}
			}

			provider := llmutil.ProviderFromViper()
			if cmd.Flags().Changed("provider") {
				provider = strings.TrimSpace(configutil.FlagOrViperString(cmd, "provider", ""))
			}
			endpoint := llmutil.EndpointForProvider(provider)
			if cmd.Flags().Changed("endpoint") {
				endpoint = strings.TrimSpace(configutil.FlagOrViperString(cmd, "endpoint", ""))
			}
			apiKey := llmutil.APIKeyForProvider(provider)
			if cmd.Flags().Changed("api-key") {
				apiKey = strings.TrimSpace(configutil.FlagOrViperString(cmd, "api-key", ""))
			}
			model := llmutil.ModelForProvider(provider)
			if cmd.Flags().Changed("model") {
				model = strings.TrimSpace(configutil.FlagOrViperString(cmd, "model", ""))
			}

			requestTimeout := configutil.FlagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout")
			client, err := llmutil.ClientFromConfig(llmconfig.ClientConfig{
				Provider:       provider,
				Endpoint:       endpoint,
				APIKey:         apiKey,
				Model:          model,
				RequestTimeout: requestTimeout,
			})
			if err != nil {
				return err
			}

			timeout := configutil.FlagOrViperDuration(cmd, "timeout", "timeout")
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			logger, err := logutil.LoggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			logOpts := logutil.LogOptionsFromViper()

			if configutil.FlagOrViperBool(cmd, "inspect-request", "") {
				inspector, err := newRequestInspector(task)
				if err != nil {
					return err
				}
				defer func() { _ = inspector.Close() }()
				setter, ok := client.(interface {
					SetDebugFn(func(label, payload string))
				})
				if !ok {
					return fmt.Errorf("inspect-request requires uniai provider client")
				}
				setter.SetDebugFn(inspector.Dump)
			}

			if configutil.FlagOrViperBool(cmd, "inspect-prompt", "") {
				inspector, err := newPromptInspector(task)
				if err != nil {
					return err
				}
				defer func() { _ = inspector.Close() }()
				client = &inspectClient{base: client, inspector: inspector}
			}

			promptSpec, _, skillAuthProfiles, err := skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsutil.SkillsConfigFromRunCmd(cmd, model))
			if err != nil {
				return err
			}

			var memManager *memory.Manager
			var memIdentity memory.Identity
			if viper.GetBool("memory.enabled") {
				id := resolveRunMemoryIdentity()
				if id.Enabled && strings.TrimSpace(id.SubjectID) != "" {
					memIdentity = id
					memManager = memory.NewManager(statepaths.MemoryDir(), viper.GetInt("memory.short_term_days"))
					if viper.GetBool("memory.injection.enabled") {
						maxItems := viper.GetInt("memory.injection.max_items")
						snap, err := memManager.BuildInjection(id.SubjectID, memory.ContextPrivate, maxItems)
						if err != nil {
							return fmt.Errorf("memory injection: %w", err)
						}
						if strings.TrimSpace(snap) != "" {
							promptSpec.Blocks = append(promptSpec.Blocks, agent.PromptBlock{
								Title:   "Memory Summaries",
								Content: snap,
							})
						}
					}
				}
			}

			var hook agent.Hook
			if configutil.FlagOrViperBool(cmd, "interactive", "interactive") {
				hook, err = newInteractiveHook()
				if err != nil {
					return err
				}
			}

			var opts []agent.Option
			if hook != nil {
				opts = append(opts, agent.WithHook(hook))
			}
			opts = append(opts, agent.WithLogger(logger))
			opts = append(opts, agent.WithLogOptions(logOpts))
			opts = append(opts, agent.WithSkillAuthProfiles(skillAuthProfiles, viper.GetBool("secrets.require_skill_profiles")))
			if !isHeartbeat {
				opts = append(opts, agent.WithPlanStepUpdate(func(runCtx *agent.Context, update agent.PlanStepUpdate) {
					if payload := formatPlanProgressUpdate(runCtx, update); payload != "" {
						_, _ = fmt.Fprintln(os.Stdout, payload)
					}
				}))
			}
			if deps.GuardFromViper != nil {
				if g := deps.GuardFromViper(logger); g != nil {
					opts = append(opts, agent.WithGuard(g))
				}
			}
			reg := (*tools.Registry)(nil)
			if deps.RegistryFromViper != nil {
				reg = deps.RegistryFromViper()
			}
			if reg == nil {
				reg = tools.NewRegistry()
			}
			if deps.RegisterPlanTool != nil {
				deps.RegisterPlanTool(reg, client, model)
			}

			engine := agent.New(
				client,
				reg,
				agent.Config{
					MaxSteps:         configutil.FlagOrViperInt(cmd, "max-steps", "max_steps"),
					ParseRetries:     configutil.FlagOrViperInt(cmd, "parse-retries", "parse_retries"),
					MaxTokenBudget:   configutil.FlagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
					IntentEnabled:    viper.GetBool("intent.enabled"),
					IntentTimeout:    requestTimeout,
					IntentMaxHistory: viper.GetInt("intent.max_history"),
				},
				promptSpec,
				opts...,
			)

			final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{Model: model, Meta: runMeta})
			if err != nil {
				if errors.Is(err, errAbortedByUser) {
					return nil
				}
				return err
			}

			if !isHeartbeat && memManager != nil && memIdentity.Enabled && strings.TrimSpace(memIdentity.SubjectID) != "" {
				if err := updateRunMemory(ctx, logger, client, model, memManager, memIdentity, task, final, requestTimeout); err != nil {
					if errors.Is(err, context.DeadlineExceeded) {
						retryutil.AsyncRetry(logger, "memory_update", 2*time.Second, requestTimeout, func(retryCtx context.Context) error {
							return updateRunMemory(retryCtx, logger, client, model, memManager, memIdentity, task, final, requestTimeout)
						})
					}
					logger.Warn("memory_update_error", "error", err.Error())
				}
			}

			logger.Info("run_done",
				"steps", len(runCtx.Steps),
				"llm_rounds", runCtx.Metrics.LLMRounds,
				"total_tokens", runCtx.Metrics.TotalTokens,
				"parse_retries", runCtx.Metrics.ParseRetries,
			)

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(final)
		},
	}

	cmd.Flags().String("task", "", "Task to run (if empty, reads from stdin).")
	cmd.Flags().Bool("heartbeat", false, "Run a single heartbeat check (ignores --task and stdin).")
	cmd.Flags().String("provider", "openai", "Provider: openai|openai_custom|deepseek|xai|gemini|azure|anthropic|bedrock|susanoo.")
	cmd.Flags().String("endpoint", "https://api.openai.com", "Base URL for provider.")
	cmd.Flags().String("model", "gpt-5.2", "Model name.")
	cmd.Flags().String("api-key", "", "API key.")
	cmd.Flags().Duration("llm-request-timeout", 90*time.Second, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().Bool("interactive", false, "Ctrl-C pauses and lets you inject extra context, then continues.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_YYYYMMDD_HHmm.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_YYYYMMDD_HHmm.md.")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Defaults: ~/.codex/skills, ~/.claude/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-auto", true, "Auto-load skills referenced in task via $SkillName.")
	cmd.Flags().String("skills-mode", "smart", "Skills mode: off|explicit|smart.")
	cmd.Flags().Int("skills-max-load", 3, "Max skills to load in smart mode.")
	cmd.Flags().Int64("skills-preview-bytes", 2048, "Bytes to preview per skill during smart selection.")
	cmd.Flags().Int("skills-catalog-limit", 200, "Max number of discovered skills to include in smart selection catalog.")
	cmd.Flags().Duration("skills-select-timeout", 10*time.Second, "Timeout for smart skills selection call.")

	cmd.Flags().Int("max-steps", 15, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", 2, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", 0, "Max cumulative token budget (0 disables).")

	cmd.Flags().Duration("timeout", 10*time.Minute, "Overall timeout.")

	return cmd
}

func resolveRunMemoryIdentity() memory.Identity {
	return memory.Identity{
		Enabled:     true,
		ExternalKey: "CLI_USER",
		SubjectID:   "CLI_USER",
	}
}

func updateRunMemory(ctx context.Context, logger *slog.Logger, client llm.Client, model string, mgr *memory.Manager, id memory.Identity, task string, final *agent.Final, requestTimeout time.Duration) error {
	if mgr == nil || client == nil {
		return nil
	}
	output := heartbeatutil.FormatFinalOutput(final)
	contactNickname := strings.TrimSpace(os.Getenv("USER"))
	if contactNickname == "" {
		contactNickname = strings.TrimSpace(os.Getenv("USERNAME"))
	}
	meta := memory.WriteMeta{
		SessionID:       "cli",
		Source:          "cli",
		Channel:         "local",
		SubjectID:       id.SubjectID,
		ContactID:       strings.TrimSpace(id.SubjectID),
		ContactNickname: contactNickname,
	}
	date := time.Now().UTC()
	_, existingContent, hasExisting, err := mgr.LoadShortTerm(date, meta.SessionID)
	if err != nil {
		return err
	}

	memCtx := ctx
	cancel := func() {}
	if requestTimeout > 0 {
		memCtx, cancel = context.WithTimeout(ctx, requestTimeout)
	}
	defer cancel()

	ctxInfo := telegramcmd.MemoryDraftContext{
		SessionID:        meta.SessionID,
		ChatType:         "cli",
		CounterpartyName: strings.TrimSpace(os.Getenv("USER")),
		TimestampUTC:     date.Format(time.RFC3339),
	}
	if ctxInfo.CounterpartyName == "" {
		ctxInfo.CounterpartyName = strings.TrimSpace(os.Getenv("USERNAME"))
	}

	draft, err := telegramcmd.BuildMemoryDraft(memCtx, client, model, nil, task, output, existingContent, ctxInfo)
	if err != nil {
		return err
	}
	draft.Promote = telegramcmd.EnforceLongTermPromotionRules(draft.Promote, nil, task)

	summary := strings.TrimSpace(draft.Summary)
	var mergedContent memory.ShortTermContent
	if hasExisting && telegramcmd.HasDraftContent(draft) {
		semantic, semanticSummary, mergeErr := telegramcmd.SemanticMergeShortTerm(memCtx, client, model, existingContent, draft)
		if mergeErr != nil {
			return mergeErr
		}
		mergedContent = semantic
		summary = semanticSummary
	} else {
		mergedContent = memory.MergeShortTerm(existingContent, draft)
	}

	if _, err := mgr.WriteShortTerm(date, mergedContent, summary, meta); err != nil {
		return err
	}

	updates := append([]memory.TaskItem{}, mergedContent.Tasks...)
	updates = append(updates, mergedContent.FollowUps...)
	if updated, err := mgr.UpdateRecentTaskStatuses(updates, meta.SessionID); err != nil {
		if logger != nil {
			logger.Warn("memory_task_sync_error", "error", err.Error())
		}
	} else if updated > 0 {
		if logger != nil {
			logger.Debug("memory_task_sync_ok", "updated", updated)
		}
	}

	if _, err := mgr.UpdateLongTerm(id.SubjectID, draft.Promote); err != nil {
		return err
	}
	if logger != nil {
		logger.Debug("memory_update_ok", "subject_id", id.SubjectID)
	}
	return nil

}
func formatPlanProgressUpdate(runCtx *agent.Context, update agent.PlanStepUpdate) string {
	if runCtx == nil || runCtx.Plan == nil {
		return ""
	}
	if update.CompletedIndex < 0 {
		return ""
	}
	total := len(runCtx.Plan.Steps)
	if total == 0 {
		return ""
	}
	payload := map[string]any{
		"type": "plan_step",
		"plan_step": map[string]any{
			"completed_index": update.CompletedIndex,
			"completed_step":  strings.TrimSpace(update.CompletedStep),
			"started_index":   update.StartedIndex,
			"started_step":    strings.TrimSpace(update.StartedStep),
			"total_steps":     total,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

var errAbortedByUser = errors.New("aborted by user")

func newInteractiveHook() (agent.Hook, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("interactive mode requires /dev/tty: %w", err)
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)

	r := bufio.NewReader(tty)

	return func(ctx context.Context, step int, agentCtx *agent.Context, messages *[]llm.Message) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-interrupts:
			_, _ = fmt.Fprintf(os.Stderr, "\n[interactive] paused at step=%d. Enter extra context (end with empty line).\n", step)
			_, _ = fmt.Fprintln(os.Stderr, "[interactive] commands: /continue (no-op), /abort (stop run)")
			note, err := readMultiline(r)
			if err != nil {
				return err
			}
			note = strings.TrimSpace(note)
			switch note {
			case "", "/continue":
				return nil
			case "/abort":
				return errAbortedByUser
			default:
				*messages = append(*messages, llm.Message{
					Role:    "user",
					Content: "Operator context:\n" + note,
				})
				_, _ = fmt.Fprintln(os.Stderr, "[interactive] context injected; continuing.")
				return nil
			}
		default:
			return nil
		}
	}, nil
}

func readMultiline(r *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
		if err != nil {
			break
		}
	}
	return strings.Join(lines, "\n"), nil
}
