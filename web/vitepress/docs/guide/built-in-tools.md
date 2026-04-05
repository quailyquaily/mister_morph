---
title: Built-in Tools
description: Static tools, runtime-injected tools, and channel-specific tools.
---

# Built-in Tools

Mistermorph does not register every tool as one flat bundle. Tools are layered by runtime environment:

1. Static tools: created from config and directory context alone.
2. Engine tools: registered when an agent engine is assembled for a run.
3. Runtime tools: require an active LLM client/model or task context.
4. Dedicated tools: only appear inside concrete runtimes such as Telegram or Slack.

## Tool Groups at a Glance

| Group | When available | Tools |
|---|---|---|
| Static tools | Available from config alone | `read_file`, `write_file`, `bash`, `url_fetch`, `web_search`, `contacts_send` |
| Engine tools | Available when an agent engine is assembled for a run | `spawn` |
| Runtime tools | Available when the LLM or required context is available | `plan_create`, `todo_update` |
| Channel-specific tools | Available when the current channel is Telegram / Slack or another concrete channel runtime | `telegram_send_voice`, `telegram_send_photo`, `telegram_send_file`, `message_react` |

## Static Tools (config-driven)

### `read_file`

Reads local text files. The agent uses it to inspect config files, logs, cached results, `SKILL.md`, or state files.

- Key limits: subject to `tools.read_file.deny_paths`; supports `file_cache_dir/...` and `file_state_dir/...` aliases.

### `write_file`

Writes local files in overwrite or append mode, for generated output, state updates, or saving downloaded results locally.

- Key limits: writes are restricted to `file_cache_dir` / `file_state_dir`; relative paths default to `file_cache_dir`; size is capped by `tools.write_file.max_bytes`.

### `bash`

Executes local `bash` commands to call existing CLIs, run one-off conversions, execute scripts, or inspect the local environment.

- Key limits: can be disabled via `tools.bash.enabled`; restricted by `deny_paths` and internal deny-token rules; child processes inherit only an allowlisted environment.

### `url_fetch`

Makes HTTP(S) requests and returns the response, or downloads the response into a local cache file. Supports `GET/POST/PUT/PATCH/DELETE`, `download_path`, and `auth_profile`.

- Key limits: sensitive request headers are blocked; requests still pass through Guard network policy.

### `web_search`

Runs a web search and returns structured search results. Useful for discovering leads, candidate pages, and public information entry points.

- Key limits: it returns search-result summaries, not full page bodies; result count is capped by `tools.web_search.max_results` and code-level limits.

### `contacts_send`

Sends one outbound message to a single contact. Delivery is chosen from the contact profile, such as Telegram, Slack, or LINE.

- Key limits: some group/supergroup contexts hide this tool by default.

## Engine Tools

These tools are registered when an agent engine is assembled for a run. They depend on the current engine state, so they are not part of the static base registry.

### `spawn`

Starts a subtask with its own context and an explicit tool whitelist. The parent agent blocks until the child finishes and receives a structured result envelope.

- Key limits: can be disabled via `tools.spawn.enabled`; the child can use only the tool names passed in `tools`; raw child transcript is not returned to the parent loop by default.

## Runtime Tools

These tools are injected dynamically while the agent is running.

### `plan_create`

Generates structured execution-plan JSON, typically for complex task decomposition.

- Key limits: step count is capped by `tools.plan_create.max_steps`.

### `todo_update`

Maintains `TODO.md` / `TODO.DONE.md` under `file_state_dir`, including add and complete operations.

- Key limits: `add` requires `people`; `complete` uses semantic matching and will error on no-match or ambiguous match.

## Dedicated Tools

These tools do not exist in plain CLI or generic embedding scenarios. They are injected only when the corresponding channel runtime has enough context.

### `telegram_send_voice`

Sends a local voice file back to the current Telegram chat.

- Key limits: only local-file sending is supported; files are typically expected under `file_cache_dir`; this tool does not do inline text-to-speech generation.

### `telegram_send_photo`

Sends a local image back to Telegram as an inline photo.

- Key limits: this is a photo-style send, not a document send; use `telegram_send_file` if the user should receive it as a file attachment.

### `telegram_send_file`

Sends a local cached file to Telegram as a document.

- Key limits: only local cached files are allowed; directories are invalid; file-size caps apply.

### `message_react`

Adds a lightweight emoji reaction to the current message, for cases like acknowledgement, approval, or a quick "seen" signal that does not justify a full text reply.

- Telegram variant: reacts to a Telegram message with an emoji and can optionally use large-reaction style.
- Slack variant: reacts to a Slack message using a Slack emoji name, not a raw Unicode emoji.
- Key limits: parameter shape differs by channel; without channel-specific message context, the tool may be absent or require explicit target parameters.

## Tool Selection in Core Embedding

You can whitelist built-ins via `integration.Config.BuiltinToolNames`.

```go
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "todo_update"}
```

Empty list means all known built-ins.

## Key Config Sections

```yaml
tools:
  read_file: ...
  write_file: ...
  spawn: ...
  bash: ...
  url_fetch: ...
  web_search: ...
  contacts_send: ...
  todo_update: ...
  plan_create: ...
```

Console Setup / Settings and the `/api/settings/agent` payload use the same nested shape, for example `tools.spawn.enabled`.

For the full configuration, see [Config Reference](/guide/config-reference.md).
