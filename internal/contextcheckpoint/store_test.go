package contextcheckpoint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestFileStoreSaveLoadDeleteAndRevision(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), "telegram:chat:one")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	checkpoint := agent.ContextCheckpoint{
		Version:         1,
		Revision:        1,
		Message:         llm.Message{Role: "user", Content: "checkpoint"},
		CoveredThrough:  "boundary:one",
		SourceModel:     "test-model",
		SourceRunID:     "run-one",
		CompactionCount: 1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.Save(context.Background(), 0, checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	got, ok, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !ok {
		t.Fatal("checkpoint not found")
	}
	if got.Revision != 1 || got.Message.Content != "checkpoint" || got.CoveredThrough != "boundary:one" {
		t.Fatalf("checkpoint = %+v", got)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatalf("stat checkpoint file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint mode = %o, want 600", info.Mode().Perm())
	}

	stale := checkpoint
	stale.Revision = 2
	if err := store.Save(context.Background(), 0, stale); !errors.Is(err, agent.ErrContextCheckpointRevisionConflict) {
		t.Fatalf("stale save error = %v, want revision conflict", err)
	}
	if err := store.Delete(context.Background(), 0); !errors.Is(err, agent.ErrContextCheckpointRevisionConflict) {
		t.Fatalf("stale delete error = %v, want revision conflict", err)
	}
	if err := store.Delete(context.Background(), 1); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	if _, ok, err := store.Load(context.Background()); err != nil || ok {
		t.Fatalf("load after delete = ok:%v err:%v", ok, err)
	}
}

func TestFileStoreRejectsInvalidCheckpointBeforeWrite(t *testing.T) {
	valid := agent.ContextCheckpoint{
		Version:         1,
		Revision:        1,
		Message:         llm.Message{Role: "user", Content: "checkpoint"},
		CompactionCount: 1,
	}
	tests := []struct {
		name   string
		mutate func(*agent.ContextCheckpoint)
	}{
		{name: "non-positive version", mutate: func(checkpoint *agent.ContextCheckpoint) { checkpoint.Version = 0 }},
		{name: "non-user message", mutate: func(checkpoint *agent.ContextCheckpoint) { checkpoint.Message.Role = "assistant" }},
		{name: "empty message", mutate: func(checkpoint *agent.ContextCheckpoint) { checkpoint.Message.Content = " " }},
		{name: "negative compaction count", mutate: func(checkpoint *agent.ContextCheckpoint) { checkpoint.CompactionCount = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewFileStore(t.TempDir(), "invalid:"+tt.name)
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			checkpoint := valid
			tt.mutate(&checkpoint)
			if err := store.Save(context.Background(), 0, checkpoint); err == nil {
				t.Fatal("Save() error = nil")
			}
			if _, found, err := store.Load(context.Background()); err != nil || found {
				t.Fatalf("Load() after rejected save = found:%v err:%v", found, err)
			}
		})
	}
}

func TestFileStoreDoesNotSaveAfterContextCancellation(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), "telegram:reset-race")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = store.Save(ctx, 0, agent.ContextCheckpoint{
		Version:  1,
		Revision: 1,
		Message:  llm.Message{Role: "user", Content: "checkpoint"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v, want context.Canceled", err)
	}
	if _, found, err := store.Load(context.Background()); err != nil || found {
		t.Fatalf("Load() after canceled save = found:%v err:%v", found, err)
	}
}

func TestFileStoreLoadRejectsInvalidStoredCheckpoint(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), "telegram:invalid-stored")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	raw, err := json.Marshal(agent.ContextCheckpoint{
		Version:         1,
		Revision:        1,
		Message:         llm.Message{Role: "user", Content: "checkpoint"},
		CompactionCount: -1,
	})
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	if err := os.WriteFile(store.path, raw, 0o600); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	if _, found, err := store.Load(context.Background()); err == nil || found {
		t.Fatalf("Load() invalid checkpoint = found:%v err:%v", found, err)
	}
}

func TestFileStoreSeparatesConversationKeys(t *testing.T) {
	root := t.TempDir()
	one, err := NewFileStore(root, "slack:one")
	if err != nil {
		t.Fatalf("new first store: %v", err)
	}
	two, err := NewFileStore(root, "slack:two")
	if err != nil {
		t.Fatalf("new second store: %v", err)
	}
	if one.path == two.path {
		t.Fatalf("store paths collide: %q", one.path)
	}
	if err := one.Save(context.Background(), 0, agent.ContextCheckpoint{
		Version:  1,
		Revision: 1,
		Message:  llm.Message{Role: "user", Content: "one"},
	}); err != nil {
		t.Fatalf("save first checkpoint: %v", err)
	}
	if _, ok, err := two.Load(context.Background()); err != nil || ok {
		t.Fatalf("second conversation leaked first checkpoint: ok:%v err:%v", ok, err)
	}
}

