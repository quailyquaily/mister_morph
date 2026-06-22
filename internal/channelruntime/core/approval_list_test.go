package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestListPendingApprovalsFiltersExpiredRecords(t *testing.T) {
	root := t.TempDir()
	store := daemonruntime.NewMemoryStore(10)
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "guard_approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	now := time.Now().UTC()
	approvalID, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		RunID:                 "run_1",
		Status:                guard.ApprovalPending,
		CreatedAt:             now.Add(-2 * time.Hour),
		ExpiresAt:             now.Add(-time.Hour),
		Decision:              guard.DecisionRequireApproval,
		ToolName:              "bash",
		ActionSummaryRedacted: "bash command",
		ActionHash:            "hash",
		ActionType:            guard.ActionToolCallPre,
		ResumeState:           []byte(`{}`),
		Reasons:               []string{"bash_requires_approval"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	pendingAt := now.Add(-90 * time.Minute)
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_1",
		Status:            daemonruntime.TaskPending,
		Task:              "run bash",
		CreatedAt:         now.Add(-2 * time.Hour),
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalID,
	})
	g := guard.New(guard.Config{
		Enabled: true,
		Approvals: guard.ApprovalsConfig{
			Enabled: true,
		},
	}, nil, approvalStore)
	resp, err := ListPendingApprovals(context.Background(), store, g, daemonruntime.ApprovalListRequest{Status: "pending"}, "test")
	if err != nil {
		t.Fatalf("ListPendingApprovals() error = %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items len = %d, want 0", len(resp.Items))
	}
}
