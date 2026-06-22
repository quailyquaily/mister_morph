package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestTaskIDForPendingApprovalFindsOlderPage(t *testing.T) {
	store := daemonruntime.NewMemoryStore(300)
	base := time.Now().UTC()
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "target_task",
		Status:            daemonruntime.TaskPending,
		CreatedAt:         base,
		ApprovalRequestID: "apr_target",
	})
	for i := 0; i < 201; i++ {
		store.Upsert(daemonruntime.TaskInfo{
			ID:                fmt.Sprintf("newer_%03d", i),
			Status:            daemonruntime.TaskPending,
			CreatedAt:         base.Add(time.Duration(i+1) * time.Second),
			ApprovalRequestID: fmt.Sprintf("apr_newer_%03d", i),
		})
	}

	if got := TaskIDForPendingApproval(store, "apr_target"); got != "target_task" {
		t.Fatalf("TaskIDForPendingApproval() = %q, want target_task", got)
	}
}
