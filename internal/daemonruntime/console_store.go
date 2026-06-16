package daemonruntime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const (
	legacyConsoleTopicFileVersion = 1
	legacyConsoleTaskLogDirName   = "log"
	legacyConsoleHeartbeatTopicID = "_heartbeat"
	legacyConsoleHeartbeatTitle   = "Heartbeat"
)

type ConsoleFileStoreOptions struct {
	RootDir        string
	Persist        bool
	Journal        *domainjournal.Journal
	JournalDir     string
	RotateMaxBytes int64
}

type ConsoleFileStore struct {
	mu sync.RWMutex

	rootDir          string
	persist          bool
	journal          *domainjournal.Journal
	projectionCursor domainjournal.Cursor

	items    map[string]TaskInfo
	topics   map[string]TopicInfo
	triggers map[string]TaskTrigger
}

type legacyConsoleTopicFile struct {
	Version   int         `json:"version"`
	UpdatedAt time.Time   `json:"updated_at"`
	Items     []TopicInfo `json:"items"`
}

type legacyConsoleTaskEvent struct {
	Type    string       `json:"type"`
	At      time.Time    `json:"at"`
	Channel string       `json:"channel"`
	Trigger *TaskTrigger `json:"trigger,omitempty"`
	Task    TaskInfo     `json:"task"`
}

