package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
)

type RunOptions struct {
	Interval                time.Duration
	InitialDelay            time.Duration
	TaskTimeout             time.Duration
	RequestTimeout          time.Duration
	AgentLimits             agent.Limits
	Source                  string
	ChecklistPath           string
	MemoryEnabled           bool
	MemoryShortTermDays     int
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	InspectPrompt           bool
	InspectRequest          bool
	Notifier                Notifier
	PokeRequests            <-chan PokeRequest
}

type Dependencies = depsutil.HeartbeatDependencies

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	return runHeartbeatLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}

func runHeartbeatLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	common := depsutil.CommonFromHeartbeat(d)

	logger, err := depsutil.LoggerFromCommon(common)
	if err != nil {
		return err
	}
	logOpts := depsutil.LogOptionsFromCommon(common)

	route, err := depsutil.ResolveLLMRouteFromCommon(common, llmutil.RoutePurposeHeartbeat)
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
	inspectors, err := newHeartbeatInspectors(opts)
	if err != nil {
		return err
	}
	defer func() {
		if inspectors != nil {
			_ = inspectors.Close()
		}
	}()
	client = inspectors.Wrap(client, route)

	baseReg := depsutil.RegistryFromCommon(common)
	if baseReg == nil {
		return fmt.Errorf("base registry is nil")
	}
	sharedGuard := depsutil.GuardFromCommon(common, logger)
	cfg := opts.AgentLimits.ToConfig()

	orchestrator, projectionWorker, cleanup, err := newHeartbeatOrchestrator(ctx, common, opts, inspectors.Wrap)
	if err != nil {
		return err
	}
	defer cleanup()
	if projectionWorker != nil {
		projectionWorker.Start(ctx)
	}

	state := &heartbeatutil.State{}
	var wg sync.WaitGroup

	runTaskAsync := func(task string, checklistEmpty bool, wakeSignal daemonruntime.PokeInput) string {
		if ctx.Err() != nil {
			return "context_canceled"
		}
		runAt := time.Now().UTC()
		taskRunID := heartbeatTaskRunID(runAt)
		extra := map[string]any{
			"task_run_id": taskRunID,
		}
		if pokeMeta := wakeSignal.MetaValue(); pokeMeta != nil {
			extra["poke"] = pokeMeta
		}
		meta := depsutil.BuildHeartbeatMetaFromDeps(d, opts.Source, opts.Interval, opts.ChecklistPath, checklistEmpty, extra)
		wg.Add(1)
		go func() {
			defer wg.Done()

			summary, runErr := runHeartbeatTask(ctx, d, heartbeatTaskOptions{
				Logger:                  logger,
				LogOptions:              logOpts,
				Client:                  client,
				Model:                   model,
				Task:                    task,
				Meta:                    meta,
				TaskRunID:               taskRunID,
				BaseRegistry:            baseReg,
				SharedGuard:             sharedGuard,
				Config:                  cfg,
				TaskTimeout:             opts.TaskTimeout,
				WakeSignal:              wakeSignal,
				MemoryOrchestrator:      orchestrator,
				MemoryProjectionWorker:  projectionWorker,
				MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
				MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
			})
			if runErr != nil {
				displayErr := depsutil.FormatRuntimeError(runErr)
				alert, alertMsg := state.EndFailure(errors.New(displayErr))
				if alert {
					logger.Warn("heartbeat_alert", "source", opts.Source, "message", alertMsg)
					notifyHeartbeat(ctx, opts.Notifier, logger, alertMsg)
					return
				}
				logger.Warn("heartbeat_error", "source", opts.Source, "error", displayErr)
				return
			}
			state.EndSuccess(time.Now())
			if summary == "" {
				summary = "empty"
			}
			logger.Info("heartbeat_summary", "source", opts.Source, "message", summary)
		}()
		return ""
	}

	runTick := func(wakeSignal daemonruntime.PokeInput) heartbeatutil.TickResult {
		result := heartbeatutil.Tick(
			state,
			func() (string, bool, error) {
				return depsutil.BuildHeartbeatTaskFromDeps(d, opts.ChecklistPath)
			},
			func(task string, checklistEmpty bool) string {
				return runTaskAsync(task, checklistEmpty, wakeSignal)
			},
		)
		switch result.Outcome {
		case heartbeatutil.TickBuildError:
			if strings.TrimSpace(result.AlertMessage) != "" {
				logger.Warn("heartbeat_alert", "source", opts.Source, "message", result.AlertMessage)
				notifyHeartbeat(ctx, opts.Notifier, logger, result.AlertMessage)
			} else if result.BuildError != nil {
				logger.Warn("heartbeat_task_error", "source", opts.Source, "error", result.BuildError.Error())
			}
		case heartbeatutil.TickSkipped:
			logger.Debug("heartbeat_skip", "source", opts.Source, "reason", result.SkipReason)
		}
		return result
	}

	handlePoke := func(req PokeRequest) {
		err := ErrorFromTickResult(runTick(req.Input))
		if req.Result == nil {
			return
		}
		select {
		case req.Result <- err:
		default:
		}
	}

	pokeRequests := opts.PokeRequests

	if opts.InitialDelay > 0 {
		initialTimer := time.NewTimer(opts.InitialDelay)
		defer initialTimer.Stop()
		initialTriggered := false
		for !initialTriggered {
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			case req, ok := <-pokeRequests:
				if !ok {
					pokeRequests = nil
					continue
				}
				handlePoke(req)
				initialTriggered = true
			case <-initialTimer.C:
				runTick(daemonruntime.PokeInput{})
				initialTriggered = true
			}
		}
	} else {
		runTick(daemonruntime.PokeInput{})
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case req, ok := <-pokeRequests:
			if !ok {
				pokeRequests = nil
				continue
			}
			handlePoke(req)
		case <-ticker.C:
			runTick(daemonruntime.PokeInput{})
		}
	}
}

