package awarenessutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestBuildHeartbeatTaskUsesOnlyHeartbeatChecklist(t *testing.T) {
	root := t.TempDir()
	checklistPath := filepath.Join(root, "HEARTBEAT.md")
	if err := os.WriteFile(checklistPath, []byte("## Check\n\n- Review current state.\n"), 0o600); err != nil {
		t.Fatalf("write heartbeat checklist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "TODO.md"), []byte("- [ ] should not leak\n"), 0o600); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "TODO.RECUR.md"), []byte("# TODO Recurring\n"), 0o600); err != nil {
		t.Fatalf("write TODO.RECUR.md: %v", err)
	}

	task, empty, err := BuildHeartbeatTask(checklistPath)
	if err != nil {
		t.Fatalf("BuildHeartbeatTask() error = %v", err)
	}
	if empty {
		t.Fatal("empty = true, want false")
	}
	if !strings.Contains(task, "Review current state.") {
		t.Fatalf("task missing heartbeat content: %q", task)
	}
	if strings.Contains(task, "TODO") || strings.Contains(task, "should not leak") {
		t.Fatalf("task should not include TODO content: %q", task)
	}
}

func TestBuildHeartbeatTaskSkipsEmptyChecklist(t *testing.T) {
	root := t.TempDir()
	checklistPath := filepath.Join(root, "HEARTBEAT.md")
	if err := os.WriteFile(checklistPath, []byte("# Heartbeat\n\n<!-- comment -->\n"), 0o600); err != nil {
		t.Fatalf("write heartbeat checklist: %v", err)
	}

	task, empty, err := BuildHeartbeatTask(checklistPath)
	if err != nil {
		t.Fatalf("BuildHeartbeatTask() error = %v", err)
	}
	if task != "" || !empty {
		t.Fatalf("task=%q empty=%v, want empty task", task, empty)
	}
}

func TestBuildPokeTaskRequiresTextBody(t *testing.T) {
	_, _, err := BuildPokeTask(daemonruntime.PokeInput{})
	if !errors.Is(err, ErrEmptyPokeBody) {
		t.Fatalf("BuildPokeTask(empty) error = %v, want ErrEmptyPokeBody", err)
	}

	task, empty, err := BuildPokeTask(daemonruntime.PokeInput{
		HasBody:     true,
		ContentType: "application/json",
		BodyText:    `{"event":"deploy"}`,
	})
	if err != nil {
		t.Fatalf("BuildPokeTask() error = %v", err)
	}
	if empty || task != `{"event":"deploy"}` {
		t.Fatalf("task=%q empty=%v, want body task", task, empty)
	}
}
