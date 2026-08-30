package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestTelegramAPIContactAvatar(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botsecret/getUserProfilePhotos":
			if got := r.URL.Query().Get("user_id"); got != "1234" {
				t.Fatalf("user_id = %q, want 1234", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{"photos": [][]map[string]any{{
					{"file_id": "small", "width": 64, "height": 64},
					{"file_id": "large", "width": 320, "height": 320},
				}}},
			})
		case "/botsecret/getFile":
			if got := r.URL.Query().Get("file_id"); got != "large" {
				t.Fatalf("file_id = %q, want large", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "result": map[string]any{"file_path": "avatars/user.png"},
			})
		case "/file/botsecret/avatars/user.png":
			_, _ = w.Write(png)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	got, found, err := newTelegramAPI(server.Client, server.URL, "secret").contactAvatar(context.Background(), 1234)
	if err != nil {
		t.Fatalf("contactAvatar() error = %v", err)
	}
	if !found || string(got) != string(png) {
		t.Fatalf("contactAvatar() = (%x, %v), want (%x, true)", got, found, png)
	}
}

func TestTelegramAPIContactAvatarMissing(t *testing.T) {
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "result": map[string]any{"photos": []any{}},
		})
	}))

	got, found, err := newTelegramAPI(server.Client, server.URL, "secret").contactAvatar(context.Background(), 1234)
	if err != nil {
		t.Fatalf("contactAvatar() error = %v", err)
	}
	if found || got != nil {
		t.Fatalf("contactAvatar() = (%x, %v), want (nil, false)", got, found)
	}
}

func TestTelegramAPIContactAvatarErrorDoesNotExposeToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := newTelegramAPI(http.DefaultClient, "https://api.telegram.invalid", "super-secret-token").contactAvatar(ctx, 1234)
	if err == nil {
		t.Fatal("contactAvatar() error = nil")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("contactAvatar() error exposes token: %v", err)
	}
}
