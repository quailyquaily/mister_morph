package domainjournal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalAppendAndReplay(t *testing.T) {
	root := t.TempDir()
	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	if _, err := j.Append(baseEvent("evt_1", "memory", "record")); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	if _, err := j.Append(baseEvent("evt_2", "task", "task_upsert")); err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}

	var got []string
	err = j.Replay(func(rec Record) error {
		got = append(got, rec.Event.ID+"@"+rec.Cursor.File+":"+itoa64(rec.Cursor.Line))
		return nil
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	want := []string{"evt_1@events.000000000000000001.jsonl:1", "evt_2@events.000000000000000001.jsonl:2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Replay() = %#v, want %#v", got, want)
	}
}

func TestJournalReplaySegmentFilesInOrder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000001.jsonl"), []byte(mustJSONLine(t, baseEvent("evt_1", "task", "task_upsert"))), 0o600); err != nil {
		t.Fatalf("WriteFile(segment 1) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000002.jsonl"), []byte(mustJSONLine(t, baseEvent("evt_2", "task", "task_update"))), 0o600); err != nil {
		t.Fatalf("WriteFile(segment 2) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000003.jsonl"), []byte(mustJSONLine(t, baseEvent("evt_3", "task", "task_update"))), 0o600); err != nil {
		t.Fatalf("WriteFile(segment 3) error = %v", err)
	}

	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	var got []string
	err = j.Replay(func(rec Record) error {
		got = append(got, rec.Event.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	want := []string{"evt_1", "evt_2", "evt_3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Replay order = %#v, want %#v", got, want)
	}
}

func TestJournalAppendRotatesToStableSegmentNames(t *testing.T) {
	root := t.TempDir()
	j, err := New(JournalOptions{Dir: root, RotateMaxBytes: 180})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	first, err := j.Append(baseEvent("evt_1", "task", "task_upsert"))
	if err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	second, err := j.Append(baseEvent("evt_2", "task", "task_update"))
	if err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}
	if first.File == "" || second.File == "" {
		t.Fatalf("append cursors must include segment files: first=%#v second=%#v", first, second)
	}
	if first.File == second.File {
		t.Fatalf("cursor files = %q and %q, want rotation to a new stable segment", first.File, second.File)
	}
	if _, err := os.Stat(filepath.Join(root, first.File)); err != nil {
		t.Fatalf("first segment %q missing after rotation: %v", first.File, err)
	}
	if _, err := os.Stat(filepath.Join(root, second.File)); err != nil {
		t.Fatalf("second segment %q missing after rotation: %v", second.File, err)
	}
}

