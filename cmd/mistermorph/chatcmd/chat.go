package chatcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"golang.org/x/term"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/climemory"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const chatBanner = `

▄▄   ▄▄  ▄▄▄  ▄▄▄▄  ▄▄▄▄  ▄▄ ▄▄
██▀▄▀██ ██▀██ ██▄█▄ ██▄█▀ ██▄██
██   ██ ▀███▀ ██ ██ ██    ██ ██
`

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

			// Load project-level memory for chat mode
			var chatMemory *climemory.Memory
			chatMemory, _ = climemory.Load(chatFileCacheDir)
			if chatMemory != nil {
				if block := chatMemory.ToPromptBlock(); block != "" {
					promptSpec.Blocks = append([]agent.PromptBlock{{
						Content: block,
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
			userName := ""
			if u, err := user.Current(); err == nil && u != nil {
				userName = strings.TrimSpace(u.Username)
			}
			if userName == "" {
				userName = strings.TrimSpace(os.Getenv("USER"))
			}
			if userName == "" {
				userName = "you"
			}

			// Load persona name for assistant display
			agentName := personautil.LoadAgentName(statepaths.FileStateDir())
			if agentName == "" {
				agentName = "assistant"
			}

			// Build user prompt based on compact mode
			var userPrompt string
			if compactMode {
				userPrompt = "\033[32m• \033[0m"
			} else {
				userPrompt = fmt.Sprintf("\033[42m\033[30m %s> \033[0m ", userName)
			}

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
					_, _ = fmt.Fprintln(writer, "\nBye! 👋")
					return nil
				case "/forget":
					if chatMemory != nil {
						chatMemory.Body = ""
						if err := chatMemory.Save(); err != nil {
							_, _ = fmt.Fprintf(writer, "error saving memory: %v\n", err)
						} else {
							_, _ = fmt.Fprintln(writer, "Memory cleared.")
						}
					} else {
						_, _ = fmt.Fprintln(writer, "No memory to clear.")
					}
					continue
				case "/memory":
					if chatMemory != nil && chatMemory.Body != "" {
						_, _ = fmt.Fprintln(writer, "\n--- Project Memory ---")
						_, _ = fmt.Fprintln(writer, chatMemory.Body)
						_, _ = fmt.Fprintln(writer, "----------------------")
					} else {
						_, _ = fmt.Fprintln(writer, "No project memory yet.")
					}
					continue
				case "/help":
					_, _ = fmt.Fprint(writer, "\n\033[1m\033[36m=== MisterMorph Chat Commands ===\033[0m\n\n")
					_, _ = fmt.Fprintln(writer, "\033[33mGeneral\033[0m")
					_, _ = fmt.Fprintln(writer, "  /exit, /quit          Exit the chat session")
					_, _ = fmt.Fprintln(writer, "  /help                 Show this help message")
					_, _ = fmt.Fprintln(writer)
					_, _ = fmt.Fprintln(writer, "\033[33mProject Memory\033[0m")
					_, _ = fmt.Fprintln(writer, "  /remember <content>   Add an entry to project memory")
					_, _ = fmt.Fprintln(writer, "  /memory               View current project memory")
					_, _ = fmt.Fprintln(writer, "  /forget               Clear all project memory")
					_, _ = fmt.Fprintln(writer)
					_, _ = fmt.Fprintln(writer, "\033[33mProject Context\033[0m")
					_, _ = fmt.Fprintln(writer, "  /init                 Read AGENTS.md from current directory")
					_, _ = fmt.Fprintln(writer, "  /update               Regenerate AGENTS.md via AI")
					_, _ = fmt.Fprintln(writer)
					_, _ = fmt.Fprintln(writer, "\033[33mModel\033[0m")
					_, _ = fmt.Fprintln(writer, "  /model                Show current model selection state")
					_, _ = fmt.Fprintln(writer, "  /model list           List all available LLM profiles")
					_, _ = fmt.Fprintln(writer, "  /model set <profile>  Switch to specified profile")
					_, _ = fmt.Fprintln(writer, "  /model reset          Reset to automatic route selection")
					_, _ = fmt.Fprintln(writer)
					_, _ = fmt.Fprintln(writer, "\033[33mShortcuts\033[0m")
					_, _ = fmt.Fprintln(writer, "  Tab                   Command auto-completion")
					_, _ = fmt.Fprintln(writer, "  Ctrl+C                Interrupt current turn / clear input line")
					_, _ = fmt.Fprintln(writer, "  ↑ / ↓                 Browse input history")
					_, _ = fmt.Fprintln(writer)
					_, _ = fmt.Fprintln(writer, "\033[90mTip: Type any text to chat with the assistant.\033[0m")
					continue
				case "/init":
					agentsPath := filepath.Join(chatFileCacheDir, "AGENTS.md")
					if _, err := os.Stat(agentsPath); err == nil {
						data, err := os.ReadFile(agentsPath)
						if err != nil {
							_, _ = fmt.Fprintf(writer, "Error reading AGENTS.md: %v\n", err)
						} else {
							_, _ = fmt.Fprintln(writer, "\n--- AGENTS.md ---")
							_, _ = fmt.Fprintln(writer, string(data))
							_, _ = fmt.Fprintln(writer, "-----------------")
						}
						continue
					}
					// Does not exist — generate via AI (fall through)
					fallthrough
				case "/update":
					agentsPath := filepath.Join(chatFileCacheDir, "AGENTS.md")
					isUpdate := strings.ToLower(input) == "/update"
					if isUpdate {
						_, _ = fmt.Fprintln(writer, "\033[33m⚙️  Regenerating AGENTS.md...\033[0m")
					}
					stopInitAnim, _ := thinkingAnimation(writer)
					initCtx, initCancel := context.WithCancel(context.Background())
					go func() {
						<-time.After(timeout)
						initCancel()
					}()
					sigCh := make(chan os.Signal, 1)
					signal.Notify(sigCh, os.Interrupt)
					go func() {
						select {
						case <-sigCh:
							initCancel()
						case <-initCtx.Done():
						}
						signal.Stop(sigCh)
					}()
					initPrompt := fmt.Sprintf(`Please analyze the project in directory %q and generate an AGENTS.md file.

