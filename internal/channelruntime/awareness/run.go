package awareness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
)

type RunOptions struct {
	Interval                time.Duration
	TaskTimeout             time.Duration
	RequestTimeout          time.Duration
	AgentLimits             agent.Limits
	EngineToolsConfig       agent.EngineToolsConfig
	Source                  string
	ChecklistPath           string
	DisableHeartbeat        bool
	MemoryEnabled           bool
	MemoryShortTermDays     int
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	InspectPrompt           bool
	InspectRequest          bool
	Notifier                Notifier
	PokeRequests            <-chan PokeRequest
	CronRequests            <-chan CronRequest
	CronEnabled             bool
	CronPath                string
	ChatInfoContactsDir     string
	ChatInfoStore           *chatinfo.Store
	ChatInfoRefresher       chatinfo.Refresher
	TaskStore               daemonruntime.TaskView
}

type Dependencies = depsutil.CommonDependencies

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	return runAwarenessLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}

func runAwarenessLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	logger, err := depsutil.LoggerFromCommon(d)
	if err != nil {
		return err
	}
	logOpts := depsutil.LogOptionsFromCommon(d)

	route, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposeAwareness)
	if err != nil {
		return err
	}
	systemPromptCacheControl, err := llmutil.SystemPromptCacheControl(route.Values.CacheTTL)
	if err != nil {
		return err
	}
	client, err := depsutil.CreateClient(d.CreateLLMClient, route)
	if err != nil {
		return err
	}
	model := strings.TrimSpace(route.ClientConfig.Model)
	if model == "" {
		return fmt.Errorf("missing model")
	}
	inspectors, err := newAwarenessInspectors(opts)
	if err != nil {
		return err
	}
	defer func() {
		if inspectors != nil {
			_ = inspectors.Close()
		}
	}()
	client = inspectors.Wrap(client, route)

	baseReg := depsutil.Registry(d.AwarenessRegistry)
	if baseReg == nil {
		baseReg = depsutil.RegistryFromCommon(d)
	}
	if baseReg == nil {
		return fmt.Errorf("base registry is nil")
	}
	sharedGuard := depsutil.GuardFromCommon(d, logger)
	cfg := opts.AgentLimits.ToConfig()

	orchestrator, projectionWorker, cleanup, err := newAwarenessOrchestrator(ctx, d, opts, inspectors.Wrap)
	if err != nil {
		return err
	}
	defer cleanup()
	if projectionWorker != nil {
		projectionWorker.Start(ctx)
	}

	state := &awarenessutil.State{}
	var wg sync.WaitGroup
	chatInfoStore, chatInfoRefresher := resolveChatInfoRuntime(opts)
	if opts.CronEnabled {
		refreshChatInfoOnStart(ctx, chatInfoStore, chatInfoRefresher, opts.ChatInfoContactsDir, logger)
	}

	runAwarenessTaskWithOpts := func(behavior awarenessutil.Behavior, task string, meta map[string]any, taskRunID string, bashEnv []cronstore.BashEnvRef) (string, error) {
		return runAwarenessTask(ctx, d, awarenessTaskOptions{
			Behavior:                 behavior,
			Logger:                   logger,
			LogOptions:               logOpts,
			Client:                   client,
			Model:                    model,
			Task:                     task,
			Meta:                     meta,
			TaskRunID:                taskRunID,
			BaseRegistry:             baseReg,
			SharedGuard:              sharedGuard,
			Config:                   cfg,
			EngineToolsConfig:        opts.EngineToolsConfig,
			TaskTimeout:              opts.TaskTimeout,
			SystemPromptCacheControl: systemPromptCacheControl,
			ClientDecorator:          inspectors.Wrap,
			MemoryOrchestrator:       orchestrator,
			MemoryProjectionWorker:   projectionWorker,
			MemoryInjectionEnabled:   opts.MemoryInjectionEnabled,
			MemoryInjectionMaxItems:  opts.MemoryInjectionMaxItems,
			ImageClient:              nil,
			TaskStore:                opts.TaskStore,
			BashEnv:                  bashEnv,
		})
	}
	runTask := func(behavior awarenessutil.Behavior, task string, meta map[string]any, taskRunID string) (string, error) {
		return runAwarenessTaskWithOpts(behavior, task, meta, taskRunID, nil)
	}

	runTaskAsync := func(behavior awarenessutil.Behavior, task string, taskEmpty bool, wakeSignal daemonruntime.PokeInput) string {
		if ctx.Err() != nil {
			return "context_canceled"
		}
		runAt := time.Now().UTC()
		taskRunID := awarenessTaskRunID(behavior, runAt)
		extra := map[string]any{
			"task_run_id": taskRunID,
		}
		meta := awarenessutil.BuildAwarenessMeta(behavior, opts.Source, opts.Interval, opts.ChecklistPath, taskEmpty, wakeSignal, extra)
		wg.Add(1)
		go func() {
			defer wg.Done()

			summary, runErr := runTask(behavior, task, meta, taskRunID)
			if runErr != nil {
				displayErr := depsutil.FormatRuntimeError(runErr)
				alert, alertMsg := state.EndFailure(errors.New(displayErr))
				if alert {
					logger.Warn("awareness_alert", "source", opts.Source, "behavior", behavior, "message", alertMsg)
					notifyAwareness(ctx, opts.Notifier, logger, alertMsg)
					return
				}
				logger.Warn("awareness_error", "source", opts.Source, "behavior", behavior, "error", displayErr)
				return
			}
			state.EndSuccess(time.Now())
			if summary == "" {
				summary = "empty"
			}
			logger.Info("awareness_summary", "source", opts.Source, "behavior", behavior, "message", summary)
		}()
		return ""
	}

	runTick := func(behavior awarenessutil.Behavior, wakeSignal daemonruntime.PokeInput) awarenessutil.TickResult {
		result := awarenessutil.Tick(
			state,
			behavior,
			func() (string, bool, error) {
				return awarenessutil.BuildAwarenessTask(behavior, opts.ChecklistPath, wakeSignal)
			},
			func(task string, taskEmpty bool) string {
				return runTaskAsync(behavior, task, taskEmpty, wakeSignal)
			},
		)
		switch result.Outcome {
		case awarenessutil.TickBuildError:
			if strings.TrimSpace(result.AlertMessage) != "" {
				logger.Warn("awareness_alert", "source", opts.Source, "behavior", behavior, "message", result.AlertMessage)
				notifyAwareness(ctx, opts.Notifier, logger, result.AlertMessage)
			} else if result.BuildError != nil {
				logger.Warn("awareness_task_error", "source", opts.Source, "behavior", behavior, "error", result.BuildError.Error())
			}
		case awarenessutil.TickSkipped:
			logger.Debug("awareness_skip", "source", opts.Source, "behavior", behavior, "reason", result.SkipReason)
		}
		return result
	}

	if opts.CronEnabled {
		cronPath := strings.TrimSpace(opts.CronPath)
		systemTasks := []cronstore.Task{}
		if !opts.DisableHeartbeat && opts.Interval > 0 {
			heartbeatCron, usedInterval, fallbackUsed, ok := cronstore.HeartbeatIntervalScheduleWithFallback(opts.Interval, DefaultHeartbeatInterval)
			if ok {
				if fallbackUsed {
					logger.Warn("heartbeat_interval_fallback", "source", opts.Source, "interval", opts.Interval.String(), "fallback_interval", usedInterval.String(), "cron", heartbeatCron)
				}
				systemTasks = append(systemTasks, cronstore.HeartbeatTask(heartbeatCron))
			} else {
				logger.Warn("heartbeat_interval_invalid", "source", opts.Source, "interval", opts.Interval.String())
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			RunCronLoop(ctx, CronLoopOptions{
				Logger:      logger,
				Source:      opts.Source,
				Path:        cronPath,
				SystemTasks: systemTasks,
				Requests:    opts.CronRequests,
				Run: func(ctx context.Context, due cronstore.DueTask) error {
					task := due.Task
					if strings.TrimSpace(task.ID) == cronstore.HeartbeatTaskID {
						taskText, empty, err := awarenessutil.BuildHeartbeatTask(opts.ChecklistPath)
						if err != nil {
							return err
						}
						if empty || strings.TrimSpace(taskText) == "" {
							logger.Debug("awareness_skip", "source", opts.Source, "behavior", awarenessutil.BehaviorHeartbeat, "reason", awarenessutil.SkipReasonEmptyTask)
							return nil
						}
						taskRunID := awarenessTaskRunID(awarenessutil.BehaviorHeartbeat, time.Now().UTC())
						meta := awarenessutil.BuildAwarenessMeta(awarenessutil.BehaviorHeartbeat, opts.Source, opts.Interval, opts.ChecklistPath, false, daemonruntime.PokeInput{}, map[string]any{
							"task_run_id":           taskRunID,
							"cron_task_id":          cronstore.HeartbeatTaskID,
							"cron_schedule":         cronstore.ScheduleForTask(task),
							"cron_scheduled_at_utc": due.ScheduledAtUTC.UTC().Format(time.RFC3339),
							"runtime_source":        strings.TrimSpace(opts.Source),
						})
						summary, err := runTask(awarenessutil.BehaviorHeartbeat, strings.TrimSpace(taskText), meta, taskRunID)
						if err != nil {
							return err
						}
						if summary == "" {
							summary = "empty"
						}
						logger.Info("awareness_summary", "source", opts.Source, "behavior", awarenessutil.BehaviorHeartbeat, "task_id", cronstore.HeartbeatTaskID, "message", summary)
						return nil
					}
					taskRunID := awarenessTaskRunID(awarenessutil.BehaviorCron, time.Now().UTC())
					extra := map[string]any{
						"task_run_id":    taskRunID,
						"runtime_source": strings.TrimSpace(opts.Source),
					}
					if notifyTarget := buildCronNotifyTargetForTask(ctx, task, time.Now().UTC(), chatInfoStore, chatInfoRefresher, logger); notifyTarget != nil {
						extra["notify_target"] = notifyTarget
					}
					meta := awarenessutil.BuildCronMeta("cron", strings.TrimSpace(task.ID), due.ScheduledAtUTC, cronstore.ScheduleForTask(task), strings.TrimSpace(task.TZ), strings.TrimSpace(task.ChatID), extra)
					summary, err := runAwarenessTaskWithOpts(awarenessutil.BehaviorCron, strings.TrimSpace(task.Content), meta, taskRunID, task.BashEnv)
					if err != nil {
						return err
					}
					if summary == "" {
						summary = "empty"
					}
					logger.Info("awareness_summary", "source", opts.Source, "behavior", awarenessutil.BehaviorCron, "task_id", strings.TrimSpace(task.ID), "message", summary)
					if !due.Manual && strings.TrimSpace(task.At) != "" {
						if _, deleteErr := cronstore.NewStore(cronPath).DeleteByID(task.ID); deleteErr != nil {
							return deleteErr
						}
					}
					return nil
				},
			})
		}()
	}

	RunPokeLoop(ctx, opts.PokeRequests, func(input daemonruntime.PokeInput) awarenessutil.TickResult {
		return runTick(awarenessutil.BehaviorPoke, input)
	})
	wg.Wait()
	return nil
}

