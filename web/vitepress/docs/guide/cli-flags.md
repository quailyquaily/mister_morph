---
title: CLI Flags
description: Supported command-line flags for mistermorph commands.
---

# CLI Flags

This page lists the user-facing CLI flags from the current `mistermorph --help` and subcommand `--help` output.

Omitted from the main list:

- Cobra built-ins such as `completion` and `help`
- `version`, which currently has no command-specific flags

## Global Flags

These flags are inherited by most commands:

- `--config`: Config file path.
- `--log-add-source`: Include source `file:line` in logs.
- `--log-format`: Logging format, `text|json`.
- `--log-include-skill-contents`: Include loaded `SKILL.md` contents in logs.
- `--log-include-thoughts`: Include model thoughts in logs.
- `--log-include-tool-params`: Include tool params in logs.
- `--log-level`: Logging level, `debug|info|warn|error`.
- `--log-max-json-bytes`: Max bytes of JSON params to log.
- `--log-max-skill-content-chars`: Max `SKILL.md` characters to log.
- `--log-max-string-value-chars`: Max characters per logged string value.
- `--log-max-thought-chars`: Max characters of thought to log.
- `--log-redact-key`: Extra param keys to redact in logs. Repeatable.

## `benchmark`

This command accepts an optional `profile-name` positional argument. Without one, it benchmarks the default route plus every named LLM profile.

- `--json`: Output benchmark results as JSON.
- `--timeout`: Overall timeout for the selected benchmarks. `0` disables the timeout.

## `run`

- `--api-key`: API key.
- `--endpoint`: Base URL for provider.
- `--heartbeat`: Run a single heartbeat check and ignore `--task` and stdin.
- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.
- `--interactive`: Allow Ctrl-C pause and interactive context injection.
- `--llm-request-timeout`: Per-LLM HTTP request timeout.
- `--max-steps`: Max tool-call steps.
- `--max-token-budget`: Max cumulative token budget.
- `--model`: Model name.
- `--parse-retries`: Max JSON parse retries.
- `--provider`: Provider name.
- `--skill`: Skill name or id to load. Repeatable.
- `--skills-dir`: Skills root directory. Repeatable.
- `--skills-enabled`: Enable configured skills loading.
- `--spawn-enabled`: Enable the spawn tool for sub-agents.
- `--task`: Task to run. If empty, reads from stdin.
- `--timeout`: Overall timeout.
- `--tool-repeat-limit`: Force final output when the same successful tool call repeats too many times.

## `console serve`

- `--allow-empty-password`: Allow console to run without `console.password` or `console.password_hash`.
- `--console-base-path`: Console base path.
- `--console-listen`: Console server listen address.
- `--console-session-ttl`: Session TTL for console bearer token.
- `--console-static-dir`: SPA static directory.
- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.

## `telegram`

- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.
- `--telegram-addressing-confidence-threshold`: Minimum addressing confidence to accept.
- `--telegram-addressing-interject-threshold`: Minimum interject score to accept.
- `--telegram-allowed-chat-id`: Allowed chat ids. Repeatable.
- `--telegram-bot-token`: Telegram bot token.
- `--telegram-group-trigger-mode`: Group trigger mode, `strict|smart|talkative`.
- `--telegram-max-concurrency`: Max number of chats processed concurrently.
- `--telegram-poll-timeout`: Long polling timeout for `getUpdates`.
- `--telegram-task-timeout`: Per-message agent timeout.

## `slack`

- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.
- `--slack-addressing-confidence-threshold`: Minimum addressing confidence to accept.
- `--slack-addressing-interject-threshold`: Minimum interject score to accept.
- `--slack-allowed-channel-id`: Allowed Slack channel ids. Repeatable.
- `--slack-allowed-team-id`: Allowed Slack team ids. Repeatable.
- `--slack-app-token`: Slack app token for Socket Mode.
- `--slack-bot-token`: Slack bot token.
- `--slack-group-trigger-mode`: Group trigger mode, `strict|smart|talkative`.
- `--slack-max-concurrency`: Max number of Slack conversations processed concurrently.
- `--slack-task-timeout`: Per-message agent timeout.

## `line`

- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.
- `--line-addressing-confidence-threshold`: Minimum addressing confidence to accept.
- `--line-addressing-interject-threshold`: Minimum interject score to accept.
- `--line-allowed-group-id`: Allowed LINE group ids. Repeatable.
- `--line-base-url`: LINE API base URL.
- `--line-channel-access-token`: LINE channel access token.
- `--line-channel-secret`: LINE channel secret for webhook signature verification.
- `--line-group-trigger-mode`: Group trigger mode, `strict|smart|talkative`.
- `--line-max-concurrency`: Max number of LINE conversations processed concurrently.
- `--line-task-timeout`: Per-message agent timeout.
- `--line-webhook-listen`: Listen address for the LINE webhook server.
- `--line-webhook-path`: HTTP path for the LINE webhook callback.

## `lark`

- `--inspect-prompt`: Dump prompt messages into `./dump`.
- `--inspect-request`: Dump LLM request/response payloads into `./dump`.
- `--lark-addressing-confidence-threshold`: Minimum addressing confidence to accept.
- `--lark-addressing-interject-threshold`: Minimum interject score to accept.
- `--lark-allowed-chat-id`: Allowed Lark chat ids. Repeatable.
- `--lark-app-id`: Lark app id.
- `--lark-app-secret`: Lark app secret.
- `--lark-base-url`: Lark Open API base URL.
- `--lark-encrypt-key`: Lark event subscription encrypt key.
- `--lark-group-trigger-mode`: Group trigger mode, `strict|smart|talkative`.
- `--lark-max-concurrency`: Max number of Lark conversations processed concurrently.
- `--lark-task-timeout`: Per-message agent timeout.
- `--lark-verification-token`: Lark event subscription verification token.
- `--lark-webhook-listen`: Listen address for the Lark webhook server.
- `--lark-webhook-path`: HTTP path for the Lark webhook callback.

## `install`

- `-y, --yes`: Skip confirmation prompts.

## `skills list`

- `--skills-dir`: Skills root directory. Repeatable.

## `skills install`

- `--clean`: Remove existing skill dir before copying.
- `--dest`: Destination directory.
- `--dry-run`: Print operations without writing files.
- `--max-bytes`: Max bytes to download for a remote `SKILL.md`.
- `--skip-existing`: Skip files that already exist in destination.
- `--timeout`: Timeout for downloading a remote `SKILL.md`.
- `-y, --yes`: Skip confirmation prompts.

## `tools`

This command currently has no command-specific flags.
