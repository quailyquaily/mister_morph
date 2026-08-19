package slack

import "testing"

func TestSlackTaskConversationUsesThreadScope(t *testing.T) {
	got := slackTaskConversation(slackJob{
		ConversationKey: "slack:T1:C1",
		TeamID:          "T1",
		ChannelID:       "C1",
		ThreadTS:        "123.456",
		ChatType:        "channel",
		UserID:          "U1",
		DisplayName:     "Alice",
		MentionUsers:    []string{"U-BOT", "U2", "U1"},
	}, "U-BOT")
	if got == nil || got.ConversationID != "slack:T1:C1:thread:123.456" || got.ConversationType != "channel" {
		t.Fatalf("conversation = %+v", got)
	}
	if len(got.Participants) != 2 || got.Participants[0].Nickname != "Alice" || got.Participants[1].ID != "U2" {
		t.Fatalf("participants = %+v", got.Participants)
	}
}