func TestFileStoreSerializesConcurrentRevisionUpdates(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), "line:concurrent")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(context.Background(), 0, agent.ContextCheckpoint{
		Version: 1, Revision: 1, Message: llm.Message{Role: "user", Content: "base"},
	}); err != nil {
		t.Fatalf("save base checkpoint: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, content := range []string{"first", "second"} {
		content := content
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Save(context.Background(), 1, agent.ContextCheckpoint{
				Version: 1, Revision: 2, Message: llm.Message{Role: "user", Content: content},
			})
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, agent.ErrContextCheckpointRevisionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent save error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d conflicts = %d, want 1/1", successes, conflicts)
	}
}

func TestPrepareHistoryFiltersCoveredItemsAndBuildsBoundaries(t *testing.T) {
	root := t.TempDir()
	conversationKey := "lark:history"
	history := []chathistory.ChatHistoryItem{
		{Channel: chathistory.ChannelLark, Kind: chathistory.KindInboundUser, MessageID: "old", Text: "old"},
		{Channel: chathistory.ChannelLark, Kind: chathistory.KindOutboundAgent, Text: "answer"},
		{Channel: chathistory.ChannelLark, Kind: chathistory.KindInboundUser, MessageID: "new", Text: "new"},
	}
	store, err := NewFileStore(root, conversationKey)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(context.Background(), 0, agent.ContextCheckpoint{
		Version:        1,
		Revision:       1,
		Message:        llm.Message{Role: "user", Content: "checkpoint"},
		CoveredThrough: chathistory.BoundaryForItem(history[1]),
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	current := chathistory.ChatHistoryItem{Channel: chathistory.ChannelLark, Kind: chathistory.KindInboundUser, MessageID: "current", Text: "current"}
	prepared, err := PrepareHistory(context.Background(), root, conversationKey, history, current)
	if err != nil {
		t.Fatalf("prepare history: %v", err)
	}
	if len(prepared.History) != 1 || prepared.History[0].MessageID != "new" {
		t.Fatalf("prepared history = %+v", prepared)
	}
	if prepared.HistoryBoundary != chathistory.BoundaryForItem(history[2]) {
		t.Fatalf("history boundary = %q", prepared.HistoryBoundary)
	}
	if prepared.CurrentMessageBoundary != chathistory.BoundaryForItem(current) {
		t.Fatalf("current boundary = %q", prepared.CurrentMessageBoundary)
	}
	if err := Reset(context.Background(), root, conversationKey); err != nil {
		t.Fatalf("reset checkpoint: %v", err)
	}
	if _, ok, err := store.Load(context.Background()); err != nil || ok {
		t.Fatalf("load after reset = ok:%v err:%v", ok, err)
	}
	prepared, err = PrepareHistory(context.Background(), root, conversationKey, history, current)
	if err != nil {
		t.Fatalf("prepare history without checkpoint: %v", err)
	}
	if len(prepared.History) != len(history) {
		t.Fatalf("history without checkpoint = %+v, want all %d items", prepared.History, len(history))
	}
}

func TestFilterMessageHistoryKeepsOnlyMessagesAfterCoveredBoundary(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new question"},
	}
	boundaries := []string{"turn-1-user", "turn-1-assistant", "turn-2-user"}

	filteredMessages, filteredBoundaries := FilterMessageHistory(messages, boundaries, "turn-1-assistant")
	if len(filteredMessages) != 1 || filteredMessages[0].Content != "new question" {
		t.Fatalf("filtered messages = %#v", filteredMessages)
	}
	if len(filteredBoundaries) != 1 || filteredBoundaries[0] != "turn-2-user" {
		t.Fatalf("filtered boundaries = %#v", filteredBoundaries)
	}

	unmatchedMessages, unmatchedBoundaries := FilterMessageHistory(messages, boundaries, "unknown")
	if len(unmatchedMessages) != len(messages) || len(unmatchedBoundaries) != len(boundaries) {
		t.Fatalf("unknown boundary changed history: messages=%#v boundaries=%#v", unmatchedMessages, unmatchedBoundaries)
	}
}
