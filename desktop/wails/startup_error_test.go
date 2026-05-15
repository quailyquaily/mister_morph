//go:build wailsdesktop

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyDesktopStartupError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want desktopStartupErrorKind
	}{
		{
			name: "backend missing",
			err:  errors.New("cannot find runnable mistermorph backend binary; set MISTERMORPH_DESKTOP_BACKEND_BIN"),
			want: desktopStartupErrorBackendMissing,
		},
		{
			name: "backend exited",
			err:  errors.New("desktop console host exited before readiness: exit status 1"),
			want: desktopStartupErrorBackendExited,
		},
		{
			name: "backend timeout",
			err:  errors.New("desktop console host did not become ready before timeout (25s)"),
			want: desktopStartupErrorBackendTimeout,
		},
		{
			name: "backend start",
			err:  errors.New("start desktop console host: permission denied"),
			want: desktopStartupErrorBackendStart,
		},
		{
			name: "unknown",
			err:  errors.New("something else"),
			want: desktopStartupErrorUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDesktopStartupError(tc.err)
			if got.Kind != tc.want {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.want)
			}
			if got.Detail == "" {
				t.Fatal("detail is empty")
			}
		})
	}
}

func TestBuildDesktopStartupErrorHTML_EscapesErrorDetail(t *testing.T) {
	html := buildDesktopStartupErrorHTML(desktopStartupErrorInfo{
		Kind:    desktopStartupErrorUnknown,
		Title:   "Bad <title>",
		Message: "Bad <message>",
		Detail:  "<script>alert(1)</script>",
	})
	if strings.Contains(html, "<title>") || strings.Contains(html, "<message>") {
		t.Fatalf("HTML contains unescaped title or message: %s", html)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatalf("HTML contains unescaped detail: %s", html)
	}
	if !strings.Contains(html, "Copy Diagnostics") {
		t.Fatalf("HTML missing diagnostics action: %s", html)
	}
	if !strings.Contains(html, "Open Log") {
		t.Fatalf("HTML missing open log action: %s", html)
	}
	if !strings.Contains(html, "Log Unavailable") {
		t.Fatalf("HTML missing unavailable log state: %s", html)
	}
}

func TestBuildDesktopStartupErrorHTML_IncludesLogBindingWhenPathExists(t *testing.T) {
	html := buildDesktopStartupErrorHTML(desktopStartupErrorInfo{
		Kind:    desktopStartupErrorBackendExited,
		Title:   "Startup failed",
		Message: "Backend exited",
		Detail:  "exit status 1",
		LogPath: "/tmp/mistermorph-desktop.log",
	})
	if !strings.Contains(html, "main.App.OpenDesktopLog") {
		t.Fatalf("HTML missing desktop log binding: %s", html)
	}
	if !strings.Contains(html, "log_path") {
		t.Fatalf("diagnostics missing log_path: %s", html)
	}
}
