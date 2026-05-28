package codex

import (
	"strings"
	"testing"

	"github.com/lyricat/goutils/structs"
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
	if _, ok := options["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should not be sent for Codex: %#v", options)
	}
	if options["parallel_tool_calls"] != true {
		t.Fatalf("existing option lost: %#v", options["parallel_tool_calls"])
	}
}

func TestPrepareCodexRequestDropsPromptCacheOptions(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Parameters: map[string]any{
			"openai": structs.JSONMap{
				"prompt_cache_key":       "mistermorph",
				"prompt_cache_retention": "24h",
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
	if _, ok := options["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should be dropped for Codex: %#v", options)
	}
	if _, ok := options["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be dropped for Codex: %#v", options)
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
	if options["response_format"] != "json_object" {
		t.Fatalf("response_format = %#v", options["response_format"])
	}
	if _, ok := got.Parameters["max_tokens"]; ok {
		t.Fatalf("max_tokens should be removed for Codex: %#v", got.Parameters)
	}
	if _, ok := got.Parameters["temperature"]; ok {
		t.Fatalf("temperature should be removed for Codex: %#v", got.Parameters)
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

func TestPrepareCodexRequestIgnoresToolOutputForJSONReminder(t *testing.T) {
	got, err := prepareCodexRequest(llm.Request{
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "list contacts"},
			{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{{
					ID:           "call_1",
					Type:         "function",
					Name:         "bash",
					RawArguments: `{"cmd":"find contacts"}`,
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "contacts/bus_outbox.json"},
		},
	})
	if err != nil {
		t.Fatalf("prepareCodexRequest() error = %v", err)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if !strings.Contains(strings.ToLower(got.Messages[0].Content), "json") {
		t.Fatalf("first input message should mention JSON: %+v", got.Messages[0])
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
