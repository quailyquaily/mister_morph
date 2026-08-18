package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/entryutil"
)

func TestProjectorProjectOnce_ProjectsGroupedTargetsAndLongTerm(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	j.now = func() time.Time { return mustTimeRFC3339(t, "2026-02-28T06:00:00Z") }

	day := mustTimeRFC3339(t, "2026-02-28T06:00:00Z")
	e1 := baseProjectorEvent("evt_1", "run_1", "2026-02-28T06:01:00Z", "tg-1001", []string{"first item"})
	e2 := baseProjectorEvent("evt_2", "run_1", "2026-02-28T06:02:00Z", "tg-1001", []string{"second item"})
	e3 := baseProjectorEvent("evt_3", "run_2", "2026-02-28T06:03:00Z", "tg-1002", []string{"another room item"})

	if err := j.Append(e1); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	if err := j.Append(e2); err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}
	if err := j.Append(e3); err != nil {
		t.Fatalf("Append(evt_3) error = %v", err)
	}

	p := NewProjector(mgr, j, ProjectorOptions{
		CheckpointBatch: 2,
		DraftResolver: fakeDraftResolver{
			byEventID: map[string]SessionDraft{
				"evt_1": {SummaryItems: []string{"first item"}},
				"evt_2": {
					SummaryItems: []string{"second item"},
					Promote:      PromoteDraft{GoalsProjects: []string{"Keep archive synced"}},
				},
				"evt_3": {SummaryItems: []string{"another room item"}},
			},
		},
	})

	got, err := p.ProjectOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProjectOnce() error = %v", err)
	}
	if got.Processed != 3 {
		t.Fatalf("ProjectOnce().Processed = %d, want 3", got.Processed)
	}
	if !got.Exhausted {
		t.Fatalf("ProjectOnce().Exhausted = false, want true")
	}

	_, content1001, ok1001, err := mgr.LoadShortTerm(day, "tg-1001")
	if err != nil {
		t.Fatalf("LoadShortTerm(tg-1001) error = %v", err)
	}
	if !ok1001 {
		t.Fatalf("LoadShortTerm(tg-1001) ok = false, want true")
	}
	if len(content1001.SummaryItems) != 2 {
		t.Fatalf("LoadShortTerm(tg-1001) summary count = %d, want 2", len(content1001.SummaryItems))
	}
	if content1001.SummaryItems[0].Content != "second item" || content1001.SummaryItems[1].Content != "first item" {
		t.Fatalf("LoadShortTerm(tg-1001) summary order = %#v, want [second item, first item]", content1001.SummaryItems)
	}

	_, content1002, ok1002, err := mgr.LoadShortTerm(day, "tg-1002")
	if err != nil {
		t.Fatalf("LoadShortTerm(tg-1002) error = %v", err)
	}
	if !ok1002 {
		t.Fatalf("LoadShortTerm(tg-1002) ok = false, want true")
	}
	if len(content1002.SummaryItems) != 1 || content1002.SummaryItems[0].Content != "another room item" {
		t.Fatalf("LoadShortTerm(tg-1002) content = %#v, want one item", content1002.SummaryItems)
	}

	longPath, _ := mgr.LongTermPath("ignored")
	data, err := os.ReadFile(longPath)
	if err != nil {
		t.Fatalf("ReadFile(long-term) error = %v", err)
	}
	_, body, _ := ParseFrontmatter(string(data))
	longContent := ParseLongTermContent(body)
	if len(longContent.Goals) != 1 || longContent.Goals[0].Content != "Keep archive synced" {
		t.Fatalf("long-term goals = %#v, want one promoted goal", longContent.Goals)
	}

	cp, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadCheckpoint() ok = false, want true")
	}
	if cp.Line != 3 {
		t.Fatalf("checkpoint line = %d, want 3", cp.Line)
	}
}

func TestProjectorCheckpointIncludesTrailingForeignEvents(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	event := baseProjectorEvent("evt_memory", "run_1", "2026-08-07T03:04:05Z", "tg-1001", []string{"remembered"})
	if err := j.Append(event); err != nil {
		t.Fatalf("Append(memory) error = %v", err)
	}
	foreignCursor, err := j.journal.Append(domainjournal.Event{
		ID:            "evt_conversation",
		Time:          "2026-08-07T03:04:06Z",
		Domain:        "conversation",
		Type:          "untriggered_inbound",
		SchemaVersion: 1,
		Payload:       []byte(`{"message_id":"42"}`),
	})
	if err != nil {
		t.Fatalf("Append(conversation) error = %v", err)
	}

	p := NewProjector(mgr, j, ProjectorOptions{DraftResolver: fakeDraftResolver{
		byEventID: map[string]SessionDraft{"evt_memory": {SummaryItems: []string{"remembered"}}},
	}})
	if _, err := p.ProjectOnce(context.Background(), 10); err != nil {
		t.Fatalf("ProjectOnce() error = %v", err)
	}
	cp, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCheckpoint() ok=false, want true")
	}
	if cp.File != foreignCursor.File || cp.Line != 2 || cp.Byte != foreignCursor.Byte {
		t.Fatalf("checkpoint = %#v, want trailing foreign cursor file=%q line=2 byte=%d", cp, foreignCursor.File, foreignCursor.Byte)
	}
}

