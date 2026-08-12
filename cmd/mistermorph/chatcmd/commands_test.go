package chatcmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestChatTimeoutContextTreatsNonPositiveTimeoutAsUnlimited(t *testing.T) {
	ctx, cancel := chatTimeoutContext(context.Background(), 0)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("zero-timeout context ended immediately: %v", ctx.Err())
	case <-time.After(10 * time.Millisecond):
	}
}

func TestChatRuntimeRegistryIncludesSharedCommands(t *testing.T) {
	sess := &chatSession{
		subjectID:    "cli_test",
		sessionStore: llmselect.NewStore(),
	}
	history := make([]llm.Message, 0)
	boundaries := make([]string, 0)
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history, &boundaries)

	res, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || res == nil {
		t.Fatalf("expected /help handled")
	}
	for _, want := range []string{"/approve", "/ctx", "/deny", "/help", "/models", "/skills", "/think", "/workspace", "/reset", "/stop"} {
		if !strings.Contains(res.Reply, want) {
			t.Fatalf("/help reply missing %q: %q", want, res.Reply)
		}
	}
}

func TestChatResetDeletesHistoryAndCheckpoint(t *testing.T) {
	root := t.TempDir()
	sess := &chatSession{
		subjectID:    "cli_reset",
		fileStateDir: root,
		sessionStore: llmselect.NewStore(),
	}
	store, err := contextcheckpoint.NewFileStore(root, sess.conversationKey())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Save(context.Background(), 0, agent.ContextCheckpoint{
		Version:  1,
		Revision: 1,
		Message:  llm.Message{Role: "user", Content: "checkpoint"},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	history := []llm.Message{{Role: "user", Content: "old"}}
	boundaries := []string{"old-boundary"}
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history, &boundaries)

	result, handled, err := reg.Dispatch(context.Background(), "/reset")
	if err != nil {
		t.Fatalf("/reset error = %v", err)
	}
	if !handled || result == nil || result.Reply != "Session reset." {
		t.Fatalf("/reset result = %#v handled = %v", result, handled)
	}
	if len(history) != 0 || len(boundaries) != 0 {
		t.Fatalf("history/boundaries after reset = %#v / %#v", history, boundaries)
	}
	if _, found, err := store.Load(context.Background()); err != nil || found {
		t.Fatalf("Load() after reset found = %v, error = %v", found, err)
	}
}

func TestChatSessionConversationKeyUsesCurrentProjectScope(t *testing.T) {
	sess := &chatSession{subjectID: "cli_1234"}
	if got := sess.conversationKey(); got != "chat:cli_1234" {
		t.Fatalf("conversationKey() = %q, want %q", got, "chat:cli_1234")
	}
}

func TestShouldSendChatStopFeedbackSkipsAcknowledgedStop(t *testing.T) {
	result := chatTurnResult{
		turn:  &activeChatTurn{stopAcknowledged: true},
		err:   context.Canceled,
		cause: runtimecontrol.ErrStoppedByUser,
	}
	if shouldSendChatStopFeedback(result) {
		t.Fatal("shouldSendChatStopFeedback() = true, want false after immediate stop ack")
	}
}

func TestShouldSendChatStopFeedbackAllowsUnacknowledgedStop(t *testing.T) {
	result := chatTurnResult{
		turn:  &activeChatTurn{},
		err:   context.Canceled,
		cause: runtimecontrol.ErrStoppedByUser,
	}
	if !shouldSendChatStopFeedback(result) {
		t.Fatal("shouldSendChatStopFeedback() = false, want true when no immediate stop ack was sent")
	}
}

func TestActiveChatTurnRequestStopClosesSteerQueue(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	queue := runtimecontrol.NewSteerQueue(0)
	turn := &activeChatTurn{
		cancel:     cancel,
		steerQueue: queue,
	}
	turn.requestStop()

	if !turn.stopAcknowledged {
		t.Fatal("stopAcknowledged = false, want true")
	}
	if !errors.Is(context.Cause(ctx), runtimecontrol.ErrStoppedByUser) {
		t.Fatalf("context cause = %v, want ErrStoppedByUser", context.Cause(ctx))
	}
	if _, err := queue.Push("late input"); err == nil {
		t.Fatal("Push() after requestStop() error = nil")
	}
}
