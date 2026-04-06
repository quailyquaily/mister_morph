package consolecmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	heartbeatloop "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/streaming"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

const (
	consoleLocalEndpointRef               = "ep_console_local"
	consoleLocalEndpointName              = "Console Local"
	consoleLocalEndpointURL               = "in-process://console-local"
	consoleTopicTitleMaxChars             = 72
	consoleTopicTitleDirectOutputMaxRunes = 32
	consoleTopicTitleTimeout              = 20 * time.Second
	consoleHeartbeatSkipNoLLM             = "console_submit_unavailable"
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
	WakeSignal      daemonruntime.PokeInput
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
	inspectors              *consoleInspectors
	store                   *daemonruntime.ConsoleFileStore
	bus                     *busruntime.Inproc
	runner                  *runtimecore.ConversationRunner[string, consoleLocalTaskJob]
	contactsSvc             *contacts.Service
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
	streamHub               *consoleStreamHub
	heartbeatState          *heartbeatutil.State
	heartbeatPokeRequests   chan heartbeatloop.PokeRequest
	handler                 http.Handler
	authToken               string
	cancelWorkers           context.CancelFunc
	seq                     atomic.Uint64
}

func newConsoleLocalRuntime(cfg serveConfig) (*consoleLocalRuntime, error) {
	logger, err := logutil.LoggerFromViper()
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	logOpts := logutil.LogOptionsFromViper()
	inspectors, err := newConsoleInspectors(cfg.inspectPrompt, cfg.inspectRequest, "console", "console", "20060102_150405")
	if err != nil {
		return nil, err
	}
	out := &consoleLocalRuntime{
		logger:     logger,
		inspectors: inspectors,
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
			values := llmutil.RuntimeValuesFromViper()
			if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
				return llmselect.ResolveMainRoute(values, llmselect.ProcessStore().Get())
			}
			return llmutil.ResolveRoute(values, purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return llmutil.BuildRouteClient(
				route,
				nil,
				llmutil.ClientFromConfigWithValues,
				func(client llm.Client, cfg llmconfig.ClientConfig, profile string) llm.Client {
					wrappedRoute := route
					wrappedRoute.Profile = strings.TrimSpace(profile)
					wrappedRoute.ClientConfig = cfg
					wrappedRoute.Fallbacks = nil
					wrapped := llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
					return inspectors.Wrap(wrapped, wrappedRoute)
				},
				logger,
			)
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
		AgentConfig:       consoleAgentConfigFromViper(),
		EngineToolsConfig: &agent.EngineToolsConfig{SpawnEnabled: consoleEngineToolsConfigFromViper().SpawnEnabled},
	}
	execRuntime, err := taskruntime.Bootstrap(commonDeps, taskRuntimeOpts)
	if err != nil {
		_ = inspectors.Close()
		return nil, err
	}
	if warning := consoleLLMCredentialsWarning(execRuntime.BootstrapMainRoute); warning != "" {
		logger.Warn("console_llm_credentials_missing",
			"provider", execRuntime.BootstrapMainProvider,
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
		_ = inspectors.Close()
		cancelWorkers()
		return nil, err
	}
	if memRuntime.ProjectionWorker != nil {
		memRuntime.ProjectionWorker.Start(workersCtx)
	}

	authToken, err := consoleLocalRuntimeAuthToken()
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, err
	}
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:          statepaths.TaskTargetDir("console"),
		HeartbeatTopicID: strings.TrimSpace(viper.GetString("tasks.targets.console.heartbeat_topic_id")),
		Persist:          consoleTaskPersistenceEnabled(),
	})
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, err
	}
	maxInFlight := viper.GetInt("bus.max_inflight")
	if maxInFlight <= 0 {
		maxInFlight = 1024
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: maxInFlight,
		Logger:      logger,
		Component:   "console",
	})
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, err
	}
	out.store = store
	out.bus = inprocBus
	out.streamHub = newConsoleStreamHub()
	out.bundle = &consoleLocalRuntimeBundle{
		taskRuntime:     execRuntime,
		mcpHost:         mcpHost,
		defaultModel:    execRuntime.BootstrapMainModel,
		defaultProvider: execRuntime.BootstrapMainProvider,
	}
	out.managedRuntimeRunning = map[string]bool{}
	out.commonDeps = commonDeps
	out.memoryEnabled = memoryEnabled
	out.defaultTimeout = defaultTimeout
	out.memoryInjectionEnabled = viper.GetBool("memory.injection.enabled")
	out.memoryInjectionMaxItems = viper.GetInt("memory.injection.max_items")
	out.memRuntime = memRuntime
	out.contactsSvc = contacts.NewService(contacts.NewFileStore(statepaths.ContactsDir()))
	out.authToken = authToken
	out.cancelWorkers = cancelWorkers
	out.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workersCtx,
		make(chan struct{}, 1),
		16,
		func(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
			out.handleTaskJob(workerCtx, conversationKey, job)
		},
	)
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, out.handleConsoleBusMessage); err != nil {
		_ = inspectors.Close()
		inprocBus.Close()
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, err
	}
	out.startHeartbeatLoop(workersCtx)
	out.handler = daemonruntime.NewHandler(out.routesOptions(strings.TrimSpace(authToken)))
	return out, nil
}

