package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/larkapi"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestLarkAPISendPhotoUploadsImageThenSendsImageMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var sent larkSendMessageRequest
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkapi.TenantAccessTokenPath:
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "tenant_access_token": "tenant-token", "expire": 7200})
		case "/im/v1/images":
			if r.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("authorization = %q, want tenant token", r.Header.Get("Authorization"))
			}
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				t.Fatalf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("image_type"); got != "message" {
				t.Fatalf("image_type = %q, want message", got)
			}
			file, _, err := r.FormFile("image")
			if err != nil {
				t.Fatalf("FormFile(image) error = %v", err)
			}
			raw, _ := io.ReadAll(file)
			_ = file.Close()
			if string(raw) != "png-data" {
				t.Fatalf("image bytes = %q, want png-data", string(raw))
			}
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]string{"image_key": "img_123"}})
		case "/im/v1/messages":
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", got)
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &sent); err != nil {
				t.Fatalf("decode sent body: %v", err)
			}
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]string{"message_id": "om_123", "chat_id": "oc_123"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	api := newLarkAPI(server.Client, server.URL, larkapi.NewTenantTokenClient(server.Client, server.URL, "app_id", "app_secret"))
	if err := api.sendPhoto(context.Background(), "oc_123", imagePath, "photo.png", ""); err != nil {
		t.Fatalf("sendPhoto() error = %v", err)
	}
	if sent.ReceiveID != "oc_123" {
		t.Fatalf("receive_id = %q, want oc_123", sent.ReceiveID)
	}
	if sent.MsgType != "image" {
		t.Fatalf("msg_type = %q, want image", sent.MsgType)
	}
	if sent.Content != `{"image_key":"img_123"}` {
		t.Fatalf("content = %q, want image_key content", sent.Content)
	}
}

func TestLarkAPISendVoiceUploadsOpusAndSendsAudioMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	audioPath := filepath.Join(dir, "voice.opus")
	if err := os.WriteFile(audioPath, []byte("opus-data"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var sent larkSendMessageRequest
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkapi.TenantAccessTokenPath:
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "tenant_access_token": "tenant-token", "expire": 7200})
		case "/im/v1/files":
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				t.Fatalf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("file_type"); got != "opus" {
				t.Fatalf("file_type = %q, want opus", got)
			}
			if got := r.FormValue("file_name"); got != "voice.opus" {
				t.Fatalf("file_name = %q, want voice.opus", got)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("FormFile(file) error = %v", err)
			}
			raw, _ := io.ReadAll(file)
			_ = file.Close()
			if string(raw) != "opus-data" {
				t.Fatalf("file bytes = %q, want opus-data", string(raw))
			}
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]string{"file_key": "file_123"}})
		case "/im/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &sent); err != nil {
				t.Fatalf("decode sent body: %v", err)
			}
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]string{"message_id": "om_123", "chat_id": "oc_123"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	api := newLarkAPI(server.Client, server.URL, larkapi.NewTenantTokenClient(server.Client, server.URL, "app_id", "app_secret"))
	if err := api.sendVoice(context.Background(), "oc_123", audioPath, "voice.opus"); err != nil {
		t.Fatalf("sendVoice() error = %v", err)
	}
	if sent.MsgType != "audio" {
		t.Fatalf("msg_type = %q, want audio", sent.MsgType)
	}
	if sent.Content != `{"file_key":"file_123"}` {
		t.Fatalf("content = %q, want file_key content", sent.Content)
	}
}

func TestLarkAPISetEmojiReaction(t *testing.T) {
	t.Parallel()

	var body map[string]struct {
		EmojiType string `json:"emoji_type"`
	}
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkapi.TenantAccessTokenPath:
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "tenant_access_token": "tenant-token", "expire": 7200})
		case "/im/v1/messages/om_123/reactions":
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode reaction body: %v", err)
			}
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]string{"reaction_id": "reaction_123"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	api := newLarkAPI(server.Client, server.URL, larkapi.NewTenantTokenClient(server.Client, server.URL, "app_id", "app_secret"))
	if err := api.setEmojiReaction(context.Background(), "om_123", "THUMBSUP"); err != nil {
		t.Fatalf("setEmojiReaction() error = %v", err)
	}
	got := body["reaction_type"].EmojiType
	if got != "THUMBSUP" {
		t.Fatalf("emoji_type = %q, want THUMBSUP", got)
	}
}

func TestLarkAPIUserAvatarURL(t *testing.T) {
	t.Parallel()

	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkapi.TenantAccessTokenPath:
			writeTestJSON(w, map[string]any{"code": 0, "msg": "success", "tenant_access_token": "tenant-token", "expire": 7200})
		case "/contact/v3/users/ou_alice":
			if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
				t.Fatalf("user_id_type = %q, want open_id", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q", got)
			}
			writeTestJSON(w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"user": map[string]any{"avatar": map[string]string{
					"avatar_72":  "https://cdn.example/alice-72.png",
					"avatar_240": "https://cdn.example/alice-240.png",
				}}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	api := newLarkAPI(server.Client, server.URL, larkapi.NewTenantTokenClient(server.Client, server.URL, "app_id", "app_secret"))
	got, err := api.userAvatarURL(context.Background(), "ou_alice")
	if err != nil {
		t.Fatalf("userAvatarURL() error = %v", err)
	}
	if got != "https://cdn.example/alice-240.png" {
		t.Fatalf("userAvatarURL() = %q, want 240px avatar", got)
	}
}

func writeTestJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
