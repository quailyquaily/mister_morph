package uniai

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

func TestBuildChatOptionsReplaceMessages(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "new"},
		},
	}

	opts := append(
		[]uniaiapi.ChatOption{uniaiapi.WithMessages(uniaiapi.User("old"))},
		buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)...,
	)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if len(built.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(built.Messages))
	}
	if built.Messages[0].Content != "new" {
		t.Fatalf("expected replaced message content 'new', got %q", built.Messages[0].Content)
	}
}

func TestBuildChatOptionsPreserveToolCallIDAsIs(t *testing.T) {
	rawID := "  call_1|ts:abc  "
	req := llm.Request{
		Messages: []llm.Message{
			{
				Role:       "tool",
				Content:    `{"content":"ok"}`,
				ToolCallID: rawID,
			},
		},
	}

	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if len(built.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(built.Messages))
	}
	if built.Messages[0].ToolCallID != rawID {
		t.Fatalf("expected tool_call_id preserved as-is %q, got %q", rawID, built.Messages[0].ToolCallID)
	}
}

func TestBuildChatOptionsMapsMessageParts(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{
				Role: "user",
				Parts: []llm.Part{
					{Type: llm.PartTypeText, Text: "describe this"},
					{Type: llm.PartTypeImageBase64, MIMEType: "image/png", DataBase64: "QUJD"},
				},
			},
		},
	}

	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if len(built.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(built.Messages))
	}
	if got := len(built.Messages[0].Parts); got != 2 {
		t.Fatalf("message parts length = %d, want 2", got)
	}
	if built.Messages[0].Parts[0].Type != uniaichat.PartTypeText || built.Messages[0].Parts[0].Text != "describe this" {
		t.Fatalf("text part mismatch: %+v", built.Messages[0].Parts[0])
	}
	if built.Messages[0].Parts[1].Type != uniaichat.PartTypeImageBase64 || built.Messages[0].Parts[1].DataBase64 != "QUJD" {
		t.Fatalf("image part mismatch: %+v", built.Messages[0].Parts[1])
	}
}

func TestBuildChatOptionsMapsInferenceProvider(t *testing.T) {
	req := llm.Request{
		Model:             "gpt-5.4",
		InferenceProvider: "openai",
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	}

	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.InferenceProvider != "openai" {
		t.Fatalf("inference_provider = %q, want openai", built.InferenceProvider)
	}
}

func TestBuildChatOptionsMapsOnStream(t *testing.T) {
	called := false
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		OnStream: func(ev llm.StreamEvent) error {
			called = true
			if ev.Delta != "abc" {
				t.Fatalf("delta = %q, want abc", ev.Delta)
			}
			if ev.ToolCallDelta == nil || ev.ToolCallDelta.Name != "message_react" {
				t.Fatalf("tool_call_delta = %#v", ev.ToolCallDelta)
			}
			if !ev.Done {
				t.Fatalf("done = false, want true")
			}
			if ev.Usage == nil || ev.Usage.TotalTokens != 9 {
				t.Fatalf("usage = %#v", ev.Usage)
			}
			if ev.Usage.Cache.CachedInputTokens != 3 {
				t.Fatalf("cached_input_tokens = %d, want 3", ev.Usage.Cache.CachedInputTokens)
			}
			if ev.Usage.Cache.CacheCreationInputTokens != 2 {
				t.Fatalf("cache_creation_input_tokens = %d, want 2", ev.Usage.Cache.CacheCreationInputTokens)
			}
			if got := ev.Usage.Cache.Details["ephemeral_5m_input_tokens"]; got != 2 {
				t.Fatalf("cache details = %#v", ev.Usage.Cache.Details)
			}
			if ev.Usage.Cost == nil || ev.Usage.Cost.Total != 0.125 {
				t.Fatalf("cost = %#v", ev.Usage.Cost)
			}
			return nil
		},
	}
	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OnStream == nil {
		t.Fatalf("expected on_stream callback")
	}
	if err := built.Options.OnStream(uniaiapi.StreamEvent{
		Delta: "abc",
		ToolCallDelta: &uniaiapi.ToolCallDelta{
			Index:     1,
			ID:        "call_1",
			Name:      "message_react",
			ArgsChunk: "{\"emoji\":\"ok_hand\"}",
		},
		Usage: &uniaichat.Usage{
			InputTokens:  4,
			OutputTokens: 5,
			TotalTokens:  9,
			Cache: uniaichat.UsageCache{
				CachedInputTokens:        3,
				CacheCreationInputTokens: 2,
				Details: map[string]int{
					"ephemeral_5m_input_tokens": 2,
				},
			},
			Cost: &uniaichat.UsageCost{
				Currency:           "USD",
				Estimated:          true,
				CachedInput:        0.025,
				CacheCreationInput: 0.050,
				Output:             0.050,
				Total:              0.125,
			},
		},
		Done: true,
	}); err != nil {
		t.Fatalf("on_stream callback returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected callback to be called")
	}
}