type heartbeatTaskOptions struct {
	Logger                  *slog.Logger
	LogOptions              agent.LogOptions
	Client                  llm.Client
	Model                   string
	Task                    string
	Meta                    map[string]any
	TaskRunID               string
	BaseRegistry            *tools.Registry
	SharedGuard             *guard.Guard
	Config                  agent.Config
	TaskTimeout             time.Duration
	WakeSignal              daemonruntime.PokeInput
	MemoryOrchestrator      *memoryruntime.Orchestrator
	MemoryProjectionWorker  *memoryruntime.ProjectionWorker
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
}

func runHeartbeatTask(ctx context.Context, d Dependencies, opts heartbeatTaskOptions) (string, error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return "", fmt.Errorf("heartbeat task is empty")
	}

	runCtx := ctx
	cancel := func() {}
	if opts.TaskTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.TaskTimeout)
	}
	defer cancel()

	promptSpec, _, err := depsutil.PromptSpecFromCommon(depsutil.CommonFromHeartbeat(d), runCtx, opts.Logger, opts.LogOptions, task, opts.Client, strings.TrimSpace(opts.Model), nil)
	if err != nil {
		return "", err
	}

	reg := cloneRegistry(opts.BaseRegistry)
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient: opts.Client,
		DefaultModel:  strings.TrimSpace(opts.Model),
	})
	promptprofile.ApplyPersonaIdentity(&promptSpec, opts.Logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, opts.Logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	promptprofile.AppendWakeSignalBlock(&promptSpec, opts.WakeSignal)
	if opts.MemoryOrchestrator != nil && opts.MemoryInjectionEnabled {
		snap, memErr := opts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      heartbeatMemorySubjectID,
			RequestContext: memory.ContextPrivate,
			MaxItems:       opts.MemoryInjectionMaxItems,
		})
		if memErr != nil {
			if opts.Logger != nil {
				opts.Logger.Warn("memory_injection_error", "source", "heartbeat", "error", memErr.Error())
			}
		} else if strings.TrimSpace(snap) != "" {
			promptprofile.AppendMemorySummariesBlock(&promptSpec, snap)
		}
	}

	engine := agent.New(
		opts.Client,
		reg,
		opts.Config,
		promptSpec,
		agent.WithLogger(opts.Logger),
		agent.WithLogOptions(opts.LogOptions),
		agent.WithGuard(opts.SharedGuard),
	)
	final, _, err := engine.Run(runCtx, task, agent.RunOptions{
		Model: strings.TrimSpace(opts.Model),
		Scene: "heartbeat.loop",
		Meta:  opts.Meta,
	})
	if err != nil {
		return "", err
	}

	summary := strings.TrimSpace(depsutil.FormatFinalOutput(final))
	if opts.MemoryOrchestrator != nil {
		if _, memErr := opts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
			TaskRunID:    opts.TaskRunID,
			SessionID:    heartbeatMemorySessionID,
			SubjectID:    heartbeatMemorySubjectID,
			Channel:      "heartbeat",
			Participants: heartbeatMemoryParticipants(),
			TaskText:     task,
			FinalOutput:  summary,
			SessionContext: memory.SessionContext{
				ConversationID: heartbeatMemorySubjectID,
			},
		}); memErr != nil && opts.Logger != nil {
			opts.Logger.Warn("memory_record_error", "source", "heartbeat", "error", memErr.Error())
		} else if opts.MemoryProjectionWorker != nil {
			opts.MemoryProjectionWorker.NotifyRecordAppended()
		}
	}

	return summary, nil
}

func notifyHeartbeat(ctx context.Context, notifier Notifier, logger *slog.Logger, message string) {
	if notifier == nil {
		return
	}
	if err := notifier.Notify(ctx, strings.TrimSpace(message)); err != nil && logger != nil {
		logger.Warn("heartbeat_notify_error", "error", err.Error())
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

type heartbeatInspectors struct {
	prompt  *llminspect.PromptInspector
	request *llminspect.RequestInspector
}

func newHeartbeatInspectors(opts runtimeLoopOptions) (*heartbeatInspectors, error) {
	out := &heartbeatInspectors{}
	if opts.InspectRequest {
		requestInspector, err := llminspect.NewRequestInspector(llminspect.Options{
			Mode:            heartbeatInspectMode(opts.Source),
			Task:            "heartbeat",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return nil, err
		}
		out.request = requestInspector
	}
	if opts.InspectPrompt {
		promptInspector, err := llminspect.NewPromptInspector(llminspect.Options{
			Mode:            heartbeatInspectMode(opts.Source),
			Task:            "heartbeat",
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

func (i *heartbeatInspectors) Wrap(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
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

func (i *heartbeatInspectors) Close() error {
	if i == nil {
		return nil
	}
	return errors.Join(closePromptInspector(i.prompt), closeRequestInspector(i.request))
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

func heartbeatInspectMode(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "", "heartbeat":
		return "heartbeat"
	default:
		return "heartbeat_" + source
	}
}

func newHeartbeatOrchestrator(ctx context.Context, common depsutil.CommonDependencies, opts runtimeLoopOptions, decorateClient func(client llm.Client, route llmutil.ResolvedRoute) llm.Client) (*memoryruntime.Orchestrator, *memoryruntime.ProjectionWorker, func(), error) {
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
