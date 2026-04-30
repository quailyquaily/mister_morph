package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSlackAPIUserIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/users.info")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
			t.Fatalf("authorization = %q", got)
		}
		if got := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q", got)
		}
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read payload: %v", err)
		}
		payload, err := url.ParseQuery(string(rawBody))
		if err != nil {
			t.Fatalf("parse payload: %v", err)
		}
		if got := strings.TrimSpace(payload.Get("user")); got != "U123" {
			t.Fatalf("user = %q, want %q", got, "U123")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"user": map[string]any{
				"id":   "U123",
				"name": "alice",
				"profile": map[string]any{
					"display_name": "Alice",
					"real_name":    "Alice Real",
				},
			},
		})
	}))
	defer server.Close()

	api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
	identity, err := api.userIdentity(context.Background(), "U123")
	if err != nil {
		t.Fatalf("userIdentity() error = %v", err)
	}
	if identity.UserID != "U123" {
		t.Fatalf("user id = %q, want %q", identity.UserID, "U123")
	}
	if identity.Username != "alice" {
		t.Fatalf("username = %q, want %q", identity.Username, "alice")
	}
	if identity.DisplayName != "Alice" {
		t.Fatalf("display name = %q, want %q", identity.DisplayName, "Alice")
	}
}

func TestSlackAPIUserIdentityFallbackAndError(t *testing.T) {
	t.Run("fallback to username for display name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"user": map[string]any{
					"id":   "U222",
					"name": "bob",
					"profile": map[string]any{
						"display_name": "",
						"real_name":    "",
					},
				},
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		identity, err := api.userIdentity(context.Background(), "U222")
		if err != nil {
			t.Fatalf("userIdentity() error = %v", err)
		}
		if identity.DisplayName != "bob" {
			t.Fatalf("display name = %q, want %q", identity.DisplayName, "bob")
		}
	})

	t.Run("fallback to user id when username is empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"user": map[string]any{
					"id":   "",
					"name": "",
					"profile": map[string]any{
						"display_name": "",
						"real_name":    "",
					},
				},
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		identity, err := api.userIdentity(context.Background(), "U333")
		if err != nil {
			t.Fatalf("userIdentity() error = %v", err)
		}
		if identity.UserID != "U333" {
			t.Fatalf("user id = %q, want %q", identity.UserID, "U333")
		}
		if identity.Username != "U333" {
			t.Fatalf("username = %q, want %q", identity.Username, "U333")
		}
		if identity.DisplayName != "U333" {
			t.Fatalf("display name = %q, want %q", identity.DisplayName, "U333")
		}
	})

	t.Run("fallback to user id on user_not_found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "user_not_found",
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		identity, err := api.userIdentity(context.Background(), "U404")
		if err != nil {
			t.Fatalf("userIdentity() error = %v", err)
		}
		if identity.UserID != "U404" {
			t.Fatalf("user id = %q, want %q", identity.UserID, "U404")
		}
		if identity.Username != "U404" {
			t.Fatalf("username = %q, want %q", identity.Username, "U404")
		}
		if identity.DisplayName != "U404" {
			t.Fatalf("display name = %q, want %q", identity.DisplayName, "U404")
		}
	})

	t.Run("slack api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_auth",
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		_, err := api.userIdentity(context.Background(), "U404")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "invalid_auth") {
			t.Fatalf("error = %v, want invalid_auth", err)
		}
	})
}

func TestSlackAPIAddReaction(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/reactions.add" {
				t.Fatalf("path = %q, want %q", r.URL.Path, "/reactions.add")
			}
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
				t.Fatalf("authorization = %q", got)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if got := strings.TrimSpace(payload["channel"].(string)); got != "C123" {
				t.Fatalf("channel = %q, want %q", got, "C123")
			}
			if got := strings.TrimSpace(payload["timestamp"].(string)); got != "1739667600.000100" {
				t.Fatalf("timestamp = %q, want %q", got, "1739667600.000100")
			}
			if got := strings.TrimSpace(payload["name"].(string)); got != "thumbsup" {
				t.Fatalf("name = %q, want %q", got, "thumbsup")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		if err := api.addReaction(context.Background(), "C123", "1739667600.000100", "thumbsup"); err != nil {
			t.Fatalf("addReaction() error = %v", err)
		}
	})

	t.Run("already_reacted treated as success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "already_reacted",
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		if err := api.addReaction(context.Background(), "C123", "1739667600.000100", "thumbsup"); err != nil {
			t.Fatalf("addReaction() error = %v", err)
		}
	})

	t.Run("slack error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_name",
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		err := api.addReaction(context.Background(), "C123", "1739667600.000100", "not-valid")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "invalid_name") {
			t.Fatalf("error = %v, want invalid_name", err)
		}
	})
}

func TestSlackAPIPostMessageWithResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/chat.postMessage")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
			t.Fatalf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if got := strings.TrimSpace(payload["channel"].(string)); got != "C123" {
			t.Fatalf("channel = %q, want %q", got, "C123")
		}
		if got := strings.TrimSpace(payload["text"].(string)); got != "working..." {
			t.Fatalf("text = %q, want %q", got, "working...")
		}
		if got := strings.TrimSpace(payload["thread_ts"].(string)); got != "1739667600.000100" {
			t.Fatalf("thread_ts = %q, want %q", got, "1739667600.000100")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": "C123",
			"ts":      "1739667601.000200",
		})
	}))
	defer server.Close()

	api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
	ref, err := api.postMessageWithResult(context.Background(), "C123", "working...", "1739667600.000100")
	if err != nil {
		t.Fatalf("postMessageWithResult() error = %v", err)
	}
	if ref.ChannelID != "C123" {
		t.Fatalf("channel_id = %q, want C123", ref.ChannelID)
	}
	if ref.MessageTS != "1739667601.000200" {
		t.Fatalf("message_ts = %q, want 1739667601.000200", ref.MessageTS)
	}
}

func TestSlackAPIUpdateMessage(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat.update" {
				t.Fatalf("path = %q, want %q", r.URL.Path, "/chat.update")
			}
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
				t.Fatalf("authorization = %q", got)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if got := strings.TrimSpace(payload["channel"].(string)); got != "C123" {
				t.Fatalf("channel = %q, want %q", got, "C123")
			}
			if got := strings.TrimSpace(payload["ts"].(string)); got != "1739667601.000200" {
				t.Fatalf("ts = %q, want %q", got, "1739667601.000200")
			}
			if got := strings.TrimSpace(payload["text"].(string)); got != "done" {
				t.Fatalf("text = %q, want %q", got, "done")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		if err := api.updateMessage(context.Background(), "C123", "1739667601.000200", "done"); err != nil {
			t.Fatalf("updateMessage() error = %v", err)
		}
	})

	t.Run("slack error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "message_not_found",
			})
		}))
		defer server.Close()

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		err := api.updateMessage(context.Background(), "C123", "1739667601.000200", "done")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "message_not_found") {
			t.Fatalf("error = %v, want message_not_found", err)
		}
	})
}

