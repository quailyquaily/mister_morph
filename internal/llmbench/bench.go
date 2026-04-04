package llmbench

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

var errorStatusPattern = regexp.MustCompile(`(?is)\bstatus\s+\d{3}\s*:\s*(.+)$`)

type BenchmarkResult struct {
	ID          string `json:"id"`
	OK          bool   `json:"ok"`
	DurationMS  int64  `json:"duration_ms"`
	Detail      string `json:"detail,omitempty"`
	Error       string `json:"error,omitempty"`
	RawResponse string `json:"raw_response,omitempty"`
}

const BenchmarksPerRun = 3

type ProfileMetadata struct {
	Profile  string
	Provider string
	APIBase  string
	Model    string
}

type ProfileResult struct {
	Profile    string            `json:"profile"`
	Provider   string            `json:"provider,omitempty"`
	APIBase    string            `json:"api_base,omitempty"`
	Model      string            `json:"model,omitempty"`
	Benchmarks []BenchmarkResult `json:"benchmarks,omitempty"`
	Error      string            `json:"error,omitempty"`
}

func (r ProfileResult) OK() bool {
	if strings.TrimSpace(r.Error) != "" {
		return false
	}
	for _, benchmark := range r.Benchmarks {
		if !benchmark.OK {
			return false
		}
	}
	return true
}

func Run(ctx context.Context, client llm.Client, meta ProfileMetadata) ProfileResult {
	return RunWithProgress(ctx, client, meta, nil)
}

func RunWithProgress(
	ctx context.Context,
	client llm.Client,
	meta ProfileMetadata,
	onBenchmark func(BenchmarkResult),
) ProfileResult {
	model := strings.TrimSpace(meta.Model)
	textResult := RunTextBenchmark(ctx, client, model)
	if onBenchmark != nil {
		onBenchmark(textResult)
	}
	jsonResult := RunJSONBenchmark(ctx, client, model)
	if onBenchmark != nil {
		onBenchmark(jsonResult)
	}
	toolResult := RunToolCallingBenchmark(ctx, client, model)
	if onBenchmark != nil {
		onBenchmark(toolResult)
	}
	return ProfileResult{
		Profile:    strings.TrimSpace(meta.Profile),
		Provider:   strings.TrimSpace(meta.Provider),
		APIBase:    strings.TrimSpace(meta.APIBase),
		Model:      model,
		Benchmarks: []BenchmarkResult{textResult, jsonResult, toolResult},
	}
}

func RunTextBenchmark(ctx context.Context, client llm.Client, model string) BenchmarkResult {
	start := time.Now()
	result, err := client.Chat(ctx, llm.Request{
		Model: strings.TrimSpace(model),
		Scene: "console.settings_test.text_reply",
		Messages: []llm.Message{
			{Role: "system", Content: "You're acting the linux cmd `echo`, will echo back the text."},
			{Role: "user", Content: "OK"},
		},
		Parameters: map[string]any{
			"max_tokens": 1024,
		},
	})
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		return BenchmarkResult{
			ID:          "text_reply",
			OK:          false,
			DurationMS:  durationMS,
			Error:       strings.TrimSpace(err.Error()),
			RawResponse: RawResponseFromError(err),
		}
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		return BenchmarkResult{
			ID:          "text_reply",
			OK:          false,
			DurationMS:  durationMS,
			Error:       "received an empty text reply",
			RawResponse: RawResponse(result),
		}
	}

	return BenchmarkResult{
		ID:          "text_reply",
		OK:          true,
		DurationMS:  durationMS,
		Detail:      SummarizeBenchmarkDetail(text),
		RawResponse: RawResponse(result),
	}
}

