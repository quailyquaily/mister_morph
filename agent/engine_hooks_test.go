package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
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

func requestContains(calls []llm.Request, callIndex int, needle string) bool {
	if callIndex < 0 || callIndex >= len(calls) {
		return false
	}
	for _, msg := range calls[callIndex].Messages {
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}

func requestScene(calls []llm.Request, callIndex int) string {
	if callIndex < 0 || callIndex >= len(calls) {
		return ""
	}
	return calls[callIndex].Scene
}

// --- mock tool ---

type mockTool struct {
	name   string
	result string
	err    error
}

func (t *mockTool) Name() string            { return t.name }
func (t *mockTool) Description() string     { return "mock tool" }
func (t *mockTool) ParameterSchema() string { return "{}" }
func (t *mockTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return t.result, t.err
}

func TestRun_SetsRequestScene(t *testing.T) {
	client := newMockClient(llm.Result{Text: `{"type":"final","output":"ok"}`})
	e := New(client, tools.NewRegistry(), Config{}, DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "test", RunOptions{Scene: "cli.loop"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if requestScene(calls, 0) != "cli.loop" {
		t.Fatalf("scene = %q, want %q", requestScene(calls, 0), "cli.loop")
	}
}

type countingTool struct {
	name   string
	result string
	count  *int
}

func (t *countingTool) Name() string            { return t.name }
func (t *countingTool) Description() string     { return "counting tool" }
func (t *countingTool) ParameterSchema() string { return "{}" }
func (t *countingTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	if t.count != nil {
		*t.count = *t.count + 1
	}
	return t.result, nil
}

type scriptedTool struct {
	name     string
	outcomes []error
	count    int
}

func (t *scriptedTool) Name() string            { return t.name }
func (t *scriptedTool) Description() string     { return "scripted tool" }
func (t *scriptedTool) ParameterSchema() string { return "{}" }
func (t *scriptedTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	t.count++
	i := t.count - 1
	if i >= 0 && i < len(t.outcomes) && t.outcomes[i] != nil {
		return "", t.outcomes[i]
	}
	return "ok", nil
}

// --- helpers ---

func finalResponse(output string) llm.Result {
	return llm.Result{
		Text: fmt.Sprintf(`{"type":"final","reasoning":"t","output":"%s"}`, output),
	}
}

func toolCallResponse(toolName string) llm.Result {
	return llm.Result{
		ToolCalls: []llm.ToolCall{{
			Name:      toolName,
			Arguments: map[string]any{},
		}},
	}
}

func baseCfg() Config {
	return Config{MaxSteps: 5}
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

func TestWithSystemPromptCacheControl_SetsField(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(
		client,
		baseRegistry(),
		baseCfg(),
		DefaultPromptSpec(),
		WithSystemPromptCacheControl(&llm.CacheControl{TTL: "5m"}),
	)
	if e.systemPromptCacheControl == nil {
		t.Fatal("expected systemPromptCacheControl to be set")
	}
	if e.systemPromptCacheControl.TTL != "5m" {
		t.Fatalf("systemPromptCacheControl.TTL = %q, want 5m", e.systemPromptCacheControl.TTL)
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

func TestEngineConfig_DefaultToolRepeatLimit(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), Config{}, DefaultPromptSpec())
	if e.config.ToolRepeatLimit != 64 {
		t.Fatalf("tool repeat limit = %d, want 64", e.config.ToolRepeatLimit)
	}
}

func TestToolRepeatLimit_Configurable(t *testing.T) {
	reg := baseRegistry()
	searchCount := 0
	reg.Register(&countingTool{name: "search", result: "ok", count: &searchCount})
	client := newMockClient(
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "first"},
			}},
		},
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "second"},
			}},
		},
		finalResponse("forced"),
	)
	e := New(client, reg, Config{MaxSteps: 5, ToolRepeatLimit: 1}, DefaultPromptSpec())

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "forced" {
		t.Fatalf("unexpected final output: %#v", f)
	}
	if searchCount != 1 {
		t.Fatalf("search execute count = %d, want 1", searchCount)
	}
	calls := client.allCalls()
	if len(calls) != 3 {
		t.Fatalf("chat calls = %d, want 3", len(calls))
	}
	if !requestContains(calls, 2, "ERR_TOOL_REPEAT_LIMIT") {
		t.Fatal("expected ERR_TOOL_REPEAT_LIMIT in follow-up model request")
	}
}

func TestToolCallRepeat_AllowsNonConsecutiveDuplicateToolCalls(t *testing.T) {
	reg := baseRegistry()
	searchCount := 0
	otherCount := 0
	reg.Register(&countingTool{name: "search", result: "search_ok", count: &searchCount})
	reg.Register(&countingTool{name: "other", result: "other_ok", count: &otherCount})

	client := newMockClient(
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "block layoffs"},
			}},
		},
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "other",
				Arguments: map[string]any{"id": 1},
			}},
		},
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "block layoffs"},
			}},
		},
		finalResponse("forced"),
	)
	e := New(client, reg, Config{MaxSteps: 8, ToolRepeatLimit: 10}, DefaultPromptSpec())

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "forced" {
		t.Fatalf("unexpected final output: %#v", f)
	}
	if searchCount != 2 {
		t.Fatalf("search execute count = %d, want 2", searchCount)
	}
	if otherCount != 1 {
		t.Fatalf("other execute count = %d, want 1", otherCount)
	}
	calls := client.allCalls()
	if len(calls) != 4 {
		t.Fatalf("chat calls = %d, want 4", len(calls))
	}
}

