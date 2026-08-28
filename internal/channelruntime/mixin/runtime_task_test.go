package mixin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestPublishMixinBusOutboundAndWaitReturnsDeliveryError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus, err := busruntime.StartInproc(busruntime.BootstrapOptions{MaxInFlight: 2, Logger: logger, Component: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	receipts := newMixinDeliveryReceipts()
	deliveryErr := errors.New("delivery failed")
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(_ context.Context, message busruntime.BusMessage) error {
		receipts.complete(message.ID, deliveryErr)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = publishMixinBusOutboundAndWait(context.Background(), bus, receipts, testConversationID, testUserID, "hello", "", "test:delivery")
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("publishMixinBusOutboundAndWait() error = %v", err)
	}
}

func TestMixinReplyRecipientLeavesGroupUnaddressed(t *testing.T) {
	t.Parallel()

	if got := mixinReplyRecipient(mixinapi.ConversationCategoryGroup, testUserID); got != "" {
		t.Fatalf("group recipient = %q, want empty conversation delivery", got)
	}
	if got := mixinReplyRecipient(mixinapi.ConversationCategoryContact, testUserID); got != testUserID {
		t.Fatalf("contact recipient = %q, want %q", got, testUserID)
	}
	message, _, err := newMixinBusOutbound(testConversationID, "", "hello", "", "test:group-delivery")
	if err != nil {
		t.Fatalf("newMixinBusOutbound(group) error = %v", err)
	}
	if message.ParticipantKey != "" {
		t.Fatalf("group participant_key = %q, want empty", message.ParticipantKey)
	}
}

type stubMixinAttachmentAPI struct{}

func (stubMixinAttachmentAPI) CreateAttachment(context.Context) (mixinapi.Attachment, error) {
	return mixinapi.Attachment{}, nil
}
func (stubMixinAttachmentAPI) UploadAttachment(context.Context, mixinapi.Attachment, string, int64, io.Reader) error {
	return nil
}
func (stubMixinAttachmentAPI) SendMessages(context.Context, []mixinapi.MessageRequest) error {
	return nil
}

func TestBuildMixinPromptMessagesSeparatesHistoryAndCurrent(t *testing.T) {
	t.Parallel()

	historyMessage, currentMessage, err := buildMixinPromptMessages([]chathistory.ChatHistoryItem{{
		Channel: chathistory.ChannelMixin, Kind: chathistory.KindInboundUser, MessageID: "m1", SentAt: time.Unix(1, 0), Text: "earlier",
	}}, mixinJob{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		MessageID:      "22222222-2222-2222-2222-222222222222",
		FromUserID:     "33333333-3333-3333-3333-333333333333",
		Text:           "latest",
		SentAt:         time.Unix(2, 0),
	}, "text-model", nil, nil)
	if err != nil {
		t.Fatalf("buildMixinPromptMessages() error = %v", err)
	}
	if historyMessage == nil || !strings.Contains(historyMessage.Content, `"text": "earlier"`) || strings.Contains(historyMessage.Content, `"text": "latest"`) {
		t.Fatalf("history message = %#v", historyMessage)
	}
	if currentMessage == nil || !strings.Contains(currentMessage.Content, "latest") {
		t.Fatalf("current message = %#v", currentMessage)
	}
}

func TestRegisterMixinChannelTools(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registerMixinChannelTools(registry, stubMixinAttachmentAPI{}, testConversationID, testUserID, t.TempDir(), 1024); err != nil {
		t.Fatalf("registerMixinChannelTools() error = %v", err)
	}
	for _, name := range []string{"mixin_send_file", "mixin_send_photo", "mixin_send_audio"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("tool %q was not registered", name)
		}
	}
}

func TestMixinHistoryUsesCanonicalIdentity(t *testing.T) {
	t.Parallel()

	item := newMixinInboundHistoryItem(mixinJob{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		ChatType:       "GROUP",
		MessageID:      "22222222-2222-2222-2222-222222222222",
		FromUserID:     "33333333-3333-3333-3333-333333333333",
		IdentityNumber: "7000123456",
		DisplayName:    "Alice",
		Text:           "hello",
	})
	if item.Channel != chathistory.ChannelMixin || item.ChatID != "mixin:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("history identity = %#v", item)
	}
	if item.Sender.UserID != "33333333-3333-3333-3333-333333333333" || item.Sender.Username != "7000123456" {
		t.Fatalf("sender = %#v", item.Sender)
	}
}
