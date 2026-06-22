package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	awarenessloop "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

type consoleNoopLLMClient struct{}

func (consoleNoopLLMClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{Text: "ok"}, nil
}

type consoleReactLLMClient struct {
	requests []llm.Request
}

func (c *consoleReactLLMClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return llm.Result{
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_react",
					Name: "message_react",
					Arguments: map[string]any{
						"emoji": "👍",
					},
				},
			},
		}, nil
	}
	return llm.Result{Text: `{"type":"final","output":"fallback","is_lightweight":false}`}, nil
}

type consoleBlockingLLMClient struct {
	entered chan struct{}
}

func (c *consoleBlockingLLMClient) Chat(ctx context.Context, _ llm.Request) (llm.Result, error) {
	if c.entered != nil {
		select {
		case c.entered <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return llm.Result{}, ctx.Err()
}

func TestConsoleLocalRoutesOptionsPoke(t *testing.T) {
	rt := &consoleLocalRuntime{}
	if got := rt.routesOptions("token").Poke; got == nil {
		t.Fatal("Poke = nil, want dynamic callback")
	}
	if err := rt.routesOptions("token").Poke(context.Background(), daemonruntime.PokeInput{}); err == nil {
		t.Fatal("Poke() error = nil, want unavailable error when awareness loop is unavailable")
	}

	rt.awarenessPokeRequests = make(chan awarenessloop.PokeRequest)
	if got := rt.routesOptions("token").Poke; got == nil {
		t.Fatal("Poke = nil, want non-nil when awareness loop is available")
	}
}

func TestConsoleLocalRoutesOptionsCronRun(t *testing.T) {
	rt := &consoleLocalRuntime{}
	if got := rt.routesOptions("token").CronRun; got == nil {
		t.Fatal("CronRun = nil, want dynamic callback")
	}
	if err := rt.routesOptions("token").CronRun(context.Background(), cronstore.Task{ID: "cron-a"}); err == nil {
		t.Fatal("CronRun() error = nil, want unavailable error when awareness loop is unavailable")
	}

	requests := make(chan awarenessloop.CronRequest)
	rt.awarenessCronRequests = requests
	errCh := make(chan error, 1)
	task := cronstore.Task{
		ID:      "cron-a",
		Cron:    "0 10 * * *",
		Content: "Run cron.",
	}
	go func() {
		errCh <- rt.routesOptions("token").CronRun(context.Background(), task)
	}()

	req := <-requests
	if req.Task.ID != task.ID {
		t.Fatalf("request task = %#v, want %q", req.Task, task.ID)
	}
	req.Result <- nil
	if err := <-errCh; err != nil {
		t.Fatalf("CronRun() error = %v", err)
	}
}

func TestConsoleLocalRuntimeListApprovalsFromPendingTasks(t *testing.T) {
	rt, approvalID, taskID := newConsoleApprovalTestRuntime(t)

	resp, err := rt.listApprovals(context.Background(), daemonruntime.ApprovalListRequest{
		Status: "pending",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("listApprovals() error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(resp.Items))
	}
	item := resp.Items[0]
	if item.ApprovalRequestID != approvalID || item.TaskID != taskID || item.Status != "pending" {
		t.Fatalf("approval item = %+v, want approval %q task %q pending", item, approvalID, taskID)
	}
	if item.ToolName != "bash" {
		t.Fatalf("item.ToolName = %q, want bash", item.ToolName)
	}
	if item.PendingAt == nil || item.PendingAt.IsZero() {
		t.Fatalf("item.PendingAt = %v, want set", item.PendingAt)
	}
}

func TestConsoleLocalRuntimeDenyApprovalCancelsPendingTask(t *testing.T) {
	rt, approvalID, taskID := newConsoleApprovalTestRuntime(t)
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: buildConsoleConversationKey("topic_a"),
		TopicID:         "topic_a",
		Task:            "run bash",
		Timeout:         time.Minute,
		Generation:      rt.generation,
	}
	rt.registerPendingApproval(approvalID, job)

	resp, err := rt.denyApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:user",
		Note:              "not now",
	})
	if err != nil {
		t.Fatalf("denyApproval() error = %v", err)
	}
	if resp.Status != string(guard.ApprovalDenied) || resp.Resumed {
		t.Fatalf("deny response = %+v, want denied not resumed", resp)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", taskID)
	}
	if task.Status != daemonruntime.TaskCanceled || strings.TrimSpace(task.Error) != "Approval denied. Task canceled." {
		t.Fatalf("task status/error = %s/%q, want canceled/approval denied", task.Status, task.Error)
	}
	if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "" || task.Result != nil {
		t.Fatalf("task pending approval fields = pending_at %v approval %q result %#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	rec, ok, err := rt.currentApprovalGuard().GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalDenied || rec.Actor != "console:user" || rec.Comment != "not now" {
		t.Fatalf("approval record = %+v, want denied by console:user", rec)
	}
}

func TestConsoleLocalRuntimeApproveApprovalEnqueuesResumeJob(t *testing.T) {
	rt, approvalID, taskID := newConsoleApprovalTestRuntime(t)
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan consoleLocalTaskJob, 1)
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			jobs <- job
		},
	)
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: buildConsoleConversationKey("topic_a"),
		TopicID:         "topic_a",
		Task:            "run bash",
		Timeout:         time.Minute,
		Generation:      rt.generation,
	}
	rt.registerPendingApproval(approvalID, job)

	resp, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:user",
		Note:              "ok",
	})
	if err != nil {
		t.Fatalf("approveApproval() error = %v", err)
	}
	if resp.Status != string(guard.ApprovalApproved) || !resp.Resumed {
		t.Fatalf("approve response = %+v, want approved resumed", resp)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", taskID)
	}
	if task.Status != daemonruntime.TaskQueued {
		t.Fatalf("task status = %s, want queued", task.Status)
	}
	if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "" || task.Result != nil {
		t.Fatalf("task pending approval fields = pending_at %v approval %q result %#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}

	select {
	case queued := <-jobs:
		if queued.TaskID != taskID || queued.ResumeApprovalID != approvalID {
			t.Fatalf("queued job = %+v, want task %q resume %q", queued, taskID, approvalID)
		}
		queued.Generation.release()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resume job")
	}
	rec, ok, err := rt.currentApprovalGuard().GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalApproved || rec.Actor != "console:user" || rec.Comment != "ok" {
		t.Fatalf("approval record = %+v, want approved by console:user", rec)
	}
}

func TestConsoleLocalRuntimeApproveApprovalReturnsStructuredResumeFailure(t *testing.T) {
	rt, approvalID, taskID := newConsoleApprovalTestRuntime(t)
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: buildConsoleConversationKey("topic_a"),
		TopicID:         "topic_a",
		Task:            "run bash",
		Timeout:         time.Minute,
		Generation:      rt.generation,
	}
	rt.registerPendingApproval(approvalID, job)

	resp, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:user",
		Note:              "ok",
	})
	if err != nil {
		t.Fatalf("approveApproval() error = %v, want structured response", err)
	}
	if resp.Status != string(guard.ApprovalApproved) || resp.Resumed || !strings.Contains(resp.Error, "task runner is unavailable") {
		t.Fatalf("approve response = %+v, want approved resumed=false runner error", resp)
	}

	task, ok := rt.store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", taskID)
	}
	if task.Status != daemonruntime.TaskFailed || !strings.Contains(task.Error, "task runner is unavailable") {
		t.Fatalf("task status/error = %s/%q, want failed runner error", task.Status, task.Error)
	}
	if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "" || task.Result != nil {
		t.Fatalf("task pending approval fields = pending_at %v approval %q result %#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	rec, ok, err := rt.currentApprovalGuard().GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalApproved {
		t.Fatalf("approval status = %s, want approved", rec.Status)
	}
}