type awarenessTaskOptions struct {
	Behavior                 awarenessutil.Behavior
	Logger                   *slog.Logger
	LogOptions               agent.LogOptions
	Client                   llm.Client
	Model                    string
	Task                     string
	Meta                     map[string]any
	TaskRunID                string
	BaseRegistry             *tools.Registry
	SharedGuard              *guard.Guard
	Config                   agent.Config
	EngineToolsConfig        agent.EngineToolsConfig
	TaskTimeout              time.Duration
	SystemPromptCacheControl *llm.CacheControl
	ClientDecorator          func(llm.Client, llmutil.ResolvedRoute) llm.Client
	MemoryOrchestrator       *memoryruntime.Orchestrator
	MemoryProjectionWorker   *memoryruntime.ProjectionWorker
	MemoryInjectionEnabled   bool
	MemoryInjectionMaxItems  int
	ImageClient              llm.ImageClient
	TaskStore                daemonruntime.TaskView
	BashEnv                  []cronstore.BashEnvRef
}

func runAwarenessTask(ctx context.Context, d Dependencies, opts awarenessTaskOptions) (summary string, runErr error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return "", fmt.Errorf("awareness task is empty")
	}
	routePurpose := ""
	reasoningEffort := ""
	if thinkTask, ok := chatcommands.ExtractThinkTask(task); ok {
		task = strings.TrimSpace(thinkTask)
		routePurpose = llmutil.RoutePurposeThink
		reasoningEffort = llmutil.ReasoningEffortXHigh
		if task == "" {
			return "", fmt.Errorf("awareness task is empty")
		}
	}

	taskClient := opts.Client
	taskModel := strings.TrimSpace(opts.Model)
	systemPromptCacheControl := opts.SystemPromptCacheControl
	if routePurpose != "" {
		route, err := depsutil.ResolveLLMRouteFromCommon(d, routePurpose)
		if err != nil {
			return "", err
		}
		if reasoningEffort != "" {
			route = llmutil.ResolvedRouteWithReasoningEffort(route, reasoningEffort)
		}
		taskClient, err = depsutil.CreateClientFromCommon(d, route)
		if err != nil {
			return "", err
		}
		defer closeAwarenessTaskClient(opts.Logger, taskClient)
		if opts.ClientDecorator != nil {
			taskClient = opts.ClientDecorator(taskClient, route)
		}
		taskModel = strings.TrimSpace(route.ClientConfig.Model)
		systemPromptCacheControl, err = llmutil.SystemPromptCacheControl(route.Values.CacheTTL)
		if err != nil {
			return "", err
		}
	}
	recordOpts := opts
	recordOpts.Model = taskModel
	recordAwarenessTaskStart(recordOpts, task, time.Now().UTC())
	defer func() {
		recordAwarenessTaskFinish(recordOpts, summary, runErr, time.Now().UTC())
	}()

	runCtx := ctx
	cancel := func() {}
	if opts.TaskTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.TaskTimeout)
	}
	defer cancel()

	var toolTriggers map[string]bool
	if d.ToolTriggers != nil {
		toolTriggers = d.ToolTriggers(task)
	}
	if len(depsutil.ACPAgentsFromCommon(d)) == 0 {
		delete(toolTriggers, toolsutil.BuiltinACPSpawn)
	}
	if len(opts.BashEnv) > 0 {
		if toolTriggers == nil {
			toolTriggers = make(map[string]bool)
		}
		toolTriggers[toolsutil.BuiltinBash] = true
	}
	promptSpec, _, err := depsutil.PromptSpecFromCommon(d, runCtx, opts.Logger, opts.LogOptions, task, taskClient, taskModel, nil)
	if err != nil {
		return "", err
	}
	promptSpec.FinalOnlyResponse = true

	reg := cloneRegistry(opts.BaseRegistry)
	if d.RegisterTriggeredStaticTools != nil && len(toolTriggers) > 0 {
		d.RegisterTriggeredStaticTools(reg, toolTriggers)
	}
	if len(opts.BashEnv) > 0 {
		injected, err := cronstore.ResolveBashEnvRefsWithOptions(opts.BashEnv, cronstore.BashEnvResolveOptions{
			Warnf: func(format string, args ...any) {
				if opts.Logger != nil {
					opts.Logger.Warn("cron_bash_env_secret_ref_warning", "warning", fmt.Sprintf(format, args...))
				}
			},
		})
		if err != nil {
			return "", err
		}
		if err := toolsutil.PatchBashInjectedEnv(reg, injected); err != nil {
			return "", err
		}
	}
	imageClient := opts.ImageClient
	imageToolTriggered := toolTriggers[toolsutil.BuiltinImageGenerate] || toolTriggers[toolsutil.BuiltinImageEdit]
	if d.RuntimeToolsConfig.Image.Configured && imageClient == nil && imageToolTriggered {
		if d.CreateImageClient != nil {
			var imageErr error
			imageClient, imageErr = d.CreateImageClient()
			if imageErr != nil && opts.Logger != nil {
				opts.Logger.Warn("image_client_create_failed", "error", imageErr.Error())
			}
		}
	}
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient: taskClient,
		DefaultModel:  taskModel,
		ImageClient:   imageClient,
		ToolTriggers:  toolTriggers,
	})
	promptprofile.ApplyPersonaIdentity(&promptSpec, opts.Logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	memoryContext := ""
	if opts.MemoryOrchestrator != nil && opts.MemoryInjectionEnabled {
		snap, memErr := opts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      awarenessMemorySubjectID,
			RequestContext: memory.ContextPrivate,
			MaxItems:       opts.MemoryInjectionMaxItems,
		})
		if memErr != nil {
			if opts.Logger != nil {
				opts.Logger.Warn("memory_injection_error", "source", "awareness", "error", memErr.Error())
			}
		} else if strings.TrimSpace(snap) != "" {
			memoryContext = snap
		}
	}
	depsutil.PromptAugmentFromCommon(d, &promptSpec, reg)
	promptprofile.AppendAwarenessPromptPatch(&promptSpec)
	promptprofile.AppendModelPromptPatches(&promptSpec, taskModel, opts.Logger)
	engineToolsConfig := opts.EngineToolsConfig
	engineToolsConfig.ToolTriggers = toolTriggers
	engineToolsConfig.PathRoots = pathroots.New(
		engineToolsConfig.PathRoots.WorkspaceDir,
		firstNonEmpty(engineToolsConfig.PathRoots.FileCacheDir, d.RuntimeToolsConfig.Image.FileCacheDir),
		firstNonEmpty(engineToolsConfig.PathRoots.FileStateDir, d.RuntimeToolsConfig.Image.FileStateDir),
	)

	engine := agent.New(
		taskClient,
		reg,
		opts.Config,
		promptSpec,
		agent.WithLogger(opts.Logger),
		agent.WithLogOptions(opts.LogOptions),
		agent.WithEngineToolsConfig(engineToolsConfig),
		agent.WithACPAgents(depsutil.ACPAgentsFromCommon(d)),
		agent.WithSystemPromptCacheControl(systemPromptCacheControl),
		agent.WithGuard(opts.SharedGuard),
	)
	final, _, err := engine.Run(runCtx, task, agent.RunOptions{
		Model: taskModel,
		Scene: "awareness." + string(opts.Behavior),
		Meta: taskruntime.ApplyObservationMeta(opts.Meta, taskruntime.ObservationMetaIDs{
			TaskID:  opts.TaskRunID,
			TraceID: opts.TaskRunID,
		}),
		MemoryContext: memoryContext,
	})
	if err != nil {
		return "", err
	}

	summary = strings.TrimSpace(depsutil.FormatFinalOutput(final))
	if opts.MemoryOrchestrator != nil {
		if memErr := opts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
			TaskRunID:    opts.TaskRunID,
			SessionID:    awarenessMemorySessionID,
			SubjectID:    awarenessMemorySubjectID,
			Channel:      "awareness",
			Participants: awarenessMemoryParticipants(),
			TaskText:     task,
			FinalOutput:  summary,
			SessionContext: memory.SessionContext{
				ConversationID: awarenessMemorySubjectID,
			},
		}); memErr != nil && opts.Logger != nil {
			opts.Logger.Warn("memory_record_error", "source", "awareness", "error", memErr.Error())
		} else if opts.MemoryProjectionWorker != nil {
			opts.MemoryProjectionWorker.NotifyRecordAppended()
		}
	}

	return summary, nil
}

