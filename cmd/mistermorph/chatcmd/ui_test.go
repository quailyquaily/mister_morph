package chatcmd

import (
	"strings"
	"testing"
)

func TestPrintChatSessionHeader_DefaultMode(t *testing.T) {
	var b strings.Builder
	printChatSessionHeader(&b, false, "openai/gpt-test", "/tmp/workspace", "/tmp/cache")
	got := b.String()

	for _, want := range []string{
		"▄▄   ▄▄",
		"model=gpt-test",
		"workspace_dir=/tmp/workspace",
		"file_cache_dir=/tmp/cache",
		"Interactive chat started. Press Ctrl+C or type /exit to quit.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("header missing %q in %q", want, got)
		}
	}
}

func TestPrintChatSessionHeader_CompactMode(t *testing.T) {
	var b strings.Builder
	printChatSessionHeader(&b, true, "openai/gpt-test", "/tmp/workspace", "/tmp/cache")
	got := b.String()

	for _, want := range []string{
		"model=gpt-test",
		"workspace_dir=/tmp/workspace",
		"file_cache_dir=/tmp/cache",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("header missing %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{
		"▄▄   ▄▄",
		"Interactive chat started. Press Ctrl+C or type /exit to quit.",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("header unexpectedly contains %q in %q", unwanted, got)
		}
	}
}

func TestDisplayModelName_BedrockARN(t *testing.T) {
	got := displayModelName("arn:aws:bedrock:ap-northeast-1::foundation-model/moonshotai.kimi-k2.5")
	if got != "moonshotai.kimi-k2.5" {
		t.Fatalf("displayModelName() = %q, want moonshotai.kimi-k2.5", got)
	}
}