func TestToLLMUsageMapsCacheAndCost(t *testing.T) {
	usage := toLLMUsage(uniaichat.Usage{
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
		Cache: uniaichat.UsageCache{
			CachedInputTokens:        4,
			CacheCreationInputTokens: 3,
			Details: map[string]int{
				"ephemeral_5m_input_tokens": 3,
			},
		},
		Cost: &uniaichat.UsageCost{
			Currency:           "USD",
			Estimated:          true,
			Input:              0.01,
			CachedInput:        0.002,
			CacheCreationInput: 0.003,
			Output:             0.02,
			Total:              0.035,
		},
	})

	if usage.InputTokens != 10 || usage.OutputTokens != 5 || usage.TotalTokens != 15 {
		t.Fatalf("usage tokens = %#v", usage)
	}
	if usage.Cache.CachedInputTokens != 4 || usage.Cache.CacheCreationInputTokens != 3 {
		t.Fatalf("usage cache = %#v", usage.Cache)
	}
	if got := usage.Cache.Details["ephemeral_5m_input_tokens"]; got != 3 {
		t.Fatalf("usage cache details = %#v", usage.Cache.Details)
	}
	if usage.Cost == nil {
		t.Fatalf("expected cost to be mapped")
	}
	if usage.Cost.Total != 0.035 || usage.Cost.CachedInput != 0.002 || usage.Cost.CacheCreationInput != 0.003 {
		t.Fatalf("usage cost = %#v", usage.Cost)
	}
}

type testRawJSON string

func (r testRawJSON) RawJSON() string {
	return string(r)
}

func TestEnrichUsageFromOpenAICompatibleRawReadsCacheCreation(t *testing.T) {
	usage := llm.Usage{
		InputTokens:  2648,
		OutputTokens: 38,
		TotalTokens:  2686,
	}
	raw := testRawJSON(`{
		"usage": {
			"prompt_tokens": 2648,
			"completion_tokens": 38,
			"total_tokens": 2686,
			"prompt_tokens_details": {
				"cached_tokens": 2390,
				"cache_read_input_tokens": 2390,
				"cache_creation_input_tokens": 255
			}
		}
	}`)

	got, changed := enrichUsageFromOpenAICompatibleRaw(usage, raw)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got.Cache.CachedInputTokens != 2390 {
		t.Fatalf("cached_input_tokens = %d, want 2390", got.Cache.CachedInputTokens)
	}
	if got.Cache.CacheCreationInputTokens != 255 {
		t.Fatalf("cache_creation_input_tokens = %d, want 255", got.Cache.CacheCreationInputTokens)
	}
}

