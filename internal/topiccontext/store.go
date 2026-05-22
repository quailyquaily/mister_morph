package topiccontext

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
)

const storeVersion = 1

type Item struct {
	ConversationKey          string  `json:"conversation_key"`
	TopicID                  string  `json:"topic_id,omitempty"`
	Runtime                  string  `json:"runtime,omitempty"`
	Model                    string  `json:"model,omitempty"`
	NormalizedModel          string  `json:"normalized_model,omitempty"`
	ContextWindowTokens      int64   `json:"context_window_tokens,omitempty"`
	ContextWindowSource      string  `json:"context_window_source,omitempty"`
	UsedInputTokens          int64   `json:"used_input_tokens,omitempty"`
	CachedInputTokens        int64   `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens,omitempty"`
	UsageRatio               float64 `json:"usage_ratio,omitempty"`
	LastRunID                string  `json:"last_run_id,omitempty"`
	LastOriginEventID        string  `json:"last_origin_event_id,omitempty"`
	UpdatedAt                string  `json:"updated_at,omitempty"`
}

type UsageSample struct {
	RunID                    string
	OriginEventID            string
	Scene                    string
	Provider                 string
	APIBase                  string
	Model                    string
	ContextWindowTokens      int64
	InputTokens              int64
	CachedInputTokens        int64
	CacheCreationInputTokens int64
	UpdatedAt                time.Time
}

type Store struct {
	path     string
	lockPath string
	mu       sync.Mutex
}

type storeFile struct {
	Version int             `json:"version"`
	Items   map[string]Item `json:"items"`
}

func NewStore(path string) *Store {
	path = strings.TrimSpace(path)
	lockPath := ""
	if path != "" {
		lockPath = path + ".lck"
	}
	return &Store{path: path, lockPath: lockPath}
}

func RuntimeStore() *Store {
	return NewStore(statepaths.TopicContextPath())
}

func ObserveUsage(ctx context.Context, sample UsageSample) {
	scope, ok := ScopeFromContext(ctx)
	if !ok || !shouldTrackScene(sample.Scene) || sample.InputTokens <= 0 {
		return
	}
	if err := RuntimeStore().UpdateFromSample(scope, sample); err != nil {
		return
	}
}

func (s *Store) Get(conversationKey string) (Item, bool, error) {
	conversationKey = normalizeConversationKey(conversationKey)
	if s == nil || conversationKey == "" {
		return Item{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readLocked()
	if err != nil {
		return Item{}, false, err
	}
	item, ok := data.Items[conversationKey]
	if !ok {
		return Item{}, false, nil
	}
	return item, true, nil
}

func (s *Store) UpdateFromSample(scope Scope, sample UsageSample) error {
	scope.ConversationKey = normalizeConversationKey(scope.ConversationKey)
	if s == nil || scope.ConversationKey == "" || sample.InputTokens <= 0 {
		return nil
	}
	item := itemFromSample(scope, sample)
	return s.withMutationLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		data.Items[scope.ConversationKey] = item
		return s.writeLocked(data)
	})
}

func (s *Store) withMutationLock(fn func() error) error {
	if s == nil || fn == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.lockPath) == "" {
		return fn()
	}
	return fsstore.WithLock(context.Background(), s.lockPath, fn)
}

func (s *Store) readLocked() (storeFile, error) {
	data := storeFile{
		Version: storeVersion,
		Items:   map[string]Item{},
	}
	if s == nil || strings.TrimSpace(s.path) == "" {
		return data, nil
	}
	var persisted storeFile
	found, err := fsstore.ReadJSON(strings.TrimSpace(s.path), &persisted)
	if err != nil {
		return data, err
	}
	if !found {
		return data, nil
	}
	if persisted.Version <= 0 {
		persisted.Version = storeVersion
	}
	for key, item := range persisted.Items {
		key = normalizeConversationKey(key)
		item.ConversationKey = normalizeConversationKey(item.ConversationKey)
		if item.ConversationKey == "" {
			item.ConversationKey = key
		}
		if key == "" || item.ConversationKey == "" {
			continue
		}
		item.TopicID = strings.TrimSpace(item.TopicID)
		item.Runtime = strings.TrimSpace(item.Runtime)
		item.Model = strings.TrimSpace(item.Model)
		item.NormalizedModel = strings.TrimSpace(item.NormalizedModel)
		item.ContextWindowSource = strings.TrimSpace(item.ContextWindowSource)
		item.LastRunID = strings.TrimSpace(item.LastRunID)
		item.LastOriginEventID = strings.TrimSpace(item.LastOriginEventID)
		item.UpdatedAt = strings.TrimSpace(item.UpdatedAt)
		if item.ContextWindowTokens > 0 && item.UsedInputTokens > 0 {
			item.UsageRatio = float64(item.UsedInputTokens) / float64(item.ContextWindowTokens)
		} else {
			item.UsageRatio = 0
		}
		data.Items[key] = item
	}
	return data, nil
}

