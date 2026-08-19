package core

import "testing"

func TestBuildTaskConversationNormalizesAndDeduplicatesParticipants(t *testing.T) {
	conversation := BuildTaskConversation(
		" slack:T1:C1:thread:123 ",
		" CHANNEL ",
		" U1 ",
		" Alice ",
		" U-BOT ",
		[]string{"U1", " U-BOT ", " U2 ", "", "U2"},
	)
	if conversation == nil {
		t.Fatal("BuildTaskConversation() = nil")
	}
	if conversation.ConversationID != "slack:T1:C1:thread:123" || conversation.ConversationType != "channel" {
		t.Fatalf("conversation = %+v", conversation)
	}
	if len(conversation.Participants) != 2 {
		t.Fatalf("participants = %+v, want 2", conversation.Participants)
	}
	if got := conversation.Participants[0]; got.ID != "U1" || got.Nickname != "Alice" {
		t.Fatalf("sender = %+v", got)
	}
	if got := conversation.Participants[1]; got.ID != "U2" || got.Nickname != "" {
		t.Fatalf("mention = %+v", got)
	}
}

func TestBuildTaskConversationReturnsNilWhenEmpty(t *testing.T) {
	if got := BuildTaskConversation("", "", "", "", "", nil); got != nil {
		t.Fatalf("BuildTaskConversation() = %+v, want nil", got)
	}
}
