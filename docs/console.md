# Mistermorph Console SPA

This document describes the Console SPA under `web/console`, used by:

```bash
morph console
```

`morph console serve` remains supported with the same flags and behavior.

Stack:
- Vue 3 + Vue Router
- `quail-ui`
- Vite (`src` -> `dist`)
- package manager: `pnpm`

Quail UI JavaScript uses named imports from `src/components/quail.js`, not the
full `QuailUI` plugin. Add new components there, including any dependencies that
the component resolves through Vue's global registry. `main.js` keeps the shared
popup-close handler and the full Quail UI stylesheet.

## Runtime Notes

- Console APIs are served under `<console.base_path>/api` (default: `/api`).
- Runtime views (`Chat`, `Runtime`, `Tasks`, `Stats`, `Audit`, `Files`, `Contacts`) read from the endpoint selected in the top bar.
- `console` always exposes one built-in local runtime endpoint (`Console Local`).
  - It runs tasks in its own runtime loop via shared runtime core.
  - Its runtime API uses the shared `daemonruntime` handler. With an explicit `server.auth_token`, the same handler is available at `<console.base_path>/runtime`; no extra TCP listener is started.
  - If `server.auth_token` is unset, the local runtime generates an internal in-process token and does not expose `<console.base_path>/runtime`.
  - Task/topic changes are written to stable segments under `<file_state_dir>/journal/`. When `tasks.persistence_targets` contains `console`, its task projection is also saved and restored across process restarts.
  - The local runtime currently provides topic-aware APIs (`GET /topics`, `DELETE /topics/{topic_id}`) and runs awareness through the shared direct awareness runtime. Periodic heartbeat is optional; `/poke` remains available when heartbeat is disabled.
- Additional remote runtime endpoints can be configured under `console.endpoints` in `config.yaml`. Each `url` is the complete runtime API base URL; new built-in runtime servers use `/runtime`.
- Remote runtime endpoints still use the shared runtime API contract, but topic APIs are only available when that runtime injects `TopicReader` / `TopicDeleter`.
- A remote endpoint whose health payload reports `mode: console` exposes the same target-owned settings as the built-in local Console. The SPA sends those requests through `/api/proxy` to the remote Console's `/runtime` API.
- Task WebSocket frames from a remote Console are relayed by the current Console. The browser never receives the remote runtime token, and task polling remains active as the fallback.

## Architecture (ASCII)

```text
            +------------------------------+
            | Browser (Console SPA)        |
            | Chat / Tasks / Runtime views |
            +---------------+--------------+
                            |
                            v
            +---------------+--------------+
            | Console Backend              |
            | <base_path>/api + /runtime   |
            | auth / endpoints / proxy     |
            +---------------+--------------+
                            |
         +------------------+-------------------+
         |                                      |
 +-------v--------+                    +--------v---------+
 | Console Local  |                    | Remote Runtime   |
 | in-process     |                    | endpoint(s)      |
 | runtime API    |                    | (from config)    |
 +-------+--------+                    +--------+---------+
         |                                      |
         v                                      v
 +-------+--------------------------------------+--------+
 | daemonruntime handlers                                |
 | /health /overview /tasks /tasks/{id} /topics?        |
 | /state/* /audit/* /contacts/*                        |
 +-------+--------------------------------------+--------+
         |                                      |
         |                                      +--> remote TaskView
         |                                           (MemoryStore or FileTaskStore)
         v
 +-------+-----------------------------------------------+
 | consoleLocalRuntime                                   |
 | per-topic ConversationRunner + direct awareness loop  |
 | + submit/topic orchestration                          |
 +-------+-------------------------------+---------------+
         |                               |
         v                               v
 +-------+--------+            +---------+---------+
 | ConsoleFileStore|            | agent.Engine     |
 | TaskView+Topic  |            | shared runtime   |
 | in-memory view  |            | execution        |
 +-------+--------+            +-------------------+
         |
         v
 +-------+-----------------------------------------------+
 | file_state_dir/journal/events.*.jsonl                 |
 | task/topic facts for ConsoleFileStore replay          |
 +-------------------------------------------------------+
```

## Features

- Overview:
  - endpoint list only
  - endpoint card click selects endpoint and opens `Chat`
  - auto-refresh every 60 seconds
- Setup:
  - dedicated `/setup` route for the minimal Console Local configuration path
  - shown when Console Local is online but local chat is not yet submittable
  - guides the user to finish provider/model/API key config, then refresh status
