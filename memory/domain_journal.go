package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const (
	domainJournalMemoryDomain = "memory"
	domainJournalMemoryRecord = "record"
	domainJournalCheckpoint   = "projection_checkpoint.json"
)

var errReplayLimitReached = errors.New("memory replay limit reached")

type EventJournal interface {
	ReplayFrom(cursor JournalCursor, limit int, fn func(JournalRecord) error) (JournalCursor, bool, error)
	LoadCheckpoint() (JournalCheckpoint, bool, error)
	SaveCheckpoint(JournalCheckpoint) error
}

type DomainJournal struct {
	root    string
	journal *domainjournal.Journal
	now     func() time.Time
}

func NewDomainJournal(root string, journal *domainjournal.Journal) *DomainJournal {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "memory"
	}
	return &DomainJournal{
		root:    root,
		journal: journal,
		now:     time.Now,
	}
}

func (j *DomainJournal) Append(event MemoryEvent) error {
	if j == nil || j.journal == nil {
		return fmt.Errorf("domain journal is required")
	}
	if err := ValidateMemoryEventForAppend(event); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode memory journal payload: %w", err)
	}
	trace := domainjournal.Trace{
		TraceID: strings.TrimSpace(event.TaskRunID),
		Runtime: strings.TrimSpace(event.Channel),
		Target:  strings.TrimSpace(event.Channel),
		TaskID:  strings.TrimSpace(event.TaskRunID),
	}
	if event.SessionContext.ConversationID != "" {
		trace.TopicID = strings.TrimSpace(event.SessionContext.ConversationID)
	}
	_, err = j.journal.Append(domainjournal.Event{
		ID:            strings.TrimSpace(event.EventID),
		Time:          strings.TrimSpace(event.TSUTC),
		Domain:        domainJournalMemoryDomain,
		Type:          domainJournalMemoryRecord,
		SchemaVersion: 1,
		Trace:         trace,
		Payload:       payload,
	})
	return err
}

func (j *DomainJournal) ReplayFrom(cursor JournalCursor, limit int, fn func(JournalRecord) error) (JournalCursor, bool, error) {
	if j == nil || j.journal == nil {
		return JournalCursor{}, false, fmt.Errorf("domain journal is required")
	}
	if fn == nil {
		return JournalCursor{}, false, fmt.Errorf("replay callback is required")
	}
	if limit <= 0 {
		return JournalCursor{}, false, fmt.Errorf("limit must be > 0")
	}
	delivered := 0
	next := cursor
	err := j.journal.ReplayFrom(domainjournal.Cursor{
		File: cursor.File,
		Line: cursor.Line,
		Byte: cursor.Byte,
	}, func(rec domainjournal.Record) error {
		if rec.Event.Domain != domainJournalMemoryDomain || rec.Event.Type != domainJournalMemoryRecord {
			next = JournalCursor{File: rec.Cursor.File, Line: rec.Cursor.Line, Byte: rec.Cursor.Byte}
			return nil
		}
		if delivered >= limit {
			return errReplayLimitReached
		}
		var event MemoryEvent
		if err := json.Unmarshal(rec.Event.Payload, &event); err != nil {
			return fmt.Errorf("decode memory journal payload %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		if err := ValidateMemoryEventForAppend(event); err != nil {
			return fmt.Errorf("invalid memory journal payload %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		next = JournalCursor{File: rec.Cursor.File, Line: rec.Cursor.Line, Byte: rec.Cursor.Byte}
		delivered++
		return fn(JournalRecord{
			Cursor: next,
			Event:  event,
		})
	})
	if errors.Is(err, errReplayLimitReached) {
		return next, false, nil
	}
	if err != nil {
		return next, false, err
	}
	return next, true, nil
}

func (j *DomainJournal) LoadCheckpoint() (JournalCheckpoint, bool, error) {
	path := j.checkpointPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return JournalCheckpoint{}, false, nil
		}
		return JournalCheckpoint{}, false, err
	}
	var cp JournalCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return JournalCheckpoint{}, false, err
	}
	if err := validateDomainJournalCheckpoint(cp); err != nil {
		return JournalCheckpoint{}, false, err
	}
	return cp, true, nil
}

func (j *DomainJournal) SaveCheckpoint(cp JournalCheckpoint) error {
	if strings.TrimSpace(cp.UpdatedAt) == "" {
		cp.UpdatedAt = j.now().UTC().Format(time.RFC3339)
	}
	if err := validateDomainJournalCheckpoint(cp); err != nil {
		return err
	}
	if err := os.MkdirAll(j.root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := j.checkpointPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (j *DomainJournal) checkpointPath() string {
	root := strings.TrimSpace(j.root)
	if root == "" {
		root = "memory"
	}
	return filepath.Join(root, domainJournalCheckpoint)
}

func validateDomainJournalCheckpoint(cp JournalCheckpoint) error {
	if strings.TrimSpace(cp.File) != cp.File {
		return fmt.Errorf("checkpoint.file must not contain leading/trailing spaces")
	}
	if cp.File != "" && !domainjournal.IsSegmentFile(cp.File) {
		return fmt.Errorf("checkpoint.file is invalid")
	}
	if cp.Line < 0 {
		return fmt.Errorf("checkpoint.line must be >= 0")
	}
	if cp.Byte < 0 {
		return fmt.Errorf("checkpoint.byte must be >= 0")
	}
	if strings.TrimSpace(cp.UpdatedAt) != cp.UpdatedAt {
		return fmt.Errorf("checkpoint.updated_at must not contain leading/trailing spaces")
	}
	if cp.UpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339, cp.UpdatedAt); err != nil {
			return fmt.Errorf("checkpoint.updated_at must be RFC3339")
		}
	}
	return nil
}