func TestSlackAPIUploadFile(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		var gotFileContent string
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/files.getUploadURLExternal":
				if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
					t.Fatalf("authorization = %q", got)
				}
				if got := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.Contains(got, "application/x-www-form-urlencoded") {
					t.Fatalf("content-type = %q", got)
				}
				rawBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read payload: %v", err)
				}
				payload, err := url.ParseQuery(string(rawBody))
				if err != nil {
					t.Fatalf("parse payload: %v", err)
				}
				if got := strings.TrimSpace(payload.Get("filename")); got != "result.txt" {
					t.Fatalf("filename = %q, want %q", got, "result.txt")
				}
				if got := strings.TrimSpace(payload.Get("length")); got != "11" {
					t.Fatalf("length = %q, want %q", got, "11")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":         true,
					"upload_url": server.URL + "/upload/v1/mock",
					"file_id":    "F123",
				})
			case "/upload/v1/mock":
				if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "" {
					t.Fatalf("authorization = %q, want empty", got)
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read upload body: %v", err)
				}
				gotFileContent = string(raw)
				_, _ = w.Write([]byte("ok"))
			case "/files.completeUploadExternal":
				if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
					t.Fatalf("authorization = %q", got)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode payload: %v", err)
				}
				if got := strings.TrimSpace(payload["channel_id"].(string)); got != "C123" {
					t.Fatalf("channel_id = %q, want %q", got, "C123")
				}
				if got := strings.TrimSpace(payload["thread_ts"].(string)); got != "1739667600.000100" {
					t.Fatalf("thread_ts = %q, want %q", got, "1739667600.000100")
				}
				if got := strings.TrimSpace(payload["initial_comment"].(string)); got != "done" {
					t.Fatalf("initial_comment = %q, want %q", got, "done")
				}
				files, ok := payload["files"].([]any)
				if !ok || len(files) != 1 {
					t.Fatalf("files payload = %#v, want one item", payload["files"])
				}
				fileMeta, ok := files[0].(map[string]any)
				if !ok {
					t.Fatalf("files[0] payload = %#v, want map", files[0])
				}
				if got := strings.TrimSpace(fileMeta["id"].(string)); got != "F123" {
					t.Fatalf("file id = %q, want %q", got, "F123")
				}
				if got := strings.TrimSpace(fileMeta["title"].(string)); got != "Result" {
					t.Fatalf("file title = %q, want %q", got, "Result")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer server.Close()

		tmp := t.TempDir()
		localFile := filepath.Join(tmp, "result.txt")
		if err := os.WriteFile(localFile, []byte("hello slack"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		if err := api.uploadFile(context.Background(), "C123", "1739667600.000100", localFile, "result.txt", "Result", "done"); err != nil {
			t.Fatalf("uploadFile() error = %v", err)
		}
		if gotFileContent != "hello slack" {
			t.Fatalf("uploaded file content = %q, want %q", gotFileContent, "hello slack")
		}
	})

	t.Run("complete upload slack error", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/files.getUploadURLExternal":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":         true,
					"upload_url": server.URL + "/upload/v1/mock",
					"file_id":    "F123",
				})
			case "/upload/v1/mock":
				_, _ = w.Write([]byte("ok"))
			case "/files.completeUploadExternal":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":    false,
					"error": "missing_scope",
				})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer server.Close()

		tmp := t.TempDir()
		localFile := filepath.Join(tmp, "result.txt")
		if err := os.WriteFile(localFile, []byte("hello slack"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		err := api.uploadFile(context.Background(), "C123", "", localFile, "", "", "")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "missing_scope") {
			t.Fatalf("error = %v, want missing_scope", err)
		}
	})

	t.Run("empty file rejected before slack call", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("unexpected slack request: %s", r.URL.Path)
		}))
		defer server.Close()

		tmp := t.TempDir()
		localFile := filepath.Join(tmp, "empty.txt")
		if err := os.WriteFile(localFile, nil, 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}

		api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
		err := api.uploadFile(context.Background(), "C123", "", localFile, "", "", "")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "file length is invalid") {
			t.Fatalf("error = %v, want file length is invalid", err)
		}
	})
}

func TestSlackAPIListEmojiNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emoji.list" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/emoji.list")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
			t.Fatalf("authorization = %q", got)
		}
		if got := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q", got)
		}
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read payload: %v", err)
		}
		payload, err := url.ParseQuery(string(rawBody))
		if err != nil {
			t.Fatalf("parse payload: %v", err)
		}
		if got := strings.TrimSpace(payload.Get("include_categories")); got != "true" {
			t.Fatalf("include_categories = %q, want %q", got, "true")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"emoji": map[string]any{
				"party_parrot": "https://example.com/parrot.png",
				"shipit":       "alias:party_parrot",
			},
			"categories": []map[string]any{
				{
					"name":        "Smileys & Emotion",
					"emoji_names": []string{"thumbsup", "older_woman"},
				},
			},
		})
	}))
	defer server.Close()

	api := newSlackAPI(server.Client(), server.URL, "xoxb-test", "xapp-test")
	names, err := api.listEmojiNames(context.Background())
	if err != nil {
		t.Fatalf("listEmojiNames() error = %v", err)
	}
	want := []string{"older_woman", "party_parrot", "shipit", "thumbsup"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}