func newConsoleApprovalTestRuntime(t *testing.T) (*consoleLocalRuntime, string, string) {
	t.Helper()
	taskStore, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "guard", "guard_approvals.json"), filepath.Join(root, ".locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	approvalID, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		ID:                    "apr_test",
		RunID:                 "task_pending",
		CreatedAt:             time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC),
		ExpiresAt:             time.Now().UTC().Add(time.Hour),
		ActionType:            guard.ActionToolCallPre,
		ToolName:              "bash",
		ActionHash:            "hash_test",
		RiskLevel:             guard.RiskHigh,
		Decision:              guard.DecisionRequireApproval,
		Reasons:               []string{"bash_requires_approval"},
		ActionSummaryRedacted: "ToolCallPre tool=bash",
		ResumeState:           []byte(`{"version":1}`),
	})
	if err != nil {
		t.Fatalf("ApprovalStore.Create() error = %v", err)
	}
	pendingAt := time.Date(2026, time.June, 22, 10, 1, 0, 0, time.UTC)
	taskID := "task_pending"
	taskStore.Upsert(daemonruntime.TaskInfo{
		ID:                taskID,
		Status:            daemonruntime.TaskPending,
		Task:              "run bash",
		Model:             "test-model",
		Timeout:           time.Minute.String(),
		CreatedAt:         time.Date(2026, time.June, 22, 9, 59, 0, 0, time.UTC),
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalID,
		Result: map[string]any{
			"final": map[string]any{
				"output": map[string]any{
					"status":              "pending",
					"approval_request_id": approvalID,
					"message":             `Approval required to execute tool "bash" at step 0.`,
				},
			},
		},
		TopicID: "topic_a",
	})

	g := guard.New(guard.Config{
		Enabled: true,
		Approvals: guard.ApprovalsConfig{
			Enabled: true,
		},
	}, nil, approvalStore)
	generation := &consoleLocalRuntimeGeneration{
		reader: viper.New(),
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: &taskruntime.Runtime{
				SharedGuard: g,
			},
		},
	}
	return &consoleLocalRuntime{
		store:      taskStore,
		generation: generation,
		streamHub:  newConsoleStreamHub(),
	}, approvalID, taskID
}

