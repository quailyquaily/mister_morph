# MisterMorph Desktop (Wails v3)

This directory contains the Wails desktop host for `mistermorph`.

## Current MVP wiring

- Runs a local `mistermorph console serve` subprocess on a random loopback port.
- Prefers a sibling or configured `mistermorph` backend binary and can auto-download one as fallback.
- If no `mistermorph` backend binary is found, desktop host tries to download a matching release binary first.
- Proxies the Wails WebView traffic to the local console server at root path `/`.
- Exposes a Go binding `App.RestartApp()` for setup-complete restart.

## Dev prerequisites

- Go (same version as repository)
- Wails v3 desktop dependencies installed for your OS
- Built and staged console assets for the backend binary

On Ubuntu/Debian, install the Linux desktop build deps first:

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
```

Build console assets first:

```bash
./scripts/build-backend.sh --output ./bin/mistermorph
```

If you use the default output path, the backend binary is `./bin/mistermorph`
(or `./bin/mistermorph.exe` on Windows).

To build a backend binary without embedding the Console SPA, use:

```bash
./scripts/build-backend.sh --no-embed-frontend --output ./bin/mistermorph
```

The bundled `mistermorph` backend should stay `CGO_ENABLED=0`.
The desktop shell itself can still use cgo through Wails/WebKit, but the child backend is more stable as a pure-Go binary, especially inside AppImage where inherited loader state can otherwise trigger early native crashes.

If a future Go dependency requires cgo, do not immediately let that leak into the bundled backend binary.
Handle it in this order:

1. Keep `./cmd/mistermorph` pure-Go if possible, and isolate the cgo dependency behind build tags, an optional package, or a separate code path that the desktop backend does not import.
2. If the feature truly needs native code, prefer a separate helper binary or a desktop-only component over making the main backend child process depend on cgo.
3. Only let the bundled backend require cgo if there is no practical isolation strategy left. In that case, update the desktop packaging docs and CI together, and re-verify AppImage/DMG/Windows bundle startup before merging.

The working rule is: the desktop shell may depend on cgo; the bundled `mistermorph console serve` backend should remain `CGO_ENABLED=0` unless there is a deliberate packaging plan for changing that constraint.

Run desktop app from source:

```bash
go run -tags 'wailsdesktop production' ./desktop/wails
```

Build desktop binary:

```bash
go build -tags 'wailsdesktop production' -o ./bin/mistermorph-desktop ./desktop/wails
```

For local Linux builds with DevTools enabled, use `scripts/build-desktop.sh`. It automatically switches Linux debug builds to `wailsdesktop dev devtools`, because Wails v3 alpha does not currently support `linux + production + devtools`.
With default outputs, that script writes `./bin/mistermorph-desktop` and `./bin/mistermorph`
(Windows: `mistermorph-desktop.exe` and `mistermorph.exe`).

## Config file forwarding

If you start desktop app with `--config <path>`, that path is forwarded to the child `console serve` subprocess.

## Backend binary discovery/download

Backend binary candidate order:

1. `MISTERMORPH_DESKTOP_BACKEND_BIN`
2. `./bin/mistermorph` (or `.exe` on Windows)
3. sibling paths near desktop executable (`mistermorph`; legacy `mistermorph-backend` still accepted)
4. `PATH` lookup (`mistermorph`)
5. download from GitHub releases (enabled by default)

Optional envs:

- `MISTERMORPH_DESKTOP_BACKEND_AUTO_DOWNLOAD=true|false` (default `true`)
- `MISTERMORPH_DESKTOP_BACKEND_VERSION=latest|vX.Y.Z` (default `latest`)
- `MISTERMORPH_DESKTOP_BACKEND_CACHE_DIR=/abs/path` (default: user cache dir under `mistermorph/desktop/backend`)
- `MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY=ondemand|always|never` (Linux only, default `ondemand`)

## Release packaging

Tag releases now build desktop release assets in GitHub Actions:

- macOS: `mistermorph-desktop-darwin-arm64.dmg` and `mistermorph-desktop-darwin-arm64.tar.gz`
- Linux: `mistermorph-desktop-linux-amd64.AppImage` and `mistermorph-desktop-linux-amd64.tar.gz`
- Windows: `mistermorph-desktop-windows-amd64.zip`
- Wails updater manifest: `update.json`

The release workflow generates `update.json` from the published GitHub release metadata and uploads it alongside the desktop assets.
For Wails updater compatibility, `update.json` prefers the macOS/Linux `tar.gz` assets and the Windows `.zip` asset.
That gives the Wails v3 updater a stable latest-release URL:

```text
https://github.com/quailyquaily/mistermorph/releases/latest/download/update.json
```

The desktop release packages bundle a sibling `mistermorph` backend binary so the packaged app can launch `console serve` without a first-run download.
The Linux updater tarball is not an `.AppImage` wrapped in another archive; it contains the unpacked AppDir bundle so the updater asset is a real Linux app payload.
That bundled backend is built with `CGO_ENABLED=0` on purpose; keep it that way unless the CLI/backend grows an unavoidable native dependency.
The Windows release bundle now includes both `mistermorph-desktop.exe` and `mistermorph.exe`; keep them in the same directory after unzip.
The Windows release workflow also generates a `.ico` and Windows `.syso` resource on the runner so the published desktop executable carries the app icon.

If you want the same Windows executable icon in a local Windows build, run:

```bash
./scripts/generate-desktop-windows-resources.sh
```