func consoleLocalRuntimeAuthToken() (string, error) {
	if token := strings.TrimSpace(viper.GetString("server.auth_token")); token != "" {
		return token, nil
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate console local auth token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
	if r != nil {
		route, err := depsutil.ResolveLLMRouteFromCommon(r.commonDeps, llmutil.RoutePurposeMainLoop)
		if err == nil {
			return strings.TrimSpace(route.ClientConfig.Provider), strings.TrimSpace(route.ClientConfig.Model)
		}
	}
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
		AgentConfig:       consoleAgentConfigFromViper(),
		EngineToolsConfig: &agent.EngineToolsConfig{SpawnEnabled: consoleEngineToolsConfigFromViper().SpawnEnabled},
	})
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return err
	}
	if warning := consoleLLMCredentialsWarning(rt.BootstrapMainRoute); warning != "" {
		r.logger.Warn("console_llm_credentials_missing",
			"provider", rt.BootstrapMainProvider,
			"hint", warning,
		)
	}
	nextBundle := &consoleLocalRuntimeBundle{
		taskRuntime:     rt,
		mcpHost:         mcpHost,
		defaultModel:    rt.BootstrapMainModel,
		defaultProvider: rt.BootstrapMainProvider,
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
	if r.bus != nil {
		_ = r.bus.Close()
	}
	if r.cancelWorkers != nil {
		r.cancelWorkers()
	}
	if r.memRuntime.Cleanup != nil {
		r.memRuntime.Cleanup()
	}
	if r.inspectors != nil {
		_ = r.inspectors.Close()
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
		Client: newInProcessRuntimeEndpointClient(r.handler, r.authToken, r.canSubmit),
	}
}

func (r *consoleLocalRuntime) canSubmit() bool {
	if r == nil {
		return false
	}
	bundle := r.currentBundle()
	if bundle == nil || bundle.taskRuntime == nil {
		return false
	}
	return consoleLLMCredentialsWarning(bundle.taskRuntime.BootstrapMainRoute) == ""
}

func (r *consoleLocalRuntime) canPokeHeartbeat() bool {
	return r != nil && r.heartbeatPokeRequests != nil
}

func (r *consoleLocalRuntime) pokeHeartbeat(ctx context.Context, input daemonruntime.PokeInput) error {
	if r == nil || r.heartbeatPokeRequests == nil {
		return fmt.Errorf("heartbeat poke is unavailable")
	}
	return heartbeatloop.Trigger(ctx, r.heartbeatPokeRequests, input)
}

func (r *consoleLocalRuntime) routesOptions(authToken string) daemonruntime.RoutesOptions {
	var poke daemonruntime.PokeFunc
	if r.canPokeHeartbeat() {
		poke = r.pokeHeartbeat
	}
	return daemonruntime.RoutesOptions{
		Mode:          "console",
		AgentNameFunc: func() string { return personautil.LoadAgentName(statepaths.FileStateDir()) },
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
				"poke_enabled":      r.canPokeHeartbeat(),
				"heartbeat_running": r.heartbeatRunning(),
			}, nil
		},
		Poke: poke,
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
	trigger := normalizeConsoleTrigger(req.Trigger, daemonruntime.TaskTrigger{
		Source: "ui",
		Event:  "chat_submit",
		Ref:    "web/console",
	})
	task := strings.TrimSpace(req.Task)
	if output, handled := r.handleConsoleModelCommand(task); handled {
		return r.submitSyntheticTask(task, output, timeout, strings.TrimSpace(req.TopicID), strings.TrimSpace(req.TopicTitle), trigger)
	}
	model := strings.TrimSpace(req.Model)
	return r.submitTaskViaBus(
		ctx,
		task,
		model,
		timeout,
		strings.TrimSpace(req.TopicID),
		strings.TrimSpace(req.TopicTitle),
		trigger,
	)
}

