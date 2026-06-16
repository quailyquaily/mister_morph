package taskruntime

import "testing"

func TestApplyObservationMetaAddsTrimmedIDs(t *testing.T) {
	meta := ApplyObservationMeta(map[string]any{
		"trigger": "console",
	}, ObservationMetaIDs{
		TaskID:        " task_1 ",
		TraceID:       " trace_1 ",
		TopicID:       " topic_1 ",
		OriginEventID: " event_1 ",
	})

	for key, want := range map[string]string{
		"task_id":         "task_1",
		"trace_id":        "trace_1",
		"topic_id":        "topic_1",
		"origin_event_id": "event_1",
		"trigger":         "console",
	} {
		got, _ := meta[key].(string)
		if got != want {
			t.Fatalf("meta[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestApplyObservationMetaOverwritesGenericIDs(t *testing.T) {
	meta := ApplyObservationMeta(map[string]any{
		"task_id":  "old_task",
		"trace_id": "old_trace",
	}, ObservationMetaIDs{
		TaskID:  "task_2",
		TraceID: "trace_2",
	})

	if got, _ := meta["task_id"].(string); got != "task_2" {
		t.Fatalf("meta[task_id] = %q, want task_2", got)
	}
	if got, _ := meta["trace_id"].(string); got != "trace_2" {
		t.Fatalf("meta[trace_id] = %q, want trace_2", got)
	}
}
