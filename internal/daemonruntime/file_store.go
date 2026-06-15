package daemonruntime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/viper"
)

type FileTaskStoreOptions struct {
	RootDir        string
	Target         string
	Persist        bool
	RotateMaxBytes int64
	Journal        *domainjournal.Journal
	JournalDir     string
}

type FileTaskStore struct {
	mu sync.RWMutex

	rootDir          string
	target           string
	persist          bool
	rotateMaxBytes   int64
	journal          *domainjournal.Journal
	projectionCursor domainjournal.Cursor

	items    map[string]TaskInfo
	triggers map[string]TaskTrigger
}

func TaskPersistenceEnabled(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, raw := range viper.GetStringSlice("tasks.persistence_targets") {
		if strings.EqualFold(strings.TrimSpace(raw), target) {
			return true
		}
	}
	return false
}

func NewTaskViewForTarget(target string, maxItems int) (TaskView, error) {
	target = strings.TrimSpace(target)
	if !TaskPersistenceEnabled(target) {
		return NewMemoryStore(maxItems), nil
	}
	return NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        statepaths.TaskTargetDir(target),
		Target:         target,
		Persist:        true,
		RotateMaxBytes: viper.GetInt64("tasks.rotate_max_bytes"),
		JournalDir:     statepaths.JournalDir(),
	})
}

func NewFileTaskStore(opts FileTaskStoreOptions) (*FileTaskStore, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	if opts.Persist && rootDir == "" {
		return nil, fmt.Errorf("task store root dir is required")
	}
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		target = "tasks"
	}
	s := &FileTaskStore{
		rootDir:        filepath.Clean(rootDir),
		target:         target,
		persist:        opts.Persist,
		rotateMaxBytes: opts.RotateMaxBytes,
		journal:        opts.Journal,
		items:          map[string]TaskInfo{},
		triggers:       map[string]TaskTrigger{},
	}
	if s.persist && s.journal == nil {
		journalDir := strings.TrimSpace(opts.JournalDir)
		journal, err := newTaskDomainJournal(journalDir, opts.RotateMaxBytes)
		if err != nil {
			return nil, err
		}
		s.journal = journal
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileTaskStore) Upsert(info TaskInfo) {
	_ = s.RecordTaskUpsert(info, TaskTrigger{})
}