AGENTS.md is a project-level guide for AI coding assistants. It should contain:

1. **Project Overview** — what this project does, its purpose, tech stack
2. **Directory Structure** — key directories and their purposes
3. **Build & Development** — how to build, test, run
4. **Coding Conventions** — naming, formatting, architecture patterns
5. **Key Dependencies** — major libraries/frameworks
6. **Special Notes** — anything AI assistants should know (env vars, config files, gotchas)

Use bash and read_file tools to explore the project structure, README, go.mod, package.json, Makefile, etc. to gather accurate information.

IMPORTANT: Do NOT use the write_file tool. Instead, write the final AGENTS.md content directly as your response text. Use markdown format. Be concise but thorough.`, chatFileCacheDir)
					final, _, err := engine.Run(initCtx, initPrompt, agent.RunOptions{
						Model:   strings.TrimSpace(mainCfg.Model),
						Scene:   "chat.init",
						History: append([]llm.Message(nil), history...),
					})
					stopInitAnim()
					initCancel()
					if err != nil {
						if errors.Is(err, context.Canceled) {
							_, _ = fmt.Fprintln(writer, "\n\033[33m⚡ Interrupted.\033[0m")
							continue
						}
						_, _ = fmt.Fprintf(writer, "Error generating AGENTS.md: %v\n", err)
						continue
					}
					content := formatChatOutput(final)
					if content == "" {
						_, _ = fmt.Fprintln(writer, "AI returned empty content. AGENTS.md not created.")
						continue
					}
					content = stripMarkdownFences(content)
					if err := os.WriteFile(agentsPath, []byte(content), 0o644); err != nil {
						_, _ = fmt.Fprintf(writer, "Error writing AGENTS.md: %v\n", err)
						continue
					}
					if isUpdate {
						_, _ = fmt.Fprintf(writer, "\033[32m✓ AGENTS.md updated at %s\033[0m\n", agentsPath)
					} else {
						_, _ = fmt.Fprintf(writer, "\033[32m✓ AGENTS.md created at %s\033[0m\n", agentsPath)
					}
					_, _ = fmt.Fprintln(writer, "\n--- AGENTS.md ---")
					_, _ = fmt.Fprintln(writer, content)
					_, _ = fmt.Fprintln(writer, "-----------------")
					history = append(history, llm.Message{Role: "user", Content: fmt.Sprintf("I have initialized this project. Here is the AGENTS.md for this project:\n\n%s", content)})
					history = append(history, llm.Message{Role: "assistant", Content: "Got it. I've read the AGENTS.md and understand this project's structure, conventions, and guidelines. I'm ready to help."})
					continue
				}

				// Handle /model commands
				if strings.HasPrefix(strings.ToLower(input), "/model") {
					output, handled, err := llmselect.ExecuteCommandText(llmValues, sessionStore, input)
					if !handled {
						continue
					}
					if err != nil {
						_, _ = fmt.Fprintf(writer, "error: %v\n", err)
						continue
					}
					_, _ = fmt.Fprintln(writer, output)
					// If the selection changed, rebuild the client and engine
					sel := sessionStore.Get()
					if sel.Mode == llmselect.ModeManual {
						newRoute, err := llmselect.ResolveMainRoute(llmValues, sel)
						if err != nil {
							_, _ = fmt.Fprintf(writer, "error resolving route: %v\n", err)
							continue
						}
						newCfg := newRoute.ClientConfig
						newClient, err := buildClient(newRoute, &newCfg)
						if err != nil {
							_, _ = fmt.Fprintf(writer, "error rebuilding client: %v\n", err)
							continue
						}
						client = newClient
						mainCfg = newCfg
						engine = makeEngine(client, mainCfg.Model)
						_, _ = fmt.Fprintf(writer, "\033[90m[active model: %s]\033[0m\n", mainCfg.Model)
					}
					continue
				}

				// Handle /remember <content> command
				if strings.HasPrefix(strings.ToLower(input), "/remember ") {
					entry := strings.TrimSpace(input[len("/remember "):])
					if entry == "" {
						_, _ = fmt.Fprintln(writer, "Usage: /remember <content>")
						continue
					}
					if chatMemory == nil {
						chatMemory = &climemory.Memory{
							AbsPath: climemory.MemoryFilePath(chatFileCacheDir),
						}
					}
					chatMemory.AppendEntry(entry)
					chatMemory.TruncateIfNeeded()
					if err := chatMemory.Save(); err != nil {
						_, _ = fmt.Fprintf(writer, "error saving memory: %v\n", err)
					} else {
						_, _ = fmt.Fprintln(writer, "Remembered.")
					}
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
				if len(runCtx.Steps) > 0 {
					summary := buildTurnSummary(input, output, runCtx.Steps)
					if summary != "" {
						if chatMemory == nil {
							chatMemory = &climemory.Memory{
								AbsPath: climemory.MemoryFilePath(chatFileCacheDir),
							}
						}
						chatMemory.AppendEntry(summary)
						chatMemory.TruncateIfNeeded()
						if err := chatMemory.Save(); err != nil {
							logger.Warn("chat_memory_save_failed", "error", err)
						} else {
							logger.Debug("chat_memory_updated", "summary", summary)
						}
					}
				}

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

// thinkingAnimation runs a spinner animation while waiting for the LLM.
func thinkingAnimation(writer io.Writer) (stop func(), setMessage func(msg string)) {
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(80 * time.Millisecond)
	done := make(chan struct{})
	msgMu := sync.RWMutex{}
	msg := "assistant is thinking..."
	var wg sync.WaitGroup

	var lastLinesMu sync.Mutex
	lastLines := 1

	calcLines := func(text string) int {
		width, _, _ := term.GetSize(int(os.Stdout.Fd()))
		if width <= 0 {
			width = 80
		}
		prefixWidth := 2 // spinner icon (1) + space (1)
		totalWidth := prefixWidth + stringDisplayWidth(text)
		lines := totalWidth / width
		if totalWidth%width != 0 {
			lines++
		}
		if lines < 1 {
			lines = 1
		}
		return lines
	}

	buildClearSeq := func(n int) string {
		if n <= 1 {
			return "\r\033[K"
		}
		var b strings.Builder
		for i := 1; i < n; i++ {
			b.WriteString("\033[A")
		}
		b.WriteString("\r")
		for i := 0; i < n; i++ {
			b.WriteString("\033[2K")
			if i < n-1 {
				b.WriteString("\033[B")
			}
		}
		for i := 1; i < n; i++ {
			b.WriteString("\033[A")
		}
		b.WriteString("\r")
		return b.String()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-ticker.C:
				msgMu.RLock()
				currentMsg := msg
				msgMu.RUnlock()

				lastLinesMu.Lock()
				prevLines := lastLines
				lastLines = calcLines(currentMsg)
				lastLinesMu.Unlock()

				clearSeq := buildClearSeq(prevLines)
				_, _ = fmt.Fprintf(writer, "%s\033[36m%s\033[0m \033[90m%s\033[0m", clearSeq, spinner[i%len(spinner)], currentMsg)
				i++
			case <-done:
				return
			}
		}
	}()
	stop = func() {
		close(done)
		ticker.Stop()
		wg.Wait()

		lastLinesMu.Lock()
		prevLines := lastLines
		lastLinesMu.Unlock()

		_, _ = fmt.Fprint(writer, buildClearSeq(prevLines))
	}
	setMessage = func(newMsg string) {
		msgMu.Lock()
		msg = truncateString(newMsg, 80)
		msgMu.Unlock()
	}
	return stop, setMessage
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func stringDisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeDisplayWidth(r)
	}
	return w
}

func runeDisplayWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r >= 0x1100 &&
		(r <= 0x115f || r == 0x2329 || r == 0x232a || (r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
			(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x20000 && r <= 0x2fffd) || (r >= 0x30000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func formatChatOutput(final *agent.Final) string {
	if final == nil {
		return ""
	}
	switch output := final.Output.(type) {
	case string:
		return strings.TrimSpace(output)
	case nil:
		payload, _ := json.MarshalIndent(final, "", "  ")
		return strings.TrimSpace(string(payload))
	default:
		payload, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(output))
		}
		return strings.TrimSpace(string(payload))
	}
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

	if update.CompletedIndex >= 0 && update.CompletedIndex == total-1 && update.StartedIndex < 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("plan: ")

	if update.CompletedIndex >= 0 && update.CompletedStep != "" {
		b.WriteString(fmt.Sprintf("✓ %s", update.CompletedStep))
	}

	if update.StartedIndex >= 0 && update.StartedStep != "" {
		if update.CompletedIndex >= 0 {
			b.WriteString(" → ")
		}
		b.WriteString(update.StartedStep)
	}

	if update.CompletedIndex >= 0 {
		b.WriteString(fmt.Sprintf(" [%d/%d]", update.CompletedIndex+1, total))
	} else if update.StartedIndex >= 0 {
		b.WriteString(fmt.Sprintf(" [%d/%d]", update.StartedIndex+1, total))
	}

	return b.String()
}

func stripMarkdownFences(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```markdown") {
		content = strings.TrimPrefix(content, "```markdown")
		content = strings.TrimSpace(content)
		if strings.HasSuffix(content, "```") {
			content = strings.TrimSuffix(content, "```")
			content = strings.TrimSpace(content)
		}
		return content
	}
	if strings.HasPrefix(content, "```") {
		idx := strings.Index(content, "\n")
		if idx > 0 {
			content = content[idx+1:]
		} else {
			content = strings.TrimPrefix(content, "```")
		}
		content = strings.TrimSpace(content)
		if strings.HasSuffix(content, "```") {
			content = strings.TrimSuffix(content, "```")
			content = strings.TrimSpace(content)
		}
		return content
	}
	return content
}

