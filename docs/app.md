# Desktop App Wrapper (Wails)

This document describes the current desktop wrapper architecture for MisterMorph.

## 1. Goal

Provide a single desktop app entrypoint that:

- launches the Console UI without asking users to run multiple terminal commands
- supports first-run setup when config is incomplete
- reuses existing backend (`mistermorph console serve`) and existing web console (`web/console`)

## 2. Current Shape (MVP)

The desktop app is implemented under `desktop/wails` and built with tag `wailsdesktop`.

- Wails process hosts the native window and Go bindings.
- A child process runs `mistermorph console serve`.
- Wails asset handler reverse-proxies WebView requests to the child process.

This keeps CLI and console backend logic unchanged and limits wrapper-specific code to a small area.
The desktop wrapper is intentionally thin: lifecycle + process hosting + proxy only, no business rules.

## 3. ASCII Architecture

```text
+--------------------------------------------------------------+
| Desktop App Process (Wails)                                 |
|                                                              |
|  +----------------------+        +------------------------+  |
|  | WebView (UI)         | <----> | Reverse Proxy Handler  |  |
|  | path: /console/*     |  HTTP  | in desktop/wails host  |  |
|  +----------------------+        +------------------------+  |
|             ^                              |                 |
|             | JS bridge                    v                 |
|  +----------------------+        +------------------------+  |
|  | App binding          |        | Child Process Manager  |  |
|  | RestartApp()         |        | start/stop/wait/health |  |
|  +----------------------+        +------------------------+  |
+----------------------------------------------|---------------+
                                               |
                                               | spawn
                                               v
                           +----------------------------------+
                           | Child: `mistermorph console serve` |
                           | listen: 127.0.0.1:<random>       |
                           | base path: /console              |
                           | setup mode: enabled              |
                           +----------------------------------+
```

## 4. Startup Sequence

```text
Desktop main
  -> resolve console static assets dir
  -> reserve random loopback port
  -> spawn child: mistermorph console serve
       --console-listen 127.0.0.1:<port>
       --console-base-path /console
       --console-static-dir <web/console/dist>
       --console-setup-mode=true
  -> poll GET /health until ready
  -> start Wails window
  -> proxy requests to child process
```

If health check timeout is reached, app startup fails fast with stderr message.

## 5. Setup Mode Flow

```text
incomplete config
  -> console backend still starts (setup mode)
  -> business APIs return 409 setup_required
  -> frontend router redirects to /setup
  -> user submits setup form
  -> backend validates (including real LLM call when required)
  -> backend writes config atomically
  -> backend returns restart_required=true
  -> frontend calls Wails binding `window.go.main.App.RestartApp()`
```

## 6. Restart Flow

`App.RestartApp()` in desktop binding:

- spawns a new instance of current executable (`os.Executable()` + original args)
- then quits current Wails process

This is used after successful setup apply.

## 7. Paths and Configuration

- frontend static assets default: `web/console/dist`
- override static assets path:
  - `MISTERMORPH_DESKTOP_CONSOLE_STATIC_DIR=/abs/path/to/dist`
- `--config <path>` passed to desktop app is forwarded to child `console serve`.

## 8. Build and Run

Build console assets first:

```bash
pnpm --dir web/console build
```

Run desktop app:

```bash
go run -tags wailsdesktop ./desktop/wails
```

Build desktop binary:

```bash
go build -tags wailsdesktop -o ./bin/mistermorph-desktop ./desktop/wails
```

## 9. Security and Scope Notes

- Child process listens on loopback only (`127.0.0.1`).
- Setup API is intentionally exposed only in setup mode.
- Sensitive setup fields are redacted in setup apply response metadata.
- This is an MVP wrapper, not yet a full packaging/distribution pipeline.

## 10. Known Gaps

- No full desktop packaging workflow in this doc yet (installer/signing/notarization).
- No dedicated UI for backend startup failure details yet.
- CLI reuse is done through child process orchestration, not an extracted in-process console module.
