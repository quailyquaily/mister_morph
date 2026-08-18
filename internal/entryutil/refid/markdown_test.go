package refid

import (
	"strings"
	"testing"
)

func TestExtractMarkdownReferenceIDs(t *testing.T) {
	ids, err := ExtractMarkdownReferenceIDs("提醒 [John](tg:1001) 通知 [Momo](aqua:12D3KooW)，再提醒 [John](tg:1001)")
	if err != nil {
		t.Fatalf("ExtractMarkdownReferenceIDs() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ExtractMarkdownReferenceIDs() len = %d, want 2", len(ids))
	}
	if ids[0] != "tg:1001" || ids[1] != "aqua:12D3KooW" {
		t.Fatalf("ExtractMarkdownReferenceIDs() ids = %#v", ids)
	}
}

func TestExtractMarkdownReferenceIDsRejectsInvalidProtocol(t *testing.T) {
	_, err := ExtractMarkdownReferenceIDs("提醒 [Momo](peer-v2:12D3KooW) 明天回复")
	if err == nil {
		t.Fatalf("expected error for invalid protocol")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid reference id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFormatMarkdownReference(t *testing.T) {
	out, err := FormatMarkdownReference("Momo", "tg:1001")
	if err != nil {
		t.Fatalf("FormatMarkdownReference() error = %v", err)
	}
	if out != "[Momo](tg:1001)" {
		t.Fatalf("FormatMarkdownReference() = %q", out)
	}
}

func TestFormatMarkdownReferenceRejectsInvalidID(t *testing.T) {
	_, err := FormatMarkdownReference("Momo", "peer-v2:12D3KooW")
	if err == nil {
		t.Fatalf("expected invalid reference id error")
	}
}
