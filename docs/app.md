# Desktop App

Mister Morph includes a desktop App that wraps the existing Console backend and UI.

## User Quick Start

Download a release asset from [GitHub Releases](https://github.com/quailyquaily/mistermorph/releases):

- macOS `arm64`: `mistermorph-desktop-darwin-arm64.dmg`
- Linux `amd64`: `mistermorph-desktop-linux-amd64.AppImage` or `mistermorph-desktop-linux-amd64.deb`
- Windows `amd64`: `mistermorph-desktop-windows-amd64.zip`

Then:

1. open the App
2. finish setup
3. use the local Console

You do not need to run `mistermorph console serve`.

## Current Shape

The desktop code lives in `desktop/wails` and builds with `wailsdesktop production`.

- the Wails process owns the native window and Go bindings
- a child process runs the bundled backend with `console serve`
- the App routes WebView traffic to that child process

The wrapper only handles lifecycle, process management, restart, and proxying.

## Architecture

```text
+--------------------------------------------------------------+
| Desktop App Process (Wails)                                  |
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
                           | Child: bundled backend `console serve` |
                           | listen: 127.0.0.1:<random>       |
                           | base path: /console              |
                           | allow-empty-password: enabled    |
                           +----------------------------------+
```

## Startup and Restart

Startup sequence:

```text
desktop main
  -> resolve backend binary path
  -> reserve random loopback port
  -> spawn child: bundled backend console serve
  -> poll GET /health until ready
  -> open native window
  -> proxy requests to the child process
```

First run:

```text
incomplete config
  -> console backend starts with allow-empty-password
  -> frontend routes to /setup
  -> user saves agent settings + identity/soul
  -> frontend calls App.RestartApp()
```

`App.RestartApp()` starts a new copy of the desktop executable, then quits the old one.

## Paths and Configuration

- Console assets are embedded in the bundled backend by default.
- You can override static assets with:
  - `console.static_dir`
  - `--console-static-dir /abs/path/to/dist`
- `--config <path>` passed to the desktop App is forwarded to the child `console serve` process.
- `--check-update` checks the desktop release manifest and prints JSON, then exits.

Enable update checks in config:

```yaml
auto_update:
  enabled: true
```

With this enabled, desktop startup checks `update.json` and downloads a verified update package into the user cache. `--check-update` also uses this setting to decide whether to download. It does not replace the running app yet; Wails v3 alpha.93 does not expose an updater service package, so applying the update still needs a platform-specific install/relaunch step in this repository.

## Local Build and Run

On Ubuntu or Debian, install desktop build dependencies first:

```bash
sudo apt-get install -y libgtk-4-dev libwebkitgtk-6.0-dev
```

Wails v3 alpha.93 defaults to GTK4/WebKitGTK 6 on Linux, and this repository
uses that default for Linux desktop builds.

Build the backend binary:

```bash
./scripts/build-backend.sh --output ./bin/mistermorph
```

Build a local desktop release binary:

```bash
./scripts/build-desktop.sh --release
```

Run from source:

```bash
go run -tags 'wailsdesktop production' ./desktop/wails
```

Build only the desktop wrapper:

```bash
go build -tags 'wailsdesktop production' -o ./bin/mistermorph-desktop ./desktop/wails
```

For local debug builds with DevTools, use:

```bash
./scripts/build-desktop.sh
```

## Release Packaging

Tagged releases currently publish:

- macOS `arm64`: `mistermorph-desktop-darwin-arm64.dmg`
- Linux `amd64`: `mistermorph-desktop-linux-amd64.AppImage`, `mistermorph-desktop-linux-amd64.deb`, and `mistermorph-desktop-linux-amd64.tar.gz`
- Windows `amd64`: `mistermorph-desktop-windows-amd64.zip`

The package includes a sibling backend binary, so the wrapper can start `console serve` locally without a first-run download. macOS and Windows name that backend `mistermorphc`; Linux keeps it as `mistermorph`.

The Linux deb package installs the app under `/opt/mistermorph`, adds the desktop entry under `/usr/share/applications`, and installs the app icon into the hicolor icon theme and `/usr/share/pixmaps`.

That backend is built with `CGO_ENABLED=0` on purpose. Keep it that way unless you have a packaging plan to change it.

The macOS tag release job requires Apple signing secrets in GitHub Actions. It imports the Developer ID Application certificate into a temporary keychain, signs the app bundle, signs the DMG, submits the DMG through `notarytool`, staples the ticket to the DMG and app bundle, then uploads the release assets.

## Known Gaps

- Local macOS test packages can still use ad hoc signing; those builds may require testers to manually bypass Gatekeeper on first launch.
- Windows ships as a zip bundle, not an installer.
- No dedicated UI yet for backend startup failures.
- The wrapper still reuses the CLI backend through child-process orchestration rather than an in-process console module.
