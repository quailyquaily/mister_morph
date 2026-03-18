package line

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestPublishLineBusOutbound(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 32,
		Logger:      logger,
		Component:   "line-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	defer inprocBus.Close()

	gotCh := make(chan busruntime.BusMessage, 1)
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		select {
		case gotCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	_, err = publishLineBusOutbound(context.Background(), inprocBus, "Cgroup123", "hello line", "rtok_abc", "corr_1")
	if err != nil {
		t.Fatalf("publishLineBusOutbound() error = %v", err)
	}

	select {
	case msg := <-gotCh:
		if msg.Direction != busruntime.DirectionOutbound {
			t.Fatalf("direction = %q, want %q", msg.Direction, busruntime.DirectionOutbound)
		}
		if msg.Channel != busruntime.ChannelLine {
			t.Fatalf("channel = %q, want %q", msg.Channel, busruntime.ChannelLine)
		}
		if msg.ConversationKey != "line:Cgroup123" {
			t.Fatalf("conversation_key = %q, want %q", msg.ConversationKey, "line:Cgroup123")
		}
		if strings.TrimSpace(msg.Extensions.ReplyTo) != "rtok_abc" {
			t.Fatalf("extensions.reply_to = %q, want %q", msg.Extensions.ReplyTo, "rtok_abc")
		}
		env, err := msg.Envelope()
		if err != nil {
			t.Fatalf("Envelope() error = %v", err)
		}
		if strings.TrimSpace(env.Text) != "hello line" {
			t.Fatalf("envelope text = %q, want %q", env.Text, "hello line")
		}
		if strings.TrimSpace(env.ReplyTo) != "rtok_abc" {
			t.Fatalf("envelope reply_to = %q, want %q", env.ReplyTo, "rtok_abc")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not receive outbound bus message")
	}
}

func TestTodoResolveContextForLine(t *testing.T) {
	t.Parallel()

	ctx := todoResolveContextForLine(lineJob{
		ChatType:     "group",
		FromUserID:   "U123",
		MentionUsers: []string{"U1", "U2"},
		Text:         "ping @U1",
	})

	if ctx.Channel != "line" {
		t.Fatalf("channel = %q, want %q", ctx.Channel, "line")
	}
	if ctx.ChatType != "group" {
		t.Fatalf("chat_type = %q, want %q", ctx.ChatType, "group")
	}
	if ctx.SpeakerUsername != "line:U123" {
		t.Fatalf("speaker_username = %q, want %q", ctx.SpeakerUsername, "line:U123")
	}
	if len(ctx.MentionUsernames) != 2 || ctx.MentionUsernames[0] != "line:U1" || ctx.MentionUsernames[1] != "line:U2" {
		t.Fatalf("mention_usernames = %#v, want [line:U1 line:U2]", ctx.MentionUsernames)
	}
	if ctx.UserInputRaw != "ping @U1" {
		t.Fatalf("user_input_raw = %q, want %q", ctx.UserInputRaw, "ping @U1")
	}
}

func TestContactsSendRuntimeContextForLinePrivateChat(t *testing.T) {
	t.Parallel()

	ctx := contactsSendRuntimeContextForLine(lineJob{
		ChatID:     "Ucurrent",
		ChatType:   "user",
		FromUserID: "Ucurrent",
	})
	if len(ctx.ForbiddenTargetIDs) != 2 {
		t.Fatalf("forbidden_target_ids len = %d, want 2", len(ctx.ForbiddenTargetIDs))
	}
	if ctx.ForbiddenTargetIDs[0] != "line_user:Ucurrent" {
		t.Fatalf("forbidden_target_ids[0] = %q, want %q", ctx.ForbiddenTargetIDs[0], "line_user:Ucurrent")
	}
	if ctx.ForbiddenTargetIDs[1] != "line:Ucurrent" {
		t.Fatalf("forbidden_target_ids[1] = %q, want %q", ctx.ForbiddenTargetIDs[1], "line:Ucurrent")
	}
}

func TestShouldPublishLineText(t *testing.T) {
	t.Parallel()

	if !shouldPublishLineText(nil) {
		t.Fatalf("shouldPublishLineText(nil) = false, want true")
	}
	if !shouldPublishLineText(&agent.Final{IsLightweight: false}) {
		t.Fatalf("shouldPublishLineText(heavy) = false, want true")
	}
	if !shouldPublishLineText(&agent.Final{IsLightweight: true}) {
		t.Fatalf("shouldPublishLineText(lightweight) = false, want true")
	}
}
