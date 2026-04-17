---
title: Subagents
description: "Typical scenarios first, then a high-level overview, then the current implementation details and test prompts."
---

# Subagents

## Common Scenarios

Use a subagent boundary mainly in these cases:

- A shell command is slow or noisy, and you want its output isolated from the parent loop.
- The work is still multi-step, but you want the inner execution to operate with a narrower tool set.
- You want one compact final result instead of leaking raw intermediate output back to the parent.
- The child work should run inside an external ACP-compatible agent instead of another local Mister Morph loop.

Choose the entry like this:

- Use `bash.run_in_subtask=true` for one concrete shell command.
- Use `spawn` when the inner execution still needs agent-style tool use such as `read_file`, `url_fetch`, or `bash`.
- Do not add an isolated layer for trivial one-step work the parent can finish directly.

## Overview

Mistermorph currently exposes three isolated-task entries:

| Entry | Starts another LLM loop | Best for | Returns |
|---|---|---|---|
| `spawn` | Yes | an inner agent that still needs tools and reasoning | `SubtaskResult` JSON envelope |
| `acp_spawn` | No local inner Mister Morph loop; starts an external ACP session instead | an external ACP-compatible agent or adapter | `SubtaskResult` JSON envelope |
| `bash.run_in_subtask=true` | No | one shell command with isolated execution/output | `SubtaskResult` JSON envelope |

Shared behavior:

- All three are synchronous. The parent waits until the inner run finishes.
- All three share the same depth limit.
- All three return the same top-level envelope shape.
- Neither path sends the raw inner transcript back into the parent loop by default.

ACP-specific note:

- `acp_spawn` still creates an inner agent boundary, but that boundary is handled by an external ACP agent process rather than another local Mister Morph engine.

This feature is about isolation and result collection. It is not a background job system yet.

## Current Implementation

### `spawn`

`spawn` is an engine-scoped tool. It appears only after an agent engine is assembled for a run.

Parameters:

- `task`: required prompt for the inner agent.
- `tools`: required non-empty tool-name array.
- `model`: optional model override for the inner agent.
- `output_schema`: optional structured-output label.
- `observe_profile`: optional observer hint. Supported values are `default`, `long_shell`, and `web_extract`.

Current behavior:

- The inner registry is built from the tool names passed in `tools`.
- Unknown or unavailable tool names are ignored.
- If no usable tool remains, the call fails.
- `spawn` is never re-exposed inside the inner agent, even if listed in `tools`.

### `acp_spawn`

`acp_spawn` is also an engine-scoped tool.

Parameters:

- `agent`: required ACP profile name from `acp.agents`
- `task`: required prompt for the external ACP agent
- `cwd`: optional working-directory override
- `output_schema`: optional structured-output label
- `observe_profile`: optional observer hint

Current behavior:

- one call creates one ACP session
- the current implementation uses `stdio` transport only
- the child path can serve ACP permission, file, and terminal callbacks
- the final result is normalized into the same `SubtaskResult` envelope used by `spawn`

For profile config and transport details, see [ACP](/guide/acp).

### `bash.run_in_subtask=true`

This is the lighter isolated-execution path.

- It uses the direct isolated path behind `bash`.
- It does not start a second LLM loop.
- Its `output_schema` is fixed to `subtask.bash.result.v1`.
- Its observer profile is fixed to `long_shell`.

Use it when the inner work is already one concrete shell step and does not need more tool decisions.

### Depth Limit

The current depth limit is `1`.

- A root run can enter one isolated extra layer.
- A run that is already inside that layer cannot enter another one.

### `output_schema`

`output_schema` is only a contract label. It is not a built-in JSON Schema registry.

If you set it for `spawn`:

- the inner agent is told to produce JSON final output;
- the runtime requires the final output to be JSON or JSON-parsable text;
- the same identifier is echoed back in the result envelope.

Mistermorph does not validate the returned object against a real schema definition.

### Result Envelope

All three entries return JSON in this shape:

```json
{
  "task_id": "sub_123",
  "status": "done",
  "summary": "subtask completed",
  "output_kind": "text",
  "output_schema": "",
  "output": "child result",
  "error": ""
}
```

Meaning of the fields:

- `status`: currently `done` or `failed`.
- `summary`: short status text for the isolated run.
- `output_kind`: `text` or `json`.
- `output_schema`: empty for plain text output, or the identifier you passed in.
- `output`: the result payload.
- `error`: set only when the run fails.

For `bash.run_in_subtask=true`, `output` is structured JSON with `exit_code`, truncation flags, `stdout`, and `stderr`.

### Test Prompts

These are good smoke tests when `spawn` and `bash` are enabled.

#### Prompt 1: `spawn` + `bash`, return one line

```text
You must call the spawn tool. Do not answer directly. Allow the inner agent to use only bash. Have it run `printf 'alpha\nbeta\ngamma\n' | sed -n '2p'`. Return only the second line.
```

Expected result: `beta`

#### Prompt 2: `spawn` + `bash`, return structured JSON

```text
You must call the spawn tool and set output_schema to `subagent.demo.echo.v1`. Allow the inner agent to use only bash. Have it run `echo '{"ok":true,"value":42}'`. Return structured JSON only, with no explanation.
```

Expected result:

```json
{"ok":true,"value":42}
```

#### Prompt 3: `bash.run_in_subtask=true`

```text
Call the bash tool and set `run_in_subtask` to true. Run `printf 'one\ntwo\nthree\n' | tail -n 1`. Do not explain anything. Return only the last line.
```

Expected result: `three`

#### Prompt 4: longer isolated shell run

```text
Call the bash tool and set `run_in_subtask` to true. Run `sleep 1; echo SUBAGENT_BASH_OK`. Reply with stdout only.
```

Expected result: `SUBAGENT_BASH_OK`

### Config and Embedding

- `tools.spawn.enabled` controls only the explicit `spawn` tool entry.
- `tools.acp_spawn.enabled` controls only the explicit `acp_spawn` tool entry.
- ACP profiles live under `acp.agents`.
- Direct isolated runs such as `bash.run_in_subtask=true` still work even if `tools.spawn.enabled=false`.
- `integration.Config.BuiltinToolNames` can include or omit `spawn` and `acp_spawn`.
- If you build an engine directly with `agent.New(...)`, `spawn` is enabled by default and `acp_spawn` is disabled by default. Override them with `agent.WithSpawnToolEnabled(...)`, `agent.WithACPSpawnToolEnabled(...)`, and `agent.WithACPAgents(...)`.

Example:

```go
cfg := integration.DefaultConfig()
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "spawn", "acp_spawn"}
cfg.Set("tools.spawn.enabled", true)
cfg.Set("tools.acp_spawn.enabled", true)
```

See also:

- [Built-in Tools](/guide/built-in-tools)
- [ACP](/guide/acp)
- [Create Your Own AI Agent: Advanced](/guide/build-your-own-agent-advanced)
- [Config Fields](/guide/config-reference)
