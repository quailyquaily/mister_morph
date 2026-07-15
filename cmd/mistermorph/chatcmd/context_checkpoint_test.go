package chatcmd

import (
	"context"
	"errors"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/llm"
)

type chatCheckpointTestStore struct {
	checkpoint agent.ContextCheckpoint
	found      bool
	err        error
}

func (s chatCheckpointTestStore) Load(context.Context) (agent.ContextCheckpoint, bool, error) {
	return s.checkpoint, s.found, s.err
}

func (chatCheckpointTestStore) Save(context.Context, int64, agent.ContextCheckpoint) error {
	return errors.New("unexpected Save call")
}

func (chatCheckpointTestStore) Delete(context.Context, int64) error {
	return errors.New("unexpected Delete call")
}

func TestReconcileChatHistoryWithCheckpointFiltersCoveredMessages(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new question"},
	}
	boundaries := []string{"turn-1-user", "turn-1-assistant", "turn-2-user"}
	store := chatCheckpointTestStore{
		found: true,
		checkpoint: agent.ContextCheckpoint{
			CoveredThrough: "turn-1-assistant",
		},
	}

	gotHistory, gotBoundaries, err := reconcileChatHistoryWithCheckpoint(
		context.Background(), store, history, boundaries, "",
	)
	if err != nil {
		t.Fatalf("reconcile history: %v", err)
	}
	if len(gotHistory) != 1 || gotHistory[0].Content != "new question" {
		t.Fatalf("history = %#v", gotHistory)
	}
	if len(gotBoundaries) != 1 || gotBoundaries[0] != "turn-2-user" {
		t.Fatalf("boundaries = %#v", gotBoundaries)
	}
}

func TestReconcileChatHistoryWithCheckpointClearsHistoryWhenCurrentInputWasCompacted(t *testing.T) {
	store := chatCheckpointTestStore{
		found: true,
		checkpoint: agent.ContextCheckpoint{
			CoveredThrough: "current-user",
		},
	}

	history, boundaries, err := reconcileChatHistoryWithCheckpoint(
		context.Background(),
		store,
		[]llm.Message{{Role: "user", Content: "previous question"}},
		[]string{"previous-user"},
		"current-user",
	)
	if err != nil {
		t.Fatalf("reconcile history: %v", err)
	}
	if history != nil || boundaries != nil {
		t.Fatalf("history = %#v, boundaries = %#v, want nil", history, boundaries)
	}
}
