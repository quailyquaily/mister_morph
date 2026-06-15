package memory

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRecovery_ReplayAfterRestartProjectsAppendedEvents(t *testing.T) {
	root := t.TempDir()

	// Process A: append journal only, no projection.
	jA := newTestDomainJournal(t, root)
	jA.now = func() time.Time { return mustTimeRFC3339(t, "2026-03-01T12:00:00Z") }
	ev := MemoryEvent{
		SchemaVersion: CurrentMemoryEventSchemaVersion,
		EventID:       "evt_recovery_1",
		TaskRunID:     "run_recovery_1",
		TSUTC:         "2026-03-01T12:00:30Z",
		SessionID:     "tg:-9001",
		SubjectID:     "tg:-9001",
		Channel:       "telegram",
		TaskText:      "remember this",
		FinalOutput:   "done",
	}
	if err := jA.Append(ev); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	// Process B: restart and replay projection from checkpoint.
	mgrB := NewManager(root, 7)
	jB := newTestDomainJournal(t, root)
	pB := NewProjector(mgrB, jB, ProjectorOptions{
		CheckpointBatch: 10,
		DraftResolver:   recoveryDraftResolver{},
	})

	cpBefore, okBefore, err := jB.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint(before) error = %v", err)
	}
	if okBefore {
		t.Fatalf("unexpected checkpoint before replay: %#v", cpBefore)
	}

	result, err := pB.ProjectOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProjectOnce() error = %v", err)
	}
	if result.Processed != 1 {
		t.Fatalf("ProjectOnce().Processed = %d, want 1", result.Processed)
	}
	if !result.Exhausted {
		t.Fatalf("ProjectOnce().Exhausted = false, want true")
	}

	day := mustTimeRFC3339(t, "2026-03-01T00:00:00Z")
	_, shortContent, shortOK, err := mgrB.LoadShortTerm(day, "tg:-9001")
	if err != nil {
		t.Fatalf("LoadShortTerm() error = %v", err)
	}
	if !shortOK {
		t.Fatalf("LoadShortTerm() ok = false, want true")
	}
	if len(shortContent.SummaryItems) != 1 || shortContent.SummaryItems[0].Content != "Recovery summary item" {
		t.Fatalf("short-term content = %#v, want one recovery summary item", shortContent.SummaryItems)
	}

	longPath, _ := mgrB.LongTermPath("ignored")
	longRaw, err := os.ReadFile(longPath)
	if err != nil {
		t.Fatalf("ReadFile(long-term) error = %v", err)
	}
	_, longBody, _ := ParseFrontmatter(string(longRaw))
	longContent := ParseLongTermContent(longBody)
	if len(longContent.Goals) != 1 || longContent.Goals[0].Content != "Recovery goal" {
		t.Fatalf("long-term goals = %#v, want one recovery goal", longContent.Goals)
	}

	cpAfter, okAfter, err := jB.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint(after) error = %v", err)
	}
	if !okAfter {
		t.Fatalf("LoadCheckpoint(after) ok = false, want true")
	}
	if cpAfter.Line != 1 {
		t.Fatalf("checkpoint line after replay = %d, want 1", cpAfter.Line)
	}
}

type recoveryDraftResolver struct{}

func (recoveryDraftResolver) ResolveDraft(ctx context.Context, event MemoryEvent, existing ShortTermContent) (SessionDraft, error) {
	return SessionDraft{
		SummaryItems: []string{"Recovery summary item"},
		Promote: PromoteDraft{
			GoalsProjects: []string{"Recovery goal"},
		},
	}, nil
}
