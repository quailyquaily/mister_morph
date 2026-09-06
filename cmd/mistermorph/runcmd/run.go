package runcmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/processsignal"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Dependencies struct {
	RegistryFromViper            func() *tools.Registry
	RegisterTriggeredStaticTools func(*tools.Registry, map[string]bool)
	GuardFromViper               func(*slog.Logger) (*guard.Guard, error)
}

const defaultHeartbeatTask = "Run the heartbeat check."

func withCLIContextCompactionStatus(ctx context.Context, logger *slog.Logger, writer io.Writer) context.Context {
	if writer == nil {
		return ctx
	}
	return taskruntime.WithContextCompactionNotification(ctx, logger, func(_ context.Context, _ agent.Event, text string) error {
		_, err := fmt.Fprintln(writer, text)
		return err
	})
}

func resolveRunRoutes(values llmutil.RuntimeValues, selectionKey string) (llmutil.ResolvedRoute, llmutil.ResolvedRoute, error) {
	mainRoute, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return llmutil.ResolvedRoute{}, llmutil.ResolvedRoute{}, err
	}
	planRoute, err := llmutil.ResolveRoute(values, llmutil.RoutePurposePlanCreate)
	if err != nil {
		return llmutil.ResolvedRoute{}, llmutil.ResolvedRoute{}, err
	}
	return llmutil.SelectRouteCandidate(mainRoute, selectionKey), llmutil.SelectRouteCandidate(planRoute, selectionKey), nil
}

type cliRunPreparation struct {
	runID              string
	workspaceDir       string
	values             llmutil.RuntimeValues
	mainRoute          llmutil.ResolvedRoute
	planRoute          llmutil.ResolvedRoute
	logger             *slog.Logger
	logOptions         agent.LogOptions
	skillsConfig       skillsutil.SkillsConfig
	runtimeToolsConfig toolsutil.RuntimeToolsRegisterConfig
	runtimePaths       runtimepaths.Paths
	agentConfig        agent.Config
	engineToolsConfig  agent.EngineToolsConfig
	clientDecorator    taskruntime.ClientDecorator
	createLLMClient    func(llmutil.ResolvedRoute) (llm.Client, error)
	createImageClient  func() (llm.ImageClient, error)
	acpAgents          []acpclient.AgentConfig
}

func applyRunClientConfigOverrides(cmd *cobra.Command, cfg *llmconfig.ClientConfig) {
	if cmd == nil || cfg == nil {
		return
	}
	if cmd.Flags().Changed("provider") {
		cfg.Provider = strings.TrimSpace(configutil.FlagOrViperString(cmd, "provider", ""))
	}
	if cmd.Flags().Changed("endpoint") {
		cfg.Endpoint = strings.TrimSpace(configutil.FlagOrViperString(cmd, "endpoint", ""))
	}
	if cmd.Flags().Changed("api-key") {
		cfg.APIKey = strings.TrimSpace(configutil.FlagOrViperString(cmd, "api-key", ""))
	}
	if cmd.Flags().Changed("model") {
		cfg.Model = strings.TrimSpace(configutil.FlagOrViperString(cmd, "model", ""))
	}
	if cmd.Flags().Changed("llm-request-timeout") {
		cfg.RequestTimeout = configutil.FlagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout")
	}
}