func (s *Store) writeLocked(data storeFile) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	if data.Items == nil {
		data.Items = map[string]Item{}
	}
	data.Version = storeVersion
	return fsstore.WriteJSONAtomic(strings.TrimSpace(s.path), data, fsstore.FileOptions{})
}

func itemFromSample(scope Scope, sample UsageSample) Item {
	windowTokens := sample.ContextWindowTokens
	windowSource := "config"
	normalizedModel := ""
	if entry, ok := llm.ResolveModelContextWindow(sample.Model); ok {
		normalizedModel = entry.NormalizedModel
		if windowTokens <= 0 {
			windowTokens = entry.ContextWindowTokens
			windowSource = "builtin"
		}
	} else if windowTokens <= 0 {
		windowSource = "unknown"
	}
	if normalizedModel == "" {
		normalizedModel = normalizeSampleModel(sample.Model)
	}
	ratio := 0.0
	if windowTokens > 0 {
		ratio = float64(sample.InputTokens) / float64(windowTokens)
	}
	updatedAt := sample.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return Item{
		ConversationKey:          normalizeConversationKey(scope.ConversationKey),
		TopicID:                  strings.TrimSpace(scope.TopicID),
		Runtime:                  strings.TrimSpace(scope.Runtime),
		Model:                    strings.TrimSpace(sample.Model),
		NormalizedModel:          normalizedModel,
		ContextWindowTokens:      windowTokens,
		ContextWindowSource:      windowSource,
		UsedInputTokens:          sample.InputTokens,
		CachedInputTokens:        sample.CachedInputTokens,
		CacheCreationInputTokens: sample.CacheCreationInputTokens,
		UsageRatio:               ratio,
		LastRunID:                strings.TrimSpace(sample.RunID),
		LastOriginEventID:        strings.TrimSpace(sample.OriginEventID),
		UpdatedAt:                updatedAt.UTC().Format(time.RFC3339),
	}
}

func shouldTrackScene(scene string) bool {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return true
	}
	return strings.HasSuffix(scene, ".loop")
}

func normalizeConversationKey(key string) string {
	return strings.TrimSpace(key)
}

func normalizeSampleModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(model, "models/") {
		return strings.TrimPrefix(model, "models/")
	}
	return model
}

func CommandFunc(conversationKey string) func() (string, error) {
	conversationKey = normalizeConversationKey(conversationKey)
	return func() (string, error) {
		return RenderCommandText(conversationKey)
	}
}

func RenderCommandText(conversationKey string) (string, error) {
	item, ok, err := RuntimeStore().Get(conversationKey)
	if err != nil {
		return "", err
	}
	if !ok {
		return "No context usage recorded for this topic yet.", nil
	}
	return RenderItemText(item), nil
}

func RenderItemText(item Item) string {
	model := strings.TrimSpace(item.Model)
	if model == "" {
		model = "unknown"
	}
	if item.ContextWindowTokens <= 0 {
		return fmt.Sprintf("Context\nmodel: %s\nwindow: unknown\nused: %s input tokens\nupdated: %s", model, formatInt64(item.UsedInputTokens), item.UpdatedAt)
	}
	return fmt.Sprintf(
		"Context\nmodel: %s\nwindow: %s tokens (%s)\nused: %s input tokens\ncached: %s input tokens\nratio: %.1f%%\nupdated: %s",
		model,
		formatInt64(item.ContextWindowTokens),
		firstNonEmpty(item.ContextWindowSource, "unknown"),
		formatInt64(item.UsedInputTokens),
		formatInt64(item.CachedInputTokens),
		item.UsageRatio*100,
		item.UpdatedAt,
	)
}

func formatInt64(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return sign + s
	}
	var out []byte
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	out = append(out, s[:first]...)
	for i := first; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return sign + string(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
