---
title: ACP
description: Use external ACP agents through acp_spawn.
---

# ACP

Mister Morph can delegate one isolated task to an external ACP agent.

Today, ACP support is intentionally narrow:

- Mister Morph is an ACP client, not an ACP server.
- Transport is `stdio` only.
- Each `acp_spawn` call creates one synchronous session and one prompt turn.
- The external agent is started from `acp.agents`.

## When to Use ACP

Use ACP when the child task should run inside an external agent stack instead of another local Mister Morph loop.

Typical examples:

- run Codex through an ACP adapter
- run another ACP-compatible coding agent
- keep the parent loop simple while delegating file edits or command execution to a specialized external agent

If you only need another local Mister Morph loop, use [Subagents](/guide/subagents) and `spawn` instead.

## What Is Supported

Current support includes:

- `authenticate` when advertised by the ACP agent
- `session/new`
- `session/set_config_option` for option ids advertised by `session/new`
- `session/prompt`
- `session/request_permission` (including hyphenated kinds such as `allow-once` used by Cursor ACP)
- `fs/read_text_file`
- `fs/write_text_file`
- minimal `terminal/*`
- conservative defaults for Cursor blocking extension methods (`cursor/ask_question` skipped; `cursor/create_plan` auto-accepted without interactive review) so the subprocess does not hang

Not supported yet:

- MCP passthrough
- session reuse
- HTTP / SSE transport

## Config

You need two pieces of config:

1. enable the explicit tool entry
2. define at least one ACP profile

```yaml
tools:
  acp_spawn:
    enabled: true

acp:
  agents:
    - name: "codex"
      command: "npx"
      args: ["-y", "@archkk/acp-codex"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        mode: "auto"
        reasoning_effort: "low"
```

Notes:

- `tools.acp_spawn.enabled` controls only the `acp_spawn` entry.
- ACP profiles always run as local `stdio` child processes.
- `session_options` is first passed through `session/new._meta`.
- If the ACP agent advertises config option ids, matching keys are also sent through `session/set_config_option`.

## Prompt Pattern

Tell the parent agent to use `acp_spawn` explicitly.

Example:

```text
Only call acp_spawn. Use the codex agent. Read ./README.md and summarize it in exactly 5 Chinese sentences. Do not call spawn. Do not read the file yourself.
```

`acp_spawn` accepts:

- `agent`
- `task`
- `cwd`
- `output_schema`
- `observe_profile`

The result comes back in the same `SubtaskResult` envelope used by other isolated task paths.

## Runtime Behavior

One `acp_spawn` call does this:

1. start the configured wrapper process
2. `initialize`
3. `authenticate` if needed
4. `session/new`
5. `session/set_config_option` for advertised options
6. `session/prompt`
7. serve ACP file, permission, and terminal callbacks
8. collect the final assistant text

## Security Notes

ACP callback permissions are not the whole boundary.

The real enforcement happens in the implemented client methods:

- allowed file roots
- allowed terminal working directories
- local write and process rules

Also, the ACP command itself is still a local child process. ACP callback limits do not automatically sandbox arbitrary direct behavior inside that process.

## Codex

Codex should be configured as an external ACP adapter.

Common choices:

- `npx -y @archkk/acp-codex`
- `mistermorph-acp-codex` after `npm i -g @archkk/acp-codex`
- `codex-acp`
- `npx -y @zed-industries/codex-acp`

Checks:

1. `codex` itself should already work.
2. `mistermorph tools` should list `acp_spawn`.
3. the ACP profile should point to your Codex ACP adapter command.

Optional live check:

```bash
MISTERMORPH_ACP_CODEX_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CodexACPIntegration -v
```

The test defaults to `codex-acp`. If you want to verify the published package instead, set `MISTERMORPH_ACP_CODEX_COMMAND=npx` and `MISTERMORPH_ACP_CODEX_ARGS="-y @archkk/acp-codex"`.

## Claude

Mistermorph no longer ships a Claude wrapper inside this repository.

Use any external Claude ACP adapter instead. Example:

```yaml
acp:
  agents:
    - name: "claude"
      command: "npx"
      args: ["-y", "@archkk/acp-claude"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        permission_mode: "dontAsk"
        allowed_tools: ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
```

Published choices:

- `npx -y @archkk/acp-claude`
- `mistermorph-acp-claude` after `npm i -g @archkk/acp-claude`

Optional live check:

```bash
MISTERMORPH_ACP_CLAUDE_INTEGRATION=1 \
MISTERMORPH_ACP_CLAUDE_COMMAND=npx \
MISTERMORPH_ACP_CLAUDE_ARGS="-y @archkk/acp-claude" \
go test ./internal/acpclient -run TestRunPrompt_ClaudeACPIntegration -v
```

## Cursor CLI (`agent acp`)

Cursor CLI already exposes ACP directly over stdio, so Mistermorph no longer keeps a proxy in this repository.

Install the Cursor CLI, ensure `agent` is on `PATH`, and authenticate (`agent login`, or pass keys/flags as documented in [Cursor ACP](https://cursor.com/docs/cli/acp)).

Example profile:

```yaml
acp:
  agents:
    - name: "cursor"
      command: "agent"
      args: ["acp"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
```

If you need flags such as an API key, place them before the final `acp`, for example `args: ["--api-key", "${CURSOR_API_KEY}", "acp"]`.

Optional live check (requires Cursor CLI installed and authenticated):

```bash
MISTERMORPH_ACP_CURSOR_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CursorACPIntegration -v
```

See also:

- [Subagents](/guide/subagents)
- [Built-in Tools](/guide/built-in-tools)
- [Config Fields](/guide/config-reference)
