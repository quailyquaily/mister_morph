package consolecmd

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/integration"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/spf13/viper"
)

const (
	consoleLocalEndpointRef   = "ep_console_local"
	consoleLocalEndpointName  = "Console Local"
	consoleConversationKey    = "console:main"
	consoleTaskOutputMaxChars = 4000
	consoleServeListenDefault = "127.0.0.1:8791"
)

type consoleLocalTaskJob struct {
	TaskID          string
	ConversationKey string
	Task            string
	Model           string
	Timeout         time.Duration
	CreatedAt       time.Time
	Version         uint64
}

type consoleLocalRuntime struct {
	logger                  *slog.Logger
	runtime                 *integration.Runtime
	store                   *daemonruntime.MemoryStore
	runner                  *runtimecore.ConversationRunner[string, consoleLocalTaskJob]
	memoryEnabled           bool
	defaultTimeout          time.Duration
	defaultModel            string
	defaultProvider         string
	memoryInjectionEnabled  bool
	memoryInjectionMaxItems int
	memRuntime              runtimecore.MemoryRuntime
	authToken               string
	serveListen             string
	cancelWorkers           context.CancelFunc
	seq                     atomic.Uint64
}

func newConsoleLocalRuntime() (*consoleLocalRuntime, error) {
	logger, err := logutil.LoggerFromViper()
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)

	llmValues := llmutil.RuntimeValuesFromViper()
	mainRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return nil, err
	}
	defaultModel := strings.TrimSpace(mainRoute.ClientConfig.Model)
	if defaultModel == "" {
		defaultModel = "gpt-5.2"
	}
	defaultProvider := strings.TrimSpace(mainRoute.ClientConfig.Provider)

	defaultTimeout := viper.GetDuration("timeout")
	if defaultTimeout <= 0 {
		defaultTimeout = 10 * time.Minute
	}

	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	commonDeps := depsutil.CommonDependencies{
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRoute(llmValues, purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
		},
	}
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

	serveListen := resolveConsoleServeListen()
	authToken := strings.TrimSpace(viper.GetString("server.auth_token"))
	if authToken == "" {
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, fmt.Errorf("missing server.auth_token (set via MISTER_MORPH_SERVER_AUTH_TOKEN) for console runtime API")
	}
	serverMaxQueue := viper.GetInt("server.max_queue")
	if serverMaxQueue <= 0 {
		serverMaxQueue = 100
	}

	store := daemonruntime.NewMemoryStore(serverMaxQueue)
	out := &consoleLocalRuntime{
		logger:                  logger,
		runtime:                 integration.New(integration.DefaultConfig()),
		store:                   store,
		memoryEnabled:           memoryEnabled,
		defaultTimeout:          defaultTimeout,
		defaultModel:            defaultModel,
		defaultProvider:         defaultProvider,
		memoryInjectionEnabled:  viper.GetBool("memory.injection.enabled"),
		memoryInjectionMaxItems: viper.GetInt("memory.injection.max_items"),
		memRuntime:              memRuntime,
		authToken:               authToken,
		serveListen:             serveListen,
		cancelWorkers:           cancelWorkers,
	}
	out.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workersCtx,
		make(chan struct{}, 1),
		16,
		func(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
			out.handleTaskJob(workerCtx, conversationKey, job)
		},
	)
	if _, err := daemonruntime.StartServer(workersCtx, logger, daemonruntime.ServerOptions{
		Listen: serveListen,
		Routes: out.routesOptions(strings.TrimSpace(authToken)),
	}); err != nil {
		cancelWorkers()
		memRuntime.Cleanup()
		return nil, fmt.Errorf("start console runtime api server: %w", err)
	}
	logger.Info("console_runtime_server_start", "addr", serveListen)
	return out, nil
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
}

func (r *consoleLocalRuntime) Endpoint() runtimeEndpoint {
	url := "http://" + strings.TrimSpace(r.serveListen)
	return runtimeEndpoint{
		Ref:    consoleLocalEndpointRef,
		Name:   consoleLocalEndpointName,
		URL:    url,
		Client: newDaemonTaskClient(url, r.authToken),
	}
}

func (r *consoleLocalRuntime) routesOptions(authToken string) daemonruntime.RoutesOptions {
	return daemonruntime.RoutesOptions{
		Mode:          "console",
		AuthToken:     strings.TrimSpace(authToken),
		TaskReader:    r.store,
		Submit:        r.submitTask,
		HealthEnabled: true,
		Overview: func(ctx context.Context) (map[string]any, error) {
			return map[string]any{
				"llm": map[string]any{
					"provider": r.defaultProvider,
					"model":    r.defaultModel,
				},
				"channel": map[string]any{
					"configured":          true,
					"telegram_configured": false,
					"slack_configured":    false,
					"running":             "console",
					"telegram_running":    false,
					"slack_running":       false,
				},
			}, nil
		},
	}
}

