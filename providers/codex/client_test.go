package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestPrepareCodexRequestMovesSystemMessagesToInstructions(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "developer", Content: "developer prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"openai": structs.JSONMap{
				"parallel_tool_calls": true,
			},
		},
	}

	got, err := prepareCodexRequest(req)
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	options, ok := got.Parameters["openai"].(structs.JSONMap)
	if !ok {
		t.Fatalf("openai options type = %T", got.Parameters["openai"])
	}
	if options["instructions"] != "system prompt\n\ndeveloper prompt" {
		t.Fatalf("instructions = %q", options["instructions"])
	}
	if options["store"] != false {
		t.Fatalf("store = %#v", options["store"])
	}
	if options["parallel_tool_calls"] != true {
		t.Fatalf("existing option lost: %#v", options["parallel_tool_calls"])
	}
}

func TestPrepareCodexRequestLeavesCompatibilityFilteringToUniai(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"max_tokens":  1024,
			"temperature": 0.2,
			"openai": structs.JSONMap{
				"prompt_cache_key":        "mistermorph",
				"prompt_cache_retention":  "24h",
				"prompt_cache_options":    map[string]any{"mode": "explicit"},
				"max_output_tokens":       512,
				"reasoning_budget_tokens": 4096,
			},
		},
	})
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	options, ok := got.Parameters["openai"].(structs.JSONMap)
	if !ok {
		t.Fatalf("openai options type = %T", got.Parameters["openai"])
	}
	if got.Parameters["max_tokens"] != 1024 || got.Parameters["temperature"] != 0.2 {
		t.Fatalf("shared compatibility fields changed: %#v", got.Parameters)
	}
	for _, key := range []string{
		"prompt_cache_key",
		"prompt_cache_retention",
		"prompt_cache_options",
		"max_output_tokens",
		"reasoning_budget_tokens",
	} {
		if _, ok := options[key]; !ok {
			t.Fatalf("%s should be preserved for uniai filtering: %#v", key, options)
		}
	}
}

func TestPrepareCodexRequestLeavesJSONModeToUniai(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"max_tokens":  1024,
			"temperature": 0,
			"openai": structs.JSONMap{
				"max_output_tokens": 512,
			},
		},
		Tools: []llm.Tool{{
			Name:           "read_file",
			ParametersJSON: `{"type":"object","properties":{"path":{"type":"string"}}}`,
		}},
	})
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	options, ok := got.Parameters["openai"].(structs.JSONMap)
	if !ok {
		t.Fatalf("openai options type = %T", got.Parameters["openai"])
	}
	if _, ok := options["response_format"]; ok {
		t.Fatalf("response_format should be added by the shared uniai adapter: %#v", options)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if got.Parameters["max_tokens"] != 1024 || got.Parameters["temperature"] != 0 {
		t.Fatalf("shared options changed: %#v", got.Parameters)
	}
	if options["max_output_tokens"] != 512 {
		t.Fatalf("max_output_tokens changed: %#v", options)
	}
}

func TestClientUsesOpenAICodexTransport(t *testing.T) {
	stateDir := t.TempDir()
	if err := codexauth.WriteToken(stateDir, codexauth.Token{
		AccessToken: "access-token",
		AccountID:   "account-id",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}

	type capturedRequest struct {
		Path      string
		Auth      string
		AccountID string
		Payload   map[string]any
		Err       error
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		select {
		case captured <- capturedRequest{
			Path:      r.URL.Path,
			Auth:      r.Header.Get("Authorization"),
			AccountID: r.Header.Get("ChatGPT-Account-ID"),
			Payload:   payload,
			Err:       err,
		}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop after capture"}}`))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint: server.URL,
		Model:    "gpt-5.5",
		StateDir: stateDir,
	})
	_, chatErr := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"temperature": 0.2,
			"max_tokens":  512,
			"openai": structs.JSONMap{
				"prompt_cache_options": map[string]any{"mode": "explicit"},
			},
		},
	})

	select {
	case got := <-captured:
		if got.Err != nil {
			t.Fatalf("decode request: %v", got.Err)
		}
		if got.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", got.Path)
		}
		if got.Auth != "Bearer access-token" || got.AccountID != "account-id" {
			t.Fatalf("auth headers = authorization %q, account %q", got.Auth, got.AccountID)
		}
		for _, key := range []string{"temperature", "max_output_tokens", "prompt_cache_options"} {
			if _, ok := got.Payload[key]; ok {
				t.Fatalf("unexpected %s in Codex request: %#v", key, got.Payload[key])
			}
		}
		if got.Payload["instructions"] != "system prompt" || got.Payload["store"] != false {
			t.Fatalf("Codex request metadata = %#v", got.Payload)
		}
	case <-time.After(time.Second):
		t.Fatalf("Codex request did not reach the configured endpoint: %v", chatErr)
	}
}

func TestClientUsesAPIKeyForCustomEndpoint(t *testing.T) {
	captured := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case captured <- r.Header.Clone():
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop after capture"}}`))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint: server.URL,
		APIKey:   "provider-key",
		Model:    "gpt-5.5",
		StateDir: t.TempDir(),
		Headers:  map[string]string{"ChatGPT-Account-ID": "must-not-be-sent"},
	})
	_, chatErr := client.Chat(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "hello"},
	}})

	select {
	case headers := <-captured:
		if got := headers.Get("Authorization"); got != "Bearer provider-key" {
			t.Fatalf("Authorization = %q, want provider API key", got)
		}
		if got := headers.Get("ChatGPT-Account-ID"); got != "" {
			t.Fatalf("ChatGPT-Account-ID = %q, want empty for API key auth", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("Codex request did not reach the configured endpoint: %v", chatErr)
	}
}

func TestPrepareCodexRequestRequiresInstructions(t *testing.T) {
	_, err := prepareCodexRequest(llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareCodexRequestCapsInstructions(t *testing.T) {
	longPrompt := strings.Repeat("a", codexInstructionsMaxBytes) + "尾部"
	got, err := prepareCodexRequest(llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: longPrompt},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	options, ok := got.Parameters["openai"].(structs.JSONMap)
	if !ok {
		t.Fatalf("openai options type = %T", got.Parameters["openai"])
	}
	instructions, _ := options["instructions"].(string)
	if len(instructions) > codexInstructionsMaxBytes {
		t.Fatalf("instructions length = %d, want <= %d", len(instructions), codexInstructionsMaxBytes)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if !strings.Contains(got.Messages[0].Content, "尾部") {
		t.Fatalf("overflow message missing tail: %+v", got.Messages[0])
	}
}

func TestSanitizeHeadersDropsAuthorization(t *testing.T) {
	got := sanitizeHeaders(map[string]string{
		"Authorization":      "Bearer bad",
		"ChatGPT-Account-ID": "bad-account",
		"X-Test":             "ok",
	})
	if _, ok := got["Authorization"]; ok {
		t.Fatalf("authorization header was not dropped: %#v", got)
	}
	if got["X-Test"] != "ok" {
		t.Fatalf("X-Test = %q", got["X-Test"])
	}
	if _, ok := got["ChatGPT-Account-ID"]; ok {
		t.Fatalf("ChatGPT-Account-ID should only come from the OAuth token: %#v", got)
	}
}
