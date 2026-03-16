package consolecmd

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

const (
	consoleLocalEndpointRef   = "ep_console_local"
	consoleLocalEndpointName  = "Console Local"
	consoleLocalEndpointURL   = "in-process://console-local"
	consoleTaskOutputMaxChars = 4000
	consoleTopicTitleMaxChars = 72
	consoleTopicTitleTimeout  = 20 * time.Second
)

type consoleLocalTaskJob struct {
	TaskID          string
	ConversationKey string
	TopicID         string
	Task            string
	Model           string
	Timeout         time.Duration
	CreatedAt       time.Time
	Trigger         daemonruntime.TaskTrigger
	AutoRenameTopic bool
	Version         uint64
}

type consoleLocalRuntimeBundle struct {
	taskRuntime     *taskruntime.Runtime
	mcpHost         *mcphost.Host
	defaultModel    string
	defaultProvider string
}

type consoleLocalRuntime struct {
	logger                  *slog.Logger
	store                   *daemonruntime.ConsoleFileStore
	runner                  *runtimecore.ConversationRunner[string, consoleLocalTaskJob]
	bundleMu                sync.RWMutex
	bundle                  *consoleLocalRuntimeBundle
	managedRuntimeMu        sync.RWMutex
	managedRuntimeRunning   map[string]bool
	commonDeps              depsutil.CommonDependencies
	memoryEnabled           bool
	defaultTimeout          time.Duration
	memoryInjectionEnabled  bool
	memoryInjectionMaxItems int
	memRuntime              runtimecore.MemoryRuntime
	handler                 http.Handler
	authToken               string
	agentName               string
	cancelWorkers           context.CancelFunc
	seq                     atomic.Uint64
}

func newConsoleLocalRuntime() (*consoleLocalRuntime, error) {
	logger, err := logutil.LoggerFromViper()
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	logOpts := logutil.LogOptionsFromViper()
	out := &consoleLocalRuntime{
		logger: logger,
	}
	var baseRegistry *tools.Registry
	var sharedGuard *guard.Guard
	var mcpHost *mcphost.Host

	commonDeps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return logOpts
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRoute(llmutil.RuntimeValuesFromViper(), purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			base, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
			if err != nil {
				return nil, err
			}
			return llmstats.WrapRuntimeClient(base, route.ClientConfig.Provider, route.ClientConfig.Endpoint, route.ClientConfig.Model, logger), nil
		},
		RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromViper(),
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		Guard: func(_ *slog.Logger) *guard.Guard {
			return sharedGuard
		},
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := skillsutil.SkillsConfigFromViper()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
	}

	baseRegistry, mcpHost = buildConsoleBaseRegistry(context.Background(), logger)
	sharedGuard = buildConsoleGuardFromViper(logger)
	taskRuntimeOpts := taskruntime.BootstrapOptions{
		AgentConfig:          consoleAgentConfigFromViper(),
		DefaultModelFallback: "gpt-5.2",
	}
	execRuntime, err := taskruntime.Bootstrap(commonDeps, taskRuntimeOpts)
	if err != nil {
		return nil, err
	}
	if warning := consoleLLMCredentialsWarning(execRuntime.MainRoute); warning != "" {
		logger.Warn("console_llm_credentials_missing",
			"provider", execRuntime.MainProvider,
			"hint", warning,
		)
	}

	defaultTimeout := viper.GetDuration("timeout")
	if defaultTimeout <= 0 {
		defaultTimeout = 10 * time.Minute
	}

	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	memoryEnabled := viper.GetBool("memory.enabled")
	memRuntime, err := runtimecore.NewMemoryRuntime(commonDeps, runtimecore.MemoryRuntimeOptions{
		Enabled:       memoryEnabled,
		ShortTermDays: viper.GetInt("memory.short_term_days"),
		Logger:        logger,
	})
	if err != nil {
		cancelWorkers()
		return nil, err
	}
	if memRuntime.ProjectionWorker != nil {
		memRuntime.ProjectionWorker.Start(workersCtx)
	}

	authToken := strings.TrimSpace(viper.GetString("server.auth_token"))
	if authToken == "" {
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, fmt.Errorf("missing server.auth_token (set via MISTER_MORPH_SERVER_AUTH_TOKEN) for console local runtime")
	}
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:          statepaths.TaskTargetDir("console"),
		HeartbeatTopicID: strings.TrimSpace(viper.GetString("tasks.targets.console.heartbeat_topic_id")),
		Persist:          consoleTaskPersistenceEnabled(),
	})
	if err != nil {
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, err
	}
	out.store = store
	out.bundle = &consoleLocalRuntimeBundle{
		taskRuntime:     execRuntime,
		mcpHost:         mcpHost,
		defaultModel:    execRuntime.MainModel,
		defaultProvider: execRuntime.MainProvider,
	}
	out.managedRuntimeRunning = map[string]bool{}
	out.commonDeps = commonDeps
	out.memoryEnabled = memoryEnabled
	out.defaultTimeout = defaultTimeout
	out.memoryInjectionEnabled = viper.GetBool("memory.injection.enabled")
	out.memoryInjectionMaxItems = viper.GetInt("memory.injection.max_items")
	out.memRuntime = memRuntime
	out.authToken = authToken
	out.agentName = personautil.LoadAgentName(statepaths.FileStateDir())
	out.cancelWorkers = cancelWorkers
	out.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workersCtx,
		make(chan struct{}, 1),
		16,
		func(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
			out.handleTaskJob(workerCtx, conversationKey, job)
		},
	)
	out.startHeartbeatLoop(workersCtx)
	out.handler = daemonruntime.NewHandler(out.routesOptions(strings.TrimSpace(authToken)))
	return out, nil
}

