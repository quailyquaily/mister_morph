# Mister Morph

Unified Agent CLI + reusable Go agent core.

## Table of contents

- [Why Mister Morph](#why-mistermorph)
- [Quickstart](#quickstart)
- [Embedding](#embedding)
- [Skills (SKILL.md)](#skills-skillmd)
- [Daemon mode (serve + submit)](#daemon-mode-serve--submit)
- [Telegram bot mode](#telegram-bot-mode)
- [Configuration](#configuration)
- [Security](#security)

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

### Run 

```bash
./bin/mistermorph run --task "Summarize this repo structure" --provider openai --model gpt-5 --api-key "$OPENAI_API_KEY" --endpoint "https://api.openai.com/v1"
```

### Human-in-the-loop (interrupt + inject)

Run with `--interactive`, then press Ctrl-C during the loop to pause and type extra context (end with an empty line).

```bash
./bin/mistermorph run --interactive --task "..." --provider openai --model gpt-4o-mini --api-key "$OPENAI_API_KEY" --endpoint "https://api.openai.com/v1"
```

## Embedding to other projects

Two common integration options:

- As a Go library: see `demo/embed-go/`.
- As a subprocess CLI: see `demo/embed-cli/`.

## Skills

`mistermorph` can discover skills under `~/.morph/skills`, `~/.claude/skills`, and `~/.codex/skills` (recursively), and inject selected `SKILL.md` content into the system prompt.

By default, `run` uses `skills.mode=smart` so the agent can decide which skills to load (no need to mention `$SkillName`).

Docs: `docs/skills.md`

```bash
./bin/mistermorph skills list
./bin/mistermorph skills show skill-creator
./bin/mistermorph run --task "Draft a new skill for X" --skills-mode smart
./bin/mistermorph run --task "Use $skill-creator to draft a new skill" --skills-mode explicit --skills-auto
./bin/mistermorph run --task "..." --skills-mode explicit --skill skill-creator
```

## Daemon mode (serve + submit)

Run a local HTTP daemon that accepts tasks sequentially (one-by-one), so you don’t need to restart the process for each task.

Security hardening (recommended for daemon mode): `docs/security.md`

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
- In groups, the bot also responds when you reply to it, or mention `@BotUsername` (if it receives the message).
- Bot replies are sent with Telegram Markdown (MarkdownV2; with fallback to plain text if Telegram rejects formatting).
- You can send a file (document/photo); it will be downloaded under `file_cache_dir/telegram/` and the agent can process it (e.g. via the `bash` tool). The agent can also send cached files back via `telegram_send_file`, and send a cached voice message via `telegram_send_voice` (recommended: `.ogg` with Opus audio).
- In Telegram mode, the last loaded skill(s) stay “sticky” per chat (so follow-up messages won’t forget SKILL.md); `/reset` clears this.
- If you configure `telegram.aliases`, the default `telegram.group_trigger_mode=smart` only triggers on aliases when the message looks like direct addressing (alias near the start + request-like text). Use `contains` for the old substring behavior.
- If you want smarter disambiguation for alias mentions, enable `telegram.addressing_llm.enabled` (and optionally set `telegram.addressing_llm.mode=always`) to let an LLM classify alias hits.
- Use `/id` to print the current chat id (useful for allowlisting group ids).
- Use `/reset` in chat to clear conversation history.
- If you omit `--telegram-allowed-chat-id`, all chats can talk to the bot (not recommended).
- By default it runs multiple chats concurrently, but processes each chat serially (config: `telegram.max_concurrency`).
- If `@` works in one group but not another, check: only one bot process is running (one `getUpdates` consumer), the supergroup id is allowlisted, and BotFather privacy mode settings.

If you enable the resident scheduler (`scheduler.enabled=true`), the agent can create persistent cron/interval jobs via the internal tools: `schedule_job`, `list_jobs`, `search_jobs`, and `unschedule_job`. For one-shot reminders, set `run_once=true`. To deliver scheduled run results back into Telegram, set `notify_telegram_chat_id` when scheduling.

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
- `--trace`

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
- `--plan-mode` (`off|auto|always`)
- `--timeout`

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
- `--telegram-group-trigger-mode` (`strict|smart|contains`)
- `--telegram-alias-prefix-max-chars`
- `--telegram-addressing-llm-enabled`
- `--telegram-addressing-llm-mode` (`borderline|always`)
- `--telegram-addressing-llm-model`
- `--telegram-addressing-llm-timeout`
- `--telegram-addressing-llm-min-confidence`
- `--telegram-poll-timeout`
- `--telegram-task-timeout`
- `--telegram-max-concurrency`
- `--telegram-history-max-messages`
- `--file-cache-dir`

**skills**
- `skills list --skills-dir` (repeatable)
- `skills show --skills-dir` (repeatable)
- `skills install-builtin --dest --dry-run --clean --skip-existing --timeout --max-bytes --yes`

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
- `MISTER_MORPH_DB_DSN`
- `MISTER_MORPH_FILE_CACHE_DIR`

Tool toggles and limits also map to env vars, for example:

- `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `MISTER_MORPH_TOOLS_BASH_CONFIRM`
- `MISTER_MORPH_TOOLS_URL_FETCH_ENABLED`
- `MISTER_MORPH_TOOLS_URL_FETCH_MAX_BYTES`

Secret values referenced by `auth_profiles.*.credential.secret_ref` are regular env vars too (example: `JSONBILL_API_KEY`).

Key meanings (see `config.example.yaml` for the canonical list):
- Core: `llm.provider`/`llm.endpoint`/`llm.model`/`llm.api_key` select the LLM backend and credentials.
- Logging: `logging.level` (`info` shows progress; `debug` adds thoughts), `logging.format` (`text|json`), plus opt-in fields `logging.include_thoughts` and `logging.include_tool_params` (redacted).
- Loop: `plan.mode` enables planning for complex tasks; `max_steps` limits tool-call rounds; `parse_retries` retries invalid JSON; `max_token_budget` is a cumulative token cap (0 disables); `timeout` is the overall run timeout; `trace` prints debug info to stderr.
- Skills: `skills.mode` controls whether skills are used (`smart` lets the agent decide); `skills.dirs` are scan roots; `skills.load` always loads specific skills; `skills.auto` additionally loads `$SkillName` references; smart mode tuning via `skills.max_load/preview_bytes/catalog_limit/select_timeout/selector_model`.
- Scheduler: `scheduler.enabled` starts the resident scheduler; `scheduler.tick` controls how often it scans for due jobs; `scheduler.concurrency` controls the worker pool size.
- Tools: all tool toggles live under `tools.*` (e.g. `tools.bash.enabled`, `tools.url_fetch.enabled`) with per-tool limits and timeouts.

## Security

Recommended systemd hardening and secret handling: `docs/security.md`.
