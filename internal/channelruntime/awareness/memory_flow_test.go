package awareness

import (
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/memory"
)

func TestAwarenessTaskRunID(t *testing.T) {
	now := time.Date(2026, 2, 28, 1, 2, 3, 456000000, time.UTC)
	id := awarenessTaskRunID(awarenessutil.BehaviorPoke, now)
	if !strings.HasPrefix(id, "poke:20260228T010203") {
		t.Fatalf("unexpected task run id: %q", id)
	}
}

func TestAwarenessMemoryParticipants(t *testing.T) {
	ev := memory.MemoryEvent{
		SchemaVersion: memory.CurrentMemoryEventSchemaVersion,
		EventID:       "evt_01",
		TaskRunID:     "awareness:20260312T120651.547000000Z",
		TSUTC:         "2026-03-12T12:06:51Z",
		SessionID:     awarenessMemorySessionID,
		SubjectID:     awarenessMemorySubjectID,
		Channel:       "awareness",
		Participants:  awarenessMemoryParticipants(),
		TaskText:      "awareness task",
		FinalOutput:   "summary",
	}
	if err := ev.ValidateForAppend(); err != nil {
		t.Fatalf("ValidateForAppend() error = %v", err)
	}
}
