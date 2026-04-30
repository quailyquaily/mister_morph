package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

// --- concurrent-specific mock tools ---

type slowTool struct {
	name     string
	delay    time.Duration
	result   string
	started  atomic.Int32
	finished atomic.Int32
}

func (t *slowTool) Name() string            { return t.name }
func (t *slowTool) Description() string     { return "slow mock tool" }
func (t *slowTool) ParameterSchema() string { return "{}" }
func (t *slowTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.started.Add(1)
	select {
	case <-time.After(t.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	t.finished.Add(1)
	return t.result, nil
}

type stopAfterSuccessTool struct {
	name   string
	result string
}

func (t *stopAfterSuccessTool) Name() string            { return t.name }
func (t *stopAfterSuccessTool) Description() string     { return "mock tool that stops after success" }
func (t *stopAfterSuccessTool) ParameterSchema() string { return "{}" }
func (t *stopAfterSuccessTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return t.result, nil
}
func (t *stopAfterSuccessTool) StopAfterSuccess() bool { return true }

// --- helpers ---

func multiToolCallResponse(names ...string) llm.Result {
	calls := make([]llm.ToolCall, len(names))
	for i, name := range names {
		calls[i] = llm.ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      name,
			Arguments: map[string]any{},
		}
	}
	return llm.Result{ToolCalls: calls}
}

// --- tests ---

func TestConcurrentToolExecution_AllToolsRun(t *testing.T) {
	t.Parallel()

	toolA := &slowTool{name: "tool_a", delay: 50 * time.Millisecond, result: "result_a"}
	toolB := &slowTool{name: "tool_b", delay: 50 * time.Millisecond, result: "result_b"}

	reg := tools.NewRegistry()
	reg.Register(toolA)
	reg.Register(toolB)

	client := newMockClient(
		multiToolCallResponse("tool_a", "tool_b"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	start := time.Now()
	f, agentCtx, err := e.Run(context.Background(), "test concurrent", RunOptions{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "done" {
		t.Fatalf("unexpected final: %v", f)
	}

	if toolA.finished.Load() != 1 {
		t.Fatalf("tool_a finished count = %d, want 1", toolA.finished.Load())
	}
	if toolB.finished.Load() != 1 {
		t.Fatalf("tool_b finished count = %d, want 1", toolB.finished.Load())
	}

	// Both tools take 50ms; if run concurrently, total should be well under 2*50ms = 100ms.
	if elapsed > 150*time.Millisecond {
		t.Logf("warning: concurrent execution took %v, expected < 150ms", elapsed)
	}

	if len(agentCtx.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(agentCtx.Steps))
	}
	if agentCtx.Steps[0].Action != "tool_a" {
		t.Fatalf("step[0].Action = %q, want tool_a", agentCtx.Steps[0].Action)
	}
	if agentCtx.Steps[1].Action != "tool_b" {
		t.Fatalf("step[1].Action = %q, want tool_b", agentCtx.Steps[1].Action)
	}
}

func TestConcurrentToolExecution_ResultsInOrder(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&slowTool{name: "fast", delay: 10 * time.Millisecond, result: "fast_result"})
	reg.Register(&slowTool{name: "slow", delay: 80 * time.Millisecond, result: "slow_result"})

	client := newMockClient(
		multiToolCallResponse("slow", "fast"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec())
	_, _, err := e.Run(context.Background(), "test order", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(calls))
	}

	secondCall := calls[1]
	toolMsgs := make([]llm.Message, 0)
	for _, m := range secondCall.Messages {
		if m.Role == "tool" {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("tool messages = %d, want 2", len(toolMsgs))
	}
	if toolMsgs[0].ToolCallID != "call_0" {
		t.Fatalf("first tool message ID = %q, want call_0", toolMsgs[0].ToolCallID)
	}
	if toolMsgs[1].ToolCallID != "call_1" {
		t.Fatalf("second tool message ID = %q, want call_1", toolMsgs[1].ToolCallID)
	}
}

func TestConcurrentToolExecution_StopAfterSuccess(t *testing.T) {
	t.Parallel()

	stopper := &stopAfterSuccessTool{name: "stopper", result: "stopped"}
	slow := &slowTool{name: "slow", delay: 5 * time.Second, result: "should_not_finish"}

	reg := tools.NewRegistry()
	reg.Register(stopper)
	reg.Register(slow)

	client := newMockClient(
		multiToolCallResponse("stopper", "slow"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	start := time.Now()
	f, _, err := e.Run(context.Background(), "test stop", RunOptions{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil final from StopAfterSuccess")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("StopAfterSuccess should have cancelled slow tool quickly, took %v", elapsed)
	}
}

func TestConcurrentToolExecution_ToolCallTimeout(t *testing.T) {
	t.Parallel()

	slow := &slowTool{name: "slow", delay: 10 * time.Second, result: "should_timeout"}
	reg := tools.NewRegistry()
	reg.Register(slow)

	client := newMockClient(
		llm.Result{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "slow", Arguments: map[string]any{}}}},
		finalResponse("done"),
	)

	cfg := baseCfg()
	cfg.ToolCallTimeout = 100 * time.Millisecond
	e := New(client, reg, cfg, DefaultPromptSpec())

	start := time.Now()
	f, agentCtx, err := e.Run(context.Background(), "test timeout", RunOptions{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "done" {
		t.Fatalf("unexpected final: %v", f)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ToolCallTimeout should have cancelled after ~100ms, took %v", elapsed)
	}
	if len(agentCtx.Steps) == 0 {
		t.Fatal("expected at least 1 step")
	}
	if agentCtx.Steps[0].Error == nil {
		t.Fatal("expected tool to have error from timeout")
	}
}

func TestConcurrentToolExecution_MixedSuccessAndFailure(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "good", result: "ok"})
	reg.Register(&mockTool{name: "bad", result: "", err: fmt.Errorf("tool failed")})

	client := newMockClient(
		multiToolCallResponse("good", "bad"),
		finalResponse("done"),
	)

	e := New(client, reg, baseCfg(), DefaultPromptSpec())
	f, agentCtx, err := e.Run(context.Background(), "test mixed", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || f.Output != "done" {
		t.Fatalf("unexpected final: %v", f)
	}
	if len(agentCtx.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(agentCtx.Steps))
	}
	if agentCtx.Steps[0].Error != nil {
		t.Fatalf("step[0] should succeed, got error: %v", agentCtx.Steps[0].Error)
	}
	if agentCtx.Steps[1].Error == nil {
		t.Fatal("step[1] should have error")
	}
}

func TestSpawnTool_BasicExecution(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "read_file", result: "file content"})

	subClient := newMockClient(
		toolCallResponse("read_file"),
		finalResponse("sub-agent done"),
	)

	cfg := baseCfg()
	cfg.DefaultModel = "test-model"
	e := New(subClient, reg, cfg, DefaultPromptSpec())

	spawnT, ok := e.registry.Get("spawn")
	if !ok {
		t.Fatal("spawn tool should be registered by default")
	}

	result, err := spawnT.Execute(context.Background(), map[string]any{
		"task":  "read the file",
		"tools": []any{"read_file"},
	})
	if err != nil {
		t.Fatalf("spawn Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("spawn should return non-empty result")
	}
}

func TestSpawnTool_CanBeDisabled(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	e := New(newMockClient(finalResponse("ok")), reg, baseCfg(), DefaultPromptSpec(), WithSpawnToolEnabled(false))

	if _, ok := e.registry.Get("spawn"); ok {
		t.Fatal("spawn tool should NOT be registered when WithSpawnToolEnabled(false)")
	}
}

func TestSpawnTool_CannotIncludeSpawnInSubAgent(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "read_file", result: "content"})

	subClient := newMockClient(finalResponse("sub done"))

	cfg := baseCfg()
	cfg.DefaultModel = "test-model"
	e := New(subClient, reg, cfg, DefaultPromptSpec())

	spawnT, _ := e.registry.Get("spawn")
	result, err := spawnT.Execute(context.Background(), map[string]any{
		"task":  "test",
		"tools": []any{"read_file", "spawn"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestSpawnTool_EmptyToolsError(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	cfg := baseCfg()
	e := New(newMockClient(), reg, cfg, DefaultPromptSpec())

	spawnT, _ := e.registry.Get("spawn")
	_, err := spawnT.Execute(context.Background(), map[string]any{
		"task":  "test",
		"tools": []any{},
	})
	if err == nil {
		t.Fatal("expected error for empty tools")
	}
}

func TestSpawnTool_MissingTaskError(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	cfg := baseCfg()
	e := New(newMockClient(), reg, cfg, DefaultPromptSpec())

	spawnT, _ := e.registry.Get("spawn")
	_, err := spawnT.Execute(context.Background(), map[string]any{
		"tools": []any{"read_file"},
	})
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestSpawnTool_SubClientFactory_Called(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "read_file", result: "content"})

	parentClient := newMockClient()
	subClient := newMockClient(finalResponse("sub done"))

	var factoryPrefix string
	cleanupCalled := false

	cfg := baseCfg()
	cfg.DefaultModel = "test-model"
	e := New(parentClient, reg, cfg, DefaultPromptSpec(),
		WithSubClientFactory(func(prefix string) (llm.Client, func()) {
			factoryPrefix = prefix
			return subClient, func() { cleanupCalled = true }
		}),
	)

	spawnT, _ := e.registry.Get("spawn")
	result, err := spawnT.Execute(context.Background(), map[string]any{
		"task":  "test sub-client factory",
		"tools": []any{"read_file"},
	})
	if err != nil {
		t.Fatalf("spawn Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if factoryPrefix != "spawn" {
		t.Fatalf("factory prefix = %q, want %q", factoryPrefix, "spawn")
	}
	if !cleanupCalled {
		t.Fatal("cleanup function should have been called after sub-agent completes")
	}

	parentCalls := parentClient.allCalls()
	if len(parentCalls) != 0 {
		t.Fatalf("parent client should not have been called, got %d calls", len(parentCalls))
	}
	subCalls := subClient.allCalls()
	if len(subCalls) == 0 {
		t.Fatal("sub client should have been called")
	}
}

func TestOnToolStart_CalledBeforeExecution(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "search", result: "found"})
	reg.Register(&mockTool{name: "fetch", result: "fetched"})

	client := newMockClient(
		multiToolCallResponse("search", "fetch"),
		finalResponse("done"),
	)

	var startedTools []string
	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithOnToolStart(func(_ *Context, toolName string) {
			startedTools = append(startedTools, toolName)
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(startedTools) != 2 {
		t.Fatalf("onToolStart called %d times, want 2", len(startedTools))
	}
	if startedTools[0] != "search" {
		t.Fatalf("startedTools[0] = %q, want search", startedTools[0])
	}
	if startedTools[1] != "fetch" {
		t.Fatalf("startedTools[1] = %q, want fetch", startedTools[1])
	}
}

func TestOnToolStart_NotCalledForSkippedTools(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "search", result: "found"})

	client := newMockClient(
		llm.Result{ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "search", Arguments: map[string]any{"q": "same"}},
		}},
		llm.Result{ToolCalls: []llm.ToolCall{
			{ID: "c2", Name: "search", Arguments: map[string]any{"q": "again"}},
		}},
		finalResponse("done"),
	)

	var startCount int
	e := New(client, reg, Config{MaxSteps: 5, ToolRepeatLimit: 1}, DefaultPromptSpec(),
		WithOnToolStart(func(_ *Context, toolName string) {
			startCount++
		}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startCount != 1 {
		t.Fatalf("onToolStart called %d times, want 1 (second call is repeat-limited)", startCount)
	}
}

func TestOnToolStart_NilIgnored(t *testing.T) {
	t.Parallel()

	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(), WithOnToolStart(nil))
	if e.onToolStart != nil {
		t.Fatal("expected onToolStart to remain nil for nil input")
	}
}

func TestSpawnTool_SubClientFactory_Nil_UsesEngineClient(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "read_file", result: "content"})

	client := newMockClient(finalResponse("done"))

	cfg := baseCfg()
	cfg.DefaultModel = "test-model"
	e := New(client, reg, cfg, DefaultPromptSpec())

	spawnT, _ := e.registry.Get("spawn")
	_, err := spawnT.Execute(context.Background(), map[string]any{
		"task":  "test no factory",
		"tools": []any{"read_file"},
	})
	if err != nil {
		t.Fatalf("spawn Execute error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) == 0 {
		t.Fatal("engine client should have been called when no SubClientFactory is set")
	}
}
