package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
