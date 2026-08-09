package daemonruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAuditLogChunkUsesCommonPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard_audit.jsonl")
	content := strings.Join([]string{"one", "two", "three", "four"}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	latest, err := readAuditLogChunk(path, "", 2)
	if err != nil {
		t.Fatalf("readAuditLogChunk(latest) error = %v", err)
	}
	if got := strings.Join(latest.Items, "\n"); got != "three\nfour" {
		t.Fatalf("latest lines = %q", got)
	}
	if !latest.HasNext || latest.NextCursor == "" {
		t.Fatalf("latest chunk missing next cursor: %+v", latest)
	}

	older, err := readAuditLogChunk(path, latest.NextCursor, 2)
	if err != nil {
		t.Fatalf("readAuditLogChunk(older) error = %v", err)
	}
	if got := strings.Join(older.Items, "\n"); got != "one\ntwo" {
		t.Fatalf("older lines = %q", got)
	}
	if older.HasNext || older.NextCursor != "" {
		t.Fatalf("oldest chunk = %+v, want no older cursor", older)
	}
}

func TestListAuditFiles_IncludesDecisionMirrors(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "guard_audit.jsonl")
	files := []string{
		"guard_audit.jsonl",
		"guard_audit.jsonl.20260323T080000Z",
		"guard_audit.allow_with_redaction.jsonl",
		"guard_audit.allow_with_redaction.jsonl.20260323T080100Z",
		"guard_audit.require_approval.jsonl",
		"guard_audit.deny.jsonl",
		"other.jsonl",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	items, err := listAuditFiles(basePath)
	if err != nil {
		t.Fatalf("listAuditFiles() error = %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("listAuditFiles() len = %d, want 6", len(items))
	}
	if items[0].Name != "guard_audit.jsonl" || !items[0].Current {
		t.Fatalf("first audit file = %#v, want current canonical file first", items[0])
	}

	got := map[string]bool{}
	for _, item := range items {
		got[item.Name] = true
	}
	for _, want := range []string{
		"guard_audit.jsonl",
		"guard_audit.jsonl.20260323T080000Z",
		"guard_audit.allow_with_redaction.jsonl",
		"guard_audit.allow_with_redaction.jsonl.20260323T080100Z",
		"guard_audit.require_approval.jsonl",
		"guard_audit.deny.jsonl",
	} {
		if !got[want] {
			t.Fatalf("listAuditFiles() missing %q in %#v", want, got)
		}
	}
	if got["other.jsonl"] {
		t.Fatalf("listAuditFiles() should not include unrelated files")
	}
}

func TestResolveAuditFilePath_AllowsDecisionMirrors(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "guard_audit.jsonl")

	got, err := resolveAuditFilePath(basePath, "guard_audit.allow_with_redaction.jsonl")
	if err != nil {
		t.Fatalf("resolveAuditFilePath(mirror) error = %v", err)
	}
	want := filepath.Join(filepath.Dir(basePath), "guard_audit.allow_with_redaction.jsonl")
	if got != want {
		t.Fatalf("resolveAuditFilePath(mirror) = %q, want %q", got, want)
	}

	got, err = resolveAuditFilePath(basePath, "guard_audit.deny.jsonl.20260323T080000Z")
	if err != nil {
		t.Fatalf("resolveAuditFilePath(rotated mirror) error = %v", err)
	}
	want = filepath.Join(filepath.Dir(basePath), "guard_audit.deny.jsonl.20260323T080000Z")
	if got != want {
		t.Fatalf("resolveAuditFilePath(rotated mirror) = %q, want %q", got, want)
	}

	if _, err := resolveAuditFilePath(basePath, "other.jsonl"); err == nil {
		t.Fatalf("resolveAuditFilePath() expected error for unrelated file")
	}
}
