# MisterMorph ACP Claude Wrapper

This wrapper lets MisterMorph talk ACP to Claude Code without depending on a third-party ACP adapter.

Current shape:

- transport: `stdio`
- ACP methods:
  - `initialize`
  - `authenticate` (no-op)
  - `session/new`
  - `session/set_config_option`
  - `session/prompt`
  - `session/cancel`
- backend: `claude -p --output-format stream-json`

Current limits:

- no session persistence
- no MCP passthrough
- no interactive approval flow
- the wrapper does not bridge Claude tool calls back into ACP file or terminal callbacks

Run it directly:

```bash
node ./wrappers/acp/claude/src/index.mjs
```

Example ACP profile:

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

Notes:

- `bare: true` is optional, but it is not safe as a default when you rely on Claude.ai login.
- Claude Code bare mode skips OAuth and keychain reads, so Claude.ai login usually requires `bare: false`.

Optional environment variables:

- `MISTERMORPH_CLAUDE_COMMAND`
  - override backend executable, default `claude`
- `MISTERMORPH_CLAUDE_ARGS`
  - extra backend args, whitespace-split, inserted before print-mode flags