- Chat:
  - send task directly to current agent
  - left secondary sidebar for topics, with one `New Topic` button, topic switching, current-topic delete, and a hidden system topic toggle exposed by clicking the topic sidebar title five times
  - topic title is seeded from the first prompt; once the new topic's task reaches the queue, a separate background LLM request names it and selects a theme icon from a fixed Phosphor catalog, without waiting for the task's reply
  - initial naming uses only the first user message, keeps its language, and does not invent a subject for greetings; request timeouts, retries, and fallback follow the configured LLM route
  - generated titles and icons are persisted together; failed naming leaves the seed title, later messages do not rename the topic, and background results cannot overwrite a customized title or restore a deleted topic
  - topic items use the selected icon, or a chat icon for old/unknown values; visible pages check metadata every five seconds without resetting topic selection, drafts, or pagination
  - `internal/taskdomain/topic_icons.json` maps theme IDs to descriptions; matching regular-weight Phosphor SVGs live in `web/console/src/assets/topic-icons/`, with their MIT license. Topic icons use CSS masks and inherit the text color without Vue icon components. Greetings use a separate waving-hand icon, with specific themes for pets, babies, home and family, food, fitness, music, and travel
  - `Regenerate name` sits below the properties in the Topic panel, separate from Delete Topic, on desktop and mobile. It generates the name and icon from the first user message and the latest six conversation turns, preferring recent substantive discussion when the subject changes
  - regeneration excludes slash commands, reasoning, tool logs, and incomplete replies. Each user message is capped at 600 characters and each completed assistant reply at 400; the total naming input is capped at 8,000 characters
  - regeneration keeps the current name and icon until success, rejects duplicate requests for the same topic, and leaves them unchanged on failure. A title revision prevents late results from overwriting subsequent edits or deletion; starting regeneration also invalidates pending initial naming. Empty and system topics cannot be regenerated
  - topic-scoped `ChatHistoryItems` style list
  - receive task progress over WebSocket for local and remote Console endpoints, with `/tasks/{id}` polling as fallback
- Tasks:
  - list + detail (read-only)
- Files:
  - unified editor for `cron.yaml`, `identity.yaml`, `soul.md`, and `HEARTBEAT.md`
- Contacts:
  - dedicated sidebar entry
  - structured list rendering from `ACTIVE.md` + `INACTIVE.md`
  - status filter (`all|active|inactive`)
  - circular cached avatars with a stable initial fallback; local and remote endpoints use the same authenticated proxy path
- Audit:
  - browse Guard audit files
  - cursor-based reads for large JSONL logs (`limit` + opaque `cursor`)
  - newest entries shown first in the UI
  - entries grouped by `run_id` for easier review
  - `OutputPublish` audit events mark `body_omitted_from_audit: true` in the raw JSONL when the final body is intentionally not persisted
  - Guard keeps `guard_audit.jsonl` as the canonical ledger and may emit per-decision mirrors such as `guard_audit.allow_with_redaction.jsonl`, `guard_audit.require_approval.jsonl`, and `guard_audit.deny.jsonl`
- Settings:
  - language selector
  - logout button (danger style)
  - entry moved to top-right, next to endpoint switcher
  - Agent, Tools, Skills, Persona, Channels, Managed Runtimes, Guard, update checks, and provider account actions target the selected Console endpoint
  - language and logout remain browser-host actions and do not target a remote endpoint
  - agent tool toggles mirror `config.yaml` structure under `tools.<name>.enabled`
  - the Settings page now exposes the `spawn` toggle together with the other agent tools
- i18n:
  - English, Chinese, Japanese
  - language selector appears on Login and Settings (not in top nav)

## API Surface (under `/api`)

Auth:
- `POST /auth/login`
- `POST /auth/logout`
- `GET /auth/me`

Console settings:
- `GET /settings/agent`
- `PUT /settings/agent`
- `POST /settings/agent/models`
- `POST /settings/agent/test`
- `GET /settings/console`
- `PUT /settings/console`
- `GET /settings/auto-update`
- `PUT /settings/auto-update`
- `POST /settings/auto-update/check`
- `/auth/codex/*`
- `/auth/xai/*`
- `/auth/pro/*`

Settings payload note:
- The `tools` object mirrors the config tree, for example `tools.spawn.enabled` and `tools.bash.enabled`.

Dashboard/system:
- `GET /endpoints`
- `GET /proxy?endpoint=<ref>&uri=<runtime-path>`

Tasks:
- `GET /proxy?endpoint=<ref>&uri=/tasks?limit=<n>[&cursor=<opaque>]`
- `POST /proxy?endpoint=<ref>&uri=/tasks`
- `GET /proxy?endpoint=<ref>&uri=/tasks/{id}`
- `GET /proxy?endpoint=<ref>&uri=/topics?limit=<n>[&cursor=<opaque>]`
- `DELETE /proxy?endpoint=<ref>&uri=/topics/{topic_id}`
- `POST /proxy?endpoint=<ref>&uri=/topics/{topic_id}/regenerate-title`