func printChatSessionHeader(writer io.Writer, model string, fileCacheDir string) {
	_, _ = fmt.Fprint(writer, chatBanner)
	if model != "" {
		_, _ = fmt.Fprintf(writer, "model=%s\n", model)
	}
	if fileCacheDir != "" {
		_, _ = fmt.Fprintf(writer, "file_cache_dir=%s\n", fileCacheDir)
	}
	_, _ = fmt.Fprintln(writer, "\033[90mInteractive chat started. Press Ctrl+C or type /exit to quit.\033[0m")
}

func buildTurnSummary(userInput, assistantOutput string, steps []agent.Step) string {
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		return ""
	}

	lower := strings.ToLower(userInput)
	if strings.HasPrefix(lower, "/remember") || strings.HasPrefix(lower, "/forget") || strings.HasPrefix(lower, "/memory") {
		return ""
	}

	var toolNames []string
	for _, step := range steps {
		if step.Action != "" {
			toolNames = append(toolNames, step.Action)
		}
	}

	if len(toolNames) == 0 {
		return ""
	}

	summary := userInput
	if len(toolNames) > 0 {
		summary += fmt.Sprintf(" (tools: %s)", strings.Join(toolNames, ", "))
	}

	const maxLen = 200
	if len(summary) > maxLen {
		summary = summary[:maxLen-3] + "..."
	}
	return summary
}
