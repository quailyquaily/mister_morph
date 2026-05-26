package chatcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
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
	imageClient      llm.ImageClient
	imageSession     *imagesession.Store
	imageScope       imagesession.Scope
	imageRetention   toolsutil.ImageToolRetention
	imageToolsActive bool
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
	launchDir        string
	fileCacheDir     string
	fileStateDir     string
	workspaceDir     string
	sessionStore     *llmselect.Store
	llmValues        llmutil.RuntimeValues
	buildClient      func(llmutil.ResolvedRoute, *llmconfig.ClientConfig) (llm.Client, error)
	makeEngine       func(*tools.Registry, llm.Client, string, map[string]bool) *agent.Engine
	basePromptSpec   agent.PromptSpec
	promptSpec       agent.PromptSpec
	loadedSkills     []string
	timeout          time.Duration
	writer           io.Writer
	sendMsg          func(msg any) // set in bubbletea mode to send messages to the TUI
	uiMu             sync.Mutex
	stopAnim         func()
	setAnimMessage   func(string)
	fileSnapshots    map[string]string // path -> content before write_file
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

func buildChatToolRegistry(deps Dependencies, toolTriggers map[string]bool) *tools.Registry {
	reg := tools.NewRegistry()
	if deps.RegistryFromViper == nil {
		return reg
	}
	reg = cloneToolRegistry(deps.RegistryFromViper())
	if deps.RegisterTriggeredStaticTools != nil {
		deps.RegisterTriggeredStaticTools(reg, toolTriggers)
	}
	return reg
}

func (s *chatSession) projectDir() string {
	if s == nil {
		return ""
	}
	if dir := strings.TrimSpace(s.workspaceDir); dir != "" {
		return dir
	}
	return strings.TrimSpace(s.launchDir)
}

func (s *chatSession) refreshProjectScope() {
	if s == nil {
		return
	}
	s.subjectID = cliMemorySubjectID(s.projectDir())
}

func (s *chatSession) rebuildPromptSpec() {
	if s == nil {
		return
	}
	spec := agent.PromptSpec{
		Identity: s.basePromptSpec.Identity,
		Rules:    append([]string(nil), s.basePromptSpec.Rules...),
		Skills:   append([]agent.PromptSkill(nil), s.basePromptSpec.Skills...),
		Blocks:   append([]agent.PromptBlock(nil), s.basePromptSpec.Blocks...),
	}
	blocks := make([]agent.PromptBlock, 0, len(spec.Blocks)+2)
	if workspaceDir := strings.TrimSpace(s.workspaceDir); workspaceDir != "" {
		if block := workspace.PromptBlock(workspaceDir); strings.TrimSpace(block.Content) != "" {
			blocks = append(blocks, block)
		}
	}
	if s.imageToolsActive {
		if block, err := s.imageSessionPromptBlock(); err == nil && strings.TrimSpace(block.Content) != "" {
			blocks = append(blocks, block)
		} else if err != nil && s.logger != nil {
			s.logger.Warn("image_session_prompt_failed", "error", err.Error())
		}
	}
	blocks = append(blocks, agent.PromptBlock{Content: chatBuiltinCommandsBlock()})
	blocks = append(blocks, spec.Blocks...)
	spec.Blocks = blocks
	s.promptSpec = spec
}

func (s *chatSession) imageSessionPromptBlock() (agent.PromptBlock, error) {
	if s == nil || s.imageSession == nil || s.imageScope.Empty() {
		return agent.PromptBlock{}, nil
	}
	roots := pathroots.New(s.workspaceDir, s.fileCacheDir, s.fileStateDir)
	return s.imageSession.PromptBlock(s.imageScope, roots, 3)
}

func (s *chatSession) activeImageAvailable() bool {
	if s == nil || s.imageSession == nil || s.imageScope.Empty() {
		return false
	}
	roots := pathroots.New(s.workspaceDir, s.fileCacheDir, s.fileStateDir)
	active, err := s.imageSession.Active(s.imageScope, roots)
	if err != nil {
		if s.logger != nil && !errors.Is(err, imagesession.ErrActiveImageMissing) {
			s.logger.Warn("image_session_active_check_failed", "error", err.Error())
		}
		return false
	}
	return active != nil && strings.TrimSpace(active.Path) != ""
}