func TestToolTracking_RebuildOnlyCountsSuccessfulSteps(t *testing.T) {
	steps := []Step{
		{
			Action:      "search",
			ActionInput: map[string]any{"q": "ok"},
		},
		{
			Action:      "search",
			ActionInput: map[string]any{"q": "fail"},
			Error:       fmt.Errorf("blocked"),
		},
		{
			Action:      "other",
			ActionInput: map[string]any{"id": 1},
		},
	}

	counts := rebuildToolTrackingFromSteps(steps)
	if counts["search"] != 1 {
		t.Fatalf("search count = %d, want 1", counts["search"])
	}
	if counts["other"] != 1 {
		t.Fatalf("other count = %d, want 1", counts["other"])
	}
	if len(counts) != 2 {
		t.Fatalf("counts size = %d, want 2", len(counts))
	}
}

func TestToolTracking_FailedCallDoesNotConsumeLimit(t *testing.T) {
	reg := baseRegistry()
	st := &scriptedTool{
		name:     "search",
		outcomes: []error{fmt.Errorf("temporary failure"), nil},
	}
	reg.Register(st)
	client := newMockClient(
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "same"},
			}},
		},
		llm.Result{
			ToolCalls: []llm.ToolCall{{
				Name:      "search",
				Arguments: map[string]any{"q": "same"},
			}},
		},
		finalResponse("done"),
	)
	e := New(client, reg, Config{MaxSteps: 6, ToolRepeatLimit: 1}, DefaultPromptSpec())

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "done" {
		t.Fatalf("unexpected final output: %#v", f)
	}
	if st.count != 2 {
		t.Fatalf("execute count = %d, want 2", st.count)
	}
	calls := client.allCalls()
	if len(calls) != 3 {
		t.Fatalf("chat calls = %d, want 3", len(calls))
	}
	if requestContains(calls, 2, "ERR_TOOL_REPEAT_LIMIT") {
		t.Fatal("failed first call should not trigger repeat-limit block on second call")
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

func TestRun_AddsSystemPromptCacheControlPart(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	reg := baseRegistry()

	e := New(
		client,
		reg,
		baseCfg(),
		DefaultPromptSpec(),
		WithSystemPromptCacheControl(&llm.CacheControl{TTL: "1h"}),
	)

	_, _, err := e.Run(context.Background(), "test task", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one LLM call")
	}

	expected := BuildSystemPrompt(reg, DefaultPromptSpec())
	msg := calls[0].Messages[0]
	if msg.Content != expected {
		t.Fatalf("system prompt content = %q, want %q", msg.Content, expected)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("system prompt parts len = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != llm.PartTypeText {
		t.Fatalf("system prompt part type = %q, want text", msg.Parts[0].Type)
	}
	if msg.Parts[0].Text != expected {
		t.Fatalf("system prompt part text = %q, want %q", msg.Parts[0].Text, expected)
	}
	if msg.Parts[0].CacheControl == nil || msg.Parts[0].CacheControl.TTL != "1h" {
		t.Fatalf("system prompt cache control = %#v, want TTL 1h", msg.Parts[0].CacheControl)
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
	resp := `{"type":"final","reasoning":"done","output":"result","custom_field":42}`
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
	resp := `{"type":"final_answer","reasoning":"done","output":"result","domain_data":"x"}`
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

func TestPlanStepUpdate_CalledWhenPlanResponseArrives(t *testing.T) {
	client := newMockClient(
		llm.Result{Text: `{"type":"plan","reasoning":"plan it","steps":[{"step":"collect data"},{"step":"summarize"}]}`},
		finalResponse("done"),
	)

	var updates []PlanStepUpdate
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(),
		WithPlanStepUpdate(func(_ *Context, update PlanStepUpdate) {
			updates = append(updates, update)
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected at least one plan update")
	}
	if updates[0].CompletedIndex != -1 {
		t.Fatalf("completed index = %d, want -1 for initial plan", updates[0].CompletedIndex)
	}
	if updates[0].StartedIndex != 0 {
		t.Fatalf("started index = %d, want 0", updates[0].StartedIndex)
	}
	if updates[0].StartedStep != "collect data" {
		t.Fatalf("started step = %q, want %q", updates[0].StartedStep, "collect data")
	}
	if updates[0].Reason != "plan_created" {
		t.Fatalf("reason = %q, want %q", updates[0].Reason, "plan_created")
	}
}

func TestRepeatedPlanPromptsForToolOrFinalAndKeepsOriginalPlan(t *testing.T) {
	client := newMockClient(
		llm.Result{Text: `{"type":"plan","steps":[{"step":"collect data","status":"in_progress"},{"step":"summarize","status":"pending"}]}`},
		llm.Result{Text: `{"type":"plan","steps":[{"step":"wrong replacement","status":"in_progress"}]}`},
		finalResponse("done"),
	)

	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	final, agentCtx, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final == nil || final.Plan == nil {
		t.Fatal("expected final plan to be preserved")
	}
	if len(final.Plan.Steps) != 2 || final.Plan.Steps[0].Step != "collect data" {
		t.Fatalf("final plan = %#v, want original plan preserved", final.Plan)
	}
	if agentCtx.Plan == nil || len(agentCtx.Plan.Steps) != 2 || agentCtx.Plan.Steps[0].Step != "collect data" {
		t.Fatalf("agent context plan = %#v, want original plan preserved", agentCtx.Plan)
	}

	calls := client.allCalls()
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if !requestContains(calls, 2, "You already created a plan. Next response must be a tool call or final.") {
		t.Fatalf("expected repeated-plan correction in third request, got calls=%#v", calls[2].Messages)
	}
}

func TestPlanStepUpdate_CalledWhenPlanCreateSucceeds(t *testing.T) {
	reg := baseRegistry()
	reg.Register(&mockTool{
		name:   "plan_create",
		result: `{"plan":{"steps":[{"step":"collect data"},{"step":"summarize"}]}}`,
	})

	client := newMockClient(
		toolCallResponse("plan_create"),
		finalResponse("done"),
	)

	var updates []PlanStepUpdate
	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithPlanStepUpdate(func(_ *Context, update PlanStepUpdate) {
			updates = append(updates, update)
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected at least one plan update")
	}
	if updates[0].CompletedIndex != -1 {
		t.Fatalf("completed index = %d, want -1 for initial plan", updates[0].CompletedIndex)
	}
	if updates[0].StartedIndex != 0 {
		t.Fatalf("started index = %d, want 0", updates[0].StartedIndex)
	}
	if updates[0].StartedStep != "collect data" {
		t.Fatalf("started step = %q, want %q", updates[0].StartedStep, "collect data")
	}
	if updates[0].Reason != "plan_created" {
		t.Fatalf("reason = %q, want %q", updates[0].Reason, "plan_created")
	}
}

// ============================================================
// Tests for forceConclusion() hook points
// ============================================================

func TestFallbackFinal_UsedOnForceConclusionLLMError(t *testing.T) {
	// Setup: 1 step, 0 parse retries → parse failure breaks loop → forceConclusion
	// forceConclusion's Chat call fails because no more responses → fallback should be used
	client2 := newMockClient(
		llm.Result{Text: "not json"}, // main loop: parse failure
		// forceConclusion: no response → error
	)
	e2 := New(client2, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0}, DefaultPromptSpec(),
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
}

func TestFallbackFinal_DefaultWhenNotSet(t *testing.T) {
	// Without fallbackFinal, forceConclusion should return the built-in fallback output.
	client := newMockClient(
		llm.Result{Text: "not json"},
		// forceConclusion: no response → error → default fallback
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0}, DefaultPromptSpec())

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}
	expected := buildForceConclusionFallbackOutput(0, forceConclusionReasonModelCallFailed)
	if f.Output != expected {
		t.Errorf("expected default fallback output=%q, got %v", expected, f.Output)
	}
}

func TestFallbackFinal_UsedOnParseError(t *testing.T) {
	// forceConclusion gets valid LLM response but parse fails → fallback used
	client := newMockClient(
		llm.Result{Text: "not json"},             // main loop parse fail
		llm.Result{Text: "still not valid json"}, // forceConclusion parse fail
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0}, DefaultPromptSpec(),
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

func TestFallbackFinal_UsedOnInvalidType(t *testing.T) {
	// forceConclusion gets valid JSON but with non-final type → fallback used
	client := newMockClient(
		llm.Result{Text: "not json"},                   // main loop parse fail
		llm.Result{Text: `{"type":"plan","steps":[]}`}, // forceConclusion: valid but wrong type
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0}, DefaultPromptSpec(),
		WithFallbackFinal(func() *Final {
			return &Final{Output: "type_fallback"}
		}),
	)

	f, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil Final")
	}
	if f.Output != "type_fallback" {
		t.Errorf("expected fallback output='type_fallback', got %v", f.Output)
	}
}

func TestForceConclusion_RawFinalAnswer_Set(t *testing.T) {
	// Main loop exhausts with parse failure, forceConclusion succeeds with final
	resp := `{"type":"final","reasoning":"forced","output":"result","extra":true}`
	client := newMockClient(
		llm.Result{Text: "not json"}, // main loop parse fail
		llm.Result{Text: resp},       // forceConclusion succeeds
	)
	e := New(client, baseRegistry(), Config{MaxSteps: 5, ParseRetries: 0}, DefaultPromptSpec())

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