func NewConsoleFileStore(opts ConsoleFileStoreOptions) (*ConsoleFileStore, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	if opts.Persist && rootDir == "" {
		return nil, fmt.Errorf("console task store root dir is required")
	}
	s := &ConsoleFileStore{
		rootDir:  filepath.Clean(rootDir),
		persist:  opts.Persist,
		journal:  opts.Journal,
		items:    map[string]TaskInfo{},
		topics:   map[string]TopicInfo{},
		triggers: map[string]TaskTrigger{},
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

func (s *ConsoleFileStore) ApplyConfig(opts ConsoleFileStoreOptions) error {
	if s == nil {
		return fmt.Errorf("console task store is nil")
	}
	rootDir := strings.TrimSpace(opts.RootDir)
	if opts.Persist && rootDir == "" {
		return fmt.Errorf("console task store root dir is required")
	}
	now := time.Now().UTC()
	nextRootDir := filepath.Clean(rootDir)
	nextJournal := opts.Journal
	if opts.Persist && nextJournal == nil {
		journalDir := strings.TrimSpace(opts.JournalDir)
		var err error
		nextJournal, err = newTaskDomainJournal(journalDir, opts.RotateMaxBytes)
		if err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldRootDir := s.rootDir
	oldPersist := s.persist

	if !opts.Persist {
		s.rootDir = nextRootDir
		s.persist = false
		s.journal = nil
		s.projectionCursor = domainjournal.Cursor{}
		return nil
	}
	if err := s.persistSnapshotAtRootLocked(nextRootDir, now); err != nil {
		return err
	}
	if oldPersist && nextRootDir == oldRootDir {
		s.rootDir = nextRootDir
		s.persist = true
		s.journal = nextJournal
		return nil
	}
	s.rootDir = nextRootDir
	s.persist = true
	s.journal = nextJournal
	return nil
}

func (s *ConsoleFileStore) CreateTopic(title string) (TopicInfo, error) {
	if s == nil {
		return TopicInfo{}, fmt.Errorf("console task store is nil")
	}
	now := time.Now().UTC()
	topic := TopicInfo{
		ID:        buildConsoleTopicID(now),
		Title:     strings.TrimSpace(title),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, err := s.appendTopicEventLocked(taskJournalTypeTopicUpsert, topic, now, TaskTrigger{})
	if err != nil {
		return TopicInfo{}, err
	}
	s.topics[topic.ID] = topic
	s.projectionCursor = cursor
	if err := s.persistSnapshotLocked(now); err != nil {
		return TopicInfo{}, err
	}
	return topic, nil
}

func (s *ConsoleFileStore) Upsert(info TaskInfo) {
	_ = s.UpsertWithTrigger(info, TaskTrigger{}, "")
}

func (s *ConsoleFileStore) RecordTaskUpsert(info TaskInfo, trigger TaskTrigger) error {
	return s.UpsertWithTrigger(info, trigger, "")
}

func (s *ConsoleFileStore) UpsertWithTrigger(info TaskInfo, trigger TaskTrigger, topicTitle string) error {
	if s == nil {
		return nil
	}
	info = normalizeConsoleTaskInfo(info)
	if info.ID == "" {
		return nil
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	info.TopicID = s.normalizeTopicIDLocked(info.TopicID)
	cursor, err := s.appendTaskEventLocked(info, now, s.triggerForTaskLocked(info.ID, trigger), taskJournalTypeTaskUpsert)
	if err != nil {
		return err
	}
	topic := s.ensureTopicLocked(info.TopicID, topicTitle, now, true)
	topicCursor, err := s.appendTopicEventLocked(taskJournalTypeTopicUpsert, topic, now, trigger)
	if err != nil {
		return err
	}
	cursor = topicCursor
	s.items[info.ID] = info
	if hasTaskTrigger(trigger) {
		s.triggers[info.ID] = normalizeTaskTrigger(trigger)
	}
	s.projectionCursor = cursor
	return s.persistSnapshotLocked(now)
}

func (s *ConsoleFileStore) Update(id string, fn func(*TaskInfo)) {
	_ = s.UpdateWithTrigger(id, TaskTrigger{}, fn)
}

func (s *ConsoleFileStore) RecordTaskUpdate(id string, trigger TaskTrigger, fn func(*TaskInfo)) error {
	return s.UpdateWithTrigger(id, trigger, fn)
}

func (s *ConsoleFileStore) UpdateWithTrigger(id string, trigger TaskTrigger, fn func(*TaskInfo)) error {
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
	item = normalizeConsoleTaskInfo(item)
	item.ID = id
	item.TopicID = s.normalizeTopicIDLocked(item.TopicID)
	cursor, err := s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, trigger), taskJournalTypeTaskUpdate)
	if err != nil {
		return err
	}
	s.items[id] = item
	if hasTaskTrigger(trigger) {
		s.triggers[id] = normalizeTaskTrigger(trigger)
	}
	s.projectionCursor = cursor
	return s.persistSnapshotLocked(now)
}

func (s *ConsoleFileStore) Get(id string) (*TaskInfo, bool) {
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

func (s *ConsoleFileStore) GetTopic(id string) (*TopicInfo, bool) {
	if s == nil {
		return nil, false
	}
	id = normalizeConsoleTopicID(id)
	if id == "" {
		return nil, false
	}
	s.mu.RLock()
	topic, ok := s.topics[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	cp := topic
	return &cp, true
}

func (s *ConsoleFileStore) GetTrigger(taskID string) (TaskTrigger, bool) {
	if s == nil {
		return TaskTrigger{}, false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return TaskTrigger{}, false
	}
	s.mu.RLock()
	trigger, ok := s.triggers[taskID]
	s.mu.RUnlock()
	if !ok {
		return TaskTrigger{}, false
	}
	return trigger, true
}

func (s *ConsoleFileStore) List(opts TaskListOptions) []TaskInfo {
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
	topicID := normalizeConsoleTopicID(opts.TopicID)

	s.mu.RLock()
	out := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		if statusNorm != "" && strings.ToLower(string(item.Status)) != statusNorm {
			continue
		}
		if topicID != "" && strings.TrimSpace(item.TopicID) != topicID {
			continue
		}
		if topicID == "" && strings.TrimSpace(item.TopicID) == ConsoleAwarenessTopicID {
			continue
		}
		if topicDeleted(s.topics[strings.TrimSpace(item.TopicID)]) {
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

func (s *ConsoleFileStore) ListTopics() []TopicInfo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	out := make([]TopicInfo, 0, len(s.topics))
	for _, topic := range s.topics {
		if topicDeleted(topic) {
			continue
		}
		out = append(out, topic)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *ConsoleFileStore) DeleteTopic(id string) bool {
	if s == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" || id == ConsoleDefaultTopicID {
		return false
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	topic, ok := s.topics[id]
	if !ok {
		return false
	}
	if topic.DeletedAt != nil {
		return true
	}
	topic.DeletedAt = &now
	topic.UpdatedAt = now
	cursor, err := s.appendTopicEventLocked(taskJournalTypeTopicDeleted, topic, now, TaskTrigger{})
	if err != nil {
		return false
	}
	s.topics[id] = topic
	s.projectionCursor = cursor
	if err := s.persistSnapshotLocked(now); err != nil {
		return false
	}
	return true
}

func (s *ConsoleFileStore) SetTopicTitle(id string, title string) error {
	return s.setTopicTitle(id, title, false)
}

func (s *ConsoleFileStore) SetTopicTitleFromLLM(id string, title string) error {
	return s.setTopicTitle(id, title, true)
}

func (s *ConsoleFileStore) setTopicTitle(id string, title string, fromLLM bool) error {
	if s == nil {
		return fmt.Errorf("console task store is nil")
	}
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	if id == "" {
		return fmt.Errorf("missing topic id")
	}
	if title == "" {
		return fmt.Errorf("missing topic title")
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	topic, ok := s.topics[id]
	if !ok || topicDeleted(topic) {
		return fmt.Errorf("topic %q not found", id)
	}
	if fromLLM && topic.LLMTitleGeneratedAt != nil {
		return nil
	}
	changed := false
	if topic.Title != title {
		topic.Title = title
		changed = true
	}
	if fromLLM && topic.LLMTitleGeneratedAt == nil {
		generatedAt := now
		topic.LLMTitleGeneratedAt = &generatedAt
		changed = true
	}
	if !changed {
		return nil
	}
	topic.UpdatedAt = now
	topic = normalizeTopicInfo(topic)
	cursor, err := s.appendTopicEventLocked(taskJournalTypeTopicTitleUpdated, topic, now, TaskTrigger{})
	if err != nil {
		return err
	}
	s.topics[id] = topic
	s.projectionCursor = cursor
	return s.persistSnapshotLocked(now)
}

func (s *ConsoleFileStore) load() error {
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
	s.pruneUnusedDefaultTopicLocked()
	now := time.Now().UTC()
	if err := s.recoverNonTerminalTasksLocked(now); err != nil {
		return err
	}
	return s.persistSnapshotLocked(now)
}

func (s *ConsoleFileStore) loadSnapshotLocked() (domainjournal.Cursor, error) {
	snap, ok, err := loadTaskProjectionSnapshot(s.rootDir)
	if err != nil {
		return domainjournal.Cursor{}, err
	}
	if !ok {
		if err := s.loadLegacyProjectionLocked(); err != nil {
			return domainjournal.Cursor{}, err
		}
		return domainjournal.Cursor{}, nil
	}
	s.items = map[string]TaskInfo{}
	for _, item := range snap.Items {
		item = normalizeConsoleTaskInfo(item)
		if item.ID != "" {
			s.items[item.ID] = item
		}
	}
	s.topics = map[string]TopicInfo{}
	for _, topic := range snap.Topics {
		topic = normalizeTopicInfo(topic)
		if topic.ID != "" {
			s.topics[topic.ID] = topic
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

func (s *ConsoleFileStore) loadLegacyProjectionLocked() error {
	if err := s.loadLegacyTopicsLocked(); err != nil {
		return err
	}
	return s.loadLegacyTaskLogsLocked()
}

func (s *ConsoleFileStore) loadLegacyTopicsLocked() error {
	path := filepath.Join(s.rootDir, "topic.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file legacyConsoleTopicFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse legacy topic.json: %w", err)
	}
	if file.Version != 0 && file.Version != legacyConsoleTopicFileVersion {
		return fmt.Errorf("unsupported legacy topic.json version: %d", file.Version)
	}
	for _, item := range file.Items {
		topic := normalizeLegacyConsoleTopicInfo(item)
		if topic.ID != "" {
			s.topics[topic.ID] = topic
		}
	}
	return nil
}

func (s *ConsoleFileStore) loadLegacyTaskLogsLocked() error {
	logDir := filepath.Join(s.rootDir, legacyConsoleTaskLogDirName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.loadLegacyTaskLogFileLocked(filepath.Join(logDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConsoleFileStore) loadLegacyTaskLogFileLocked(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event legacyConsoleTaskEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("parse legacy task log %s line %d: %w", filepath.Base(path), lineNo, err)
		}
		if event.Type != taskJournalTypeTaskUpsert {
			continue
		}
		info := normalizeLegacyConsoleTaskInfo(event.Task)
		if info.ID == "" {
			continue
		}
		info.TopicID = s.normalizeTopicIDLocked(info.TopicID)
		s.items[info.ID] = info
		if event.Trigger != nil && hasTaskTrigger(*event.Trigger) {
			s.triggers[info.ID] = normalizeTaskTrigger(*event.Trigger)
		}
		s.ensureTopicLocked(info.TopicID, "", info.CreatedAt, false)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read legacy task log %s: %w", filepath.Base(path), err)
	}
	return nil
}

func (s *ConsoleFileStore) replayJournalLocked(cursor domainjournal.Cursor) error {
	if s.journal == nil {
		return nil
	}
	return s.journal.ReplayFrom(cursor, func(rec domainjournal.Record) error {
		if rec.Event.Domain != taskJournalDomain {
			return nil
		}
		var payload taskJournalPayload
		if err := json.Unmarshal(rec.Event.Payload, &payload); err != nil {
			return fmt.Errorf("decode console task journal payload %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		if strings.TrimSpace(payload.Target) != "" && !strings.EqualFold(strings.TrimSpace(payload.Target), "console") {
			return nil
		}
		switch rec.Event.Type {
		case taskJournalTypeTopicUpsert, taskJournalTypeTopicTitleUpdated, taskJournalTypeTopicDeleted:
			if payload.Topic == nil {
				return nil
			}
			topic := normalizeTopicInfo(*payload.Topic)
			if topic.ID != "" {
				s.topics[topic.ID] = topic
				s.projectionCursor = rec.Cursor
			}
		case taskJournalTypeTaskUpsert, taskJournalTypeTaskUpdate:
			if payload.Task == nil {
				return nil
			}
			info := normalizeConsoleTaskInfo(*payload.Task)
			if info.ID == "" {
				return nil
			}
			s.items[info.ID] = info
			if payload.Trigger != nil && hasTaskTrigger(*payload.Trigger) {
				s.triggers[info.ID] = normalizeTaskTrigger(*payload.Trigger)
			}
			s.ensureTopicLocked(info.TopicID, "", info.CreatedAt, false)
			s.projectionCursor = rec.Cursor
		}
		return nil
	})
}

func (s *ConsoleFileStore) recoverNonTerminalTasksLocked(now time.Time) error {
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

func (s *ConsoleFileStore) appendTaskEventLocked(info TaskInfo, now time.Time, trigger TaskTrigger, defaultType string) (domainjournal.Cursor, error) {
	if !s.persist {
		return domainjournal.Cursor{}, nil
	}
	return appendTaskDomainEvent(s.journal, "console", defaultType, now, trigger, &info, nil)
}

func (s *ConsoleFileStore) appendTopicEventLocked(typ string, topic TopicInfo, now time.Time, trigger TaskTrigger) (domainjournal.Cursor, error) {
	if !s.persist {
		return domainjournal.Cursor{}, nil
	}
	topic = normalizeTopicInfo(topic)
	return appendTaskDomainEvent(s.journal, "console", typ, now, trigger, nil, &topic)
}

func (s *ConsoleFileStore) persistSnapshotLocked(now time.Time) error {
	return s.persistSnapshotAtRootLocked(s.rootDir, now)
}

func (s *ConsoleFileStore) persistSnapshotAtRootLocked(rootDir string, now time.Time) error {
	if !s.persist {
		return nil
	}
	items := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	topics := make([]TopicInfo, 0, len(s.topics))
	for _, topic := range s.topics {
		topics = append(topics, normalizeTopicInfo(topic))
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].CreatedAt.Equal(topics[j].CreatedAt) {
			return topics[i].ID < topics[j].ID
		}
		return topics[i].CreatedAt.Before(topics[j].CreatedAt)
	})
	triggers := make(map[string]TaskTrigger, len(s.triggers))
	for id, trigger := range s.triggers {
		if hasTaskTrigger(trigger) {
			triggers[id] = normalizeTaskTrigger(trigger)
		}
	}
	return saveTaskProjectionSnapshot(rootDir, taskProjectionSnapshot{
		UpdatedAt: now,
		Cursor:    s.projectionCursor,
		Items:     items,
		Topics:    topics,
		Triggers:  triggers,
	})
}

func (s *ConsoleFileStore) ensureTopicLocked(topicID string, title string, now time.Time, touch bool) TopicInfo {
	topicID = normalizeConsoleTopicID(topicID)
	if topicID == "" {
		topicID = ConsoleDefaultTopicID
	}
	title = strings.TrimSpace(title)
	topic, ok := s.topics[topicID]
	if !ok {
		topic = TopicInfo{
			ID:        topicID,
			Title:     title,
			CreatedAt: nonZeroTime(now),
			UpdatedAt: nonZeroTime(now),
		}
		if topic.ID == ConsoleDefaultTopicID && topic.Title == "" {
			topic.Title = ConsoleDefaultTopicTitle
		}
		if topic.ID == ConsoleAwarenessTopicID && topic.Title == "" {
			topic.Title = ConsoleAwarenessTopicTitle
		}
		s.topics[topicID] = topic
		return topic
	}
	changed := false
	if title != "" && topic.Title != title {
		topic.Title = title
		changed = true
	}
	if touch {
		topic.UpdatedAt = nonZeroTime(now)
		changed = true
	}
	if topic.ID == ConsoleDefaultTopicID && topic.Title == "" {
		topic.Title = ConsoleDefaultTopicTitle
		changed = true
	}
	if topic.ID == ConsoleAwarenessTopicID && topic.Title == "" {
		topic.Title = ConsoleAwarenessTopicTitle
		changed = true
	}
	if changed {
		s.topics[topicID] = normalizeTopicInfo(topic)
	}
	return topic
}

func (s *ConsoleFileStore) pruneUnusedDefaultTopicLocked() {
	topic, ok := s.topics[ConsoleDefaultTopicID]
	if !ok || topicDeleted(topic) {
		return
	}
	for _, item := range s.items {
		if strings.TrimSpace(item.TopicID) == ConsoleDefaultTopicID {
			return
		}
	}
	delete(s.topics, ConsoleDefaultTopicID)
}

func (s *ConsoleFileStore) normalizeTopicIDLocked(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return ConsoleDefaultTopicID
	}
	return topicID
}

func (s *ConsoleFileStore) triggerForTaskLocked(taskID string, trigger TaskTrigger) TaskTrigger {
	if hasTaskTrigger(trigger) {
		return normalizeTaskTrigger(trigger)
	}
	if saved, ok := s.triggers[taskID]; ok && hasTaskTrigger(saved) {
		return saved
	}
	return TaskTrigger{}
}

func buildConsoleTopicID(now time.Time) string {
	_ = nonZeroTime(now)
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func normalizeConsoleTopicID(topicID string) string {
	return strings.TrimSpace(topicID)
}

func normalizeLegacyConsoleTopicID(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == legacyConsoleHeartbeatTopicID {
		return ConsoleAwarenessTopicID
	}
	return topicID
}

func normalizeLegacyConsoleTaskInfo(info TaskInfo) TaskInfo {
	info.TopicID = normalizeLegacyConsoleTopicID(info.TopicID)
	return normalizeConsoleTaskInfo(info)
}

func normalizeLegacyConsoleTopicInfo(topic TopicInfo) TopicInfo {
	topic.ID = normalizeLegacyConsoleTopicID(topic.ID)
	topic = normalizeTopicInfo(topic)
	if topic.ID == ConsoleAwarenessTopicID && strings.EqualFold(topic.Title, legacyConsoleHeartbeatTitle) {
		topic.Title = ConsoleAwarenessTopicTitle
	}
	return topic
}

func normalizeConsoleTaskInfo(info TaskInfo) TaskInfo {
	info.ID = strings.TrimSpace(info.ID)
	info.Task = strings.TrimSpace(info.Task)
	info.Model = strings.TrimSpace(info.Model)
	info.Timeout = strings.TrimSpace(info.Timeout)
	info.Error = strings.TrimSpace(info.Error)
	info.TopicID = normalizeConsoleTopicID(info.TopicID)
	if info.TopicID == "" {
		info.TopicID = ConsoleDefaultTopicID
	}
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now().UTC()
	} else {
		info.CreatedAt = info.CreatedAt.UTC()
	}
	info.Status, _ = ParseTaskStatus(string(info.Status))
	return info
}

func normalizeTaskTrigger(trigger TaskTrigger) TaskTrigger {
	return TaskTrigger{
		Source:  strings.TrimSpace(trigger.Source),
		Event:   strings.TrimSpace(trigger.Event),
		Ref:     strings.TrimSpace(trigger.Ref),
		TraceID: strings.TrimSpace(trigger.TraceID),
	}
}

func hasTaskTrigger(trigger TaskTrigger) bool {
	return strings.TrimSpace(trigger.Source) != "" ||
		strings.TrimSpace(trigger.Event) != "" ||
		strings.TrimSpace(trigger.Ref) != "" ||
		strings.TrimSpace(trigger.TraceID) != ""
}

func normalizeTopicInfo(topic TopicInfo) TopicInfo {
	topic.ID = normalizeConsoleTopicID(topic.ID)
	topic.Title = strings.TrimSpace(topic.Title)
	if topic.ID == ConsoleAwarenessTopicID && topic.Title == "" {
		topic.Title = ConsoleAwarenessTopicTitle
	}
	if topic.LLMTitleGeneratedAt != nil {
		generatedAt := topic.LLMTitleGeneratedAt.UTC()
		topic.LLMTitleGeneratedAt = &generatedAt
	}
	topic.CreatedAt = nonZeroTime(topic.CreatedAt)
	topic.UpdatedAt = nonZeroTime(topic.UpdatedAt)
	if topic.UpdatedAt.Before(topic.CreatedAt) {
		topic.UpdatedAt = topic.CreatedAt
	}
	if topic.DeletedAt != nil {
		deletedAt := topic.DeletedAt.UTC()
		topic.DeletedAt = &deletedAt
	}
	return topic
}

func topicDeleted(topic TopicInfo) bool {
	return topic.DeletedAt != nil
}

func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

func consoleTopicKey(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return ConsoleDefaultTopicID
	}
	var b strings.Builder
	for _, r := range topicID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	key := strings.TrimSpace(b.String())
	if key == "" {
		return ConsoleDefaultTopicID
	}
	return key
}
