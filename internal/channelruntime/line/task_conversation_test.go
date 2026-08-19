package line

import "testing"

func TestLineTaskConversationUsesInboundIdentity(t *testing.T) {
	got := lineTaskConversation(lineJob{
		ConversationKey: "line:C1",
		ChatType:        "group",
		FromUserID:      "U1",
		DisplayName:     "Alice",
		MentionUsers:    []string{"U-BOT", "U2", "U1"},
	}, "U-BOT")
	if got == nil || got.ConversationID != "line:C1" || got.ConversationType != "group" {
		t.Fatalf("conversation = %+v", got)
	}
	if len(got.Participants) != 2 || got.Participants[0].Nickname != "Alice" || got.Participants[1].ID != "U2" {
		t.Fatalf("participants = %+v", got.Participants)
	}
}
