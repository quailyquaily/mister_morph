package slack

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestMarkSlackMissingApprovalHandleApproveFailsPendingTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(300)
	pendingAt := time.Now().UTC()
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_1",
		Status:            daemonruntime.TaskPending,
		CreatedAt:         pendingAt,
		PendingAt:         &pendingAt,
		ApprovalRequestID: "apr_1",
		Result:            map[string]any{"status": "pending"},
	})
	addNewerSlackPendingApprovalTasks(store, pendingAt, 201)

	taskID, resumed, err := markSlackMissingApprovalHandle(store, "apr_1", true)
	if taskID != "task_1" || resumed {
		t.Fatalf("result = taskID %q resumed %v, want task_1 false", taskID, resumed)
	}
	if err == nil || !strings.Contains(err.Error(), "approval resume failed: pending approval handle is unavailable") {
		t.Fatalf("error = %v, want approval resume failed", err)
	}
	task, ok := store.Get("task_1")
	if !ok {
		t.Fatal("task_1 missing")
	}
	if task.Status != daemonruntime.TaskFailed {
		t.Fatalf("task status = %q, want failed", task.Status)
	}
	if !strings.Contains(task.Error, "pending approval handle is unavailable") {
		t.Fatalf("task error = %q, want missing handle error", task.Error)
	}
	if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "" || task.Result != nil {
		t.Fatalf("pending approval fields = pending_at %v approval %q result %#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func TestMarkSlackMissingApprovalHandleDenyCancelsPendingTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(300)
	pendingAt := time.Now().UTC()
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_1",
		Status:            daemonruntime.TaskPending,
		CreatedAt:         pendingAt,
		PendingAt:         &pendingAt,
		ApprovalRequestID: "apr_1",
		Result:            map[string]any{"status": "pending"},
	})
	addNewerSlackPendingApprovalTasks(store, pendingAt, 201)

	taskID, resumed, err := markSlackMissingApprovalHandle(store, "apr_1", false)
	if err != nil {
		t.Fatalf("markSlackMissingApprovalHandle() error = %v", err)
	}
	if taskID != "task_1" || resumed {
		t.Fatalf("result = taskID %q resumed %v, want task_1 false", taskID, resumed)
	}
	task, ok := store.Get("task_1")
	if !ok {
		t.Fatal("task_1 missing")
	}
	if task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task status = %q, want canceled", task.Status)
	}
	if task.Error != slackApprovalResultText(false) {
		t.Fatalf("task error = %q, want %q", task.Error, slackApprovalResultText(false))
	}
	if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != "apr_1" || task.Result != nil {
		t.Fatalf("approval fields = pending_at %v approval %q result %#v, want resolved approval reference", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func addNewerSlackPendingApprovalTasks(store *daemonruntime.MemoryStore, base time.Time, count int) {
	for i := 0; i < count; i++ {
		store.Upsert(daemonruntime.TaskInfo{
			ID:                fmt.Sprintf("newer_%03d", i),
			Status:            daemonruntime.TaskPending,
			CreatedAt:         base.Add(time.Duration(i+1) * time.Second),
			ApprovalRequestID: fmt.Sprintf("apr_newer_%03d", i),
		})
	}
}
