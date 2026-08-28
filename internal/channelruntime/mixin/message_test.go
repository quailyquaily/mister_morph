package mixin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

func TestDecodeMixinText(t *testing.T) {
	t.Parallel()

	for _, category := range []string{mixinapi.MessageCategoryPlainText, mixinapi.MessageCategoryPlainPost} {
		text, supported, err := decodeMixinText(category, base64.RawURLEncoding.EncodeToString([]byte(" hello ")))
		if err != nil || !supported || text != "hello" {
			t.Fatalf("decodeMixinText(%s) = %q, %v, %v", category, text, supported, err)
		}
	}
}

func TestDecodeMixinAttachmentPayload(t *testing.T) {
	payload := mixinAttachmentPayload{
		AttachmentID: "44444444-4444-4444-4444-444444444444",
		MimeType:     "image/png",
		Name:         "photo.png",
		Size:         123,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, supported, err := decodeMixinAttachment(mixinapi.MessageCategoryPlainImage, base64.RawURLEncoding.EncodeToString(raw))
	if err != nil || !supported || got != payload {
		t.Fatalf("decodeMixinAttachment() = %#v, %v, %v", got, supported, err)
	}
	if _, supported, err := decodeMixinAttachment(mixinapi.MessageCategoryPlainSticker, "ignored"); err != nil || supported {
		t.Fatalf("unsupported attachment = %v, %v", supported, err)
	}
}

func TestSplitMixinTextPreservesUTF8AndContent(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("界", 30)
	parts := splitMixinText(text, 20)
	if len(parts) < 2 || strings.Join(parts, "") != text {
		t.Fatalf("parts = %#v", parts)
	}
	for _, part := range parts {
		if len([]byte(part)) > 20 {
			t.Fatalf("part exceeds limit: %q", part)
		}
	}
}

func TestDecodeMixinTextRejectsInvalidAndIgnoresUnsupported(t *testing.T) {
	t.Parallel()

	if _, supported, err := decodeMixinText(mixinapi.MessageCategoryPlainText, "not-base64!"); err == nil || !supported {
		t.Fatalf("invalid text = supported %v, error %v", supported, err)
	}
	if text, supported, err := decodeMixinText(mixinapi.MessageCategoryPlainSticker, "ignored"); err != nil || supported || text != "" {
		t.Fatalf("sticker = %q, %v, %v", text, supported, err)
	}
}

func TestMixinMentionUsesTokenBoundariesAndCanBeRemoved(t *testing.T) {
	t.Parallel()

	const identity = "7000123456"
	if !mixinBotMentioned("hi @7000123456, help", identity) {
		t.Fatal("expected bot mention")
	}
	if mixinBotMentioned("hi @70001234567", identity) {
		t.Fatal("longer identity must not match")
	}
	if got := stripMixinBotMention("  hi @7000123456, help  ", identity); got != "hi , help" {
		t.Fatalf("stripped text = %q", got)
	}
}

func TestGroupCommandRequiresExplicitTrigger(t *testing.T) {
	t.Parallel()

	if mixinGroupCommandAllowed("/id", false) {
		t.Fatal("bare group command must not be handled")
	}
	if !mixinGroupCommandAllowed("/id", true) {
		t.Fatal("mentioned group command should be handled")
	}
	if !mixinGroupCommandAllowed("hello", false) {
		t.Fatal("ordinary messages are not command-gated")
	}
}

func TestMixinAllowlistBypassCommands(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		text string
		want bool
	}{
		{text: "/id", want: true},
		{text: "/id@7000123456", want: true},
		{text: "/pair @7000765432", want: true},
		{text: "/help", want: false},
		{text: "hello", want: false},
	} {
		if got := mixinBypassesAllowlist(test.text); got != test.want {
			t.Errorf("mixinBypassesAllowlist(%q) = %v, want %v", test.text, got, test.want)
		}
	}
}