func newCLIRunPreparer(prep cliRunPreparation, deps Dependencies) (*taskruntime.Runtime, error) {
	resolveRoute := func(purpose string) (llmutil.ResolvedRoute, error) {
		switch strings.TrimSpace(purpose) {
		case "", llmutil.RoutePurposeMainLoop:
			return prep.mainRoute, nil
		case llmutil.RoutePurposePlanCreate:
			return prep.planRoute, nil
		default:
			route, err := llmutil.ResolveRoute(prep.values, purpose)
			if err != nil {
				return llmutil.ResolvedRoute{}, err
			}
			return llmutil.SelectRouteCandidate(route, prep.runID), nil
		}
	}
	createLLMClient := prep.createLLMClient
	if createLLMClient == nil {
		createLLMClient = func(llmutil.ResolvedRoute) (llm.Client, error) {
			return nil, fmt.Errorf("create LLM client dependency missing")
		}
	}
	registry := deps.RegistryFromViper
	if registry == nil {
		registry = func() *tools.Registry { return tools.NewRegistry() }
	}
	common := depsutil.CommonDependencies{
		Logger:            func() (*slog.Logger, error) { return prep.logger, nil },
		LogOptions:        func() agent.LogOptions { return prep.logOptions },
		ResolveLLMRoute:   resolveRoute,
		CreateLLMClient:   createLLMClient,
		CreateImageClient: prep.createImageClient,
		Registry:          registry,
		ToolTriggers: func(task string) map[string]bool {
			return toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, prep.skillsConfig))
		},
		RegisterTriggeredStaticTools: deps.RegisterTriggeredStaticTools,
		ACPAgents: func() []acpclient.AgentConfig {
			return append([]acpclient.AgentConfig(nil), prep.acpAgents...)
		},
		RuntimeToolsConfig: prep.runtimeToolsConfig,
		RuntimePaths:       prep.runtimePaths,
		Guard:              deps.GuardFromViper,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			skillsCfg := prep.skillsConfig
			skillsCfg.Requested = append(append([]string(nil), skillsCfg.Requested...), stickySkills...)
			spec, loaded, err := skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsCfg)
			if err != nil {
				return agent.PromptSpec{}, nil, err
			}
			if block := workspace.PromptBlock(prep.workspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
			return spec, loaded, nil
		},
	}
	return taskruntime.NewRunPreparer(common, taskruntime.BootstrapOptions{
		AgentConfig:       prep.agentConfig,
		EngineToolsConfig: &prep.engineToolsConfig,
		ClientDecorator:   prep.clientDecorator,
	})
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
				hbTask, checklistEmpty, err := awarenessutil.BuildHeartbeatTask(hbChecklist)
				if err != nil {
					return err
				}
				task = hbTask
				if checklistEmpty {
					task = defaultHeartbeatTask
				}
				runMeta = awarenessutil.BuildAwarenessMeta(
					awarenessutil.BehaviorHeartbeat,
					"cli",
					viper.GetDuration("heartbeat.interval"),
					hbChecklist,
					checklistEmpty,
					awarenessdomain.PokeInput{},
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
			workspaceDir, err := workspace.ResolveInitialWorkspace(
				launchDir,
				rawWorkspace,
				noWorkspace,
				viper.GetString("workspace_dir"),
				nil,
			)
			if err != nil {
				return err
			}

			llmValues, err := llmutil.RuntimeValuesFromViper()
			if err != nil {
				return err
			}
			runtimePaths := runtimepaths.FromReader(viper.GetViper())
			runID := llmstats.NewSyntheticRunID("cli")
			mainRoute, planRoute, err := resolveRunRoutes(llmValues, runID)
			if err != nil {
				return err
			}
			mainCfg := mainRoute.ClientConfig
			applyRunClientConfigOverrides(cmd, &mainCfg)
			mainRoute.ClientConfig = mainCfg
			mainRoute.Values = llmutil.RuntimeValuesWithClientConfig(mainRoute.Values, mainCfg)
			if cmd.Flags().Changed("llm-request-timeout") && mainCfg.RequestTimeout > 0 {
				mainRoute.Values.ImageTimeoutRaw = mainCfg.RequestTimeout.String()
			}

			interactive := configutil.FlagOrViperBool(cmd, "interactive", "interactive")
			runParent := cmd.Context()
			if interactive {
				runParent = processsignal.InteractiveParent(runParent)
			}
			timeout := configutil.FlagOrViperDuration(cmd, "timeout", "timeout")
			ctx, cancel := context.WithTimeout(runParent, timeout)
			defer cancel()

			logger, err := logutil.LoggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			logOpts := logutil.LogOptionsFromViper()
			skillsCfg := skillsutil.SkillsConfigFromRunCmd(cmd)
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
			runtimeToolsCfg := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
			imageValues := mainRoute.Values
			runtimeToolsCfg.Image = toolsutil.ApplyImageToolLLMConfig(runtimeToolsCfg.Image, toolsutil.ImageToolLLMConfig{
				Provider:            imageValues.Provider,
				APIKey:              imageValues.APIKey,
				Model:               imageValues.Model,
				ImageProvider:       imageValues.ImageProvider,
				ImageAPIKey:         imageValues.ImageAPIKey,
				ImageModel:          imageValues.ImageModel,
				CloudflareAccountID: imageValues.CloudflareAccountID,
				CloudflareAPIToken:  imageValues.CloudflareAPIToken,
			})

			var hook agent.Hook
			if interactive {
				hook, err = newInteractiveHook()
				if err != nil {
					return err
				}
			}

			var clientDecorator taskruntime.ClientDecorator
			if promptInspector != nil || requestInspector != nil {
				clientDecorator = func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
					return llminspect.WrapClient(client, llminspect.ClientOptions{
						PromptInspector:  promptInspector,
						RequestInspector: requestInspector,
						APIBase:          route.ClientConfig.Endpoint,
						Model:            route.ClientConfig.Model,
					})
				}
			}
			topicStore := topiccontext.NewStore(runtimePaths.TopicContextPath)
			createLLMClient := func(route llmutil.ResolvedRoute) (llm.Client, error) {
				return llmutil.BuildRouteClient(
					route,
					nil,
					llmutil.ClientFromConfigWithValues,
					func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
						return llmstats.WrapClient(client, llmstats.ClientOptions{
							Provider:            cfg.Provider,
							APIBase:             cfg.Endpoint,
							DefaultModel:        cfg.Model,
							ContextWindowTokens: cfg.ContextWindowTokens,
							JournalDir:          runtimePaths.LLMUsageJournalDir,
							TopicContextStore:   topicStore,
							Logger:              logger,
						})
					},
					logger,
				)
			}
			createImageClient := func() (llm.ImageClient, error) {
				client, err := llmutil.ImageClientFromValues(imageValues)
				if err != nil {
					return client, err
				}
				meta := llmutil.ResolveImageClientMetadata(imageValues)
				return llmstats.WrapImageClient(client, llmstats.ClientOptions{
					Provider:     meta.Provider,
					APIBase:      meta.Endpoint,
					DefaultModel: meta.Model,
					JournalDir:   runtimePaths.LLMUsageJournalDir,
					Logger:       logger,
				}), nil
			}
			preparer, err := newCLIRunPreparer(cliRunPreparation{
				runID:              runID,
				workspaceDir:       workspaceDir,
				values:             llmValues,
				mainRoute:          mainRoute,
				planRoute:          planRoute,
				logger:             logger,
				logOptions:         logOpts,
				skillsConfig:       skillsCfg,
				runtimeToolsConfig: runtimeToolsCfg,
				runtimePaths:       runtimePaths,
				agentConfig: agent.Config{
					MaxSteps:        configutil.FlagOrViperInt(cmd, "max-steps", "max_steps"),
					ParseRetries:    configutil.FlagOrViperInt(cmd, "parse-retries", "parse_retries"),
					MaxTokenBudget:  configutil.FlagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
					ToolRepeatLimit: configutil.FlagOrViperInt(cmd, "tool-repeat-limit", "tool_repeat_limit"),
					DefaultModel:    strings.TrimSpace(mainCfg.Model),
					ContextCompaction: agent.NewContextCompactionConfig(
						viper.GetBool("context_compaction.enabled"),
						viper.GetFloat64("context_compaction.trigger_ratio"),
					),
				},
				engineToolsConfig: agent.EngineToolsConfig{
					SpawnEnabled:    viper.GetBool("tools.spawn.enabled"),
					ACPSpawnEnabled: viper.GetBool("tools.acp_spawn.enabled"),
					CoderEnabled:    viper.GetBool("tools.coder.enabled"),
					PathRoots: pathroots.New(
						"",
						strings.TrimSpace(viper.GetString("file_cache_dir")),
						strings.TrimSpace(viper.GetString("file_state_dir")),
					),
					CoderPathExtra: append([]string(nil), viper.GetStringSlice("tools.coder.path_extra")...),
				},
				clientDecorator:   clientDecorator,
				createLLMClient:   createLLMClient,
				createImageClient: createImageClient,
				acpAgents:         acpclient.AgentsFromViper(),
			}, deps)
			if err != nil {
				return err
			}
			defer func() { _ = preparer.Close() }()

			ctx = llmstats.WithRunID(ctx, runID)
			ctx = pathroots.WithWorkspaceDir(ctx, workspaceDir)
			ctx = withCLIContextCompactionStatus(ctx, logger, cmd.ErrOrStderr())
			var planStepUpdate func(*agent.Context, agent.PlanStepUpdate)
			if !isHeartbeat {
				planStepUpdate = func(runCtx *agent.Context, update agent.PlanStepUpdate) {
					if payload := formatPlanProgressUpdate(runCtx, update); payload != "" {
						_, _ = fmt.Fprintln(os.Stdout, payload)
					}
				}
			}
			result, err := preparer.Run(ctx, taskruntime.RunRequest{
				Task:                     task,
				Model:                    strings.TrimSpace(mainCfg.Model),
				Route:                    &mainRoute,
				Scene:                    "cli.loop",
				Meta:                     runMeta,
				DisableTodoWorkflow:      isHeartbeat,
				DisableContextCompaction: isHeartbeat,
				Hook:                     hook,
				PlanStepUpdate:           planStepUpdate,
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
			final := result.Final
			runCtx := result.Context

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
	cmd.Flags().String("provider", "", "Override LLM provider.")
	cmd.Flags().String("endpoint", "", "Override LLM endpoint.")
	cmd.Flags().String("model", "", "Override LLM model.")
	cmd.Flags().String("api-key", "", "API key.")
	cmd.Flags().Duration("llm-request-timeout", configdefaults.DefaultLLMRequestTimeout, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().Bool("interactive", false, "Ctrl-C pauses and lets you inject extra context, then continues.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_YYYYMMDD_HHmm.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_YYYYMMDD_HHmm.md.")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Default: file_state_dir/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-enabled", true, "Enable loading configured skills.")

	cmd.Flags().Int("max-steps", configdefaults.DefaultMaxSteps, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", configdefaults.DefaultParseRetries, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", configdefaults.DefaultMaxTokenBudget, "Max cumulative token budget (0 disables).")
	cmd.Flags().Int("tool-repeat-limit", configdefaults.DefaultToolRepeatLimit, "Force final when the same successful tool call repeats this many times.")

	cmd.Flags().Duration("timeout", configdefaults.DefaultTaskTimeout, "Overall timeout.")

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
