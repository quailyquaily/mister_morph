---
date: 2026-02-20
title: Console HTTP Admin (API + Vue3 SPA) - Requirements and Implementation Notes
status: implemented
---

# Console HTTP Admin (API + Vue3 SPA)

## 1) Purpose

Mistermorph Console is a local, authenticated web admin surface used for:
- runtime overview
- task inspection
- state file editing
- system diagnostics
- Guard audit log browsing

The runtime command is:

```bash
mistermorph console serve --console-static-dir ./web/console/dist
```

Base path defaults to `/console`, and APIs are under `/console/api/*`.

## 2) Runtime Architecture

Console is split into two pieces:
- Console backend (`mistermorph console serve`): auth + admin APIs + static SPA hosting
- Runtime daemon APIs (selected by `endpoint_ref`): source of task list/detail and mode health

Important boundary:
- Console does not maintain its own task database.
- It reads tasks from the endpoint selected in the UI top bar.
- If multiple long-running processes exist (`serve`, `telegram`, `slack`), each process can be configured as one Console endpoint.

## 3) Configuration and Flags

Primary console config keys:

```yaml
console:
  enabled: true
  listen: "127.0.0.1:9080"
  base_path: "/console"
  static_dir: ""
  password: ""
  password_hash: ""
  session_ttl: "12h"
  endpoints:
    - name: "Main"
      url: "http://127.0.0.1:8787"
      auth_token: "${MISTER_MORPH_ENDPOINT_MAIN_TOKEN}"
```

Required inputs:
- `console.static_dir` (or `--console-static-dir`) must contain `index.html`.
- One of `console.password` or `console.password_hash` must be set.

Recommended env vars:
- `MISTER_MORPH_CONSOLE_PASSWORD`
- `MISTER_MORPH_CONSOLE_PASSWORD_HASH`
- endpoint token envs referenced by `console.endpoints[*].auth_token` (use `${ENV_VAR}` syntax)

## 4) Auth and Security Model

### 4.1 Login/session
- `POST /auth/login` returns `access_token`, `token_type=Bearer`, `expires_at`.
- Console stores only `sha256(token)` server-side (in memory) with expiry.
- `POST /auth/logout` invalidates the token.

### 4.2 Password verification
- If `console.password_hash` is set, bcrypt is used.
- Otherwise, plain password comparison uses constant-time compare.

### 4.3 Fixed anti-bruteforce policy
- window: `10m`
- max failures per IP: `20`
- max failures per account+IP key: `5`
- lock duration: `15m`
- random failure delay: `200ms..1200ms`

### 4.4 Transport and caching safety
- JSON APIs send `Cache-Control: no-store` (plus `Pragma/Expires/Vary`).
- SPA API client requests use `fetch(..., { cache: "no-store" })`.
- Default bind is loopback (`127.0.0.1`).

## 5) API Surface (Current)

Auth:
- `POST /console/api/auth/login`
- `POST /console/api/auth/logout`
- `GET /console/api/auth/me`

Overview/system:
- `GET /console/api/endpoints`
- `GET /console/api/dashboard/overview`
- `GET /console/api/system/health`
- `GET /console/api/system/config`
- `GET /console/api/system/diagnostics`

Tasks (read-only):
- `GET /console/api/tasks`
- `GET /console/api/tasks/{id}`

Runtime query parameter:
- `endpoint_ref` (required):
  - `GET /console/api/dashboard/overview`
  - `GET /console/api/tasks`
  - `GET /console/api/tasks/{id}`

TODO files:
- `GET /console/api/todo/files`
- `GET /console/api/todo/files/{name}` (`TODO.md`, `TODO.DONE.md`)
- `PUT /console/api/todo/files/{name}`

Contacts files:
- `GET /console/api/contacts/files`
- `GET /console/api/contacts/files/{name}` (`ACTIVE.md`, `INACTIVE.md`)
- `PUT /console/api/contacts/files/{name}`

Persona files:
- `GET /console/api/persona/files`
- `GET /console/api/persona/files/{name}` (`IDENTITY.md`, `SOUL.md`)
- `PUT /console/api/persona/files/{name}`

Audit:
- `GET /console/api/audit/files`
- `GET /console/api/audit/logs?file=<name>&max_bytes=<n>&before=<offset>`

## 6) SPA Information Architecture

Routes:
- `/console/login`
- `/console/dashboard`
- `/console/tasks`
- `/console/tasks/:id`
- `/console/todo`
- `/console/contacts`
- `/console/persona`
- `/console/audit`
- `/console/settings`

Current UX behavior:
- Login guard redirects unauthenticated users to `/login`.
- Language selector (`QLanguageSelector`) is shown on:
  - Login page
  - Settings page
- It is intentionally not shown in the top navigation bar.
- Logout action is in Settings and uses danger styling.

## 7) Overview Page Behavior

Overview is grouped by category and currently includes:
- Basic: version, started time, uptime, health
- Model: LLM provider + model
- Channels:
  - Telegram/Slack configured state
  - Telegram/Slack running state
  - A dot badge per channel (`success` when running, `default` when not running)
- Runtime metrics:
  - Go version
  - goroutine count
  - heap alloc/sys
  - heap objects
  - GC cycles

Refresh behavior:
- no manual refresh button on overview
- automatic refresh every 60 seconds

## 8) Audit Log Browsing Design

### 8.1 Data source
- Guard JSONL audit path resolves from:
  - `guard.audit.jsonl_path`, or
  - default `<file_state_dir>/<guard.dir_name>/audit/guard_audit.jsonl`

### 8.2 Large-file strategy (no full load)
- The backend reads a byte window (`max_bytes`) instead of loading entire files.
- Default window: `128 KiB`
- Min window: `4 KiB`
- Max window: `2 MiB`
- `before` acts as a cursor for paging older/newer windows.

### 8.3 File-window semantics
- Without `before`, API reads from file tail (latest window).
- `has_older` indicates whether older content exists.
- UI supports:
  - `Latest`
  - `Older`
  - `Newer`

### 8.4 Ordering and presentation
- Backend returns lines in file order within the selected window.
- UI renders items in reverse order so newest records appear first.
- JSON audit lines are formatted into readable fields (decision/risk/action/tool/run/step/summary/reasons).
- Non-JSON lines fall back to raw rendering.

### 8.5 Staleness protections
- API responses are marked `no-store`.
- Frontend requests use `cache: "no-store"`.
- Audit chunk reading uses a single opened file descriptor for stat + read to avoid stale size/read races.

## 9) Diagnostics Scope

Diagnostics checks currently include:
- `console_static_dir` readable
- `console_static_index` readable
- `file_state_dir` writable
- `file_cache_dir` writable
- `contacts_active` readable
- `contacts_inactive` readable

System config endpoint exposes non-secret runtime settings and boolean secret-presence flags (`password_set`, `password_hash_set`) instead of raw secrets.

## 10) Known Constraints

- Task history is runtime-scoped to the daemon process Console is connected to.
- Guard audit shows Guard decision events; errors that fail before Guard action stages (for example, some direct LLM request failures) may not create new audit records.
- Single-instance in-memory session store (no distributed session persistence).

## 11) Acceptance Checklist (Implemented)

- Auth required for protected `/console/api/*` routes.
- Fixed anti-bruteforce policy active.
- Overview includes model + runtime + channel status and auto-refreshes every minute.
- Settings contains language selector and danger logout action.
- Audit entry exists in navigation.
- Audit viewer supports large files via byte-window paging and shows newest items first.
- API + client disable caching for admin JSON responses.
