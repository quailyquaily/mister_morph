package filecache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupAppliesAllLimitsAndKeepsProtectedFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, size int, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
		modTime := now.Add(-age)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("Chtimes(%q) error = %v", name, err)
		}
		return path
	}

	expired := write("expired.txt", 1, 4*time.Hour)
	oldest := write("oldest.txt", 4, 2*time.Hour)
	protected := write("protected.txt", 4, time.Hour)
	newest := write("newest.txt", 4, 30*time.Minute)

	err := Cleanup(dir, Limits{
		MaxAge:        3 * time.Hour,
		MaxFiles:      2,
		MaxTotalBytes: 8,
	}, map[string]bool{protected: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	for _, removed := range []string{expired, oldest} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("Stat(%q) error = %v, want not exist", removed, err)
		}
	}
	for _, kept := range []string{protected, newest} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("Stat(%q) error = %v, want file kept", kept, err)
		}
	}
}

func TestCleanupDoesNothingWhenLimitsAreDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(path, []byte("kept"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := Cleanup(dir, Limits{}, nil); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat() error = %v, want file kept", err)
	}
}
