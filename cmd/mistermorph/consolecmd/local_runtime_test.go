package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	awarenessloop "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

type consoleNoopLLMClient struct{}

func (consoleNoopLLMClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{Text: "ok"}, nil
}

func TestConsoleLLMCredentialsWarningUsesXAIOAuthTokenStore(t *testing.T) {
	stateDir := t.TempDir()
	route := llmutil.ResolvedRoute{
		Values: llmutil.RuntimeValues{FileStateDir: stateDir},
		ClientConfig: llmconfig.ClientConfig{
			Provider: "xai_oauth",
			Model:    "grok-4.5",
		},
	}

	if got := consoleLLMCredentialsWarning(route); got != "sign in with xAI Grok OAuth to enable Console Local chat submit" {
		t.Fatalf("warning before login = %q", got)
	}
	if err := xaiauth.WriteToken(stateDir, xaiauth.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("xaiauth.WriteToken() error = %v", err)
	}
	if got := consoleLLMCredentialsWarning(route); got != "" {
		t.Fatalf("warning after login = %q, want empty", got)
	}
}

type consoleTopicTitleLLMClient struct {
	request    llm.Request
	closeCalls int
}

func (c *consoleTopicTitleLLMClient) Chat(_ context.Context, request llm.Request) (llm.Result, error) {
	c.request = request
	return llm.Result{Text: "Selected title"}, nil
}

func (c *consoleTopicTitleLLMClient) Close() error {
	c.closeCalls++
	return nil
}

type consoleFinalLLMClient struct {
	calls int
}

func (c *consoleFinalLLMClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	c.calls++
	return llm.Result{Text: `{"type":"final","output":"persisted final","is_lightweight":false}`}, nil
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
	if err := rt.routesOptions("token").Poke(context.Background(), awarenessdomain.PokeInput{}); err == nil {
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
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
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

func TestConsolePendingApprovalExpiryTerminatesTaskAndReleasesGeneration(t *testing.T) {
	rt, approvalID, taskID := newConsoleApprovalTestRuntimeWithExpiry(t, time.Now().UTC().Add(30*time.Millisecond))
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: buildConsoleConversationKey("topic_a"),
		TopicID:         "topic_a",
		Task:            "run bash",
		Timeout:         time.Minute,
		Generation:      rt.generation,
	}
	rt.registerPendingApproval(approvalID, job)
	oldGeneration := rt.generation
	oldGeneration.retire()

	waitForConsoleApprovalState(t, time.Second, func() bool {
		task, ok := rt.store.Get(taskID)
		return ok && task.Status == daemonruntime.TaskCanceled
	})
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("expired approval handle remained registered")
	}
	rec, ok, err := oldGeneration.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalExpired {
		t.Fatalf("approval status = %s, want expired", rec.Status)
	}
	task, _ := rt.store.Get(taskID)
	if task.Error != runtimecore.ApprovalExpiredTaskError || task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("expired task = %+v", task)
	}
	oldGeneration.mu.Lock()
	refs, cleaned := oldGeneration.refs, oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refs != 0 || !cleaned {
		t.Fatalf("old generation refs/cleaned = %d/%v, want 0/true", refs, cleaned)
	}
}

func TestConsoleDenyApprovalAfterReloadUsesPendingJobGeneration(t *testing.T) {
	rt, approvalID, _ := newConsoleApprovalTestRuntime(t)
	oldGeneration := rt.generation
	job := consoleLocalTaskJob{
		TaskID:          "task_pending",
		ConversationKey: buildConsoleConversationKey("topic_a"),
		Generation:      oldGeneration,
	}
	rt.registerPendingApproval(approvalID, job)
	newGeneration, newGuard := newConsoleApprovalGeneration(t, approvalID)
	rt.generation = newGeneration
	oldGeneration.retire()
	listed, err := rt.listApprovals(context.Background(), daemonruntime.ApprovalListRequest{Status: "pending", Limit: 10})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ApprovalRequestID != approvalID {
		t.Fatalf("listApprovals() = %+v, err=%v; want old-generation pending approval", listed, err)
	}

	if _, err := rt.denyApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:user",
	}); err != nil {
		t.Fatalf("denyApproval() error = %v", err)
	}
	assertApprovalStatus(t, oldGeneration.bundle.taskRuntime.SharedGuard, approvalID, guard.ApprovalDenied)
	assertApprovalStatus(t, newGuard, approvalID, guard.ApprovalPending)
	oldGeneration.mu.Lock()
	refs, cleaned := oldGeneration.refs, oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refs != 0 || !cleaned {
		t.Fatalf("old generation refs/cleaned = %d/%v, want 0/true", refs, cleaned)
	}
}

func TestConsoleApproveApprovalAfterReloadUsesPendingJobGeneration(t *testing.T) {
	rt, approvalID, _ := newConsoleApprovalTestRuntime(t)
	oldGeneration := rt.generation
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan consoleLocalTaskJob, 1)
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) { jobs <- job },
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
	)
	rt.registerPendingApproval(approvalID, consoleLocalTaskJob{
		TaskID:          "task_pending",
		ConversationKey: buildConsoleConversationKey("topic_a"),
		Generation:      oldGeneration,
	})
	newGeneration, newGuard := newConsoleApprovalGeneration(t, approvalID)
	rt.generation = newGeneration
	oldGeneration.retire()

	if _, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:user",
	}); err != nil {
		t.Fatalf("approveApproval() error = %v", err)
	}
	select {
	case queued := <-jobs:
		queued.Generation.release()
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resumed job")
	}
	assertApprovalStatus(t, oldGeneration.bundle.taskRuntime.SharedGuard, approvalID, guard.ApprovalApproved)
	assertApprovalStatus(t, newGuard, approvalID, guard.ApprovalPending)
	oldGeneration.mu.Lock()
	refs, cleaned := oldGeneration.refs, oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refs != 0 || !cleaned {
		t.Fatalf("old generation refs/cleaned = %d/%v, want 0/true", refs, cleaned)
	}
}

