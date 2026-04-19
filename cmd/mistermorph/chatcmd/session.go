package chatcmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
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

type chatSession struct {
	cmd              *cobra.Command
	deps             Dependencies
	logger           *slog.Logger
	logOpts          agent.LogOptions
	client           llm.Client
	mainCfg          llmconfig.ClientConfig
	engine           *agent.Engine
	toolRegistry     *tools.Registry
	runtimeToolsCfg  toolsutil.RuntimeToolsRegisterConfig
	memManager       *memory.Manager
	memOrchestrator  *memoryruntime.Orchestrator
	memWorker        *memoryruntime.ProjectionWorker
	memCleanup       func()
	subjectID        string
	compactMode      bool
	userName         string
	agentName        string
	chatFileCacheDir string
	sessionStore     *llmselect.Store
	llmValues        llmutil.RuntimeValues
	buildClient      func(llmutil.ResolvedRoute, *llmconfig.ClientConfig) (llm.Client, error)
	makeEngine       func(*tools.Registry, llm.Client, string) *agent.Engine
	promptSpec       agent.PromptSpec
	timeout          time.Duration
	writer           io.Writer
	uiMu             sync.Mutex
	stopAnim         func()
	setAnimMessage   func(string)
}

func cloneToolRegistry(base *tools.Registry) *tools.Registry {
	reg := tools.NewRegistry()
	if base == nil {
		return reg
	}
	for _, t := range base.All() {
		reg.Register(t)
	}
	return reg
}

func buildChatToolRegistry(deps Dependencies) *tools.Registry {
	if deps.RegistryFromViper == nil {
		return tools.NewRegistry()
	}
	return cloneToolRegistry(deps.RegistryFromViper())
}

func (s *chatSession) rebuildRuntimeState() error {
	currentRoute, err := llmselect.ResolveMainRoute(s.llmValues, s.sessionStore.Get())
	if err != nil {
		return err
	}

	reg := buildChatToolRegistry(s.deps)

	planRoute, err := llmutil.ResolveRoute(s.llmValues, llmutil.RoutePurposePlanCreate)
	if err != nil {
		return err
	}
	planClient := s.client
	if !planRoute.SameProfile(currentRoute) {
		planClient, err = s.buildClient(planRoute, nil)
		if err != nil {
			return err
		}
	}

	toolsutil.RegisterRuntimeTools(reg, s.runtimeToolsCfg, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    s.client,
		DefaultModel:     strings.TrimSpace(s.mainCfg.Model),
		PlanCreateClient: planClient,
		PlanCreateModel:  strings.TrimSpace(planRoute.ClientConfig.Model),
	})

	s.toolRegistry = reg
	s.engine = s.makeEngine(reg, s.client, s.mainCfg.Model)
	return nil
}

func (s *chatSession) setWriter(writer io.Writer) {
	if s == nil {
		return
	}
	s.uiMu.Lock()
	s.writer = writer
	s.uiMu.Unlock()
}

func (s *chatSession) currentWriter() io.Writer {
	if s == nil {
		return io.Discard
	}
	s.uiMu.Lock()
	writer := s.writer
	cmd := s.cmd
	s.uiMu.Unlock()
	if writer != nil {
		return writer
	}
	if cmd != nil {
		return cmd.OutOrStdout()
	}
	return io.Discard
}

func (s *chatSession) startThinkingAnimation() {
	if s == nil {
		return
	}
	writer := s.currentWriter()
	stopAnim, setAnimMessage := thinkingAnimation(writer)
	s.uiMu.Lock()
	s.stopAnim = stopAnim
	s.setAnimMessage = setAnimMessage
	s.uiMu.Unlock()
}

func (s *chatSession) stopThinkingAnimation() {
	if s == nil {
		return
	}
	s.uiMu.Lock()
	stopAnim := s.stopAnim
	s.stopAnim = nil
	s.setAnimMessage = nil
	s.uiMu.Unlock()
	if stopAnim != nil {
		stopAnim()
	}
}

func (s *chatSession) setThinkingMessage(msg string) {
	if s == nil {
		return
	}
	s.uiMu.Lock()
	setAnimMessage := s.setAnimMessage
	s.uiMu.Unlock()
	if setAnimMessage != nil {
		setAnimMessage(msg)
	}
}

