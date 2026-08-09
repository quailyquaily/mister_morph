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
	"github.com/quailyquaily/mistermorph/internal/pagination"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
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
	journalDir       string
	persist          bool
	journal          *domainjournal.Journal
	projectionCursor domainjournal.Cursor
	projectionErr    error

	items             map[string]TaskInfo
	topics            map[string]TopicInfo
	triggers          map[string]TaskTrigger
	orderedIDs        []string
	orderedIDsByTopic map[string][]string
	orderedTopicIDs   []string
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
		rootDir:           filepath.Clean(rootDir),
		persist:           opts.Persist,
		journal:           opts.Journal,
		items:             map[string]TaskInfo{},
		topics:            map[string]TopicInfo{},
		triggers:          map[string]TaskTrigger{},
		orderedIDsByTopic: map[string][]string{},
	}
	if s.persist && s.journal == nil {
		journalDir := strings.TrimSpace(opts.JournalDir)
		journal, err := taskdomain.NewJournal(journalDir, opts.RotateMaxBytes)
		if err != nil {
			return nil, err
		}
		s.journal = journal
		s.journalDir = cleanOptionalPath(journalDir)
	} else if s.persist {
		s.journalDir = cleanOptionalPath(opts.JournalDir)
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
	nextJournalDir := cleanOptionalPath(opts.JournalDir)
	if opts.Persist && nextJournal == nil {
		journalDir := strings.TrimSpace(opts.JournalDir)
		var err error
		nextJournal, err = taskdomain.NewJournal(journalDir, opts.RotateMaxBytes)
		if err != nil {
			return err
		}
		nextJournalDir = cleanOptionalPath(journalDir)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldRootDir := s.rootDir
	oldPersist := s.persist
	oldJournalDir := s.journalDir
	oldJournal := s.journal

	if !opts.Persist {
		s.rootDir = nextRootDir
		s.journalDir = ""
		s.persist = false
		s.journal = nil
		s.projectionCursor = domainjournal.Cursor{}
		s.projectionErr = nil
		return nil
	}

	nextCursor := s.projectionCursor
	if !sameConsoleJournalStorage(oldPersist, oldJournal, oldJournalDir, nextJournal, nextJournalDir) {
		var err error
		nextCursor, err = s.seedJournalLocked(nextJournal, now)
		if err != nil {
			return err
		}
	}
	projectionErr := s.saveSnapshotAtRootLocked(nextRootDir, now, nextCursor)
	if oldPersist && nextRootDir == oldRootDir && nextCursor == s.projectionCursor {
		s.rootDir = nextRootDir
		s.journalDir = nextJournalDir
		s.persist = true
		s.journal = nextJournal
		s.projectionErr = projectionErr
		return nil
	}
	s.rootDir = nextRootDir
	s.journalDir = nextJournalDir
	s.persist = true
	s.journal = nextJournal
	s.projectionCursor = nextCursor
	s.projectionErr = projectionErr
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
	cursor, err := s.appendTopicEventLocked(taskdomain.JournalTypeTopicUpsert, topic, now, TaskTrigger{})
	if err != nil {
		return TopicInfo{}, err
	}
	s.topics[topic.ID] = topic
	s.orderedTopicIDs = upsertOrderedTopicID(s.orderedTopicIDs, s.topics, topic.ID)
	s.projectionCursor = cursor
	return topic, nil
}

func (s *ConsoleFileStore) Upsert(info TaskInfo) error {
	return s.UpsertWithTrigger(info, TaskTrigger{}, "")
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
	currentTopic, topicExists := s.topics[info.TopicID]
	topic := nextConsoleTopic(currentTopic, topicExists, info.TopicID, topicTitle, now, true)
	cursor, err := s.appendTaskEventLocked(info, &topic, now, s.triggerForTaskLocked(info.ID, trigger), taskdomain.JournalTypeTaskUpsert)
	if err != nil {
		return err
	}
	previous, existed := s.items[info.ID]
	previousTopic := currentTopic
	s.items[info.ID] = info
	s.updateTaskOrderLocked(previous, existed, info)
	s.topics[topic.ID] = topic
	if !topicExists || topicOrderChanged(previousTopic, topic) {
		s.orderedTopicIDs = upsertOrderedTopicID(s.orderedTopicIDs, s.topics, topic.ID)
	}
	if taskdomain.HasTaskTrigger(trigger) {
		s.triggers[info.ID] = taskdomain.NormalizeTaskTrigger(trigger)
	}
	s.projectionCursor = cursor
	return nil
}

func (s *ConsoleFileStore) Update(id string, fn func(*TaskInfo)) error {
	return s.UpdateWithTrigger(id, TaskTrigger{}, fn)
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
	cursor, err := s.appendTaskEventLocked(item, nil, now, s.triggerForTaskLocked(id, trigger), taskdomain.JournalTypeTaskUpdate)
	if err != nil {
		return err
	}
	previous := s.items[id]
	s.items[id] = item
	s.updateTaskOrderLocked(previous, true, item)
	if taskdomain.HasTaskTrigger(trigger) {
		s.triggers[id] = taskdomain.NormalizeTaskTrigger(trigger)
	}
	s.projectionCursor = cursor
	return nil
}

// ProjectionError reports the latest startup snapshot write or load failure.
// Task and topic events remain committed in the journal and replay from the
// saved cursor.
func (s *ConsoleFileStore) ProjectionError() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projectionErr
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

	cursor, cursorOK := pagination.ParseKeysetCursor(opts.Cursor)
	useCursor := cursorOK && strings.TrimSpace(opts.Cursor) != ""
	s.mu.RLock()
	orderedIDs := s.orderedIDs
	if topicID != "" {
		orderedIDs = s.orderedIDsByTopic[topicID]
	}
	start := 0
	if useCursor {
		start = sort.Search(len(orderedIDs), func(i int) bool {
			item := s.items[orderedIDs[i]]
			return pagination.FollowsKeysetCursor(item.CreatedAt, item.ID, cursor)
		})
	}
	out := make([]TaskInfo, 0, limit)
	for _, id := range orderedIDs[start:] {
		item, ok := s.items[id]
		if !ok {
			continue
		}
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
		if len(out) >= limit {
			break
		}
	}
	s.mu.RUnlock()
	return out
}

