package daemonruntime

import (
	"sort"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/pagination"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

const defaultMaxItems = 1000
const (
	taskListDefaultLimit     = 20
	taskListMaxLimit         = 200
	taskListInternalMaxLimit = taskListMaxLimit + 1
	topicListDefaultLimit    = 100
	topicListMaxLimit        = 200
	topicListInternalMax     = topicListMaxLimit + 1
)

type TaskListOptions = taskdomain.TaskListOptions

// TaskReader is the minimal read API required by the daemon HTTP routes.
type TaskReader = taskdomain.TaskReader
type TaskUpdater = taskdomain.TaskUpdater
type TaskWriter = taskdomain.TaskWriter
type TaskView = taskdomain.TaskView
type TaskEventRecorder = taskdomain.TaskEventRecorder

type TopicReader interface {
	ListTopicsPage(opts TopicListOptions) []TopicInfo
	GetTopic(id string) (*TopicInfo, bool)
}

type TopicListOptions struct {
	Limit  int
	Cursor string
}

type TopicDeleter interface {
	DeleteTopic(id string) (bool, error)
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

func (s *MemoryStore) Upsert(info TaskInfo) error {
	if s == nil {
		return nil
	}
	id := strings.TrimSpace(info.ID)
	if id == "" {
		return nil
	}
	info.ID = id
	info.Status, _ = taskdomain.ParseTaskStatus(string(info.Status))

	s.mu.Lock()
	s.items[id] = info
	s.pruneLocked()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Update(id string, fn func(*TaskInfo)) error {
	if s == nil || fn == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	item, ok := s.items[id]
	if ok {
		fn(&item)
		item.ID = id
		item.Status, _ = taskdomain.ParseTaskStatus(string(item.Status))
		s.items[id] = item
	}
	s.mu.Unlock()
	return nil
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
	if cursor, ok := pagination.ParseKeysetCursor(opts.Cursor); ok && strings.TrimSpace(opts.Cursor) != "" {
		filtered := out[:0]
		for _, item := range out {
			if pagination.FollowsKeysetCursor(item.CreatedAt, item.ID, cursor) {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
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
