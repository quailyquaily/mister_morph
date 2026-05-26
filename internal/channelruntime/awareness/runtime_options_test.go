package awareness

import (
	"testing"
	"time"
)

func TestNormalizeRuntimeLoopOptionsDefaults(t *testing.T) {
	got := normalizeRuntimeLoopOptions(runtimeLoopOptions{})
	if got.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want 30m", got.Interval)
	}
	if got.TaskTimeout != 10*time.Minute {
		t.Fatalf("task timeout = %v, want 10m", got.TaskTimeout)
	}
	if got.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want 90s", got.RequestTimeout)
	}
	if got.Source != "awareness" {
		t.Fatalf("source = %q, want awareness", got.Source)
	}
	if got.ChecklistPath == "" {
		t.Fatalf("checklist path should not be empty")
	}
}

func TestResolveRuntimeLoopOptionsFromRunOptionsCarriesInspectFlags(t *testing.T) {
	got := resolveRuntimeLoopOptionsFromRunOptions(RunOptions{
		InspectPrompt:  true,
		InspectRequest: true,
	})
	if !got.InspectPrompt {
		t.Fatal("InspectPrompt = false, want true")
	}
	if !got.InspectRequest {
		t.Fatal("InspectRequest = false, want true")
	}
}