func TestConsoleLocalRoutesOptionsOverviewOmitsAwarenessRunning(t *testing.T) {
	reader := viper.New()
	reader.Set("telegram.bot_token", "tg-token")
	reader.Set("slack.bot_token", "slack-bot")
	reader.Set("slack.app_token", "slack-app")
	rt := &consoleLocalRuntime{
		generation:            &consoleLocalRuntimeGeneration{reader: reader},
		awarenessPokeRequests: make(chan awarenessloop.PokeRequest),
	}

	payload, err := rt.routesOptions("token").Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if _, ok := payload["awareness_running"]; ok {
		t.Fatalf("awareness_running exists, want omitted for direct awareness runtime")
	}
	if _, ok := payload["heartbeat_running"]; ok {
		t.Fatalf("heartbeat_running exists, want omitted")
	}
}

func TestConsoleLocalReloadAwarenessLoopKeepsPokeWhenHeartbeatDisabled(t *testing.T) {
	reader := viper.New()
	reader.Set("heartbeat.enabled", false)
	reader.Set("heartbeat.interval", time.Minute)
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()

	rt := &consoleLocalRuntime{
		workersCtx: workersCtx,
		generation: consoleLocalAwarenessTestGeneration(reader),
	}
	rt.reloadAwarenessLoop()
	t.Cleanup(func() {
		rt.awarenessMu.Lock()
		if rt.awarenessCancel != nil {
			rt.awarenessCancel()
			rt.awarenessCancel = nil
		}
		rt.awarenessMu.Unlock()
	})

	if !rt.canPokeAwareness() {
		t.Fatal("canPokeAwareness() = false, want true when heartbeat is disabled")
	}
}

func consoleLocalAwarenessTestGeneration(reader *viper.Viper) *consoleLocalRuntimeGeneration {
	logger := slog.Default()
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "bedrock",
			Model:    "test-model",
		},
	}
	baseRegistry := tools.NewRegistry()
	commonDeps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return consoleNoopLLMClient{}, nil
		},
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
	}
	return &consoleLocalRuntimeGeneration{
		reader:     reader,
		logger:     logger,
		commonDeps: commonDeps,
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: &taskruntime.Runtime{
				BaseRegistry:       baseRegistry,
				BootstrapMainRoute: route,
			},
		},
	}
}

func TestBuildConsoleLocalRuntimeBundlePassesCoderEngineToolConfig(t *testing.T) {
	reader := viper.New()
	reader.Set("file_cache_dir", t.TempDir())
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("tools.coder.enabled", true)
	reader.Set("tools.coder.path_extra", []string{"/opt/coder/bin"})

	logger := slog.Default()
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "bedrock",
			Model:    "test-model",
		},
	}
	snapshot := consoleLocalRuntimeConfigSnapshot{
		reader: reader,
		commonDeps: depsutil.CommonDependencies{
			Logger: func() (*slog.Logger, error) {
				return logger, nil
			},
			LogOptions: func() agent.LogOptions {
				return agent.LogOptions{}
			},
			ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
				return route, nil
			},
			CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
				return consoleNoopLLMClient{}, nil
			},
			PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
				return agent.DefaultPromptSpec(), nil, nil
			},
		},
	}

	bundle, _, err := buildConsoleLocalRuntimeBundle(logger, nil, snapshot)
	if err != nil {
		t.Fatalf("buildConsoleLocalRuntimeBundle() error = %v", err)
	}
	if bundle != nil && bundle.mcpHost != nil {
		t.Cleanup(func() { _ = bundle.mcpHost.Close() })
	}
	if bundle == nil || bundle.taskRuntime == nil {
		t.Fatal("bundle.taskRuntime = nil")
	}
	if !bundle.taskRuntime.EngineToolsConfig.CoderEnabled {
		t.Fatal("CoderEnabled = false, want true from tools.coder.enabled")
	}
	if len(bundle.taskRuntime.EngineToolsConfig.CoderPathExtra) != 1 || bundle.taskRuntime.EngineToolsConfig.CoderPathExtra[0] != "/opt/coder/bin" {
		t.Fatalf("CoderPathExtra = %#v, want /opt/coder/bin", bundle.taskRuntime.EngineToolsConfig.CoderPathExtra)
	}
}