Notes:
- Topic APIs are guaranteed on `Console Local`; other runtimes may return `503` if they do not expose topic readers/deleters.
- Regeneration returns the updated topic, including its title, icon, and `title_revision`. It returns `400` for an empty or system topic, `404` for a missing topic, `409` for an in-flight request or concurrent title change, and `503` for generation failure or an unsupported runtime. It uses the configured LLM request timeout, retries, and fallback, not the ordinary 20-second proxy timeout; disconnecting cancels the request.

Runtime routes used through `/proxy`:
- Selected Console settings:
  - `GET|PUT /settings/agent`
  - `POST /settings/agent/models`
  - `POST /settings/agent/test`
  - `GET|PUT /settings/console`
  - `GET|PUT /settings/auto-update`
  - `POST /settings/auto-update/check`
  - `/auth/codex/*`, `/auth/xai/*`, `/auth/pro/*`
- Overview/runtime:
  - `GET /overview`
- Files:
  - `GET /state/files`
  - `GET /state/files/{name}` (`cron.yaml|identity.yaml|soul.md|HEARTBEAT.md`)
  - `PUT /state/files/{name}`
  - `GET /persona/files`
  - `GET /persona/files/{name}` (`identity.yaml|soul.md`)
  - `PUT /persona/files/{name}`
  - `GET /persona/avatar`
  - `PUT /persona/avatar`
  - `DELETE /persona/avatar`
- Contacts:
  - `GET /contacts/list?status=all|active|inactive`
  - `GET /contacts/avatar?contact_id=<contact_id>`

`/contacts/list` may return an endpoint-relative `avatar_url` for a Contact whose cached avatar exists. The URL is derived state and is not written to Contact YAML. Console fetches the image through the selected endpoint, so a remote Contact avatar is never read from the local runtime by mistake.
- Audit:
  - `GET /audit/files`
  - `GET /audit/logs?file=<name>&limit=<n>[&cursor=<opaque>]`

Paginated runtime lists return `items`, `limit`, `has_next`, and optional `next_cursor`.

## Security and Caching Notes

- Console password is required (`console.password` or `console.password_hash`).
- Protected APIs require Bearer token auth.
- Anti-bruteforce protection is enabled in the backend.
- JSON API responses use no-store cache headers.
- SPA fetch requests use `cache: "no-store"`.

## Setup Wizard

- When no readable `config.yaml` is found, `morph install` starts an interactive setup wizard.
- The wizard now includes Console setup inputs:
  - `console.listen`
  - `console.base_path`
  - `console.password`
  - first `console.endpoints[]` entry (`name`, `url`, `auth_token` env var name)
- After input, wizard prints:
  - generated Console config snippet
  - suggested env var names
  - endpoint health probe result (`GET <endpoint>/health`)
- If the endpoint URL is local loopback (`localhost` / `127.0.0.1` / `::1`), wizard auto-generates a runtime auth token and uses `MISTER_MORPH_SERVER_AUTH_TOKEN` for endpoint auth.

## Build (production static)

1. Build frontend:

```bash
cd web/console
pnpm install
pnpm build
```

2. Stage embedded console assets for the CLI/backend binary:

```bash
cd ../..
./scripts/stage-console-assets.sh
```

3. Start console backend + static hosting:

```bash
MISTER_MORPH_SERVER_AUTH_TOKEN=dev-token \
MISTER_MORPH_ENDPOINT_TELEGRAM_TOKEN=dev-token \
MISTER_MORPH_CONSOLE_PASSWORD=secret \
go run ./cmd/mistermorph console
```

Example `config.yaml` snippet (`console.endpoints` is optional now):

```yaml
server:
  auth_token: "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

console:
  endpoints:
    - name: "Telegram"
      url: "http://127.0.0.1:8787/runtime"
      auth_token: "${MISTER_MORPH_ENDPOINT_TELEGRAM_TOKEN}"
```

4. Open:

`http://127.0.0.1:9080/`

## Dev (hot reload)

1. Build and stage console assets once before running the CLI from source:

```bash
cd web/console
pnpm install
pnpm build
cd ../..
./scripts/stage-console-assets.sh
```

2. Start console backend:

```bash
MISTER_MORPH_CONSOLE_PASSWORD=secret \
MISTER_MORPH_SERVER_AUTH_TOKEN=dev-token \
go run ./cmd/mistermorph console
```

3. Start Vite dev server:

```bash
cd web/console
pnpm install
pnpm dev
```

4. Open:

`http://127.0.0.1:5173/`

Notes:
- Vite proxies `/api` to `http://127.0.0.1:9080`.
- During frontend dev, Vite page is enough; backend static `dist` is mainly for production serving.
- If you omit `--console-static-dir`, `console` falls back to its embedded SPA assets.
- `./scripts/stage-console-assets.sh` is required before `go run ./cmd/mistermorph ...`, because the CLI validates embedded Console assets at startup.
- Optional external endpoints should point to an existing channel runtime such as `morph telegram`, `morph slack`, `morph line`, or `morph lark`.