func TestProjectorProjectOnce_RespectsReplayLimitAndAdvancesCheckpoint(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	j.now = func() time.Time { return mustTimeRFC3339(t, "2026-02-28T08:00:00Z") }

	events := []MemoryEvent{
		baseProjectorEvent("evt_1", "run_1", "2026-02-28T08:01:00Z", "tg-2001", []string{"one"}),
		baseProjectorEvent("evt_2", "run_1", "2026-02-28T08:02:00Z", "tg-2001", []string{"two"}),
		baseProjectorEvent("evt_3", "run_1", "2026-02-28T08:03:00Z", "tg-2001", []string{"three"}),
	}
	for i, ev := range events {
		if err := j.Append(ev); err != nil {
			t.Fatalf("Append(events[%d]) error = %v", i, err)
		}
	}

	p := NewProjector(mgr, j, ProjectorOptions{
		CheckpointBatch: 2,
		DraftResolver: fakeDraftResolver{
			byEventID: map[string]SessionDraft{
				"evt_1": {SummaryItems: []string{"one"}},
				"evt_2": {SummaryItems: []string{"two"}},
				"evt_3": {SummaryItems: []string{"three"}},
			},
		},
	})

	first, err := p.ProjectOnce(context.Background(), 2)
	if err != nil {
		t.Fatalf("ProjectOnce(limit=2 first) error = %v", err)
	}
	if first.Processed != 2 {
		t.Fatalf("first.Processed = %d, want 2", first.Processed)
	}
	if first.Exhausted {
		t.Fatalf("first.Exhausted = true, want false")
	}
	cp1, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint(first) error = %v", err)
	}
	if !ok || cp1.Line != 2 {
		t.Fatalf("checkpoint after first = %#v, want line=2", cp1)
	}

	second, err := p.ProjectOnce(context.Background(), 2)
	if err != nil {
		t.Fatalf("ProjectOnce(limit=2 second) error = %v", err)
	}
	if second.Processed != 1 {
		t.Fatalf("second.Processed = %d, want 1", second.Processed)
	}
	if !second.Exhausted {
		t.Fatalf("second.Exhausted = false, want true")
	}
	cp2, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint(second) error = %v", err)
	}
	if !ok || cp2.Line != 3 {
		t.Fatalf("checkpoint after second = %#v, want line=3", cp2)
	}
}

func TestProjectorProjectOnce_ProjectionErrorStillAdvancesCheckpoint(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	j.now = func() time.Time { return mustTimeRFC3339(t, "2026-02-28T09:00:00Z") }

	day := mustTimeRFC3339(t, "2026-02-28T09:00:00Z")
	_, err := mgr.WriteShortTerm(day, ShortTermContent{
		SummaryItems: []SummaryItem{
			{Created: "2026-02-28 08:59", Content: "existing item"},
		},
	}, WriteMeta{SessionID: "tg-3001"})
	if err != nil {
		t.Fatalf("WriteShortTerm(existing) error = %v", err)
	}

	e1 := baseProjectorEvent("evt_1", "run_1", "2026-02-28T09:01:00Z", "tg-3001", []string{"alpha"})
	e2 := baseProjectorEvent("evt_2", "run_1", "2026-02-28T09:02:00Z", "tg-3001", []string{"beta"})
	if err := j.Append(e1); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	if err := j.Append(e2); err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}

	p := NewProjector(mgr, j, ProjectorOptions{
		CheckpointBatch:  10,
		SemanticResolver: alwaysFailResolver{},
		DraftResolver: fakeDraftResolver{
			byEventID: map[string]SessionDraft{
				"evt_1": {SummaryItems: []string{"alpha"}},
				"evt_2": {SummaryItems: []string{"beta"}},
			},
		},
	})

	_, err = p.ProjectOnce(context.Background(), 10)
	if err == nil {
		t.Fatalf("ProjectOnce() error = nil, want semantic dedupe error")
	}
	if !strings.Contains(err.Error(), "semantic dedupe failed") {
		t.Fatalf("ProjectOnce() error = %v, want semantic dedupe failed", err)
	}

	cp, ok, cpErr := j.LoadCheckpoint()
	if cpErr != nil {
		t.Fatalf("LoadCheckpoint() error = %v", cpErr)
	}
	if !ok {
		t.Fatalf("LoadCheckpoint() ok = false, want true")
	}
	if cp.Line != 2 {
		t.Fatalf("checkpoint line = %d, want 2", cp.Line)
	}

	errorLog := filepath.Join(root, "log", "projector-error.jsonl")
	if _, statErr := os.Stat(errorLog); !os.IsNotExist(statErr) {
		t.Fatalf("projector error log should not exist, stat error = %v", statErr)
	}
}