func (r *consoleLocalRuntime) handleConsoleModelCommand(task string) (string, bool) {
	output, handled, err := llmselect.ExecuteCommandText(llmutil.RuntimeValuesFromViper(), llmselect.ProcessStore(), task)
	if !handled {
		return "", false
	}
	if err != nil {
		return "error: " + strings.TrimSpace(err.Error()), true
	}
	return output, true
}

func (r *consoleLocalRuntime) submitSyntheticTask(task string, output string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	job, _, err := r.acceptTask(task, "", timeout, topicID, topicTitle, trigger)
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	finishedAt := time.Now().UTC()
	r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = map[string]any{
			"final": map[string]any{
				"output": strings.TrimSpace(output),
			},
		}
	})
	return daemonruntime.SubmitTaskResponse{
		ID:      job.TaskID,
		Status:  daemonruntime.TaskDone,
		TopicID: job.TopicID,
	}, nil
}

func (r *consoleLocalRuntime) enqueueTask(ctx context.Context, task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	job, resp, err := r.acceptTask(task, model, timeout, topicID, topicTitle, trigger)
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	err = r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	})
	if err != nil {
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return resp, nil
}

func (r *consoleLocalRuntime) handleTaskJob(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
	if r == nil {
		return
	}
	runtimecore.MarkTaskRunning(r.store, job.TaskID)
	if r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskRunning))
	}
	if r.logger != nil {
		r.logger.Info("console_stream_enabled",
			"task_id", job.TaskID,
			"conversation_key", conversationKey,
			"topic_id", strings.TrimSpace(job.TopicID),
			"model", strings.TrimSpace(job.Model),
		)
	}

	replySink := newConsoleReplySink(r.streamHub, job.TaskID, r.logger)
	eventSink := newConsoleEventPreviewSink(r.streamHub, job.TaskID, r.logger)
	if bundle := r.currentBundle(); bundle != nil {
		eventSink.observer = newConsoleLLMObserver(bundle.taskRuntime, job.Model, r.logger)
	}
	streamer := streaming.NewFinalOutputStreamer(streaming.FinalOutputStreamerOptions{
		Sink: replySink,
	})
	streamTracker := newConsoleStreamTracker(r.logger, job.TaskID)
	onStream := func(event llm.StreamEvent) error {
		return streamTracker.Handle(event, streamer.Handle)
	}

	runCtx, cancel := context.WithTimeout(workerCtx, job.Timeout)
	runCtx = agent.WithEventSinkContext(runCtx, eventSink)
	final, agentCtx, runErr := r.runTask(runCtx, conversationKey, job, onStream)
	contextDeadline := daemonruntime.IsContextDeadline(runCtx, runErr)
	cancel()

	if runErr != nil {
		eventSink.Close()
		displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(runErr))
		if displayErr == "" {
			displayErr = strings.TrimSpace(runErr.Error())
		}
		_ = replySink.Abort(context.Background(), errors.New(displayErr))
		streamTracker.LogSummary("failed")
		r.completeHeartbeatTask(job, heartbeatTaskResultFailure, errors.New(displayErr), time.Time{})
		runtimecore.MarkTaskFailed(r.store, job.TaskID, displayErr, contextDeadline)
		return
	}

	if pendingID, ok := pendingApprovalID(final); ok {
		eventSink.Close()
		if r.streamHub != nil {
			r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskPending))
		}
		pendingAt := time.Now().UTC()
		r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskPending
			info.PendingAt = &pendingAt
			info.ApprovalRequestID = pendingID
			info.Result = buildConsoleTaskResult(final, agentCtx)
		})
		streamTracker.LogSummary("pending")
		r.completeHeartbeatTask(job, heartbeatTaskResultSkipped, nil, time.Time{})
		return
	}

	finishedAt := time.Now().UTC()
	output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	eventSink.Close()
	_ = replySink.Finalize(context.Background(), output)
	streamTracker.LogSummary("done")
	r.completeHeartbeatTask(job, heartbeatTaskResultSuccess, nil, finishedAt)
	r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = buildConsoleTaskResult(final, agentCtx)
	})
	r.maybeRefreshTopicTitle(job, output)
}

