package chatcmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestChatRuntimeRegistryIncludesSharedCommands(t *testing.T) {
	sess := &chatSession{
		subjectID:    "cli_test",
		sessionStore: llmselect.NewStore(),
	}
	history := make([]llm.Message, 0)
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history)

	res, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || res == nil {
		t.Fatalf("expected /help handled")
	}
	for _, want := range []string{"/ctx", "/help", "/models", "/skills", "/think", "/workspace", "/reset", "/stop"} {
		if !strings.Contains(res.Reply, want) {
			t.Fatalf("/help reply missing %q: %q", want, res.Reply)
		}
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
