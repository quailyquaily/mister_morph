package codex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if options["prompt_cache_key"] != "mistermorph" {
		t.Fatalf("prompt_cache_key = %#v", options["prompt_cache_key"])
	}
	if options["parallel_tool_calls"] != true {
		t.Fatalf("existing option lost: %#v", options["parallel_tool_calls"])
	}
}

func TestPrepareCodexRequestForcesJSONFormat(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"max_tokens": 1024,
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
	if options["response_format"] != "json_object" {
		t.Fatalf("response_format = %#v", options["response_format"])
	}
	if _, ok := got.Parameters["max_tokens"]; ok {
		t.Fatalf("max_tokens should be removed for Codex: %#v", got.Parameters)
	}
	if _, ok := options["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens should be removed for Codex: %#v", options)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if !strings.Contains(strings.ToLower(got.Messages[0].Content), "json") {
		t.Fatalf("first input message should mention JSON: %+v", got.Messages[0])
	}
}

func TestPrepareCodexRequestDoesNotDuplicateJSONReminder(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "return json please"},
		},
	})
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if got.Messages[0].Content != "return json please" {
		t.Fatalf("message = %+v", got.Messages[0])
	}
}

func TestClientSendsBearerTokenAndCodexRequestShape(t *testing.T) {
	stateDir := t.TempDir()
	if err := codexauth.WriteToken(stateDir, codexauth.Token{
		AccessToken: "access-token",
		AccountID:   "acc_123",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}

	var capturedAuth string
	var capturedAccount string
	var capturedBeta string
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedAccount = r.Header.Get("ChatGPT-Account-ID")
		capturedBeta = r.Header.Get("OpenAI-Beta")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		capturedBody = string(body)
		http.Error(w, `{"detail":"Bad Request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(Config{
		Endpoint: server.URL + "/backend-api/codex",
		Model:    "gpt-5.5",
		StateDir: stateDir,
	})
	_, err := client.Chat(context.Background(), llm.Request{
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"max_tokens": 1024,
			"openai": structs.JSONMap{
				"max_output_tokens": 512,
			},
		},
		Tools: []llm.Tool{{
			Name:           "read_file",
			ParametersJSON: `{"type":"object","properties":{"path":{"type":"string"}}}`,
		}},
	})
	if err == nil {
		t.Fatal("Chat() expected upstream error")
	}
	if capturedAuth != "Bearer access-token" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedAccount != "acc_123" {
		t.Fatalf("ChatGPT-Account-ID = %q", capturedAccount)
	}
	if capturedBeta != "" {
		t.Fatalf("OpenAI-Beta should not be sent on HTTP responses request, got %q", capturedBeta)
	}
	for _, want := range []string{
		`"instructions":"system prompt"`,
		`"store":false`,
		`"prompt_cache_key":"mistermorph"`,
		`"text":{"format":{"type":"json_object"}}`,
		`"text":"JSON response format reminder: return a JSON object as instructed."`,
		`"stream":true`,
		`"tools":[`,
		`"input":[`,
	} {
		if !strings.Contains(capturedBody, want) {
			t.Fatalf("request body missing %q: %s", want, capturedBody)
		}
	}
	if strings.Contains(capturedBody, "prompt_cache_retention") {
		t.Fatalf("request body should not include prompt_cache_retention: %s", capturedBody)
	}
	if strings.Contains(capturedBody, "max_output_tokens") || strings.Contains(capturedBody, "max_tokens") {
		t.Fatalf("request body should not include unsupported max token params: %s", capturedBody)
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
		"Authorization": "Bearer bad",
		"X-Test":        "ok",
	})
	if _, ok := got["Authorization"]; ok {
		t.Fatalf("authorization header was not dropped: %#v", got)
	}
	if got["X-Test"] != "ok" {
		t.Fatalf("X-Test = %q", got["X-Test"])
	}
}