func newConsoleApprovalTestRuntime(t *testing.T) (*consoleLocalRuntime, string, string) {
	return newConsoleApprovalTestRuntimeWithExpiry(t, time.Now().UTC().Add(time.Hour))
}

func newConsoleApprovalTestRuntimeWithExpiry(t *testing.T, expiresAt time.Time) (*consoleLocalRuntime, string, string) {
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
		ExpiresAt:             expiresAt,
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
	rt := &consoleLocalRuntime{
		store:      taskStore,
		generation: generation,
		streamHub:  newConsoleStreamHub(),
	}
	rt.consoleExecutionState = newConsoleExecutionState(rt.expirePendingApproval, rt.closePendingApproval)
	t.Cleanup(rt.Close)
	return rt, approvalID, taskID
}

func newConsoleApprovalGeneration(t *testing.T, approvalID string) (*consoleLocalRuntimeGeneration, *guard.Guard) {
	t.Helper()
	root := t.TempDir()
	store, err := guard.NewFileApprovalStore(filepath.Join(root, "guard_approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	if _, err := store.Create(context.Background(), guard.ApprovalRecord{
		ID:         approvalID,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "new-generation-hash",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, store)
	return &consoleLocalRuntimeGeneration{
		reader: viper.New(),
		bundle: &consoleLocalRuntimeBundle{taskRuntime: &taskruntime.Runtime{SharedGuard: g}},
	}, g
}

func assertApprovalStatus(t *testing.T, g *guard.Guard, approvalID string, want guard.ApprovalStatus) {
	t.Helper()
	rec, ok, err := g.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval(%q) ok=%v err=%v", approvalID, ok, err)
	}
	if rec.Status != want {
		t.Fatalf("approval %q status = %s, want %s", approvalID, rec.Status, want)
	}
}

func waitForConsoleApprovalState(t *testing.T, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for approval state")
}

func TestConsoleLocalRoutesOptionsOverviewOmitsAwarenessRunning(t *testing.T) {
	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("file_cache_dir", t.TempDir())
	reader.Set("telegram.bot_token", "tg-token")
	reader.Set("slack.bot_token", "slack-bot")
	reader.Set("slack.app_token", "slack-app")
	paths := runtimepaths.FromReader(reader)
	rt := &consoleLocalRuntime{
		generation: &consoleLocalRuntimeGeneration{
			reader: reader,
			paths:  paths,
			commonDeps: depsutil.CommonDependencies{
				ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
					return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "test-model"}}, nil
				},
			},
		},
		runtimePaths:          paths,
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
		consoleExecutionState: &consoleExecutionState{workersCtx: workersCtx},
		generation:            consoleLocalAwarenessTestGeneration(reader),
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
	reader.Set("guard.enabled", true)

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

	bundle, builtDeps, err := buildConsoleLocalRuntimeBundle(logger, nil, snapshot)
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
	additionalGuard, err := builtDeps.Guard(logger)
	if err != nil {
		t.Fatalf("Guard() error = %v", err)
	}
	if additionalGuard == nil {
		t.Fatal("Guard() returned nil for enabled guard")
	}
	t.Cleanup(func() {
		_ = additionalGuard.Close()
		_ = bundle.taskRuntime.Close()
	})
	if additionalGuard == bundle.taskRuntime.SharedGuard {
		t.Fatal("Guard() reused task runtime guard, want a caller-owned instance")
	}
}

func TestConsoleGenerationCleanupClosesTaskRuntime(t *testing.T) {
	sink := &consoleLifecycleAuditSink{}
	generation := &consoleLocalRuntimeGeneration{
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: &taskruntime.Runtime{
				SharedGuard: guard.New(guard.Config{Enabled: true}, sink, nil),
			},
		},
	}

	generation.cleanupNow()
	generation.cleanupNow()
	if sink.closeCalls != 1 {
		t.Fatalf("task runtime guard close calls = %d, want 1", sink.closeCalls)
	}
}