func TestJournalReplayFromCursorSkipsEarlierSegmentContent(t *testing.T) {
	root := t.TempDir()
	firstName := "events.000000000000000001.jsonl"
	secondName := "events.000000000000000002.jsonl"
	badLine := []byte("{bad json}\n")
	if err := os.WriteFile(filepath.Join(root, firstName), badLine, 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, secondName), []byte(mustJSONLine(t, baseEvent("evt_2", "task", "task_update"))), 0o600); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}

	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	var got []string
	err = j.ReplayFrom(Cursor{File: firstName, Byte: int64(len(badLine)), Line: 1}, func(rec Record) error {
		got = append(got, rec.Event.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
	if strings.Join(got, ",") != "evt_2" {
		t.Fatalf("ReplayFrom() ids = %#v, want [evt_2]", got)
	}
}

func TestJournalReplayReportsBadLine(t *testing.T) {
	root := t.TempDir()
	raw := mustJSONLine(t, baseEvent("evt_1", "memory", "record")) + "{bad json\n"
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000001.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	err = j.Replay(func(rec Record) error { return nil })
	if err == nil {
		t.Fatal("Replay() error = nil, want bad line error")
	}
	if !strings.Contains(err.Error(), "events.000000000000000001.jsonl:2") {
		t.Fatalf("Replay() error = %v, want file and line", err)
	}
}

func TestJournalReplaySkipsBlankLines(t *testing.T) {
	root := t.TempDir()
	raw := " \t\n" + mustJSONLine(t, baseEvent("evt_1", "memory", "record")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000001.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var ids []string
	if err := j.Replay(func(rec Record) error {
		ids = append(ids, rec.Event.ID)
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if strings.Join(ids, ",") != "evt_1" {
		t.Fatalf("Replay() ids = %#v, want evt_1", ids)
	}
}

func TestAppendRejectsInvalidEnvelope(t *testing.T) {
	j, err := New(JournalOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	_, err = j.Append(Event{
		ID:            "evt_missing_domain",
		Time:          "2026-06-15T01:02:03Z",
		Type:          "record",
		SchemaVersion: 1,
		Payload:       json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("Append(invalid) error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("Append(invalid) error = %v, want domain error", err)
	}
}

func TestReplayAllowsUnknownFutureFields(t *testing.T) {
	root := t.TempDir()
	raw := `{"id":"evt_1","time":"2026-06-15T01:02:03Z","domain":"task","type":"task_update","schema_version":1,"trace":{"trace_id":"tr_1","turn_id":"turn_1","history_item_id":"hist_1","parent_item_id":"hist_0","tool_call_id":"call_1","request_id":"req_1"},"payload":{},"future":"ok"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "events.000000000000000001.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	var got Event
	if err := j.Replay(func(rec Record) error {
		got = rec.Event
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if got.Trace.TraceID != "tr_1" {
		t.Fatalf("Replay() trace = %#v, want known trace fields and ignored future fields", got.Trace)
	}
}

func TestJournalIndexStoresOnlyRecordRefs(t *testing.T) {
	root := t.TempDir()
	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	if _, err := j.Append(baseEvent("evt_indexed", "task", "task_update")); err != nil {
		t.Fatalf("Append(indexed) error = %v", err)
	}

	if err := j.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("reopen New() error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	index, err := reopened.ReadIndex("task", "task_1", 10)
	if err != nil {
		t.Fatalf("ReadIndex() error = %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("len(index) = %d, want 1", len(index))
	}
	if index[0].Key != "task_1" || index[0].Ref.File == "" {
		t.Fatalf("index[0] = %#v, want key and record ref", index[0])
	}
	record, err := reopened.ReadAt(index[0].Ref)
	if err != nil {
		t.Fatalf("ReadAt(index ref) error = %v", err)
	}
	if record.Event.ID != "evt_indexed" {
		t.Fatalf("ReadAt(index ref) id = %q, want evt_indexed", record.Event.ID)
	}
}

func TestJournalAppendCommitsWhenDerivedIndexWriteFails(t *testing.T) {
	root := t.TempDir()
	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	// A regular file at index/ makes every derived index write fail while the
	// authoritative event segment remains writable.
	if err := os.WriteFile(filepath.Join(root, "index"), []byte("blocked\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}

	cursor, err := j.Append(baseEvent("evt_committed_without_index", "task", "task_update"))
	if err != nil {
		t.Fatalf("Append() error = %v, want committed event to succeed", err)
	}
	if cursor.File == "" || cursor.Byte == 0 {
		t.Fatalf("Append() cursor = %#v, want committed cursor", cursor)
	}
	if j.IndexError() == nil {
		t.Fatal("IndexError() = nil, want explicit degraded index state")
	}

	var replayed []string
	if err := j.Replay(func(rec Record) error {
		replayed = append(replayed, rec.Event.ID)
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if strings.Join(replayed, ",") != "evt_committed_without_index" {
		t.Fatalf("Replay() ids = %#v, want committed event", replayed)
	}

	if err := j.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("reopen New() error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopened.IndexError() == nil {
		t.Fatal("reopened IndexError() = nil, want persisted degraded state")
	}

	index, err := reopened.ReadIndex("task", "task_1", 10)
	if err != nil {
		t.Fatalf("ReadIndex() error = %v, want journal fallback", err)
	}
	if len(index) != 1 {
		t.Fatalf("len(ReadIndex()) = %d, want 1", len(index))
	}
	record, err := reopened.ReadAt(index[0].Ref)
	if err != nil {
		t.Fatalf("ReadAt(fallback ref) error = %v", err)
	}
	if record.Event.ID != "evt_committed_without_index" {
		t.Fatalf("ReadAt(fallback ref).Event.ID = %q", record.Event.ID)
	}
}

func TestJournalRebuildIndexesClearsDegradedStateAndRestoresHistory(t *testing.T) {
	root := t.TempDir()
	j, err := New(JournalOptions{Dir: root})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	indexPath := filepath.Join(root, "index")
	if err := os.WriteFile(indexPath, []byte("blocked\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}
	if _, err := j.Append(baseEvent("evt_missing_index", "task", "task_update")); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("Remove(index blocker) error = %v", err)
	}
	if _, err := j.Append(baseEvent("evt_index_after_failure", "task", "task_update")); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	if j.IndexError() == nil {
		t.Fatal("IndexError() = nil before rebuild")
	}

	if err := j.RebuildIndexes(); err != nil {
		t.Fatalf("RebuildIndexes() error = %v", err)
	}
	if j.IndexError() != nil {
		t.Fatalf("IndexError() after rebuild = %v", j.IndexError())
	}
	if _, err := os.Stat(filepath.Join(root, indexDirtyFile)); !os.IsNotExist(err) {
		t.Fatalf("dirty marker stat error = %v, want not exist", err)
	}

	index, err := j.ReadIndex("task", "task_1", 10)
	if err != nil {
		t.Fatalf("ReadIndex() error = %v", err)
	}
	if len(index) != 2 {
		t.Fatalf("len(ReadIndex()) = %d, want 2 rebuilt records", len(index))
	}
	first, err := j.ReadAt(index[0].Ref)
	if err != nil {
		t.Fatalf("ReadAt(first) error = %v", err)
	}
	second, err := j.ReadAt(index[1].Ref)
	if err != nil {
		t.Fatalf("ReadAt(second) error = %v", err)
	}
	if first.Event.ID != "evt_missing_index" || second.Event.ID != "evt_index_after_failure" {
		t.Fatalf("rebuilt event ids = %q, %q", first.Event.ID, second.Event.ID)
	}
}

func baseEvent(id, domain, typ string) Event {
	return Event{
		ID:            id,
		Time:          "2026-06-15T01:02:03Z",
		Domain:        domain,
		Type:          typ,
		SchemaVersion: 1,
		Trace: Trace{
			TraceID: "tr_" + id,
			Runtime: "console",
			Target:  "console",
			TopicID: "topic_1",
			TaskID:  "task_1",
		},
		Payload: json.RawMessage(`{"value":"` + id + `"}`),
	}
}

func mustJSONLine(t *testing.T, event Event) string {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(raw) + "\n"
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
