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
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type RunOptions struct {
	Interval            time.Duration
	TaskTimeout         time.Duration
	RequestTimeout      time.Duration
	AgentLimits         agent.Limits
	EngineToolsConfig   agent.EngineToolsConfig
	Source              string
	ChecklistPath       string
	DisableHeartbeat    bool
	InspectPrompt       bool
	InspectRequest      bool
	Notifier            Notifier
	CronNotify          CronNotifyFunc
	PokeRequests        <-chan PokeRequest
	CronRequests        <-chan CronRequest
	CronEnabled         bool
	CronPath            string
	ChatInfoContactsDir string
	ChatInfoStore       *chatinfo.Store
	ChatInfoRefresher   chatinfo.Refresher
	TaskStore           daemonruntime.TaskView
}

type Dependencies = depsutil.CommonDependencies

func awarenessTaskRunID(behavior awarenessutil.Behavior, now time.Time) string {
	return fmt.Sprintf("%s:%s", behavior, now.UTC().Format("20060102T150405.000000000Z07:00"))
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	if err := d.Validate(); err != nil {
		return err
	}
	return runAwarenessLoop(ctx, d, normalizeRunOptions(opts, d.RuntimePaths))
}

func runAwarenessLoop(ctx context.Context, d Dependencies, opts RunOptions) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger, err := d.Logger()
	if err != nil {
		return err
	}
	logOpts := agent.LogOptions{}
	if d.LogOptions != nil {
		logOpts = d.LogOptions()
	}

	route, err := d.ResolveLLMRoute(llmutil.RoutePurposeAwareness)
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
	var (
		client                   llm.Client
		systemPromptCacheControl *llm.CacheControl
	)
	if len(route.Candidates) == 0 {
		systemPromptCacheControl, err = llmutil.SystemPromptCacheControl(route.Values.CacheTTL)
		if err != nil {
			return err
		}
		client, err = d.CreateLLMClient(route)
		if err != nil {
			var closeErr error
			if closer, ok := client.(io.Closer); ok {
				closeErr = closer.Close()
			}
			if closeErr != nil {
				closeErr = fmt.Errorf("close awareness LLM client after creation failure: %w", closeErr)
			}
			return errors.Join(err, closeErr)
		}
		client = inspectors.Wrap(client, route)
		if closer, ok := client.(io.Closer); ok {
			defer func() {
				if err := closer.Close(); err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("close awareness LLM client: %w", err))
				}
			}()
		}
	}

	var baseReg *tools.Registry
	if d.AwarenessRegistry != nil {
		baseReg = d.AwarenessRegistry()
	}
	if baseReg == nil {
		if d.Registry != nil {
			baseReg = d.Registry()
		}
	}
	if baseReg == nil {
		return fmt.Errorf("base registry is nil")
	}
	var sharedGuard *guard.Guard
	if d.Guard != nil {
		sharedGuard, err = d.Guard(logger)
		if err != nil {
			return fmt.Errorf("initialize awareness guard: %w", err)
		}
	}
	if sharedGuard != nil {
		defer func() {
			if err := sharedGuard.Close(); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close awareness guard: %w", err))
			}
		}()
	}
	cfg := opts.AgentLimits.ToConfig()

	state := &awarenessutil.State{}
	var wg sync.WaitGroup
	chatInfoStore, chatInfoRefresher := resolveChatInfoRuntime(opts)
	if opts.CronEnabled {
		refreshChatInfoOnStart(ctx, chatInfoStore, chatInfoRefresher, opts.ChatInfoContactsDir, logger)
	}

	runAwarenessTaskWithOpts := func(behavior awarenessutil.Behavior, task string, meta map[string]any, taskRunID, llmProfile string, bashEnv []cronstore.BashEnvRef) (string, error) {
		return runAwarenessTask(ctx, d, awarenessTaskOptions{
			Behavior:                 behavior,
			Logger:                   logger,
			LogOptions:               logOpts,
			Client:                   client,
			Route:                    route,
			Model:                    model,
			Task:                     task,
			Meta:                     meta,
			TaskRunID:                taskRunID,
			LLMProfile:               llmProfile,
			BaseRegistry:             baseReg,
			SharedGuard:              sharedGuard,
			Config:                   cfg,
			EngineToolsConfig:        opts.EngineToolsConfig,
			TaskTimeout:              opts.TaskTimeout,
			SystemPromptCacheControl: systemPromptCacheControl,
			ClientDecorator:          inspectors.Wrap,
			ImageClient:              nil,
			TaskStore:                opts.TaskStore,
			BashEnv:                  bashEnv,
		})
	}
	runTask := func(behavior awarenessutil.Behavior, task string, meta map[string]any, taskRunID string) (string, error) {
		return runAwarenessTaskWithOpts(behavior, task, meta, taskRunID, "", nil)
	}

	runTaskAsync := func(behavior awarenessutil.Behavior, task string, taskEmpty bool, wakeSignal awarenessdomain.PokeInput) string {
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

	runTick := func(behavior awarenessutil.Behavior, wakeSignal awarenessdomain.PokeInput) awarenessutil.TickResult {
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
						meta := awarenessutil.BuildAwarenessMeta(awarenessutil.BehaviorHeartbeat, opts.Source, opts.Interval, opts.ChecklistPath, false, awarenessdomain.PokeInput{}, map[string]any{
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
					if profile := strings.TrimSpace(task.LLMProfile); profile != "" {
						extra["llm_profile"] = profile
					}
					if !cronstore.IsConsoleNotificationChatID(task.ChatID) {
						if notifyTarget := buildCronNotifyTargetForTask(ctx, task, time.Now().UTC(), chatInfoStore, chatInfoRefresher, logger); notifyTarget != nil {
							extra["notify_target"] = notifyTarget
						}
					}
					meta := awarenessutil.BuildCronMeta("cron", strings.TrimSpace(task.ID), due.ScheduledAtUTC, cronstore.ScheduleForTask(task), strings.TrimSpace(task.TZ), strings.TrimSpace(task.ChatID), extra)
					summary, err := runAwarenessTaskWithOpts(awarenessutil.BehaviorCron, strings.TrimSpace(task.Content), meta, taskRunID, task.LLMProfile, task.BashEnv)
					if err != nil {
						notifyCronResult(ctx, opts.CronNotify, logger, task, taskRunID, "", err)
						return err
					}
					if summary == "" {
						summary = "empty"
					}
					notifyCronResult(ctx, opts.CronNotify, logger, task, taskRunID, summary, nil)
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

	RunPokeLoop(ctx, opts.PokeRequests, func(input awarenessdomain.PokeInput) awarenessutil.TickResult {
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
	Route                    llmutil.ResolvedRoute
	Model                    string
	Task                     string
	Meta                     map[string]any
	TaskRunID                string
	LLMProfile               string
	BaseRegistry             *tools.Registry
	SharedGuard              *guard.Guard
	Config                   agent.Config
	EngineToolsConfig        agent.EngineToolsConfig
	TaskTimeout              time.Duration
	SystemPromptCacheControl *llm.CacheControl
	ClientDecorator          func(llm.Client, llmutil.ResolvedRoute) llm.Client
	ImageClient              llm.ImageClient
	TaskStore                daemonruntime.TaskView
	BashEnv                  []cronstore.BashEnvRef
}

func runAwarenessTask(ctx context.Context, d Dependencies, opts awarenessTaskOptions) (summary string, runErr error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return "", fmt.Errorf("awareness task is empty")
	}
	workspaceDir := firstNonEmpty(opts.EngineToolsConfig.PathRoots.WorkspaceDir, d.DefaultWorkspaceDir)
	ctx = pathroots.WithWorkspaceDir(ctx, workspaceDir)
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
	llmProfile := strings.TrimSpace(opts.LLMProfile)
	var taskRoute llmutil.ResolvedRoute
	useTaskRoute := false
	if llmProfile != "" {
		profileRoutePurpose := routePurpose
		if profileRoutePurpose == "" {
			profileRoutePurpose = llmutil.RoutePurposeAwareness
		}
		if d.ResolveLLMRouteWithProfile == nil {
			return "", fmt.Errorf("resolve LLM route with profile dependency is missing")
		}
		var err error
		taskRoute, err = d.ResolveLLMRouteWithProfile(profileRoutePurpose, llmProfile)
		if err != nil {
			var missingProfile *llmutil.MissingProfileError
			if errors.As(err, &missingProfile) && strings.TrimSpace(missingProfile.Profile) == llmProfile {
				if opts.Logger != nil {
					opts.Logger.Warn("cron_llm_profile_invalid",
						"profile", llmProfile,
						"task_run_id", strings.TrimSpace(opts.TaskRunID),
						"fallback", "route_default",
						"error", err.Error(),
					)
				}
				llmProfile = ""
			} else {
				return "", fmt.Errorf("resolve llm profile %q: %w", llmProfile, err)
			}
		} else {
			useTaskRoute = true
		}
	}
	if !useTaskRoute && routePurpose != "" {
		var err error
		taskRoute, err = d.ResolveLLMRoute(routePurpose)
		if err != nil {
			return "", err
		}
		useTaskRoute = true
	}
	if !useTaskRoute && len(opts.Route.Candidates) > 0 {
		taskRoute = opts.Route
		useTaskRoute = true
	}
	if useTaskRoute {
		selectionKey := strings.TrimSpace(opts.TaskRunID)
		if selectionKey == "" {
			selectionKey = strings.TrimSpace(llmstats.RunIDFromContext(ctx))
		}
		if selectionKey == "" {
			selectionKey = strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx))
		}
		taskRoute = llmutil.SelectRouteCandidate(taskRoute, selectionKey)
		if reasoningEffort != "" {
			taskRoute = llmutil.ResolvedRouteWithReasoningEffort(taskRoute, reasoningEffort)
		}
		taskModel = strings.TrimSpace(taskRoute.ClientConfig.Model)
	}
	recordOpts := opts
	recordOpts.Model = taskModel
	if err := recordAwarenessTaskStart(recordOpts, task, time.Now().UTC()); err != nil {
		return "", fmt.Errorf("record awareness task start: %w", err)
	}
	taskRecordFinished := false
	defer func() {
		if taskRecordFinished {
			return
		}
		taskRecordFinished = true
		if err := recordAwarenessTaskFinish(recordOpts, summary, runErr, time.Now().UTC()); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("record awareness task finish: %w", err))
		}
	}()

	if useTaskRoute {
		var err error
		taskClient, err = d.CreateLLMClient(taskRoute)
		if err != nil {
			return "", err
		}
		defer closeAwarenessTaskClient(opts.Logger, taskClient)
		if opts.ClientDecorator != nil {
			taskClient = opts.ClientDecorator(taskClient, taskRoute)
		}
		systemPromptCacheControl, err = llmutil.SystemPromptCacheControl(taskRoute.Values.CacheTTL)
		if err != nil {
			return "", err
		}
	}

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
	var acpAgents []acpclient.AgentConfig
	if d.ACPAgents != nil {
		acpAgents = d.ACPAgents()
	}
	if len(acpAgents) == 0 {
		delete(toolTriggers, toolsutil.BuiltinACPSpawn)
	}
	if len(opts.BashEnv) > 0 {
		if toolTriggers == nil {
			toolTriggers = make(map[string]bool)
		}
		toolTriggers[toolsutil.BuiltinBash] = true
	}
	promptSpec, _, err := d.PromptSpec(runCtx, opts.Logger, opts.LogOptions, task, taskClient, taskModel, nil)
	if err != nil {
		return "", err
	}
	promptSpec.FinalOnlyResponse = true
	if block := workspace.PromptBlock(workspaceDir); strings.TrimSpace(block.Content) != "" {
		promptSpec.Blocks = append(promptSpec.Blocks, block)
	}

	reg := opts.BaseRegistry.Clone()
	if d.RegisterTriggeredStaticTools != nil {
		reg.Remove(toolsutil.BuiltinAgentSend)
		if toolTriggers == nil {
			toolTriggers = make(map[string]bool)
		}
		toolTriggers[toolsutil.BuiltinAgentSend] = true
	}
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
			if imageErr != nil {
				if closer, ok := imageClient.(io.Closer); ok && closer != nil {
					if closeErr := closer.Close(); closeErr != nil && opts.Logger != nil {
						opts.Logger.Warn("awareness_image_client_close_failed", "error", closeErr.Error())
					}
				}
				imageClient = nil
				if opts.Logger != nil {
					opts.Logger.Warn("image_client_create_failed", "error", imageErr.Error())
				}
			} else if closer, ok := imageClient.(io.Closer); ok && closer != nil {
				defer func() {
					if closeErr := closer.Close(); closeErr != nil && opts.Logger != nil {
						opts.Logger.Warn("awareness_image_client_close_failed", "error", closeErr.Error())
					}
				}()
			}
		}
	}
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient: taskClient,
		DefaultModel:  taskModel,
		ImageClient:   imageClient,
		ToolTriggers:  toolTriggers,
		PersonaDir:    d.RuntimePaths.PersonaDir,
	})
	promptprofile.ApplyPersonaIdentity(&promptSpec, opts.Logger, d.RuntimePaths.PersonaDir)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	if d.PromptAugment != nil {
		d.PromptAugment(&promptSpec, reg)
	}
	promptprofile.AppendAwarenessPromptPatch(&promptSpec)
	promptprofile.AppendModelPromptPatches(&promptSpec, taskModel)
	engineToolsConfig := opts.EngineToolsConfig
	engineToolsConfig.ToolTriggers = toolTriggers
	engineToolsConfig.PathRoots = pathroots.New(
		workspaceDir,
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
		agent.WithACPAgents(acpAgents),
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
		DisableContextCompaction: true,
	})
	if err != nil {
		return "", err
	}

	summary = strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	taskRecordFinished = true
	if err := recordAwarenessTaskFinish(recordOpts, summary, nil, time.Now().UTC()); err != nil {
		return "", fmt.Errorf("record awareness task finish: %w", err)
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

func recordAwarenessTaskStart(opts awarenessTaskOptions, task string, now time.Time) error {
	if opts.TaskStore == nil {
		return nil
	}
	taskID := strings.TrimSpace(opts.TaskRunID)
	if taskID == "" {
		return nil
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
	return taskdomain.RecordTaskUpsert(opts.TaskStore, info, trigger)
}

func recordAwarenessTaskFinish(opts awarenessTaskOptions, summary string, runErr error, now time.Time) error {
	if opts.TaskStore == nil {
		return nil
	}
	taskID := strings.TrimSpace(opts.TaskRunID)
	if taskID == "" {
		return nil
	}
	finishedAt := now.UTC()
	trigger := awarenessTaskTrigger(opts)
	return taskdomain.RecordTaskUpdate(opts.TaskStore, taskID, trigger, func(info *daemonruntime.TaskInfo) {
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

type awarenessInspectors struct {
	prompt  *llminspect.PromptInspector
	request *llminspect.RequestInspector
}

func newAwarenessInspectors(opts RunOptions) (*awarenessInspectors, error) {
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
	var promptErr, requestErr error
	if i.prompt != nil {
		promptErr = i.prompt.Close()
	}
	if i.request != nil {
		requestErr = i.request.Close()
	}
	return errors.Join(promptErr, requestErr)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