func TestEnrichUsageFromOpenAICompatibleRawReadsUsageObject(t *testing.T) {
	usage := llm.Usage{
		InputTokens:  2648,
		OutputTokens: 38,
		TotalTokens:  2686,
	}
	raw := testRawJSON(`{
		"prompt_tokens": 2648,
		"completion_tokens": 38,
		"total_tokens": 2686,
		"prompt_tokens_details": {
			"cache_read_input_tokens": 2390,
			"cache_creation_input_tokens": 255
		}
	}`)

	got, changed := enrichUsageFromOpenAICompatibleRaw(usage, raw)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got.Cache.CachedInputTokens != 2390 {
		t.Fatalf("cached_input_tokens = %d, want 2390", got.Cache.CachedInputTokens)
	}
	if got.Cache.CacheCreationInputTokens != 255 {
		t.Fatalf("cache_creation_input_tokens = %d, want 255", got.Cache.CacheCreationInputTokens)
	}
}

func TestEnrichUsageFromOpenAICompatibleRawReadsLastStreamChunk(t *testing.T) {
	usage := llm.Usage{
		InputTokens:  2648,
		OutputTokens: 38,
		TotalTokens:  2686,
	}
	rawChunks := []testRawJSON{
		testRawJSON(`{"choices":[{"delta":{"content":"hello"}}]}`),
		testRawJSON(`{"usage":{"prompt_tokens_details":{"cache_read_input_tokens":2390,"cache_creation_input_tokens":255}}}`),
	}

	got, changed := enrichUsageFromOpenAICompatibleRaw(usage, rawChunks)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got.Cache.CachedInputTokens != 2390 {
		t.Fatalf("cached_input_tokens = %d, want 2390", got.Cache.CachedInputTokens)
	}
	if got.Cache.CacheCreationInputTokens != 255 {
		t.Fatalf("cache_creation_input_tokens = %d, want 255", got.Cache.CacheCreationInputTokens)
	}
}

func TestEnrichUsageFromOpenAICompatibleRawReadsOpenAIStreamChunks(t *testing.T) {
	usage := llm.Usage{
		InputTokens:  2648,
		OutputTokens: 38,
		TotalTokens:  2686,
	}
	chunks := make([]openai.ChatCompletionChunk, 0, 2)
	for _, raw := range []string{
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4-6","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4-6","choices":[],"usage":{"prompt_tokens":2648,"completion_tokens":38,"total_tokens":2686,"prompt_tokens_details":{"cached_tokens":2390,"cache_read_input_tokens":2390,"cache_creation_input_tokens":255}}}`,
	} {
		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			t.Fatalf("unmarshal chunk: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	got, changed := enrichUsageFromOpenAICompatibleRaw(usage, chunks)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got.Cache.CachedInputTokens != 2390 {
		t.Fatalf("cached_input_tokens = %d, want 2390", got.Cache.CachedInputTokens)
	}
	if got.Cache.CacheCreationInputTokens != 255 {
		t.Fatalf("cache_creation_input_tokens = %d, want 255", got.Cache.CacheCreationInputTokens)
	}
}

func TestRecalculateUsageCostIncludesCacheCreation(t *testing.T) {
	cachedInputRate := 0.30
	cacheCreationRate := 3.75
	pricing := &uniaiapi.PricingCatalog{Chat: []uniaiapi.ChatPricingRule{
		{
			Model:                                 "claude-sonnet-4-6",
			InputUSDPerMillion:                    3.0,
			OutputUSDPerMillion:                   15.0,
			CachedInputUSDPerMillion:              &cachedInputRate,
			CacheCreationInputUSDPerMillion:       &cacheCreationRate,
			CacheCreationInputDetailUSDPerMillion: nil,
		},
	}}
	usage := llm.Usage{
		InputTokens:  2648,
		OutputTokens: 38,
		TotalTokens:  2686,
		Cache: llm.UsageCache{
			CachedInputTokens:        2390,
			CacheCreationInputTokens: 255,
		},
		Cost: &llm.UsageCost{Total: 999},
	}

	got := recalculateUsageCost(usage, pricing, "", "anthropic/claude-sonnet-4-6")
	if got.Cost == nil {
		t.Fatalf("cost = nil")
	}
	wantTotal := 0.00225225
	if math.Abs(got.Cost.Total-wantTotal) > 0.000000001 {
		t.Fatalf("total cost = %.10f, want %.10f", got.Cost.Total, wantTotal)
	}
	if got.Cost.CacheCreationInput <= 0 {
		t.Fatalf("cache creation cost = %.10f, want > 0", got.Cost.CacheCreationInput)
	}
}

func TestBuildChatOptionsDisablesOnStreamForGeminiProvider(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		OnStream: func(llm.StreamEvent) error { return nil },
	}
	opts := buildChatOptions(req, "gemini", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OnStream != nil {
		t.Fatalf("expected on_stream callback to be disabled for gemini provider")
	}
}

func TestBuildChatOptionsDisablesOnStreamForAnthropicProvider(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		OnStream: func(llm.StreamEvent) error { return nil },
	}
	opts := buildChatOptions(req, "anthropic", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OnStream != nil {
		t.Fatalf("expected on_stream callback to be disabled for anthropic provider")
	}
}

func TestBuildChatOptionsDisablesOnStreamForCloudflareProvider(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		OnStream: func(llm.StreamEvent) error { return nil },
	}
	opts := buildChatOptions(req, "cloudflare", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OnStream != nil {
		t.Fatalf("expected on_stream callback to be disabled for cloudflare provider")
	}
}

func TestBuildChatOptionsMapsDebugFn(t *testing.T) {
	var gotLabel, gotPayload string
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		DebugFn: func(label, payload string) {
			gotLabel = label
			gotPayload = payload
		},
	}
	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.DebugFn == nil {
		t.Fatalf("expected debug callback")
	}
	built.Options.DebugFn("openai.chat.request", `{"messages":[]}`)
	if gotLabel != "openai.chat.request" || gotPayload != `{"messages":[]}` {
		t.Fatalf("debug callback mismatch: label=%q payload=%q", gotLabel, gotPayload)
	}
}