func (r *consoleLocalRuntime) currentBundle() *consoleLocalRuntimeBundle {
	if r == nil {
		return nil
	}
	r.bundleMu.RLock()
	defer r.bundleMu.RUnlock()
	return r.bundle
}

func (r *consoleLocalRuntime) defaultLLMConfig() (string, string) {
	bundle := r.currentBundle()
	if bundle == nil {
		return "", ""
	}
	return strings.TrimSpace(bundle.defaultProvider), strings.TrimSpace(bundle.defaultModel)
}

func (r *consoleLocalRuntime) ReloadAgentConfig() error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	baseRegistry, mcpHost := buildConsoleBaseRegistry(context.Background(), r.logger)
	sharedGuard := buildConsoleGuardFromViper(r.logger)
	deps := r.commonDeps
	deps.Registry = func() *tools.Registry { return baseRegistry }
	deps.Guard = func(_ *slog.Logger) *guard.Guard { return sharedGuard }
	deps.RuntimeToolsConfig = toolsutil.LoadRuntimeToolsRegisterConfigFromViper()
	rt, err := taskruntime.Bootstrap(deps, taskruntime.BootstrapOptions{
		AgentConfig:          consoleAgentConfigFromViper(),
		DefaultModelFallback: "gpt-5.2",
	})
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return err
	}
	if warning := consoleLLMCredentialsWarning(rt.MainRoute); warning != "" {
		r.logger.Warn("console_llm_credentials_missing",
			"provider", rt.MainProvider,
			"hint", warning,
		)
	}
	nextBundle := &consoleLocalRuntimeBundle{
		taskRuntime:     rt,
		mcpHost:         mcpHost,
		defaultModel:    rt.MainModel,
		defaultProvider: rt.MainProvider,
	}
	r.bundleMu.Lock()
	prevBundle := r.bundle
	r.bundle = nextBundle
	r.bundleMu.Unlock()
	if prevBundle != nil && prevBundle.mcpHost != nil {
		_ = prevBundle.mcpHost.Close()
	}
	return nil
}

func (r *consoleLocalRuntime) Close() {
	if r == nil {
		return
	}
	if r.cancelWorkers != nil {
		r.cancelWorkers()
	}
	if r.memRuntime.Cleanup != nil {
		r.memRuntime.Cleanup()
	}
	bundle := r.currentBundle()
	if bundle != nil && bundle.mcpHost != nil {
		_ = bundle.mcpHost.Close()
	}
}

