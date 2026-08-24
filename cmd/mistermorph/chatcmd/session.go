package chatcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
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

type chatSession struct {
	version                string
	cmd                    *cobra.Command
	rootContext            context.Context
	logger                 *slog.Logger
	taskRuntime            *taskruntime.Runtime
	imageScopeKey          string
	mainCfg                llmconfig.ClientConfig
	runtimeToolsCfg        toolsutil.RuntimeToolsRegisterConfig
	projectID              string
	compactMode            bool
	launchDir              string
	fileCacheDir           string
	fileStateDir           string
	topicContextStore      *topiccontext.Store
	workspaceDir           string
	defaultWorkspaceDir    string
	sessionStore           *llmselect.Store
	llmValues              llmutil.RuntimeValues
	clientOverridesEnabled bool
	loadedSkills           []string
	skillItems             []skillsutil.SkillStatusItem
	onPlanStepUpdate       func(*agent.Context, agent.PlanStepUpdate)
	onToolCallStart        func(*agent.Context, agent.ToolCall)
	onToolCallDone         func(*agent.Context, agent.ToolCall, string, error)
	timeout                time.Duration
	writer                 io.Writer
	sendMsg                func(msg any) // set in bubbletea mode to send messages to the TUI
	uiMu                   sync.Mutex
	foregroundCancel       context.CancelFunc
	fileSnapshots          map[string]string // path -> content before write_file
}

func buildChatToolRegistry(deps Dependencies, toolTriggers map[string]bool) *tools.Registry {
	reg := tools.NewRegistry()
	if deps.RegistryFromViper == nil {
		return reg
	}
	if resolved := deps.RegistryFromViper(); resolved != nil {
		reg = resolved.Clone()
	}
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
	s.projectID = cliProjectID(s.projectDir())
}

func (s *chatSession) conversationKey() string {
	if s == nil {
		return ""
	}
	projectID := strings.TrimSpace(s.projectID)
	if projectID == "" {
		projectID = cliProjectID(s.projectDir())
	}
	if projectID == "" {
		return ""
	}
	return "chat:" + projectID
}

func cliProjectID(dir string) string {
	hash := sha256.Sum256([]byte(dir))
	return "cli_" + hex.EncodeToString(hash[:])[:16]
}

func (s *chatSession) contextCheckpointRoot() string {
	if s != nil {
		if root := pathutil.ResolveStateDir(s.fileStateDir); strings.TrimSpace(root) != "" {
			return root
		}
	}
	return statepaths.FileStateDir()
}