func TestConsoleLocalRuntimeMessageReactContinuesToFinalText(t *testing.T) {
	client := &consoleReactLLMClient{}
	logger := slog.Default()
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "test-model",
		},
	}
	baseRegistry := tools.NewRegistry()
	commonDeps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
	}
	taskRuntime, err := taskruntime.Bootstrap(commonDeps, taskruntime.BootstrapOptions{
		AgentConfig: agent.Config{
			MaxSteps:        3,
			ParseRetries:    0,
			ToolRepeatLimit: 2,
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	generation := &consoleLocalRuntimeGeneration{
		reader:     viper.New(),
		logger:     logger,
		commonDeps: commonDeps,
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: taskRuntime,
		},
	}
	rt := &consoleLocalRuntime{}
	final, runCtx, err := rt.runTask(context.Background(), "topic_a", consoleLocalTaskJob{
		TaskID:          "task_react",
		TopicID:         "topic_a",
		ConversationKey: "topic_a",
		Task:            "Hi",
		CreatedAt:       time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC),
		Generation:      generation,
		Trigger:         daemonruntime.TaskTrigger{Source: "ui", TraceID: "trace_react"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if final == nil {
		t.Fatal("final = nil")
	}
	if got := strings.TrimSpace(fmt.Sprint(final.Output)); got != "fallback" {
		t.Fatalf("final.Output = %q, want final text", got)
	}
	if len(client.requests) != 2 {
		t.Fatalf("client.requests length = %d, want 2", len(client.requests))
	}
	if !llmRequestHasTool(client.requests[0], "message_react") {
		t.Fatalf("first request tools missing message_react: %#v", client.requests[0].Tools)
	}
	meta := decodeConsoleInjectedMeta(t, client.requests[0])
	for key, want := range map[string]string{
		"run_id":           "task_react",
		"task_id":          "task_react",
		"trace_id":         "trace_react",
		"topic_id":         "topic_a",
		"console_task_id":  "task_react",
		"console_topic_id": "topic_a",
	} {
		got, _ := meta[key].(string)
		if got != want {
			t.Fatalf("meta[%s] = %q, want %q; meta=%#v", key, got, want, meta)
		}
	}
	if runCtx == nil || len(runCtx.Steps) != 1 || runCtx.Steps[0].Action != "message_react" {
		t.Fatalf("run steps = %#v, want single message_react step", runCtx)
	}
}

func decodeConsoleInjectedMeta(t *testing.T, req llm.Request) map[string]any {
	t.Helper()
	if len(req.Messages) < 2 {
		t.Fatalf("request messages len = %d, want at least 2", len(req.Messages))
	}
	var payload struct {
		Meta map[string]any `json:"mister_morph_meta"`
	}
	if err := json.Unmarshal([]byte(req.Messages[1].Content), &payload); err != nil {
		t.Fatalf("decode meta message error = %v; content=%q", err, req.Messages[1].Content)
	}
	if len(payload.Meta) == 0 {
		t.Fatalf("mister_morph_meta missing in %q", req.Messages[1].Content)
	}
	return payload.Meta
}

func llmRequestHasTool(req llm.Request, name string) bool {
	name = strings.TrimSpace(name)
	for _, tool := range req.Tools {
		if strings.TrimSpace(tool.Name) == name {
			return true
		}
	}
	return false
}

func TestConsoleTopicTitleFromOutput(t *testing.T) {
	t.Run("short output becomes title", func(t *testing.T) {
		got := consoleTopicTitleFromOutput("  Short answer.  ")
		if got != "Short answer" {
			t.Fatalf("consoleTopicTitleFromOutput() = %q, want %q", got, "Short answer")
		}
	})

	t.Run("long output requires llm", func(t *testing.T) {
		got := consoleTopicTitleFromOutput(strings.Repeat("a", consoleTopicTitleDirectOutputMaxRunes+1))
		if got != "" {
			t.Fatalf("consoleTopicTitleFromOutput() = %q, want empty", got)
		}
	})
}

func TestConsoleLocalRuntimeMaybeRefreshTopicTitleUsesShortOutput(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("seed title")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	rt := &consoleLocalRuntime{store: store}
	rt.maybeRefreshTopicTitle(consoleLocalTaskJob{
		TopicID:         topic.ID,
		Task:            "first task",
		AutoRenameTopic: true,
	}, "Direct title")

	updated, ok := store.GetTopic(topic.ID)
	if !ok || updated == nil {
		t.Fatalf("GetTopic(%q) missing", topic.ID)
	}
	if updated.Title != "Direct title" {
		t.Fatalf("updated.Title = %q, want %q", updated.Title, "Direct title")
	}
	if updated.LLMTitleGeneratedAt != nil {
		t.Fatal("updated.LLMTitleGeneratedAt != nil, want nil for direct title path")
	}
}

func TestBuildConsoleTaskResultMetricsUsesSnakeCase(t *testing.T) {
	start := time.Date(2026, time.April, 4, 12, 34, 56, 0, time.UTC)
	result := buildConsoleTaskResult(&agent.Final{Output: "done"}, &agent.Context{
		Metrics: &agent.Metrics{
			LLMRounds:    3,
			TotalTokens:  120,
			TotalCost:    0.42,
			StartTime:    start,
			ElapsedMs:    1500,
			ToolCalls:    2,
			ParseRetries: 1,
		},
	}, nil)

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}

	var payload struct {
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}

	if payload.Metrics["llm_rounds"] != float64(3) {
		t.Fatalf("metrics.llm_rounds = %#v, want 3", payload.Metrics["llm_rounds"])
	}
	if payload.Metrics["total_tokens"] != float64(120) {
		t.Fatalf("metrics.total_tokens = %#v, want 120", payload.Metrics["total_tokens"])
	}
	if payload.Metrics["total_cost"] != 0.42 {
		t.Fatalf("metrics.total_cost = %#v, want 0.42", payload.Metrics["total_cost"])
	}
	if payload.Metrics["elapsed_ms"] != float64(1500) {
		t.Fatalf("metrics.elapsed_ms = %#v, want 1500", payload.Metrics["elapsed_ms"])
	}
	if payload.Metrics["tool_calls"] != float64(2) {
		t.Fatalf("metrics.tool_calls = %#v, want 2", payload.Metrics["tool_calls"])
	}
	if payload.Metrics["parse_retries"] != float64(1) {
		t.Fatalf("metrics.parse_retries = %#v, want 1", payload.Metrics["parse_retries"])
	}
	if got := payload.Metrics["start_time"]; got != start.Format(time.RFC3339) {
		t.Fatalf("metrics.start_time = %#v, want %q", got, start.Format(time.RFC3339))
	}
	if _, ok := payload.Metrics["LLMRounds"]; ok {
		t.Fatalf("metrics unexpectedly contains camelCase key: %#v", payload.Metrics)
	}
	if _, ok := payload.Metrics["TotalTokens"]; ok {
		t.Fatalf("metrics unexpectedly contains camelCase key: %#v", payload.Metrics)
	}
}

func TestBuildConsoleTaskResultIncludesPlan(t *testing.T) {
	result := buildConsoleTaskResult(&agent.Final{Output: "done"}, &agent.Context{
		Plan: &agent.Plan{
			Steps: []agent.PlanStep{
				{Step: "collect logs", Status: agent.PlanStatusCompleted},
				{Step: "patch bug", Status: agent.PlanStatusInProgress},
			},
		},
	}, nil)

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}

	var payload struct {
		Plan *consolePlanProgress `json:"plan"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if payload.Plan == nil {
		t.Fatal("payload.Plan = nil")
	}
	if len(payload.Plan.Steps) != 2 {
		t.Fatalf("len(payload.Plan.Steps) = %d, want 2", len(payload.Plan.Steps))
	}
	if payload.Plan.Steps[1].Status != agent.PlanStatusInProgress {
		t.Fatalf("payload.Plan.Steps[1].Status = %q, want %q", payload.Plan.Steps[1].Status, agent.PlanStatusInProgress)
	}
}

func TestBuildConsoleTaskResultIncludesActivity(t *testing.T) {
	result := buildConsoleTaskResult(&agent.Final{Output: "done"}, &agent.Context{}, &consoleActivityProgress{
		Current: &consoleActivityEntry{
			ID:     "tool:1",
			Kind:   "tool",
			Name:   "web_search",
			Status: "done",
			Args: map[string]any{
				"q": "alpha",
			},
		},
		History: []consoleActivityEntry{
			{
				ID:     "tool:1",
				Kind:   "tool",
				Name:   "web_search",
				Status: "done",
			},
		},
	})

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}

	var payload struct {
		Activity *consoleActivityProgress `json:"activity"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if payload.Activity == nil || payload.Activity.Current == nil {
		t.Fatalf("payload.Activity = %#v, want current activity", payload.Activity)
	}
	if payload.Activity.Current.Name != "web_search" {
		t.Fatalf("payload.Activity.Current.Name = %q, want web_search", payload.Activity.Current.Name)
	}
	if payload.Activity.Current.Args["q"] != "alpha" {
		t.Fatalf("payload.Activity.Current.Args[q] = %#v, want alpha", payload.Activity.Current.Args["q"])
	}
}

func TestBuildConsoleTopicHistoryUsesRecentPriorTasks(t *testing.T) {
	base := time.Date(2026, time.March, 23, 10, 0, 0, 0, time.UTC)
	tasks := make([]daemonruntime.TaskInfo, 0, 10)
	for i := 9; i >= 1; i-- {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		finishedAt := createdAt.Add(15 * time.Second)
		result := map[string]any{
			"final": map[string]any{
				"output": fmt.Sprintf("answer %d", i),
			},
		}
		if i == 7 {
			result = map[string]any{"output": "answer 7"}
		}
		tasks = append(tasks, daemonruntime.TaskInfo{
			ID:         fmt.Sprintf("task_%02d", i),
			Status:     daemonruntime.TaskDone,
			Task:       fmt.Sprintf("question %d", i),
			CreatedAt:  createdAt,
			FinishedAt: &finishedAt,
			TopicID:    "topic_a",
			Result:     result,
		})
	}
	tasks = append(tasks, daemonruntime.TaskInfo{
		ID:        "task_future",
		Status:    daemonruntime.TaskDone,
		Task:      "future question",
		CreatedAt: base.Add(11 * time.Minute),
		TopicID:   "topic_a",
		Result:    map[string]any{"output": "future answer"},
	})

	history := buildConsoleTopicHistory(tasks, consoleLocalTaskJob{
		TaskID:     "task_current",
		TopicID:    "topic_a",
		Task:       "current question",
		CreatedAt:  base.Add(10 * time.Minute),
		Trigger:    daemonruntime.TaskTrigger{Source: "ui"},
		Timeout:    time.Minute,
		Version:    1,
		WakeSignal: daemonruntime.PokeInput{},
	}, consoleHistoryRestoreTaskLimit)

	if len(history) != 12 {
		t.Fatalf("len(history) = %d, want 12", len(history))
	}

	gotTexts := make([]string, 0, len(history))
	for _, item := range history {
		gotTexts = append(gotTexts, item.Text)
	}
	wantTexts := []string{
		"question 4", "answer 4",
		"question 5", "answer 5",
		"question 6", "answer 6",
		"question 7", "answer 7",
		"question 8", "answer 8",
		"question 9", "answer 9",
	}
	if strings.Join(gotTexts, "\n") != strings.Join(wantTexts, "\n") {
		t.Fatalf("history texts = %#v, want %#v", gotTexts, wantTexts)
	}
}

func TestConsoleLocalRuntimeLoadConsoleTopicHistoryReplaysPersistedTasks(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	base := time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 8; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		finishedAt := createdAt.Add(30 * time.Second)
		result := map[string]any{
			"final": map[string]any{
				"output": fmt.Sprintf("persisted answer %d", i),
			},
		}
		if i == 6 {
			result = map[string]any{"output": "persisted answer 6"}
		}
		if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
			ID:         fmt.Sprintf("persisted_task_%02d", i),
			Status:     daemonruntime.TaskDone,
			Task:       fmt.Sprintf("persisted question %d", i),
			Model:      "gpt-5.2",
			Timeout:    "10m0s",
			CreatedAt:  createdAt,
			FinishedAt: &finishedAt,
			TopicID:    topic.ID,
			Result:     result,
		}, daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
			t.Fatalf("UpsertWithTrigger(done %d) error = %v", i, err)
		}
	}

	currentCreatedAt := base.Add(9 * time.Minute)
	if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        "persisted_task_current",
		Status:    daemonruntime.TaskQueued,
		Task:      "current persisted question",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: currentCreatedAt,
		TopicID:   topic.ID,
	}, daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
		t.Fatalf("UpsertWithTrigger(current) error = %v", err)
	}

	reloaded, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}

	rt := &consoleLocalRuntime{store: reloaded}
	history := rt.loadConsoleTopicHistory(consoleLocalTaskJob{
		TaskID:    "persisted_task_current",
		TopicID:   topic.ID,
		Task:      "current persisted question",
		CreatedAt: currentCreatedAt,
	})
	if len(history) != 12 {
		t.Fatalf("len(history) = %d, want 12", len(history))
	}
	if history[0].Kind != chathistory.KindInboundUser || history[0].Text != "persisted question 3" {
		t.Fatalf("history[0] = %#v, want persisted question 3 inbound", history[0])
	}
	last := history[len(history)-1]
	if last.Kind != chathistory.KindOutboundAgent || last.Text != "persisted answer 8" {
		t.Fatalf("history[last] = %#v, want persisted answer 8 outbound", last)
	}
}

