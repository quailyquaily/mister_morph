package slackclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestUpdateMessageWithBlocksSendsBlocksPayload(t *testing.T) {
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.update" {
			t.Fatalf("path = %q, want /chat.update", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if got := strings.TrimSpace(payload["channel"].(string)); got != "C123" {
			t.Fatalf("channel = %q, want C123", got)
		}
		if got := strings.TrimSpace(payload["ts"].(string)); got != "1739667601.000200" {
			t.Fatalf("ts = %q, want 1739667601.000200", got)
		}
		blocks, ok := payload["blocks"].([]any)
		if !ok || len(blocks) != 1 {
			t.Fatalf("blocks = %#v, want one block", payload["blocks"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	client := New(server.Client, server.URL, "xoxb-test")
	err := client.UpdateMessageWithBlocks(context.Background(), "C123", "1739667601.000200", "fallback", []Block{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*working...*",
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateMessageWithBlocks() error = %v", err)
	}
}

func TestPostMessageWithBlocksSendsBlocksPayload(t *testing.T) {
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("path = %q, want /chat.postMessage", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if got := strings.TrimSpace(payload["channel"].(string)); got != "C123" {
			t.Fatalf("channel = %q, want C123", got)
		}
		if got := strings.TrimSpace(payload["thread_ts"].(string)); got != "1739667600.000100" {
			t.Fatalf("thread_ts = %q, want 1739667600.000100", got)
		}
		blocks, ok := payload["blocks"].([]any)
		if !ok || len(blocks) != 1 {
			t.Fatalf("blocks = %#v, want one block", payload["blocks"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C123", "ts": "1739667601.000200"})
	}))

	client := New(server.Client, server.URL, "xoxb-test")
	err := client.PostMessageWithBlocks(context.Background(), "C123", "fallback", "1739667600.000100", []Block{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*approval required*",
			},
		},
	})
	if err != nil {
		t.Fatalf("PostMessageWithBlocks() error = %v", err)
	}
}