func consoleLLMCredentialsWarning(route llmutil.ResolvedRoute) string {
	provider := strings.ToLower(strings.TrimSpace(route.ClientConfig.Provider))
	switch provider {
	case "bedrock":
		// Bedrock may rely on ambient AWS credentials outside llm.* config.
		return ""
	case "cloudflare":
		if strings.TrimSpace(route.Values.CloudflareAccountID) == "" {
			return "set llm.cloudflare.account_id to enable Console Local chat submit"
		}
		if strings.TrimSpace(route.ClientConfig.APIKey) == "" {
			return "set llm.cloudflare.api_token, llm.api_key, or MISTER_MORPH_LLM_API_KEY to enable Console Local chat submit"
		}
		return ""
	default:
		if strings.TrimSpace(route.ClientConfig.APIKey) == "" {
			return "set llm.api_key or MISTER_MORPH_LLM_API_KEY to enable Console Local chat submit"
		}
		return ""
	}
}

func (r *consoleLocalRuntime) Endpoint() runtimeEndpoint {
	return runtimeEndpoint{
		Ref:    consoleLocalEndpointRef,
		Name:   consoleLocalEndpointName,
		URL:    consoleLocalEndpointURL,
		Client: newInProcessRuntimeEndpointClient(r.handler, r.authToken),
	}
}

func (r *consoleLocalRuntime) routesOptions(authToken string) daemonruntime.RoutesOptions {
	return daemonruntime.RoutesOptions{
		Mode:          "console",
		AgentName:     strings.TrimSpace(r.agentName),
		AuthToken:     strings.TrimSpace(authToken),
		TaskReader:    r.store,
		TopicReader:   r.store,
		TopicDeleter:  r.store,
		Submit:        r.submitTask,
		HealthEnabled: true,
		Overview: func(ctx context.Context) (map[string]any, error) {
			provider, model := r.defaultLLMConfig()
			return map[string]any{
				"llm": map[string]any{
					"provider": provider,
					"model":    model,
				},
				"channel": map[string]any{
					"configured":          true,
					"telegram_configured": strings.TrimSpace(viper.GetString("telegram.bot_token")) != "",
					"slack_configured": strings.TrimSpace(viper.GetString("slack.bot_token")) != "" &&
						strings.TrimSpace(viper.GetString("slack.app_token")) != "",
					"running":          "console",
					"telegram_running": r.isManagedRuntimeRunning("telegram"),
					"slack_running":    r.isManagedRuntimeRunning("slack"),
				},
			}, nil
		},
	}
}

func (r *consoleLocalRuntime) SetManagedRuntimeRunning(kind string, running bool) {
	if r == nil {
		return
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return
	}
	r.managedRuntimeMu.Lock()
	defer r.managedRuntimeMu.Unlock()
	if r.managedRuntimeRunning == nil {
		r.managedRuntimeRunning = map[string]bool{}
	}
	r.managedRuntimeRunning[kind] = running
}

func (r *consoleLocalRuntime) isManagedRuntimeRunning(kind string) bool {
	if r == nil {
		return false
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return false
	}
	r.managedRuntimeMu.RLock()
	defer r.managedRuntimeMu.RUnlock()
	return r.managedRuntimeRunning[kind]
}

func (r *consoleLocalRuntime) submitTask(ctx context.Context, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
	timeout := r.defaultTimeout
	if strings.TrimSpace(req.Timeout) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(req.Timeout))
		if err != nil || d <= 0 {
			return daemonruntime.SubmitTaskResponse{}, daemonruntime.BadRequest("invalid timeout (use Go duration like 2m, 30s)")
		}
		timeout = d
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		_, model = r.defaultLLMConfig()
	}
	trigger := normalizeConsoleTrigger(req.Trigger, daemonruntime.TaskTrigger{
		Source: "ui",
		Event:  "chat_submit",
		Ref:    "web/console",
	})
	return r.enqueueTask(
		ctx,
		strings.TrimSpace(req.Task),
		model,
		timeout,
		strings.TrimSpace(req.TopicID),
		strings.TrimSpace(req.TopicTitle),
		trigger,
	)
}

