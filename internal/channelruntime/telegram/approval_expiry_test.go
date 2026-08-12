package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type telegramApprovalExpiryCloseRaceStore struct {
	guard.ApprovalStore
	getStarted chan struct{}
	unblockGet chan struct{}
	startOnce  sync.Once
}

type telegramApprovalExpiryRetryTaskStore struct {
	daemonruntime.TaskView
	err       error
	failures  atomic.Int32
	callCount atomic.Int32
}

func (s *telegramApprovalExpiryRetryTaskStore) Update(id string, update func(*daemonruntime.TaskInfo)) error {
	s.callCount.Add(1)
	for {
		remaining := s.failures.Load()
		if remaining <= 0 {
			return s.TaskView.Update(id, update)
		}
		if s.failures.CompareAndSwap(remaining, remaining-1) {
			return s.err
		}
	}
}

func (s *telegramApprovalExpiryCloseRaceStore) Resolve(context.Context, string, guard.ApprovalStatus, string, string) error {
	return errors.New("resolve failed")
}

func (s *telegramApprovalExpiryCloseRaceStore) Get(context.Context, string) (guard.ApprovalRecord, bool, error) {
	s.startOnce.Do(func() { close(s.getStarted) })
	<-s.unblockGet
	return guard.ApprovalRecord{}, false, errors.New("get failed")
}

func TestTelegramPendingApprovalExpiresTaskAndHandle(t *testing.T) {
	t.Parallel()

	g, store, approvalID, taskID, expiresAt := newTelegramApprovalExpiryFixture(t)
	registry := newTelegramPendingApprovalRegistry(g, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { registry.Close() })
	registry.Register(approvalID, telegramJob{TaskID: taskID}, expiresAt)

	waitForTelegramApprovalExpiry(t, func() bool {
		task, ok := store.Get(taskID)
		return ok && task.Status == daemonruntime.TaskCanceled
	})
	if _, ok := registry.Get(approvalID); ok {
		t.Fatal("expired Telegram approval handle remained registered")
	}
	rec, ok, err := g.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalExpired {
		t.Fatalf("approval status = %s, want expired", rec.Status)
	}
	task, _ := store.Get(taskID)
	if task.Error != core.ApprovalExpiredTaskError || task.PendingAt != nil || task.ApprovalRequestID != approvalID {
		t.Fatalf("expired task = %+v", task)
	}
}

func TestTelegramPendingApprovalRetriesFailedTaskFinalization(t *testing.T) {
	t.Parallel()

	g, store, approvalID, taskID, expiresAt := newTelegramApprovalExpiryFixture(t)
	retryStore := &telegramApprovalExpiryRetryTaskStore{TaskView: store, err: errors.New("task store unavailable")}
	retryStore.failures.Store(2)
	registry := newTelegramPendingApprovalRegistry(g, retryStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { registry.Close() })
	if _, _, err := registry.Register(approvalID, telegramJob{TaskID: taskID}, expiresAt); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	waitForTelegramApprovalExpiry(t, func() bool {
		task, ok := store.Get(taskID)
		return ok && task.Status == daemonruntime.TaskCanceled
	})
	if got := retryStore.callCount.Load(); got < 3 {
		t.Fatalf("task update calls = %d, want initial writes plus retry", got)
	}
	if _, ok := registry.Get(approvalID); ok {
		t.Fatal("approval handle remained registered after successful retry")
	}
}

func TestTelegramPendingApprovalExpiryFailsTaskWhenClosePreventsRestore(t *testing.T) {
	root := t.TempDir()
	baseStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	expiresAt := time.Now().UTC().Add(20 * time.Millisecond)
	approvalID, err := baseStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         "apr_telegram_close_race",
		ExpiresAt:  expiresAt,
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	raceStore := &telegramApprovalExpiryCloseRaceStore{
		ApprovalStore: baseStore,
		getStarted:    make(chan struct{}),
		unblockGet:    make(chan struct{}),
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, raceStore)
	taskStore := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	taskID := "task_telegram_close_race"
	taskStore.Upsert(daemonruntime.TaskInfo{ID: taskID, Status: daemonruntime.TaskPending, PendingAt: &pendingAt, ApprovalRequestID: approvalID, Result: map[string]any{"pending": true}})
	registry := newTelegramPendingApprovalRegistry(g, taskStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, _, err := registry.Register(approvalID, telegramJob{TaskID: taskID}, expiresAt); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	select {
	case <-raceStore.getStarted:
	case <-time.After(time.Second):
		t.Fatal("approval expiry did not reach the indeterminate read")
	}
	state := &telegramRuntimeState{taskStore: taskStore, guard: g, pendingApprovals: registry}
	gotTaskID, resumed, decisionErr := state.applyApprovalDecision(context.Background(), approvalID, true, "telegram:duplicate")
	if gotTaskID != taskID || resumed || !errors.Is(decisionErr, core.ErrPendingApprovalClaimInFlight) {
		t.Fatalf("decision during expiry = task %q resumed %v err %v; want in-flight", gotTaskID, resumed, decisionErr)
	}
	pending, _ := taskStore.Get(taskID)
	if pending.Status != daemonruntime.TaskPending || pending.ApprovalRequestID != approvalID {
		t.Fatalf("task during expiry ownership = %#v, want unchanged pending", pending)
	}
	closeResult := make(chan []core.PendingApprovalHandle[telegramJob], 1)
	go func() { closeResult <- registry.Close() }()
	for {
		_, _, closeErr := registry.Claim("apr_close_probe")
		if errors.Is(closeErr, core.ErrPendingApprovalRegistryUnavailable) {
			break
		}
		if closeErr != nil {
			t.Fatalf("probe Claim() error = %v", closeErr)
		}
	}
	close(raceStore.unblockGet)
	select {
	case handles := <-closeResult:
		if len(handles) != 0 {
			t.Fatalf("Close() handles = %#v, want callback to own the handle", handles)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not wait for the expiry callback")
	}

	waitForTelegramApprovalExpiry(t, func() bool {
		task, ok := taskStore.Get(taskID)
		return ok && task.Status == daemonruntime.TaskFailed
	})
	task, _ := taskStore.Get(taskID)
	if task.Error != core.ApprovalExpiryResolutionFailedTaskError || task.FinishedAt == nil {
		t.Fatalf("task after failed expiry restore = %#v, want failed terminal state", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields after failed expiry restore = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func newTelegramApprovalExpiryFixture(t *testing.T) (*guard.Guard, *daemonruntime.MemoryStore, string, string, time.Time) {
	t.Helper()
	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	expiresAt := time.Now().UTC().Add(30 * time.Millisecond)
	approvalID, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         "apr_telegram_expiry",
		ExpiresAt:  expiresAt,
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	store := daemonruntime.NewMemoryStore(16)
	pendingAt := time.Now().UTC()
	taskID := "task_telegram_expiry"
	store.Upsert(daemonruntime.TaskInfo{ID: taskID, Status: daemonruntime.TaskPending, PendingAt: &pendingAt, ApprovalRequestID: approvalID})
	return g, store, approvalID, taskID, expiresAt
}

func waitForTelegramApprovalExpiry(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Telegram approval expiry")
}
