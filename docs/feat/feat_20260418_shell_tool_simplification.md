---
date: 2026-04-18
title: Shell Tool Simplification
status: draft
---

# Shell Tool Simplification

## 1) Goal

This note turns the previous simplification idea into an executable design.

The goal is not to redesign shell selection in this branch.
The goal is to reduce duplicated shell execution logic while preserving the currently shipped behavior of:

- `bash`
- `powershell`

## 2) Scope

This design is intentionally narrow.

In scope:

- extract one shared shell execution path
- keep `bash` and `powershell` as separate tools
- keep current config keys
- keep current Console Settings model
- keep current prompt behavior

Out of scope:

- replacing dual shell booleans with a single shell mode
- making `powershell` feature-parity with `bash`
- changing tool names or payload shape
- redesigning registry/config/defaults architecture in the same refactor

## 3) Current Duplication

Today the two tool implementations duplicate most of the same lifecycle:

- enabled check
- `cmd` parsing
- alias expansion for `file_cache_dir` and `file_state_dir`
- deny-path and deny-token checks
- `cwd` resolution
- timeout parsing
- process launch
- stdout/stderr collection
- observation formatting

The most important difference is not the shell itself.
The most important difference is behavior above the runner:

- `bash` supports `run_in_subtask`
- `bash` emits streamed tool output events
- `powershell` does not currently do either

That means the simplification target should be:

- unify the shared execution core
- leave tool-specific behavior at the edge

## 4) Design Principles

The refactor should obey these rules:

1. No behavior regressions in normal `bash` execution.
2. No behavior regressions in normal `powershell` execution.
3. `run_in_subtask` stays bash-only until explicitly expanded later.
4. Existing observation text format stays unchanged.
5. Existing config keys stay unchanged.
6. Streaming remains opt-in at the runner boundary rather than becoming implicit.

## 5) Target Architecture

The desired structure is:

```text
BashTool.Execute
  -> parse common shell request
  -> if run_in_subtask: existing bash-only path
  -> run shared shell runner with bash launcher

PowerShellTool.Execute
  -> parse common shell request
  -> run shared shell runner with powershell launcher
```

The shared part should live in a new internal helper such as:

- `tools/builtin/shell_runner.go`

The tool files should become thin wrappers:

- `bash.go`
- `powershell.go`

## 6) Proposed Shared Types

The exact names can change, but the shape should be close to this:

```go
type shellToolCommon struct {
    ToolName        string
    Enabled         bool
    DefaultTimeout  time.Duration
    MaxOutputBytes  int
    BaseDirs        []string
    DenyPaths       []string
    DenyTokens      []string
    InjectedEnvVars []string
}

type shellInvocation struct {
    Command string
    CWD     string
    Timeout time.Duration
}

type shellRunnerSpec struct {
    Program         string
    ArgsPrefix      []string
    BuildEnv        func(injected []string) []string
    MatchDeniedPath func(cmd string, denyPaths []string) (string, bool)
    StreamOutput    bool
    EmitChunk       func(ctx context.Context, stream, text string)
}

type shellExecutionResult struct {
    ExitCode        int
    StdoutTruncated bool
    StderrTruncated bool
    Stdout          string
    Stderr          string
}
```

The point of this split is:

- `shellToolCommon` owns stable config-derived behavior
- `shellRunnerSpec` owns shell-specific launch details
- `shellInvocation` is the parsed request
- `shellExecutionResult` is the shared output contract

## 7) Proposed Shared Functions

The shared helper layer should own these functions:

```go
func prepareShellInvocation(params map[string]any, common shellToolCommon) (shellInvocation, error)
func resolveShellCWD(baseDirs []string, raw string) (string, error)
func expandShellPathAliases(baseDirs []string, cmd string) (string, error)
func runShellCommand(ctx context.Context, common shellToolCommon, spec shellRunnerSpec, inv shellInvocation) (shellExecutionResult, error)
func formatShellObservation(result shellExecutionResult) string
```

And likely also:

```go
func readCommandPipes(...)
func normalizeExecutionError(toolName string, timeout time.Duration, exitCode int, err error) error
```

These functions should not know about:

- subtask orchestration
- prompt wording
- tool registry wiring

They should only know how to execute a shell command safely and return a stable result.

## 8) What Moves Into Shared Code

Move now:

- timeout parsing from `params["timeout_seconds"]`
- alias expansion
- cwd resolution
- deny-token check
- UTF-8 cleanup
- limited stdout/stderr buffering
- common observation rendering
- process execution loop