func (r *consoleLocalRuntime) enqueueTask(ctx context.Context, task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	if r == nil {
		return daemonruntime.SubmitTaskResponse{}, fmt.Errorf("console runtime is not initialized")
	}
	now := time.Now().UTC()
	seq := r.seq.Add(1)
	taskID := daemonruntime.BuildTaskID("console", now.UnixNano(), seq, rand.Uint64())
	topicID = strings.TrimSpace(topicID)
	explicitTopicTitle := strings.TrimSpace(topicTitle)
	autoRenameTopic := topicID == "" && explicitTopicTitle == ""
	topicTitle = seedConsoleTopicTitle(task, topicTitle)
	if topicID == "" {
		topic, err := r.store.CreateTopic(topicTitle)
		if err != nil {
			return daemonruntime.SubmitTaskResponse{}, err
		}
		topicID = topic.ID
		if strings.TrimSpace(topicTitle) == "" {
			topicTitle = strings.TrimSpace(topic.Title)
		}
	}
	conversationKey := buildConsoleConversationKey(topicID)

	if err := r.store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      strings.TrimSpace(task),
		Model:     model,
		Timeout:   timeout.String(),
		CreatedAt: now,
		TopicID:   topicID,
	}, trigger, topicTitle); err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}

	err := r.runner.Enqueue(ctx, conversationKey, func(version uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{
			TaskID:          taskID,
			ConversationKey: conversationKey,
			TopicID:         topicID,
			Task:            strings.TrimSpace(task),
			Model:           model,
			Timeout:         timeout,
			CreatedAt:       now,
			Trigger:         trigger,
			AutoRenameTopic: autoRenameTopic,
			Version:         version,
		}
	})
	if err != nil {
		runtimecore.MarkTaskFailed(r.store, taskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return daemonruntime.SubmitTaskResponse{
		ID:      taskID,
		Status:  daemonruntime.TaskQueued,
		TopicID: topicID,
	}, nil
}

func (r *consoleLocalRuntime) handleTaskJob(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
	if r == nil {
		return
	}
	runtimecore.MarkTaskRunning(r.store, job.TaskID)

	runCtx, cancel := context.WithTimeout(workerCtx, job.Timeout)
	final, agentCtx, runErr := r.runTask(runCtx, conversationKey, job)
	contextDeadline := daemonruntime.IsContextDeadline(runCtx, runErr)
	cancel()

	if runErr != nil {
		displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(runErr))
		if displayErr == "" {
			displayErr = strings.TrimSpace(runErr.Error())
		}
		runtimecore.MarkTaskFailed(r.store, job.TaskID, displayErr, contextDeadline)
		return
	}

	if pendingID, ok := pendingApprovalID(final); ok {
		pendingAt := time.Now().UTC()
		r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskPending
			info.PendingAt = &pendingAt
			info.ApprovalRequestID = pendingID
			info.Result = buildConsoleTaskResult(final, agentCtx)
		})
		return
	}

	finishedAt := time.Now().UTC()
	r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = buildConsoleTaskResult(final, agentCtx)
		if strings.TrimSpace(output) != "" {
			// Keep output summary for old readers that only consume result.output.
			if resultMap, ok := info.Result.(map[string]any); ok {
				resultMap["output"] = daemonruntime.TruncateUTF8(output, consoleTaskOutputMaxChars)
				info.Result = resultMap
			}
		}
	})
	r.maybeRefreshTopicTitle(job, strings.TrimSpace(outputfmt.FormatFinalOutput(final)))
}