func resolveConsoleServeListen() string {
	if listen := strings.TrimSpace(viper.GetString("console.serve_listen")); listen != "" {
		return listen
	}
	if legacy := strings.TrimSpace(viper.GetString("server.listen")); legacy != "" {
		return legacy
	}
	return consoleServeListenDefault
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
		model = r.defaultModel
	}

	now := time.Now().UTC()
	seq := r.seq.Add(1)
	taskID := daemonruntime.BuildTaskID("console", now.UnixNano(), seq, rand.Uint64())
	r.store.Upsert(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      strings.TrimSpace(req.Task),
		Model:     model,
		Timeout:   timeout.String(),
		CreatedAt: now,
	})

	err := r.runner.Enqueue(ctx, consoleConversationKey, func(version uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{
			TaskID:          taskID,
			ConversationKey: consoleConversationKey,
			Task:            strings.TrimSpace(req.Task),
			Model:           model,
			Timeout:         timeout,
			CreatedAt:       now,
			Version:         version,
		}
	})
	if err != nil {
		runtimecore.MarkTaskFailed(r.store, taskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return daemonruntime.SubmitTaskResponse{
		ID:     taskID,
		Status: daemonruntime.TaskQueued,
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
}

func (r *consoleLocalRuntime) runTask(ctx context.Context, conversationKey string, job consoleLocalTaskJob) (*agent.Final, *agent.Context, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("console runtime is not initialized")
	}
	ctx = llmstats.WithRunID(ctx, job.TaskID)

	runOpts := agent.RunOptions{
		Model: strings.TrimSpace(job.Model),
		Scene: "console.loop",
		Meta: map[string]any{
			"trigger":         "console",
			"console_task_id": job.TaskID,
		},
	}

	memSubjectID := buildConsoleMemorySubjectID(conversationKey)
	if r.memoryEnabled && r.memRuntime.Orchestrator != nil && memSubjectID != "" && r.memoryInjectionEnabled {
		snap, memErr := r.memRuntime.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      memSubjectID,
			RequestContext: memory.ContextPrivate,
			MaxItems:       r.memoryInjectionMaxItems,
		})
		if memErr != nil {
			r.logger.Warn("memory_injection_error", "source", "console", "subject_id", memSubjectID, "error", memErr.Error())
		} else if msg := renderConsoleMemoryHistoryMessage(snap); msg != "" {
			runOpts.History = append(runOpts.History, llm.Message{
				Role:    "user",
				Content: msg,
			})
			r.logger.Info("memory_injection_applied", "source", "console", "subject_id", memSubjectID, "snapshot_len", len(snap))
		} else {
			r.logger.Debug("memory_injection_skipped", "source", "console", "reason", "empty_snapshot", "subject_id", memSubjectID)
		}
	}

	final, agentCtx, err := r.runtime.RunTask(ctx, strings.TrimSpace(job.Task), runOpts)
	if err != nil {
		return final, agentCtx, err
	}

	output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	if r.memoryEnabled && output != "" && r.memRuntime.Orchestrator != nil && memSubjectID != "" {
		recordOffset, memErr := r.memRuntime.Orchestrator.Record(buildConsoleMemoryRecordRequest(job, memSubjectID, output))
		if memErr != nil {
			r.logger.Warn("memory_record_error", "source", "console", "subject_id", memSubjectID, "error", memErr.Error())
		} else {
			r.logger.Debug("memory_record_ok", "source", "console", "subject_id", memSubjectID, "offset_file", recordOffset.File, "offset_line", recordOffset.Line)
			if r.memRuntime.ProjectionWorker != nil {
				r.memRuntime.ProjectionWorker.NotifyRecordAppended()
			}
		}
	}

	return final, agentCtx, nil
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

func renderConsoleMemoryHistoryMessage(snapshot string) string {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return ""
	}
	return strings.TrimSpace("[[ Memory Summaries ]]\n" + snapshot)
}

func buildConsoleMemorySubjectID(conversationKey string) string {
	key := strings.TrimSpace(conversationKey)
	if key == "" {
		key = "main"
	}
	if strings.HasPrefix(strings.ToLower(key), "console:") {
		return key
	}
	return "console:" + key
}

func buildConsoleMemoryRecordRequest(job consoleLocalTaskJob, subjectID, output string) memoryruntime.RecordRequest {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		subjectID = "console:main"
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
