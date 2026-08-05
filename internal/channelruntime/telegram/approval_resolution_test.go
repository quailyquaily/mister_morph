package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type telegramApprovalFailingAudit struct {
	err error
}

func (s telegramApprovalFailingAudit) Emit(context.Context, guard.AuditEvent) error {
	return s.err
}

func (telegramApprovalFailingAudit) Close() error { return nil }

type telegramApprovalResolveErrorStore struct {
	guard.ApprovalStore
	err              error
	getErr           error
	resolveAttempted bool
}

type telegramApprovalUpdateErrorStore struct {
	daemonruntime.TaskView
	err       error
	failures  int
	callCount int
}

type telegramApprovalBlockingGetStore struct {
	guard.ApprovalStore
	started chan struct{}
	allow   chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (s *telegramApprovalBlockingGetStore) Get(ctx context.Context, id string) (guard.ApprovalRecord, bool, error) {
	if s.calls.Add(1) == 1 {
		s.once.Do(func() { close(s.started) })
		<-s.allow
	}
	return s.ApprovalStore.Get(ctx, id)
}

func (s *telegramApprovalUpdateErrorStore) Update(id string, update func(*daemonruntime.TaskInfo)) error {
	s.callCount++
	if s.failures > 0 {
		s.failures--
		return s.err
	}
	return s.TaskView.Update(id, update)
}

func (s *telegramApprovalResolveErrorStore) Resolve(ctx context.Context, id string, status guard.ApprovalStatus, actor, comment string) error {
	if !s.resolveAttempted {
		s.resolveAttempted = true
		return s.err
	}
	return s.ApprovalStore.Resolve(ctx, id, status, actor, comment)
}

func (s *telegramApprovalResolveErrorStore) Get(ctx context.Context, id string) (guard.ApprovalRecord, bool, error) {
	if s.resolveAttempted && s.getErr != nil {
		err := s.getErr
		s.getErr = nil
		return guard.ApprovalRecord{}, false, err
	}
	return s.ApprovalStore.Get(ctx, id)
}

func TestTelegramApprovalAuditFailureTerminatesClaimedTask(t *testing.T) {
	auditErr := errors.New("approval audit unavailable")
	for _, tc := range []struct {
		name       string
		approved   bool
		wantStatus daemonruntime.TaskStatus
		wantRecord guard.ApprovalStatus
	}{
		{name: "approve fails closed", approved: true, wantStatus: daemonruntime.TaskFailed, wantRecord: guard.ApprovalApproved},
		{name: "deny cancels", approved: false, wantStatus: daemonruntime.TaskCanceled, wantRecord: guard.ApprovalDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, auditErr, nil, nil)

			gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, tc.approved, "telegram:test")
			if gotTaskID != taskID || resumed {
				t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
			}
			if err != auditErr {
				t.Fatalf("decision error = %v, want original audit error", err)
			}
			task, ok := state.taskStore.Get(taskID)
			if !ok || task.Status != tc.wantStatus || task.FinishedAt == nil {
				t.Fatalf("task = %+v, want terminal status %q", task, tc.wantStatus)
			}
			if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "" || task.Result != nil {
				t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
			}
			if _, ok := state.pendingApprovals.Get(approvalID); ok {
				t.Fatal("claimed approval handle remained registered")
			}
			rec, ok, getErr := state.guard.GetApproval(context.Background(), approvalID)
			if getErr != nil || !ok || rec.Status != tc.wantRecord {
				t.Fatalf("approval = %+v, ok=%v err=%v; want %q", rec, ok, getErr, tc.wantRecord)
			}
		})
	}
}