func TestConsoleLocalRuntimeMessageReactContinuesToFinalText(t *testing.T) {
	client := &consoleReactLLMClient{}
	logger := slog.Default()
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "report.pdf"), []byte("report"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "diagram.png"), []byte("image-data"), 0o600); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}
	reader := viper.New()
	reader.Set("file_cache_dir", t.TempDir())
	reader.Set("file_state_dir", t.TempDir())
	paths := runtimepaths.FromReader(reader)
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "gpt-5.2",
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
		RuntimePaths: paths,
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
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
		reader:     reader,
		logger:     logger,
		commonDeps: commonDeps,
		paths:      paths,
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: taskRuntime,
		},
	}
	rt := &consoleLocalRuntime{}
	final, runCtx, err := rt.runTask(context.Background(), "topic_a", consoleLocalTaskJob{
		TaskID:          "task_react",
		TopicID:         "topic_a",
		ConversationKey: "topic_a",
		WorkspaceDir:    workspaceDir,
		Task:            "Hi",
		FileReferences: []daemonruntime.FileReference{
			{DirName: "workspace_dir", Path: "report.pdf"},
			{DirName: "workspace_dir", Path: "diagram.png"},
		},
		CreatedAt:  time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC),
		Generation: generation,
		Trigger:    daemonruntime.TaskTrigger{Source: "ui", TraceID: "trace_react"},
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
	requestJSON, err := json.Marshal(client.requests[0].Messages)
	if err != nil {
		t.Fatalf("json.Marshal(messages) error = %v", err)
	}
	if !strings.Contains(string(requestJSON), "## Task File References") || !strings.Contains(string(requestJSON), `\"path\":\"report.pdf\"`) {
		t.Fatalf("request messages missing file reference prompt: %s", string(requestJSON))
	}
	foundImagePart := false
	for _, message := range client.requests[0].Messages {
		for _, part := range message.Parts {
			if part.Type == llm.PartTypeImageBase64 && part.MIMEType == "image/png" && part.DataBase64 != "" {
				foundImagePart = true
			}
		}
	}
	if !foundImagePart {
		t.Fatalf("request messages missing PNG image part: %#v", client.requests[0].Messages)
	}
	for _, message := range client.requests[0].Messages {
		if message.Role == "user" && strings.Contains(message.Content, `"current_message"`) && strings.Contains(message.Content, "report.pdf") {
			t.Fatalf("current user message contains a file reference: %s", message.Content)
		}
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

func TestConsoleGenerateTopicTitleSelectsWeightedRouteBeforeClient(t *testing.T) {
	weightedRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "weighted-title",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "title-a", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "title-a-model"}},
			{Profile: "title-b", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "title-b-model"}},
		},
	}
	client := &consoleTopicTitleLLMClient{}
	var builtRoute llmutil.ResolvedRoute
	generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) { return weightedRoute, nil },
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			builtRoute = route
			return client, nil
		},
	}}
	const runID = "console-title-run"

	title, err := (&consoleLocalRuntime{}).generateTopicTitle(llmstats.WithRunID(context.Background(), runID), generation, "summarize", "long output")
	if err != nil {
		t.Fatalf("generateTopicTitle() error = %v", err)
	}
	if title != "Selected title" {
		t.Fatalf("title = %q, want Selected title", title)
	}
	want := llmutil.SelectRouteCandidate(weightedRoute, runID)
	if len(builtRoute.Candidates) != 0 || builtRoute.ClientConfig.Model != want.ClientConfig.Model {
		t.Fatalf("built route = %#v, want concrete model %q", builtRoute, want.ClientConfig.Model)
	}
	if client.request.Model != want.ClientConfig.Model {
		t.Fatalf("request model = %q, want %q", client.request.Model, want.ClientConfig.Model)
	}
	if client.closeCalls != 1 {
		t.Fatalf("client close calls = %d, want 1", client.closeCalls)
	}
}

