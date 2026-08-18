package llmstats

import (
	"path/filepath"
	"testing"
	"time"
)

func TestJournalRotateBySize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journal := NewJournal(root, JournalOptions{MaxFileBytes: 1})
	journal.now = func() time.Time {
		return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	}
	defer func() { _ = journal.Close() }()

	record := func(model string) RequestRecord {
		return RequestRecord{
			TS:           time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
			Provider:     "openai",
			APIBase:      "https://api.openai.com",
			Model:        model,
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		}
	}

	off1, err := journal.Append(record("gpt-5.2"))
	if err != nil {
		t.Fatalf("Append(1) error = %v", err)
	}
	off2, err := journal.Append(record("gpt-5-mini"))
	if err != nil {
		t.Fatalf("Append(2) error = %v", err)
	}
	off3, err := journal.Append(record("gpt-5-nano"))
	if err != nil {
		t.Fatalf("Append(3) error = %v", err)
	}

	if off1.File != "since-2026-03-07-0001.jsonl" || off1.Line != 1 || off1.Byte <= 0 {
		t.Fatalf("offset1 = %+v, want file 0001 line 1", off1)
	}
	if off2.File != "since-2026-03-07-0002.jsonl" || off2.Line != 1 || off2.Byte <= 0 {
		t.Fatalf("offset2 = %+v, want file 0002 line 1", off2)
	}
	if off3.File != "since-2026-03-07-0003.jsonl" || off3.Line != 1 || off3.Byte <= 0 {
		t.Fatalf("offset3 = %+v, want file 0003 line 1", off3)
	}

	for _, name := range []string{off1.File, off2.File, off3.File} {
		if _, err := filepath.Abs(filepath.Join(root, name)); err != nil {
			t.Fatalf("Abs(%s) error = %v", name, err)
		}
	}
}

func TestParseJournalSegmentFile(t *testing.T) {
	segment, ok := parseJournalSegmentFile("  since-2026-03-07-0012.jsonl  ")
	if !ok || segment.Key != "since-2026-03-07-0012.jsonl" || segment.Date != "2026-03-07" || segment.Index != 12 {
		t.Fatalf("parseJournalSegmentFile() = %+v, %v", segment, ok)
	}
	if _, ok := parseJournalSegmentFile("since-2026-03-07-0000.jsonl"); ok {
		t.Fatal("parseJournalSegmentFile() accepted index zero")
	}
}
