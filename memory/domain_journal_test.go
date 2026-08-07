package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestDomainJournalAppendAndReplayMemoryEvents(t *testing.T) {
	root := t.TempDir()
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = rawJournal.Close() })
	j := NewDomainJournal(t.TempDir(), rawJournal)

	if err := j.Append(baseDomainMemoryEvent("evt_1", "run_1")); err != nil {
		t.Fatalf("Append(memory) error = %v", err)
	}
	if _, err := rawJournal.Append(domainjournal.Event{
		ID:            "evt_task",
		Time:          "2026-06-15T01:02:03Z",
		Domain:        "task",
		Type:          "task_upsert",
		SchemaVersion: 1,
		Payload:       []byte(`{}`),
	}); err != nil {
		t.Fatalf("Append(task) error = %v", err)
	}

	var got []string
	next, exhausted, err := j.ReplayFrom(JournalCursor{}, 10, func(rec JournalRecord) error {
		got = append(got, rec.Event.EventID)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
	if !exhausted {
		t.Fatalf("ReplayFrom() exhausted=false, want true")
	}
	if len(got) != 1 || got[0] != "evt_1" {
		t.Fatalf("ReplayFrom() ids = %#v, want [evt_1]", got)
	}
	if next.File != "events.000000000000000001.jsonl" || next.Line != 2 || next.Byte <= 0 {
		t.Fatalf("ReplayFrom() next = %#v, want cursor after skipped task event", next)
	}
}

func TestDomainJournalReplayFromAdvancesAcrossOnlyForeignEvents(t *testing.T) {
	root := t.TempDir()
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = rawJournal.Close() })
	j := NewDomainJournal(t.TempDir(), rawJournal)

	want, err := rawJournal.Append(domainjournal.Event{
		ID:            "evt_conversation",
		Time:          "2026-06-15T01:02:03Z",
		Domain:        "conversation",
		Type:          "untriggered_inbound",
		SchemaVersion: 1,
		Payload:       []byte(`{"message_id":"42"}`),
	})
	if err != nil {
		t.Fatalf("Append(conversation) error = %v", err)
	}
	want.Line = 1

	delivered := 0
	next, exhausted, err := j.ReplayFrom(JournalCursor{}, 10, func(JournalRecord) error {
		delivered++
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
	if !exhausted {
		t.Fatal("ReplayFrom() exhausted=false, want true")
	}
	if delivered != 0 {
		t.Fatalf("delivered = %d, want 0", delivered)
	}
	if next.File != want.File || next.Line != want.Line || next.Byte != want.Byte {
		t.Fatalf("next = %#v, want %#v", next, want)
	}
}

func TestDomainJournalReplayFromReturnsDeliveredCursorOnCallbackError(t *testing.T) {
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = rawJournal.Close() })
	j := NewDomainJournal(t.TempDir(), rawJournal)

	want, err := rawJournal.Append(domainMemoryDomainEvent(t, "evt_1"))
	if err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	want.Line = 1
	wantErr := errors.New("stop replay")

	next, exhausted, err := j.ReplayFrom(JournalCursor{}, 10, func(JournalRecord) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReplayFrom() error = %v, want %v", err, wantErr)
	}
	if exhausted {
		t.Fatal("ReplayFrom() exhausted=true, want false")
	}
	if next.File != want.File || next.Line != want.Line || next.Byte != want.Byte {
		t.Fatalf("next = %#v, want delivered cursor %#v", next, want)
	}
}