func (s *ConsoleFileStore) updateTaskOrderLocked(previous TaskInfo, existed bool, next TaskInfo) {
	orderChanged := !existed || taskOrderChanged(previous, next)
	previousTopicID := strings.TrimSpace(previous.TopicID)
	nextTopicID := strings.TrimSpace(next.TopicID)
	topicChanged := existed && previousTopicID != nextTopicID
	if orderChanged {
		s.orderedIDs = upsertOrderedTaskID(s.orderedIDs, s.items, next.ID)
	}
	if s.orderedIDsByTopic == nil {
		s.orderedIDsByTopic = map[string][]string{}
	}
	if topicChanged {
		previousIDs := removeOrderedTaskID(s.orderedIDsByTopic[previousTopicID], next.ID)
		if len(previousIDs) == 0 {
			delete(s.orderedIDsByTopic, previousTopicID)
		} else {
			s.orderedIDsByTopic[previousTopicID] = previousIDs
		}
	}
	if orderChanged || topicChanged {
		s.orderedIDsByTopic[nextTopicID] = upsertOrderedTaskID(s.orderedIDsByTopic[nextTopicID], s.items, next.ID)
	}
}

func (s *ConsoleFileStore) ListTopicsPage(opts TopicListOptions) []TopicInfo {
	if s == nil {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = topicListDefaultLimit
	}
	if limit > topicListInternalMax {
		limit = topicListInternalMax
	}
	cursor, cursorOK := pagination.ParseKeysetCursor(opts.Cursor)
	useCursor := cursorOK && strings.TrimSpace(opts.Cursor) != ""
	s.mu.RLock()
	start := 0
	if useCursor {
		start = sort.Search(len(s.orderedTopicIDs), func(i int) bool {
			topic := s.topics[s.orderedTopicIDs[i]]
			return pagination.FollowsKeysetCursor(topic.UpdatedAt, topic.ID, cursor)
		})
	}
	out := make([]TopicInfo, 0, limit)
	for _, id := range s.orderedTopicIDs[start:] {
		topic, ok := s.topics[id]
		if !ok || topicDeleted(topic) {
			continue
		}
		out = append(out, topic)
		if len(out) >= limit {
			break
		}
	}
	s.mu.RUnlock()
	return out
}