func TestStreamDebugCaptureEmitsPartialOnError(t *testing.T) {
	var gotLabel, gotPayload string
	capture := newStreamDebugCapture("openai", func(label, payload string) {
		gotLabel = label
		gotPayload = payload
	}, true)

	wrapped := capture.Wrap(func(llm.StreamEvent) error { return nil })
	if err := wrapped(llm.StreamEvent{Delta: `{"type":"final","output":"hel`}); err != nil {
		t.Fatalf("stream callback returned error: %v", err)
	}
	if err := wrapped(llm.StreamEvent{ToolCallDelta: &llm.StreamToolCallDelta{
		Index:     0,
		ID:        "call_1",
		Name:      "write_file",
		ArgsChunk: `{"path":"index`,
	}}); err != nil {
		t.Fatalf("stream callback returned error: %v", err)
	}

	capture.EmitPartial(errors.New("unexpected end of JSON input"))

	if gotLabel != "openai.chat.stream.partial" {
		t.Fatalf("label = %q, want openai.chat.stream.partial", gotLabel)
	}
	var payload struct {
		Provider   string                `json:"provider"`
		Error      string                `json:"error"`
		Events     int                   `json:"events"`
		Text       string                `json:"text"`
		ToolCalls  []streamDebugToolCall `json:"tool_calls"`
		DeltaBytes int                   `json:"delta_bytes"`
	}
	if err := json.Unmarshal([]byte(gotPayload), &payload); err != nil {
		t.Fatalf("partial payload is not JSON: %v\n%s", err, gotPayload)
	}
	if payload.Provider != "openai" || payload.Error != "unexpected end of JSON input" {
		t.Fatalf("unexpected payload metadata: %#v", payload)
	}
	if payload.Events != 2 || payload.Text != `{"type":"final","output":"hel` {
		t.Fatalf("unexpected partial text/events: %#v", payload)
	}
	if payload.DeltaBytes != len(payload.Text) {
		t.Fatalf("delta_bytes = %d, want %d", payload.DeltaBytes, len(payload.Text))
	}
	if len(payload.ToolCalls) != 1 || payload.ToolCalls[0].Name != "write_file" || payload.ToolCalls[0].ArgsChunk != `{"path":"index` {
		t.Fatalf("unexpected tool call partials: %#v", payload.ToolCalls)
	}
}

