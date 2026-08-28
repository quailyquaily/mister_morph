package agentpair

import "testing"

func TestParseAdminsUsesStablePlatformIDs(t *testing.T) {
	admins, err := ParseAdmins([]string{
		" tg:1234 ",
		"slack:T123:U234",
		"line_user:U345",
		"lark_user:ou_456",
		"mixin:11111111-1111-1111-1111-111111111111",
		"tg:1234",
	})
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	for _, id := range []string{"tg:1234", "slack:T123:U234", "line_user:U345", "lark_user:ou_456", "mixin:11111111-1111-1111-1111-111111111111"} {
		if !admins.Contains(id) {
			t.Errorf("admins does not contain %q", id)
		}
	}
	if admins.Contains("tg:9999") {
		t.Fatal("admins contains an unconfigured id")
	}
}

func TestParseAdminsAcceptsTelegramContactReference(t *testing.T) {
	admins, err := ParseAdmins([]string{" tg:@BallCatCat ", "mixin:@7000123456"})
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	if !admins.Contains("tg:@ballcatcat") {
		t.Fatal("admins does not contain the Telegram contact reference")
	}
	if !admins.Contains("mixin:@7000123456") {
		t.Fatal("admins does not contain the Mixin contact reference")
	}
}

func TestParseAdminsRejectsMalformedIDs(t *testing.T) {
	for _, raw := range []string{
		"tg:@",
		"tg:@bad name",
		"tg:-1001",
		"slack:T123",
		"slack:T123:C234",
		"line:group-id",
		"lark:chat-id",
		"unknown:123",
		"mixin:not-a-uuid",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseAdmins([]string{raw}); err == nil {
				t.Fatalf("ParseAdmins(%q) expected error", raw)
			}
		})
	}
}