func applyChatClientConfigOverrides(cmd *cobra.Command, cfg *llmconfig.ClientConfig) {
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

func chatTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func (s *chatSession) rebuildRuntimeState(ctx context.Context) error {
	return s.rebuildRuntimeStateForTask(ctx, "Interactive chat session")
}

func (s *chatSession) rebuildRuntimeStateForTask(ctx context.Context, task string) error {
	selectionKey := llmstats.NewSyntheticRunID("chat-state")
	prepared, err := s.prepareRuntimeForTaskRoute(ctx, task, llmutil.RoutePurposeMainLoop, "", selectionKey)
	if err != nil {
		return err
	}
	mainCfg := prepared.Route.ClientConfig
	loadedSkills := append([]string(nil), prepared.LoadedSkills...)
	if err := prepared.Cleanup(); err != nil {
		return err
	}
	s.mainCfg = mainCfg
	s.loadedSkills = loadedSkills
	return nil
}

func resolveChatTaskRoute(values llmutil.RuntimeValues, selection llmselect.MainSelection, routePurpose string, reasoningEffort string, selectionKey string) (llmutil.ResolvedRoute, bool, error) {
	routePurpose = strings.ToLower(strings.TrimSpace(routePurpose))
	var currentRoute llmutil.ResolvedRoute
	var err error
	if routePurpose == "" || routePurpose == llmutil.RoutePurposeMainLoop {
		currentRoute, err = llmselect.ResolveMainRoute(values, selection)
	} else {
		currentRoute, err = llmutil.ResolveRoute(values, routePurpose)
	}
	if err != nil {
		return llmutil.ResolvedRoute{}, false, err
	}
	mainWasWeighted := len(currentRoute.Candidates) > 0
	currentRoute = llmutil.SelectRouteCandidate(currentRoute, selectionKey)
	if strings.TrimSpace(reasoningEffort) != "" {
		currentRoute = llmutil.ResolvedRouteWithReasoningEffort(currentRoute, reasoningEffort)
	}
	return currentRoute, mainWasWeighted, nil
}

func (s *chatSession) prepareRuntimeForTaskRoute(ctx context.Context, task string, routePurpose string, reasoningEffort string, selectionKey string) (*taskruntime.PreparedEngine, error) {
	if s == nil || s.taskRuntime == nil {
		return nil, fmt.Errorf("chat task runtime is not initialized")
	}
	if ctx == nil {
		ctx = s.rootContext
	}
	if ctx == nil {
		return nil, fmt.Errorf("chat context is not initialized")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		task = "Interactive chat session"
	}
	currentRoute, _, err := resolveChatTaskRoute(s.llmValues, s.sessionStore.Get(), routePurpose, reasoningEffort, selectionKey)
	if err != nil {
		return nil, err
	}
	runCfg := currentRoute.ClientConfig
	mainPurpose := strings.TrimSpace(routePurpose) == "" || routePurpose == llmutil.RoutePurposeMainLoop
	if mainPurpose && s.clientOverridesEnabled {
		applyChatClientConfigOverrides(s.cmd, &runCfg)
	}
	currentRoute.ClientConfig = runCfg
	currentRoute.Values = llmutil.RuntimeValuesWithClientConfig(currentRoute.Values, runCfg)
	if strings.TrimSpace(selectionKey) != "" {
		ctx = llmstats.WithRunID(ctx, selectionKey)
	}
	imageValues := llmutil.RuntimeValuesWithClientConfig(currentRoute.Values, runCfg)
	if s.cmd != nil && s.cmd.Flags().Changed("llm-request-timeout") && runCfg.RequestTimeout > 0 {
		imageValues.ImageTimeoutRaw = runCfg.RequestTimeout.String()
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
	prepared, err := s.taskRuntime.PrepareEngine(ctx, taskruntime.RunRequest{
		Task:                    task,
		Route:                   &currentRoute,
		RoutePurpose:            routePurpose,
		ReasoningEffortOverride: reasoningEffort,
		Scene:                   "chat.loop",
		ImageToolScope:          s.imageScopeKey,
		ImageToolRetention:      toolsutil.ImageToolRetentionSticky,
		RuntimeToolsConfig:      &runtimeToolsCfg,
		CreateImageClient: func() (llm.ImageClient, error) {
			return llmutil.ImageClientFromValuesWithStats(imageValues, s.logger)
		},
		PlanStepUpdate:  s.onPlanStepUpdate,
		OnToolCallStart: s.onToolCallStart,
		OnToolCallDone:  s.onToolCallDone,
	})
	if err != nil {
		return nil, err
	}
	return prepared, nil
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

func (s *chatSession) beginForegroundCommand(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	s.uiMu.Lock()
	s.foregroundCancel = cancel
	s.uiMu.Unlock()
	return ctx, func() {
		cancel()
		s.uiMu.Lock()
		s.foregroundCancel = nil
		s.uiMu.Unlock()
	}
}

func (s *chatSession) cancelForegroundCommand() bool {
	if s == nil {
		return false
	}
	s.uiMu.Lock()
	cancel := s.foregroundCancel
	s.uiMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *chatSession) clearActivity() {
	if s != nil && s.sendMsg != nil {
		s.sendMsg(thinkingMsg{on: false})
	}
}

func (s *chatSession) setActivity(msg string, tool bool) {
	if s != nil && s.sendMsg != nil {
		s.sendMsg(thinkingMsg{on: true, message: msg, tool: tool})
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
	defaultWorkspaceDir := strings.TrimSpace(viper.GetString("workspace_dir"))
	workspaceDir, err := workspace.ResolveInitialWorkspace(launchDir, rawWorkspace, noWorkspace, defaultWorkspaceDir, nil)
	if err != nil {
		return nil, err
	}
	if noWorkspace {
		defaultWorkspaceDir = ""
	}

	llmValues, err := llmutil.RuntimeValuesFromViper()
	if err != nil {
		return nil, err
	}
	runtimePaths := runtimepaths.FromReader(viper.GetViper())
	topicContextStore := topiccontext.NewStore(runtimePaths.TopicContextPath)
	sessionStore := llmselect.NewStore()
	if cmd.Flags().Changed("profile") {
		profileName, _ := cmd.Flags().GetString("profile")
		profileName = strings.TrimSpace(profileName)
		if profileName != "" {
			if _, err := llmutil.ResolveRouteWithProfileOverride(llmValues, llmutil.RoutePurposeMainLoop, profileName); err != nil {
				return nil, fmt.Errorf("failed to resolve profile %q: %w", profileName, err)
			}
			sessionStore.SetProfile(profileName)
		}
	}
	runtimeToolsCfg := toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
	rootContext := processsignal.InteractiveParent(cmd.Context())

	projectDir := strings.TrimSpace(workspaceDir)
	if projectDir == "" {
		projectDir = launchDir
	}
	compactMode := configutil.FlagOrViperBool(cmd, "compact-mode", "chat.compact_mode")
	sess := &chatSession{
		version:                strings.TrimSpace(deps.Version),
		cmd:                    cmd,
		rootContext:            rootContext,
		logger:                 logger,
		imageScopeKey:          "chat:" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		runtimeToolsCfg:        runtimeToolsCfg,
		projectID:              cliProjectID(projectDir),
		compactMode:            compactMode,
		launchDir:              launchDir,
		fileCacheDir:           fileCacheDir,
		fileStateDir:           fileStateDir,
		topicContextStore:      topicContextStore,
		workspaceDir:           workspaceDir,
		defaultWorkspaceDir:    defaultWorkspaceDir,
		sessionStore:           sessionStore,
		llmValues:              llmValues,
		clientOverridesEnabled: true,
		timeout:                timeout,
		fileSnapshots:          make(map[string]string),
	}
	configureChatSessionCallbacks(sess, logger)

	resolveRoute := func(purpose string) (llmutil.ResolvedRoute, error) {
		purpose = strings.TrimSpace(purpose)
		if purpose == "" || purpose == llmutil.RoutePurposeMainLoop {
			route, resolveErr := llmselect.ResolveMainRoute(llmValues, sess.sessionStore.Get())
			if resolveErr != nil {
				return llmutil.ResolvedRoute{}, resolveErr
			}
			if len(route.Candidates) == 0 {
				cfg := route.ClientConfig
				applyChatClientConfigOverrides(cmd, &cfg)
				route.ClientConfig = cfg
				route.Values = llmutil.RuntimeValuesWithClientConfig(route.Values, cfg)
			}
			return route, nil
		}
		return llmutil.ResolveRoute(llmValues, purpose)
	}
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
					TopicContextStore:   topicContextStore,
					Logger:              logger,
				})
			},
			logger,
		)
	}
	common := depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return logger, nil },
		LogOptions:      func() agent.LogOptions { return logOpts },
		ResolveLLMRoute: resolveRoute,
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRouteWithProfileOverride(llmValues, purpose, profile)
		},
		CreateLLMClient: createLLMClient,
		CreateImageClient: func() (llm.ImageClient, error) {
			return llmutil.ImageClientFromValuesWithStats(llmValues, logger)
		},
		Registry: func() *tools.Registry {
			return buildChatToolRegistry(deps, nil)
		},
		ToolTriggers: func(task string) map[string]bool {
			skillsCfg := skillsutil.SkillsConfigFromRunCmd(cmd)
			return toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, skillsCfg))
		},
		RegisterTriggeredStaticTools: deps.RegisterTriggeredStaticTools,
		ACPAgents:                    acpclient.AgentsFromViper,
		RuntimeToolsConfig:           runtimeToolsCfg,
		RuntimePaths:                 runtimePaths,
		Guard:                        deps.GuardFromViper,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, opts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			skillsCfg := skillsutil.SkillsConfigFromRunCmd(cmd)
			skillsCfg.Requested = append(append([]string(nil), skillsCfg.Requested...), stickySkills...)
			spec, loaded, promptErr := skillsutil.PromptSpecWithSkills(ctx, logger, opts, task, client, model, skillsCfg)
			if promptErr != nil {
				return agent.PromptSpec{}, nil, promptErr
			}
			blocks := make([]agent.PromptBlock, 0, 2+len(spec.Blocks))
			if block := workspace.PromptBlock(sess.workspaceDir); strings.TrimSpace(block.Content) != "" {
				blocks = append(blocks, block)
			}
			blocks = append(blocks, agent.PromptBlock{Content: chatBuiltinCommandsBlock()})
			spec.Blocks = append(blocks, spec.Blocks...)
			return spec, loaded, nil
		},
	}
	engineToolsConfig := agent.EngineToolsConfig{
		SpawnEnabled:    viper.GetBool("tools.spawn.enabled"),
		ACPSpawnEnabled: viper.GetBool("tools.acp_spawn.enabled"),
		CoderEnabled:    viper.GetBool("tools.coder.enabled"),
		PathRoots:       pathroots.New("", fileCacheDir, fileStateDir),
		CoderPathExtra:  append([]string(nil), viper.GetStringSlice("tools.coder.path_extra")...),
	}
	taskRuntime, err := taskruntime.NewRunPreparer(common, taskruntime.BootstrapOptions{
		AgentConfig: agent.Config{
			MaxSteps:        configutil.FlagOrViperInt(cmd, "max-steps", "max_steps"),
			ParseRetries:    configutil.FlagOrViperInt(cmd, "parse-retries", "parse_retries"),
			MaxTokenBudget:  configutil.FlagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
			ToolRepeatLimit: configutil.FlagOrViperInt(cmd, "tool-repeat-limit", "tool_repeat_limit"),
			ContextCompaction: agent.NewContextCompactionConfig(
				viper.GetBool("context_compaction.enabled"),
				viper.GetFloat64("context_compaction.trigger_ratio"),
				viper.GetInt("context_compaction.output_reserve_tokens"),
			),
		},
		EngineToolsConfig: &engineToolsConfig,
	})
	if err != nil {
		return nil, err
	}
	sess.taskRuntime = taskRuntime
	promptCtx, cancel := chatTimeoutContext(rootContext, timeout)
	err = sess.rebuildRuntimeState(promptCtx)
	cancel()
	if err != nil {
		_ = taskRuntime.Close()
		sess.taskRuntime = nil
		return nil, err
	}
	skillStatus, err := skillsutil.BuildSkillStatus(skillsutil.SkillsConfigFromRunCmd(cmd), sess.loadedSkills)
	if err != nil {
		logger.Warn("chat_skill_picker_load_failed", "error", err.Error())
	} else {
		sess.skillItems = append(skillStatus.Loaded, skillStatus.Available...)
	}

	return sess, nil
}

func (s *chatSession) cleanup() {
	if s == nil {
		return
	}
	if s.taskRuntime != nil {
		if err := s.taskRuntime.Close(); err != nil && s.logger != nil {
			s.logger.Warn("chat_runtime_close_failed", "error", err.Error())
		}
		s.taskRuntime = nil
	}
}
