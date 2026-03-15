package daemonruntime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const (
	consoleTaskEventTypeUpsert = "task_upsert"
	consoleTopicFileVersion    = 1
)

type ConsoleFileStoreOptions struct {
	RootDir          string
	HeartbeatTopicID string
	Persist          bool
}

type ConsoleFileStore struct {
	mu sync.RWMutex

	rootDir          string
	logDir           string
	topicPath        string
	heartbeatTopicID string
	persist          bool

	items    map[string]TaskInfo
	topics   map[string]TopicInfo
	triggers map[string]TaskTrigger
}

type consoleTaskEvent struct {
	Type    string       `json:"type"`
	At      time.Time    `json:"at"`
	Channel string       `json:"channel"`
	Trigger *TaskTrigger `json:"trigger,omitempty"`
	Task    TaskInfo     `json:"task"`
}

type consoleTopicFile struct {
	Version   int         `json:"version"`
	UpdatedAt time.Time   `json:"updated_at"`
	Items     []TopicInfo `json:"items"`
}

func NewConsoleFileStore(opts ConsoleFileStoreOptions) (*ConsoleFileStore, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	if opts.Persist && rootDir == "" {
		return nil, fmt.Errorf("console task store root dir is required")
	}
	heartbeatTopicID := strings.TrimSpace(opts.HeartbeatTopicID)
	if heartbeatTopicID == "" {
		heartbeatTopicID = "_heartbeat"
	}
	s := &ConsoleFileStore{
		rootDir:          filepath.Clean(rootDir),
		logDir:           filepath.Join(filepath.Clean(rootDir), "log"),
		topicPath:        filepath.Join(filepath.Clean(rootDir), "topic.json"),
		heartbeatTopicID: heartbeatTopicID,
		persist:          opts.Persist,
		items:            map[string]TaskInfo{},
		topics:           map[string]TopicInfo{},
		triggers:         map[string]TaskTrigger{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ConsoleFileStore) HeartbeatTopicID() string {
	if s == nil {
		return "_heartbeat"
	}
	return s.heartbeatTopicID
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
	s.topics[topic.ID] = topic
	if err := s.persistTopicsLocked(now); err != nil {
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
	s.ensureTopicLocked(info.TopicID, topicTitle, now, true)
	s.items[info.ID] = info
	if hasTaskTrigger(trigger) {
		s.triggers[info.ID] = normalizeTaskTrigger(trigger)
	}
	if err := s.appendTaskEventLocked(info, now, s.triggerForTaskLocked(info.ID, trigger)); err != nil {
		return err
	}
	return s.persistTopicsLocked(now)
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
	s.items[id] = item
	if hasTaskTrigger(trigger) {
		s.triggers[id] = normalizeTaskTrigger(trigger)
	}
	return s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, trigger))
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

func (s *ConsoleFileStore) List(opts TaskListOptions) []TaskInfo {
	if s == nil {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
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
		if topicID == "" && strings.TrimSpace(item.TopicID) == s.heartbeatTopicID {
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
	s.topics[id] = topic
	if err := s.persistTopicsLocked(now); err != nil {
		return false
	}
	return true
}

func (s *ConsoleFileStore) SetTopicTitle(id string, title string) error {
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
	if topic.Title == title {
		return nil
	}
	topic.Title = title
	topic.UpdatedAt = now
	s.topics[id] = normalizeTopicInfo(topic)
	return s.persistTopicsLocked(now)
}

func (s *ConsoleFileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.persist {
		return nil
	}

	if err := s.loadTopicsLocked(); err != nil {
		return err
	}
	if err := s.replayLogsLocked(); err != nil {
		return err
	}
	s.pruneUnusedDefaultTopicLocked()
	now := time.Now().UTC()
	if err := s.persistTopicsLocked(now); err != nil {
		return err
	}
	return s.recoverNonTerminalTasksLocked(now)
}

func (s *ConsoleFileStore) loadTopicsLocked() error {
	var payload consoleTopicFile
	ok, err := fsstore.ReadJSON(s.topicPath, &payload)
	if err != nil || !ok {
		return err
	}
	for _, topic := range payload.Items {
		if strings.TrimSpace(topic.ID) == "" {
			continue
		}
		s.topics[strings.TrimSpace(topic.ID)] = normalizeTopicInfo(topic)
	}
	return nil
}

func (s *ConsoleFileStore) replayLogsLocked() error {
	entries, err := os.ReadDir(s.logDir)
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
		if err := s.replayLogFileLocked(filepath.Join(s.logDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConsoleFileStore) replayLogFileLocked(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event consoleTaskEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("decode console task event %s: %w", path, err)
		}
		if event.Type != consoleTaskEventTypeUpsert {
			continue
		}
		info := normalizeConsoleTaskInfo(event.Task)
		if info.ID == "" {
			continue
		}
		s.items[info.ID] = info
		if event.Trigger != nil && hasTaskTrigger(*event.Trigger) {
			s.triggers[info.ID] = normalizeTaskTrigger(*event.Trigger)
		}
		s.ensureTopicLocked(info.TopicID, "", info.CreatedAt, false)
	}
	return scanner.Err()
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
		s.items[id] = item
		if err := s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, TaskTrigger{})); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConsoleFileStore) appendTaskEventLocked(info TaskInfo, now time.Time, trigger TaskTrigger) error {
	if !s.persist {
		return nil
	}
	path := filepath.Join(s.logDir, fmt.Sprintf("%s_%s.jsonl", now.Format("2006-01-02"), consoleTopicKey(info.TopicID)))
	writer, err := fsstore.NewJSONLWriter(path, fsstore.JSONLOptions{
		RotateMaxBytes: 1 << 60,
		SyncEachWrite:  true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	event := consoleTaskEvent{
		Type:    consoleTaskEventTypeUpsert,
		At:      now,
		Channel: "console",
		Task:    info,
	}
	if hasTaskTrigger(trigger) {
		trigger = normalizeTaskTrigger(trigger)
		event.Trigger = &trigger
	}
	return writer.AppendJSON(event)
}

func (s *ConsoleFileStore) persistTopicsLocked(now time.Time) error {
	if !s.persist {
		return nil
	}
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
	return fsstore.WriteJSONAtomic(s.topicPath, consoleTopicFile{
		Version:   consoleTopicFileVersion,
		UpdatedAt: now,
		Items:     topics,
	}, fsstore.FileOptions{})
}

func (s *ConsoleFileStore) ensureTopicLocked(topicID string, title string, now time.Time, touch bool) TopicInfo {
	topicID = strings.TrimSpace(topicID)
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
		if topic.ID == s.heartbeatTopicID && topic.Title == "" {
			topic.Title = ConsoleHeartbeatTopicTitle
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
	if topic.ID == s.heartbeatTopicID && topic.Title == "" {
		topic.Title = ConsoleHeartbeatTopicTitle
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
	now = nonZeroTime(now)
	return fmt.Sprintf("topic_%d_%x", now.UnixNano(), rand.Uint64())
}

func normalizeConsoleTaskInfo(info TaskInfo) TaskInfo {
	info.ID = strings.TrimSpace(info.ID)
	info.Task = strings.TrimSpace(info.Task)
	info.Model = strings.TrimSpace(info.Model)
	info.Timeout = strings.TrimSpace(info.Timeout)
	info.Error = strings.TrimSpace(info.Error)
	info.TopicID = strings.TrimSpace(info.TopicID)
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
		Source: strings.TrimSpace(trigger.Source),
		Event:  strings.TrimSpace(trigger.Event),
		Ref:    strings.TrimSpace(trigger.Ref),
	}
}

func hasTaskTrigger(trigger TaskTrigger) bool {
	return strings.TrimSpace(trigger.Source) != "" ||
		strings.TrimSpace(trigger.Event) != "" ||
		strings.TrimSpace(trigger.Ref) != ""
}

func normalizeTopicInfo(topic TopicInfo) TopicInfo {
	topic.ID = strings.TrimSpace(topic.ID)
	topic.Title = strings.TrimSpace(topic.Title)
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
