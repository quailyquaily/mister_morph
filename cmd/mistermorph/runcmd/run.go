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
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Dependencies struct {
	RegistryFromViper func() *tools.Registry
	GuardFromViper    func(*slog.Logger) *guard.Guard
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
				hbTask, checklistEmpty, err := heartbeatutil.BuildHeartbeatTask(hbChecklist)
				if err != nil {
					return err
				}
				task = hbTask
				runMeta = heartbeatutil.BuildHeartbeatMeta(
					"cli",
					viper.GetDuration("heartbeat.interval"),
					hbChecklist,
					checklistEmpty,
					nil,
					nil,
				)
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

			launchDir, err := os.Getwd()
			if err != nil {
				return err
			}
			rawWorkspace, _ := cmd.Flags().GetString("workspace")
			noWorkspace, _ := cmd.Flags().GetBool("no-workspace")
			workspaceDir, err := workspace.ResolveInitialWorkspace(launchDir, rawWorkspace, noWorkspace, nil)
			if err != nil {
				return err
			}

			llmValues := llmutil.RuntimeValuesFromViper()
			mainRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposeMainLoop)
			if err != nil {
				return err
			}
			mainCfg := mainRoute.ClientConfig
			if cmd.Flags().Changed("provider") {
				mainCfg.Provider = strings.TrimSpace(configutil.FlagOrViperString(cmd, "provider", ""))
			}
			if cmd.Flags().Changed("endpoint") {
				mainCfg.Endpoint = strings.TrimSpace(configutil.FlagOrViperString(cmd, "endpoint", ""))
			}
			if cmd.Flags().Changed("api-key") {
				mainCfg.APIKey = strings.TrimSpace(configutil.FlagOrViperString(cmd, "api-key", ""))
			}
			if cmd.Flags().Changed("model") {
				mainCfg.Model = strings.TrimSpace(configutil.FlagOrViperString(cmd, "model", ""))
			}
			if cmd.Flags().Changed("llm-request-timeout") {
				mainCfg.RequestTimeout = configutil.FlagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout")
			}

			timeout := configutil.FlagOrViperDuration(cmd, "timeout", "timeout")
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			logger, err := logutil.LoggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)
			client, err := llmutil.BuildRouteClient(
				mainRoute,
				&mainCfg,
				llmutil.ClientFromConfigWithValues,
				func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
					return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
				},
				logger,
			)
			if err != nil {
				return err
			}

			logOpts := logutil.LogOptionsFromViper()
			var requestInspector *llminspect.RequestInspector
			var promptInspector *llminspect.PromptInspector

			if configutil.FlagOrViperBool(cmd, "inspect-request", "") {
				requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
					Task: task,
				})
				if err != nil {
					return err
				}
				defer func() { _ = requestInspector.Close() }()
			}

			if configutil.FlagOrViperBool(cmd, "inspect-prompt", "") {
				promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
					Task: task,
				})
				if err != nil {
					return err
				}
				defer func() { _ = promptInspector.Close() }()
			}
			client = llminspect.WrapClient(client, llminspect.ClientOptions{
				PromptInspector:  promptInspector,
				RequestInspector: requestInspector,
				APIBase:          mainCfg.Endpoint,
				Model:            mainCfg.Model,
			})
			systemPromptCacheControl, err := llmutil.SystemPromptCacheControl(mainRoute.Values.CacheTTL)
			if err != nil {
				return err
			}

			reg := (*tools.Registry)(nil)
			if deps.RegistryFromViper != nil {
				reg = deps.RegistryFromViper()
			}
			if reg == nil {
				reg = tools.NewRegistry()
			}
			planClient := client
			planModel := strings.TrimSpace(mainCfg.Model)
			planRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposePlanCreate)
			if err != nil {
				return err
			}
			if !planRoute.SameProfile(mainRoute) {
				planClient, err = llmutil.BuildRouteClient(
					planRoute,
					nil,
					llmutil.ClientFromConfigWithValues,
					func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
						return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
					},
					logger,
				)
				if err != nil {
					return err
				}
				planClient = llminspect.WrapClient(planClient, llminspect.ClientOptions{
					PromptInspector:  promptInspector,
					RequestInspector: requestInspector,
					APIBase:          planRoute.ClientConfig.Endpoint,
					Model:            planRoute.ClientConfig.Model,
				})
			}
			planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
			toolsutil.RegisterRuntimeTools(reg, toolsutil.LoadRuntimeToolsRegisterConfigFromViper(), toolsutil.RuntimeToolLLMOptions{
				DefaultClient:    client,
				DefaultModel:     strings.TrimSpace(mainCfg.Model),
				PlanCreateClient: planClient,
				PlanCreateModel:  planModel,
			})

			promptSpec, _, err := skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, strings.TrimSpace(mainCfg.Model), skillsutil.SkillsConfigFromRunCmd(cmd))
			if err != nil {
				return err
			}
			promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
			promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
			promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
			promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
			promptprofile.AppendGPT5PromptPatch(&promptSpec, strings.TrimSpace(mainCfg.Model), logger)
			if block := workspace.PromptBlock(workspaceDir); strings.TrimSpace(block.Content) != "" {
				promptSpec.Blocks = append([]agent.PromptBlock{block}, promptSpec.Blocks...)
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
			if systemPromptCacheControl != nil {
				opts = append(opts, agent.WithSystemPromptCacheControl(systemPromptCacheControl))
			}
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

			if promptInspector != nil || requestInspector != nil {
				opts = append(opts, agent.WithSubClientFactory(func(prefix string) (llm.Client, func()) {
					var pi *llminspect.PromptInspector
					var ri *llminspect.RequestInspector
					if promptInspector != nil {
						var err error
						pi, err = llminspect.NewPromptInspector(llminspect.Options{
							Task:   task,
							Prefix: prefix,
						})
						if err != nil {
							logger.Warn("spawn_prompt_inspector_error", "error", err.Error())
						}
					}
					if requestInspector != nil {
						var err error
						ri, err = llminspect.NewRequestInspector(llminspect.Options{
							Task:   task,
							Prefix: prefix,
						})
						if err != nil {
							logger.Warn("spawn_request_inspector_error", "error", err.Error())
						}
					}
					subClient := llminspect.WrapClient(client, llminspect.ClientOptions{
						PromptInspector:  pi,
						RequestInspector: ri,
						APIBase:          mainCfg.Endpoint,
						Model:            mainCfg.Model,
					})
					cleanup := func() {
						if pi != nil {
							_ = pi.Close()
						}
						if ri != nil {
							_ = ri.Close()
						}
					}
					return subClient, cleanup
				}))
			}

			engine := agent.New(
				client,
				reg,
				agent.Config{
					MaxSteps:        configutil.FlagOrViperInt(cmd, "max-steps", "max_steps"),
					ParseRetries:    configutil.FlagOrViperInt(cmd, "parse-retries", "parse_retries"),
					MaxTokenBudget:  configutil.FlagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
					ToolRepeatLimit: configutil.FlagOrViperInt(cmd, "tool-repeat-limit", "tool_repeat_limit"),
					DefaultModel:    strings.TrimSpace(mainCfg.Model),
				},
				promptSpec,
				append(opts,
					agent.WithEngineToolsConfig(agent.EngineToolsConfig{
						SpawnEnabled:    viper.GetBool("tools.spawn.enabled"),
						ACPSpawnEnabled: viper.GetBool("tools.acp_spawn.enabled"),
					}),
					agent.WithACPAgents(acpclient.AgentsFromViper()),
				)...,
			)

			runID := llmstats.NewSyntheticRunID("cli")
			ctx = llmstats.WithRunID(ctx, runID)
			ctx = pathroots.WithWorkspaceDir(ctx, workspaceDir)
			final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{
				Model: strings.TrimSpace(mainCfg.Model),
				Scene: "cli.loop",
				Meta:  runMeta,
			})
			if err != nil {
				if errors.Is(err, errAbortedByUser) {
					return nil
				}
				displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
				if displayErr == "" {
					displayErr = strings.TrimSpace(err.Error())
				}
				return fmt.Errorf("%s", displayErr)
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
	cmd.Flags().String("workspace", "", "Attach a workspace directory for this run.")
	cmd.Flags().Bool("no-workspace", false, "Run without a workspace attachment.")
	cmd.Flags().String("provider", "openai", "Provider: openai|openai_resp|openai_custom|deepseek|xai|gemini|azure|anthropic|bedrock|susanoo|cloudflare.")
	cmd.Flags().String("endpoint", "https://api.openai.com", "Base URL for provider.")
	cmd.Flags().String("model", "gpt-5.2", "Model name.")
	cmd.Flags().String("api-key", "", "API key.")
	cmd.Flags().Duration("llm-request-timeout", 90*time.Second, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().Bool("interactive", false, "Ctrl-C pauses and lets you inject extra context, then continues.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_YYYYMMDD_HHmm.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_YYYYMMDD_HHmm.md.")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Default: file_state_dir/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-enabled", true, "Enable loading configured skills.")

	cmd.Flags().Int("max-steps", 15, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", 2, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", 0, "Max cumulative token budget (0 disables).")
	cmd.Flags().Int("tool-repeat-limit", 3, "Force final when the same successful tool call repeats this many times.")

	cmd.Flags().Duration("timeout", 10*time.Minute, "Overall timeout.")

	return cmd
}

func formatPlanProgressUpdate(runCtx *agent.Context, update agent.PlanStepUpdate) string {
	if runCtx == nil || runCtx.Plan == nil {
		return ""
	}
	if update.CompletedIndex < 0 && update.StartedIndex < 0 {
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