func TestConsoleGenerateTopicTitleClosesClientReturnedWithCreateError(t *testing.T) {
	client := &consoleTopicTitleLLMClient{}
	wantErr := errors.New("create failed")
	generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "test-model"}}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, wantErr
		},
	}}

	_, err := (&consoleLocalRuntime{}).generateTopicTitle(context.Background(), generation, "summarize", "output")
	if !errors.Is(err, wantErr) {
		t.Fatalf("generateTopicTitle() error = %v, want %v", err, wantErr)
	}
	if client.closeCalls != 1 {
		t.Fatalf("client close calls = %d, want 1", client.closeCalls)
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
		WakeSignal: awarenessdomain.PokeInput{},
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

func TestBuildConsoleTopicHistorySkipsContextCompactTasks(t *testing.T) {
	base := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	finishedAt := base.Add(time.Minute)
	tasks := []daemonruntime.TaskInfo{
		{
			ID:         "task_normal",
			Status:     daemonruntime.TaskDone,
			Task:       "normal question",
			CreatedAt:  base,
			FinishedAt: &finishedAt,
			TopicID:    "topic_a",
			Result:     map[string]any{"output": "normal answer"},
		},
		{
			ID:         "task_compact",
			Status:     daemonruntime.TaskDone,
			Task:       "/ctx compact",
			CreatedAt:  base.Add(2 * time.Minute),
			FinishedAt: &finishedAt,
			TopicID:    "topic_a",
			Result:     map[string]any{"output": "Context compacted."},
		},
	}

	history := buildConsoleTopicHistory(tasks, consoleLocalTaskJob{
		TaskID:    "task_current",
		TopicID:   "topic_a",
		Task:      "current question",
		CreatedAt: base.Add(3 * time.Minute),
	}, consoleHistoryRestoreTaskLimit)
	if len(history) != 2 || history[0].Text != "normal question" || history[1].Text != "normal answer" {
		t.Fatalf("history = %#v", history)
	}
}

func TestBuildConsoleTopicHistoryPlacesSteersBeforeTargetReply(t *testing.T) {
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	targetFinishedAt := base.Add(3 * time.Minute)
	firstSteerFinishedAt := base.Add(time.Minute + time.Second)
	secondSteerFinishedAt := base.Add(2*time.Minute + time.Second)
	tasks := []daemonruntime.TaskInfo{
		{
			ID:                "task_steer_2",
			Status:            daemonruntime.TaskDone,
			Task:              "再短一点",
			CreatedAt:         base.Add(2 * time.Minute),
			FinishedAt:        &secondSteerFinishedAt,
			TopicID:           "topic_a",
			SteerTargetTaskID: "task_target",
			Result:            map[string]any{"output": runtimecontrol.FeedbackSteerAccepted},
		},
		{
			ID:                "task_steer_1",
			Status:            daemonruntime.TaskDone,
			Task:              "改成这个风格",
			CreatedAt:         base.Add(time.Minute),
			FinishedAt:        &firstSteerFinishedAt,
			TopicID:           "topic_a",
			SteerTargetTaskID: "task_target",
			Result:            map[string]any{"output": runtimecontrol.FeedbackSteerAccepted},
		},
		{
			ID:         "task_target",
			Status:     daemonruntime.TaskDone,
			Task:       "写一个故事",
			CreatedAt:  base,
			FinishedAt: &targetFinishedAt,
			TopicID:    "topic_a",
			Result:     map[string]any{"output": "最终故事"},
		},
	}

	history := buildConsoleTopicHistory(tasks, consoleLocalTaskJob{
		TaskID:    "task_current",
		TopicID:   "topic_a",
		Task:      "继续",
		CreatedAt: base.Add(4 * time.Minute),
	}, consoleHistoryRestoreTaskLimit)

	gotTexts := make([]string, 0, len(history))
	for _, item := range history {
		gotTexts = append(gotTexts, item.Text)
	}
	wantTexts := []string{"写一个故事", "改成这个风格", "再短一点", "最终故事"}
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
		store:                 store,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			jobs <- job
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
	)

	oldReader := viper.New()
	oldReader.Set("timeout", "2m")
	newReader := viper.New()
	newReader.Set("timeout", "9m")
	oldGeneration := &consoleLocalRuntimeGeneration{
		generation: 1,
		reader:     oldReader,
		commonDeps: depsutil.CommonDependencies{
			ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
				return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "old-model"}}, nil
			},
		},
	}
	newGeneration := &consoleLocalRuntimeGeneration{generation: 2, reader: newReader}
	rt.generation = newGeneration

	oldGeneration.acquire()
	job, _, err := rt.acceptTask(
		oldGeneration,
		"hello",
		"",
		"",
		time.Minute,
		"",
		"",
		"",
		nil,
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

func TestConsoleLocalRuntimeSubmitTaskStoresResolvedModel(t *testing.T) {
	stateRoot := t.TempDir()
	journalDir := filepath.Join(stateRoot, "journal")
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    filepath.Join(stateRoot, "tasks", "console"),
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	logger := slog.Default()
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{
		MaxInFlight: 4,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(context.Context, busruntime.BusMessage) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	reader := viper.New()
	reader.Set("timeout", time.Minute)
	generation := &consoleLocalRuntimeGeneration{
		reader: reader,
		logger: logger,
		commonDeps: depsutil.CommonDependencies{
			ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
				model := "main-model"
				if purpose == llmutil.RoutePurposeThink {
					model = "think-model"
				}
				return llmutil.ResolvedRoute{
					Purpose: purpose,
					ClientConfig: llmconfig.ClientConfig{
						Provider: "test",
						Model:    model,
					},
				}, nil
			},
			ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
				model := profile + "-main-model"
				if purpose == llmutil.RoutePurposeThink {
					model = profile + "-think-model"
				}
				return llmutil.ResolvedRoute{
					Purpose: purpose,
					Profile: profile,
					ClientConfig: llmconfig.ClientConfig{
						Provider: "test",
						Model:    model,
					},
				}, nil
			},
		},
	}
	rt := &consoleLocalRuntime{
		store:                 store,
		bus:                   bus,
		generation:            generation,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}

	tests := []struct {
		name             string
		task             string
		requestedModel   string
		requestedProfile string
		wantModel        string
		wantRouteModel   string
		wantProfile      string
	}{
		{
			name:           "main route",
			task:           "hello",
			wantModel:      "main-model",
			wantRouteModel: "main-model",
		},
		{
			name:           "requested model",
			task:           "hello",
			requestedModel: "requested-model",
			wantModel:      "requested-model",
			wantRouteModel: "main-model",
		},
		{
			name:           "think route",
			task:           "/think analyze this",
			requestedModel: "requested-model",
			wantModel:      "think-model",
			wantRouteModel: "think-model",
		},
		{
			name:             "selected profile",
			task:             "hello",
			requestedModel:   "ignored-model",
			requestedProfile: "cheap",
			wantModel:        "cheap-main-model",
			wantRouteModel:   "cheap-main-model",
			wantProfile:      "cheap",
		},
		{
			name:             "think route with selected profile",
			task:             "/think analyze this",
			requestedProfile: "cheap",
			wantModel:        "cheap-think-model",
			wantRouteModel:   "cheap-think-model",
			wantProfile:      "cheap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
				Task:       tt.task,
				Model:      tt.requestedModel,
				LLMProfile: tt.requestedProfile,
			})
			if err != nil {
				t.Fatalf("submitTask() error = %v", err)
			}
			job, ok := rt.takePendingJob(resp.ID)
			if !ok {
				t.Fatalf("takePendingJob(%q) missing", resp.ID)
			}
			if ok && job.Generation != nil {
				job.Generation.release()
			}
			if job.Route == nil {
				t.Fatalf("queued job route = nil")
			}
			if got := strings.TrimSpace(job.Route.ClientConfig.Model); got != strings.TrimSpace(tt.wantRouteModel) {
				t.Fatalf("queued route model = %q, want %q", got, tt.wantRouteModel)
			}
			if tt.requestedProfile != "" && job.Route.Profile != tt.requestedProfile {
				t.Fatalf("queued route profile = %q, want %q", job.Route.Profile, tt.requestedProfile)
			}
			stored, ok := store.Get(resp.ID)
			if !ok || stored == nil {
				t.Fatalf("store.Get(%q) missing", resp.ID)
			}
			if stored.Model != tt.wantModel {
				t.Fatalf("stored.Model = %q, want %q", stored.Model, tt.wantModel)
			}
			if stored.LLMProfile != tt.wantProfile {
				t.Fatalf("stored.LLMProfile = %q, want %q", stored.LLMProfile, tt.wantProfile)
			}

			indexRecords, err := domainjournal.ReadIndexDir(journalDir, "task", resp.ID, 10)
			if err != nil {
				t.Fatalf("ReadIndexDir() error = %v", err)
			}
			if len(indexRecords) == 0 {
				t.Fatalf("task journal index for %q is empty", resp.ID)
			}
			record, err := domainjournal.ReadAtDir(journalDir, indexRecords[len(indexRecords)-1].Ref)
			if err != nil {
				t.Fatalf("ReadAtDir() error = %v", err)
			}
			var payload struct {
				Task *daemonruntime.TaskInfo `json:"task"`
			}
			if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
				t.Fatalf("json.Unmarshal(task journal payload) error = %v", err)
			}
			if payload.Task == nil || payload.Task.Model != tt.wantModel || payload.Task.LLMProfile != tt.wantProfile {
				t.Fatalf("journal task = %#v, want model/profile %q/%q", payload.Task, tt.wantModel, tt.wantProfile)
			}
			topicRecords, err := domainjournal.ReadIndexDir(journalDir, "topic", resp.TopicID, 10)
			if err != nil {
				t.Fatalf("ReadIndexDir(topic) error = %v", err)
			}
			if len(topicRecords) != 1 {
				t.Fatalf("topic journal event count = %d, want one atomic task+topic event", len(topicRecords))
			}
		})
	}

	t.Run("weighted route stores selected candidate", func(t *testing.T) {
		weightedRoute := llmutil.ResolvedRoute{
			Purpose:  llmutil.RoutePurposeMainLoop,
			Identity: "weighted-console",
			Candidates: []llmutil.ResolvedCandidate{
				{Profile: "small", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "small-model"}},
				{Profile: "large", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "large-model"}},
			},
		}
		generation.commonDeps.ResolveLLMRoute = func(string) (llmutil.ResolvedRoute, error) {
			return weightedRoute, nil
		}
		resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{Task: "weighted"})
		if err != nil {
			t.Fatalf("submitTask() error = %v", err)
		}
		job, ok := rt.takePendingJob(resp.ID)
		if !ok {
			t.Fatalf("takePendingJob(%q) missing", resp.ID)
		}
		if ok && job.Generation != nil {
			job.Generation.release()
		}
		stored, ok := store.Get(resp.ID)
		if !ok || stored == nil {
			t.Fatalf("store.Get(%q) missing", resp.ID)
		}
		want := llmutil.SelectRouteCandidate(weightedRoute, resp.ID).ClientConfig.Model
		if stored.Model != want {
			t.Fatalf("stored.Model = %q, selected model = %q", stored.Model, want)
		}
		if job.Route == nil || len(job.Route.Candidates) != 0 || job.Route.ClientConfig.Model != want {
			t.Fatalf("queued route = %#v, want frozen candidate %q", job.Route, want)
		}
		generation.commonDeps.ResolveLLMRoute = func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "changed", Model: "changed-model"}}, nil
		}
		if job.Route.ClientConfig.Model != want {
			t.Fatalf("queued route changed to %q after resolver update, want %q", job.Route.ClientConfig.Model, want)
		}
	})
}

