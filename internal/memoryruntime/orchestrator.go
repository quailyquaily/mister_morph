package memoryruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/memory"
)

type OrchestratorOptions struct {
	Now        func() time.Time
	NewEventID func() string
}

type MemoryJournalWriter interface {
	Append(memory.MemoryEvent) error
}

type Orchestrator struct {
	manager   *memory.Manager
	journal   MemoryJournalWriter
	projector *memory.Projector
	now       func() time.Time
	newEvent  func() string
}

const journalSourceHistoryCap = 3

type PrepareInjectionRequest struct {
	SubjectID      string
	RequestContext memory.RequestContext
	MaxItems       int
}

type RecordRequest struct {
	TaskRunID      string
	TSUTC          string
	SessionID      string
	SubjectID      string
	Channel        string
	Participants   []memory.MemoryParticipant
	TaskText       string
	FinalOutput    string
	SourceHistory  []chathistory.ChatHistoryItem
	SessionContext memory.SessionContext
}

func New(manager *memory.Manager, journal MemoryJournalWriter, projector *memory.Projector, opts OrchestratorOptions) (*Orchestrator, error) {
	if manager == nil {
		return nil, fmt.Errorf("memory manager is required")
	}
	if journal == nil {
		return nil, fmt.Errorf("memory journal is required")
	}
	if projector == nil {
		return nil, fmt.Errorf("memory projector is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewEventID == nil {
		opts.NewEventID = func() string { return "evt_" + uuid.NewString() }
	}
	return &Orchestrator{
		manager:   manager,
		journal:   journal,
		projector: projector,
		now:       opts.Now,
		newEvent:  opts.NewEventID,
	}, nil
}

func (o *Orchestrator) PrepareInjection(req PrepareInjectionRequest) (string, error) {
	return o.manager.BuildInjection(req.SubjectID, req.RequestContext, req.MaxItems)
}

func (o *Orchestrator) Record(req RecordRequest) error {
	tsUTC := strings.TrimSpace(req.TSUTC)
	if tsUTC == "" {
		tsUTC = o.now().UTC().Format(time.RFC3339)
	}
	event := memory.MemoryEvent{
		SchemaVersion:  memory.CurrentMemoryEventSchemaVersion,
		EventID:        strings.TrimSpace(o.newEvent()),
		TaskRunID:      strings.TrimSpace(req.TaskRunID),
		TSUTC:          tsUTC,
		SessionID:      strings.TrimSpace(req.SessionID),
		SubjectID:      strings.TrimSpace(req.SubjectID),
		Channel:        strings.TrimSpace(req.Channel),
		Participants:   append([]memory.MemoryParticipant(nil), req.Participants...),
		TaskText:       strings.TrimSpace(req.TaskText),
		FinalOutput:    strings.TrimSpace(req.FinalOutput),
		SourceHistory:  capChatHistoryItems(req.SourceHistory, journalSourceHistoryCap),
		SessionContext: req.SessionContext,
	}
	return o.journal.Append(event)
}

func capChatHistoryItems(items []chathistory.ChatHistoryItem, limit int) []chathistory.ChatHistoryItem {
	if len(items) == 0 {
		return nil
	}
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]chathistory.ChatHistoryItem(nil), items...)
}
