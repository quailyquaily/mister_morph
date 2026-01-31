package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/quailyquaily/mister_morph/llm"
	"github.com/quailyquaily/mister_morph/tools"
)

// --- mock LLM client ---

type mockClient struct {
	mu        sync.Mutex
	responses []llm.Result
	calls     []llm.Request
	idx       int
}

func newMockClient(responses ...llm.Result) *mockClient {
	return &mockClient{responses: responses}
}

func (m *mockClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.idx >= len(m.responses) {
		return llm.Result{}, fmt.Errorf("no more mock responses")
	}
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func (m *mockClient) allCalls() []llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]llm.Request, len(m.calls))
	copy(out, m.calls)
	return out
}

// --- mock tool ---

type mockTool struct {
	name   string
	result string
	err    error
}

func (t *mockTool) Name() string                                                    { return t.name }
func (t *mockTool) Description() string                                             { return "mock tool" }
func (t *mockTool) ParameterSchema() string                                         { return "{}" }
func (t *mockTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return t.result, t.err
}

// --- helpers ---

func finalResponse(output string) llm.Result {
	return llm.Result{
		Text: fmt.Sprintf(`{"type":"final","final":{"thought":"t","output":"%s"}}`, output),
	}
}

func toolCallResponse(toolName string) llm.Result {
	return llm.Result{
		Text: fmt.Sprintf(`{"type":"tool_call","tool_call":{"thought":"t","tool_name":"%s","tool_params":{}}}`, toolName),
	}
}

func baseCfg() Config {
	return Config{MaxSteps: 5, PlanMode: "off"}
}

func baseRegistry() *tools.Registry {
	return tools.NewRegistry()
}

// ============================================================
// Tests for Option functions and Engine field assignments
// ============================================================

func TestWithPromptBuilder_SetsField(t *testing.T) {
	called := false
	fn := func(r *tools.Registry, task string) string {
		called = true
		return "custom prompt"
	}
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithPromptBuilder(fn))
	if e.promptBuilder == nil {
		t.Fatal("expected promptBuilder to be set")
	}
	_ = called
}

func TestWithParamsBuilder_SetsField(t *testing.T) {
	fn := func(opts RunOptions) map[string]any {
		return map[string]any{"temp": 0.5}
	}
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithParamsBuilder(fn))
	if e.paramsBuilder == nil {
		t.Fatal("expected paramsBuilder to be set")
	}
}

func TestWithOnToolSuccess_SetsField(t *testing.T) {
	fn := func(ctx *Context, toolName string) {}
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithOnToolSuccess(fn))
	if e.onToolSuccess == nil {
		t.Fatal("expected onToolSuccess to be set")
	}
}

func TestWithFallbackFinal_SetsField(t *testing.T) {
	fn := func() *Final { return &Final{Output: "fallback"} }
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithFallbackFinal(fn))
	if e.fallbackFinal == nil {
		t.Fatal("expected fallbackFinal to be set")
	}
}

func TestWithPromptBuilder_NilIgnored(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithPromptBuilder(nil))
	if e.promptBuilder != nil {
		t.Fatal("expected promptBuilder to remain nil for nil input")
	}
}

func TestWithParamsBuilder_NilIgnored(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithParamsBuilder(nil))
	if e.paramsBuilder != nil {
		t.Fatal("expected paramsBuilder to remain nil for nil input")
	}
}

func TestWithOnToolSuccess_NilIgnored(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithOnToolSuccess(nil))
	if e.onToolSuccess != nil {
		t.Fatal("expected onToolSuccess to remain nil for nil input")
	}
}

func TestWithFallbackFinal_NilIgnored(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithFallbackFinal(nil))
	if e.fallbackFinal != nil {
		t.Fatal("expected fallbackFinal to remain nil for nil input")
	}
}

// ============================================================
// Tests for Run() integration points
// ============================================================

func TestPromptBuilder_OverridesDefault(t *testing.T) {
	customPrompt := "CUSTOM_SYSTEM_PROMPT_XYZ"
	client := newMockClient(finalResponse("ok"))

	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(),
		WithPromptBuilder(func(r *tools.Registry, task string) string {
			return customPrompt
		}),
	)

	_, _, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one LLM call")
	}

	// First message should be system prompt with custom content
	if calls[0].Messages[0].Content != customPrompt {
		t.Errorf("expected system prompt=%q, got %q", customPrompt, calls[0].Messages[0].Content)
	}
}

