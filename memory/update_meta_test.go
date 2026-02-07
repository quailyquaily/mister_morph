package memory

import (
	"os"
	"testing"
	"time"
)

func TestWriteShortTermStoresContactMeta(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, 7)
	date := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)

	_, err := mgr.WriteShortTerm(date, ShortTermContent{
		SessionSummary: []KVItem{{Title: "Who", Value: "Alice"}},
	}, "hello", WriteMeta{
		SessionID:       "telegram:1",
		Source:          "telegram",
		Channel:         "private",
		SubjectID:       "ext:telegram:1001",
		ContactID:       "tg:@alice",
		ContactNickname: "Alice",
	})
	if err != nil {
		t.Fatalf("WriteShortTerm() error = %v", err)
	}

	path, _ := mgr.ShortTermSessionPath(date, "telegram:1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	fm, _, ok := ParseFrontmatter(string(raw))
	if !ok {
		t.Fatalf("expected frontmatter in %s", path)
	}
	if fm.ContactID != "tg:@alice" {
		t.Fatalf("frontmatter contact_id mismatch: got %q want %q", fm.ContactID, "tg:@alice")
	}
	if fm.ContactNickname != "Alice" {
		t.Fatalf("frontmatter contact_nickname mismatch: got %q want %q", fm.ContactNickname, "Alice")
	}
}
