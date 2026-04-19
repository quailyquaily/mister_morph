package chatcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
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
		Use:   "chat",
		Short: "Start an interactive chat session",
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout := configutil.FlagOrViperDuration(cmd, "timeout", "timeout")

			verbose, _ := cmd.Flags().GetBool("verbose")
			loggerCfg := logutil.LoggerConfigFromViper()
			if !verbose {
				loggerCfg.Level = "error"
			}
			logger, err := logutil.LoggerFromConfig(loggerCfg)
			if err != nil {
				return err
			}
			slog.SetDefault(logger)
			logOpts := logutil.LogOptionsFromViper()

			chatFileCacheDir, err := resolveChatFileCacheDir()
			if err != nil {
				return err
			}
			viper.Set("file_cache_dir", chatFileCacheDir)

			llmValues := llmutil.RuntimeValuesFromViper()
			mainRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposeMainLoop)
			if err != nil {
				return err
			}

			// Support --profile flag to use a named LLM profile from config
			if cmd.Flags().Changed("profile") {
				profileName, _ := cmd.Flags().GetString("profile")
				profileName = strings.TrimSpace(profileName)
				if profileName != "" {
					mainRoute, err = llmutil.ResolveRouteWithProfileOverride(llmValues, llmutil.RoutePurposeMainLoop, profileName)
					if err != nil {
						return fmt.Errorf("failed to resolve profile %q: %w", profileName, err)
					}
				}
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

			// Session-level model selection store (per-chat session, not global)
			sessionStore := llmselect.NewStore()
			if cmd.Flags().Changed("profile") {
				profileName, _ := cmd.Flags().GetString("profile")
				if strings.TrimSpace(profileName) != "" {
					sessionStore.SetProfile(profileName)
				}
			}

			buildClient := func(route llmutil.ResolvedRoute, cfgOverride *llmconfig.ClientConfig) (llm.Client, error) {
				return llmutil.BuildRouteClient(
					route,
					cfgOverride,
					llmutil.ClientFromConfigWithValues,
					func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
						return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
					},
					logger,
				)
			}

			client, err := buildClient(mainRoute, &mainCfg)
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
			}
			planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
			toolsutil.RegisterRuntimeTools(reg, toolsutil.LoadRuntimeToolsRegisterConfigFromViper(), toolsutil.RuntimeToolLLMOptions{
				DefaultClient:    client,
				DefaultModel:     strings.TrimSpace(mainCfg.Model),
				PlanCreateClient: planClient,
				PlanCreateModel:  planModel,
			})

			baseCtx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			promptSpec, _, err := skillsutil.PromptSpecWithSkills(baseCtx, logger, logOpts, "Interactive chat session", client, strings.TrimSpace(mainCfg.Model), skillsutil.SkillsConfigFromRunCmd(cmd))
			if err != nil {
				return err
			}
			promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
			promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
			promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
			promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
			promptprofile.AppendGPT5PromptPatch(&promptSpec, strings.TrimSpace(mainCfg.Model), logger)

			// Inject chat working directory context into system prompt
			promptSpec.Blocks = append([]agent.PromptBlock{{
				Content: fmt.Sprintf("## Chat Session Context\n\n"+
					"You are running in an interactive chat session. The user's current working directory is:\n\n"+
					"  %s\n\n"+
					"CRITICAL: All file operations (read_file, write_file, bash) MUST use paths relative to THIS directory by default. "+
					"When the user asks you to create or modify files WITHOUT specifying a path, write them to this directory (NOT to file_state_dir or ~/.morph/). "+
					"Only use file_state_dir or ~/.morph/ when the user explicitly asks for configuration files, memory, or state storage. "+
					"You may use `bash` with `ls`, `find`, etc. to explore the directory structure when needed.\n\n"+
					"## Built-in Chat Commands\n\n"+
					"The user can type these special commands at any time:\n"+
					"- `/exit` or `/quit` — exit the chat session\n"+
					"- `/forget` — clear the project memory\n"+
					"- `/memory` — display the current project memory\n"+
					"- `/remember <content>` — add an entry to project memory\n"+
					"- `/init` — generate an AGENTS.md file for the current project (analyzes the codebase and creates a guide for AI assistants)\n"+
					"- `/update` — regenerate AGENTS.md, overwriting the existing file (useful after major project changes)\n"+
					"If the user asks about any of these commands, explain what they do.", chatFileCacheDir),
			}}, promptSpec.Blocks...)

			// Initialize memory runtime
			subjectID := cliMemorySubjectID(chatFileCacheDir)
			_, memOrchestrator, memWorker, memCleanup, err := initChatMemoryRuntime(chatFileCacheDir, logger)
			if err != nil {
				logger.Warn("chat_memory_init_failed", "error", err.Error())
			}
			if memCleanup != nil {
				defer memCleanup()
			}
			if memWorker != nil {
				memWorker.Start(baseCtx)
			}

			// Inject memory context into prompt
			if memOrchestrator != nil {
				memCtx, memErr := memOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
					SubjectID:      subjectID,
					RequestContext: memory.ContextPrivate,
					MaxItems:       20,
				})
				if memErr != nil {
					logger.Warn("chat_memory_injection_failed", "error", memErr.Error())
				} else if strings.TrimSpace(memCtx) != "" {
					promptSpec.Blocks = append([]agent.PromptBlock{{
						Content: "## Project Memory\n\n" + memCtx,
					}}, promptSpec.Blocks...)
				}
			}

			var opts []agent.Option
			opts = append(opts, agent.WithLogger(logger))
			opts = append(opts, agent.WithLogOptions(logOpts))

			if deps.GuardFromViper != nil {
				if g := deps.GuardFromViper(logger); g != nil {
					opts = append(opts, agent.WithGuard(g))
				}
			}

			// Determine compact mode from flag or config.
			compactMode := configutil.FlagOrViperBool(cmd, "compact-mode", "chat.compact_mode")

			// Get system username for user prompt
			userName := buildUserName()

			// Load persona name for assistant display
			agentName := personautil.LoadAgentName(statepaths.FileStateDir())
			if agentName == "" {
				agentName = "assistant"
			}

			// Build user prompt based on compact mode
			userPrompt := buildUserPrompt(compactMode, userName)

			// Use rl.Stdout() as the unified writer
			var writer io.Writer

			var setAnimMessage func(msg string)
			var stopAnim func()

			// Add tool start callback to show what tools are being used
			opts = append(opts, agent.WithOnToolStart(func(runCtx *agent.Context, toolName string) {
				msg := fmt.Sprintf("\x1b[90m  used \x1b[36m%s\x1b[0m", toolName)
				_, _ = fmt.Fprintf(writer, "\r\033[K%s\n", msg)
			}))
			opts = append(opts, agent.WithPlanStepUpdate(func(runCtx *agent.Context, update agent.PlanStepUpdate) {
				logger.Debug("plan_step_update_callback", "completedIndex", update.CompletedIndex, "startedIndex", update.StartedIndex, "startedStep", update.StartedStep, "reason", update.Reason)
				payload := formatPlanProgressUpdate(runCtx, update)
				if payload != "" {
					if setAnimMessage != nil {
						setAnimMessage(payload)
					}
				} else if update.CompletedIndex >= 0 && update.CompletedStep != "" {
					if stopAnim != nil {
						stopAnim()
					}
					total := 0
					if runCtx != nil && runCtx.Plan != nil {
						total = len(runCtx.Plan.Steps)
					}
					_, _ = fmt.Fprintf(writer, "\033[90mplan: ✓ %s", update.CompletedStep)
					if total > 0 {
						_, _ = fmt.Fprintf(writer, " [%d/%d]", update.CompletedIndex+1, total)
					}
					_, _ = fmt.Fprint(writer, "\033[0m\n")
					stopAnim, setAnimMessage = thinkingAnimation(writer)
				} else if setAnimMessage != nil {
					setAnimMessage("assistant is thinking...")
				}
			}))

			makeEngine := func(engClient llm.Client, defaultModel string) *agent.Engine {
				return agent.New(
					engClient,
					reg,
					agent.Config{
						MaxSteps:        configutil.FlagOrViperInt(cmd, "max-steps", "max_steps"),
						ParseRetries:    configutil.FlagOrViperInt(cmd, "parse-retries", "parse_retries"),
						MaxTokenBudget:  configutil.FlagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
						ToolRepeatLimit: configutil.FlagOrViperInt(cmd, "tool-repeat-limit", "tool_repeat_limit"),
						DefaultModel:    strings.TrimSpace(defaultModel),
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
			}
			engine := makeEngine(client, mainCfg.Model)

			autoComplete := readline.NewPrefixCompleter(
				readline.PcItem("/exit"),
				readline.PcItem("/quit"),
				readline.PcItem("/forget"),
				readline.PcItem("/memory"),
				readline.PcItem("/remember "),
				readline.PcItem("/init"),
				readline.PcItem("/update"),
				readline.PcItem("/model"),
				readline.PcItem("/help"),
			)

			rl, err := readline.NewEx(&readline.Config{
				Prompt:       userPrompt,
				HistoryFile:  filepath.Join(os.Getenv("HOME"), ".mistermorph_chat_history"),
				AutoComplete: autoComplete,
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.OutOrStderr(),
			})
			if err != nil {
				return err
			}
			defer rl.Close()

			writer = rl.Stdout()

			printChatSessionHeader(writer, strings.TrimSpace(mainCfg.Model), chatFileCacheDir)

			history := make([]llm.Message, 0, 32)
			turn := 0
			for {
				input, err := rl.Readline()
				if err != nil {
					if err == readline.ErrInterrupt {
						if len(input) == 0 {
							_, _ = fmt.Fprintln(writer, "\nBye! 👋")
							return nil
						}
						continue
					}
					if err == io.EOF {
						_, _ = fmt.Fprintln(writer)
						return nil
					}
					return err
				}
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}

				switch strings.ToLower(input) {
				case "exit", "/exit", "/quit":
					handleExit(writer)
					return nil
				case "/forget":
					handleForget(writer, memOrchestrator, memWorker, subjectID)
					continue
				case "/memory":
					handleMemory(writer, memOrchestrator, subjectID)
					continue
				case "/help":
					handleHelp(writer)
					continue
				case "/init":
					agentsPath := filepath.Join(chatFileCacheDir, "AGENTS.md")
					if handleInitRead(writer, agentsPath) {
						continue
					}
					// Does not exist — generate via AI (fall through)
					fallthrough
				case "/update":
					newHistory, ok := handleAgentsGenerate(writer, input, chatFileCacheDir, timeout, engine, mainCfg.Model, history)
					if ok {
						history = newHistory
					}
					continue
				}

				// Handle /model commands
				if strings.HasPrefix(strings.ToLower(input), "/model") {
					newClient, newCfg, handled := handleModelCommand(writer, input, llmValues, sessionStore, buildClient)
					if handled {
						client = newClient
						mainCfg = newCfg
						engine = makeEngine(client, mainCfg.Model)
					}
					continue
				}

				// Handle /remember <content> command
				if strings.HasPrefix(strings.ToLower(input), "/remember ") {
					handleRemember(writer, input, memOrchestrator, memWorker, subjectID)
					continue
				}

				turnCtx, turnCancel := context.WithCancel(context.Background())
				go func() {
					<-time.After(timeout)
					turnCancel()
				}()
				runID := llmstats.NewSyntheticRunID("chat")
				turnCtx = llmstats.WithRunID(turnCtx, runID)
				stopAnim, setAnimMessage = thinkingAnimation(writer)
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, os.Interrupt)
				go func() {
					select {
					case <-sigCh:
						turnCancel()
					case <-turnCtx.Done():
					}
					signal.Stop(sigCh)
				}()

				final, runCtx, err := engine.Run(turnCtx, input, agent.RunOptions{
					Model:   strings.TrimSpace(mainCfg.Model),
					Scene:   "chat.interactive",
					History: append([]llm.Message(nil), history...),
				})

				stopAnim()
				turnCancel()
				if err != nil {
					if errors.Is(err, context.Canceled) {
						_, _ = fmt.Fprintln(writer, "\n\033[33m⚡ Interrupted.\033[0m")
						continue
					}
					displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
					if displayErr == "" {
						displayErr = strings.TrimSpace(err.Error())
					}
					_, _ = fmt.Fprintf(writer, "error: %s\n", displayErr)
					continue
				}

				output := formatChatOutput(final)
				if compactMode {
					_, _ = fmt.Fprintf(writer, "%s\n", output)
				} else {
					_, _ = fmt.Fprintf(writer, "\033[48;5;208m\033[30m %s> \033[0m %s\n", agentName, output)
				}

				history = append(history,
					llm.Message{Role: "user", Content: input},
					llm.Message{Role: "assistant", Content: output},
				)

				logger.Info("chat_turn_done",
					"turn", turn,
					"steps", len(runCtx.Steps),
					"llm_rounds", runCtx.Metrics.LLMRounds,
					"total_tokens", runCtx.Metrics.TotalTokens,
				)

				// Auto-update memory if there were tool calls
				autoUpdateMemory(writer, logger, memOrchestrator, memWorker, subjectID, runID, input, output, runCtx.Steps)

				turn++
			}
		},
	}

	cmd.Flags().String("provider", "", "Override LLM provider.")
	cmd.Flags().String("endpoint", "", "Override LLM endpoint.")
	cmd.Flags().String("model", "", "Override LLM model.")
	cmd.Flags().String("api-key", "", "Override API key.")
	cmd.Flags().String("profile", "", "Named LLM profile from config (e.g., 'kimi', 'gemini-alt', 'zhipu'). Overrides provider/model/api-key from the profile.")
	cmd.Flags().Duration("llm-request-timeout", 90*time.Second, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Default: file_state_dir/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-enabled", true, "Enable loading configured skills.")
	cmd.Flags().Int("max-steps", 15, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", 2, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", 0, "Max cumulative token budget (0 disables).")
	cmd.Flags().Int("tool-repeat-limit", 3, "Force final when the same successful tool call repeats this many times.")
	cmd.Flags().Duration("timeout", 30*time.Minute, "Overall timeout.")
	cmd.Flags().Bool("compact-mode", false, "Compact display mode: omit user/assistant name prefixes in prompts and output.")
	cmd.Flags().Bool("verbose", false, "Show info-level logs (default: only errors).")

	return cmd
}

func resolveChatFileCacheDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory for chat file_cache_dir: %w", err)
	}
	return filepath.Clean(wd), nil
}
