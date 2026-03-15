package daemonruntime

import (
	"sort"
	"strings"
	"sync"
)

const defaultMaxItems = 1000

type TaskListOptions struct {
	Status  TaskStatus
	Limit   int
	TopicID string
}

// TaskReader is the minimal read API required by the daemon HTTP routes.
type TaskReader interface {
	List(opts TaskListOptions) []TaskInfo
	Get(id string) (*TaskInfo, bool)
}

type TaskUpdater interface {
	Update(id string, fn func(*TaskInfo))
}

type TaskWriter interface {
	Upsert(info TaskInfo)
	TaskUpdater
}

type TaskView interface {
	TaskReader
	TaskWriter
}

type TaskEventRecorder interface {
	RecordTaskUpsert(info TaskInfo, trigger TaskTrigger) error
	RecordTaskUpdate(id string, trigger TaskTrigger, fn func(*TaskInfo)) error
}

func RecordTaskUpsert(store TaskWriter, info TaskInfo, trigger TaskTrigger) error {
	if store == nil {
		return nil
	}
	if recorder, ok := store.(TaskEventRecorder); ok {
		return recorder.RecordTaskUpsert(info, trigger)
	}
	store.Upsert(info)
	return nil
}

func RecordTaskUpdate(store TaskUpdater, id string, trigger TaskTrigger, fn func(*TaskInfo)) error {
	if store == nil || fn == nil {
		return nil
	}
	if recorder, ok := store.(TaskEventRecorder); ok {
		return recorder.RecordTaskUpdate(id, trigger, fn)
	}
	store.Update(id, fn)
	return nil
}

type TopicReader interface {
	ListTopics() []TopicInfo
}

type TopicDeleter interface {
	DeleteTopic(id string) bool
}

// MemoryStore is an in-memory task view used by long-running runtimes.
type MemoryStore struct {
	mu       sync.RWMutex
	items    map[string]TaskInfo
	maxItems int
}

func NewMemoryStore(maxItems int) *MemoryStore {
	if maxItems <= 0 {
		maxItems = defaultMaxItems
	}
	return &MemoryStore{
		items:    make(map[string]TaskInfo),
		maxItems: maxItems,
	}
}

func (s *MemoryStore) Upsert(info TaskInfo) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(info.ID)
	if id == "" {
		return
	}
	info.ID = id
	info.Status, _ = ParseTaskStatus(string(info.Status))

	s.mu.Lock()
	s.items[id] = info
	s.pruneLocked()
	s.mu.Unlock()
}

func (s *MemoryStore) Update(id string, fn func(*TaskInfo)) {
	if s == nil || fn == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.mu.Lock()
	item, ok := s.items[id]
	if ok {
		fn(&item)
		item.ID = id
		item.Status, _ = ParseTaskStatus(string(item.Status))
		s.items[id] = item
	}
	s.mu.Unlock()
}

func (s *MemoryStore) Get(id string) (*TaskInfo, bool) {
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

func (s *MemoryStore) List(opts TaskListOptions) []TaskInfo {
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

func (s *MemoryStore) pruneLocked() {
	if s.maxItems <= 0 || len(s.items) <= s.maxItems {
		return
	}
	all := make([]TaskInfo, 0, len(s.items))
	for _, item := range s.items {
		all = append(all, item)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	keep := make(map[string]TaskInfo, s.maxItems)
	for i := 0; i < len(all) && i < s.maxItems; i++ {
		keep[all[i].ID] = all[i]
	}
	s.items = keep
}
