# Mister Morph

Unified Agent CLI + reusable Go agent core.

## Table of contents

- [Why Mister Morph](#why-mistermorph)
- [Quickstart](#quickstart)
- [Daemon mode](#daemon-mode)
- [Telegram bot mode](#telegram-bot-mode)
- [Embedding](#embedding-to-other-projects)
- [Built-in Tools](#built-in-tools)
- [Skills](#skills)
- [Security](#security)
- [Debug](#debug)
- [Configuration](#configuration)

## Why Mister Morph

What makes this project worth looking at:

- Reusable Go core: run the agent as a CLI, or embed it as a library/subprocess in other apps.
- Serious secure defaults: secrets stay out of prompts/tool params/logs via profile-based auth and redaction (see [docs/security.md](docs/security.md)).
- Practical Skills system: discover + inject `SKILL.md`, with smart selection and optional auth profile declarations for safe API use (see [docs/skills.md](docs/skills.md)).

## Quickstart

### Build

```bash
go build -o ./bin/mistermorph ./cmd/mistermorph
```

### Install the Agent

```bash
./bin/mistermorph install
# or
./bin/mistermorph install <dir>
```

the `install` command install required files and builtin skills and places them under `~/.morph/skills/` (or specified dir, if provided by `<dir>`).

### Setup an API key

Open the config file `~/.morph/config.yaml` and set your LLM provider API key, e.g. for OpenAI:

```yaml
llm:
  provider: "openai"
  endpoint: "https://api.openai.com/v1"
  model: "gpt-5.2"
  api_key: "YOUR_OPENAI_API_KEY_HERE"
```

### Run 

```bash
./bin/mistermorph run --task "Summarize this repo structure" --provider openai --model gpt-5 --api-key "$API_KEY" --endpoint "https://api.openai.com/v1"
```

### Human-in-the-loop

Run with `--interactive`, then press Ctrl-C during the loop to pause and type extra context (end with an empty line).

```bash
./bin/mistermorph run --interactive --task "..." --provider openai --model gpt-5.2 --api-key "$API_KEY" --endpoint "https://api.openai.com/v1"
```

## Daemon mode

Run a local HTTP daemon that accepts tasks sequentially (one-by-one), so you don’t need to restart the process for each task.

Start the daemon:

```bash
export MISTER_MORPH_SERVER_AUTH_TOKEN="change-me"
./bin/mistermorph serve --server-port 8787 --log-level info
```

Submit a task:

```bash
./bin/mistermorph submit --server-url http://127.0.0.1:8787 --auth-token "$MISTER_MORPH_SERVER_AUTH_TOKEN" --wait \
  --task "Summarize this repo and write to ./summary.md"
```

## Telegram bot mode

Run a Telegram bot (long polling) so you can chat with the agent from Telegram:

```bash
export MISTER_MORPH_TELEGRAM_BOT_TOKEN="123456:ABC..."
./bin/mistermorph telegram --telegram-allowed-chat-id 123456789 --log-level info
```

Notes:
- Use `/ask <task>` in groups.
- In groups, the bot also responds when you reply to it, or mention `@BotUsername`.
- Bot replies are sent with Telegram MarkdownV2.
- You can send a file; it will be downloaded under `file_cache_dir/telegram/` and the agent can process it. The agent can also send cached files back via `telegram_send_file`, and send a voice message via `telegram_send_voice` (requires a local TTS engine (e.g. `espeak-ng`) + `ffmpeg`/`opusenc`).
- The last loaded skill(s) stay “sticky” per chat (so follow-up messages won’t forget SKILL.md); `/reset` clears this.
- If you configure `telegram.aliases`, the default `telegram.group_trigger_mode=smart` only triggers on aliases when the message looks like direct addressing. Alias hits are LLM-validated in smart mode.
- You can tune smart addressing with `telegram.smart_addressing_max_chars` and `telegram.smart_addressing_confidence` for the trigger detection.
- Use `/id` to print the current chat id (useful for allowlisting group ids).
- Use `/reset` in chat to clear conversation history.
- If you omit `--telegram-allowed-chat-id`, all chats can talk to the bot (not recommended).
- By default it runs multiple chats concurrently, but processes each chat serially (config: `telegram.max_concurrency`).

## Embedding to other projects

Two common integration options:

- As a Go library: see `demo/embed-go/`.
- As a subprocess CLI: see `demo/embed-cli/`.

## Built-in Tools

Core tools available to the agent:

- `echo`: echo a value (debugging/formatting).
- `read_file`: read local text files.
- `write_file`: write local text files under `file_cache_dir`.
- `bash`: run a shell command (disabled by default).
- `url_fetch`: HTTP fetch with optional auth profiles.
- `web_search`: web search (DuckDuckGo HTML).
- `plan_create`: generate a structured plan.

Tools only available in Telegram mode:

- `telegram_send_file`: send a file in Telegram.
- `telegram_send_voice`: send a voice message in Telegram.
- `telegram_react`: add an emoji reaction in Telegram.

## Skills

`mistermorph` can discover skills under `~/.morph/skills`, `~/.claude/skills`, and `~/.codex/skills` (recursively), and inject selected `SKILL.md` content into the system prompt.

By default, `run` uses `skills.mode=smart` so the agent can decide which skills to load (no need to mention `$SkillName`).

Docs: [`docs/skills.md`](docs/skills.md).

```bash
# list available skills
./bin/mistermorph skills list
# Use a specific skill in the run command
./bin/mistermorph run --task "..." --skills-mode explicit --skill skill-name
# install remote skills 
./bin/mistermorph skills install <remote-skill-url> 
```

### Security Meachanisms for Skills

1. Install audit: When installing remote skills, Mister Morph will preview the skill content and do a basic security audit (e.g., look for dangerous commands in scripts) before asking for user confirmation.
2. Auth profiles2. Auth profiles: Skills can declare required auth profiles in `auth_profiles` field. The agent will only use skills whose auth profiles are configured on the host, preventing accidental secret leaks (see `assets/skills/moltbook` as the example, see `secrets` and `auth_profiles` sections in the config file).

## Security

Recommended systemd hardening and secret handling: `docs/security.md`.

## Debug

### Logging

There is an argument `--log-level` set for logging level and format:

```bash
./bin/mistermorph run --log-level debug --task "..."
```

### Dump internal debug data

There are 2 arguments `--inspect-prompt`/`--inspect-request` for dumping internal state for debugging:

```bash
./bin/mistermorph run --inspect-prompt --inspect-request --task "..."
```

These arguments will dump the final system/user/tool prompts and the full LLM request/response JSON as plain text files to `./dump` directory. 

## Configuration

`mistermorph` uses Viper, so you can configure it via flags, env vars, or a config file.

- Config file: `--config /path/to/config.yaml` (supports `.yaml/.yml/.json/.toml/.ini`)
- Env var prefix: `MISTER_MORPH_`
- Nested keys: replace `.` and `-` with `_` (e.g. `tools.bash.enabled` → `MISTER_MORPH_TOOLS_BASH_ENABLED=true`)


### CLI flags

**Global (all commands)**
- `--config`
- `--log-level`
- `--log-format`
- `--log-add-source`
- `--log-include-thoughts`
- `--log-include-tool-params`
- `--log-include-skill-contents`
- `--log-max-thought-chars`
- `--log-max-json-bytes`
- `--log-max-string-value-chars`
- `--log-max-skill-content-chars`
- `--log-redact-key` (repeatable)

**run**
- `--task`
- `--provider`
- `--endpoint`
- `--model`
- `--api-key`
- `--llm-request-timeout`
- `--interactive`
- `--skills-dir` (repeatable)
- `--skill` (repeatable)
- `--skills-auto`
- `--skills-mode` (`off|explicit|smart`)
- `--skills-max-load`
- `--skills-preview-bytes`
- `--skills-catalog-limit`
- `--skills-select-timeout`
- `--max-steps`
- `--parse-retries`
- `--max-token-budget`
- `--timeout`
- `--inspect-prompt`
- `--inspect-request`

**serve**
- `--server-bind`
- `--server-port`
- `--server-auth-token`
- `--server-max-queue`

**submit**
- `--task`
- `--server-url`
- `--auth-token`
- `--model`
- `--submit-timeout`
- `--wait`
- `--poll-interval`

**telegram**
- `--telegram-bot-token`
- `--telegram-allowed-chat-id` (repeatable)
- `--telegram-alias` (repeatable)
- `--telegram-group-trigger-mode` (`strict|smart`)
- `--telegram-smart-addressing-max-chars`
- `--telegram-smart-addressing-confidence`
- `--telegram-poll-timeout`
- `--telegram-task-timeout`
- `--telegram-max-concurrency`
- `--telegram-history-max-messages`
- `--file-cache-dir`

**skills**
- `skills list --skills-dir` (repeatable)
- `skills show --skills-dir` (repeatable)
- `skills install --dest --dry-run --clean --skip-existing --timeout --max-bytes --yes`

**install**
- `install [dir]`

### Environment variables

Common env vars (these map to config keys):

- `MISTER_MORPH_CONFIG`
- `MISTER_MORPH_LLM_PROVIDER`
- `MISTER_MORPH_LLM_ENDPOINT`
- `MISTER_MORPH_LLM_MODEL`
- `MISTER_MORPH_LLM_API_KEY`
- `MISTER_MORPH_LLM_REQUEST_TIMEOUT`
- `MISTER_MORPH_LOGGING_LEVEL`
- `MISTER_MORPH_LOGGING_FORMAT`
- `MISTER_MORPH_SERVER_AUTH_TOKEN`
- `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `MISTER_MORPH_GUARD_APPROVALS_SQLITE_DSN`
- `MISTER_MORPH_FILE_CACHE_DIR`

Provider-specific settings use the same mapping, for example:
- `llm.azure.api_key` → `MISTER_MORPH_LLM_AZURE_API_KEY`
- `llm.bedrock.model_arn` → `MISTER_MORPH_LLM_BEDROCK_MODEL_ARN`

Tool toggles and limits also map to env vars, for example:

- `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `MISTER_MORPH_TOOLS_BASH_CONFIRM`
- `MISTER_MORPH_TOOLS_URL_FETCH_ENABLED`
- `MISTER_MORPH_TOOLS_URL_FETCH_MAX_BYTES`

Secret values referenced by `auth_profiles.*.credential.secret_ref` are regular env vars too (example: `JSONBILL_API_KEY`).

Key meanings (see `assets/config/config.example.yaml` for the canonical list):
- Core: `llm.provider` selects the backend. Most providers use `llm.endpoint`/`llm.api_key`/`llm.model`. Azure and Bedrock have dedicated config blocks (`llm.azure.*`, `llm.bedrock.*`). `llm.tools_emulation_mode` controls tool-call emulation for models without native tool calling (`off|fallback|force`).
- Logging: `logging.level` (`info` shows progress; `debug` adds thoughts), `logging.format` (`text|json`), plus opt-in fields `logging.include_thoughts` and `logging.include_tool_params` (redacted).
- Loop: `max_steps` limits tool-call rounds; `parse_retries` retries invalid JSON; `max_token_budget` is a cumulative token cap (0 disables); `timeout` is the overall run timeout.
- Skills: `skills.mode` controls whether skills are used (`smart` lets the agent decide); `file_state_dir` + `skills.dir_name` define the default skills root (also scans `~/.claude/skills` and `~/.codex/skills`); `skills.load` always loads specific skills; `skills.auto` additionally loads `$SkillName` references; smart mode tuning via `skills.max_load/preview_bytes/catalog_limit/select_timeout/selector_model`.
- Tools: all tool toggles live under `tools.*` (e.g. `tools.bash.enabled`, `tools.url_fetch.enabled`) with per-tool limits and timeouts.