func (r *consoleLocalRuntime) runTask(ctx context.Context, conversationKey string, job consoleLocalTaskJob) (*agent.Final, *agent.Context, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("console runtime is not initialized")
	}
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	task := strings.TrimSpace(job.Task)
	if task == "" {
		return nil, nil, fmt.Errorf("empty console task")
	}
	model := strings.TrimSpace(job.Model)
	if model == "" {
		_, model = r.defaultLLMConfig()
	}
	memSubjectID := buildConsoleMemorySubjectID(conversationKey)
	memoryHooks := taskruntime.MemoryHooks{
		Source:    "console",
		SubjectID: memSubjectID,
	}
	if r.memoryEnabled && r.memRuntime.Orchestrator != nil && memSubjectID != "" {
		memoryHooks.InjectionEnabled = r.memoryInjectionEnabled
		memoryHooks.InjectionMaxItems = r.memoryInjectionMaxItems
		memoryHooks.PrepareInjection = func(maxItems int) (string, error) {
			return r.memRuntime.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
				SubjectID:      memSubjectID,
				RequestContext: memory.ContextPrivate,
				MaxItems:       maxItems,
			})
		}
		memoryHooks.Record = func(_ *agent.Final, finalOutput string) error {
			_, err := r.memRuntime.Orchestrator.Record(buildConsoleMemoryRecordRequest(job, memSubjectID, finalOutput))
			return err
		}
		memoryHooks.NotifyRecorded = func() {
			if r.memRuntime.ProjectionWorker != nil {
				r.memRuntime.ProjectionWorker.NotifyRecordAppended()
			}
		}
	}
	bundle := r.currentBundle()
	if bundle == nil || bundle.taskRuntime == nil {
		return nil, nil, fmt.Errorf("console task runtime is not initialized")
	}
	result, err := bundle.taskRuntime.Run(ctx, taskruntime.RunRequest{
		Task:  task,
		Model: model,
		Scene: "console.loop",
		Meta: map[string]any{
			"trigger":          consoleTriggerSource(job.Trigger),
			"console_task_id":  job.TaskID,
			"console_topic_id": strings.TrimSpace(job.TopicID),
		},
		Memory: memoryHooks,
	})
	if err != nil {
		return result.Final, result.Context, err
	}
	return result.Final, result.Context, nil
}

func buildConsoleTaskResult(final *agent.Final, runCtx *agent.Context) map[string]any {
	out := map[string]any{
		"final": final,
	}
	if runCtx != nil {
		out["metrics"] = runCtx.Metrics
		out["steps"] = summarizeConsoleSteps(runCtx)
	}
	return out
}

func (r *consoleLocalRuntime) maybeRefreshTopicTitle(job consoleLocalTaskJob, finalOutput string) {
	if r == nil || !job.AutoRenameTopic {
		return
	}
	topicID := strings.TrimSpace(job.TopicID)
	if topicID == "" || topicID == daemonruntime.ConsoleDefaultTopicID || topicID == r.store.HeartbeatTopicID() {
		return
	}
	taskText := strings.TrimSpace(job.Task)
	if taskText == "" {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), consoleTopicTitleTimeout)
		defer cancel()

		title, err := r.generateTopicTitle(ctx, taskText, finalOutput)
		if err != nil {
			r.logger.Debug("console_topic_title_generate_failed", "topic_id", topicID, "error", err.Error())
			return
		}
		if err := r.store.SetTopicTitle(topicID, title); err != nil {
			r.logger.Debug("console_topic_title_update_failed", "topic_id", topicID, "error", err.Error())
		}
	}()
}

func (r *consoleLocalRuntime) generateTopicTitle(ctx context.Context, task string, finalOutput string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("console runtime is not initialized")
	}
	route, err := depsutil.ResolveLLMRouteFromCommon(r.commonDeps, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return "", err
	}
	client, err := depsutil.CreateClientFromCommon(r.commonDeps, route)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(route.ClientConfig.Model)
	if model == "" {
		_, model = r.defaultLLMConfig()
	}
	task = daemonruntime.TruncateUTF8(strings.Join(strings.Fields(task), " "), 1200)
	finalOutput = daemonruntime.TruncateUTF8(strings.Join(strings.Fields(finalOutput), " "), 1200)
	if task == "" {
		return "", fmt.Errorf("task is empty")
	}
	if finalOutput == "" {
		finalOutput = "(no final output)"
	}

	result, err := client.Chat(ctx, llm.Request{
		Model: model,
		Scene: "console.topic_title",
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "Generate a short conversation topic title. " +
					"Reply with plain text only, keep the original language, use at most 8 words, and do not wrap the answer in quotes.",
			},
			{
				Role:    "user",
				Content: "User task:\n" + task + "\n\nFinal output:\n" + finalOutput,
			},
		},
	})
	if err != nil {
		return "", err
	}
	title := sanitizeConsoleTopicTitle(result.Text)
	if title == "" {
		return "", fmt.Errorf("generated topic title is empty")
	}
	return title, nil
}