func closeAwarenessTaskClient(logger *slog.Logger, client llm.Client) {
	if client == nil {
		return
	}
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil && logger != nil {
		logger.Warn("awareness_task_client_close_failed", "error", err.Error())
	}
}

func recordAwarenessTaskStart(opts awarenessTaskOptions, task string, now time.Time) {
	if opts.TaskStore == nil {
		return
	}
	taskID := strings.TrimSpace(opts.TaskRunID)
	if taskID == "" {
		return
	}
	startedAt := now.UTC()
	info := daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskRunning,
		Task:      strings.TrimSpace(task),
		Model:     strings.TrimSpace(opts.Model),
		CreatedAt: startedAt,
		StartedAt: &startedAt,
		TopicID:   daemonruntime.ConsoleAwarenessTopicID,
	}
	if opts.TaskTimeout > 0 {
		info.Timeout = opts.TaskTimeout.String()
	}
	trigger := awarenessTaskTrigger(opts)
	err := daemonruntime.RecordTaskUpsert(opts.TaskStore, info, trigger)
	if err != nil && opts.Logger != nil {
		opts.Logger.Warn("awareness_task_record_error", "task_id", taskID, "error", err.Error())
	}
}

func recordAwarenessTaskFinish(opts awarenessTaskOptions, summary string, runErr error, now time.Time) {
	if opts.TaskStore == nil {
		return
	}
	taskID := strings.TrimSpace(opts.TaskRunID)
	if taskID == "" {
		return
	}
	finishedAt := now.UTC()
	trigger := awarenessTaskTrigger(opts)
	err := daemonruntime.RecordTaskUpdate(opts.TaskStore, taskID, trigger, func(info *daemonruntime.TaskInfo) {
		info.FinishedAt = &finishedAt
		if runErr != nil {
			info.Status = daemonruntime.TaskFailed
			info.Error = depsutil.FormatRuntimeError(runErr)
			return
		}
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.Result = map[string]any{
			"final": map[string]any{
				"output": strings.TrimSpace(summary),
			},
		}
	})
	if err != nil && opts.Logger != nil {
		opts.Logger.Warn("awareness_task_record_error", "task_id", taskID, "error", err.Error())
	}
}