func (s *chatSession) rebuildRuntimeState() error {
	return s.rebuildRuntimeStateForTask("")
}

func (s *chatSession) rebuildRuntimeStateForTask(task string) error {
	currentRoute, err := llmselect.ResolveMainRoute(s.llmValues, s.sessionStore.Get())
	if err != nil {
		return err
	}

	skillsCfg := skillsutil.SkillsConfigFromRunCmd(s.cmd)
	activeImage := s.activeImageAvailable()
	toolTriggers := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, skillsCfg))
	toolTriggers = toolsutil.AddImageToolIntentTriggers(toolTriggers, task, activeImage)
	if len(acpclient.AgentsFromViper()) == 0 {
		delete(toolTriggers, toolsutil.BuiltinACPSpawn)
	}
	reg := buildChatToolRegistry(s.deps, toolTriggers)

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

	imageValues := llmutil.RuntimeValuesWithClientConfig(currentRoute.Values, s.mainCfg)
	if s.cmd != nil && s.cmd.Flags().Changed("llm-request-timeout") && s.mainCfg.RequestTimeout > 0 {
		imageValues.ImageTimeoutRaw = s.mainCfg.RequestTimeout.String()
	}
	runtimeToolsCfg := s.runtimeToolsCfg
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
	if (toolTriggers[toolsutil.BuiltinImageGenerate] ||
		toolTriggers[toolsutil.BuiltinImageEdit]) &&
		s.imageSession == nil {
		s.imageSession = imagesession.NewStore(s.fileStateDir)
		if s.imageScope.Empty() {
			s.imageScope = imagesession.NewScope("chat:" + strings.ReplaceAll(uuid.NewString(), "-", ""))
		}
	}
	imageToolTriggered := toolTriggers[toolsutil.BuiltinImageGenerate] || toolTriggers[toolsutil.BuiltinImageEdit]
	imageRetained := s.imageRetention.Resolve(toolsutil.ImageToolRetentionSticky, imageToolTriggered)
	s.imageToolsActive = imageRetained
	imageClient := s.imageClient
	if runtimeToolsCfg.Image.Configured && (imageRetained || imageToolTriggered) && imageClient == nil {
		built, imageErr := llmutil.ImageClientFromValuesWithStats(imageValues, s.logger)
		if imageErr != nil {
			if s.logger != nil {
				s.logger.Warn("image_client_create_failed", "error", imageErr.Error())
			}
		} else {
			imageClient = built
			s.imageClient = built
		}
	}
	toolsutil.RegisterRuntimeTools(reg, runtimeToolsCfg, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    s.client,
		DefaultModel:     strings.TrimSpace(s.mainCfg.Model),
		PlanCreateClient: planClient,
		PlanCreateModel:  strings.TrimSpace(planRoute.ClientConfig.Model),
		ImageClient:      imageClient,
		ImageSession:     s.imageSession,
		ImageScope:       s.imageScope,
		ImageRetained:    imageRetained,
		ToolTriggers:     toolTriggers,
	})

	s.rebuildPromptSpec()
	s.toolRegistry = reg
	s.engine = s.makeEngine(reg, s.client, s.mainCfg.Model, toolTriggers)
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
	if s.sendMsg != nil {
		s.sendMsg(thinkingMsg{on: true})
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
	if s.sendMsg != nil {
		s.sendMsg(thinkingMsg{on: false})
	}
	if stopAnim != nil {
		stopAnim()
	}
}

