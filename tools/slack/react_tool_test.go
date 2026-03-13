package slack

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type stubReactAPI struct {
	channelID string
	messageTS string
	emoji     string
	err       error
}

func (s *stubReactAPI) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	_ = ctx
	s.channelID = channelID
	s.messageTS = messageTS
	s.emoji = emoji
	return s.err
}

func (s *stubReactAPI) SendFile(ctx context.Context, channelID, threadTS, filePath, filename, title, initialComment string) error {
	_ = ctx
	_ = channelID
	_ = threadTS
	_ = filePath
	_ = filename
	_ = title
	_ = initialComment
	return nil
}

func TestSlackReactToolExecute_DefaultTarget(t *testing.T) {
	api := &stubReactAPI{}
	tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup"})
	out, err := tool.Execute(context.Background(), map[string]any{
		"emoji": ":thumbsup:",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out != "reacted with :thumbsup:" {
		t.Fatalf("output = %q, want %q", out, "reacted with :thumbsup:")
	}
	if api.channelID != "C123" || api.messageTS != "1739667600.000100" || api.emoji != "thumbsup" {
		t.Fatalf("api payload mismatch: channel=%q ts=%q emoji=%q", api.channelID, api.messageTS, api.emoji)
	}
	last := tool.LastReaction()
	if last == nil {
		t.Fatalf("LastReaction() = nil")
	}
	if last.ChannelID != "C123" || last.MessageTS != "1739667600.000100" || last.Emoji != "thumbsup" {
		t.Fatalf("last reaction mismatch: %+v", *last)
	}
}

func TestSlackReactToolExecute_OverrideTarget(t *testing.T) {
	api := &stubReactAPI{}
	tool := NewReactTool(api, "C123", "1739667600.000100", map[string]bool{
		"C123": true,
		"C456": true,
	}, []string{"thumbsup"})
	out, err := tool.Execute(context.Background(), map[string]any{
		"channel_id": "C456",
		"message_ts": "1739667600.000200",
		"emoji":      "thumbsup",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out != "reacted with :thumbsup:" {
		t.Fatalf("output = %q, want %q", out, "reacted with :thumbsup:")
	}
	if api.channelID != "C456" || api.messageTS != "1739667600.000200" || api.emoji != "thumbsup" {
		t.Fatalf("api payload mismatch: channel=%q ts=%q emoji=%q", api.channelID, api.messageTS, api.emoji)
	}
}

func TestSlackReactToolExecute_ValidationAndAPIError(t *testing.T) {
	t.Run("missing emoji", func(t *testing.T) {
		api := &stubReactAPI{}
		tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("invalid emoji name", func(t *testing.T) {
		api := &stubReactAPI{}
		tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{
			"emoji": "thumbs up",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("emoji not in workspace list", func(t *testing.T) {
		api := &stubReactAPI{}
		tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{
			"emoji": "older_woman",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("missing runtime channel context", func(t *testing.T) {
		api := &stubReactAPI{}
		tool := NewReactTool(api, "", "1739667600.000100", nil, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{
			"emoji": "thumbsup",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("unauthorized channel", func(t *testing.T) {
		api := &stubReactAPI{}
		tool := NewReactTool(api, "C123", "1739667600.000100", map[string]bool{
			"C123": true,
		}, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{
			"channel_id": "C999",
			"emoji":      "thumbsup",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("api error", func(t *testing.T) {
		api := &stubReactAPI{err: fmt.Errorf("already_reacted")}
		tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup"})
		if _, err := tool.Execute(context.Background(), map[string]any{
			"emoji": "thumbsup",
		}); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestSlackReactToolEmojiGuidance(t *testing.T) {
	api := &stubReactAPI{}
	tool := NewReactTool(api, "C123", "1739667600.000100", nil, []string{"thumbsup", "older_woman"})
	if got := tool.Description(); got == "" || !containsAll(got, []string{"Slack-style emoji name", "thumbsup", "older_woman"}) {
		t.Fatalf("description guidance missing, got: %q", got)
	}
	schema := tool.ParameterSchema()
	if !containsAll(schema, []string{"Slack-style emoji name", "thumbsup", "older_woman"}) {
		t.Fatalf("parameter schema guidance missing, got: %q", schema)
	}
}

func containsAll(s string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