func summarizeConsoleSteps(ctx *agent.Context) []map[string]any {
	if ctx == nil || len(ctx.Steps) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(ctx.Steps))
	for _, s := range ctx.Steps {
		m := map[string]any{
			"step":        s.StepNumber,
			"action":      s.Action,
			"duration_ms": s.Duration.Milliseconds(),
		}
		if s.Error != nil {
			m["error"] = s.Error.Error()
		}
		out = append(out, m)
	}
	return out
}

func consoleTaskPersistenceEnabled() bool {
	for _, target := range viper.GetStringSlice("tasks.persistence_targets") {
		if strings.EqualFold(strings.TrimSpace(target), "console") {
			return true
		}
	}
	return false
}

func buildConsoleConversationKey(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		topicID = daemonruntime.ConsoleDefaultTopicID
	}
	return "console:" + topicID
}

func seedConsoleTopicTitle(task string, explicit string) string {
	if title := strings.TrimSpace(explicit); title != "" {
		return title
	}
	task = strings.Join(strings.Fields(task), " ")
	if task == "" {
		return ""
	}
	title := daemonruntime.TruncateUTF8(task, consoleTopicTitleMaxChars)
	if len([]rune(task)) > len([]rune(title)) {
		title = strings.TrimSpace(title) + "..."
	}
	return strings.TrimSpace(title)
}

func sanitizeConsoleTopicTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'`")
	raw = strings.Join(strings.Fields(strings.ReplaceAll(raw, "\n", " ")), " ")
	raw = strings.TrimRight(raw, ".,;:!?，。！？；：")
	raw = daemonruntime.TruncateUTF8(raw, consoleTopicTitleMaxChars)
	return strings.TrimSpace(raw)
}

func buildConsoleMemorySubjectID(conversationKey string) string {
	key := strings.TrimSpace(conversationKey)
	if key == "" {
		return buildConsoleConversationKey(daemonruntime.ConsoleDefaultTopicID)
	}
	if strings.HasPrefix(strings.ToLower(key), "console:") {
		return key
	}
	return buildConsoleConversationKey(key)
}

func normalizeConsoleTrigger(in *daemonruntime.TaskTrigger, fallback daemonruntime.TaskTrigger) daemonruntime.TaskTrigger {
	if in == nil {
		return fallback
	}
	trigger := daemonruntime.TaskTrigger{
		Source: strings.TrimSpace(in.Source),
		Event:  strings.TrimSpace(in.Event),
		Ref:    strings.TrimSpace(in.Ref),
	}
	if strings.TrimSpace(trigger.Source) == "" &&
		strings.TrimSpace(trigger.Event) == "" &&
		strings.TrimSpace(trigger.Ref) == "" {
		return fallback
	}
	if strings.TrimSpace(trigger.Source) == "" {
		trigger.Source = fallback.Source
	}
	if strings.TrimSpace(trigger.Event) == "" {
		trigger.Event = fallback.Event
	}
	if strings.TrimSpace(trigger.Ref) == "" {
		trigger.Ref = fallback.Ref
	}
	return trigger
}

func consoleTriggerSource(trigger daemonruntime.TaskTrigger) string {
	if source := strings.TrimSpace(trigger.Source); source != "" {
		return source
	}
	return "console"
}