func TestConsoleLocalRuntimeHandleConsoleBusInboundUsesPendingJobGeneration(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan consoleLocalTaskJob, 1)
	rt := &consoleLocalRuntime{
		store:       store,
		pendingJobs: map[string]consoleLocalTaskJob{},
	}
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			jobs <- job
		},
	)

	oldReader := viper.New()
	oldReader.Set("timeout", "2m")
	newReader := viper.New()
	newReader.Set("timeout", "9m")
	oldGeneration := &consoleLocalRuntimeGeneration{generation: 1, reader: oldReader}
	newGeneration := &consoleLocalRuntimeGeneration{generation: 2, reader: newReader}
	rt.generation = newGeneration

	oldGeneration.acquire()
	job, _, err := rt.acceptTask(
		oldGeneration,
		"hello",
		"",
		time.Minute,
		"",
		"",
		"",
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	rt.pendingJobs[job.TaskID] = job

	err = rt.handleConsoleBusInbound(context.Background(), busruntime.BusMessage{
		Channel:       busruntime.ChannelConsole,
		Direction:     busruntime.DirectionInbound,
		CorrelationID: job.TaskID,
	})
	if err != nil {
		t.Fatalf("handleConsoleBusInbound() error = %v", err)
	}

	select {
	case queued := <-jobs:
		if queued.Generation != oldGeneration {
			t.Fatalf("queued.Generation = %#v, want old generation %#v", queued.Generation, oldGeneration)
		}
		if queued.Timeout != time.Minute {
			t.Fatalf("queued.Timeout = %v, want %v", queued.Timeout, time.Minute)
		}
		queued.Generation.release()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued job")
	}

	if _, ok := rt.pendingJobs[job.TaskID]; ok {
		t.Fatalf("pendingJobs[%q] still exists, want removed after enqueue", job.TaskID)
	}
}