func (r *consoleLocalRuntime) runTask(ctx context.Context, conversationKey string, job consoleLocalTaskJob, onStream llm.StreamHandler) (*agent.Final, *agent.Context, error) {
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
	historyMsgs, currentMsg, err := r.buildConsolePromptMessages(job)
	if err != nil {
		return nil, nil, err
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
	meta := map[string]any{
		"trigger":          consoleTriggerSource(job.Trigger),
		"console_task_id":  job.TaskID,
		"console_topic_id": strings.TrimSpace(job.TopicID),
	}
	if pokeMeta := job.WakeSignal.MetaValue(); pokeMeta != nil {
		meta["poke"] = pokeMeta
	}
	var promptAugment taskruntime.PromptAugmentFunc
	if !job.WakeSignal.IsZero() {
		wakeSignal := job.WakeSignal.Normalize()
		promptAugment = func(spec *agent.PromptSpec, _ *tools.Registry) {
			promptprofile.AppendWakeSignalBlock(spec, wakeSignal)
		}
	}
	bundle := r.currentBundle()
	if bundle == nil || bundle.taskRuntime == nil {
		return nil, nil, fmt.Errorf("console task runtime is not initialized")
	}
	result, err := bundle.taskRuntime.Run(ctx, taskruntime.RunRequest{
		Task:           task,
		Model:          model,
		Scene:          "console.loop",
		History:        historyMsgs,
		CurrentMessage: currentMsg,
		OnStream:       onStream,
		Meta:           meta,
		PromptAugment:  promptAugment,
		Memory:         memoryHooks,
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
		out["metrics"] = buildConsoleTaskMetrics(runCtx.Metrics)
		out["steps"] = summarizeConsoleSteps(runCtx)
	}
	return out
}

// Keep Metrics untagged for resume-state compatibility and normalize only the
// console task projection that is exposed via task logs and APIs.
func buildConsoleTaskMetrics(metrics *agent.Metrics) map[string]any {
	if metrics == nil {
		return nil
	}
	return map[string]any{
		"llm_rounds":    metrics.LLMRounds,
		"total_tokens":  metrics.TotalTokens,
		"total_cost":    metrics.TotalCost,
		"start_time":    metrics.StartTime,
		"elapsed_ms":    metrics.ElapsedMs,
		"tool_calls":    metrics.ToolCalls,
		"parse_retries": metrics.ParseRetries,
	}
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
	topic, ok := r.store.GetTopic(topicID)
	if !ok || topic == nil {
		return
	}
	if topic.LLMTitleGeneratedAt != nil {
		return
	}
	if title := consoleTopicTitleFromOutput(finalOutput); title != "" {
		if err := r.store.SetTopicTitle(topicID, title); err != nil {
			r.logger.Debug("console_topic_title_update_failed", "topic_id", topicID, "error", err.Error())
		}
		return
	}
	if strings.TrimSpace(finalOutput) == "" {
		return
	}

	go func() {
		if current, ok := r.store.GetTopic(topicID); ok && current != nil && current.LLMTitleGeneratedAt != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), consoleTopicTitleTimeout)
		defer cancel()

		title, err := r.generateTopicTitle(ctx, taskText, finalOutput)
		if err != nil {
			r.logger.Debug("console_topic_title_generate_failed", "topic_id", topicID, "error", err.Error())
			return
		}
		if err := r.store.SetTopicTitleFromLLM(topicID, title); err != nil {
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

func consoleTopicTitleFromOutput(output string) string {
	output = strings.Join(strings.Fields(strings.TrimSpace(output)), " ")
	if output == "" {
		return ""
	}
	if utf8.RuneCountInString(output) > consoleTopicTitleDirectOutputMaxRunes {
		return ""
	}
	return sanitizeConsoleTopicTitle(output)
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

type heartbeatTaskResult int

const (
	heartbeatTaskResultSuccess heartbeatTaskResult = iota
	heartbeatTaskResultFailure
	heartbeatTaskResultSkipped
)

func isHeartbeatTaskJob(job consoleLocalTaskJob) bool {
	return strings.EqualFold(strings.TrimSpace(job.Trigger.Source), "heartbeat")
}

func (r *consoleLocalRuntime) heartbeatRunning() bool {
	if r == nil || r.heartbeatState == nil {
		return false
	}
	_, _, _, running := r.heartbeatState.Snapshot()
	return running
}

func (r *consoleLocalRuntime) completeHeartbeatTask(
	job consoleLocalTaskJob,
	result heartbeatTaskResult,
	runErr error,
	now time.Time,
) {
	if r == nil || r.heartbeatState == nil || !isHeartbeatTaskJob(job) {
		return
	}
	switch result {
	case heartbeatTaskResultSuccess:
		if now.IsZero() {
			now = time.Now().UTC()
		}
		r.heartbeatState.EndSuccess(now)
	case heartbeatTaskResultSkipped:
		r.heartbeatState.EndSkipped()
	default:
		if runErr == nil {
			runErr = errors.New("heartbeat task failed")
		}
		r.heartbeatState.EndFailure(runErr)
	}
}

func (r *consoleLocalRuntime) enqueueHeartbeatTask(ctx context.Context, task string, _ bool, wakeSignal daemonruntime.PokeInput) string {
	if r == nil {
		return "runtime_unavailable"
	}
	model := func() string {
		_, model := r.defaultLLMConfig()
		return model
	}()
	trigger := daemonruntime.TaskTrigger{
		Source: "heartbeat",
		Event:  "heartbeat_tick",
		Ref:    "console",
	}
	if !wakeSignal.IsZero() {
		trigger.Event = "heartbeat_poke"
		trigger.Ref = "console/poke"
	}
	job, _, err := r.acceptTask(
		task,
		model,
		r.defaultTimeout,
		r.store.HeartbeatTopicID(),
		daemonruntime.ConsoleHeartbeatTopicTitle,
		trigger,
	)
	if err != nil {
		return err.Error()
	}
	job.WakeSignal = wakeSignal.Normalize()
	if err := r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	}); err != nil {
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return err.Error()
	}
	return ""
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
	r.heartbeatState = hbState
	hbChecklist := statepaths.HeartbeatChecklistPath()
	r.heartbeatPokeRequests = make(chan heartbeatloop.PokeRequest)

	runHeartbeatTick := func(wakeSignal daemonruntime.PokeInput) heartbeatutil.TickResult {
		if !r.canSubmit() {
			return heartbeatutil.TickResult{
				Outcome:    heartbeatutil.TickSkipped,
				SkipReason: consoleHeartbeatSkipNoLLM,
			}
		}
		result := heartbeatutil.Tick(
			hbState,
			func() (string, bool, error) {
				return heartbeatutil.BuildHeartbeatTask(hbChecklist)
			},
			func(task string, checklistEmpty bool) string {
				return r.enqueueHeartbeatTask(context.Background(), task, checklistEmpty, wakeSignal)
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
			if result.SkipReason == consoleHeartbeatSkipNoLLM {
				break
			}
			r.logger.Debug("heartbeat_skip", "source", "console", "reason", result.SkipReason)
		}
		return result
	}

	go func() {
		heartbeatloop.RunScheduler(ctx, heartbeatloop.SchedulerOptions{
			InitialDelay: 15 * time.Second,
			Interval:     hbInterval,
			PokeRequests: r.heartbeatPokeRequests,
		}, runHeartbeatTick)
	}()
}

func buildConsoleMemoryRecordRequest(job consoleLocalTaskJob, subjectID, output string) memoryruntime.RecordRequest {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		subjectID = buildConsoleConversationKey(daemonruntime.ConsoleDefaultTopicID)
	}
	now := time.Now().UTC()
	inbound := newConsoleInboundHistoryItem(job)
	inbound.ChatID = subjectID
	outbound := chathistory.ChatHistoryItem{
		Channel:          consoleHistoryChannel,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           subjectID,
		ChatType:         "private",
		ReplyToMessageID: job.TaskID,
		SentAt:           now,
		Sender: chathistory.ChatHistorySender{
			UserID:     consoleAgentUserID,
			Username:   consoleAgentUsername,
			Nickname:   consoleAgentNickname,
			IsBot:      true,
			DisplayRef: consoleAgentUsername,
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
