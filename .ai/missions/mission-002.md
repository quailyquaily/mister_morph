# Mission 002: mister_morph engine 领域 hooks

## Task Background

mister_morph 现有扩展点仅有通用 `Hook func(ctx, step, agentCtx, messages)` 和 `PromptSpec`。sourcefinder 需要：自定义 prompt 构建、LLM 请求参数注入、tool 成功回调、自定义兜底答案。本 mission 通过 4 个 `WithXxx` Option 函数扩展 engine，保持 `Run()` 签名不变。

依赖 Mission 001（使用 `RawFinalAnswer` 和 `Request.Parameters`）。

## Files List

| File | Change |
|---|---|
| `agent/engine.go` | 新增 4 个 Engine 字段、4 个 Option 函数、Run() 5 个接入点、forceConclusion() 1 个接入点 |

## Logic Pseudocode

### Engine 新增字段
```
type Engine struct {
    // existing fields unchanged
    promptBuilder  func(registry *tools.Registry, task string) string
    paramsBuilder  func(opts RunOptions) map[string]any
    onToolSuccess  func(ctx *Context, toolName string)
    fallbackFinal  func() *Final
}
```

### 4 个 Option 函数
```
func WithPromptBuilder(fn func(*tools.Registry, string) string) Option
func WithParamsBuilder(fn func(RunOptions) map[string]any) Option
func WithOnToolSuccess(fn func(*Context, string)) Option
func WithFallbackFinal(fn func() *Final) Option
```
每个函数内 nil 检查后赋值到对应 Engine 字段。

### Run() 接入点

1. **Prompt 构建** (替换 line 98):
```
if e.promptBuilder != nil:
    systemPrompt = e.promptBuilder(e.registry, task)
else:
    systemPrompt = BuildSystemPrompt(e.registry, e.spec)
```

2. **Params 构建** (Run 开头，循环前):
```
var extraParams map[string]any
if e.paramsBuilder != nil:
    extraParams = e.paramsBuilder(opts)
```

3. **LLM 调用** (循环内每次 Chat 调用):
```
result, err := e.client.Chat(ctx, llm.Request{
    Model: model, Messages: messages, ForceJSON: true,
    Parameters: extraParams,  // NEW: pass through
})
```

4. **Tool 成功回调** (tool_call case, toolErr == nil 后):
```
if toolErr == nil && e.onToolSuccess != nil:
    e.onToolSuccess(agentCtx, tc.Name)
```

5. **Final 返回前** (final/final_answer case, return 前):
```
agentCtx.RawFinalAnswer = resp.RawFinalAnswer
```

### forceConclusion() 接入点

**Fallback** (LLM 错误或解析失败时):
```
if e.fallbackFinal != nil:
    fb := e.fallbackFinal()
    return fb, agentCtx, nil
// else existing default: &Final{Output: "insufficient_evidence", ...}
```

**成功解析时** (return 前):
```
agentCtx.RawFinalAnswer = resp.RawFinalAnswer
```

## Acceptance Criteria

1. `go build ./...` passes
2. 不使用任何新 Option 时，行为与修改前完全一致
3. `WithPromptBuilder` 设置后，`BuildSystemPrompt` 不被调用，自定义函数被调用
4. `WithParamsBuilder` 返回的 map 出现在每次 `llm.Request.Parameters` 中
5. `WithOnToolSuccess` 回调在 tool 执行成功时被调用，失败时不调用
6. `WithFallbackFinal` 设置后，forceConclusion 失败时返回自定义 `*Final` 而非默认值
7. final 返回时 `agentCtx.RawFinalAnswer` 非 nil（包含原始 JSON）

## Human Notes

(empty)

## Results

See commit 3d31786.

### Changes

**`agent/engine.go`** — Added 4 new fields to Engine struct (lines 85-88), 4 `WithXxx` Option functions (lines 37-67), and 6 integration points in `Run()` and `forceConclusion()`:

- **promptBuilder** (line 135-140): When set, replaces `BuildSystemPrompt()` with custom function. When nil, default behavior unchanged.
- **paramsBuilder** (lines 178-180): Computed once before the main loop. Result passed as `Parameters` in every `llm.Request` (main loop line 196, forceConclusion line 452).
- **onToolSuccess** (line 362): Fires after successful tool execution. Guarded by `found && toolErr == nil` to exclude unknown tools and tool errors.
- **RawFinalAnswer** (lines 257, 465): Set on `agentCtx` from `resp.RawFinalAnswer` in both the final/final_answer case and forceConclusion success path.
- **fallbackFinal** (forceConclusion lines 455, 462, 469): Applied at all three failure points (LLM error, parse error, invalid type). When nil, existing `"insufficient_evidence"` default preserved.
- **forceConclusion signature** updated to accept `extraParams map[string]any` to pass through to its Chat call.

**`agent/engine_hooks_test.go`** (new) — 21 tests covering all Option functions, Run() integration points, forceConclusion hook points, backward compatibility, and edge cases (nil options, unknown tools, tool errors). Uses mock `llm.Client` and mock `tools.Tool`.

### Deviations

