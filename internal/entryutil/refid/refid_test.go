package refid

import "testing"

func TestParse(t *testing.T) {
	protocol, id, ok := Parse("SLACK:T123:C456")
	if !ok {
		t.Fatalf("Parse() should succeed")
	}
	if protocol != "slack" || id != "T123:C456" {
		t.Fatalf("Parse() mismatch: protocol=%q id=%q", protocol, id)
	}
}

func TestNormalize(t *testing.T) {
	out, ok := Normalize("TG:-1001981343441")
	if !ok {
		t.Fatalf("Normalize() should succeed")
	}
	if out != "tg:-1001981343441" {
		t.Fatalf("Normalize() mismatch: got %q", out)
	}
}

func TestIsValid(t *testing.T) {
	if !IsValid("peer:12D3KooWPeer") {
		t.Fatalf("IsValid(custom protocol) should be true")
	}
	if !IsValid("line_user:U123") {
		t.Fatalf("IsValid(protocol with underscore) should be true")
	}
	if IsValid("peer-v2:12D3KooWPeer") {
		t.Fatalf("IsValid(protocol with punctuation) should be false")
	}
	if IsValid("not-a-reference") {
		t.Fatalf("IsValid(invalid) should be false")
	}
}

func TestParseTelegramChatIDHint(t *testing.T) {
	chatID, hasHint, err := ParseTelegramChatIDHint("tg:-1001981343441")
	if err != nil || !hasHint || chatID != -1001981343441 {
		t.Fatalf("ParseTelegramChatIDHint(tg) mismatch: chat_id=%d has_hint=%v err=%v", chatID, hasHint, err)
	}
	chatID, hasHint, err = ParseTelegramChatIDHint("tg:-1001981343441_4425")
	if err != nil || !hasHint || chatID != -1001981343441 {
		t.Fatalf("ParseTelegramChatIDHint(topic) mismatch: chat_id=%d has_hint=%v err=%v", chatID, hasHint, err)
	}
	_, hasHint, err = ParseTelegramChatIDHint("12345")
	if err == nil || !hasHint {
		t.Fatalf("ParseTelegramChatIDHint(raw) expected has_hint=true error, has_hint=%v err=%v", hasHint, err)
	}
	_, hasHint, err = ParseTelegramChatIDHint("")
	if err != nil || hasHint {
		t.Fatalf("ParseTelegramChatIDHint(empty) mismatch: has_hint=%v err=%v", hasHint, err)
	}
	_, hasHint, err = ParseTelegramChatIDHint("slack:T001:C002")
	if err == nil || !hasHint {
		t.Fatalf("ParseTelegramChatIDHint(non tg) expected has_hint=true error")
	}
}

func TestParseSlackChatIDHint(t *testing.T) {
	teamID, channelID, hasHint, err := ParseSlackChatIDHint("SLACK:T001:C002")
	if err != nil || !hasHint || teamID != "T001" || channelID != "C002" {
		t.Fatalf("ParseSlackChatIDHint(valid) mismatch: team=%q channel=%q has_hint=%v err=%v", teamID, channelID, hasHint, err)
	}
	_, _, hasHint, err = ParseSlackChatIDHint("tg:1001")
	if err != nil || hasHint {
		t.Fatalf("ParseSlackChatIDHint(non slack) mismatch: has_hint=%v err=%v", hasHint, err)
	}
	_, _, hasHint, err = ParseSlackChatIDHint("slack:T001")
	if err == nil || !hasHint {
		t.Fatalf("ParseSlackChatIDHint(invalid slack) expected has_hint=true error")
	}
}

func TestParseLineChatIDHint(t *testing.T) {
	chatID, hasHint, err := ParseLineChatIDHint("line:Cgroup001")
	if err != nil || !hasHint || chatID != "Cgroup001" {
		t.Fatalf("ParseLineChatIDHint(valid) mismatch: chat_id=%q has_hint=%v err=%v", chatID, hasHint, err)
	}
	_, hasHint, err = ParseLineChatIDHint("tg:1001")
	if err != nil || hasHint {
		t.Fatalf("ParseLineChatIDHint(non line) mismatch: has_hint=%v err=%v", hasHint, err)
	}
	_, hasHint, err = ParseLineChatIDHint("line:")
	if err == nil || !hasHint {
		t.Fatalf("ParseLineChatIDHint(invalid line) expected has_hint=true error")
	}
}

func TestParseLarkChatIDHint(t *testing.T) {
	chatID, hasHint, err := ParseLarkChatIDHint("lark:oc_group001")
	if err != nil || !hasHint || chatID != "oc_group001" {
		t.Fatalf("ParseLarkChatIDHint(valid) mismatch: chat_id=%q has_hint=%v err=%v", chatID, hasHint, err)
	}
	_, hasHint, err = ParseLarkChatIDHint("tg:1001")
	if err != nil || hasHint {
		t.Fatalf("ParseLarkChatIDHint(non lark) mismatch: has_hint=%v err=%v", hasHint, err)
	}
	_, hasHint, err = ParseLarkChatIDHint("lark:")
	if err == nil || !hasHint {
		t.Fatalf("ParseLarkChatIDHint(invalid lark) expected has_hint=true error")
	}
}

func TestParseLineContactIDs(t *testing.T) {
	chatID, ok := ParseLineChatContactID("line:C100")
	if !ok || chatID != "C100" {
		t.Fatalf("ParseLineChatContactID(valid) mismatch: chat_id=%q ok=%v", chatID, ok)
	}
	userID, ok := ParseLineUserContactID("line_user:U100")
	if !ok || userID != "U100" {
		t.Fatalf("ParseLineUserContactID(valid) mismatch: user_id=%q ok=%v", userID, ok)
	}
	if _, ok := ParseLineChatContactID("line:"); ok {
		t.Fatalf("ParseLineChatContactID(invalid) expected ok=false")
	}
	if _, ok := ParseLineUserContactID("line_user:"); ok {
		t.Fatalf("ParseLineUserContactID(invalid) expected ok=false")
	}
	if NormalizeLineID("  U123 ") != "U123" {
		t.Fatalf("NormalizeLineID mismatch")
	}
	if !LineIDLooksLikeUserID("U123") {
		t.Fatalf("LineIDLooksLikeUserID expected true")
	}
	if LineIDLooksLikeUserID("C123") {
		t.Fatalf("LineIDLooksLikeUserID expected false")
	}
}

func TestParseLarkContactIDs(t *testing.T) {
	chatID, ok := ParseLarkChatContactID("lark:oc_group001")
	if !ok || chatID != "oc_group001" {
		t.Fatalf("ParseLarkChatContactID(valid) mismatch: chat_id=%q ok=%v", chatID, ok)
	}
	openID, ok := ParseLarkUserContactID("lark_user:ou_123")
	if !ok || openID != "ou_123" {
		t.Fatalf("ParseLarkUserContactID(valid) mismatch: open_id=%q ok=%v", openID, ok)
	}
	if _, ok := ParseLarkChatContactID("lark:"); ok {
		t.Fatalf("ParseLarkChatContactID(invalid) expected ok=false")
	}
	if _, ok := ParseLarkUserContactID("lark_user:"); ok {
		t.Fatalf("ParseLarkUserContactID(invalid) expected ok=false")
	}
	if NormalizeLarkID("  ou_123 ") != "ou_123" {
		t.Fatalf("NormalizeLarkID mismatch")
	}
}
