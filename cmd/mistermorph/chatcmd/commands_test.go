package chatcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmselect"
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
	for _, want := range []string{"/ctx", "/help", "/models", "/skills", "/workspace", "/reset"} {
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
