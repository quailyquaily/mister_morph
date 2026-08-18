package llmbench

import (
	"context"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

type stubClient struct {
	result llm.Result
}

func (c stubClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return c.result, nil
}

func TestRunJSONBenchmarkReturnsCanonicalDetail(t *testing.T) {
	result := RunJSONBenchmark(context.Background(), stubClient{result: llm.Result{Text: `{"message":"Hello"}`}}, "model")
	if !result.OK || result.Detail != "Hello" {
		t.Fatalf("RunJSONBenchmark() = %+v, want successful Hello detail", result)
	}
}

func TestRawResponseIncludesStructuredFields(t *testing.T) {
	got := RawResponse(llm.Result{
		Text: "ok",
		JSON: map[string]any{"answer": true},
		ToolCalls: []llm.ToolCall{
			{Name: "ping"},
		},
	})
	if got == "" {
		t.Fatal("RawResponse() returned an empty structured response")
	}
}
