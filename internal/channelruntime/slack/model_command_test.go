package slack

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
)

func TestSlackCommandRegistryHandlesHelpAndModel(t *testing.T) {
	var gotModelText string
	reg := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand: func(text string) (string, bool, error) {
			gotModelText = text
			return "model ok", true, nil
		},
		WorkspaceKey: "conv",
	})

	help, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || help == nil {
		t.Fatalf("expected /help handled")
	}
	for _, want := range []string{"/help", "/models", "/think", "/workspace"} {
		if !strings.Contains(help.Reply, want) {
			t.Fatalf("/help reply missing %q: %q", want, help.Reply)
		}
	}

	model, handled, err := reg.Dispatch(context.Background(), "/models set cheap")
	if err != nil {
		t.Fatalf("/models error = %v", err)
	}
	if !handled || model == nil || model.Reply != "model ok" {
		t.Fatalf("unexpected /models result: %#v handled=%v", model, handled)
	}
	if gotModelText != "/models set cheap" {
		t.Fatalf("model text = %q, want %q", gotModelText, "/models set cheap")
	}
}

func TestSlackCtxCompactFallsThroughToTaskRuntime(t *testing.T) {
	handled, err := maybeHandleSlackCommand(
		context.Background(),
		Dependencies{},
		nil,
		nil,
		"slack:conversation",
		slackInboundEvent{ChatType: "private", Text: "/ctx compact"},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("maybeHandleSlackCommand() error = %v", err)
	}
	if handled {
		t.Fatal("/ctx compact was consumed as a synchronous command")
	}
}

func TestSlackContextCommandUsesRuntimeTopicContextPath(t *testing.T) {
	conversationKey := "slack:team-a:channel-a"
	topicPath := filepath.Join(t.TempDir(), "topic_context.json")
	if err := topiccontext.NewStore(topicPath).UpdateFromSample(topiccontext.Scope{ConversationKey: conversationKey}, topiccontext.UsageSample{
		Model:       "slack-captured-model",
		InputTokens: 12,
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("UpdateFromSample() error = %v", err)
	}
	bus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 4,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Component:   "slack-context-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	defer bus.Close()
	got := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(_ context.Context, msg busruntime.BusMessage) error {
		got <- msg
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	handled, err := maybeHandleSlackCommand(
		context.Background(),
		Dependencies{CommonDependencies: depsutil.CommonDependencies{RuntimePaths: runtimepaths.Paths{TopicContextPath: topicPath}}},
		bus,
		nil,
		conversationKey,
		slackInboundEvent{TeamID: "team-a", ChannelID: "channel-a", ChatType: "private", MessageTS: "1.0", Text: "/ctx"},
		"",
		nil,
	)
	if err != nil || !handled {
		t.Fatalf("maybeHandleSlackCommand() handled=%v, err=%v", handled, err)
	}
	select {
	case msg := <-got:
		envelope, err := msg.Envelope()
		if err != nil {
			t.Fatalf("Envelope() error = %v", err)
		}
		if !strings.Contains(envelope.Text, "slack-captured-model") {
			t.Fatalf("context reply = %q, want captured model", envelope.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context reply was not published")
	}
}
