package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestRunLocalCheckpointStoreRevisionAndDelete(t *testing.T) {
	store := newRunLocalCheckpointStore()
	checkpoint := ContextCheckpoint{
		Version: 1, Revision: 1, Message: llm.Message{Role: "user", Content: "checkpoint"},
	}
	if err := store.Save(context.Background(), 0, checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if err := store.Save(context.Background(), 0, checkpoint); !errors.Is(err, ErrContextCheckpointRevisionConflict) {
		t.Fatalf("stale save error = %v", err)
	}
	got, ok, err := store.Load(context.Background())
	if err != nil || !ok || !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("load checkpoint = %+v ok:%v err:%v", got, ok, err)
	}
	if err := store.Delete(context.Background(), 1); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	if _, ok, err := store.Load(context.Background()); err != nil || ok {
		t.Fatalf("load after delete = ok:%v err:%v", ok, err)
	}
}

func TestInsertLoadedCheckpointPlacesItAfterFixedMessages(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "meta"},
		{Role: "user", Content: "history"},
		{Role: "user", Content: "current"},
	}
	checkpoint := ContextCheckpoint{Message: llm.Message{Role: "user", Content: "checkpoint"}}
	got, err := insertLoadedCheckpoint(messages, 2, checkpoint)
	if err != nil {
		t.Fatalf("insert checkpoint: %v", err)
	}
	want := []llm.Message{messages[0], messages[1], checkpoint.Message, messages[2], messages[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestCoveredThroughForSelectionUsesNewestSelectedBoundary(t *testing.T) {
	boundaries := map[int]string{
		2: "history-boundary",
		4: "current-boundary",
	}
	got := coveredThroughForSelection(transcriptSelection{Start: 2, End: 6}, boundaries)
	if got != "current-boundary" {
		t.Fatalf("boundary = %q, want current-boundary", got)
	}
	got = coveredThroughForSelection(transcriptSelection{Start: 2, End: 4}, boundaries)
	if got != "history-boundary" {
		t.Fatalf("boundary = %q, want history-boundary", got)
	}
}
