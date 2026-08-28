package mixin

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestParseMixinApprovalCommand(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		id       string
		approved bool
		ok       bool
	}{
		{name: "approve", text: "/approve apr_123", id: "apr_123", approved: true, ok: true},
		{name: "deny", text: "  /deny apr_456  ", id: "apr_456", approved: false, ok: true},
		{name: "missing id", text: "/approve"},
		{name: "extra argument", text: "/deny apr_456 now"},
		{name: "other command", text: "/stop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, approved, ok := parseMixinApprovalCommand(tt.text)
			if id != tt.id || approved != tt.approved || ok != tt.ok {
				t.Fatalf("parseMixinApprovalCommand(%q) = %q, %v, %v", tt.text, id, approved, ok)
			}
		})
	}
}

func TestMixinApprovalRequiresOriginalSenderAndResolvesOnlyOnce(t *testing.T) {
	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	approvalID, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         "apr_mixin_sender",
		RunID:      "task_mixin_sender",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		Status:     guard.ApprovalPending,
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	generations := runtimecore.NewStaticRuntimeGenerationManager(runtimecore.ChannelRuntimeBundle{
		TaskRuntime: &taskruntime.Runtime{SharedGuard: g},
	}, nil)
	defer generations.Close()
	lease, err := generations.Capture()
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	taskStore := daemonruntime.NewMemoryStore(8)
	pendingAt := time.Now().UTC()
	if err := taskStore.Upsert(daemonruntime.TaskInfo{
		ID: "task_mixin_sender", Status: daemonruntime.TaskPending, CreatedAt: pendingAt,
		PendingAt: &pendingAt, ApprovalRequestID: approvalID,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	manager := newMixinApprovalManager(nil, taskStore, generations, context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer manager.close()
	job := mixinJob{
		TaskID: "task_mixin_sender", ConversationKey: "mixin:conversation", ConversationID: "conversation",
		FromUserID: "user-a", Generation: lease,
	}
	if err := manager.register(approvalID, job); err != nil {
		t.Fatalf("register() error = %v", err)
	}
	listed, err := manager.listApprovals(context.Background(), daemonruntime.ApprovalListRequest{Status: "pending"})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ApprovalRequestID != approvalID {
		t.Fatalf("listApprovals() = %#v, error %v", listed, err)
	}
	detail, found, err := manager.getApproval(context.Background(), approvalID)
	if err != nil || !found || detail.TaskID != "task_mixin_sender" {
		t.Fatalf("getApproval() = %#v, %v, %v", detail, found, err)
	}
	_, _, err = manager.apply(context.Background(), approvalID, false, "mixin:user-b", func(job mixinJob) bool {
		return job.FromUserID == "user-b"
	})
	if err == nil {
		t.Fatal("apply() unauthorized error = nil")
	}
	if _, ok := manager.pending.Get(approvalID); !ok {
		t.Fatal("unauthorized decision consumed pending approval")
	}
	rec, found, err := g.GetApproval(context.Background(), approvalID)
	if err != nil || !found || rec.Status != guard.ApprovalPending {
		t.Fatalf("approval after unauthorized decision = %#v, %v, %v", rec, found, err)
	}
	decision, err := manager.deny(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console",
	})
	if err != nil || decision.Resumed || decision.TaskID != "task_mixin_sender" || decision.Status != string(guard.ApprovalDenied) {
		t.Fatalf("deny() = %#v, error %v", decision, err)
	}
	task, ok := taskStore.Get("task_mixin_sender")
	if !ok || task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task after deny = %#v, %v", task, ok)
	}
	if _, _, err := manager.apply(context.Background(), approvalID, false, "mixin:user-a", nil); err == nil {
		t.Fatal("duplicate apply() error = nil")
	}
}

func TestMixinApprovalRequestTextIncludesCompleteDetails(t *testing.T) {
	rec := guard.ApprovalRecord{
		ID:                    "apr_123",
		ToolName:              "bash",
		Reasons:               []string{"shell command execution", "workspace write"},
		ActionSummaryRedacted: "run a shell command",
		ResumeState:           []byte(`{"v":1,"pending_tool":{"tool_call":{"tool_name":"bash","tool_params":{"cmd":"printf 'first line\\nsecond line\\n'","cwd":"workspace_dir"}}}}`),
	}
	text := mixinApprovalRequestText(rec)
	for _, want := range []string{
		"Approval required",
		"Tool: bash",
		"shell command execution",
		"workspace write",
		"run a shell command",
		"cmd: printf 'first line\\nsecond line\\n'",
		"cwd: workspace_dir",
		"/approve apr_123",
		"/deny apr_123",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("approval text does not contain %q:\n%s", want, text)
		}
	}
}