func TestStreamDebugCaptureEmitsResponseForStreamResult(t *testing.T) {
	var gotLabel, gotPayload string
	capture := newStreamDebugCapture("azure", func(label, payload string) {
		gotLabel = label
		gotPayload = payload
	}, true)

	capture.EmitResponse(&uniaichat.Result{Text: "done"})

	if gotLabel != "azure.chat.response" {
		t.Fatalf("label = %q, want azure.chat.response", gotLabel)
	}
	if !strings.Contains(gotPayload, `"text":"done"`) {
		t.Fatalf("response payload missing text: %s", gotPayload)
	}
}

func TestStreamDebugLabelsOpenAIResponsesProvider(t *testing.T) {
	if got := chatResponseDebugLabel("openai_resp"); got != "openai.responses.response" {
		t.Fatalf("response label = %q", got)
	}
	if got := chatStreamPartialDebugLabel("openai_resp"); got != "openai.responses.stream.partial" {
		t.Fatalf("partial label = %q", got)
	}
}

func TestBuildChatOptionsAppliesResponseFormatWithoutTools(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	}
	opts := buildChatOptions(req, "", "", "", "", true, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OpenAI == nil {
		t.Fatal("expected openai options to be set")
	}
	if got, ok := built.Options.OpenAI["response_format"]; !ok || got != "json_object" {
		t.Fatalf("response_format = %#v, want %q", got, "json_object")
	}
}

func TestBuildChatOptionsSkipsResponseFormatWhenToolsPresent(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		Tools: []llm.Tool{{
			Name:           "web_search",
			Description:    "search",
			ParametersJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
		}},
	}
	opts := buildChatOptions(req, "", "", "", "", true, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if len(built.Tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(built.Tools))
	}
	if built.Options.OpenAI != nil {
		if _, ok := built.Options.OpenAI["response_format"]; ok {
			t.Fatalf("did not expect response_format when tools are present: %#v", built.Options.OpenAI)
		}
	}
}

func TestBuildChatOptionsMapsPromptCacheOptionsForOpenAIResp(t *testing.T) {
	req := llm.Request{
		Scene: "runtime.loop",
		Messages: []llm.Message{
			{Role: "system", Content: "stable system"},
			{Role: "user", Content: "hello"},
		},
	}
	opts := buildChatOptions(req, "openai_resp", "gpt-5.4", "short", "cache-test", true, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OpenAI == nil {
		t.Fatal("expected openai options to be set")
	}
	if got, _ := built.Options.OpenAI["prompt_cache_key"].(string); !strings.HasPrefix(got, "cache-test-mm-") {
		t.Fatalf("prompt_cache_key = %#v, want cache-test-prefixed derived key", got)
	}
	if got := built.Options.OpenAI["prompt_cache_retention"]; got != "in_memory" {
		t.Fatalf("prompt_cache_retention = %#v, want in_memory", got)
	}
	if got := built.Options.OpenAI["response_format"]; got != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", got)
	}
}

func TestBuildChatOptionsUsesPromptCacheKeyPrefixWithoutStablePayload(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	}
	opts := buildChatOptions(req, "openai_resp", "gpt-5.4", "short", "manual-test", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.OpenAI == nil {
		t.Fatal("expected openai options to be set")
	}
	if got := built.Options.OpenAI["prompt_cache_key"]; got != "manual-test" {
		t.Fatalf("prompt_cache_key = %#v, want manual-test", got)
	}
}

func TestBuildChatOptionsMapsPromptCacheOptionsForAzure(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: "stable system"},
			{Role: "user", Content: "hello"},
		},
	}
	opts := buildChatOptions(req, "azure", "gpt-5.4", "long", "", true, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.Azure == nil {
		t.Fatal("expected azure options to be set")
	}
	if got := built.Options.Azure["prompt_cache_key"]; got == "" || got == nil {
		t.Fatalf("prompt_cache_key = %#v, want non-empty derived key", got)
	}
	if got := built.Options.Azure["prompt_cache_retention"]; got != "24h" {
		t.Fatalf("prompt_cache_retention = %#v, want 24h", got)
	}
	if got := built.Options.Azure["response_format"]; got != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", got)
	}
}

