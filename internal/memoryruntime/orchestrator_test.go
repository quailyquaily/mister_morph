package memoryruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/memory"
)

func TestOrchestratorRecordAndProjectOnce(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{
		CheckpointBatch: 10,
		DraftResolver: stubDraftResolver{
			draft: memory.SessionDraft{
				SummaryItems: []string{"one", "Two"},
			},
		},
	})

	now := mustRFC3339(t, "2026-03-01T09:10:00Z")
	o, err := New(mgr, j, p, OrchestratorOptions{
		Now:        func() time.Time { return now },
		NewEventID: func() string { return "evt_fixed" },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = o.Record(RecordRequest{
		TaskRunID: "run_123",
		SessionID: "tg--1001",
		SubjectID: "tg--1001",
		Channel:   "telegram",
		TaskText:  "hello",
		SourceHistory: []chathistory.ChatHistoryItem{{
			Channel: chathistory.ChannelTelegram,
			Kind:    chathistory.KindInboundUser,
			Text:    "ping",
		}},
		SessionContext: memory.SessionContext{
			ConversationID: "1001",
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	next, exhausted, err := j.ReplayFrom(memory.JournalCursor{}, 10, func(rec memory.JournalRecord) error {
		if rec.Event.EventID != "evt_fixed" {
			t.Fatalf("event_id = %q, want evt_fixed", rec.Event.EventID)
		}
		if rec.Event.TSUTC != now.UTC().Format(time.RFC3339) {
			t.Fatalf("ts_utc = %q, want %q", rec.Event.TSUTC, now.UTC().Format(time.RFC3339))
		}
		if len(rec.Event.SourceHistory) != 1 || rec.Event.SourceHistory[0].Text != "ping" {
			t.Fatalf("source_history = %#v, want one ping message", rec.Event.SourceHistory)
		}
		if rec.Event.SessionContext.ConversationID != "1001" {
			t.Fatalf("session_context.conversation_id = %q, want 1001", rec.Event.SessionContext.ConversationID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
	if !exhausted {
		t.Fatalf("ReplayFrom() exhausted = false, want true")
	}
	if next.Line != 1 {
		t.Fatalf("ReplayFrom() next line = %d, want 1", next.Line)
	}

	got, err := p.ProjectOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProjectOnce() error = %v", err)
	}
	if got.Processed != 1 || !got.Exhausted {
		t.Fatalf("ProjectOnce() result = %#v, want processed=1 exhausted=true", got)
	}

	day := mustRFC3339(t, "2026-03-01T00:00:00Z")
	_, content, ok, err := mgr.LoadShortTerm(day, "tg--1001")
	if err != nil {
		t.Fatalf("LoadShortTerm() error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadShortTerm() ok = false, want true")
	}
	if len(content.SummaryItems) != 2 {
		t.Fatalf("short-term item count = %d, want 2", len(content.SummaryItems))
	}
}

func TestPrepareInjectionWithAdapter(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	mgr.Now = func() time.Time { return mustRFC3339(t, "2026-03-02T12:00:00Z") }
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})
	o, err := New(mgr, j, p, OrchestratorOptions{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	day := mustRFC3339(t, "2026-03-02T00:00:00Z")
	_, err = mgr.WriteShortTerm(day, memory.ShortTermContent{
		SummaryItems: []memory.SummaryItem{
			{Created: "2026-03-02 10:00", Content: "Discussed roadmap"},
		},
	}, memory.WriteMeta{SessionID: "tg--2002"})
	if err != nil {
		t.Fatalf("WriteShortTerm() error = %v", err)
	}
	if _, err := mgr.UpdateLongTerm("ignored", memory.PromoteDraft{
		GoalsProjects: []string{"Ship phase D"},
	}); err != nil {
		t.Fatalf("UpdateLongTerm() error = %v", err)
	}

	inj, err := o.PrepareInjectionWithAdapter(fakeInjectionAdapter{
		subjectID: "tg--2002",
		reqCtx:    memory.ContextPrivate,
	}, 20)
	if err != nil {
		t.Fatalf("PrepareInjectionWithAdapter() error = %v", err)
	}
	if !strings.Contains(inj, "<Memory:LongTerm:Summary>") {
		t.Fatalf("PrepareInjectionWithAdapter() missing long-term block: %q", inj)
	}
	if !strings.Contains(inj, "<Memory:ShortTerm:Recent>") {
		t.Fatalf("PrepareInjectionWithAdapter() missing short-term block: %q", inj)
	}
}

func TestRecordWithAdapter(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})
	o, err := New(mgr, j, p, OrchestratorOptions{
		NewEventID: func() string { return "evt_adapter" },
		Now:        func() time.Time { return mustRFC3339(t, "2026-03-03T12:00:00Z") },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = o.RecordWithAdapter(fakeRecordAdapter{
		req: RecordRequest{
			TaskRunID: "run_adapter",
			SessionID: "heartbeat",
			SubjectID: "heartbeat",
			Channel:   "heartbeat",
			TaskText:  "tick",
		},
	})
	if err != nil {
		t.Fatalf("RecordWithAdapter() error = %v", err)
	}

	var gotID string
	_, _, err = j.ReplayFrom(memory.JournalCursor{}, 10, func(rec memory.JournalRecord) error {
		gotID = rec.Event.EventID
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
	if gotID != "evt_adapter" {
		t.Fatalf("event_id = %q, want evt_adapter", gotID)
	}
}

func TestRecordCapsJournalSourceHistoryToLatestThree(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})
	o, err := New(mgr, j, p, OrchestratorOptions{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = o.Record(RecordRequest{
		TaskRunID: "run_cap",
		SessionID: "tg--cap",
		SubjectID: "tg--cap",
		Channel:   "telegram",
		TaskText:  "cap",
		SourceHistory: []chathistory.ChatHistoryItem{
			{Kind: chathistory.KindInboundUser, Text: "one"},
			{Kind: chathistory.KindInboundUser, Text: "two"},
			{Kind: chathistory.KindInboundUser, Text: "three"},
			{Kind: chathistory.KindInboundUser, Text: "four"},
			{Kind: chathistory.KindOutboundAgent, Text: "five"},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	_, _, err = j.ReplayFrom(memory.JournalCursor{}, 10, func(rec memory.JournalRecord) error {
		if len(rec.Event.SourceHistory) != 3 {
			t.Fatalf("len(source_history) = %d, want 3", len(rec.Event.SourceHistory))
		}
		if rec.Event.SourceHistory[0].Text != "three" || rec.Event.SourceHistory[1].Text != "four" || rec.Event.SourceHistory[2].Text != "five" {
			t.Fatalf("source_history texts = %#v, want [three four five]", []string{
				rec.Event.SourceHistory[0].Text,
				rec.Event.SourceHistory[1].Text,
				rec.Event.SourceHistory[2].Text,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayFrom() error = %v", err)
	}
}

type fakeInjectionAdapter struct {
	subjectID string
	reqCtx    memory.RequestContext
}

func (f fakeInjectionAdapter) ResolveSubjectID() (string, error) {
	return f.subjectID, nil
}

func (f fakeInjectionAdapter) ResolveRequestContext() (memory.RequestContext, error) {
	return f.reqCtx, nil
}

type fakeRecordAdapter struct {
	req RecordRequest
	err error
}

func (f fakeRecordAdapter) BuildRecordRequest() (RecordRequest, error) {
	if f.err != nil {
		return RecordRequest{}, f.err
	}
	return f.req, nil
}

type stubDraftResolver struct {
	draft memory.SessionDraft
	err   error
}

func (s stubDraftResolver) ResolveDraft(ctx context.Context, event memory.MemoryEvent, existing memory.ShortTermContent) (memory.SessionDraft, error) {
	if s.err != nil {
		return memory.SessionDraft{}, s.err
	}
	return s.draft, nil
}

func newTestDomainJournal(t *testing.T, root string) *memory.DomainJournal {
	t.Helper()
	raw, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           filepath.Join(root, "journal"),
		SyncEachWrite: true,
	})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return memory.NewDomainJournal(root, raw)
}

func mustRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed
}

func TestNewRequiresDependencies(t *testing.T) {
	_, err := New(nil, nil, nil, OrchestratorOptions{})
	if err == nil {
		t.Fatalf("New(nil,nil,nil) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "memory manager is required") {
		t.Fatalf("New(nil,nil,nil) error = %v, want manager required", err)
	}

	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	_, err = New(mgr, nil, nil, OrchestratorOptions{})
	if err == nil || !strings.Contains(err.Error(), "memory journal is required") {
		t.Fatalf("New(mgr,nil,nil) error = %v, want journal required", err)
	}

	j := newTestDomainJournal(t, root)
	_, err = New(mgr, j, nil, OrchestratorOptions{})
	if err == nil || !strings.Contains(err.Error(), "memory projector is required") {
		t.Fatalf("New(mgr,j,nil) error = %v, want projector required", err)
	}
}

func TestRecordWithAdapterBuildError(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{})
	o, err := New(mgr, j, p, OrchestratorOptions{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = o.RecordWithAdapter(fakeRecordAdapter{err: fmt.Errorf("bad input")})
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("RecordWithAdapter(build error) = %v, want bad input", err)
	}
}
