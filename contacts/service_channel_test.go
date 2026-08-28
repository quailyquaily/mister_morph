package contacts

import (
	"strings"
	"testing"
)

func TestResolveDecisionChannel_LineChatHint(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID: "tg:@alice",
		Channel:   ChannelTelegram,
	}, ShareDecision{
		ChatID: "line:Cgroup001",
	})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelLine {
		t.Fatalf("channel mismatch: got %q want %q", channel, ChannelLine)
	}
}

func TestResolveDecisionChannel_LineTargetFallback(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID:   "line_user:U100",
		Channel:     ChannelLine,
		LineUserID:  "U100",
		LineChatIDs: []string{"Cgroup001"},
	}, ShareDecision{})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelLine {
		t.Fatalf("channel mismatch: got %q want %q", channel, ChannelLine)
	}
}

func TestResolveDecisionChannel_LineUserContactIDFallback(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID: "line_user:U101",
	}, ShareDecision{})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelLine {
		t.Fatalf("channel mismatch: got %q want %q", channel, ChannelLine)
	}
}

func TestResolveDecisionChannel_LarkChatHint(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID: "tg:@alice",
		Channel:   ChannelTelegram,
	}, ShareDecision{
		ChatID: "lark:oc_group001",
	})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelLark {
		t.Fatalf("channel mismatch: got %q want %q", channel, ChannelLark)
	}
}

func TestResolveDecisionChannel_LarkTargetFallback(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID:  "lark_user:ou_123",
		Channel:    ChannelLark,
		LarkOpenID: "ou_123",
		LarkChatIDs: []string{
			"oc_group001",
		},
	}, ShareDecision{})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelLark {
		t.Fatalf("channel mismatch: got %q want %q", channel, ChannelLark)
	}
}

func TestResolveDecisionChannel_MixinTargets(t *testing.T) {
	const userID = "773e5e77-4107-45c2-b648-8fc722ed77f5"
	const chatID = "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34"
	contact := Contact{
		ContactID: userID, Channel: ChannelMixin, MixinUserID: userID,
		MixinChatIDs: []string{chatID},
	}
	if channel, err := ResolveDecisionChannel(contact, ShareDecision{}); err != nil || channel != ChannelMixin {
		t.Fatalf("default route = %q, %v", channel, err)
	}
	if channel, err := ResolveDecisionChannel(contact, ShareDecision{ChatID: "mixin:" + chatID}); err != nil || channel != ChannelMixin {
		t.Fatalf("chat route = %q, %v", channel, err)
	}
}

func TestResolveDecisionChannel_InvalidProtocolHint(t *testing.T) {
	_, err := ResolveDecisionChannel(Contact{
		ContactID: "contact:test",
		Channel:   ChannelTelegram,
	}, ShareDecision{
		ChatID: "discord:123",
	})
	if err == nil {
		t.Fatalf("ResolveDecisionChannel() expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid chat_id") {
		t.Fatalf("ResolveDecisionChannel() error mismatch: got %q", err.Error())
	}
}

func TestResolveDecisionChannel_MissingProtocolHint(t *testing.T) {
	_, err := ResolveDecisionChannel(Contact{
		ContactID: "contact:test",
		Channel:   ChannelTelegram,
	}, ShareDecision{
		ChatID: "-1001981343441",
	})
	if err == nil {
		t.Fatalf("ResolveDecisionChannel() expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid chat_id") {
		t.Fatalf("ResolveDecisionChannel() error mismatch: got %q", err.Error())
	}
}

func TestResolveDecisionChannel_ExplicitContactReferenceIsStrict(t *testing.T) {
	contact := Contact{
		ContactID:        "contact:smith",
		Channel:          ChannelSlack,
		TGUsername:       "smith_bot",
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
	}

	channel, err := ResolveDecisionChannel(contact, ShareDecision{ContactID: "tg:@smith_bot"})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelTelegram {
		t.Fatalf("channel = %q, want %q", channel, ChannelTelegram)
	}

	contact.TGUsername = ""
	if _, err := ResolveDecisionChannel(contact, ShareDecision{ContactID: "tg:@smith_bot"}); err == nil {
		t.Fatal("ResolveDecisionChannel() expected unavailable explicit channel error")
	}
}

func TestResolveDecisionChannel_ContactReferenceUsesDefaultRouting(t *testing.T) {
	channel, err := ResolveDecisionChannel(Contact{
		ContactID:        "contact:smith",
		Channel:          ChannelSlack,
		TGUsername:       "smith_bot",
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
	}, ShareDecision{ContactID: "contact:smith"})
	if err != nil {
		t.Fatalf("ResolveDecisionChannel() error = %v", err)
	}
	if channel != ChannelSlack {
		t.Fatalf("channel = %q, want %q", channel, ChannelSlack)
	}
}

func TestSyntheticMixinReferenceIsChatTarget(t *testing.T) {
	const chatID = "773e5e77-4107-45c2-b648-8fc722ed77f5"
	contact, ok, err := syntheticChatContact("mixin:" + chatID)
	if err != nil || !ok {
		t.Fatalf("syntheticChatContact() = %#v, %v, %v", contact, ok, err)
	}
	if contact.MixinUserID != "" || len(contact.MixinChatIDs) != 1 || contact.MixinChatIDs[0] != chatID {
		t.Fatalf("synthetic Mixin contact = %#v", contact)
	}
}
