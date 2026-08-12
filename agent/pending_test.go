package agent

import (
	"reflect"
	"testing"
)

func TestPendingApprovalToolFromResumeState(t *testing.T) {
	want := map[string]any{
		"cmd":             "printf 'complete command'",
		"cwd":             "/srv/morph",
		"timeout_seconds": float64(180),
	}
	got, ok := PendingApprovalToolFromResumeState([]byte(`{
		"v": 1,
		"pending_tool": {
			"tool_call": {
				"tool_name": "bash",
				"tool_params": {
					"cmd": "printf 'complete command'",
					"cwd": "/srv/morph",
					"timeout_seconds": 180
				}
			}
		}
	}`))
	if !ok {
		t.Fatal("PendingApprovalToolFromResumeState() ok = false")
	}
	if got.Name != "bash" || !reflect.DeepEqual(got.Params, want) {
		t.Fatalf("PendingApprovalToolFromResumeState() = %#v, want bash %#v", got, want)
	}
}

func TestPendingApprovalToolFromResumeStateRejectsInvalidState(t *testing.T) {
	if _, ok := PendingApprovalToolFromResumeState([]byte(`{"v":1}`)); ok {
		t.Fatal("PendingApprovalToolFromResumeState() ok = true, want false")
	}
}