func (s *chatSession) setThinkingMessage(msg string) {
	if s == nil {
		return
	}
	if s.sendMsg != nil {
		s.sendMsg(thinkingMsg{on: true, message: msg})
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

	launchDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	launchDir = pathroots.New(launchDir, "", "").WorkspaceDir
	fileStateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	if fileStateDir == "" {
		fileStateDir = statepaths.FileStateDir()
	}
	fileCacheDir := strings.TrimSpace(viper.GetString("file_cache_dir"))
	rawWorkspace, _ := cmd.Flags().GetString("workspace")
	noWorkspace, _ := cmd.Flags().GetBool("no-workspace")
	workspaceDir, err := workspace.ResolveInitialWorkspace(launchDir, rawWorkspace, noWorkspace, nil)
	if err != nil {
		return nil, err
	}

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
				return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, cfg.ContextWindowTokens, logger)
			},
			logger,
		)
	}

	client, err := buildClient(mainRoute, &mainCfg)
	if err != nil {
		return nil, err
	}

	reg := buildChatToolRegistry(deps, nil)
	runtimeToolsCfg := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
	imageValues := llmutil.RuntimeValuesWithClientConfig(mainRoute.Values, mainCfg)
	if cmd.Flags().Changed("llm-request-timeout") && mainCfg.RequestTimeout > 0 {
		imageValues.ImageTimeoutRaw = mainCfg.RequestTimeout.String()
	}
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
	var imageClient llm.ImageClient
	var imageSession *imagesession.Store
	imageScope := imagesession.Scope{}
	if runtimeToolsCfg.Image.GenerateEnabled || runtimeToolsCfg.Image.EditEnabled {
		imageSession = imagesession.NewStore(fileStateDir)
		imageScope = imagesession.NewScope("chat:" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	}

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
				return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, cfg.ContextWindowTokens, logger)
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
		ImageClient:      imageClient,
		ImageSession:     imageSession,
		ImageScope:       imageScope,
	})

	// Use a long-lived context for the memory projection worker so it survives
	// beyond buildChatSession(). The worker is stopped when the REPL exits via
	// sess.cleanup() which cancels this context.
	workerCtx, workerCancel := context.WithCancel(context.Background())

	promptCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	promptSpec, loadedSkills, err := skillsutil.PromptSpecWithSkills(promptCtx, logger, logOpts, "Interactive chat session", client, strings.TrimSpace(mainCfg.Model), skillsutil.SkillsConfigFromRunCmd(cmd))
	if err != nil {
		return nil, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	promptprofile.AppendGPT5PromptPatch(&promptSpec, strings.TrimSpace(mainCfg.Model), logger)

	// Initialize memory runtime
	projectDir := strings.TrimSpace(workspaceDir)
	if projectDir == "" {
		projectDir = launchDir
	}
	subjectID := cliMemorySubjectID(projectDir)
	memManager, memOrchestrator, memWorker, memCleanup, err := initChatMemoryRuntime(projectDir, logger)
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

	var sess *chatSession

	// Add tool start callback to show what tools are being used
	opts = append(opts, agent.WithOnToolStart(func(runCtx *agent.Context, toolName string) {
		if sess == nil {
			return
		}
		writer := sess.currentWriter()
		msg := fmt.Sprintf("\x1b[38;5;245m  used %s\x1b[0m", toolName)
		_, _ = fmt.Fprintf(writer, "\r\033[K%s\n", msg)
	}))
	opts = append(opts, agent.WithPlanStepUpdate(func(runCtx *agent.Context, update agent.PlanStepUpdate) {
		if sess == nil {
			return
		}
		logger.Debug("plan_step_update_callback", "completedIndex", update.CompletedIndex, "startedIndex", update.StartedIndex, "startedStep", update.StartedStep, "reason", update.Reason)
		if update.StartedIndex >= 0 && update.StartedStep != "" {
			// Step started: stop spinner, print plan text safely, then restart.
			sess.stopThinkingAnimation()
			writer := sess.currentWriter()
			total := 0
			if runCtx != nil && runCtx.Plan != nil {
				total = len(runCtx.Plan.Steps)
			}
			_, _ = fmt.Fprintf(writer, "\033[38;5;245m → plan: %s", update.StartedStep)
			if total > 0 {
				_, _ = fmt.Fprintf(writer, " [%d/%d]", update.StartedIndex+1, total)
			}
			_, _ = fmt.Fprint(writer, "\033[0m\n")
			sess.startThinkingAnimation()
		} else if update.CompletedIndex >= 0 && update.CompletedStep != "" {
			sess.stopThinkingAnimation()
			writer := sess.currentWriter()
			total := 0
			if runCtx != nil && runCtx.Plan != nil {
				total = len(runCtx.Plan.Steps)
			}
			_, _ = fmt.Fprintf(writer, "\033[38;5;245m → plan: ✓ %s", update.CompletedStep)
			if total > 0 {
				_, _ = fmt.Fprintf(writer, " [%d/%d]", update.CompletedIndex+1, total)
			}
			_, _ = fmt.Fprint(writer, "\033[0m\n")
			sess.startThinkingAnimation()
		} else {
			sess.setThinkingMessage("assistant is thinking...")
		}
	}))

	// Capture old file content before write_file executes so we can render diffs.
	opts = append(opts, agent.WithOnToolCallStart(func(runCtx *agent.Context, tc agent.ToolCall) {
		if sess == nil {
			return
		}
		if tc.Name == "write_file" {
			path, _ := tc.Params["path"].(string)
			path = sess.resolveWritePath(path)
			if path == "" {
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return // File doesn't exist yet (new file) – nothing to diff.
			}
			sess.fileSnapshots[path] = string(data)
		}
	}))

	// Render diff after write_file completes successfully.
	opts = append(opts, agent.WithOnToolCallDone(func(runCtx *agent.Context, tc agent.ToolCall, observation string, err error) {
		if sess == nil || err != nil {
			return
		}
		if tc.Name == "write_file" {
			path, _ := tc.Params["path"].(string)
			resolvedPath := sess.resolveWritePath(path)
			if resolvedPath == "" {
				return
			}
			oldContent, hadOld := sess.fileSnapshots[resolvedPath]
			delete(sess.fileSnapshots, resolvedPath)
			writer := sess.currentWriter()
			if !hadOld {
				// New file — show the full content as a diff from empty.
				newData, readErr := os.ReadFile(resolvedPath)
				if readErr != nil {
					return
				}
				diff := clifmt.RenderDiff(resolvedPath, "", string(newData))
				if diff != "" {
					sess.stopThinkingAnimation()
					_, _ = fmt.Fprintln(writer, diff)
					sess.startThinkingAnimation()
				}
				return
			}
			newData, readErr := os.ReadFile(resolvedPath)
			if readErr != nil {
				return
			}
			newContent := string(newData)
			if oldContent == newContent {
				return // No change.
			}
			diff := clifmt.RenderDiff(resolvedPath, oldContent, newContent)
			if diff != "" {
				sess.stopThinkingAnimation()
				_, _ = fmt.Fprintln(writer, diff)
				sess.startThinkingAnimation()
			}
		}
	}))

	makeEngine := func(engReg *tools.Registry, engClient llm.Client, defaultModel string, toolTriggers map[string]bool) *agent.Engine {
		currentPromptSpec := promptSpec
		if sess != nil {
			currentPromptSpec = sess.promptSpec
		}
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
			currentPromptSpec,
			append(opts,
				agent.WithEngineToolsConfig(agent.EngineToolsConfig{
					SpawnEnabled:    viper.GetBool("tools.spawn.enabled"),
					ACPSpawnEnabled: viper.GetBool("tools.acp_spawn.enabled"),
					ToolTriggers:    toolTriggers,
				}),
				agent.WithACPAgents(acpclient.AgentsFromViper()),
			)...,
		)
	}
	engine := makeEngine(reg, client, mainCfg.Model, nil)

	sess = &chatSession{
		cmd:             cmd,
		deps:            deps,
		logger:          logger,
		logOpts:         logOpts,
		client:          client,
		imageClient:     imageClient,
		imageSession:    imageSession,
		imageScope:      imageScope,
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
		subjectID:      subjectID,
		compactMode:    compactMode,
		userName:       userName,
		sessionStore:   sessionStore,
		llmValues:      llmValues,
		buildClient:    buildClient,
		makeEngine:     makeEngine,
		launchDir:      launchDir,
		fileCacheDir:   fileCacheDir,
		fileStateDir:   fileStateDir,
		workspaceDir:   workspaceDir,
		basePromptSpec: promptSpec,
		promptSpec:     promptSpec,
		loadedSkills:   loadedSkills,
		timeout:        timeout,
		fileSnapshots:  make(map[string]string),
	}
	sess.rebuildPromptSpec()
	sess.engine = sess.makeEngine(sess.toolRegistry, sess.client, sess.mainCfg.Model, nil)

	return sess, nil
}