Move with strategy hooks:

- deny-path matching
  - bash uses current token matching
  - powershell uses current Windows-normalized matching
- env construction
  - powershell still layers on top of the bash allowlist
- output streaming
  - enabled for bash
  - disabled for powershell

Do not move yet:

- `run_in_subtask`
- bash subtask result envelope helpers
- any future parity work

## 9) Bash Wrapper After Refactor

After the refactor, `BashTool.Execute` should keep its current public behavior:

1. Reject if disabled.
2. Parse common invocation via shared helper.
3. If `run_in_subtask` is true, keep current bash-specific subtask path.
4. Otherwise call the shared runner with:
   - `Program: "bash"`
   - `ArgsPrefix: []string{"-lc"}`
   - `BuildEnv: bashToolEnv`
   - `MatchDeniedPath: bashCommandDenied`
   - `StreamOutput: true`
   - `EmitChunk: bash tool output emitter`
5. Format the result with the shared observation formatter.

This preserves the bash-specific behavior while deleting duplicated low-level code.

## 10) PowerShell Wrapper After Refactor

After the refactor, `PowerShellTool.Execute` should:

1. Reject if disabled.
2. Parse common invocation via shared helper.
3. Call the shared runner with:
   - `Program: "powershell"`
   - `ArgsPrefix: []string{"-NoProfile", "-Command"}`
   - `BuildEnv: powershellToolEnv`
   - `MatchDeniedPath: powershellCommandDenied`
   - `StreamOutput: false`
4. Format the result with the shared observation formatter.

This keeps the current product decision intact:

- PowerShell works
- it stays simpler than bash for now

## 11) File Layout Proposal

Suggested file split:

- `tools/builtin/shell_runner.go`
  - shared types
  - shared runner
  - common observation formatting
- `tools/builtin/shell_paths.go`
  - alias expansion
  - cwd resolution
- `tools/builtin/shell_output.go`
  - limited buffer
  - shared pipe reading helpers

Possible alternative:

- keep it all in `shell_runner.go` first

Recommendation:

- start with one file
- split later only if it becomes noisy

That keeps the first refactor easier to review.

## 12) Migration Plan

### Step 1

Introduce shared types and pure helper functions only:

- invocation parsing
- cwd resolution
- alias expansion
- observation formatting

No behavior change yet.

### Step 2

Introduce shared process runner with optional streaming callback.

Use it first from `powershell`, because that path is simpler.

### Step 3

Switch normal `bash` execution to the shared runner while keeping:

- `run_in_subtask`
- subtask envelope code
- bash event emission

### Step 4

Delete dead duplicated helpers from `bash.go` and `powershell.go`.

### Step 5

Only after the runner refactor is stable, consider a separate follow-up for:

- shell registration/config cleanup
- shell selection model cleanup

## 13) Testing Plan

The refactor should add or preserve tests around these behaviors:

- bash env allowlist behavior
- powershell env allowlist behavior
- bash deny-path matching
- powershell deny-path Windows normalization
- cwd alias resolution
- identical observation format for both tools
- bash streaming path still emits output events
- bash `run_in_subtask` behavior unchanged

The most important regression tests are not shell-specific.
They are contract-specific:

- same error messages
- same observation layout
- same deny behavior

## 14) Risks

### Risk 1

Shared code accidentally drifts toward bash semantics and changes PowerShell behavior.

Mitigation:

- keep deny-path matcher and env builder injected
- do not normalize everything to one shell policy

### Risk 2

Streaming support leaks into PowerShell unintentionally.

Mitigation:

- make streaming an explicit `StreamOutput` option
- no default-on behavior in the runner

### Risk 3

Refactor scope grows into config and UI cleanup.

Mitigation:

- keep config keys untouched
- keep Console Settings untouched
- keep prompt logic untouched

## 15) Follow-Up Opportunities

After the runner extraction is complete, the next reasonable simplifications are:

- collapse `StaticBashConfig` and `StaticPowerShellConfig` around a shared shell config sub-struct
- reduce duplicate registration code in `internal/toolsutil/static_register.go`
- later decide whether shell enablement should remain two booleans or become one mode

These should be separate changes.

## 16) Recommendation

The recommended next implementation is:

1. build a shared shell runner
2. migrate `powershell` first
3. migrate normal `bash` execution second
4. leave shell selection/config redesign for later

That is the lowest-risk path that still meaningfully reduces overdesign.
