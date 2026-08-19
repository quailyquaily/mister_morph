package lark

import "testing"

func TestLarkTaskConversationUsesInboundIdentity(t *testing.T) {
	got := larkTaskConversation(larkJob{
		ConversationKey: "lark:oc_1",
		ChatType:        "p2p",
		FromUserID:      "ou_1",
		DisplayName:     "Alice",
		MentionUsers:    []string{"ou_2", "ou_1"},
	})
	if got == nil || got.ConversationID != "lark:oc_1" || got.ConversationType != "p2p" {
		t.Fatalf("conversation = %+v", got)
	}
	if len(got.Participants) != 2 || got.Participants[0].Nickname != "Alice" || got.Participants[1].ID != "ou_2" {
		t.Fatalf("participants = %+v", got.Participants)
	}
}
