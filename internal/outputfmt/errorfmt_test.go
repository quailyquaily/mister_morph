package outputfmt

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeErrorText_RemovesHostAndRedactsSensitiveQuery(t *testing.T) {
	in := `LLM call failed at step 1: Post "https://api.siray.ai/v1beta/models/google%2Fgemini-3-flash-preview:generateContent?key=sk-test-secret&alt=json": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`

	out := SanitizeErrorText(in)
	if strings.Contains(out, "api.siray.ai") {
		t.Fatalf("host should be removed, got %q", out)
	}
	if strings.Contains(out, "sk-test-secret") {
		t.Fatalf("sensitive key value should be redacted, got %q", out)
	}
	if !strings.Contains(out, `Post "/v1beta/models/google%2Fgemini-3-flash-preview:generateContent?`) {
		t.Fatalf("expected path/query to be kept, got %q", out)
	}
	if !strings.Contains(out, "key=%5Bredacted%5D") {
		t.Fatalf("expected key query to be redacted, got %q", out)
	}
	if !strings.Contains(out, "alt=json") {
		t.Fatalf("expected non-sensitive query to remain, got %q", out)
	}
}

func TestSanitizeErrorText_MultipleURLs(t *testing.T) {
	in := `fetch failed: https://a.example.com/ping?token=abc then https://b.example.com/health?ok=1`
	out := SanitizeErrorText(in)
	if strings.Contains(out, "a.example.com") || strings.Contains(out, "b.example.com") {
		t.Fatalf("hosts should be removed, got %q", out)
	}
	if !strings.Contains(out, "/ping?token=%5Bredacted%5D") {
		t.Fatalf("first url should keep path/query, got %q", out)
	}
	if !strings.Contains(out, "/health?ok=1") {
		t.Fatalf("second url should keep path/query, got %q", out)
	}
}

func TestFormatErrorForDisplay(t *testing.T) {
	if got := FormatErrorForDisplay(nil); got != "" {
		t.Fatalf("nil error should format as empty string, got %q", got)
	}
	err := errors.New(`Post "https://example.com/api?apikey=123": bad gateway`)
	got := FormatErrorForDisplay(err)
	if strings.Contains(got, "example.com") {
		t.Fatalf("host should be removed, got %q", got)
	}
	if !strings.Contains(got, "/api?apikey=%5Bredacted%5D") {
		t.Fatalf("expected redacted apikey query, got %q", got)
	}
}