func (s *ConsoleFileStore) DeleteTopic(id string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("console task store is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" || id == ConsoleDefaultTopicID {
		return false, nil
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	topic, ok := s.topics[id]
	if !ok {
		return false, nil
	}
	if topic.DeletedAt != nil {
		return true, nil
	}
	topic.DeletedAt = &now
	topic.UpdatedAt = now
	cursor, err := s.appendTopicEventLocked(taskdomain.JournalTypeTopicDeleted, topic, now, TaskTrigger{})
	if err != nil {
		return false, err
	}
	s.topics[id] = topic
	s.orderedTopicIDs = removeOrderedTopicID(s.orderedTopicIDs, id)
	s.projectionCursor = cursor
	return true, nil
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
	cursor, err := s.appendTopicEventLocked(taskdomain.JournalTypeTopicTitleUpdated, topic, now, TaskTrigger{})
	if err != nil {
		return err
	}
	previous := s.topics[id]
	s.topics[id] = topic
	if topicOrderChanged(previous, topic) {
		s.orderedTopicIDs = upsertOrderedTopicID(s.orderedTopicIDs, s.topics, id)
	}
	s.projectionCursor = cursor
	return nil
}

func (s *ConsoleFileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.persist {
		return nil
	}

	start, err := s.loadSnapshotLocked()
	if err != nil {
		s.items = map[string]TaskInfo{}
		s.topics = map[string]TopicInfo{}
		s.triggers = map[string]TaskTrigger{}
		s.projectionCursor = domainjournal.Cursor{}
		s.projectionErr = err
		start = domainjournal.Cursor{}
	}
	if err := s.replayJournalLocked(start); err != nil {
		return err
	}
	s.pruneUnusedDefaultTopicLocked()
	now := time.Now().UTC()
	if err := s.recoverNonTerminalTasksLocked(now); err != nil {
		return err
	}
	s.orderedIDs = rebuildOrderedTaskIDs(s.items)
	s.orderedIDsByTopic = groupOrderedTaskIDsByTopic(s.orderedIDs, s.items)
	s.orderedTopicIDs = rebuildOrderedTopicIDs(s.topics)
	s.projectionErr = s.persistSnapshotLocked(now)
	return nil
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
		if id != "" && taskdomain.HasTaskTrigger(trigger) {
			s.triggers[id] = taskdomain.NormalizeTaskTrigger(trigger)
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
		if event.Type != taskdomain.JournalTypeTaskUpsert {
			continue
		}
		info := normalizeLegacyConsoleTaskInfo(event.Task)
		if info.ID == "" {
			continue
		}
		info.TopicID = s.normalizeTopicIDLocked(info.TopicID)
		s.items[info.ID] = info
		if event.Trigger != nil && taskdomain.HasTaskTrigger(*event.Trigger) {
			s.triggers[info.ID] = taskdomain.NormalizeTaskTrigger(*event.Trigger)
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
		if rec.Event.Domain != taskdomain.JournalDomain {
			s.projectionCursor = rec.Cursor
			return nil
		}
		payload, err := taskdomain.DecodeJournalPayload(rec.Event.Payload)
		if err != nil {
			return fmt.Errorf("decode console task journal payload %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		s.projectionCursor = rec.Cursor
		if strings.TrimSpace(payload.Target) != "" && !strings.EqualFold(strings.TrimSpace(payload.Target), "console") {
			return nil
		}
		switch rec.Event.Type {
		case taskdomain.JournalTypeTopicUpsert, taskdomain.JournalTypeTopicTitleUpdated, taskdomain.JournalTypeTopicDeleted:
			if payload.Topic == nil {
				return nil
			}
			topic := normalizeTopicInfo(*payload.Topic)
			if topic.ID != "" {
				s.topics[topic.ID] = topic
			}
		case taskdomain.JournalTypeTaskUpsert, taskdomain.JournalTypeTaskUpdate:
			if payload.Task == nil {
				return nil
			}
			info := normalizeConsoleTaskInfo(*payload.Task)
			if info.ID == "" {
				return nil
			}
			s.items[info.ID] = info
			if payload.Trigger != nil && taskdomain.HasTaskTrigger(*payload.Trigger) {
				s.triggers[info.ID] = taskdomain.NormalizeTaskTrigger(*payload.Trigger)
			}
			if payload.Topic != nil {
				topic := normalizeTopicInfo(*payload.Topic)
				if topic.ID != "" {
					s.topics[topic.ID] = topic
				}
			} else {
				s.ensureTopicLocked(info.TopicID, "", info.CreatedAt, false)
			}
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
		item.PendingAt = nil
		item.ApprovalRequestID = ""
		item.Result = nil
		cursor, err := s.appendTaskEventLocked(item, nil, now, s.triggerForTaskLocked(id, TaskTrigger{}), taskdomain.JournalTypeTaskUpdate)
		if err != nil {
			return err
		}
		s.items[id] = item
		s.projectionCursor = cursor
	}
	return nil
}

func (s *ConsoleFileStore) appendTaskEventLocked(info TaskInfo, topic *TopicInfo, now time.Time, trigger TaskTrigger, defaultType string) (domainjournal.Cursor, error) {
	if !s.persist {
		return domainjournal.Cursor{}, nil
	}
	return taskdomain.AppendJournalEvent(s.journal, "console", defaultType, now, trigger, &info, topic)
}

func (s *ConsoleFileStore) appendTopicEventLocked(typ string, topic TopicInfo, now time.Time, trigger TaskTrigger) (domainjournal.Cursor, error) {
	if !s.persist {
		return domainjournal.Cursor{}, nil
	}
	topic = normalizeTopicInfo(topic)
	return taskdomain.AppendJournalEvent(s.journal, "console", typ, now, trigger, nil, &topic)
}

func (s *ConsoleFileStore) persistSnapshotLocked(now time.Time) error {
	return s.persistSnapshotAtRootLocked(s.rootDir, now)
}

func (s *ConsoleFileStore) persistSnapshotAtRootLocked(rootDir string, now time.Time) error {
	if !s.persist {
		return nil
	}
	return s.saveSnapshotAtRootLocked(rootDir, now, s.projectionCursor)
}

func (s *ConsoleFileStore) saveSnapshotAtRootLocked(rootDir string, now time.Time, cursor domainjournal.Cursor) error {
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
		if taskdomain.HasTaskTrigger(trigger) {
			triggers[id] = taskdomain.NormalizeTaskTrigger(trigger)
		}
	}
	return saveTaskProjectionSnapshot(rootDir, taskProjectionSnapshot{
		UpdatedAt: now,
		Cursor:    cursor,
		Items:     items,
		Topics:    topics,
		Triggers:  triggers,
	})
}

func (s *ConsoleFileStore) seedJournalLocked(journal *domainjournal.Journal, now time.Time) (domainjournal.Cursor, error) {
	if journal == nil {
		return domainjournal.Cursor{}, fmt.Errorf("journal is required")
	}
	cursor := domainjournal.Cursor{}
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
	for _, topic := range topics {
		if strings.TrimSpace(topic.ID) == "" {
			continue
		}
		next, err := taskdomain.AppendJournalEvent(journal, "console", taskdomain.JournalTypeTopicUpsert, now, TaskTrigger{}, nil, &topic)
		if err != nil {
			return domainjournal.Cursor{}, err
		}
		cursor = next
	}

	items := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, normalizeConsoleTaskInfo(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		next, err := taskdomain.AppendJournalEvent(journal, "console", taskdomain.JournalTypeTaskUpsert, now, s.triggerForTaskLocked(item.ID, TaskTrigger{}), &item, nil)
		if err != nil {
			return domainjournal.Cursor{}, err
		}
		cursor = next
	}
	return cursor, nil
}

func (s *ConsoleFileStore) ensureTopicLocked(topicID string, title string, now time.Time, touch bool) TopicInfo {
	topicID = normalizeConsoleTopicID(topicID)
	if topicID == "" {
		topicID = ConsoleDefaultTopicID
	}
	topic, ok := s.topics[topicID]
	topic = nextConsoleTopic(topic, ok, topicID, title, now, touch)
	s.topics[topicID] = topic
	return topic
}

func nextConsoleTopic(topic TopicInfo, exists bool, topicID string, title string, now time.Time, touch bool) TopicInfo {
	title = strings.TrimSpace(title)
	if !exists {
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
		return normalizeTopicInfo(topic)
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
		topic = normalizeTopicInfo(topic)
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
	if taskdomain.HasTaskTrigger(trigger) {
		return taskdomain.NormalizeTaskTrigger(trigger)
	}
	if saved, ok := s.triggers[taskID]; ok && taskdomain.HasTaskTrigger(saved) {
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

func cleanOptionalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func sameConsoleJournalStorage(oldPersist bool, oldJournal *domainjournal.Journal, oldDir string, nextJournal *domainjournal.Journal, nextDir string) bool {
	if !oldPersist {
		return false
	}
	oldDir = cleanOptionalPath(oldDir)
	nextDir = cleanOptionalPath(nextDir)
	if oldDir != "" && nextDir != "" {
		return oldDir == nextDir
	}
	return oldJournal != nil && oldJournal == nextJournal
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
	info.SteerTargetTaskID = strings.TrimSpace(info.SteerTargetTaskID)
	if info.TopicID == "" {
		info.TopicID = ConsoleDefaultTopicID
	}
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now().UTC()
	} else {
		info.CreatedAt = info.CreatedAt.UTC()
	}
	info.Status, _ = taskdomain.ParseTaskStatus(string(info.Status))
	return info
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