func TestProjectorProjectOnce_RejectsEmptySubjectID(t *testing.T) {
	ev := baseProjectorEvent("evt_1", "run_1", "2026-02-28T10:01:00Z", "", []string{"alpha"})
	_, err := shortTermTargetFromEvent(ev)
	if err == nil {
		t.Fatalf("shortTermTargetFromEvent() error = nil, want subject_id is required")
	}
	if !strings.Contains(err.Error(), "subject_id is required") {
		t.Fatalf("shortTermTargetFromEvent() error = %v, want subject_id is required", err)
	}
}

func TestProjectorProjectOnce_IdempotentWhenReplayingSameEvents(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	j.now = func() time.Time { return mustTimeRFC3339(t, "2026-02-28T11:00:00Z") }

	e1 := baseProjectorEvent("evt_1", "run_1", "2026-02-28T11:01:00Z", "tg-5001", []string{"same item"})
	e2 := baseProjectorEvent("evt_2", "run_1", "2026-02-28T11:02:00Z", "tg-5001", []string{"another item"})
	if err := j.Append(e1); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	if err := j.Append(e2); err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}

	p := NewProjector(mgr, j, ProjectorOptions{CheckpointBatch: 10})
	p.opts.DraftResolver = fakeDraftResolver{
		byEventID: map[string]SessionDraft{
			"evt_1": {SummaryItems: []string{"same item"}},
			"evt_2": {SummaryItems: []string{"another item"}},
		},
	}
	if _, err := p.ProjectOnce(context.Background(), 10); err != nil {
		t.Fatalf("ProjectOnce(first) error = %v", err)
	}

	day := mustTimeRFC3339(t, "2026-02-28T00:00:00Z")
	_, firstContent, ok, err := mgr.LoadShortTerm(day, "tg-5001")
	if err != nil {
		t.Fatalf("LoadShortTerm(first) error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadShortTerm(first) ok = false, want true")
	}
	firstCount := len(firstContent.SummaryItems)
	if firstCount != 2 {
		t.Fatalf("summary count after first projection = %d, want 2", firstCount)
	}

	if err := j.SaveCheckpoint(JournalCheckpoint{}); err != nil {
		t.Fatalf("SaveCheckpoint(reset) error = %v", err)
	}
	if _, err := p.ProjectOnce(context.Background(), 10); err != nil {
		t.Fatalf("ProjectOnce(second replay) error = %v", err)
	}

	_, secondContent, ok, err := mgr.LoadShortTerm(day, "tg-5001")
	if err != nil {
		t.Fatalf("LoadShortTerm(second) error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadShortTerm(second) ok = false, want true")
	}
	secondCount := len(secondContent.SummaryItems)
	if secondCount != firstCount {
		t.Fatalf("summary count after replay = %d, want %d", secondCount, firstCount)
	}
}

