package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestListPendingApprovalsIncludesCompleteBashParams(t *testing.T) {
	root := t.TempDir()
	store := daemonruntime.NewMemoryStore(10)
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "guard_approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	command := strings.Repeat("printf 'approval details'; ", 24)
	params := map[string]any{
		"cmd":             command,
		"cwd":             "/srv/morph",
		"timeout_seconds": float64(180),
		"run_in_subtask":  true,
	}
	resumeState, err := json.Marshal(map[string]any{
		"v": 1,
		"pending_tool": map[string]any{
			"tool_call": map[string]any{
				"tool_name":   "bash",
				"tool_params": params,
			},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	now := time.Now().UTC()
	approvalID, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		RunID:                 "run_1",
		Status:                guard.ApprovalPending,
		CreatedAt:             now,
		ExpiresAt:             now.Add(time.Hour),
		Decision:              guard.DecisionRequireApproval,
		ToolName:              "bash",
		ActionSummaryRedacted: "ToolCallPre tool=bash",
		ActionHash:            "hash",
		ActionType:            guard.ActionToolCallPre,
		ResumeState:           resumeState,
		Reasons:               []string{"bash_requires_approval"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_1",
		Status:            daemonruntime.TaskPending,
		CreatedAt:         now,
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
	if len(resp.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(resp.Items))
	}
	if !reflect.DeepEqual(resp.Items[0].ToolParams, params) {
		t.Fatalf("ToolParams = %#v, want %#v", resp.Items[0].ToolParams, params)
	}
}

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