func TestBuildChatOptionsDoesNotInjectTemperatureWhenUnset(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	}
	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.Temperature != nil {
		t.Fatalf("expected temperature to remain unset, got %v", *built.Options.Temperature)
	}
}

func TestBuildChatOptionsAppliesConfiguredDefaults(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	}
	temperature := 0.4
	reasoningBudget := 8192
	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, &temperature, "high", &reasoningBudget)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.Temperature == nil || *built.Options.Temperature != 0.4 {
		t.Fatalf("temperature = %#v, want 0.4", built.Options.Temperature)
	}
	if built.Options.ReasoningEffort == nil || *built.Options.ReasoningEffort != uniaichat.ReasoningEffortHigh {
		t.Fatalf("reasoning effort = %#v, want high", built.Options.ReasoningEffort)
	}
	if built.Options.ReasoningBudget == nil || *built.Options.ReasoningBudget != 8192 {
		t.Fatalf("reasoning budget = %#v, want 8192", built.Options.ReasoningBudget)
	}
}

func TestBuildChatOptionsSkipsReasoningBudgetForOpenAIResp(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	}
	reasoningBudget := 8192
	opts := buildChatOptions(req, "openai_resp", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "high", &reasoningBudget)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.ReasoningEffort == nil || *built.Options.ReasoningEffort != uniaichat.ReasoningEffortHigh {
		t.Fatalf("reasoning effort = %#v, want high", built.Options.ReasoningEffort)
	}
	if built.Options.ReasoningBudget != nil {
		t.Fatalf("reasoning budget = %#v, want nil", built.Options.ReasoningBudget)
	}
}

func TestBuildChatOptionsRequestTemperatureOverridesConfiguredDefault(t *testing.T) {
	req := llm.Request{
		Messages:   []llm.Message{{Role: "user", Content: "hello"}},
		Parameters: map[string]any{"temperature": 0.1},
	}
	temperature := 0.4
	opts := buildChatOptions(req, "", "", "", "", false, uniaiapi.ToolsEmulationOff, &temperature, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.Temperature == nil || *built.Options.Temperature != 0.1 {
		t.Fatalf("temperature = %#v, want 0.1", built.Options.Temperature)
	}
}

func TestPartRoundTripBetweenLLMAndUniai(t *testing.T) {
	src := []llm.Part{
		{Type: llm.PartTypeText, Text: "hello", CacheControl: &llm.CacheControl{TTL: "5m"}},
		{Type: llm.PartTypeImageURL, URL: "https://example.com/a.png"},
		{Type: llm.PartTypeImageBase64, MIMEType: "image/jpeg", DataBase64: "QUJD"},
	}
	toUniai := toUniaiPartsFromLLM("anthropic", src)
	back := toLLMParts(toUniai)

	if !reflect.DeepEqual(back, src) {
		t.Fatalf("parts mismatch: got %+v want %+v", back, src)
	}
}

func TestBuildChatOptionsMapsToolCacheControl(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		Tools: []llm.Tool{{
			Name:           "lookup",
			Description:    "search",
			ParametersJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
			CacheControl:   &llm.CacheControl{TTL: "1h"},
		}},
	}
	opts := buildChatOptions(req, "anthropic", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)

	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if len(built.Tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(built.Tools))
	}
	if built.Tools[0].CacheControl == nil || built.Tools[0].CacheControl.TTL != "1h" {
		t.Fatalf("tool cache control = %#v, want 1h", built.Tools[0].CacheControl)
	}
}

