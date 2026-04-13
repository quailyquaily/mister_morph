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
      enable: true
      type: "stdio"
      command: "codex-acp"
      args: []
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

Also, the wrapper itself is still a local child process. ACP callback limits do not automatically sandbox arbitrary direct behavior inside that wrapper.

## Codex Paths

There are now two Codex paths.

### External Adapter

You can still use an ACP adapter such as `codex-acp`.

Checks:

1. `codex` itself should already work.
2. `mistermorph tools` should list `acp_spawn`.
3. the ACP profile should point to `codex-acp`.

### Native Wrapper in This Repository

The repository also includes a Codex wrapper owned by Mister Morph:

```yaml
acp:
  agents:
    - name: "codex"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/codex/src/index.mjs"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        approval_policy: "never"
```

Current scope of the native wrapper:

- backend is `codex app-server`
- no third-party ACP adapter is required
- no interactive approval flow yet
- default `approval_policy` is `never`

The existing opt-in live integration test can target this wrapper too:

```bash
MISTERMORPH_ACP_CODEX_INTEGRATION=1 \
MISTERMORPH_ACP_CODEX_COMMAND=node \
MISTERMORPH_ACP_CODEX_ARGS="./wrappers/acp/codex/src/index.mjs" \
go test ./internal/acpclient -run TestRunPrompt_CodexACPIntegration -v
```

## Claude Native Wrapper

The repository also includes a native Claude wrapper:

```yaml
acp:
  agents:
    - name: "claude"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/claude/src/index.mjs"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        permission_mode: "dontAsk"
        allowed_tools: ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
```

Current scope:

- backend is `claude -p --output-format stream-json`
- no third-party ACP adapter is required
- no interactive approval flow yet
- Claude internal tools are not bridged back into ACP file or terminal callbacks

Notes:

- `bare: true` is optional, not the default
- if you rely on Claude.ai login, keep `bare: false` because bare mode skips OAuth and keychain reads

The repository also includes an opt-in live integration test:

```bash
MISTERMORPH_ACP_CLAUDE_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_ClaudeNativeWrapperIntegration -v
```

## Cursor CLI (`agent acp`)

The Cursor CLI runs an ACP server via `agent acp` over stdio. Unlike the Codex/Claude bridges, this repository ships a **transparent stdio proxy** that forwards JSON-RPC lines to the Cursor CLI.

Install the Cursor CLI, ensure `agent` is on `PATH`, and authenticate (`agent login`, or pass keys/flags as documented in [Cursor ACP](https://cursor.com/docs/cli/acp)).

Example profile:

```yaml
acp:
  agents:
    - name: "cursor"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/cursor/src/index.mjs"]
      env:
        MISTERMORPH_CURSOR_ARGS: "--api-key ${CURSOR_API_KEY}"
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
```

Notes:

- `MISTERMORPH_CURSOR_COMMAND` overrides the `agent` binary path
- `MISTERMORPH_CURSOR_ARGS` are extra argv tokens inserted before the final `acp` subcommand
- Team MCP servers from the Cursor dashboard are not supported in ACP mode (per Cursor docs)

Optional live check (requires Cursor CLI installed and authenticated):

```bash
MISTERMORPH_ACP_CURSOR_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CursorACPProxyIntegration -v
```

See also:

- [Subagents](/guide/subagents)
- [Built-in Tools](/guide/built-in-tools)
- [Config Fields](/guide/config-reference)