func TestTelegramConcurrentApprovalDecisionHasSingleOwner(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ownerApproved bool
		wantStatus    daemonruntime.TaskStatus
	}{
		{name: "approve owns claim", ownerApproved: true, wantStatus: daemonruntime.TaskQueued},
		{name: "deny owns claim", ownerApproved: false, wantStatus: daemonruntime.TaskCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, approvalID, taskID, gate := newTelegramConcurrentApprovalFixture(t)
			type decisionResult struct {
				taskID  string
				resumed bool
				err     error
			}
			ownerResult := make(chan decisionResult, 1)
			go func() {
				gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, tc.ownerApproved, "telegram:owner")
				ownerResult <- decisionResult{taskID: gotTaskID, resumed: resumed, err: err}
			}()
			select {
			case <-gate.started:
			case <-time.After(time.Second):
				t.Fatal("owner did not reach approval read")
			}

			gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, !tc.ownerApproved, "telegram:duplicate")
			if gotTaskID != taskID || resumed || !errors.Is(err, runtimecore.ErrPendingApprovalClaimInFlight) {
				t.Fatalf("duplicate decision = task %q resumed %v err %v; want in-flight", gotTaskID, resumed, err)
			}
			pending, _ := state.taskStore.Get(taskID)
			if pending.Status != daemonruntime.TaskPending || pending.ApprovalRequestID != approvalID {
				t.Fatalf("task during owner decision = %#v, want unchanged pending", pending)
			}

			close(gate.allow)
			select {
			case result := <-ownerResult:
				if result.taskID != taskID || result.err != nil || result.resumed != tc.ownerApproved {
					t.Fatalf("owner decision = %#v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("owner decision did not finish")
			}
			task, _ := state.taskStore.Get(taskID)
			if task.Status != tc.wantStatus {
				t.Fatalf("task status = %s, want %s", task.Status, tc.wantStatus)
			}
		})
	}
}

func TestTelegramApprovalPreCommitFailureRestoresHandle(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, resolveErr, nil)

	gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:test")
	if gotTaskID != taskID || resumed {
		t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
	}
	if !errors.Is(err, resolveErr) {
		t.Fatalf("decision error = %v, want resolve error", err)
	}
	job, ok := state.pendingApprovals.Get(approvalID)
	if !ok || job.TaskID != taskID {
		t.Fatalf("restored job = %+v, ok=%v", job, ok)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskPending || task.ApprovalRequestID != approvalID {
		t.Fatalf("task = %+v, want pending", task)
	}
	rec, ok, getErr := state.guard.GetApproval(context.Background(), approvalID)
	if getErr != nil || !ok || rec.Status != guard.ApprovalPending {
		t.Fatalf("approval = %+v, ok=%v err=%v; want pending", rec, ok, getErr)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _ = state.taskStore.Get(taskID)
		if task.Status == daemonruntime.TaskCanceled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task status after restored timer = %q, want canceled", task.Status)
	}
	if _, ok := state.pendingApprovals.Get(approvalID); ok {
		t.Fatal("restored approval handle remained after expiry")
	}
}

func TestTelegramApprovalUnknownOutcomeFailsClosed(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	getErr := errors.New("approval read unavailable")
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, resolveErr, getErr)

	gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:test")
	if gotTaskID != taskID || resumed {
		t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
	}
	if !errors.Is(err, resolveErr) || !errors.Is(err, getErr) {
		t.Fatalf("decision error = %v, want resolve and read errors", err)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := state.pendingApprovals.Get(approvalID); ok {
		t.Fatal("unknown approval handle remained registered")
	}
}

func TestTelegramApprovalQueuedUpdateFailureFailsClosed(t *testing.T) {
	updateErr := errors.New("queue task update unavailable")
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, nil, nil)
	store := &telegramApprovalUpdateErrorStore{TaskView: state.taskStore, err: updateErr, failures: 1}
	state.taskStore = store
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	t.Cleanup(cancelRunner)
	state.runner = runtimecore.NewConversationRunner(
		runnerCtx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, telegramJob) {},
		runtimecore.ConversationRunnerOptions[string, telegramJob]{},
	)

	gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:test")
	if gotTaskID != taskID || resumed {
		t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
	}
	if err != updateErr {
		t.Fatalf("decision error = %v, want original update error", err)
	}
	if store.callCount != 2 {
		t.Fatalf("task update calls = %d, want queued attempt plus fail-closed update", store.callCount)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := state.pendingApprovals.Get(approvalID); ok {
		t.Fatal("approval handle remained registered")
	}
	rec, ok, getErr := state.guard.GetApproval(context.Background(), approvalID)
	if getErr != nil || !ok || rec.Status != guard.ApprovalApproved {
		t.Fatalf("approval = %+v, ok=%v err=%v; want approved", rec, ok, getErr)
	}
}

