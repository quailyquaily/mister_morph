package logutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func TestResolveFileLogDir_DefaultsUnderStateDir(t *testing.T) {
	got := ResolveFileLogDir(filepath.Join(t.TempDir(), "state"), "")
	if !strings.HasSuffix(got, filepath.Join("state", "logs")) {
		t.Fatalf("ResolveFileLogDir() = %q, want to end with state/logs", got)
	}
}

func TestResolveFileLogDir_CustomDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom-logs")
	got := ResolveFileLogDir("", dir)
	if got != pathutil.ExpandHomePath(dir) {
		t.Fatalf("ResolveFileLogDir() = %q, want %q", got, pathutil.ExpandHomePath(dir))
	}
}

func TestParseFileLogMaxAge(t *testing.T) {
	got, err := ParseFileLogMaxAge("168h")
	if err != nil {
		t.Fatalf("ParseFileLogMaxAge() error = %v", err)
	}
	if got != 7*24*time.Hour {
		t.Fatalf("ParseFileLogMaxAge() = %s, want 168h", got)
	}
	if _, err := ParseFileLogMaxAge("0s"); err == nil {
		t.Fatalf("ParseFileLogMaxAge(0s) expected error")
	}
}

func TestLoggerFromConfigRejectsUnknownLevel(t *testing.T) {
	_, err := LoggerFromConfig(LoggerConfig{Level: "verbose", FileDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "unknown logging.level") {
		t.Fatalf("LoggerFromConfig() error = %v, want unknown-level error", err)
	}
}

func TestDailyLogWriter_RotatesByLocalDate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 4, 24, 23, 59, 0, 0, time.Local)
	writer, err := newDailyLogWriter(dailyLogWriterConfig{
		Dir:      dir,
		MaxAge:   DefaultFileLogMaxAge,
		Now:      func() time.Time { return now },
		FileBase: "test-",
	})
	if err != nil {
		t.Fatalf("newDailyLogWriter() error = %v", err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("first\n")); err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := writer.Write([]byte("second\n")); err != nil {
		t.Fatalf("Write(second) error = %v", err)
	}

	first, err := os.ReadFile(filepath.Join(dir, "test-2026-04-24.jsonl"))
	if err != nil {
		t.Fatalf("read first file: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "test-2026-04-25.jsonl"))
	if err != nil {
		t.Fatalf("read second file: %v", err)
	}
	if strings.TrimSpace(string(first)) != "first" || strings.TrimSpace(string(second)) != "second" {
		t.Fatalf("rotated contents = %q / %q", first, second)
	}
}

func TestDailyLogWriter_CleansByFilenameDateOnly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"test-2026-04-01.jsonl": "old\n",
		"test-2026-04-23.jsonl": "new\n",
		"notes.txt":             "keep\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	writer, err := newDailyLogWriter(dailyLogWriterConfig{
		Dir:      dir,
		MaxAge:   7 * 24 * time.Hour,
		Now:      func() time.Time { return time.Date(2026, 4, 24, 12, 0, 0, 0, time.Local) },
		FileBase: "test-",
	})
	if err != nil {
		t.Fatalf("newDailyLogWriter() error = %v", err)
	}
	defer writer.Close()

	if _, err := os.Stat(filepath.Join(dir, "test-2026-04-01.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old log stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test-2026-04-23.jsonl")); err != nil {
		t.Fatalf("new log should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatalf("unrelated file should remain: %v", err)
	}
}