- `onToolSuccess` guard uses `found && toolErr == nil` (spec had `toolErr == nil`). This prevents the callback from firing when a tool is not found in the registry, which is the correct semantic since the tool didn't "succeed" — it wasn't executed at all.
- `forceConclusion` signature changed to accept `extraParams` so params are also injected in forced conclusion LLM calls (consistent with main loop behavior).
- `fallbackFinal` applied at all three error paths in forceConclusion (LLM error, parse error, invalid response type), not just one. This ensures the custom fallback is used whenever forceConclusion cannot produce a valid final.

### Acceptance Criteria Verification

1. `go build ./...` passes ✓
2. `TestNoOptions_BehaviorUnchanged` verifies identical behavior without options ✓
3. `TestPromptBuilder_OverridesDefault` + `TestPromptBuilder_DefaultUsedWhenNil` ✓
4. `TestParamsBuilder_InjectedIntoRequest` + `TestParamsBuilder_PassedToAllCalls` ✓
5. `TestOnToolSuccess_CalledOnSuccess` + `NotCalledOnError` + `NotCalledForUnknownTool` ✓
6. `TestFallbackFinal_UsedOnForceConclusionLLMError` + `UsedOnParseError` + `DefaultWhenNotSet` ✓
7. `TestRawFinalAnswer_SetOnContext` + `SetForFinalAnswerType` + `TestForceConclusion_RawFinalAnswer_Set` ✓

## Review 3d31786

Reviewing commit `3d31786` (Mission 002: Add 4 domain hook Options to Engine).

### Verification

- **`go build ./...`**: Passes, zero errors.
- **`go test ./... -v -count=1`**: All 31 tests pass (27 in `agent`, 2 in `llm`, 2 more in `agent` from Mission 001). Zero failures.

### Critical Issues

None.

### Major Issues

None.

### Minor Issues

1. **`agent/engine_hooks_test.go:388-429` — Dead code in `TestFallbackFinal_UsedOnForceConclusionLLMError`**: The first engine `e` (lines 390-399) is constructed but never used for `Run()`. Only `e2` (lines 412-416) is exercised. Line 428 `_ = e` suppresses the unused warning. The test logic is correct via `e2`, but the dead `e` block is confusing and should be cleaned up.

### Improvement Suggestions

1. **`agent/engine.go:137` — `promptBuilder` receives `task` but default path does not**: The custom `promptBuilder` signature is `func(*tools.Registry, string)` where the second arg is `task`. The default `BuildSystemPrompt(e.registry, e.spec)` does not use `task`. This is fine for the hook contract (callers may need it), but worth noting the asymmetry.

2. **`agent/engine_hooks_test.go` — No test for `fallbackFinal` on invalid-type path in forceConclusion**: Tests cover LLM error and parse error paths, but not the third path where `resp.Type` is neither `final` nor `final_answer` (engine.go:472-477). The code is correct (verified by reading), but adding a test for this branch would improve coverage.

### Test Gaps / Residual Risks

1. **No integration test for `extraParams` in `forceConclusion`**: `TestParamsBuilder_PassedToAllCalls` verifies params in the main loop, but does not exercise the `forceConclusion` path. The code at engine.go:449-453 correctly passes `extraParams`, verified by reading.
2. **`onToolSuccess` callback receives `agentCtx` by pointer**: Callers can mutate engine state. This is by design (noted in spec), but downstream consumers should be aware.

### Acceptance Criteria Checklist

| # | Criterion | Verdict |
|---|---|---|
| 1 | `go build ./...` passes | PASS — verified |
| 2 | No options → behavior unchanged | PASS — `TestNoOptions_BehaviorUnchanged` |
| 3 | `WithPromptBuilder` overrides `BuildSystemPrompt` | PASS — `TestPromptBuilder_OverridesDefault`, engine.go:136-140 |
| 4 | `WithParamsBuilder` map in every `llm.Request.Parameters` | PASS — `TestParamsBuilder_InjectedIntoRequest`, `TestParamsBuilder_PassedToAllCalls`, engine.go:203 |
| 5 | `WithOnToolSuccess` called on success, not on failure | PASS — `TestOnToolSuccess_CalledOnSuccess`, `NotCalledOnError`, `NotCalledForUnknownTool`, engine.go:368-370 |
| 6 | `WithFallbackFinal` returns custom `*Final` on forceConclusion failure | PASS — `TestFallbackFinal_UsedOnForceConclusionLLMError`, `UsedOnParseError`, `DefaultWhenNotSet`, engine.go:457-477 |
| 7 | `agentCtx.RawFinalAnswer` set on final return | PASS — `TestRawFinalAnswer_SetOnContext`, `SetForFinalAnswerType`, `TestForceConclusion_RawFinalAnswer_Set`, engine.go:264,479 |

### Verdict

All 7 acceptance criteria met. Implementation matches spec with reasonable deviations (documented in Results). No correctness bugs, no security risks, no regressions. Tests cover all hook functions across main loop and forceConclusion paths.

**Eval**: FactualAccuracy(5) Completeness(5) CitationPrecision(5) ToolEfficiency(5) TokenBudget(4) = 24/25 (Excellent)

**Notes**: Clean implementation. 558-line test file is thorough but has minor dead code (the unused `e` in one test). TokenBudget docked 1 point for test file size relative to 82 lines of production changes, though coverage is comprehensive.