func TestBuildChatOptionsKeepsExplicitCacheControlForAnthropic(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{
				Role: "system",
				Parts: []llm.Part{{
					Type:         llm.PartTypeText,
					Text:         "sys",
					CacheControl: &llm.CacheControl{TTL: "5m"},
				}},
			},
			{Role: "user", Content: "hello"},
		},
		Tools: []llm.Tool{{
			Name:           "lookup",
			Description:    "search",
			ParametersJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
			CacheControl:   &llm.CacheControl{TTL: "1h"},
		}},
	}

	opts := buildChatOptions(req, "anthropic", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if got := built.Messages[0].Parts[0].CacheControl; got == nil || got.TTL != "5m" {
		t.Fatalf("system cache control = %#v, want TTL 5m", got)
	}
	if got := built.Tools[0].CacheControl; got == nil || got.TTL != "1h" {
		t.Fatalf("tool cache control = %#v, want TTL 1h", got)
	}
}

func TestBuildChatOptionsStripsExplicitCacheControlForOpenAI(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{{
			Role: "system",
			Parts: []llm.Part{{
				Type:         llm.PartTypeText,
				Text:         "sys",
				CacheControl: &llm.CacheControl{TTL: "5m"},
			}},
		}},
		Tools: []llm.Tool{{
			Name:           "lookup",
			Description:    "search",
			ParametersJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
			CacheControl:   &llm.CacheControl{TTL: "1h"},
		}},
	}

	opts := buildChatOptions(req, "openai_resp", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if got := built.Messages[0].Parts[0].CacheControl; got != nil {
		t.Fatalf("system cache control = %#v, want nil", got)
	}
	if got := built.Tools[0].CacheControl; got != nil {
		t.Fatalf("tool cache control = %#v, want nil", got)
	}
}

func TestBuildChatOptionsStripsOnlySystemPromptCacheControlForBedrock(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{
				Role: "system",
				Parts: []llm.Part{{
					Type:         llm.PartTypeText,
					Text:         "sys",
					CacheControl: &llm.CacheControl{TTL: "5m"},
				}},
			},
			{
				Role: "user",
				Parts: []llm.Part{{
					Type:         llm.PartTypeText,
					Text:         "prefix",
					CacheControl: &llm.CacheControl{TTL: "1h"},
				}},
			},
		},
		Tools: []llm.Tool{{
			Name:           "lookup",
			Description:    "search",
			ParametersJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
			CacheControl:   &llm.CacheControl{TTL: "1h"},
		}},
	}

	opts := buildChatOptions(req, "bedrock", "", "", "", false, uniaiapi.ToolsEmulationOff, nil, "", nil)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if got := built.Messages[0].Parts[0].CacheControl; got != nil {
		t.Fatalf("system cache control = %#v, want nil", got)
	}
	if got := built.Messages[1].Parts[0].CacheControl; got == nil || got.TTL != "1h" {
		t.Fatalf("user cache control = %#v, want TTL 1h", got)
	}
	if got := built.Tools[0].CacheControl; got != nil {
		t.Fatalf("tool cache control = %#v, want nil", got)
	}
}

func TestNewStoresCacheTTLDefault(t *testing.T) {
	client := New(Config{
		Provider:       "openai_resp",
		Model:          "gpt-5.2",
		CacheTTL:       "long",
		CacheKeyPrefix: "test-prefix",
	})

	if client.cacheTTL != "long" {
		t.Fatalf("cacheTTL = %q, want long", client.cacheTTL)
	}
	if client.cacheKeyPrefix != "test-prefix" {
		t.Fatalf("cacheKeyPrefix = %q, want test-prefix", client.cacheKeyPrefix)
	}
}

