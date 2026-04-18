---
title: Environment Variables
description: Complete env var model, mapping rule, and compatibility variables.
---

# Environment Variables

## Precedence

Effective precedence is:

1. CLI flags
2. `MISTER_MORPH_*` env vars
3. `config.yaml`
4. code defaults

## Complete Support Rule

All config keys are env-overridable through one rule:

- Prefix with `MISTER_MORPH_`
- Convert to upper case
- Replace `.` and `-` with `_`

Examples:

- `llm.api_key` -> `MISTER_MORPH_LLM_API_KEY`
- `tools.bash.enabled` -> `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `tools.powershell.enabled` -> `MISTER_MORPH_TOOLS_POWERSHELL_ENABLED`
- `tools.spawn.enabled` -> `MISTER_MORPH_TOOLS_SPAWN_ENABLED`
- `mcp.servers` -> `MISTER_MORPH_MCP_SERVERS`

So all fields listed in [Config Fields](/guide/config-reference) are supported as env vars.

## High-Frequency Variables

- `MISTER_MORPH_CONFIG`
- `MISTER_MORPH_LLM_PROVIDER`
- `MISTER_MORPH_LLM_ENDPOINT`
- `MISTER_MORPH_LLM_MODEL`
- `MISTER_MORPH_LLM_API_KEY`
- `MISTER_MORPH_SERVER_AUTH_TOKEN`
- `MISTER_MORPH_CONSOLE_PASSWORD`
- `MISTER_MORPH_CONSOLE_PASSWORD_HASH`
- `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `MISTER_MORPH_SLACK_BOT_TOKEN`
- `MISTER_MORPH_SLACK_APP_TOKEN`
- `MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN`
- `MISTER_MORPH_LINE_CHANNEL_SECRET`
- `MISTER_MORPH_LARK_APP_ID`
- `MISTER_MORPH_LARK_APP_SECRET`
- `MISTER_MORPH_FILE_STATE_DIR`
- `MISTER_MORPH_FILE_CACHE_DIR`

## `${ENV_VAR}` Expansion Inside Config

All string values in config support `${ENV_VAR}` expansion.

```yaml
llm:
  api_key: "${OPENAI_API_KEY}"
mcp:
  servers:
    - name: remote
      headers:
        Authorization: "Bearer ${MCP_REMOTE_TOKEN}"
```

Notes:

- only `${NAME}` form is expanded
- bare `$NAME` is not expanded
- missing vars become empty string with warning

## Compatibility / Special Env Vars

- `TELEGRAM_BOT_TOKEN`
  - fallback for `mistermorph telegram send`
  - preferred var is still `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `NO_COLOR` and `TERM=dumb`
  - affect CLI color output behavior only

## Practical Pattern

For secrets, keep config value as `${ENV_VAR}` and set the secret in runtime environment.