func TestPromptBuilder_DefaultUsedWhenNil(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	reg := baseRegistry()

	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one LLM call")
	}

	expected := BuildSystemPrompt(reg, DefaultPromptSpec())
	if calls[0].Messages[0].Content != expected {
		t.Error("expected default BuildSystemPrompt to be used when promptBuilder is nil")
	}
}

func TestParamsBuilder_InjectedIntoRequest(t *testing.T) {
	extraParams := map[string]any{"temperature": 0.3, "top_p": 0.9}
	client := newMockClient(finalResponse("ok"))

	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(),
		WithParamsBuilder(func(opts RunOptions) map[string]any {
			return extraParams
		}),
	)

	_, _, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one LLM call")
	}
	if calls[0].Parameters == nil {
		t.Fatal("expected Parameters to be set on LLM request")
	}
	if calls[0].Parameters["temperature"] != 0.3 {
		t.Errorf("expected temperature=0.3, got %v", calls[0].Parameters["temperature"])
	}
	if calls[0].Parameters["top_p"] != 0.9 {
		t.Errorf("expected top_p=0.9, got %v", calls[0].Parameters["top_p"])
	}
}

func TestParamsBuilder_NilMeansNoParams(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one LLM call")
	}
	if calls[0].Parameters != nil {
		t.Errorf("expected Parameters=nil when no paramsBuilder, got %v", calls[0].Parameters)
	}
}

