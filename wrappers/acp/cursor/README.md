# MisterMorph ACP Cursor proxy

This wrapper is a **transparent stdio proxy** between MisterMorph (ACP client) and the Cursor CLI’s ACP server (`agent acp`). It does not translate protocols; it forwards newline-delimited JSON-RPC lines.

## Requirements

- Cursor CLI `agent` on `PATH`, or set `MISTERMORPH_CURSOR_COMMAND` to the executable path.
- Authentication: run `agent login`, or pass API key / token via Cursor-supported flags or env (see [Cursor ACP documentation](https://cursor.com/cn/docs/cli/acp)).

## Usage

```bash
node ./wrappers/acp/cursor/src/index.mjs
```

Example ACP profile:

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

## Environment

- `MISTERMORPH_CURSOR_COMMAND` — backend executable (default: `agent`)
- `MISTERMORPH_CURSOR_ARGS` — extra arguments inserted **before** the final `acp` subcommand (space-separated)

## Notes

- Team MCP servers from the Cursor dashboard are not supported in ACP mode (per Cursor docs); project/user `.cursor/mcp.json` may apply when the CLI runs in your project directory.
- If `agent` is missing or not executable, the proxy writes a clear message to stderr and exits (via `child_process` `error` handling).

## Opt-in integration test

From the repository root (Go 1.25+), with `agent` and `node` on `PATH` and Cursor CLI authenticated:

```bash
MISTERMORPH_ACP_CURSOR_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CursorACPProxyIntegration -v
```
