package slack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type stubSendFileAPI struct {
	channelID      string
	threadTS       string
	filePath       string
	filename       string
	title          string
	initialComment string
	err            error
}

func (s *stubSendFileAPI) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	_ = ctx
	_ = channelID
	_ = messageTS
	_ = emoji
	return nil
}

func (s *stubSendFileAPI) SendFile(ctx context.Context, channelID, threadTS, filePath, filename, title, initialComment string) error {
	_ = ctx
	s.channelID = channelID
	s.threadTS = threadTS
	s.filePath = filePath
	s.filename = filename
	s.title = title
	s.initialComment = initialComment
	return s.err
}

func TestSlackSendFileToolExecute_DefaultContext(t *testing.T) {
	cacheDir := t.TempDir()
	localFile := filepath.Join(cacheDir, "nested", "result.txt")
	if err := os.MkdirAll(filepath.Dir(localFile), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(localFile, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubSendFileAPI{}
	tool := NewSendFileTool(api, "C123", "1739667600.000100", nil, cacheDir, 1024)
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":            "nested/result.txt",
		"initial_comment": "done",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out != "uploaded file: result.txt" {
		t.Fatalf("output = %q, want %q", out, "uploaded file: result.txt")
	}
	if api.channelID != "C123" || api.threadTS != "1739667600.000100" {
		t.Fatalf("target mismatch: channel=%q thread=%q", api.channelID, api.threadTS)
	}
	if api.filePath != localFile {
		t.Fatalf("file path = %q, want %q", api.filePath, localFile)
	}
	if api.filename != "result.txt" || api.title != "result.txt" {
		t.Fatalf("name/title mismatch: filename=%q title=%q", api.filename, api.title)
	}
	if api.initialComment != "done" {
		t.Fatalf("initial comment = %q, want %q", api.initialComment, "done")
	}
}

func TestSlackSendFileToolExecute_OverrideTargetAndMetadata(t *testing.T) {
	cacheDir := t.TempDir()
	localFile := filepath.Join(cacheDir, "report.txt")
	if err := os.WriteFile(localFile, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubSendFileAPI{}
	tool := NewSendFileTool(api, "C123", "1739667600.000100", map[string]bool{
		"C123": true,
		"C456": true,
	}, cacheDir, 1024)
	_, err := tool.Execute(context.Background(), map[string]any{
		"channel_id":      "C456",
		"thread_ts":       "1739667600.000200",
		"path":            "report.txt",
		"filename":        "final report.md",
		"title":           "Final Report",
		"initial_comment": "artifact",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if api.channelID != "C456" || api.threadTS != "1739667600.000200" {
		t.Fatalf("target mismatch: channel=%q thread=%q", api.channelID, api.threadTS)
	}
	if api.filename != "final_report.md" {
		t.Fatalf("filename = %q, want %q", api.filename, "final_report.md")
	}
	if api.title != "Final Report" {
		t.Fatalf("title = %q, want %q", api.title, "Final Report")
	}
}

func TestSlackSendFileToolExecute_ValidationAndAPIError(t *testing.T) {
	cacheDir := t.TempDir()
	localFile := filepath.Join(cacheDir, "result.txt")
	if err := os.WriteFile(localFile, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Run("missing channel_id without runtime context", func(t *testing.T) {
		api := &stubSendFileAPI{}
		tool := NewSendFileTool(api, "", "", nil, cacheDir, 1024)
		if _, err := tool.Execute(context.Background(), map[string]any{
			"path": "result.txt",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("missing path", func(t *testing.T) {
		api := &stubSendFileAPI{}
		tool := NewSendFileTool(api, "C123", "", nil, cacheDir, 1024)
		if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("unauthorized channel", func(t *testing.T) {
		api := &stubSendFileAPI{}
		tool := NewSendFileTool(api, "C123", "", map[string]bool{
			"C123": true,
		}, cacheDir, 1024)
		if _, err := tool.Execute(context.Background(), map[string]any{
			"channel_id": "C999",
			"path":       "result.txt",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("api error", func(t *testing.T) {
		api := &stubSendFileAPI{err: fmt.Errorf("slack files.upload failed: invalid_auth")}
		tool := NewSendFileTool(api, "C123", "", nil, cacheDir, 1024)
		if _, err := tool.Execute(context.Background(), map[string]any{
			"path": "result.txt",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})
}
