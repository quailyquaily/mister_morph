package bus

import "testing"

func TestBuildConversationKey(t *testing.T) {
	key, err := BuildConversationKey(ChannelTelegram, "-1001")
	if err != nil {
		t.Fatalf("BuildConversationKey() error = %v", err)
	}
	if key != "tg:-1001" {
		t.Fatalf("conversation key mismatch: got %q", key)
	}
}

func TestBuildConversationKeyConsole(t *testing.T) {
	key, err := BuildConversationKey(ChannelConsole, "0195a5e9-1a2b-7c3d-8e4f-123456789abc")
	if err != nil {
		t.Fatalf("BuildConversationKey() error = %v", err)
	}
	if key != "console:0195a5e9-1a2b-7c3d-8e4f-123456789abc" {
		t.Fatalf("conversation key mismatch: got %q", key)
	}
}

func TestBuildConversationKeyLine(t *testing.T) {
	key, err := BuildLineConversationKey("Cgroup123")
	if err != nil {
		t.Fatalf("BuildLineConversationKey() error = %v", err)
	}
	if key != "line:Cgroup123" {
		t.Fatalf("conversation key mismatch: got %q", key)
	}
}

func TestBuildConversationKeyLark(t *testing.T) {
	key, err := BuildLarkConversationKey("oc_group123")
	if err != nil {
		t.Fatalf("BuildLarkConversationKey() error = %v", err)
	}
	if key != "lark:oc_group123" {
		t.Fatalf("conversation key mismatch: got %q", key)
	}
}

func TestBuildConversationKeyRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		channel Channel
		id      string
	}{
		{name: "invalid channel", channel: Channel("unknown"), id: "1"},
		{name: "empty id", channel: ChannelTelegram, id: "   "},
		{name: "id contains space", channel: ChannelTelegram, id: "a b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildConversationKey(tc.channel, tc.id); err == nil {
				t.Fatalf("BuildConversationKey() expected error")
			}
		})
	}
}
