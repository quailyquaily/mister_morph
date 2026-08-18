package awarenessutil

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/chatinfo"
)

func TestBuildCronNotifyTargetIncludesPeopleAndChatProfile(t *testing.T) {
	info := chatinfo.Info{
		ChatID:   "tg:-100123",
		Platform: "telegram",
		Type:     "supergroup",
		Name:     "Project Room",
	}
	target := BuildCronNotifyTarget("Remind [Alice](tg:@alice), [Alice again](TG:@ALICE), and [Bob](slack:T111:U222).", "tg:-100123", &info)
	if target == nil {
		t.Fatalf("target is nil")
	}
	if target["chat_id"] != "tg:-100123" {
		t.Fatalf("chat_id = %#v", target["chat_id"])
	}
	people, ok := target["people"].([]map[string]string)
	if !ok {
		t.Fatalf("people type = %T", target["people"])
	}
	if len(people) != 2 {
		t.Fatalf("len(people) = %d, want 2", len(people))
	}
	if people[0]["contact_id"] != "tg:@alice" || people[0]["label"] != "Alice" || people[0]["ref"] != "[Alice](tg:@alice)" {
		t.Fatalf("first person mismatch: %#v", people[0])
	}
	chat, ok := target["chat_profile"].(map[string]string)
	if !ok {
		t.Fatalf("chat_profile type = %T", target["chat_profile"])
	}
	if chat["platform"] != "telegram" || chat["name"] != "Project Room" {
		t.Fatalf("chat_profile mismatch: %#v", chat)
	}
	if _, ok := chat["avatar_ref"]; ok {
		t.Fatalf("chat_profile should not include avatar_ref: %#v", chat)
	}
}

func TestBuildCronNotifyTargetWithoutChatProfile(t *testing.T) {
	target := BuildCronNotifyTarget("Send status to the room.", "tg:-100123", nil)
	if target == nil {
		t.Fatalf("target is nil")
	}
	if _, ok := target["chat_profile"]; ok {
		t.Fatalf("chat_profile should be absent: %#v", target)
	}
	people, ok := target["people"].([]map[string]string)
	if !ok || len(people) != 0 {
		t.Fatalf("people mismatch: %#v", target["people"])
	}
}

func TestBuildCronNotifyTargetSkipsEmptyChatID(t *testing.T) {
	if got := BuildCronNotifyTarget("Remind [Alice](tg:@alice).", "", nil); got != nil {
		t.Fatalf("target = %#v, want nil", got)
	}
}
