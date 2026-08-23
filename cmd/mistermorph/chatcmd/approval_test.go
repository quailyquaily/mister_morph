package chatcmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
)

type chatApprovalTestStore struct {
	record guard.ApprovalRecord
}

func (s *chatApprovalTestStore) Create(context.Context, guard.ApprovalRecord) (string, error) {
	return "", errors.New("not implemented")
}

func (s *chatApprovalTestStore) Get(context.Context, string) (guard.ApprovalRecord, bool, error) {
	return s.record, true, nil
}

func (s *chatApprovalTestStore) Resolve(_ context.Context, _ string, status guard.ApprovalStatus, actor, comment string) error {
	if s.record.Status != guard.ApprovalPending {
		return guard.ErrApprovalNotPending
	}
	s.record.Status = status
	s.record.Actor = actor
	s.record.Comment = comment
	return nil
}

func (s *chatApprovalTestStore) ConsumeApproved(context.Context, string) (guard.ApprovalRecord, error) {
	return guard.ApprovalRecord{}, errors.New("not implemented")
}

func TestParseChatApprovalDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  chatApprovalDecision
	}{
		{input: "y", want: chatApprovalApprove},
		{input: "YES", want: chatApprovalApprove},
		{input: "/approve", want: chatApprovalApprove},
		{input: "n", want: chatApprovalDeny},
		{input: "No", want: chatApprovalDeny},
		{input: "/deny", want: chatApprovalDeny},
		{input: "/stop", want: chatApprovalDeny},
		{input: "run something else", want: chatApprovalUndecided},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			if got := parseChatApprovalDecision(test.input); got != test.want {
				t.Fatalf("parseChatApprovalDecision(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestChatApprovalDataIncludesExactToolDetails(t *testing.T) {
	t.Parallel()

	record := guard.ApprovalRecord{
		ID:       "apr_test",
		ToolName: "bash",
		Reasons:  []string{"shell command execution", "workspace write"},
		ResumeState: []byte(`{
			"v": 1,
			"pending_tool": {
				"tool_call": {
					"tool_name": "bash",
					"tool_params": {
						"cmd": "echo approval details",
						"timeout": 30
					}
				}
			}
		}`),
	}

	data := chatApprovalData(record)
	parts := append([]string{data.tool, data.action}, data.reasons...)
	for _, param := range data.params {
		parts = append(parts, param.name, param.value)
	}
	got := strings.Join(parts, "\n")
	for _, want := range []string{
		"bash",
		"$ echo approval details",
		"cmd",
		"echo approval details",
		"timeout",
		"30",
		"shell command execution",
		"workspace write",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("chatApprovalData() = %q, want substring %q", got, want)
		}
	}
}

func TestChatApprovalDataFallsBackToRedactedSummary(t *testing.T) {
	t.Parallel()

	got := chatApprovalData(guard.ApprovalRecord{
		ToolName:              "powershell",
		ActionSummaryRedacted: "PowerShell command",
	})
	if got.action != "PowerShell command" {
		t.Fatalf("chatApprovalData().action = %q, want redacted summary", got.action)
	}
}

func TestResolveChatApprovalInputCommitsDecision(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input      string
		want       chatApprovalDecision
		wantStatus guard.ApprovalStatus
	}{
		{input: "/approve", want: chatApprovalApprove, wantStatus: guard.ApprovalApproved},
		{input: "/deny", want: chatApprovalDeny, wantStatus: guard.ApprovalDenied},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			store := &chatApprovalTestStore{record: guard.ApprovalRecord{
				ID:        "apr_test",
				Status:    guard.ApprovalPending,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			}}
			approvalGuard := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, store)

			decision, state, err := resolveChatApprovalInput(context.Background(), approvalGuard, store.record.ID, test.input, "chat:test", time.Now().UTC())
			if err != nil {
				t.Fatalf("resolveChatApprovalInput() error = %v", err)
			}
			if decision != test.want || state != runtimecore.ApprovalCommitCommitted {
				t.Fatalf("decision/state = %v/%v, want %v/committed", decision, state, test.want)
			}
			if store.record.Status != test.wantStatus || store.record.Actor != "chat:test" {
				t.Fatalf("record = %+v, want status %q actor chat:test", store.record, test.wantStatus)
			}
		})
	}
}

func TestResolveChatApprovalInputExpiresStaleApproval(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := &chatApprovalTestStore{record: guard.ApprovalRecord{
		ID:        "apr_expired",
		Status:    guard.ApprovalPending,
		ExpiresAt: now.Add(-time.Second),
	}}
	approvalGuard := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, store)

	decision, state, err := resolveChatApprovalInput(context.Background(), approvalGuard, store.record.ID, "/approve", "chat:test", now)
	if err != nil {
		t.Fatalf("resolveChatApprovalInput() error = %v", err)
	}
	if decision != chatApprovalExpired || state != runtimecore.ApprovalCommitCommitted {
		t.Fatalf("decision/state = %v/%v, want expired/committed", decision, state)
	}
	if store.record.Status != guard.ApprovalExpired {
		t.Fatalf("record status = %q, want expired", store.record.Status)
	}
}