func TestConsoleSyntheticTaskUpdateFailureReleasesOnlyCallerOwnership(t *testing.T) {
	runtime := newConsoleGenerationTestRuntime(t, t.TempDir(), t.TempDir())
	generation, err := runtime.captureGeneration()
	if err != nil {
		t.Fatalf("captureGeneration() error = %v", err)
	}
	generation.acquire()
	updateErr := errors.New("synthetic task update unavailable")
	runtime.taskUpdater = &consoleApprovalUpdateErrorStore{
		TaskUpdater: runtime.store,
		err:         updateErr,
		failures:    1,
	}

	_, err = runtime.submitTaskWithGeneration(context.Background(), generation, daemonruntime.SubmitTaskRequest{Task: "/stop"})
	if !errors.Is(err, updateErr) {
		t.Fatalf("submitTaskWithGeneration() error = %v, want %v", err, updateErr)
	}
	if got := consoleGenerationRefs(generation); got != 1 {
		t.Fatalf("generation refs after failed synthetic task = %d, want second owner ref", got)
	}
	generation.release()
}

func TestResolveConsoleAdmittedRouteFallsBackWhenRouteResolverIsMissing(t *testing.T) {
	generation := &consoleLocalRuntimeGeneration{
		reader: viper.New(),
		bundle: &consoleLocalRuntimeBundle{defaultModel: "fallback-model"},
	}
	route, model, err := resolveConsoleAdmittedRoute(generation, "hello", "", "", "task_test")
	if err != nil {
		t.Fatalf("resolveConsoleAdmittedRoute() error = %v", err)
	}
	if model != "fallback-model" {
		t.Fatalf("resolved model = %q, want fallback-model", model)
	}
	if route.ClientConfig.Model != "fallback-model" {
		t.Fatalf("route model = %q, want fallback-model", route.ClientConfig.Model)
	}
}

