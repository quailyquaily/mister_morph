package slack

import "testing"

func TestSlackApprovalButtonValueRoundTrip(t *testing.T) {
	data := slackApprovalButtonValue("apr_123", true)
	id, approved, ok := parseSlackApprovalButtonValue(data)
	if !ok || !approved || id != "apr_123" {
		t.Fatalf("parse approve value = id %q approved %v ok %v, want apr_123 true true", id, approved, ok)
	}

	data = slackApprovalButtonValue("apr_123", false)
	id, approved, ok = parseSlackApprovalButtonValue(data)
	if !ok || approved || id != "apr_123" {
		t.Fatalf("parse deny value = id %q approved %v ok %v, want apr_123 false true", id, approved, ok)
	}
}

func TestParseSlackApprovalButtonValueRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "x:a:apr_1", "ap:x:apr_1", "ap:a:", "ap:a"} {
		if _, _, ok := parseSlackApprovalButtonValue(raw); ok {
			t.Fatalf("parseSlackApprovalButtonValue(%q) ok=true, want false", raw)
		}
	}
}

func TestBuildSlackApprovalBlocks(t *testing.T) {
	blocks := buildSlackApprovalBlocks("approval required", "apr_123")
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	actions, ok := blocks[1]["elements"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions = %#v, want two elements", blocks[1]["elements"])
	}
	if actions[0]["action_id"] != slackApprovalActionApprove || actions[0]["value"] != "ap:a:apr_123" {
		t.Fatalf("approve action = %#v", actions[0])
	}
	if actions[1]["action_id"] != slackApprovalActionDeny || actions[1]["value"] != "ap:d:apr_123" {
		t.Fatalf("deny action = %#v", actions[1])
	}
}

func TestSlackApprovalResultText(t *testing.T) {
	if got := slackApprovalResultText(false); got != "Approval denied. Task canceled." {
		t.Fatalf("deny text = %q", got)
	}
	if got := slackApprovalResultText(true); got != "Approved. Resuming task." {
		t.Fatalf("approve text = %q", got)
	}
}
