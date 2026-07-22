package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildInjectedMetaMessageTruncatedKeepsObservationIDs(t *testing.T) {
	raw, ok := buildInjectedMetaMessage(map[string]any{
		"trigger":         "console",
		"model":           "gpt-5.5",
		"correlation_id":  "corr_1",
		"run_id":          "run_1",
		"task_id":         "task_1",
		"trace_id":        "trace_1",
		"topic_id":        "topic_1",
		"origin_event_id": "event_1",
		"large":           strings.Repeat("x", maxInjectedMetaBytes*2),
	})
	if !ok {
		t.Fatal("buildInjectedMetaMessage() ok = false, want true")
	}

	var payload struct {
		Meta map[string]any `json:"mister_morph_meta"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for key, want := range map[string]string{
		"trigger":         "console",
		"model":           "gpt-5.5",
		"correlation_id":  "corr_1",
		"run_id":          "run_1",
		"task_id":         "task_1",
		"trace_id":        "trace_1",
		"topic_id":        "topic_1",
		"origin_event_id": "event_1",
	} {
		got, _ := payload.Meta[key].(string)
		if got != want {
			t.Fatalf("meta[%s] = %q, want %q; meta=%#v", key, got, want, payload.Meta)
		}
	}
	if payload.Meta["truncated"] != true {
		t.Fatalf("meta[truncated] = %#v, want true", payload.Meta["truncated"])
	}
	if _, exists := payload.Meta["large"]; exists {
		t.Fatalf("large payload should be omitted from truncated meta")
	}
}
