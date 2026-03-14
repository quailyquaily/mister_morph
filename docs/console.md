# Mistermorph Console SPA

This document describes the Console SPA under `web/console`, used by:

```bash
mistermorph console serve
```

Stack:
- Vue 3 + Vue Router
- `quail-ui`
- Vite (`src` -> `dist`)
- package manager: `pnpm`

## Runtime Notes

- Console APIs are served under `/api`.
- Runtime views (`Chat`, `Runtime`, `Tasks`, `Stats`, `Audit`, `Memory`, `Files`, `Contacts`) read from the endpoint selected in the top bar.
- Runtime endpoints are configured under `console.endpoints` in `config.yaml`.
- Console itself does not persist task history.

## Features

- Overview:
  - endpoint list + setup guide states (no endpoint/offline/single-ready/multi-ready)
  - endpoint card click selects endpoint and opens `Chat`
  - auto-refresh every 60 seconds
- Chat:
  - send task directly to current agent
  - `ChatHistoryItems` style list
  - poll task status/result from runtime `/tasks/{id}`
- Tasks:
  - list + detail (read-only)
- Files:
  - unified editor for `TODO.md`, `TODO.DONE.md`, `IDENTITY.md`, `SOUL.md`, `HEARTBEAT.md`
- Contacts:
  - dedicated sidebar entry
  - structured list rendering from `ACTIVE.md` + `INACTIVE.md`
  - status filter (`all|active|inactive`)
- Memory:
  - browse and edit memory files (`index.md`, recent short-term session files)
- Audit:
  - browse Guard audit files
  - windowed reads for large JSONL logs (`max_bytes` + `before`)
  - newest entries shown first in the UI
  - entries grouped by `run_id` for easier review
- Settings:
  - language selector
  - logout button (danger style)
  - entry moved to top-right, next to endpoint switcher
- i18n:
  - English, Chinese, Japanese
  - language selector appears on Login and Settings (not in top nav)

## API Surface (under `/api`)

Auth:
- `POST /auth/login`
- `POST /auth/logout`
- `GET /auth/me`

Dashboard/system:
- `GET /endpoints`
- `GET /proxy?endpoint=<ref>&uri=<runtime-path>`

Tasks:
- `GET /proxy?endpoint=<ref>&uri=/tasks?...`
- `POST /proxy?endpoint=<ref>&uri=/tasks`
- `GET /proxy?endpoint=<ref>&uri=/tasks/{id}`

Runtime routes used through `/proxy`:
- Overview/runtime:
  - `GET /overview`
- Files:
  - `GET /state/files`
  - `GET /state/files/{name}` (`TODO.md|TODO.DONE.md|IDENTITY.md|SOUL.md|HEARTBEAT.md`)
  - `PUT /state/files/{name}`
- Contacts:
  - `GET /contacts/list?status=all|active|inactive`
- Memory:
  - `GET /memory/files`
  - `GET /memory/files/{id}` (`index.md` or `YYYY-MM-DD/<session>.md`)
  - `PUT /memory/files/{id}`
- Audit:
  - `GET /audit/files`
  - `GET /audit/logs?file=<name>&max_bytes=<n>&before=<offset>`

## Security and Caching Notes

- Console password is required (`console.password` or `console.password_hash`).
- Protected APIs require Bearer token auth.
- Anti-bruteforce protection is enabled in the backend.
- JSON API responses use no-store cache headers.
- SPA fetch requests use `cache: "no-store"`.

## Setup Wizard

- When no readable `config.yaml` is found, `mistermorph install` starts an interactive setup wizard.
- The wizard now includes Console setup inputs:
  - `console.listen`
  - `console.base_path`
  - `console.password`
  - first `console.endpoints[]` entry (`name`, `url`, `auth_token` env var name)
- After input, wizard prints:
  - generated Console config snippet
  - suggested env var names
  - endpoint health probe result (`GET <endpoint>/health`)
- If the endpoint URL is local loopback (`localhost` / `127.0.0.1` / `::1`), wizard auto-generates a runtime auth token and uses `MISTER_MORPH_SERVER_AUTH_TOKEN` for both:
  - `server.auth_token`
  - `console.endpoints[0].auth_token`

## Build (production static)

1. Build frontend:

```bash
cd web/console
pnpm install
pnpm build
```

2. Start daemon (task API source):

```bash
MISTER_MORPH_SERVER_AUTH_TOKEN=dev-token \
go run ./cmd/mistermorph serve --server-auth-token dev-token
```

3. Start console backend + static hosting:

```bash
MISTER_MORPH_ENDPOINT_MAIN_TOKEN=dev-token \
MISTER_MORPH_CONSOLE_PASSWORD=secret \
go run ./cmd/mistermorph console serve --console-static-dir ./web/console/dist
```

Example `config.yaml` snippet:

```yaml
console:
  endpoints:
    - name: "Main"
      url: "http://127.0.0.1:8787"
      auth_token: "${MISTER_MORPH_ENDPOINT_MAIN_TOKEN}"
```

4. Open:

`http://127.0.0.1:9080/`

## Dev (hot reload)

1. Start daemon:

```bash
MISTER_MORPH_SERVER_AUTH_TOKEN=dev-token \
go run ./cmd/mistermorph serve --server-auth-token dev-token
```

2. Start console backend:

```bash
MISTER_MORPH_CONSOLE_PASSWORD=secret \
MISTER_MORPH_SERVER_AUTH_TOKEN=dev-token \
go run ./cmd/mistermorph console serve
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
- `--console-static-dir` is optional in dev. If you omit it, `console serve` exposes only `/api` and does not serve the SPA itself.