func TestTelegramExpiredApprovalPreCommitFailureFailsClosed(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	state, approvalID, taskID := newTelegramApprovalResolutionFixtureAt(t, nil, resolveErr, nil, time.Now().UTC().Add(-time.Minute))

	gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:test")
	if gotTaskID != taskID || resumed {
		t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
	}
	if err != resolveErr {
		t.Fatalf("decision error = %v, want original resolve error", err)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := state.pendingApprovals.Get(approvalID); ok {
		t.Fatal("expired approval handle remained registered")
	}
}

func TestTelegramExpiredApprovalIndeterminateClickFailsMatchingTask(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	getErr := errors.New("approval read unavailable")
	state, approvalID, taskID := newTelegramApprovalResolutionFixtureAt(t, nil, resolveErr, getErr, time.Now().UTC().Add(-time.Minute))

	gotTaskID, resumed, err := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:test")
	if gotTaskID != taskID || resumed {
		t.Fatalf("decision result = task %q resumed %v, want %q false", gotTaskID, resumed, taskID)
	}
	if !errors.Is(err, resolveErr) || !errors.Is(err, getErr) {
		t.Fatalf("decision error = %v, want resolve and read errors", err)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func TestTelegramRegisterPendingApprovalReturnsClosedRegistryError(t *testing.T) {
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, nil, nil)
	state.pendingApprovals.Close()

	err := state.registerPendingApproval(approvalID, telegramJob{TaskID: taskID})
	if !errors.Is(err, runtimecore.ErrPendingApprovalRegistryUnavailable) {
		t.Fatalf("register error = %v, want registry unavailable", err)
	}
}

func TestTelegramReplacingPendingApprovalReleasesDisplacedGeneration(t *testing.T) {
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, nil, nil)
	var cleaned atomic.Int32
	manager := runtimecore.NewStaticRuntimeGenerationManager(runtimecore.ChannelRuntimeBundle{
		TaskRuntime: &taskruntime.Runtime{SharedGuard: state.guard},
		Cleanup:     func() { cleaned.Add(1) },
	}, nil)
	first, err := manager.Capture()
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.registerPendingApproval(approvalID, telegramJob{TaskID: taskID, Generation: first}); err != nil {
		t.Fatal(err)
	}
	if err := state.registerPendingApproval(approvalID, telegramJob{TaskID: taskID, Generation: second}); err != nil {
		t.Fatal(err)
	}

	manager.Close()
	second.Release()
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("generation cleanup count = %d, want 1", got)
	}
}

func TestTelegramCloseExpiresUnclaimedPendingApproval(t *testing.T) {
	state, approvalID, taskID := newTelegramApprovalResolutionFixture(t, nil, nil, nil)

	state.close()

	record, ok, err := state.guard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if record.Status != guard.ApprovalExpired || record.Actor != "system:telegram_shutdown" || record.Comment != "telegram runtime closed before approval decision" {
		t.Fatalf("approval after close = %+v, want shutdown expiry", record)
	}
	task, ok := state.taskStore.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.Error != telegramRuntimeClosedTaskError {
		t.Fatalf("task after close = %+v, want failed runtime-closed task", task)
	}
}

func TestTelegramCloseDoesNotOverwriteResolvedApproval(t *testing.T) {
	state, approvalID, _ := newTelegramApprovalResolutionFixture(t, nil, nil, nil)
	if err := state.guard.ResolveApproval(context.Background(), approvalID, guard.ApprovalDenied, "tester", "denied first"); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	state.close()

	record, ok, err := state.guard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if record.Status != guard.ApprovalDenied || record.Actor != "tester" || record.Comment != "denied first" {
		t.Fatalf("approval after close = %+v, want original denied record", record)
	}
}

