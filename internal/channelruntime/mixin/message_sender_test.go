package mixin

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

func TestMixinMessageSenderExpandsAndCachesGroupRecipients(t *testing.T) {
	api := &fakeMixinAPI{
		users: map[string]mixinapi.User{testBotID: {UserID: testBotID}},
		conversations: map[string]mixinapi.Conversation{testConversationID: {
			ConversationID: testConversationID,
			Category:       mixinapi.ConversationCategoryGroup,
			Participants: []mixinapi.ConversationParticipant{
				{UserID: testBotID},
				{UserID: testUserID},
				{UserID: "44444444-4444-4444-4444-444444444444"},
			},
		}},
	}
	tracker := newRecentMessageTracker(8)
	sender := newMixinMessageSender(api, testBotID, tracker)
	request := mixinapi.MessageRequest{
		ConversationID: testConversationID,
		MessageID:      "55555555-5555-5555-5555-555555555555",
		Category:       mixinapi.MessageCategoryPlainText,
		DataBase64:     base64.RawURLEncoding.EncodeToString([]byte("hello")),
	}

	if err := sender.SendMessages(context.Background(), []mixinapi.MessageRequest{request}); err != nil {
		t.Fatalf("SendMessages(first) error = %v", err)
	}
	request.MessageID = "66666666-6666-6666-6666-666666666666"
	if err := sender.SendMessages(context.Background(), []mixinapi.MessageRequest{request}); err != nil {
		t.Fatalf("SendMessages(second) error = %v", err)
	}

	if api.readConversationCalls != 1 {
		t.Fatalf("ReadConversation() calls = %d, want 1", api.readConversationCalls)
	}
	if len(api.sent) != 4 {
		t.Fatalf("sent messages = %d, want 4", len(api.sent))
	}
	for _, message := range api.sent {
		if message.RecipientID == "" || message.RecipientID == testBotID {
			t.Fatalf("message recipient = %q", message.RecipientID)
		}
		if !tracker.Contains(testConversationID, message.MessageID) {
			t.Fatalf("sent message %q was not tracked", message.MessageID)
		}
	}
}