func awarenessTaskTrigger(opts awarenessTaskOptions) daemonruntime.TaskTrigger {
	return daemonruntime.TaskTrigger{
		Source:  "console",
		Event:   "awareness_" + string(awarenessutil.NormalizeBehavior(string(opts.Behavior))),
		TraceID: strings.TrimSpace(opts.TaskRunID),
	}
}

func notifyAwareness(ctx context.Context, notifier Notifier, logger *slog.Logger, message string) {
	if notifier == nil {
		return
	}
	text := strings.TrimSpace(message)
	if text == "" || strings.HasPrefix(text, "ALERT:") {
		return
	}
	if err := notifier.Notify(ctx, text); err != nil && logger != nil {
		logger.Warn("awareness_notify_error", "error", err.Error())
	}
}

func cloneRegistry(base *tools.Registry) *tools.Registry {
	reg := tools.NewRegistry()
	if base == nil {
		return reg
	}
	for _, t := range base.All() {
		reg.Register(t)
	}
	return reg
}

type awarenessInspectors struct {
	prompt  *llminspect.PromptInspector
	request *llminspect.RequestInspector
}

func newAwarenessInspectors(opts runtimeLoopOptions) (*awarenessInspectors, error) {
	out := &awarenessInspectors{}
	if opts.InspectRequest {
		requestInspector, err := llminspect.NewRequestInspector(llminspect.Options{
			Mode:            awarenessInspectMode(opts.Source),
			Task:            "awareness",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return nil, err
		}
		out.request = requestInspector
	}
	if opts.InspectPrompt {
		promptInspector, err := llminspect.NewPromptInspector(llminspect.Options{
			Mode:            awarenessInspectMode(opts.Source),
			Task:            "awareness",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			_ = out.Close()
			return nil, err
		}
		out.prompt = promptInspector
	}
	return out, nil
}

func (i *awarenessInspectors) Wrap(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
	if i == nil {
		return client
	}
	return llminspect.WrapClient(client, llminspect.ClientOptions{
		PromptInspector:  i.prompt,
		RequestInspector: i.request,
		APIBase:          route.ClientConfig.Endpoint,
		Model:            strings.TrimSpace(route.ClientConfig.Model),
	})
}

func (i *awarenessInspectors) Close() error {
	if i == nil {
		return nil
	}
	return errors.Join(closePromptInspector(i.prompt), closeRequestInspector(i.request))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func closePromptInspector(inspector *llminspect.PromptInspector) error {
	if inspector == nil {
		return nil
	}
	return inspector.Close()
}

func closeRequestInspector(inspector *llminspect.RequestInspector) error {
	if inspector == nil {
		return nil
	}
	return inspector.Close()
}

func awarenessInspectMode(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "", "heartbeat":
		return "awareness"
	default:
		return "awareness_" + source
	}
}

func newAwarenessOrchestrator(ctx context.Context, common depsutil.CommonDependencies, opts runtimeLoopOptions, decorateClient func(client llm.Client, route llmutil.ResolvedRoute) llm.Client) (*memoryruntime.Orchestrator, *memoryruntime.ProjectionWorker, func(), error) {
	memRuntime, err := runtimecore.NewMemoryRuntime(common, runtimecore.MemoryRuntimeOptions{
		Enabled:       opts.MemoryEnabled,
		ShortTermDays: opts.MemoryShortTermDays,
		Decorate:      decorateClient,
	})
	if err != nil {
		return nil, nil, func() {}, err
	}
	return memRuntime.Orchestrator, memRuntime.ProjectionWorker, memRuntime.Cleanup, nil
}
