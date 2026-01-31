# Mission 001: mister_morph 基础类型扩展

## Task Background

mister_morph 的 `Final{Thought, Output, Plan}` 在 JSON 反序列化时会丢失领域自定义字段（如 sourcefinder 的 `sources`、`truth_assessment`）。同时 `llm.Usage` 缺少费用追踪。本 mission 在类型层和解析层补齐这两个能力，为后续 engine hooks 和 sourcefinder 迁移打基础。

## Files List

| File | Change |
|---|---|
| `llm/llm.go` | `Usage` struct 增加 `Cost float64` 字段 |
| `agent/types.go` | `AgentResponse` 增加 `RawFinalAnswer json.RawMessage` (tag: `json:"-"`) |
| `agent/parser.go` | `unmarshalAndValidate()` 对 final 类型二次提取原始 JSON 并赋值到 `RawFinalAnswer` |
| `agent/context.go` | `Context` 增加 `RawFinalAnswer json.RawMessage`；`AddUsage()` 增加 `c.Metrics.TotalCost += usage.Cost` |

## Logic Pseudocode

### llm/llm.go
```
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    Cost         float64   // NEW
}
```

### agent/types.go
```
type AgentResponse struct {
    // existing fields unchanged
    RawFinalAnswer json.RawMessage `json:"-"`  // NEW: raw bytes of final/final_answer payload
}
```

### agent/parser.go — unmarshalAndValidate()
```
after json.Unmarshal(data, &resp) succeeds:

if resp.Type == TypeFinal || resp.Type == TypeFinalAnswer:
    var raw struct {
        Final       json.RawMessage `json:"final,omitempty"`
        FinalAnswer json.RawMessage `json:"final_answer,omitempty"`
    }
    if json.Unmarshal(data, &raw) == nil:
        if len(raw.FinalAnswer) > 0:
            resp.RawFinalAnswer = raw.FinalAnswer
        else:
            resp.RawFinalAnswer = raw.Final
```

### agent/context.go
```
type Context struct {
    // existing fields
    RawFinalAnswer json.RawMessage  // NEW
}

func (c *Context) AddUsage(usage llm.Usage, dur time.Duration):
    // existing logic
    c.Metrics.TotalCost += usage.Cost  // NEW line
```

## Acceptance Criteria

1. `go build ./...` passes with no errors
2. Existing code is unaffected — `Cost` defaults to 0, `RawFinalAnswer` defaults to nil
3. `ParseResponse` for a `final_answer` type response populates `RawFinalAnswer` with the raw JSON bytes of the `final_answer` object
4. `ParseResponse` for a `tool_call` type response leaves `RawFinalAnswer` as nil
5. `AddUsage` with `Usage{Cost: 0.05}` increments `Metrics.TotalCost` by 0.05

## Human Notes

(empty)

## Results

See commit 8e764cd.

### Changes

- `llm/llm.go:17` — Added `Cost float64` field to `Usage` struct
- `agent/types.go:70` — Added `RawFinalAnswer json.RawMessage` with `json:"-"` tag to `AgentResponse`
- `agent/parser.go:80-93` — After initial unmarshal in `unmarshalAndValidate()`, performs a second unmarshal for `final`/`final_answer` types to extract the raw JSON bytes into `resp.RawFinalAnswer`
- `agent/context.go:4` — Added `encoding/json` import
- `agent/context.go:24` — Added `RawFinalAnswer json.RawMessage` field to `Context`
- `agent/context.go:49` — Added `c.Metrics.TotalCost += usage.Cost` to `AddUsage()`

### Tests added

- `llm/llm_test.go` — Tests `Usage.Cost` field exists and defaults to zero
- `agent/parser_test.go` — Tests `RawFinalAnswer` populated for `final_answer` type (preserving domain-specific fields like `sources`), populated for `final` type (preserving `truth_assessment`), and nil for `tool_call` type
- `agent/context_test.go` — Tests `Context.RawFinalAnswer` field exists/defaults to nil, is assignable, and `AddUsage` accumulates cost correctly across multiple calls

### Acceptance criteria verification

All 5 acceptance criteria pass: `go build ./...` succeeds, defaults are zero/nil, `ParseResponse` populates `RawFinalAnswer` for final types and leaves nil for tool_call, and `AddUsage` accumulates cost.

### No deviations from spec.

## Review

(empty)