func TestResolveConsoleTaskRouteFixesProfileCandidateForRun(t *testing.T) {
	weightedRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "weighted-console-profile",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "text", Weight: 1, Values: llmutil.RuntimeValues{SupportsImageParts: boolPointer(false)}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "text-model"}},
			{Profile: "vision", Weight: 1, Values: llmutil.RuntimeValues{SupportsImageParts: boolPointer(true)}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "vision-model"}},
		},
	}
	generation := &consoleLocalRuntimeGeneration{
		commonDeps: depsutil.CommonDependencies{
			ResolveLLMRouteWithProfile: func(purpose string, _ string) (llmutil.ResolvedRoute, error) {
				if purpose != llmutil.RoutePurposeMainLoop {
					return llmutil.ResolvedRoute{}, fmt.Errorf("route purpose = %q, want main_loop", purpose)
				}
				return weightedRoute, nil
			},
		},
	}
	keys := map[bool]string{}
	for index := 0; index < 100 && len(keys) < 2; index++ {
		key := fmt.Sprintf("console-profile-run-%d", index)
		selected := llmutil.SelectRouteCandidate(weightedRoute, key)
		keys[*selected.Values.SupportsImageParts] = key
	}
	if len(keys) != 2 {
		t.Fatalf("failed to find selection keys for both weighted candidates: %#v", keys)
	}
	imagePath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o600); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}

	for supportsImages, runID := range keys {
		t.Run(fmt.Sprintf("supports_images_%t", supportsImages), func(t *testing.T) {
			ctx := llmstats.WithRunID(context.Background(), runID)
			got, err := resolveConsoleTaskRoute(ctx, generation, nil, "", "selected-profile")
			if err != nil {
				t.Fatalf("resolveConsoleTaskRoute() error = %v", err)
			}
			want := llmutil.SelectRouteCandidate(weightedRoute, runID)
			if len(got.Candidates) != 0 || got.ClientConfig.Model != want.ClientConfig.Model {
				t.Fatalf("resolved route = %#v, want concrete model %q", got, want.ClientConfig.Model)
			}
			if got.Values.SupportsImageParts == nil || *got.Values.SupportsImageParts != supportsImages {
				t.Fatalf("supports_image_parts = %#v, want %t", got.Values.SupportsImageParts, supportsImages)
			}

			_, currentMessage, err := renderConsolePromptMessages(nil, consoleLocalTaskJob{Task: "inspect image"}, got.ClientConfig.Model, got.Values.SupportsImageParts, []string{imagePath}, slog.Default())
			if err != nil {
				t.Fatalf("renderConsolePromptMessages() error = %v", err)
			}
			hasImagePart := false
			for _, part := range currentMessage.Parts {
				if part.Type == llm.PartTypeImageBase64 {
					hasImagePart = true
				}
			}
			if hasImagePart != supportsImages {
				t.Fatalf("has image part = %t, selected capability = %t; message=%#v", hasImagePart, supportsImages, currentMessage)
			}
		})
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

	generation := &consoleLocalRuntimeGeneration{
		reader: viper.New(),
		commonDeps: depsutil.CommonDependencies{
			ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
				return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test-model"}}, nil
			},
		},
	}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}

	job, _, err := rt.acceptTask(
		generation,
		"hello",
		"",
		"",
		time.Minute,
		topic.ID,
		"",
		"",
		nil,
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

func TestValidateConsoleFileReferences(t *testing.T) {
	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "report.pdf"), []byte("workspace"), 0o600); err != nil {
		t.Fatalf("WriteFile(workspace) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "uploads"), 0o700); err != nil {
		t.Fatalf("MkdirAll(cache) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "uploads", "notes.txt"), []byte("cache"), 0o600); err != nil {
		t.Fatalf("WriteFile(cache) error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(workspaceDir, "folder"), 0o700); err != nil {
		t.Fatalf("Mkdir(folder) error = %v", err)
	}

	tests := []struct {
		name         string
		references   []daemonruntime.FileReference
		workspaceDir string
		want         []daemonruntime.FileReference
		wantError    string
	}{
		{
			name: "workspace and cache files",
			references: []daemonruntime.FileReference{
				{DirName: "workspace_dir", Path: "report.pdf"},
				{DirName: "file_cache_dir", Path: "uploads/notes.txt"},
			},
			workspaceDir: workspaceDir,
			want: []daemonruntime.FileReference{
				{DirName: "workspace_dir", Path: "report.pdf"},
				{DirName: "file_cache_dir", Path: "uploads/notes.txt"},
			},
		},
		{
			name:       "invalid alias",
			references: []daemonruntime.FileReference{{DirName: "file_state_dir", Path: "report.pdf"}},
			wantError:  "invalid dir_name",
		},
		{
			name:         "workspace unavailable",
			references:   []daemonruntime.FileReference{{DirName: "workspace_dir", Path: "report.pdf"}},
			workspaceDir: "",
			wantError:    "workspace_dir is not available",
		},
		{
			name:         "absolute path",
			references:   []daemonruntime.FileReference{{DirName: "workspace_dir", Path: filepath.Join(workspaceDir, "report.pdf")}},
			workspaceDir: workspaceDir,
			wantError:    "path must be relative",
		},
		{
			name:         "path escape",
			references:   []daemonruntime.FileReference{{DirName: "workspace_dir", Path: "../report.pdf"}},
			workspaceDir: workspaceDir,
			wantError:    "outside",
		},
		{
			name:         "missing file",
			references:   []daemonruntime.FileReference{{DirName: "workspace_dir", Path: "missing.pdf"}},
			workspaceDir: workspaceDir,
			wantError:    "does not exist",
		},
		{
			name:         "directory",
			references:   []daemonruntime.FileReference{{DirName: "workspace_dir", Path: "folder"}},
			workspaceDir: workspaceDir,
			wantError:    "regular file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateConsoleFileReferences(tt.references, tt.workspaceDir, cacheDir)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("validateConsoleFileReferences() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateConsoleFileReferences() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("validateConsoleFileReferences() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveConsoleImageReferencePaths(t *testing.T) {
	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	workspaceImage := filepath.Join(workspaceDir, "diagram.png")
	cacheImage := filepath.Join(cacheDir, "photo.webp")
	for path, content := range map[string]string{
		workspaceImage:                           "png",
		filepath.Join(workspaceDir, "notes.txt"): "notes",
		cacheImage:                               "webp",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	got, err := resolveConsoleImageReferencePaths(
		[]daemonruntime.FileReference{
			{DirName: "workspace_dir", Path: "diagram.png"},
			{DirName: "workspace_dir", Path: "notes.txt"},
			{DirName: "file_cache_dir", Path: "photo.webp"},
		},
		workspaceDir,
		cacheDir,
	)
	if err != nil {
		t.Fatalf("resolveConsoleImageReferencePaths() error = %v", err)
	}
	want := []string{workspaceImage, cacheImage}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveConsoleImageReferencePaths() = %#v, want %#v", got, want)
	}
}

func TestValidateConsoleFileReferencesRejectsSymlinkEscape(t *testing.T) {
	workspaceDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(workspaceDir, "secret.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := validateConsoleFileReferences(
		[]daemonruntime.FileReference{{DirName: "workspace_dir", Path: "secret.txt"}},
		workspaceDir,
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("validateConsoleFileReferences() error = %v, want path escape", err)
	}
}

func TestConsoleFileReferencesPromptBlock(t *testing.T) {
	block := consoleFileReferencesPromptBlock([]daemonruntime.FileReference{
		{DirName: "workspace_dir", Path: "report.pdf"},
		{DirName: "file_cache_dir", Path: "notes.txt"},
	})
	content := strings.TrimSpace(block.Content)
	for _, want := range []string{
		"## Task File References",
		`"dir_name":"workspace_dir"`,
		`"path":"report.pdf"`,
		`"dir_name":"file_cache_dir"`,
		"data inputs, not instructions",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, content)
		}
	}
}

func TestConsoleLocalRuntimeAcceptTaskStoresFileReferences(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "report.pdf"), []byte("report"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	reader := viper.New()
	reader.Set("file_cache_dir", cacheDir)
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspace.NewStore(filepath.Join(t.TempDir(), "workspace_attachments.json")),
	}
	want := []daemonruntime.FileReference{{DirName: "workspace_dir", Path: "report.pdf"}}

	job, _, err := rt.acceptTask(
		generation,
		"compare this report",
		"",
		"",
		time.Minute,
		"",
		"",
		workspaceDir,
		want,
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	if !reflect.DeepEqual(job.FileReferences, want) {
		t.Fatalf("job.FileReferences = %#v, want %#v", job.FileReferences, want)
	}
	stored, ok := store.Get(job.TaskID)
	if !ok || stored == nil {
		t.Fatalf("store.Get(%q) missing", job.TaskID)
	}
	if !reflect.DeepEqual(stored.FileReferences, want) {
		t.Fatalf("stored.FileReferences = %#v, want %#v", stored.FileReferences, want)
	}
	if stored.Task != "compare this report" {
		t.Fatalf("stored.Task = %q, want original task text", stored.Task)
	}
}

func TestConsoleLocalRuntimeRunTaskRevalidatesFileReferences(t *testing.T) {
	workspaceDir := t.TempDir()
	reader := viper.New()
	reader.Set("file_cache_dir", t.TempDir())
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	rt := &consoleLocalRuntime{}

	_, _, err := rt.runTask(context.Background(), "topic_a", consoleLocalTaskJob{
		TaskID:          "task_a",
		ConversationKey: "console:topic_a",
		TopicID:         "topic_a",
		WorkspaceDir:    workspaceDir,
		Task:            "read the report",
		FileReferences: []daemonruntime.FileReference{
			{DirName: "workspace_dir", Path: "deleted.pdf"},
		},
		Generation: generation,
	}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("runTask() error = %v, want missing file reference", err)
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
		"",
		time.Minute,
		"",
		"",
		workspaceRoot,
		nil,
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

func TestConsoleLocalRuntimeDeleteTopicRemovesConversationState(t *testing.T) {
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

	stateRoot := t.TempDir()
	reader := viper.New()
	reader.Set("file_state_dir", stateRoot)
	reader.Set("file_cache_dir", t.TempDir())
	paths := runtimepaths.FromReader(reader)
	checkpointStore, err := contextcheckpoint.NewFileStore(stateRoot, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := checkpointStore.Save(context.Background(), 0, agent.ContextCheckpoint{
		Version:  1,
		Revision: 1,
		Message:  llm.Message{Role: "user", Content: "checkpoint"},
	}); err != nil {
		t.Fatalf("checkpoint Save() error = %v", err)
	}
	runControl := runtimecontrol.New()
	conversationKey := buildConsoleConversationKey(topic.ID)
	lease, err := runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
		Runtime:         "console",
		ConversationKey: conversationKey,
		TaskID:          "active-task",
	})
	if err != nil {
		t.Fatalf("StartLease() error = %v", err)
	}
	defer lease.Finish()
	rt := &consoleLocalRuntime{
		store:                 store,
		workspaceStore:        workspaceStore,
		generation:            &consoleLocalRuntimeGeneration{reader: reader, paths: paths},
		runtimePaths:          paths,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	rt.runControl = runControl
	deleted, err := rt.deleteTopic(topic.ID)
	if err != nil || !deleted {
		t.Fatalf("deleteTopic(%q) = %v, %v; want true, nil", topic.ID, deleted, err)
	}
	if !lease.UserStopped() {
		t.Fatalf("active run cause = %v, want user stop", context.Cause(lease.Context))
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != "" {
		t.Fatalf("currentDir = %q, want empty after topic delete", currentDir)
	}
	if _, found, err := checkpointStore.Load(context.Background()); err != nil || found {
		t.Fatalf("checkpoint after topic delete found = %v, error = %v", found, err)
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
		store:                 store,
		generation:            generation,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	rt.runControl = control
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
		store:                 store,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
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
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("file_cache_dir", t.TempDir())
	reader.Set("timeout", time.Minute)
	paths := runtimepaths.FromReader(reader)
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
		RuntimePaths: paths,
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
		paths:      paths,
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime: taskRuntime,
		},
	}
	generation.acquire()
	rt := &consoleLocalRuntime{
		store:                 store,
		streamHub:             newConsoleStreamHub(),
		generation:            generation,
		runtimePaths:          paths,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
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
		store:                 store,
		generation:            generation,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	rt.runControl = control
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
	if resp.SteerTargetTaskID != "task_active" {
		t.Fatalf("resp.SteerTargetTaskID = %q, want task_active", resp.SteerTargetTaskID)
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
	if task.SteerTargetTaskID != "task_active" {
		t.Fatalf("task.SteerTargetTaskID = %q, want task_active", task.SteerTargetTaskID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	output := strings.TrimSpace(fmt.Sprint(final["output"]))
	if output != runtimecontrol.FeedbackSteerAccepted {
		t.Fatalf("final.output = %q, want steer acknowledgement", output)
	}
}

func TestConsoleTaskDoesNotPublishRunningWhenLifecycleWriteFails(t *testing.T) {
	runtime, store, client, job, journalDir := newConsoleLifecyclePersistenceTestRuntime(t)
	if err := os.Mkdir(filepath.Join(journalDir, "events.000000000000000003.jsonl"), 0o700); err != nil {
		t.Fatalf("Mkdir(blocked running segment) error = %v", err)
	}
	frames, unsubscribe := runtime.streamHub.Subscribe(job.TaskID)
	defer unsubscribe()

	runtime.handleTaskJob(context.Background(), job.ConversationKey, job)

	if frame, ok := runtime.streamHub.Latest(job.TaskID); ok {
		t.Fatalf("latest stream frame = %#v, want no published running state", frame)
	}
	select {
	case frame := <-frames:
		t.Fatalf("stream frame = %#v, want none", frame)
	default:
	}
	task, ok := store.Get(job.TaskID)
	if !ok || task == nil || task.Status != daemonruntime.TaskQueued {
		t.Fatalf("task after failed running write = %#v, ok=%v", task, ok)
	}
	if client.calls != 0 {
		t.Fatalf("LLM calls after failed running write = %d, want 0", client.calls)
	}
}

func TestConsoleTaskDoesNotPublishFinalWhenDoneWriteFails(t *testing.T) {
	runtime, store, client, job, journalDir := newConsoleLifecyclePersistenceTestRuntime(t)
	if err := os.Mkdir(filepath.Join(journalDir, "events.000000000000000004.jsonl"), 0o700); err != nil {
		t.Fatalf("Mkdir(blocked done segment) error = %v", err)
	}
	frames, unsubscribe := runtime.streamHub.Subscribe(job.TaskID)
	defer unsubscribe()

	runtime.handleTaskJob(context.Background(), job.ConversationKey, job)

	latest, ok := runtime.streamHub.Latest(job.TaskID)
	if !ok {
		t.Fatal("latest stream frame missing")
	}
	if latest.Status == string(daemonruntime.TaskDone) {
		t.Fatalf("latest status = %q, want persistence failure instead of final", latest.Status)
	}
	if latest.Status != string(daemonruntime.TaskFailed) || !latest.Done {
		t.Fatalf("latest stream frame = %#v, want terminal failure", latest)
	}
	for {
		select {
		case frame := <-frames:
			if frame.Status == string(daemonruntime.TaskDone) {
				t.Fatalf("published done frame before durable state: %#v", frame)
			}
		default:
			goto drained
		}
	}

drained:
	task, ok := store.Get(job.TaskID)
	if !ok || task == nil || task.Status != daemonruntime.TaskRunning {
		t.Fatalf("task after failed done write = %#v, ok=%v", task, ok)
	}
	if client.calls != 1 {
		t.Fatalf("LLM calls before failed done write = %d, want 1", client.calls)
	}
	var persistedStatuses []daemonruntime.TaskStatus
	if err := domainjournal.ReplayDir(journalDir, func(record domainjournal.Record) error {
		var payload struct {
			Task *daemonruntime.TaskInfo `json:"task"`
		}
		if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
			return err
		}
		if payload.Task != nil && payload.Task.ID == job.TaskID {
			persistedStatuses = append(persistedStatuses, payload.Task.Status)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	wantStatuses := []daemonruntime.TaskStatus{daemonruntime.TaskQueued, daemonruntime.TaskRunning}
	if !reflect.DeepEqual(persistedStatuses, wantStatuses) {
		t.Fatalf("persisted task statuses = %#v, want %#v", persistedStatuses, wantStatuses)
	}
}

func newConsoleLifecyclePersistenceTestRuntime(t *testing.T) (*consoleLocalRuntime, *daemonruntime.ConsoleFileStore, *consoleFinalLLMClient, consoleLocalTaskJob, string) {
	t.Helper()
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:        root,
		Persist:        true,
		JournalDir:     journalDir,
		RotateMaxBytes: 1,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Lifecycle")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	reader := viper.New()
	reader.Set("file_cache_dir", t.TempDir())
	reader.Set("file_state_dir", t.TempDir())
	logger := slog.Default()
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "test-model"},
	}
	registry := tools.NewRegistry()
	client := &consoleFinalLLMClient{}
	commonDeps := depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return logger, nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry { return registry },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
		RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
	}
	taskRuntime, err := taskruntime.Bootstrap(commonDeps, taskruntime.BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 1},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	generation := &consoleLocalRuntimeGeneration{
		reader:     reader,
		logger:     logger,
		commonDeps: commonDeps,
		bundle:     &consoleLocalRuntimeBundle{taskRuntime: taskRuntime},
	}
	generation.acquire()
	runtime := &consoleLocalRuntime{
		store:                 store,
		streamHub:             newConsoleStreamHub(),
		generation:            generation,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	job := consoleLocalTaskJob{
		TaskID:          "task_lifecycle_persistence",
		ConversationKey: buildConsoleConversationKey(topic.ID),
		TopicID:         topic.ID,
		Task:            "return a final answer",
		Model:           "test-model",
		Timeout:         time.Minute,
		CreatedAt:       time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		Trigger:         daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"},
		Generation:      generation,
	}
	if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        job.TaskID,
		Status:    daemonruntime.TaskQueued,
		Task:      job.Task,
		Model:     job.Model,
		Timeout:   job.Timeout.String(),
		CreatedAt: job.CreatedAt,
		TopicID:   job.TopicID,
	}, job.Trigger, ""); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}
	return runtime, store, client, job, journalDir
}