func (s *FileTaskStore) RecordTaskUpsert(info TaskInfo, trigger TaskTrigger) error {
	if s == nil {
		return nil
	}
	info = normalizeFileTaskInfo(info)
	if info.ID == "" {
		return nil
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	actualTrigger := s.triggerForTaskLocked(info.ID, trigger)
	cursor, err := s.appendTaskEventLocked(info, now, actualTrigger, taskJournalTypeTaskUpsert)
	if err != nil {
		return err
	}
	s.items[info.ID] = info
	if hasTaskTrigger(trigger) {
		s.triggers[info.ID] = normalizeTaskTrigger(trigger)
	}
	s.projectionCursor = cursor
	return s.persistSnapshotLocked(now)
}

func (s *FileTaskStore) Update(id string, fn func(*TaskInfo)) {
	_ = s.RecordTaskUpdate(id, TaskTrigger{}, fn)
}

func (s *FileTaskStore) RecordTaskUpdate(id string, trigger TaskTrigger, fn func(*TaskInfo)) error {
	if s == nil || fn == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok {
		return nil
	}
	fn(&item)
	item = normalizeFileTaskInfo(item)
	item.ID = id
	actualTrigger := s.triggerForTaskLocked(id, trigger)
	cursor, err := s.appendTaskEventLocked(item, now, actualTrigger, taskJournalTypeTaskUpdate)
	if err != nil {
		return err
	}
	if hasTaskTrigger(trigger) {
		s.triggers[id] = normalizeTaskTrigger(trigger)
	}
	s.items[id] = item
	s.projectionCursor = cursor
	return s.persistSnapshotLocked(now)
}

func (s *FileTaskStore) Get(id string) (*TaskInfo, bool) {
	if s == nil {
		return nil, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	s.mu.RLock()
	item, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	cp := item
	return &cp, true
}

func (s *FileTaskStore) List(opts TaskListOptions) []TaskInfo {
	if s == nil {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = taskListDefaultLimit
	}
	if limit > taskListInternalMaxLimit {
		limit = taskListInternalMaxLimit
	}
	statusNorm := strings.TrimSpace(strings.ToLower(string(opts.Status)))
	topicID := strings.TrimSpace(opts.TopicID)

	s.mu.RLock()
	out := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		if statusNorm != "" && strings.ToLower(string(item.Status)) != statusNorm {
			continue
		}
		if topicID != "" && strings.TrimSpace(item.TopicID) != topicID {
			continue
		}
		out = append(out, item)
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	out = filterTasksByCursor(out, opts.Cursor)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *FileTaskStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.persist {
		return nil
	}
	start, err := s.loadSnapshotLocked()
	if err != nil {
		return err
	}
	if err := s.replayJournalLocked(start); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.recoverNonTerminalTasksLocked(now); err != nil {
		return err
	}
	return s.persistSnapshotLocked(now)
}

func (s *FileTaskStore) loadSnapshotLocked() (domainjournal.Cursor, error) {
	snap, ok, err := loadTaskProjectionSnapshot(s.rootDir)
	if err != nil || !ok {
		return domainjournal.Cursor{}, err
	}
	s.items = map[string]TaskInfo{}
	for _, item := range snap.Items {
		item = normalizeFileTaskInfo(item)
		if item.ID != "" {
			s.items[item.ID] = item
		}
	}
	s.triggers = map[string]TaskTrigger{}
	for id, trigger := range snap.Triggers {
		id = strings.TrimSpace(id)
		if id != "" && hasTaskTrigger(trigger) {
			s.triggers[id] = normalizeTaskTrigger(trigger)
		}
	}
	s.projectionCursor = snap.Cursor
	return snap.Cursor, nil
}

func (s *FileTaskStore) replayJournalLocked(cursor domainjournal.Cursor) error {
	if s.journal == nil {
		return nil
	}
	return s.journal.ReplayFrom(cursor, func(rec domainjournal.Record) error {
		if rec.Event.Domain != taskJournalDomain {
			return nil
		}
		var payload taskJournalPayload
		if err := json.Unmarshal(rec.Event.Payload, &payload); err != nil {
			return fmt.Errorf("decode task journal payload %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		if strings.TrimSpace(payload.Target) != "" && !strings.EqualFold(strings.TrimSpace(payload.Target), s.target) {
			return nil
		}
		switch rec.Event.Type {
		case taskJournalTypeTaskUpsert, taskJournalTypeTaskUpdate:
			if payload.Task == nil {
				return nil
			}
			info := normalizeFileTaskInfo(*payload.Task)
			if info.ID == "" {
				return nil
			}
			s.items[info.ID] = info
			if payload.Trigger != nil && hasTaskTrigger(*payload.Trigger) {
				s.triggers[info.ID] = normalizeTaskTrigger(*payload.Trigger)
			}
			s.projectionCursor = rec.Cursor
		}
		return nil
	})
}

func (s *FileTaskStore) recoverNonTerminalTasksLocked(now time.Time) error {
	for id, item := range s.items {
		switch item.Status {
		case TaskQueued, TaskRunning, TaskPending:
		default:
			continue
		}
		item.Status = TaskCanceled
		item.Error = "runtime restarted"
		item.FinishedAt = &now
		cursor, err := s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, TaskTrigger{}), taskJournalTypeTaskUpdate)
		if err != nil {
			return err
		}
		s.items[id] = item
		s.projectionCursor = cursor
	}
	return nil
}

func (s *FileTaskStore) appendTaskEventLocked(info TaskInfo, now time.Time, trigger TaskTrigger, defaultType string) (domainjournal.Cursor, error) {
	if !s.persist {
		return domainjournal.Cursor{}, nil
	}
	return appendTaskDomainEvent(s.journal, s.target, defaultType, now, trigger, &info, nil)
}

func (s *FileTaskStore) persistSnapshotLocked(now time.Time) error {
	if !s.persist {
		return nil
	}
	items := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	triggers := make(map[string]TaskTrigger, len(s.triggers))
	for id, trigger := range s.triggers {
		if hasTaskTrigger(trigger) {
			triggers[id] = normalizeTaskTrigger(trigger)
		}
	}
	return saveTaskProjectionSnapshot(s.rootDir, taskProjectionSnapshot{
		UpdatedAt: now,
		Cursor:    s.projectionCursor,
		Items:     items,
		Triggers:  triggers,
	})
}

func normalizeFileTaskInfo(info TaskInfo) TaskInfo {
	info.ID = strings.TrimSpace(info.ID)
	info.Task = strings.TrimSpace(info.Task)
	info.Model = strings.TrimSpace(info.Model)
	info.Timeout = strings.TrimSpace(info.Timeout)
	info.Error = strings.TrimSpace(info.Error)
	info.TopicID = strings.TrimSpace(info.TopicID)
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now().UTC()
	} else {
		info.CreatedAt = info.CreatedAt.UTC()
	}
	info.Status, _ = ParseTaskStatus(string(info.Status))
	return info
}

func (s *FileTaskStore) triggerForTaskLocked(taskID string, trigger TaskTrigger) TaskTrigger {
	if hasTaskTrigger(trigger) {
		return normalizeTaskTrigger(trigger)
	}
	if saved, ok := s.triggers[taskID]; ok && hasTaskTrigger(saved) {
		return saved
	}
	return TaskTrigger{}
}
