package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8_ASCII(t *testing.T) {
	s := "hello world"
	got := truncateUTF8(s, 5)
	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestTruncateUTF8_NoTruncation(t *testing.T) {
	s := "short"
	got := truncateUTF8(s, 100)
	if got != s {
		t.Fatalf("expected %q, got %q", s, got)
	}
}

func TestTruncateUTF8_ChineseCharacters(t *testing.T) {
	// Each Chinese character is 3 bytes in UTF-8.
	s := "你好世界测试" // 6 chars = 18 bytes
	// Truncate at 7 bytes: should get "你好" (6 bytes), not split the 3rd char.
	got := truncateUTF8(s, 7)
	if got != "你好" {
		t.Fatalf("expected %q, got %q", "你好", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
}

func TestTruncateUTF8_Emoji(t *testing.T) {
	// 🎉 is 4 bytes in UTF-8.
	s := "ab🎉cd"
	// Truncate at 4 bytes: should get "ab" (2 bytes), not split the emoji.
	got := truncateUTF8(s, 4)
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
	// "ab" is 2 bytes, "ab🎉" is 6 bytes. At limit 4, we should get "ab".
	if got != "ab" {
		t.Fatalf("expected %q, got %q", "ab", got)
	}
}

func TestTruncateUTF8_ExactBoundary(t *testing.T) {
	s := "abc你" // 3 + 3 = 6 bytes
	got := truncateUTF8(s, 6)
	if got != s {
		t.Fatalf("expected %q, got %q", s, got)
	}
}

func TestTruncateUTF8_AlwaysValidUTF8(t *testing.T) {
	// Build a string with mixed multi-byte content.
	s := strings.Repeat("你好🎉世界", 200) // lots of multi-byte chars
	for limit := 1; limit <= len(s); limit += 7 {
		got := truncateUTF8(s, limit)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at limit=%d: %q", limit, got)
		}
		if len(got) > limit {
			t.Fatalf("too long at limit=%d: len=%d", limit, len(got))
		}
	}
}
