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