func TestConsoleLocalRuntimeAcceptTaskLoadsWorkspaceAttachment(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	if _, _, err := workspaceStore.Set(buildConsoleConversationKey(topic.ID), workspace.Attachment{WorkspaceDir: workspaceRoot}); err != nil {
		t.Fatalf("workspaceStore.Set() error = %v", err)
	}

	generation := &consoleLocalRuntimeGeneration{reader: viper.New()}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}

	job, _, err := rt.acceptTask(
		generation,
		"hello",
		"",
		time.Minute,
		topic.ID,
		"",
		"",
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	if job.WorkspaceDir != workspaceRoot {
		t.Fatalf("job.WorkspaceDir = %q, want %q", job.WorkspaceDir, workspaceRoot)
	}
	if job.Trigger.TraceID != job.TaskID {
		t.Fatalf("job.Trigger.TraceID = %q, want task id %q", job.Trigger.TraceID, job.TaskID)
	}
	trigger, ok := store.GetTrigger(job.TaskID)
	if !ok {
		t.Fatalf("store.GetTrigger(%q) missing", job.TaskID)
	}
	if trigger.TraceID != job.TaskID {
		t.Fatalf("stored trigger trace_id = %q, want %q", trigger.TraceID, job.TaskID)
	}
}

func TestConsoleLocalRuntimeAcceptTaskStoresRequestedWorkspaceAttachment(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	generation := &consoleLocalRuntimeGeneration{reader: viper.New()}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}

	job, _, err := rt.acceptTask(
		generation,
		"hello",
		"",
		time.Minute,
		"",
		"",
		workspaceRoot,
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	if job.TopicID == "" {
		t.Fatal("job.TopicID is empty")
	}
	if job.WorkspaceDir != workspaceRoot {
		t.Fatalf("job.WorkspaceDir = %q, want %q", job.WorkspaceDir, workspaceRoot)
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(job.TopicID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != workspaceRoot {
		t.Fatalf("currentDir = %q, want %q", currentDir, workspaceRoot)
	}
}

func TestConsoleLocalRuntimeDeleteTopicRemovesWorkspaceAttachment(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	if _, _, err := workspaceStore.Set(buildConsoleConversationKey(topic.ID), workspace.Attachment{WorkspaceDir: workspaceRoot}); err != nil {
		t.Fatalf("workspaceStore.Set() error = %v", err)
	}

	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}
	if !rt.deleteTopic(topic.ID) {
		t.Fatalf("deleteTopic(%q) = false, want true", topic.ID)
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != "" {
		t.Fatalf("currentDir = %q, want empty after topic delete", currentDir)
	}
}

func TestConsoleLocalRuntimeSubmitTaskHandlesWorkspaceCommand(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
		generation:     generation,
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task:    "/workspace attach " + workspaceRoot,
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("submitTask() error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}
	if resp.TopicID != topic.ID {
		t.Fatalf("resp.TopicID = %q, want %q", resp.TopicID, topic.ID)
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != workspaceRoot {
		t.Fatalf("currentDir = %q, want %q", currentDir, workspaceRoot)
	}
	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", resp.ID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(final["output"])); got != "workspace attached: "+workspaceRoot {
		t.Fatalf("final.output = %q, want %q", got, "workspace attached: "+workspaceRoot)
	}
}

func TestConsoleLocalRuntimeSubmitTaskHandlesHelpCommand(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	rt := &consoleLocalRuntime{
		store:      store,
		generation: generation,
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task: "/help",
	})
	if err != nil {
		t.Fatalf("submitTask() error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}
	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", resp.ID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	output := strings.TrimSpace(fmt.Sprint(final["output"]))
	for _, want := range []string{"/ctx", "/help", "/models", "/skills", "/workspace"} {
		if !strings.Contains(output, want) {
			t.Fatalf("final.output missing %q: %q", want, output)
		}
	}
}

func TestConsoleLocalRuntimeSubmitTaskHandlesStopCommand(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	control := runtimecontrol.New()
	rt := &consoleLocalRuntime{
		store:      store,
		generation: generation,
		runControl: control,
	}
	runCtx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	conversationKey := buildConsoleConversationKey(topic.ID)
	if err := control.Start(runtimecontrol.ActiveRun{
		Runtime:         "console",
		ConversationKey: conversationKey,
		TopicID:         topic.ID,
		TaskID:          "task_active",
		RunID:           "task_active",
		Cancel:          cancel,
		Snapshot: func() string {
			return "LLM 轮次 2，计划 1/3"
		},
	}); err != nil {
		t.Fatalf("RunControl.Start() error = %v", err)
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task:    "/stop",
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("submitTask(/stop) error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}
	if !errors.Is(context.Cause(runCtx), runtimecontrol.ErrStoppedByUser) {
		t.Fatalf("context cause = %v, want ErrStoppedByUser", context.Cause(runCtx))
	}
	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", resp.ID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	output := strings.TrimSpace(fmt.Sprint(final["output"]))
	if output != runtimecontrol.FeedbackStopped {
		t.Fatalf("final.output = %q, want stopped acknowledgement", output)
	}
}

func TestConsoleLocalRuntimeStopTaskByTaskIDAndTopicID(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	rt := &consoleLocalRuntime{
		store:      store,
		runControl: runtimecontrol.New(),
	}

	startActive := func(taskID string) context.Context {
		t.Helper()
		ctx, cancel := context.WithCancelCause(context.Background())
		if err := rt.runControl.Start(runtimecontrol.ActiveRun{
			Runtime:         "console",
			ConversationKey: buildConsoleConversationKey(topic.ID),
			TopicID:         topic.ID,
			TaskID:          taskID,
			Cancel:          cancel,
			Snapshot: func() string {
				return "计划 1/2"
			},
		}); err != nil {
			t.Fatalf("RunControl.Start() error = %v", err)
		}
		return ctx
	}

	taskCtx := startActive("task_active")
	byTask, err := rt.stopTask(context.Background(), daemonruntime.StopTaskRequest{
		TaskID: "task_active",
	})
	if err != nil {
		t.Fatalf("stopTask(task_id) error = %v", err)
	}
	if !byTask.Found || byTask.TaskID != "task_active" || byTask.Status != "stopping" || byTask.Progress != "计划 1/2" {
		t.Fatalf("stopTask(task_id) = %+v", byTask)
	}
	if !errors.Is(context.Cause(taskCtx), runtimecontrol.ErrStoppedByUser) {
		t.Fatalf("task context cause = %v, want ErrStoppedByUser", context.Cause(taskCtx))
	}
	rt.runControl.Finish("console", buildConsoleConversationKey(topic.ID), "task_active")

	topicCtx := startActive("task_topic")
	byTopic, err := rt.stopTask(context.Background(), daemonruntime.StopTaskRequest{
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("stopTask(topic_id) error = %v", err)
	}
	if !byTopic.Found || byTopic.TopicID != topic.ID || byTopic.Status != "stopping" || byTopic.Progress != "计划 1/2" {
		t.Fatalf("stopTask(topic_id) = %+v", byTopic)
	}
	if !errors.Is(context.Cause(topicCtx), runtimecontrol.ErrStoppedByUser) {
		t.Fatalf("topic context cause = %v, want ErrStoppedByUser", context.Cause(topicCtx))
	}
}

func TestConsoleLocalRuntimeStopCommandCancelsRunningTask(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	reader := viper.New()
	reader.Set("timeout", time.Minute)
	logger := slog.Default()
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "test-model",
		},
	}
	client := &consoleBlockingLLMClient{entered: make(chan struct{}, 1)}
	baseRegistry := tools.NewRegistry()
	commonDeps := depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return logger, nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return baseRegistry
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
	}
	taskRuntime, err := taskruntime.Bootstrap(commonDeps, taskruntime.BootstrapOptions{
		AgentConfig: agent.Config{
			MaxSteps:        1,
			ParseRetries:    0,
			ToolRepeatLimit: 1,
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	generation := &consoleLocalRuntimeGeneration{
		reader:     reader,
		logger:     logger,
		commonDeps: commonDeps,
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: taskRuntime,
		},
	}
	generation.acquire()
	rt := &consoleLocalRuntime{
		store:      store,
		streamHub:  newConsoleStreamHub(),
		generation: generation,
		runControl: runtimecontrol.New(),
	}
	job := consoleLocalTaskJob{
		TaskID:          "task_running",
		ConversationKey: buildConsoleConversationKey(topic.ID),
		TopicID:         topic.ID,
		Task:            "long task",
		Timeout:         time.Minute,
		CreatedAt:       time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC),
		Trigger:         daemonruntime.TaskTrigger{Source: "ui"},
		Generation:      generation,
	}
	if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        job.TaskID,
		Status:    daemonruntime.TaskQueued,
		Task:      job.Task,
		Model:     "test-model",
		Timeout:   job.Timeout.String(),
		CreatedAt: job.CreatedAt,
		TopicID:   topic.ID,
	}, job.Trigger, ""); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rt.handleTaskJob(context.Background(), job.ConversationKey, job)
	}()

	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("LLM client was not called")
	}
	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task:    "/stop",
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("submitTask(/stop) error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("stop resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("running task did not stop")
	}
	task, ok := store.Get(job.TaskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", job.TaskID)
	}
	if task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task.Status = %q, want %q; error=%q", task.Status, daemonruntime.TaskCanceled, task.Error)
	}
	if strings.TrimSpace(task.Error) != "stopped by user" {
		t.Fatalf("task.Error = %q, want stopped by user", task.Error)
	}
}