func TestProjectorProjectOnce_UsesDraftResolverForRawEvents(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	j.now = func() time.Time { return mustTimeRFC3339(t, "2026-03-01T06:00:00Z") }

	ev := baseProjectorEvent("evt_raw_1", "run_raw_1", "2026-03-01T06:01:00Z", "tg-raw-1", nil)
	ev.SessionID = "tg-raw-1"
	ev.SourceHistory = []chathistory.ChatHistoryItem{{
		Channel: chathistory.ChannelTelegram,
		Kind:    chathistory.KindInboundUser,
		Text:    "hello",
	}}
	ev.SessionContext = SessionContext{
		ConversationID: "123",
	}
	if err := j.Append(ev); err != nil {
		t.Fatalf("Append(raw event) error = %v", err)
	}

	p := NewProjector(mgr, j, ProjectorOptions{
		CheckpointBatch: 10,
		DraftResolver: fakeDraftResolver{
			draft: SessionDraft{
				SummaryItems: []string{"resolved in projector"},
				Promote: PromoteDraft{
					GoalsProjects: []string{"projected goal"},
				},
			},
		},
	})

	if _, err := p.ProjectOnce(context.Background(), 10); err != nil {
		t.Fatalf("ProjectOnce() error = %v", err)
	}

	day := mustTimeRFC3339(t, "2026-03-01T00:00:00Z")
	_, content, ok, err := mgr.LoadShortTerm(day, "tg-raw-1")
	if err != nil {
		t.Fatalf("LoadShortTerm() error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadShortTerm() ok = false, want true")
	}
	if len(content.SummaryItems) != 1 || content.SummaryItems[0].Content != "resolved in projector" {
		t.Fatalf("short-term content = %#v, want resolved draft item", content.SummaryItems)
	}

	longPath, _ := mgr.LongTermPath("ignored")
	data, err := os.ReadFile(longPath)
	if err != nil {
		t.Fatalf("ReadFile(long-term) error = %v", err)
	}
	_, body, _ := ParseFrontmatter(string(data))
	longContent := ParseLongTermContent(body)
	if len(longContent.Goals) != 1 || longContent.Goals[0].Content != "projected goal" {
		t.Fatalf("long-term goals = %#v, want projected goal", longContent.Goals)
	}
}

func TestProjectShortTermBucketsRunsTargetsConcurrently(t *testing.T) {
	mgr := NewManager(t.TempDir(), 7)
	day := time.Now().UTC()
	dayDir, _ := mgr.ShortTermDayDir(day)
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(dayDir, name), nil, 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	p := NewProjector(mgr, nil, ProjectorOptions{DraftResolver: blockingDraftResolver{
		started: started,
		release: release,
	}})
	buckets := map[string]shortTermBucket{}
	order := []string{"one", "two"}
	for _, key := range order {
		buckets[key] = shortTermBucket{
			Target: shortTermTarget{DayUTC: day, SessionID: key, Key: key},
			Records: []JournalRecord{{Event: MemoryEvent{
				EventID:   key,
				TSUTC:     day.Format(time.RFC3339Nano),
				SessionID: key,
				SubjectID: key,
			}}},
		}
	}

	done := make(chan []error, 1)
	go func() {
		_, errs := p.projectShortTermBuckets(context.Background(), buckets, order)
		done <- errs
	}()

	for i := 0; i < len(order); i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			<-done
			t.Fatalf("started %d projections concurrently, want %d", i, len(order))
		}
	}
	close(release)
	if errs := <-done; len(errs) != 0 {
		t.Fatalf("projectShortTermBuckets() errors = %v", errs)
	}
}

type alwaysFailResolver struct{}

func (alwaysFailResolver) SelectDedupKeepIndices(ctx context.Context, items []entryutil.SemanticItem) ([]int, error) {
	return nil, fmt.Errorf("resolver failed")
}

type fakeDraftResolver struct {
	draft     SessionDraft
	byEventID map[string]SessionDraft
	err       error
}

type blockingDraftResolver struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (r blockingDraftResolver) ResolveDraft(context.Context, MemoryEvent, ShortTermContent) (SessionDraft, error) {
	r.started <- struct{}{}
	<-r.release
	return SessionDraft{SummaryItems: []string{"projected"}}, nil
}

func (f fakeDraftResolver) ResolveDraft(ctx context.Context, event MemoryEvent, existing ShortTermContent) (SessionDraft, error) {
	if f.err != nil {
		return SessionDraft{}, f.err
	}
	if draft, ok := f.byEventID[event.EventID]; ok {
		return draft, nil
	}
	return f.draft, nil
}

func baseProjectorEvent(eventID, runID, tsUTC, subjectID string, summaryItems []string) MemoryEvent {
	finalOutput := strings.Join(summaryItems, "\n")
	return MemoryEvent{
		SchemaVersion: CurrentMemoryEventSchemaVersion,
		EventID:       eventID,
		TaskRunID:     runID,
		TSUTC:         tsUTC,
		SessionID:     "tg:-1000",
		SubjectID:     subjectID,
		Channel:       "telegram",
		Participants:  nil,
		TaskText:      "task",
		FinalOutput:   finalOutput,
	}
}

func newTestDomainJournal(t *testing.T, root string) *DomainJournal {
	t.Helper()
	raw, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           filepath.Join(root, "journal"),
		SyncEachWrite: true,
	})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return NewDomainJournal(root, raw)
}

func mustTimeRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return out
}
