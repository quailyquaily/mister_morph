# Tools Reference

This document describes the built-in and runtime-injected tool parameters currently implemented in the codebase (based on the current tool constructors/registrars and runtime wiring functions).

## Registration and Availability

### 1) Tool classes in current code

- `static` tools (fully constructable from config only):
  - `read_file`, `write_file`, `bash`, `powershell`, `url_fetch`, `web_search`, `contacts_send`.
  - `contacts_send` is static, but default exposure is limited to awareness runs when enabled, or to explicit `$contacts_send` opt-in.
- `engine-scoped` tools:
  - `spawn`: registered when an agent engine is assembled for a run; depends on the current subtask runner, parent tool lookup, and default model.
  - `coder`: registered when an agent engine is assembled for a run; depends on the current subtask runner and starts the local Codex or Claude Code CLI.
  - `acp_spawn`: registered when an agent engine is assembled for a run; depends on ACP agent profiles plus the current subtask runner.
- `runtime-dependent` tools:
  - `todo_update`: runtime-injected, depends on active LLM client/model plus cron/contacts paths from runtime config.
  - `plan_create`: runtime-injected, depends on active LLM client/model.
  - `image_generate`, `image_edit`: per-task runtime tools. They depend on usable image LLM config, `file_cache_dir`, and image intent/retention state.
  - `telegram_send_voice`, `telegram_send_photo`, `telegram_send_file`: runtime-injected, depend on active Telegram API context/chat metadata.
  - `message_react`: runtime-injected in Telegram and Slack runtimes; params/context differ by channel.

### 2) ASCII architecture

```text
Config Source A (CLI/Channels)                    Config Source B (integration)
--------------------------------                  --------------------------------
LoadRuntimeToolsRegisterConfigFromViper()         runtimeSnapshot + feature flags
                 |                                                 |
                 v                                                 v
          RuntimeToolsRegisterConfig <---------------------- build runtime cfg
        { PlanCreateRegisterConfig, TodoUpdateRegisterConfig, ImageToolsRegisterConfig }
                                   |
                                   v
tools.NewRegistry()
   |
   v
RegisterStaticTools(...)
   |
   v
Execution path split:

  A) run / serve / integration run-engine
     RegisterRuntimeTools(reg, runtimeCfg, llmClient, model)
       |-- RegisterImageTools(...)
       |-- RegisterPlanTool(...)
       `-- RegisterTodoUpdateTool(...)
            |
            v
         buildLLMTools(...) -> Engine exposes tool schemas to LLM

  B) telegram / slack / line task runtimes
     clone/copy base registry (base registry required non-nil)
       `-- RegisterRuntimeTools(taskReg, runtimeCfg, llmClient, model)
              |-- RegisterImageTools(...)
              |-- RegisterPlanTool(...)
              `-- RegisterTodoUpdateTool(...)
                    |
                    v
               SetTodoUpdateToolAddContext(...)
               + Telegram runtime tools (telegram_*)
               + Slack runtime tool (`message_react`, when runtime context and emoji catalog are available)
                    |
                    v
               buildLLMTools(...) -> Engine exposes tool schemas to LLM
   |
   v
