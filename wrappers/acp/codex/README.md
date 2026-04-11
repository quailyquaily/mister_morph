# MisterMorph ACP Codex Wrapper

This wrapper lets MisterMorph talk ACP to Codex without depending on a third-party ACP adapter.

Current shape:

- transport: `stdio`
- ACP methods:
  - `initialize`
  - `authenticate` (no-op)
  - `session/new`
  - `session/set_config_option`
  - `session/prompt`
  - `session/cancel`
- backend: `codex app-server`

Current limits:

- no session persistence
- no MCP passthrough
- no interactive approval flow
- default `approval_policy` is `never`

Run it directly:

```bash
node ./wrappers/acp/codex/src/index.mjs
```

Example ACP profile:

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

Optional environment variables:

- `MISTERMORPH_CODEX_COMMAND`
  - override backend executable, default `codex`
- `MISTERMORPH_CODEX_ARGS`
  - extra backend args, whitespace-split, appended after `app-server`
- `MISTERMORPH_CODEX_AUTO_APPROVE`
  - when set to `1`, auto-accept Codex command/file approval requests for the session