func RunJSONBenchmark(ctx context.Context, client llm.Client, model string) BenchmarkResult {
	start := time.Now()
	result, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		Scene:     "console.settings_test.json_response",
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: "You wrap the input by a JSON object and echo back the JSON object only. for example, IF input is `Hello` THEN return {\"message\": \"Hello\"}."},
			{Role: "user", Content: "Hello"},
		},
		Parameters: map[string]any{
			"max_tokens": 1024,
		},
	})
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		return BenchmarkResult{
			ID:          "json_response",
			OK:          false,
			DurationMS:  durationMS,
			Error:       strings.TrimSpace(err.Error()),
			RawResponse: RawResponseFromError(err),
		}
	}

	var payload struct {
		Message string `json:"message"`
	}
	if err := jsonutil.DecodeWithFallback(result.Text, &payload); err != nil {
		return BenchmarkResult{
			ID:          "json_response",
			OK:          false,
			DurationMS:  durationMS,
			Error:       "response was not valid json",
			RawResponse: RawResponse(result),
		}
	}
	if strings.TrimSpace(payload.Message) != "Hello" {
		return BenchmarkResult{
			ID:          "json_response",
			OK:          false,
			DurationMS:  durationMS,
			Error:       "json response is not so correct",
			RawResponse: RawResponse(result),
		}
	}

	detail := SummarizeBenchmarkDetail(strings.TrimSpace(payload.Message))
	if detail == "" {
		detail = "status=ok"
	}
	return BenchmarkResult{
		ID:          "json_response",
		OK:          true,
		DurationMS:  durationMS,
		Detail:      detail,
		RawResponse: RawResponse(result),
	}
}

func RunToolCallingBenchmark(ctx context.Context, client llm.Client, model string) BenchmarkResult {
	start := time.Now()
	result, err := client.Chat(ctx, llm.Request{
		Model: strings.TrimSpace(model),
		Scene: "console.settings_test.tool_calling",
		Messages: []llm.Message{
			{Role: "system", Content: "You are a tool calling test. Always call the ping tool exactly once."},
			{Role: "user", Content: "Call the ping tool now."},
		},
		Tools: []llm.Tool{
			{
				Name:           "ping",
				Description:    "Connectivity check tool.",
				ParametersJSON: `{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
			},
		},
		Parameters: map[string]any{
			"max_tokens": 1024,
		},
	})
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		return BenchmarkResult{
			ID:          "tool_calling",
			OK:          false,
			DurationMS:  durationMS,
			Error:       strings.TrimSpace(err.Error()),
			RawResponse: RawResponseFromError(err),
		}
	}

	for _, call := range result.ToolCalls {
		if strings.EqualFold(strings.TrimSpace(call.Name), "ping") {
			return BenchmarkResult{
				ID:          "tool_calling",
				OK:          true,
				DurationMS:  durationMS,
				Detail:      "called ping",
				RawResponse: RawResponse(result),
			}
		}
	}

	detail := SummarizeBenchmarkDetail(strings.TrimSpace(result.Text))
	if detail == "" {
		detail = "model replied without calling the tool"
	} else {
		detail = "model replied without calling the tool: " + detail
	}
	return BenchmarkResult{
		ID:          "tool_calling",
		OK:          false,
		DurationMS:  durationMS,
		Error:       detail,
		RawResponse: RawResponse(result),
	}
}

func RawResponse(result llm.Result) string {
	text := strings.TrimSpace(result.Text)
	if len(result.ToolCalls) == 0 && result.JSON == nil {
		return text
	}

	payload := map[string]any{}
	if text != "" {
		payload["text"] = text
	}
	if result.JSON != nil {
		payload["json"] = result.JSON
	}
	if len(result.ToolCalls) > 0 {
		payload["tool_calls"] = result.ToolCalls
	}
	if len(payload) == 0 {
		return ""
	}

	serialized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return text
	}
	return string(serialized)
}

func RawResponseFromError(err error) string {
	if err == nil {
		return ""
	}

	text := strings.TrimSpace(err.Error())
	if text == "" {
		return ""
	}

	matches := errorStatusPattern.FindStringSubmatch(text)
	if len(matches) == 2 {
		if raw := strings.TrimSpace(matches[1]); raw != "" {
			return raw
		}
	}

	return text
}

func SummarizeBenchmarkDetail(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	const maxLen = 140
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen-1]) + "…"
}
