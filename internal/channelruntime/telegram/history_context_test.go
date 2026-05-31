package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestNewTelegramInboundHistoryItem_StructuredFields(t *testing.T) {
	sentAt := time.Date(2026, 2, 13, 8, 20, 10, 0, time.UTC)
	item := newTelegramInboundHistoryItem(telegramJob{
		ChatID:           -100123,
		MessageID:        88,
		ReplyToMessageID: 77,
		SentAt:           sentAt,
		ChatType:         "group",
		FromUserID:       42,
		FromUsername:     "alice",
		FromDisplayName:  "Alice",
		Text:             "Quoted message:\n> @bob: 明天补充细节\n\nUser request:\n请看下 @carol 的更新",
		Images: []chathistory.ChatHistoryImage{{
			ID:   "img_tg_1",
			Path: "file_cache_dir/telegram/chat_1/a.jpg",
		}},
	})

	if item.Channel != chathistory.ChannelTelegram {
		t.Fatalf("channel = %q, want %q", item.Channel, chathistory.ChannelTelegram)
	}
	if item.Kind != chathistory.KindInboundUser {
		t.Fatalf("kind = %q, want %q", item.Kind, chathistory.KindInboundUser)
	}
	if item.ChatID != "-100123" {
		t.Fatalf("chat_id = %q, want -100123", item.ChatID)
	}
	if item.MessageID != "88" || item.ReplyToMessageID != "77" {
		t.Fatalf("message ids mismatch: message_id=%q reply_to_message_id=%q", item.MessageID, item.ReplyToMessageID)
	}
	if !item.SentAt.Equal(sentAt) {
		t.Fatalf("sent_at mismatch: got %s want %s", item.SentAt, sentAt)
	}
	if item.Sender.DisplayRef != "[Alice](tg:@alice)" {
		t.Fatalf("sender display_ref = %q, want [Alice](tg:@alice)", item.Sender.DisplayRef)
	}
	if !strings.Contains(item.Text, "[carol](tg:@carol)") {
		t.Fatalf("text mention should be normalized: %q", item.Text)
	}
	if item.Quote == nil {
		t.Fatalf("quote should be present")
	}
	if item.Quote.SenderRef != "[bob](tg:@bob)" {
		t.Fatalf("quote sender_ref = %q, want [bob](tg:@bob)", item.Quote.SenderRef)
	}
	if !strings.Contains(item.Quote.MarkdownBlock, "> [bob](tg:@bob)") {
		t.Fatalf("quote markdown should keep blockquote + normalized mention: %q", item.Quote.MarkdownBlock)
	}
	if len(item.Images) != 1 || item.Images[0].ID != "img_tg_1" {
		t.Fatalf("images = %#v, want one telegram image", item.Images)
	}
}

func TestTrimChatHistoryItems_ModeCaps(t *testing.T) {
	items := make([]chathistory.ChatHistoryItem, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, chathistory.ChatHistoryItem{Text: "m"})
	}

	talkativeCap := telegramHistoryCapForMode("talkative")
	trimmedTalkative := trimChatHistoryItems(items, talkativeCap)
	if len(trimmedTalkative) != 16 {
		t.Fatalf("talkative cap len = %d, want 16", len(trimmedTalkative))
	}

	smartCap := telegramHistoryCapForMode("smart")
	trimmedSmart := trimChatHistoryItems(items, smartCap)
	if len(trimmedSmart) != 8 {
		t.Fatalf("smart cap len = %d, want 8", len(trimmedSmart))
	}
}
