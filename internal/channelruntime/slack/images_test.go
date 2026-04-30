package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadSlackImageToCache(t *testing.T) {
	t.Parallel()

	raw := []byte("png-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xoxb-token" {
			t.Fatalf("authorization = %q, want bot token", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	api := newSlackAPI(srv.Client(), "", "xoxb-token", "xapp-token")
	path, err := downloadSlackImageToCache(context.Background(), api, t.TempDir(), slackEventFile{
		ID:                 "F111",
		Mimetype:           "image/png",
		URLPrivateDownload: srv.URL + "/file",
		Size:               int64(len(raw)),
	}, 1024*1024)
	if err != nil {
		t.Fatalf("downloadSlackImageToCache() error = %v", err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("extension = %q, want .png", filepath.Ext(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloadSlackImageToCacheUsesFilesInfoForSlackConnectPlaceholder(t *testing.T) {
	t.Parallel()

	raw := []byte("png-data")
	var filesInfoCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files.info":
			filesInfoCalled = true
			if r.Header.Get("Authorization") != "Bearer xoxb-token" {
				t.Fatalf("authorization = %q, want bot token", r.Header.Get("Authorization"))
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "file=F333") {
				t.Fatalf("files.info body = %q, want file=F333", string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"file": map[string]any{
					"id":                   "F333",
					"name":                 "photo.png",
					"mimetype":             "image/png",
					"url_private_download": srvURL(r) + "/file",
					"size":                 len(raw),
				},
			})
		case "/file":
			if r.Header.Get("Authorization") != "Bearer xoxb-token" {
				t.Fatalf("download authorization = %q, want bot token", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(raw)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	api := newSlackAPI(srv.Client(), srv.URL, "xoxb-token", "xapp-token")
	path, err := downloadSlackImageToCache(context.Background(), api, t.TempDir(), slackEventFile{
		ID:         "F333",
		Mode:       "file_access",
		FileAccess: "check_file_info",
	}, 1024*1024)
	if err != nil {
		t.Fatalf("downloadSlackImageToCache() error = %v", err)
	}
	if !filesInfoCalled {
		t.Fatalf("files.info was not called")
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("extension = %q, want .png", filepath.Ext(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloadSlackImageToCacheRejectsUnknownType(t *testing.T) {
	t.Parallel()

	api := newSlackAPI(http.DefaultClient, "", "xoxb-token", "xapp-token")
	_, err := downloadSlackImageToCache(context.Background(), api, t.TempDir(), slackEventFile{
		ID:                 "F111",
		Mimetype:           "application/octet-stream",
		URLPrivateDownload: "https://files.slack.test/file",
		Size:               10,
	}, 1024*1024)
	if err == nil {
		t.Fatalf("downloadSlackImageToCache() expected error")
	}
}

func srvURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