func TestDomainJournalReplayFromCursorSkipsEarlierSegmentContent(t *testing.T) {
	root := t.TempDir()
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	if err := rawJournal.Close(); err != nil {
		t.Fatalf("rawJournal.Close() error = %v", err)
	}
	firstName := "events.000000000000000001.jsonl"
	secondName := "events.000000000000000002.jsonl"
	badLine := []byte("{bad json}\n")
	if err := os.WriteFile(filepath.Join(root, firstName), badLine, 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, secondName), []byte(domainMemoryEventLine(t, "evt_2")+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}
	j := NewDomainJournal(t.TempDir(), rawJournal)

	var got []string
	next, exhausted, err := j.ReplayFrom(JournalCursor{File: firstName, Line: 1, Byte: int64(len(badLine))}, 10, func(rec JournalRecord) error {
		got = append(got, rec.Event.EventID)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom(cursor) error = %v", err)
	}
	if !exhausted {
		t.Fatalf("ReplayFrom(cursor) exhausted=false, want true")
	}
	if len(got) != 1 || got[0] != "evt_2" {
		t.Fatalf("ReplayFrom(cursor) ids = %#v, want [evt_2]", got)
	}
	if next.File != secondName || next.Line != 1 || next.Byte <= 0 {
		t.Fatalf("ReplayFrom(cursor) next = %#v, want second segment cursor", next)
	}
}

func TestDomainJournalReplayFromStopsAfterLimit(t *testing.T) {
	root := t.TempDir()
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	firstCursor, err := rawJournal.Append(domainMemoryDomainEvent(t, "evt_1"))
	if err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	secondCursor, err := rawJournal.Append(domainMemoryDomainEvent(t, "evt_2"))
	if err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}
	if err := rawJournal.Close(); err != nil {
		t.Fatalf("rawJournal.Close() error = %v", err)
	}
	badLine := []byte("{bad json after limit}\n")
	if err := appendFile(filepath.Join(root, secondCursor.File), badLine); err != nil {
		t.Fatalf("append bad line error = %v", err)
	}
	j := NewDomainJournal(t.TempDir(), rawJournal)

	var got []string
	next, exhausted, err := j.ReplayFrom(JournalCursor{}, 1, func(rec JournalRecord) error {
		got = append(got, rec.Event.EventID)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom(limit=1) error = %v", err)
	}
	if exhausted {
		t.Fatal("ReplayFrom(limit=1) exhausted=true, want false")
	}
	if len(got) != 1 || got[0] != "evt_1" {
		t.Fatalf("ReplayFrom(limit=1) ids = %#v, want [evt_1]", got)
	}
	if next.File != firstCursor.File || next.Byte != firstCursor.Byte || next.Line <= 0 {
		t.Fatalf("next = %#v, want first event cursor with file %q and byte %d", next, firstCursor.File, firstCursor.Byte)
	}
}

func TestDomainJournalCheckpoint(t *testing.T) {
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = rawJournal.Close() })
	j := NewDomainJournal(t.TempDir(), rawJournal)

	cp := JournalCheckpoint{File: "events.000000000000000001.jsonl", Line: 3, Byte: 128}
	if err := j.SaveCheckpoint(cp); err != nil {
		t.Fatalf("SaveCheckpoint() error = %v", err)
	}
	got, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCheckpoint() ok=false, want true")
	}
	if got.File != cp.File || got.Line != cp.Line || got.Byte != cp.Byte || got.UpdatedAt == "" {
		t.Fatalf("LoadCheckpoint() = %#v, want file/line/byte with updated_at", got)
	}
}

func domainMemoryEventLine(t *testing.T, id string) string {
	t.Helper()
	raw, err := json.Marshal(domainMemoryDomainEvent(t, id))
	if err != nil {
		t.Fatalf("Marshal(domain event) error = %v", err)
	}
	return string(raw)
}

func domainMemoryDomainEvent(t *testing.T, id string) domainjournal.Event {
	t.Helper()
	event := baseDomainMemoryEvent(id, "run_"+id)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal(memory event) error = %v", err)
	}
	return domainjournal.Event{
		ID:            id,
		Time:          event.TSUTC,
		Domain:        domainJournalMemoryDomain,
		Type:          domainJournalMemoryRecord,
		SchemaVersion: 1,
		Payload:       payload,
	}
}

func appendFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(data)
	return err
}

func baseDomainMemoryEvent(id, runID string) MemoryEvent {
	return MemoryEvent{
		SchemaVersion: CurrentMemoryEventSchemaVersion,
		EventID:       id,
		TaskRunID:     runID,
		TSUTC:         "2026-06-15T01:02:03Z",
		SessionID:     "session_1",
		SubjectID:     "subject_1",
		Channel:       "console",
		Participants: []MemoryParticipant{{
			ID:       0,
			Nickname: "MisterMorph",
		}},
		TaskText:    "hello",
		FinalOutput: "world",
	}
}
