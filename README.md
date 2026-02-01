# mister_morph

Unified Agent CLI + reusable Go agent core.

## Build

```bash
go build ./cmd/mister_morph
```

## Run (OpenAI-compatible)

```bash
./mister_morph run --task "Summarize this repo structure" --provider openai --model gpt-4o-mini --api-key "$OPENAI_API_KEY"
```

## Human-in-the-loop (interrupt + inject)

Run with `--interactive`, then press Ctrl-C during the loop to pause and type extra context (end with an empty line).

```bash
./mister_morph run --interactive --task "..." --provider openai --model gpt-4o-mini --api-key "$OPENAI_API_KEY"
```

## Skills (SKILL.md)

`mister_morph` can discover skills under `~/.codex/skills` and `~/.claude/skills` (recursively), and inject selected `SKILL.md` content into the system prompt.

By default, `run` uses `skills.mode=smart` so the agent can decide which skills to load (no need to mention `$SkillName`).

```bash
./mister_morph skills list
./mister_morph skills show skill-creator
./mister_morph run --task "Draft a new skill for X" --skills-mode smart
./mister_morph run --task "Use $skill-creator to draft a new skill" --skills-mode explicit --skills-auto
./mister_morph run --task "..." --skills-mode explicit --skill skill-creator
```

## Tools: `web_search`, `url_fetch`, `write_file`, `bash`

- `web_search` is enabled by default and returns a short list of search results.
- `url_fetch` is enabled by default and can send HTTP(S) requests (GET/POST/PUT/DELETE, optional headers/body; truncated output). For unsupported cases, use `bash` with `curl`.
- `write_file` is enabled by default and writes text content to a local file (restricted to `file_cache_dir/`).
- `bash` is disabled by default (dangerous). Enable via config `tools.bash.enabled: true` (recommended: `tools.bash.confirm: true`).

## Daemon Mode (Serve + Submit)

Run a local HTTP daemon that accepts tasks sequentially (one-by-one), so you don’t need to restart the process for each task.

Start the daemon:

```bash
export MISTER_MORPH_SERVER_AUTH_TOKEN="change-me"
./mister_morph serve --server-port 8787 --log-level info
```

Submit a task:

```bash
./mister_morph submit --server-url http://127.0.0.1:8787 --auth-token "$MISTER_MORPH_SERVER_AUTH_TOKEN" --wait \
  --task "Summarize this repo and write to ./summary.md"
```

## Telegram Bot Mode

Run a Telegram bot (long polling) so you can chat with the agent from Telegram:

```bash
export MISTER_MORPH_TELEGRAM_BOT_TOKEN="123456:ABC..."
./mister_morph telegram --telegram-allowed-chat-id 123456789 --log-level info
```

Notes:
- Use `/ask <task>` in groups.
- In groups, the bot also responds when you reply to it, or mention `@BotUsername` (if it receives the message).
- You can send a file (document/photo); it will be downloaded under `file_cache_dir/telegram/` and the agent can process it (e.g. via the `bash` tool). The agent can also send cached files back via `telegram_send_file`.
- In Telegram mode, the last loaded skill(s) stay “sticky” per chat (so follow-up messages won’t forget SKILL.md); `/reset` clears this.
- If you configure `telegram.aliases`, the default `telegram.group_trigger_mode=smart` only triggers on aliases when the message looks like direct addressing (alias near the start + request-like text). Use `contains` for the old substring behavior.
- If you want smarter disambiguation for alias mentions, enable `telegram.addressing_llm.enabled` (and optionally set `telegram.addressing_llm.mode=always`) to let an LLM classify alias hits.
- Use `/id` to print the current chat id (useful for allowlisting group ids).
- Use `/reset` in chat to clear conversation history.
- If you omit `--telegram-allowed-chat-id`, all chats can talk to the bot (not recommended).
- By default it runs multiple chats concurrently, but processes each chat serially (config: `telegram.max_concurrency`).
- If `@` works in one group but not another, check: only one bot process is running (one `getUpdates` consumer), the supergroup id is allowlisted, and BotFather privacy mode settings.

## Config (Viper)

- Flags: `--provider`, `--model`, `--api-key`, `--endpoint`, `--llm-request-timeout`, `--plan-mode`, `--max-steps`, `--parse-retries`, `--timeout`, `--trace`, `--log-level`, `--log-format`, `--server-port`, `--server-auth-token`
- Env vars: `MISTER_MORPH_LLM_PROVIDER`, `MISTER_MORPH_LLM_MODEL`, `MISTER_MORPH_LLM_API_KEY`, `MISTER_MORPH_LLM_ENDPOINT` (nested keys also work, e.g. `MISTER_MORPH_TOOLS_BASH_ENABLED=true`)
- Optional config file: `--config path/to/config` (supports `.yaml/.yml/.json/.toml/.ini`)

Key meanings (see `mister_morph/config.example.yaml` for the canonical list):
- Core: `llm.provider`/`llm.endpoint`/`llm.model`/`llm.api_key` select the LLM backend and credentials.
- Logging: `logging.level` (`info` shows progress; `debug` adds thoughts), `logging.format` (`text|json`), plus opt-in fields `logging.include_thoughts` and `logging.include_tool_params` (redacted).
- Loop: `plan.mode` enables planning for complex tasks; `max_steps` limits tool-call rounds; `parse_retries` retries invalid JSON; `max_token_budget` is a cumulative token cap (0 disables); `timeout` is the overall run timeout; `trace` prints debug info to stderr.
- Skills: `skills.mode` controls whether skills are used (`smart` lets the agent decide); `skills.dirs` are scan roots; `skills.load` always loads specific skills; `skills.auto` additionally loads `$SkillName` references; smart mode tuning via `skills.max_load/preview_bytes/catalog_limit/select_timeout/selector_model`.
- Tools: all tool toggles live under `tools.*` (e.g. `tools.bash.enabled`, `tools.url_fetch.enabled`) with per-tool limits and timeouts.

Example config: `mister_morph/config.example.yaml:1`
