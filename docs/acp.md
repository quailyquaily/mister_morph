# ACP

This page describes the user-facing ACP support now available in Mistermorph.

## What It Is

Mistermorph can delegate one isolated subtask to an external ACP agent.

Today that means:

- Mistermorph acts as an ACP client.
- The external agent runs as a local child process over `stdio`.
- The parent agent uses the explicit `acp_spawn` tool to start that subtask.

This is separate from `spawn`.

- `spawn` starts another local Mistermorph agent loop.
- `acp_spawn` starts an external ACP-compatible agent or adapter.

## Current Scope

The current implementation is intentionally narrow:

- client-only; Mistermorph is not an ACP server
- `stdio` transport only
- one synchronous session per call
- one prompt turn per call
- text prompts only
- `authenticate` when the agent advertises auth methods
- `session/set_config_option` when `session/new` advertises config option ids
- client callbacks for:
  - `session/request_permission`
  - `fs/read_text_file`
  - `fs/write_text_file`
  - `terminal/create`
  - `terminal/output`
  - `terminal/wait_for_exit`
  - `terminal/kill`
  - `terminal/release`

Not supported yet:

- MCP passthrough
- session reuse
- HTTP / SSE transport
- interactive approval UI

## Config

ACP support has two config surfaces:

1. Enable the explicit tool entry.
2. Define one or more ACP agent profiles.

Example:

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

Field notes:

- `tools.acp_spawn.enabled` controls only the explicit `acp_spawn` tool entry.
- `acp.agents[].name` is the profile name the parent agent uses.
- `cwd`, `read_roots`, and `write_roots` constrain ACP file and terminal callbacks.
- `session_options` is passed into `session/new._meta`.
- If the ACP agent advertises config option ids in `session/new`, matching keys from `session_options` are also sent through `session/set_config_option`.

## How to Use It

At runtime, the parent agent must explicitly choose `acp_spawn`.

Typical prompt:

```text
Only call acp_spawn. Use the codex agent. Read ./README.md and summarize it in exactly 5 Chinese sentences. Do not call spawn. Do not read the file yourself.
```

The `acp_spawn` parameters are:

- `agent`: ACP profile name
- `task`: task text for the external agent
- `cwd`: optional working-directory override
- `output_schema`: optional structured-output label
- `observe_profile`: optional local observation hint

The result comes back in the same `SubtaskResult` envelope used by other isolated task paths.

## Execution Model

One `acp_spawn` call does this:

1. load the ACP profile
2. start the wrapper process
3. `initialize`
4. `authenticate` if needed
5. `session/new`
6. `session/set_config_option` for advertised option ids
7. `session/prompt`
8. serve ACP callbacks during the turn
9. collect final text and stop reason
10. close the session/process

This means the parent agent does not need to know whether the child path was local `spawn` or ACP `acp_spawn`. Both return the same top-level envelope.

## Security and Limits

Two limits matter here.

First, ACP permission requests are not the real security boundary. The real boundary is what Mistermorph actually implements in its client callbacks:

- allowed file roots
- allowed terminal working directories
- local write and process rules

Second, the wrapper process itself is still a local child process.

That means:

- ACP callback limits apply to ACP method calls
- they do not automatically sandbox arbitrary direct behavior inside the wrapper itself

So ACP support should be treated as controlled delegation, not a hard sandbox.

## Codex Paths

There are now two Codex paths.

### External Adapter

You can still point ACP at an external adapter such as `codex-acp`.

Practical checks:

1. `codex` itself must already work on the machine.
2. `mistermorph tools` should show `acp_spawn`.
3. the ACP profile should point to `codex-acp`.

### Native Wrapper in This Repository

The repository now also includes a Codex wrapper owned by Mistermorph:

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

## Claude Paths

Claude now also has a native wrapper in this repository.

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

Current scope of the native Claude wrapper:

- backend is `claude -p --output-format stream-json`
- no third-party ACP adapter is required
- no interactive approval flow yet
- Claude internal tools are not bridged back into ACP file or terminal callbacks

Two practical notes:

- `bare: true` is optional, not the default
- if you rely on Claude.ai login, keep `bare: false` because bare mode skips OAuth and keychain reads

There is also an opt-in live integration test:

```bash
MISTERMORPH_ACP_CLAUDE_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_ClaudeNativeWrapperIntegration -v
```

## See Also

- [Tools](./tools.md)
- [Configuration](./configuration.md)
- [Feature Design](./feat/feat_20260410_acp_agent_support.md)
- [Implementation Progress](./feat/feat_20260410_acp_agent_support_impl.md)