func TestToolCallThoughtSignatureRoundTrip(t *testing.T) {
	sig := "sig_abc"
	origArgs := `{"path":"/tmp/a.txt","dup":1,"dup":2}`
	orig := []uniaiapi.ToolCall{
		{
			ID:               "call_1",
			Type:             "function",
			ThoughtSignature: sig,
			Function: uniaiapi.ToolCallFunction{
				Name:      "read_file",
				Arguments: origArgs,
			},
		},
	}

	out := toLLMToolCalls(orig)
	if len(out) != 1 {
		t.Fatalf("expected 1 llm tool call, got %d", len(out))
	}
	if out[0].ThoughtSignature != sig {
		t.Fatalf("expected round-tripped thought signature %q, got %q", sig, out[0].ThoughtSignature)
	}
	if out[0].RawArguments != origArgs {
		t.Fatalf("expected raw arguments %q, got %q", origArgs, out[0].RawArguments)
	}

	uniaiCalls := toUniaiToolCallsFromLLM(out)
	if len(uniaiCalls) != 1 {
		t.Fatalf("expected 1 uniai tool call, got %d", len(uniaiCalls))
	}
	if uniaiCalls[0].ThoughtSignature != sig {
		t.Fatalf("expected thought signature %q, got %q", sig, uniaiCalls[0].ThoughtSignature)
	}
	if uniaiCalls[0].Function.Arguments != origArgs {
		t.Fatalf("expected exact raw arguments %q, got %q", origArgs, uniaiCalls[0].Function.Arguments)
	}
}

func TestEnsureGeminiToolCallThoughtSignaturesDecodeFromID(t *testing.T) {
	rawID := "call_1|ts:c2lnX2Zyb21faWQ"
	calls := []llm.ToolCall{{
		ID:           rawID,
		Name:         "read_file",
		RawArguments: `{"path":"/tmp/a.txt"}`,
	}}

	out := ensureGeminiToolCallThoughtSignatures(calls)
	if len(out) != 1 {
		t.Fatalf("expected 1 call, got %d", len(out))
	}
	if out[0].ThoughtSignature != "sig_from_id" {
		t.Fatalf("expected signature decoded from id, got %q", out[0].ThoughtSignature)
	}
}

func TestEnsureGeminiToolCallThoughtSignaturesCarryForward(t *testing.T) {
	calls := []llm.ToolCall{
		{
			ID:               "call_1",
			Name:             "read_file",
			RawArguments:     `{"path":"a"}`,
			ThoughtSignature: "sig_1",
		},
		{
			ID:           "call_2",
			Name:         "read_file",
			RawArguments: `{"path":"b"}`,
		},
	}

	out := ensureGeminiToolCallThoughtSignatures(calls)
	if len(out) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(out))
	}
	if out[1].ThoughtSignature != "sig_1" {
		t.Fatalf("expected second call to inherit prior signature, got %q", out[1].ThoughtSignature)
	}
}

func TestEnsureGeminiToolCallThoughtSignaturesSynthesize(t *testing.T) {
	calls := []llm.ToolCall{{
		ID:           "call_42",
		Name:         "read_file",
		RawArguments: `{"path":"a"}`,
	}}

	out1 := ensureGeminiToolCallThoughtSignatures(calls)
	out2 := ensureGeminiToolCallThoughtSignatures(calls)
	if len(out1) != 1 || len(out2) != 1 {
		t.Fatalf("unexpected output lengths: %d, %d", len(out1), len(out2))
	}
	if out1[0].ThoughtSignature == "" {
		t.Fatalf("expected synthesized signature")
	}
	if out1[0].ThoughtSignature != out2[0].ThoughtSignature {
		t.Fatalf("expected deterministic synthesized signature, got %q vs %q", out1[0].ThoughtSignature, out2[0].ThoughtSignature)
	}
}

func TestShouldEnsureGeminiThoughtSignature(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{
			name:     "provider gemini with non gemini model",
			provider: "gemini",
			model:    "gpt-5.2",
			want:     true,
		},
		{
			name:     "openai provider with gemini model",
			provider: "openai",
			model:    "carrot/gemini-3-pro",
			want:     false,
		},
		{
			name:     "empty provider with gemini model",
			provider: "",
			model:    "carrot/gemini-3-pro",
			want:     false,
		},
		{
			name:     "other provider with gemini model",
			provider: "anthropic",
			model:    "carrot/gemini-3-pro",
			want:     false,
		},
		{
			name:     "non gemini provider and model",
			provider: "openai",
			model:    "gpt-5.2",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldEnsureGeminiThoughtSignature(tt.provider, tt.model); got != tt.want {
				t.Fatalf("shouldEnsureGeminiThoughtSignature(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}