func TestConsoleLocalRuntimeSubmitTaskSteersRunningTask(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	control := runtimecontrol.New()
	queue := runtimecontrol.NewSteerQueue(0)
	rt := &consoleLocalRuntime{
		store:      store,
		generation: generation,
		runControl: control,
	}
	runCtx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	conversationKey := buildConsoleConversationKey(topic.ID)
	if err := control.Start(runtimecontrol.ActiveRun{
		Runtime:         "console",
		ConversationKey: conversationKey,
		TopicID:         topic.ID,
		TaskID:          "task_active",
		RunID:           "task_active",
		Cancel:          cancel,
		SteerQueue:      queue,
	}); err != nil {
		t.Fatalf("RunControl.Start() error = %v", err)
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task:    "请改成简短回答",
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("submitTask(steer) error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}
	if runCtx.Err() != nil {
		t.Fatalf("steer canceled active run: %v", runCtx.Err())
	}
	items := queue.Drain()
	if len(items) != 1 || strings.TrimSpace(items[0]) != "请改成简短回答" {
		t.Fatalf("steer queue = %#v, want one queued input", items)
	}
	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", resp.ID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	output := strings.TrimSpace(fmt.Sprint(final["output"]))
	if output != runtimecontrol.FeedbackSteerAccepted {
		t.Fatalf("final.output = %q, want steer acknowledgement", output)
	}
}
