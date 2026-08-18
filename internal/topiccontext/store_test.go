package topiccontext

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreUpdateFromSampleUsesConfiguredWindow(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "topic_context.json"))
	err := store.UpdateFromSample(Scope{ConversationKey: " console:topic-1 ", TopicID: " topic-1 ", Runtime: " console "}, UsageSample{
		Model:               "openai/gpt-5.5",
		ContextWindowTokens: 100,
		InputTokens:         25,
		UpdatedAt:           time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("UpdateFromSample() error = %v", err)
	}
	item, ok, err := store.Get(" console:topic-1 ")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() found = false, want true")
	}
	if item.ContextWindowTokens != 100 || item.UsageRatio != 0.25 {
		t.Fatalf("item = %+v, want context window 100 and ratio 0.25", item)
	}
	if item.ContextWindowSource != "config" {
		t.Fatalf("context source = %q, want config", item.ContextWindowSource)
	}
	if item.NormalizedModel != "gpt-5.5" {
		t.Fatalf("normalized model = %q, want gpt-5.5", item.NormalizedModel)
	}
}

func TestStoreUpdateFromSampleUsesBuiltinWindow(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "topic_context.json"))
	err := store.UpdateFromSample(Scope{ConversationKey: "k"}, UsageSample{
		Model:       "openai/gpt-5.5",
		InputTokens: 105000,
		UpdatedAt:   time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("UpdateFromSample() error = %v", err)
	}
	item, ok, err := store.Get("k")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() found = false, want true")
	}
	if item.ContextWindowTokens != 1050000 {
		t.Fatalf("context window = %d, want 1050000", item.ContextWindowTokens)
	}
	if item.ContextWindowSource != "builtin" {
		t.Fatalf("context source = %q, want builtin", item.ContextWindowSource)
	}
}

func TestStoreUpdateFromSampleReplacesPreviousUsage(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "topic_context.json"))
	scope := Scope{ConversationKey: "console:topic-1"}
	if err := store.UpdateFromSample(scope, UsageSample{
		Model:               "gpt-5.5",
		ContextWindowTokens: 10000,
		InputTokens:         1000,
		UpdatedAt:           time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("UpdateFromSample() first error = %v", err)
	}
	if err := store.UpdateFromSample(scope, UsageSample{
		Model:               "gpt-5.5",
		ContextWindowTokens: 10000,
		InputTokens:         1300,
		CachedInputTokens:   300,
		UpdatedAt:           time.Unix(2, 0),
	}); err != nil {
		t.Fatalf("UpdateFromSample() second error = %v", err)
	}

	item, ok, err := store.Get("console:topic-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() found = false, want true")
	}
	if item.UsedInputTokens != 1300 {
		t.Fatalf("used input tokens = %d, want 1300", item.UsedInputTokens)
	}
	if item.CachedInputTokens != 300 {
		t.Fatalf("cached input tokens = %d, want 300", item.CachedInputTokens)
	}
	if item.UsageRatio < 0.129 || item.UsageRatio > 0.131 {
		t.Fatalf("usage ratio = %v, want about 0.13", item.UsageRatio)
	}
}

func TestShouldTrackSceneOnlyTracksLoops(t *testing.T) {
	tests := []struct {
		scene string
		want  bool
	}{
		{scene: "console.loop", want: true},
		{scene: "telegram.loop", want: true},
		{scene: "telegram.context_compact", want: false},
		{scene: "settings.benchmark", want: false},
		{scene: "memory.draft", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.scene, func(t *testing.T) {
			if got := shouldTrackScene(tc.scene); got != tc.want {
				t.Fatalf("shouldTrackScene(%q) = %v, want %v", tc.scene, got, tc.want)
			}
		})
	}
}

func TestRenderItemTextUsesMarkdownList(t *testing.T) {
	got := RenderItemText(Item{
		Model:               "gpt-5.5",
		ContextWindowTokens: 1050000,
		UsedInputTokens:     123456,
		CachedInputTokens:   23456,
		UsageRatio:          0.117577,
		UpdatedAt:           "2026-05-22T00:00:00Z",
	})
	want := "**Context**\n\n" +
		"- **Window used:** 11.8%\n" +
		"- **Used input:** 123,456 / 1,050,000 tokens\n" +
		"- **Cached input:** 23,456 tokens\n" +
		"- **Model:** gpt-5.5\n" +
		"- **Updated:** 2026-05-22T00:00:00Z"
	if got != want {
		t.Fatalf("RenderItemText() = %q, want %q", got, want)
	}
}

func TestRenderItemTextHandlesUnknownWindow(t *testing.T) {
	got := RenderItemText(Item{
		UsedInputTokens: 42,
		UpdatedAt:       "2026-05-22T00:00:00Z",
	})
	want := "**Context**\n\n" +
		"- **Window used:** unknown\n" +
		"- **Used input:** 42 tokens\n" +
		"- **Context window:** unknown\n" +
		"- **Model:** unknown\n" +
		"- **Updated:** 2026-05-22T00:00:00Z"
	if got != want {
		t.Fatalf("RenderItemText() = %q, want %q", got, want)
	}
}

func TestStoreConcurrentWritesKeepItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topic_context.json")
	const count = 8
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := NewStore(path)
			errs <- store.UpdateFromSample(Scope{ConversationKey: fmt.Sprintf("console:topic-%d", i)}, UsageSample{
				Model:       "gpt-5.5",
				InputTokens: int64(100 + i),
				UpdatedAt:   time.Unix(int64(i+1), 0),
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("UpdateFromSample() error = %v", err)
		}
	}

	store := NewStore(path)
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("console:topic-%d", i)
		item, ok, err := store.Get(key)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
		if !ok {
			t.Fatalf("Get(%q) found = false, want true", key)
		}
		if item.UsedInputTokens != int64(100+i) {
			t.Fatalf("Get(%q).used = %d, want %d", key, item.UsedInputTokens, 100+i)
		}
	}
}