func (r *consoleLocalRuntime) startHeartbeatLoop(ctx context.Context) {
	if r == nil {
		return
	}
	hbEnabled := viper.GetBool("heartbeat.enabled")
	hbInterval := viper.GetDuration("heartbeat.interval")
	if !hbEnabled || hbInterval <= 0 {
		return
	}
	hbState := &heartbeatutil.State{}
	hbChecklist := statepaths.HeartbeatChecklistPath()
	heartbeatTopicID := r.store.HeartbeatTopicID()

	runHeartbeatTick := func() heartbeatutil.TickResult {
		result := heartbeatutil.Tick(
			hbState,
			func() (string, bool, error) {
				return heartbeatutil.BuildHeartbeatTask(hbChecklist)
			},
			func(task string, checklistEmpty bool) string {
				if _, err := r.enqueueTask(
					context.Background(),
					task,
					func() string {
						_, model := r.defaultLLMConfig()
						return model
					}(),
					r.defaultTimeout,
					heartbeatTopicID,
					daemonruntime.ConsoleHeartbeatTopicTitle,
					daemonruntime.TaskTrigger{
						Source: "heartbeat",
						Event:  "heartbeat_tick",
						Ref:    "console",
					},
				); err != nil {
					return err.Error()
				}
				return ""
			},
		)
		switch result.Outcome {
		case heartbeatutil.TickBuildError:
			if strings.TrimSpace(result.AlertMessage) != "" {
				r.logger.Warn("heartbeat_alert", "source", "console", "message", result.AlertMessage)
			} else if result.BuildError != nil {
				r.logger.Warn("heartbeat_task_error", "source", "console", "error", result.BuildError.Error())
			}
		case heartbeatutil.TickSkipped:
			r.logger.Debug("heartbeat_skip", "source", "console", "reason", result.SkipReason)
		}
		return result
	}

	go func() {
		initialTimer := time.NewTimer(15 * time.Second)
		defer initialTimer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-initialTimer.C:
		}
		_ = runHeartbeatTick()

		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = runHeartbeatTick()
			}
		}
	}()
}

func buildConsoleMemoryRecordRequest(job consoleLocalTaskJob, subjectID, output string) memoryruntime.RecordRequest {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		subjectID = buildConsoleConversationKey(daemonruntime.ConsoleDefaultTopicID)
	}
	now := time.Now().UTC()
	sentAt := job.CreatedAt
	if sentAt.IsZero() {
		sentAt = now
	}
	inbound := chathistory.ChatHistoryItem{
		Channel:   "console",
		Kind:      chathistory.KindInboundUser,
		ChatID:    subjectID,
		ChatType:  "private",
		MessageID: job.TaskID,
		SentAt:    sentAt,
		Sender: chathistory.ChatHistorySender{
			UserID:     "console:user",
			Username:   "console",
			Nickname:   "Console User",
			DisplayRef: "console:user",
		},
		Text: strings.TrimSpace(job.Task),
	}
	outbound := chathistory.ChatHistoryItem{
		Channel:          "console",
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           subjectID,
		ChatType:         "private",
		ReplyToMessageID: job.TaskID,
		SentAt:           now,
		Sender: chathistory.ChatHistorySender{
			UserID:     "0",
			Username:   "agent",
			Nickname:   "MisterMorph",
			IsBot:      true,
			DisplayRef: "agent",
		},
		Text: strings.TrimSpace(output),
	}
	return memoryruntime.RecordRequest{
		TaskRunID:   strings.TrimSpace(job.TaskID),
		SessionID:   subjectID,
		SubjectID:   subjectID,
		Channel:     "console",
		TaskText:    strings.TrimSpace(job.Task),
		FinalOutput: strings.TrimSpace(output),
		Participants: []memory.MemoryParticipant{
			{ID: "console:user", Nickname: "Console User", Protocol: "console"},
			{ID: 0, Nickname: "MisterMorph", Protocol: ""},
		},
		SourceHistory: []chathistory.ChatHistoryItem{
			inbound,
			outbound,
		},
		SessionContext: memory.SessionContext{
			ConversationID:     subjectID,
			ConversationType:   "private",
			CounterpartyID:     "console:user",
			CounterpartyName:   "Console User",
			CounterpartyHandle: "console",
			CounterpartyLabel:  "[Console User](console:user)",
		},
	}
}

func pendingApprovalID(final *agent.Final) (string, bool) {
	if final == nil || final.Output == nil {
		return "", false
	}
	switch v := final.Output.(type) {
	case agent.PendingOutput:
		if strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case *agent.PendingOutput:
		if v != nil && strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case map[string]any:
		st, _ := v["status"].(string)
		id, _ := v["approval_request_id"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "pending") && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), true
		}
	}
	return "", false
}
