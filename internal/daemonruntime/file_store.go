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

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/viper"
)

const fileTaskEventTypeUpsert = "task_upsert"

type FileTaskStoreOptions struct {
	RootDir        string
	Target         string
	Persist        bool
	RotateMaxBytes int64
}

type FileTaskStore struct {
	mu sync.RWMutex

	rootDir        string
	logDir         string
	logPath        string
	target         string
	persist        bool
	rotateMaxBytes int64

	items    map[string]TaskInfo
	triggers map[string]TaskTrigger
}

type fileTaskEvent struct {
	Type    string       `json:"type"`
	At      time.Time    `json:"at"`
	Target  string       `json:"target,omitempty"`
	Trigger *TaskTrigger `json:"trigger,omitempty"`
	Task    TaskInfo     `json:"task"`
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
		logDir:         filepath.Join(filepath.Clean(rootDir), "log"),
		logPath:        filepath.Join(filepath.Clean(rootDir), "log", "tasks.jsonl"),
		target:         target,
		persist:        opts.Persist,
		rotateMaxBytes: opts.RotateMaxBytes,
		items:          map[string]TaskInfo{},
		triggers:       map[string]TaskTrigger{},
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

	s.items[info.ID] = info
	if hasTaskTrigger(trigger) {
		s.triggers[info.ID] = normalizeTaskTrigger(trigger)
	}
	return s.appendTaskEventLocked(info, now, s.triggerForTaskLocked(info.ID, trigger))
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
	s.items[id] = item
	if hasTaskTrigger(trigger) {
		s.triggers[id] = normalizeTaskTrigger(trigger)
	}
	return s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, trigger))
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

func (s *FileTaskStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.persist {
		return nil
	}
	if err := s.replayLogsLocked(); err != nil {
		return err
	}
	return s.recoverNonTerminalTasksLocked(time.Now().UTC())
}

func (s *FileTaskStore) replayLogsLocked() error {
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
		name := entry.Name()
		if !strings.HasPrefix(strings.ToLower(name), "tasks.jsonl") {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "tasks.jsonl" {
			return false
		}
		if names[j] == "tasks.jsonl" {
			return true
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		if err := s.replayLogFileLocked(filepath.Join(s.logDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileTaskStore) replayLogFileLocked(path string) error {
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
		var event fileTaskEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("decode task event %s: %w", path, err)
		}
		if event.Type != fileTaskEventTypeUpsert {
			continue
		}
		info := normalizeFileTaskInfo(event.Task)
		if info.ID == "" {
			continue
		}
		s.items[info.ID] = info
		if event.Trigger != nil && hasTaskTrigger(*event.Trigger) {
			s.triggers[info.ID] = normalizeTaskTrigger(*event.Trigger)
		}
	}
	return scanner.Err()
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
		s.items[id] = item
		if err := s.appendTaskEventLocked(item, now, s.triggerForTaskLocked(id, TaskTrigger{})); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileTaskStore) appendTaskEventLocked(info TaskInfo, now time.Time, trigger TaskTrigger) error {
	if !s.persist {
		return nil
	}
	writer, err := fsstore.NewJSONLWriter(s.logPath, fsstore.JSONLOptions{
		RotateMaxBytes: s.rotateMaxBytes,
		SyncEachWrite:  true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	event := fileTaskEvent{
		Type:   fileTaskEventTypeUpsert,
		At:     now,
		Target: s.target,
		Task:   info,
	}
	if hasTaskTrigger(trigger) {
		trigger = normalizeTaskTrigger(trigger)
		event.Trigger = &trigger
	}
	return writer.AppendJSON(event)
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
