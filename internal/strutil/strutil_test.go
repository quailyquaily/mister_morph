package strutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8_Empty(t *testing.T) {
	if got := TruncateUTF8("", 10); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTruncateUTF8_ZeroMax(t *testing.T) {
	if got := TruncateUTF8("hello", 0); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTruncateUTF8_ASCII(t *testing.T) {
	got := TruncateUTF8("hello world", 5)
	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestTruncateUTF8_NoTruncation(t *testing.T) {
	s := "short"
	got := TruncateUTF8(s, 100)
	if got != s {
		t.Fatalf("expected %q, got %q", s, got)
	}
}

func TestTruncateUTF8_ChineseCharacters(t *testing.T) {
	s := "你好世界测试" // 6 chars = 18 bytes
	got := TruncateUTF8(s, 7)
	if got != "你好" {
		t.Fatalf("expected %q, got %q", "你好", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
}

func TestTruncateUTF8_Emoji(t *testing.T) {
	s := "ab🎉cd"
	got := TruncateUTF8(s, 4)
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
	if got != "ab" {
		t.Fatalf("expected %q, got %q", "ab", got)
	}
}

func TestTruncateUTF8_ExactBoundary(t *testing.T) {
	s := "abc你" // 3 + 3 = 6 bytes
	got := TruncateUTF8(s, 6)
	if got != s {
		t.Fatalf("expected %q, got %q", s, got)
	}
}

func TestTruncateUTF8_AlwaysValidUTF8(t *testing.T) {
	s := strings.Repeat("你好🎉世界", 200)
	for limit := 1; limit <= len(s); limit += 7 {
		got := TruncateUTF8(s, limit)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at limit=%d: %q", limit, got)
		}
		if len(got) > limit {
			t.Fatalf("too long at limit=%d: len=%d", limit, len(got))
		}
	}
}
