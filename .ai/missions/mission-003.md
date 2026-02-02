# Mission 003: Core Agent Engine Bugs

## Task Background

Fix three bugs in the agent engine identified in GitHub Issue #1:

- **BUG-1 (High)**: `agent/context.go:46-48` — Token-counting fallback checks cumulative `TotalTokens == 0`, so after round 1 the fallback is never triggered again even when later rounds report `TotalTokens=0`.
- **BUG-4 (Low)**: `agent/engine_loop.go:189-198, 220-227` — Final thought and tool thought are each logged at both Info and Debug under separate `IncludeThoughts` checks, producing duplicate output.
- **RES-5 (Medium)**: `agent/engine_loop.go:284-287` — Each step appends full observation text to message history with no truncation, risking LLM context window overflow on long runs.

## Acceptance Criteria

1. Multi-round `AddUsage` with `TotalTokens=0` accumulates `InputTokens + OutputTokens` correctly via fallback every round.
2. No duplicate log lines for final or tool thoughts when `IncludeThoughts` is true.
3. Long tool observations are truncated before being appended to message history.
4. All existing tests pass.

## Files

- `agent/context.go` — AddUsage fix
- `agent/context_test.go` — AddUsage tests
- `agent/engine_loop.go` — logging dedup + observation truncation
- `agent/engine_loop_test.go` — new test file for engine loop behavior

## Implementation Steps

1. RED: Write test for multi-round AddUsage fallback → verify failure
2. GREEN: Fix AddUsage in context.go → verify pass
3. RED: Write test verifying no duplicate log entries → verify failure
4. GREEN: Fix duplicate logging in engine_loop.go → verify pass
5. RED: Write test for observation truncation in messages → verify failure
6. GREEN: Add truncation logic in engine_loop.go → verify pass
7. Run full test suite: `go test ./...`

## Results

See commit 836d45d.

### Changes

**BUG-1 — AddUsage fallback** (`agent/context.go:44-48`): Changed the fallback condition from checking the cumulative `c.Metrics.TotalTokens == 0` to checking the per-call `usage.TotalTokens == 0`. When a provider reports `TotalTokens=0`, the fallback now correctly adds `InputTokens + OutputTokens` every round instead of only on the first call.

**BUG-4 — Duplicate logging** (`agent/engine_loop.go:189-193, 214-218`): Removed the redundant Debug-level log blocks for `final_thought` and `tool_thought` that duplicated the Info-level entries when `IncludeThoughts` was enabled. Now: `IncludeThoughts=true` → single Info entry with thought content; `IncludeThoughts=false` → single Info/Debug entry with thought length only.

**RES-5 — Observation truncation** (`agent/engine_loop.go:18, 281-284`): Added `maxObservationChars` constant (128 KB) and truncation logic before appending tool results to the message history. The full observation is still recorded in the `Step` struct for local context; only the LLM-facing message is truncated.

### Files modified

- `agent/context.go:44-48` — AddUsage fallback fix
- `agent/context_test.go:39-78` — Added `TestAddUsageFallbackMultiRound`
- `agent/engine_loop.go:18, 189-193, 214-218, 281-287` — Logging dedup + truncation
- `agent/engine_loop_test.go` (new) — `TestFinalThought_NoDuplicateLog`, `TestToolThought_NoDuplicateLog`, `TestLongObservation_TruncatedInMessages`

### Verification

Ran `go test ./... -v` — all packages pass, 0 failures (36 agent tests, plus cmd, llm, skills, tools/builtin).

## Spec Compliance Report

### Acceptance Criteria Verification

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Multi-round `AddUsage` with `TotalTokens=0` accumulates `InputTokens + OutputTokens` correctly via fallback every round. | PASS | `agent/context.go:45-49` — condition changed from `c.Metrics.TotalTokens == 0` (cumulative) to `usage.TotalTokens > 0` (per-call). Fallback branch at line 48 adds `InputTokens + OutputTokens` each round. Test `TestAddUsageFallbackMultiRound` (`agent/context_test.go:55-78`) exercises 3 consecutive rounds with `TotalTokens=0` and asserts cumulative 150 → 450 → 525. |
| 2 | No duplicate log lines for final or tool thoughts when `IncludeThoughts` is true. | PASS | `agent/engine_loop.go:194-198` — final thought has a single `if/else` block: Info when `IncludeThoughts=true`, Info with length-only otherwise. The redundant Debug-level `final_thought` / `final_thought_len` block was removed (visible in diff). `agent/engine_loop.go:220-224` — tool thought likewise uses a single `if/else`: Info with content vs Debug with length. The duplicate `if IncludeThoughts { Debug(...) }` block was removed. Tests `TestFinalThought_NoDuplicateLog` (`agent/engine_loop_test.go:80-106`) and `TestToolThought_NoDuplicateLog` (`agent/engine_loop_test.go:108-134`) capture logs and assert exactly 1 entry per message key with 0 duplicates. |
| 3 | Long tool observations are truncated before being appended to message history. | PASS | `agent/engine_loop.go:18` — `maxObservationChars` constant set to `128 * 1024` (128 KB). `agent/engine_loop.go:281-284` — `msgObservation` is truncated to `maxObservationChars` with `"\n...(truncated)"` suffix before appending to `st.messages`. The full observation is still stored in the `Step` struct at line 236. Test `TestLongObservation_TruncatedInMessages` (`agent/engine_loop_test.go:140-172`) creates a 300 KB observation and asserts the LLM-facing message is under 200 KB. |
| 4 | All existing tests pass. | PASS | `go test ./...` — all packages pass: agent (0.160s), cmd/mister_morph (0.254s), llm (cached), skills (0.356s), tools/builtin (0.393s). 0 failures. |

### Scope Compliance
- Scope creep: None — only the 4 files listed in the spec were modified (plus `.ai/missions/` tracking docs).
- Missing scope: None — all 4 files addressed, all 3 bugs fixed, all 4 acceptance criteria met.

### Verdict: PASS
