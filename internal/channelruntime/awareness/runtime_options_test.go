package awareness

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
)

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	stateDir := t.TempDir()
	got := normalizeRunOptions(RunOptions{}, runtimepaths.Paths{
		HeartbeatPath: filepath.Join(stateDir, "HEARTBEAT.md"),
		CronPath:      filepath.Join(stateDir, "cron.yaml"),
		ContactsDir:   filepath.Join(stateDir, "contacts"),
	})
	if got.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want 30m", got.Interval)
	}
	if got.TaskTimeout != time.Hour {
		t.Fatalf("task timeout = %v, want 1h", got.TaskTimeout)
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

func TestNormalizeRunOptionsCarriesInspectFlags(t *testing.T) {
	got := normalizeRunOptions(RunOptions{
		InspectPrompt:  true,
		InspectRequest: true,
	}, runtimepaths.Paths{})
	if !got.InspectPrompt {
		t.Fatal("InspectPrompt = false, want true")
	}
	if !got.InspectRequest {
		t.Fatal("InspectRequest = false, want true")
	}
}