func TestTelegramRegisterPendingApprovalRejectsUnavailableApprovalRecord(t *testing.T) {
	getErr := errors.New("approval read unavailable")
	root := t.TempDir()
	emptyStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	tests := []struct {
		name    string
		guard   *guard.Guard
		wantErr error
	}{
		{name: "guard unavailable"},
		{
			name: "approval read fails",
			guard: guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, &telegramApprovalResolveErrorStore{
				getErr:           getErr,
				resolveAttempted: true,
			}),
			wantErr: getErr,
		},
		{
			name:    "approval is missing",
			guard:   guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, emptyStore),
			wantErr: guard.ErrApprovalNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := &telegramRuntimeState{
				guard:            tc.guard,
				pendingApprovals: runtimecore.NewPendingApprovalRegistry[telegramJob](nil),
			}
			err := state.registerPendingApproval("apr_missing", telegramJob{TaskID: "task_missing"})
			if tc.wantErr == nil {
				if err == nil || !strings.Contains(err.Error(), "approvals are unavailable") {
					t.Fatalf("register error = %v, want approvals unavailable", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("register error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func newTelegramApprovalResolutionFixture(t *testing.T, auditErr, resolveErr, getErr error) (*telegramRuntimeState, string, string) {
	return newTelegramApprovalResolutionFixtureAt(t, auditErr, resolveErr, getErr, time.Time{})
}

func newTelegramApprovalResolutionFixtureAt(t *testing.T, auditErr, resolveErr, getErr error, expiresAt time.Time) (*telegramRuntimeState, string, string) {
	t.Helper()
	root := t.TempDir()
	baseStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(time.Hour)
		if resolveErr != nil {
			expiresAt = time.Now().UTC().Add(time.Second)
		}
	}
	approvalID, err := baseStore.Create(context.Background(), guard.ApprovalRecord{
		ID:                    "apr_telegram_resolution",
		RunID:                 "task_telegram_resolution",
		ExpiresAt:             expiresAt,
		ActionType:            guard.ActionToolCallPre,
		ToolName:              "bash",
		ActionHash:            "hash",
		RiskLevel:             guard.RiskHigh,
		Decision:              guard.DecisionRequireApproval,
		ActionSummaryRedacted: "ToolCallPre tool=bash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var approvalStore guard.ApprovalStore = baseStore
	if resolveErr != nil {
		approvalStore = &telegramApprovalResolveErrorStore{ApprovalStore: baseStore, err: resolveErr, getErr: getErr}
	}
	var audit guard.AuditSink
	if auditErr != nil {
		audit = telegramApprovalFailingAudit{err: auditErr}
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, audit, approvalStore)
	taskStore := daemonruntime.NewMemoryStore(8)
	pendingAt := time.Now().UTC()
	taskID := "task_telegram_resolution"
	if err := taskStore.Upsert(daemonruntime.TaskInfo{
		ID:                taskID,
		Status:            daemonruntime.TaskPending,
		CreatedAt:         pendingAt,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalID,
		Result:            map[string]any{"status": "pending"},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	registry := newTelegramPendingApprovalRegistry(g, taskStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { registry.Close() })
	handleExpiresAt := expiresAt
	if !handleExpiresAt.After(time.Now()) {
		handleExpiresAt = time.Now().UTC().Add(time.Hour)
	}
	registry.Register(approvalID, telegramJob{TaskID: taskID, ConversationKey: "telegram:test"}, handleExpiresAt)
	return &telegramRuntimeState{
		workersCtx:       context.Background(),
		taskStore:        taskStore,
		guard:            g,
		pendingApprovals: registry,
	}, approvalID, taskID
}

func newTelegramConcurrentApprovalFixture(t *testing.T) (*telegramRuntimeState, string, string, *telegramApprovalBlockingGetStore) {
	t.Helper()
	root := t.TempDir()
	baseStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	approvalID := "apr_telegram_concurrent"
	if _, err := baseStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         approvalID,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gate := &telegramApprovalBlockingGetStore{
		ApprovalStore: baseStore,
		started:       make(chan struct{}),
		allow:         make(chan struct{}),
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, gate)
	taskStore := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	taskID := "task_telegram_concurrent"
	if err := taskStore.Upsert(daemonruntime.TaskInfo{ID: taskID, Status: daemonruntime.TaskPending, PendingAt: &pendingAt, ApprovalRequestID: approvalID}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	registry := newTelegramPendingApprovalRegistry(g, taskStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, _, err := registry.Register(approvalID, telegramJob{TaskID: taskID, ConversationKey: "telegram:concurrent"}, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	state := &telegramRuntimeState{
		workersCtx:       workersCtx,
		taskStore:        taskStore,
		guard:            g,
		pendingApprovals: registry,
	}
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](
		workersCtx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, telegramJob) {},
		runtimecore.ConversationRunnerOptions[string, telegramJob]{},
	)
	t.Cleanup(func() {
		cancelWorkers()
		state.runner.WaitClosed()
		registry.Close()
	})
	return state, approvalID, taskID, gate
}