// resolveWritePath resolves a write_file path to an absolute path,
// matching the behavior of tools/builtin/write_file.go.
func (s *chatSession) resolveWritePath(userPath string) string {
	roots := pathroots.New(s.workspaceDir, s.fileCacheDir, s.fileStateDir)
	if strings.TrimSpace(roots.FileCacheDir) == "" && strings.TrimSpace(roots.FileStateDir) == "" && strings.TrimSpace(roots.WorkspaceDir) == "" {
		return ""
	}

	userPath = pathutil.ExpandHomePath(userPath)
	userPath = strings.TrimSpace(userPath)
	if userPath == "" {
		return ""
	}

	// Alias handling: workspace_dir/..., file_cache_dir/..., file_state_dir/...
	trimmed := strings.TrimLeft(userPath, "/\\")
	lower := strings.ToLower(trimmed)
	prefixes := []struct {
		alias  string
		prefix string
	}{
		{"workspace_dir", "workspace_dir/"}, {"workspace_dir", "workspace_dir\\"},
		{"file_cache_dir", "file_cache_dir/"}, {"file_cache_dir", "file_cache_dir\\"},
		{"file_state_dir", "file_state_dir/"}, {"file_state_dir", "file_state_dir\\"},
	}
	switch lower {
	case "workspace_dir", "file_cache_dir", "file_state_dir":
		return ""
	}
	for _, p := range prefixes {
		if !strings.HasPrefix(lower, p.prefix) {
			continue
		}
		base := strings.TrimSpace(roots.BaseDir(p.alias))
		if base == "" {
			return ""
		}
		baseAbs, _ := filepath.Abs(base)
		rest := strings.TrimLeft(trimmed[len(p.prefix):], "/\\")
		if rest == "" {
			return ""
		}
		cand := filepath.Join(baseAbs, rest)
		candAbs, _ := filepath.Abs(cand)
		if !pathutil.IsWithinDir(baseAbs, candAbs) {
			return ""
		}
		return candAbs
	}

	// Absolute path — must be within allowed base dirs.
	if filepath.IsAbs(userPath) {
		candAbs, err := filepath.Abs(filepath.Clean(userPath))
		if err != nil {
			return ""
		}
		for _, base := range roots.AllowedBaseDirs() {
			baseAbs, err := filepath.Abs(base)
			if err != nil {
				continue
			}
			if pathutil.IsWithinDir(baseAbs, candAbs) || filepath.Clean(baseAbs) == filepath.Clean(candAbs) {
				return candAbs
			}
		}
		return ""
	}

	// Relative path — resolved under the default base dir.
	defaultBase := strings.TrimSpace(roots.DefaultFileDir())
	if defaultBase == "" {
		return ""
	}
	baseAbs, _ := filepath.Abs(defaultBase)
	userPath = strings.TrimLeft(strings.TrimSpace(userPath), "/\\")
	if userPath == "" {
		return ""
	}
	cand := filepath.Join(baseAbs, userPath)
	candAbs, _ := filepath.Abs(cand)
	if !pathutil.IsWithinDir(baseAbs, candAbs) {
		return ""
	}
	return candAbs
}

func (s *chatSession) cleanup() {
	if s.memCleanup != nil {
		s.memCleanup()
	}
}