LLM tool call -> registry.Get(name) -> tool.Execute(...)
```

Flow notes:

- Phase A (static): build base registry via `RegisterStaticTools`.
- Phase A.5 (engine tools): register engine-scoped tools such as `spawn`, `coder`, and `acp_spawn` when `agent.New(...)` assembles a runnable engine.
- Phase B (runtime deps): build `RuntimeToolsRegisterConfig`, then inject via `RegisterRuntimeTools`.
- Tool `enabled=false` means the tool is not exposed by default. A task can opt in for one turn with `$name`, for example `$bash` or `$image_generate`.
- `$name` does not execute a tool directly. It only makes the matched tool schema available for the current task.
- If `$name` does not match any skill or tool, it remains ordinary user text. The runtime does not report a missing capability.
- Explicit opt-in does not bypass guard rules, sandbox limits, credentials, runtime prerequisites, or host tool allowlists.
- `coder` follows the same explicit opt-in path: `tools.coder.enabled=true` exposes it by default, and `$coder` exposes it for the current task. When selected, it starts local Codex / Claude Code with approval and permission prompts bypassed. If those CLIs are outside the service PATH, set `tools.coder.path_extra`.
- `$image_generate` / `$image_edit` and natural-language image intent use the same per-task tool trigger path.
- Image tools are checked per task, not once at process startup.
- Image tools are registered only when image config is usable. Full inheritance from top-level `llm.*` is allowed only for top-level `openai` or `gemini` with `llm.api_key`; `openai_codex` auth does not provide image credentials.
- Phase C (task shaping):
  - `run`/`serve`/integration run-engine: inject runtime tools directly into execution registry.
  - `telegram`/`slack`/`line`: copy base registry per task, re-register runtime tools on task registry, then bind task context with `SetTodoUpdateToolAddContext`.
  - Telegram-only task registry adds `telegram_send_voice`, `telegram_send_photo`, `telegram_send_file`, `message_react`.
  - Slack task registry may add `message_react` when runtime context allows.
- Image-tool retention:
  - console web topics and CLI chat sessions keep image tools after the first image-intent task.
  - Telegram, Slack, Lark, and LINE conversations keep image tools for 16 subsequent turns; another image-intent task refreshes the counter.
- First-principles invariants:
  - correctness: task toolset matches chat/channel context.
  - isolation: `todo_update` context is task-scoped.
  - determinism: no hidden fallback registration path.
  - minimality: Phase C shapes task registry only.

### 3) From registry to execution

- `buildLLMTools` converts registry entries to `[]llm.Tool` using each tool's `Name()`, `Description()`, `ParameterSchema()`.
- Engine sends this set on each LLM call.
- On tool call, engine resolves by `registry.Get(name)` and runs `tool.Execute(ctx, params)`.

### 4) `mistermorph tools` command view

- `tools` command prints:
  - `Core tools`: from base registry.
  - `Extra tools`: preview of engine-scoped and runtime-dependent tools (currently `spawn`, `coder`, `acp_spawn`, `plan_create`, `todo_update`, and image tools when task intent allows them).
  - `Telegram tools`: static preview rows for Telegram runtime tools.

## `read_file`

Purpose: read local text file content (very long output may be truncated).

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | `string` | Yes | None | File path. Supports `file_cache_dir/<path>` and `file_state_dir/<path>` aliases. |

Constraints:

- Access can be blocked by `tools.read_file.deny_paths`.
- Alias paths must include a relative file path; passing only `file_cache_dir` or `file_state_dir` is invalid.

## `write_file`

Purpose: write local files (overwrite or append).

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | `string` | Yes | None | Target path. Relative paths are written under `file_cache_dir` by default. Supports `file_state_dir/<path>`. |
| `content` | `string` | Yes | None | Text content to write. |
| `mode` | `string` | No | `overwrite` | `overwrite` or `append`. |

Constraints:

- Parent directories are created automatically when needed.
- Writes are allowed only under `file_cache_dir` / `file_state_dir`.
- Content size is limited by `tools.write_file.max_bytes`.

## `bash`

Purpose: execute local `bash` commands.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `cmd` | `string` | Yes | None | Bash command to execute. Supports `file_cache_dir/...` and `file_state_dir/...` aliases. |
| `cwd` | `string` | No | Current directory | Working directory for command execution. Supports `file_cache_dir/...` and `file_state_dir/...` aliases. |
| `timeout_seconds` | `number` | No | `tools.bash.timeout` | Timeout override in seconds. |
| `run_in_subtask` | `boolean` | No | `false` | If `true`, run the command inside the direct bash subtask boundary and return the structured subtask envelope JSON. |

Constraints:

- Default enablement is platform-specific: enabled by default on Linux/macOS, disabled by default on Windows. Override with `tools.bash.enabled`.
- Restricted by `tools.bash.deny_paths` and internal deny-token rules.
- Runs with an allowlisted environment instead of inheriting the full parent process environment.
- Extra environment variables can be injected via `tools.bash.injected_env_vars`. Each entry may be:
  - a variable name string: resolved from the parent process when config is loaded, then injected as a fixed `name=value` pair;
  - an object `{name, value}`: injects the literal value (supports `${ENV_VAR}` and `${aws-sm:<secret-id>}` expansion when the config file is loaded via `ReadExpandedConfig`).
- `tools.bash.rewrite.enabled` optionally prefixes commands before execution with `tools.bash.rewrite.binary`. For example, binary `rtk` turns `git status` into `rtk git status`.

## `powershell`

Purpose: execute local PowerShell commands.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `cmd` | `string` | Yes | None | PowerShell command to execute. Supports `file_cache_dir/...`, `file_state_dir/...`, and backslash variants such as `file_cache_dir\...`. |
| `cwd` | `string` | No | Current directory | Working directory for command execution. Supports `file_cache_dir/...` and `file_state_dir/...` aliases. |
| `timeout_seconds` | `number` | No | `tools.powershell.timeout` | Timeout override in seconds. |

Constraints:

- Default enablement is platform-specific: enabled by default on Windows, disabled by default on Linux/macOS. Override with `tools.powershell.enabled`.
- Restricted by `tools.powershell.deny_paths` and internal deny-token rules.
- Runs with an allowlisted environment instead of inheriting the full parent process environment.
- Extra environment variables can be injected via `tools.powershell.injected_env_vars`, using the same string or `{name, value}` object forms as `tools.bash.injected_env_vars`.
- Unlike `bash`, it does not currently support `run_in_subtask`.

## `url_fetch`

Purpose: make HTTP(S) requests and return responses (or download to file).

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `url` | `string` | Yes | None | Request URL. Only `http/https` are supported. |
| `method` | `string` | No | `GET` | `GET` / `POST` / `PUT` / `PATCH` / `DELETE`. |
| `auth_profile` | `string` | No | None | Auth profile ID (available when secrets are enabled). |
| `headers` | `object<string,string>` | No | None | Custom headers (allowlist/denylist applies). |
| `body` | `string|object|array|number|boolean|null` | No | None | Request body (for `POST/PUT/PATCH` only). |
| `download_path` | `string` | No | None | Save response body to a cache-directory path. |
| `timeout_seconds` | `number` | No | `tools.url_fetch.timeout` | Timeout override in seconds. |
| `max_bytes` | `integer` | No | `tools.url_fetch.max_bytes` or download cap | Maximum bytes to read. |

Constraints:

- Parent directories for `download_path` are created automatically.
- With `download_path`, the tool returns download metadata instead of embedding large response bodies.
- `headers` has security restrictions (for example, direct `Authorization` and `Cookie` are blocked).
- If `headers` is not provided and `body` is provided, `Content-Type` is inferred from body type (`application/json` for JSON/object bodies, `text/plain` for plain strings).
- At debug log level, the tool logs sanitized outbound request fields (`url`, `method`, `headers`).
- Requests are subject to guard network policy.

## `web_search`

Purpose: run web search and return structured results (current implementation uses DuckDuckGo HTML).

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `q` | `string` | Yes | None | Search keywords. |
| `max_results` | `integer` | No | `tools.web_search.max_results` | Maximum returned results (hard-capped at 20 in code). |

## `todo_update`

Purpose: maintain scheduled TODO tasks in `file_state_dir/cron.yaml`.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `action` | `string` | Yes | None | `add_once`, `add_recurring`, or `delete`. |
| `title` | `string` | No | Default task title | Short task title for `add_once` and `add_recurring`. |
| `content` | `string` | Yes for add actions; required for delete without `id` | None | Task content, or semantic delete query when `id` is omitted. |
| `at` | `string` | Yes for `add_once` | None | One-time schedule in `YYYY-MM-DD HH:mm`. |
| `cron` | `string` | Yes for `add_recurring` | None | Five-field numeric cron expression. |
| `tz` | `string` | No | Runtime local timezone | IANA timezone or UTC offset, for example `Asia/Tokyo` or `UTC+8`. |
| `id` | `string` | No for add; preferred for delete | Generated for add | Stable task ID. |
| `people` | `array<string>` | No | None | Mentioned people to resolve into explicit reference IDs in added content. |
| `chat_id` | `string` | No | Empty | Task-context chat ID, for example `tg:-1001234567890`. |

Returns:

- `add_once` and `add_recurring` return `AddResult` JSON:
  - `ok`: whether operation succeeded (boolean).
  - `action`: executed action.
  - `task_count`: number of tasks after the update.
  - `task`: added task.
  - `warnings`: optional reference-resolution warnings.
- `delete` returns `DeleteResult` JSON:
  - `ok`: whether operation succeeded.
  - `action`: `delete`.
  - `task_count`: number of tasks after deletion.
  - `deleted`: deleted task.

Constraints:

- Controlled by `tools.todo_update.enabled`.
- Add actions require an LLM client and model when `people` resolution is needed.
- Add actions validate markdown reference IDs before writing.
- Delete by `id` does not require an LLM.
- Delete without `id` uses LLM semantic matching on `content`; no match and ambiguous match both return errors.

Errors (string matching):

| Error substring | Trigger |
|---|---|
| `todo_update tool is disabled` | Tool is disabled. |
| `action is required` | Missing `action`. |
| `content is required` | Missing or empty `content`. |
| `invalid action:` | `action` is not `add_once/add_recurring/delete`. |
| `todo_update unavailable (missing llm client)` | LLM client not injected. |
| `todo_update unavailable (missing llm model)` | LLM model not configured. |
| `invalid reference id:` | Invalid `(...)` reference exists in text. |
| `missing_reference_id` | Mentioned person cannot be uniquely resolved to a reference ID. |
| `people must be an array of strings` | `people` is not a string array. |
| `invalid reference_resolve response` | Reference insertion LLM returned invalid JSON. |
| `no matching cron task in cron.yaml` | Delete found no matching task. |
| `ambiguous cron task match` | Delete without `id` matched multiple candidates. |
| `invalid semantic_match response` | Delete semantic match LLM returned invalid JSON/schema. |

## `contacts_send`

Purpose: send one message to one or more contacts (auto-routed via Telegram/Slack/LINE/Lark).

Contact profile maintenance:

- To read contacts, use `read_file` on `file_state_dir/contacts/ACTIVE.md` and `file_state_dir/contacts/INACTIVE.md`.
- To update contacts, use `write_file` to edit those files directly (following the YAML profile template structure).

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `contact_id` | `string` | Yes | None | Target contact ID. Multiple contacts may be passed as comma-separated values. |
| `chat_id` | `string` | No | Empty | Optional chat hint (for example `tg:-1001234567890`, `slack:T001:C002`, `line:Cgroup001`). |
| `content_type` | `string` | No | `application/json` | Payload type; must be envelope JSON type. |
| `message_text` | `string` | Conditionally required | None | Message text; the tool wraps it into an envelope. |
| `message_base64` | `string` | Conditionally required | None | base64url-encoded envelope JSON. |
| `session_id` | `string` | No | Empty | Session ID (UUIDv7). `contacts_send` always sends `chat.message`. |
| `reply_to` | `string` | No | Empty | Optional reply target `message_id`. |

Constraints:

- Can be disabled via `tools.contacts_send.enabled`.
- Default exposure requires awareness mode and `tools.contacts_send.enabled=true`.
- Non-awareness runtimes, including Telegram private sessions, do not expose `contacts_send` by default.
- Explicit `$contacts_send` opt-in can expose it for the current task even when `tools.contacts_send.enabled=false`; it does not bypass selected tool allowlists, guard rules, credentials, or runtime prerequisites.
- `contacts_send` always uses topic `chat.message` (caller does not pass `topic`).
- If cross-session forwarding is needed in group chat (for example, explicit "DM someone"), trigger it via explicit task/command, not by routing ordinary group replies to `contacts_send`.
- If `chat_id` is provided:
  - Telegram: used only when matching `tg_private_chat_id` or `tg_group_chat_ids`; otherwise falls back to `tg_private_chat_id`.
  - Slack: used directly as `slack:<team_id>:<channel_id>`.
  - LINE: used only when matching `line_chat_ids`; otherwise falls back to `line_user_id`.
  - If still unavailable, the tool returns an error.
- At least one of `message_text` or `message_base64` is required.
- `content_type` defaults to `application/json`, and must be `application/json` (parameters allowed, for example `application/json; charset=utf-8`).
- If `message_base64` is provided, decoded payload must be envelope JSON containing `message_id` / `text` / `sent_at (RFC3339)` / `session_id (UUIDv7)`.
- Sending to human contacts is allowed by default; actual deliverability still depends on sendable targets in contact profiles (private/group chat IDs).

## `plan_create`

Purpose: generate execution-plan JSON. Usually called by the system for complex tasks.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `task` | `string` | Yes | None | Task description to plan. |
| `max_steps` | `integer` | No | Config default (`tools.plan_create.max_steps`, usually 6) | Maximum number of steps. |
| `style` | `string` | No | Empty | Plan style hint, for example `terse`. |
| `model` | `string` | No | Current default model | Override model for plan generation. |

## `image_generate`

Purpose: generate one image from a text prompt and save it as a local file.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `prompt` | `string` | Yes | None | Image generation prompt. |
| `output_path` | `string` | No | `file_cache_dir/images/<timestamp>-<id>.<ext>` | Output path. Supports `workspace_dir/...` and `file_cache_dir/...`; relative paths resolve under `file_cache_dir/images/`. |

Constraints:

- Controlled by `tools.image_generate.enabled`.
- Registered only when the current task has explicit image intent, or when the current session has retained image-tool state.
- Uses `llm.image.model`, or the current runtime model when the image model is empty.
- `openai_codex` auth is chat-only. Use explicit `llm.image` credentials when chat uses Codex auth.
- Produces exactly one image.
- Output files are limited to `workspace_dir` and `file_cache_dir`.
- Returned MIME type decides the extension. A conflicting `output_path` extension returns an error.

## `image_edit`

Purpose: edit one local image from a text prompt and save one output image.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `prompt` | `string` | Yes | None | Image edit prompt. |
| `input_path` | `string` | No | None | Input image path. Supports `workspace_dir/...` and `file_cache_dir/...`. Required unless `use_active_image` is true. |
| `use_active_image` | `boolean` | No | `false` | Use the current session active image as input when `input_path` is empty. |
| `output_path` | `string` | No | `file_cache_dir/images/<timestamp>-<id>.<ext>` | Output path. Supports `workspace_dir/...` and `file_cache_dir/...`; relative paths resolve under `file_cache_dir/images/`. |

Constraints:

- Controlled by `tools.image_edit.enabled`.
- Registered only when the current task has explicit image intent, or when the current session has retained image-tool state.
- Uses `llm.image.model`, or the current runtime model when the image model is empty.
- `openai_codex` auth is chat-only. Use explicit `llm.image` credentials when chat uses Codex auth.
- Accepts exactly one input image and produces exactly one output image.
- Input and output files are limited to `workspace_dir` and `file_cache_dir`; `file_state_dir` is not accepted.
- Current-turn channel image attachments are exposed to the model as `file_cache_dir/...` aliases when available.
- Successful `image_generate` and `image_edit` calls set the current session active image when the runtime has a conversation scope.

## `telegram_send_file`

Purpose: send a local cached file (document) to the current Telegram chat.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | `string` | Yes | None | Local file path. Supports absolute path or relative path under `file_cache_dir`. |
| `filename` | `string` | No | `basename(path)` | File name displayed in Telegram. |
| `caption` | `string` | No | Empty | Optional file caption. |

Constraints:

- Available only in Telegram mode.
- `path` supports `file_cache_dir/<path>` alias form.
- Only files under `file_cache_dir` can be sent; directories return errors.
- File size is limited by tool cap (currently 20 MiB by default).

## `telegram_send_photo`

Purpose: send a local cached image to the current Telegram chat as an inline photo.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | `string` | Yes | None | Local image path. Supports absolute path or relative path under `file_cache_dir`. |
| `caption` | `string` | No | Empty | Optional photo caption. |

Constraints:

- Available only in Telegram mode.
- `path` supports `file_cache_dir/<path>` alias form.
- Only files under `file_cache_dir` can be sent; directories return errors.
- File size is limited by tool cap (currently 20 MiB by default).
- This tool sends the image as an inline Telegram photo; use `telegram_send_file` when the user should receive it as a document.

## `telegram_send_voice`

Purpose: send a Telegram voice message from a local voice file.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `chat_id` | `integer` | No | Current context chat | Target Telegram `chat_id`. Required if there is no active chat context. |
| `path` | `string` | Yes | None | Local voice file path (recommended `.ogg`/Opus). Supports absolute path or relative path under `file_cache_dir`. |
| `filename` | `string` | No | `basename(path)` | File name displayed in Telegram. |

Constraints:

- Available only in Telegram mode.
- Only local-file sending is supported; inline text-to-speech is not supported.
- Local files are limited to `file_cache_dir` and file-size caps (currently 20 MiB by default).

## `message_react` (Telegram)

Purpose: add emoji reactions to Telegram messages.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `chat_id` | `integer` | No | Current context chat | Target Telegram `chat_id`. |
| `message_id` | `integer` | No | Trigger message ID | Message ID to react to. |
| `emoji` | `string` | Yes | None | Reaction emoji. |
| `is_big` | `boolean` | No | Empty | Whether to use Telegram large reaction style. |

Constraints:

- Available only in Telegram mode.
- Requires `message_id` context in Telegram mode (or explicit `message_id` input).

## `message_react` (Slack)

Purpose: add emoji reactions to Slack messages.

Parameters:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `channel_id` | `string` | No | Current context channel | Target Slack `channel_id`. |
| `message_ts` | `string` | No | Trigger message ts | Message timestamp (`ts`) to react to. |
| `emoji` | `string` | Yes | None | Slack emoji name (for example `thumbsup` or `:thumbsup:`). |

Constraints:

- Available only in Slack mode.
- Requires `channel_id` and `message_ts` context in Slack mode (or explicit params).
- Emoji must be a valid Slack emoji name format (not raw Unicode emoji).
- If emoji catalog is loaded, emoji name must exist in current workspace catalog.
- Subject to `allowed_channel_ids` restriction when configured.

## Notes

- Runtime parameter validation follows each tool's `ParameterSchema()` and execution-time checks inside the corresponding tool/runtime handlers.
- If a tool is disabled by configuration, it returns a `... tool is disabled` error.

## TODO

- Refactor duplicated Phase C task-registry shaping logic across Telegram/Slack/LINE runtime tool re-registration into a shared helper.