func TestOnToolSuccess_CalledOnSuccess(t *testing.T) {
	reg := baseRegistry()
	reg.Register(&mockTool{name: "search", result: "found it"})

	var callbackTool string
	client := newMockClient(
		toolCallResponse("search"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithOnToolSuccess(func(ctx *Context, toolName string) {
			callbackTool = toolName
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callbackTool != "search" {
		t.Errorf("expected onToolSuccess called with 'search', got %q", callbackTool)
	}
}

func TestOnToolSuccess_NotCalledOnError(t *testing.T) {
	reg := baseRegistry()
	reg.Register(&mockTool{name: "search", result: "", err: fmt.Errorf("tool failed")})

	called := false
	client := newMockClient(
		toolCallResponse("search"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithOnToolSuccess(func(ctx *Context, toolName string) {
			called = true
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected onToolSuccess NOT to be called on tool error")
	}
}

func TestOnToolSuccess_NotCalledForUnknownTool(t *testing.T) {
	reg := baseRegistry()
	// no tool registered

	called := false
	client := newMockClient(
		toolCallResponse("nonexistent"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithOnToolSuccess(func(ctx *Context, toolName string) {
			called = true
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected onToolSuccess NOT to be called for unknown tool")
	}
}

func TestRawFinalAnswer_SetOnContext(t *testing.T) {
	resp := `{"type":"final","final":{"thought":"done","output":"result","custom_field":42}}`
	client := newMockClient(llm.Result{Text: resp})

	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, agentCtx, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentCtx.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be set on agentCtx")
	}

	var m map[string]any
	if err := json.Unmarshal(agentCtx.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer not valid JSON: %v", err)
	}
	if m["custom_field"] != float64(42) {
		t.Errorf("expected custom_field=42, got %v", m["custom_field"])
	}
}

func TestRawFinalAnswer_SetForFinalAnswerType(t *testing.T) {
	resp := `{"type":"final_answer","final_answer":{"thought":"done","output":"result","domain_data":"x"}}`
	client := newMockClient(llm.Result{Text: resp})

	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, agentCtx, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentCtx.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be set for final_answer type")
	}

	var m map[string]any
	if err := json.Unmarshal(agentCtx.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer not valid JSON: %v", err)
	}
	if m["domain_data"] != "x" {
		t.Errorf("expected domain_data='x', got %v", m["domain_data"])
	}
}

// ============================================================
// Tests for forceConclusion() hook points
// ============================================================

func TestFallbackFinal_UsedOnForceConclusionLLMError(t *testing.T) {
	// LLM errors on first call in forceConclusion
	client := &mockClient{
		responses: []llm.Result{},
	}

	customFallback := &Final{Output: "custom_fallback_answer"}
	e := New(client, baseRegistry(), Config{MaxSteps: 1, PlanMode: "off"}, DefaultPromptSpec(),
		WithFallbackFinal(func() *Final {
			return customFallback
		}),
	)

	// The main loop runs once, gets an error (no responses), which returns error.
	// We need the main loop to exhaust steps and fall through to forceConclusion.
	// So: first call returns unparseable text to exhaust parse retries, then
	// forceConclusion's LLM call fails.

	// Setup: 1 step, 0 parse retries → parse failure breaks loop → forceConclusion
	// forceConclusion's Chat call fails because no more responses → fallback should be used
	client2 := newMockClient(
		llm.Result{Text: "not json"},  // main loop: parse failure
		// forceConclusion: no response → error
	)
	e2 := New(client2, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0, PlanMode: "off"}, DefaultPromptSpec(),
		WithFallbackFinal(func() *Final {
			return &Final{Output: "my_fallback"}
		}),
	)

	f, _, err := e2.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}
	if f.Output != "my_fallback" {
		t.Errorf("expected fallback output='my_fallback', got %v", f.Output)
	}
	_ = e // suppress unused
}

func TestFallbackFinal_DefaultWhenNotSet(t *testing.T) {
	// Without fallbackFinal, forceConclusion should return default "insufficient_evidence"
	client := newMockClient(
		llm.Result{Text: "not json"},
		// forceConclusion: no response → error → default fallback
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0, PlanMode: "off"}, DefaultPromptSpec())

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}
	if f.Output != "insufficient_evidence" {
		t.Errorf("expected default fallback output='insufficient_evidence', got %v", f.Output)
	}
}

func TestFallbackFinal_UsedOnParseError(t *testing.T) {
	// forceConclusion gets valid LLM response but parse fails → fallback used
	client := newMockClient(
		llm.Result{Text: "not json"},            // main loop parse fail
		llm.Result{Text: "still not valid json"}, // forceConclusion parse fail
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0, PlanMode: "off"}, DefaultPromptSpec(),
		WithFallbackFinal(func() *Final {
			return &Final{Output: "parse_fallback"}
		}),
	)

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}
	if f.Output != "parse_fallback" {
		t.Errorf("expected fallback output='parse_fallback', got %v", f.Output)
	}
}

func TestForceConclusion_RawFinalAnswer_Set(t *testing.T) {
	// Main loop exhausts with parse failure, forceConclusion succeeds with final
	resp := `{"type":"final","final":{"thought":"forced","output":"result","extra":true}}`
	client := newMockClient(
		llm.Result{Text: "not json"},  // main loop parse fail
		llm.Result{Text: resp},        // forceConclusion succeeds
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0, PlanMode: "off"}, DefaultPromptSpec())

	_, agentCtx, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentCtx.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be set in forceConclusion path")
	}
	var m map[string]any
	if err := json.Unmarshal(agentCtx.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer not valid JSON: %v", err)
	}
	if m["extra"] != true {
		t.Errorf("expected extra=true, got %v", m["extra"])
	}
}

// ============================================================
// Tests for backward compatibility (no options = same behavior)
// ============================================================

func TestNoOptions_BehaviorUnchanged(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	reg := baseRegistry()
	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	f, agentCtx, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}

	calls := client.allCalls()
	expected := BuildSystemPrompt(reg, DefaultPromptSpec())
	if calls[0].Messages[0].Content != expected {
		t.Error("expected default prompt when no promptBuilder")
	}
	if calls[0].Parameters != nil {
		t.Error("expected nil Parameters when no paramsBuilder")
	}
	_ = agentCtx
}

func TestParamsBuilder_PassedToAllCalls(t *testing.T) {
	// When tool calls happen, every Chat call should have the extra params
	reg := baseRegistry()
	reg.Register(&mockTool{name: "search", result: "found"})

	extra := map[string]any{"temperature": 0.1}
	client := newMockClient(
		toolCallResponse("search"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithParamsBuilder(func(opts RunOptions) map[string]any {
			return extra
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	for i, c := range calls {
		if c.Parameters == nil {
			t.Errorf("call %d: expected Parameters to be set", i)
		} else if c.Parameters["temperature"] != 0.1 {
			t.Errorf("call %d: expected temperature=0.1, got %v", i, c.Parameters["temperature"])
		}
	}
}