func buildChatSession(cmd *cobra.Command, deps Dependencies) (*chatSession, error) {
	timeout := configutil.FlagOrViperDuration(cmd, "timeout", "timeout")

	verbose, _ := cmd.Flags().GetBool("verbose")
	loggerCfg := logutil.LoggerConfigFromViper()
	if !verbose {
		loggerCfg.Level = "error"
	}
	logger, err := logutil.LoggerFromConfig(loggerCfg)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	logOpts := logutil.LogOptionsFromViper()

	chatFileCacheDir, err := resolveChatFileCacheDir()
	if err != nil {
		return nil, err
	}
	viper.Set("file_cache_dir", chatFileCacheDir)

	llmValues := llmutil.RuntimeValuesFromViper()
	mainRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return nil, err
	}

	// Support --profile flag to use a named LLM profile from config
	if cmd.Flags().Changed("profile") {
		profileName, _ := cmd.Flags().GetString("profile")
		profileName = strings.TrimSpace(profileName)
		if profileName != "" {
			mainRoute, err = llmutil.ResolveRouteWithProfileOverride(llmValues, llmutil.RoutePurposeMainLoop, profileName)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve profile %q: %w", profileName, err)
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
		return nil, err
	}

	reg := buildChatToolRegistry(deps)
	runtimeToolsCfg := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()

	planClient := client
	planModel := strings.TrimSpace(mainCfg.Model)
	planRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposePlanCreate)
	if err != nil {
		return nil, err
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
			return nil, err
		}
	}
	planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
	toolsutil.RegisterRuntimeTools(reg, runtimeToolsCfg, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    client,
		DefaultModel:     strings.TrimSpace(mainCfg.Model),
		PlanCreateClient: planClient,
		PlanCreateModel:  planModel,
	})

	// Use a long-lived context for the memory projection worker so it survives
	// beyond buildChatSession(). The worker is stopped when the REPL exits via
	// sess.cleanup() which cancels this context.
	workerCtx, workerCancel := context.WithCancel(context.Background())

	promptCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	promptSpec, _, err := skillsutil.PromptSpecWithSkills(promptCtx, logger, logOpts, "Interactive chat session", client, strings.TrimSpace(mainCfg.Model), skillsutil.SkillsConfigFromRunCmd(cmd))
	if err != nil {
		return nil, err
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
			"- `/reset` — reset the current conversation (clear history, keep memory)\n"+
			"- `/memory` — display the current project memory\n"+
			"- `/remember <content>` — add a long-term memory item for the current project\n"+
			"- `/init` — generate an AGENTS.md file for the current project (analyzes the codebase and creates a guide for AI assistants)\n"+
			"- `/update` — regenerate AGENTS.md, overwriting the existing file (useful after major project changes)\n"+
			"If the user asks about any of these commands, explain what they do.", chatFileCacheDir),
	}}, promptSpec.Blocks...)

	// Initialize memory runtime
	subjectID := cliMemorySubjectID(chatFileCacheDir)
	memManager, memOrchestrator, memWorker, memCleanup, err := initChatMemoryRuntime(chatFileCacheDir, logger)
	if err != nil {
		logger.Warn("chat_memory_init_failed", "error", err.Error())
	}
	if memWorker != nil {
		memWorker.Start(workerCtx)
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

	var sess *chatSession

	// Add tool start callback to show what tools are being used
	opts = append(opts, agent.WithOnToolStart(func(runCtx *agent.Context, toolName string) {
		if sess == nil {
			return
		}
		writer := sess.currentWriter()
		msg := fmt.Sprintf("\x1b[90m  used \x1b[36m%s\x1b[0m", toolName)
		_, _ = fmt.Fprintf(writer, "\r\033[K%s\n", msg)
	}))
	opts = append(opts, agent.WithPlanStepUpdate(func(runCtx *agent.Context, update agent.PlanStepUpdate) {
		if sess == nil {
			return
		}
		logger.Debug("plan_step_update_callback", "completedIndex", update.CompletedIndex, "startedIndex", update.StartedIndex, "startedStep", update.StartedStep, "reason", update.Reason)
		payload := formatPlanProgressUpdate(runCtx, update)
		if payload != "" {
			sess.setThinkingMessage(payload)
		} else if update.CompletedIndex >= 0 && update.CompletedStep != "" {
			sess.stopThinkingAnimation()
			writer := sess.currentWriter()
			total := 0
			if runCtx != nil && runCtx.Plan != nil {
				total = len(runCtx.Plan.Steps)
			}
			_, _ = fmt.Fprintf(writer, "\033[90mplan: ✓ %s", update.CompletedStep)
			if total > 0 {
				_, _ = fmt.Fprintf(writer, " [%d/%d]", update.CompletedIndex+1, total)
			}
			_, _ = fmt.Fprint(writer, "\033[0m\n")
			sess.startThinkingAnimation()
		} else {
			sess.setThinkingMessage("assistant is thinking...")
		}
	}))

	makeEngine := func(engReg *tools.Registry, engClient llm.Client, defaultModel string) *agent.Engine {
		return agent.New(
			engClient,
			engReg,
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
	engine := makeEngine(reg, client, mainCfg.Model)

	sess = &chatSession{
		cmd:             cmd,
		deps:            deps,
		logger:          logger,
		logOpts:         logOpts,
		client:          client,
		mainCfg:         mainCfg,
		engine:          engine,
		toolRegistry:    reg,
		runtimeToolsCfg: runtimeToolsCfg,
		memManager:      memManager,
		memOrchestrator: memOrchestrator,
		memWorker:       memWorker,
		memCleanup: func() {
			workerCancel()
			if memCleanup != nil {
				memCleanup()
			}
		},
		subjectID:        subjectID,
		compactMode:      compactMode,
		userName:         userName,
		agentName:        agentName,
		chatFileCacheDir: chatFileCacheDir,
		sessionStore:     sessionStore,
		llmValues:        llmValues,
		buildClient:      buildClient,
		makeEngine:       makeEngine,
		promptSpec:       promptSpec,
		timeout:          timeout,
	}

	return sess, nil
}

func (s *chatSession) cleanup() {
	if s.memCleanup != nil {
		s.memCleanup()
	}
}
