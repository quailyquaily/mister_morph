# MisterMorph Desktop (Wails v3)

This directory contains the Wails desktop host for `mistermorph`.

## Current MVP wiring

- Runs a local backend `console serve` subprocess on a random loopback port.
- Prefers a sibling or configured backend binary and can auto-download one as fallback.
- If no bundled backend binary is found, desktop host tries to download a matching CLI release binary first.
- Proxies the Wails WebView traffic to the local console server at root path `/`.
- Exposes a Go binding `App.RestartApp()` for setup-complete restart.

## Dev prerequisites

- Go (same version as repository)
- Wails v3 desktop dependencies installed for your OS
- Built and staged console assets for the backend binary

On Ubuntu/Debian, install the Linux desktop build deps first:

```bash
sudo apt-get install -y libgtk-4-dev libwebkitgtk-6.0-dev
```

Wails v3 alpha.93 defaults to GTK4/WebKitGTK 6 on Linux, and this repository
uses that default for Linux desktop builds.

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
With default outputs, that script writes `./bin/MisterMorph` and `./bin/mistermorphc` on macOS, `./bin/MisterMorph.exe` and `./bin/mistermorphc.exe` on Windows, and `./bin/mistermorph-desktop` and `./bin/mistermorph` on Linux.

## Config file forwarding

If you start desktop app with `--config <path>`, that path is forwarded to the child `console serve` subprocess.
Use `--check-update` to run a one-shot update check and print JSON without opening the desktop window.

Update config:

```yaml
auto_update:
  enabled: true
```

When enabled, the desktop host checks the release `update.json` on startup and downloads the verified update package into the user cache. `--check-update` also uses this setting to decide whether to download. This step prepares the update package but does not replace the running app yet; Wails v3 alpha.93 does not expose an updater service package, so applying the update still needs a platform-specific install/relaunch step in this repository.

## Backend binary discovery/download

Backend binary candidate order:

1. `MISTERMORPH_DESKTOP_BACKEND_BIN`
2. `./bin/mistermorphc` on macOS/Windows, or `./bin/mistermorph` on Linux
3. sibling paths near desktop executable (`mistermorphc` before `mistermorph` on macOS/Windows; `mistermorph` on Linux; legacy `mistermorph-backend` still accepted)
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
- Linux: `mistermorph-desktop-linux-amd64.AppImage`, `mistermorph-desktop-linux-amd64.deb`, and `mistermorph-desktop-linux-amd64.tar.gz`
- Windows: `mistermorph-desktop-windows-amd64.zip`
- Update manifest: `update.json`

The release workflow generates `update.json` from the published release metadata and uploads it alongside the desktop assets.
The manifest prefers the macOS/Linux `tar.gz` assets and the Windows `.zip` asset.
The desktop runtime checks the stable latest-release URL:

```text
https://downloads.mistermorph.com/latest/update.json
```

The macOS and Windows desktop release packages bundle a sibling `mistermorphc` backend binary; the Linux package keeps the sibling backend as `mistermorph`.
The Linux deb package installs the app under `/opt/mistermorph`, adds the desktop entry under `/usr/share/applications`, and installs the app icon into the hicolor icon theme and `/usr/share/pixmaps`.
The Linux updater tarball is not an `.AppImage` wrapped in another archive; it contains the unpacked AppDir bundle so the updater asset is a real Linux app payload.
That bundled backend is built with `CGO_ENABLED=0` on purpose; keep it that way unless the CLI/backend grows an unavoidable native dependency.
The Windows release bundle includes both `MisterMorph.exe` and `mistermorphc.exe`; keep them in the same directory after unzip.
The Windows release workflow also generates a `.ico` and Windows `.syso` resource on the runner so the published desktop executable carries the app icon.
The macOS packaging script signs the `.app` bundle in two modes:

- with `CODESIGN_IDENTITY`: Developer ID signing, plus DMG notarization if Apple notarization credentials are also present
- without `CODESIGN_IDENTITY`: ad hoc signing for local builds or test-user distribution

The DMG opens as a fixed Finder window with a MisterMorph background, the app on the left, and an Applications shortcut on the right. Users install it by dragging the app onto Applications.

The tag release workflow requires these GitHub Actions secrets for the macOS DMG job:

- `APPLE_CERTIFICATE_BASE64`: base64-encoded `.p12` containing the Developer ID Application certificate and private key
- `APPLE_CERTIFICATE_PASSWORD`: password for that `.p12`
- `APPLE_CODESIGN_IDENTITY`: full codesign identity, for example `Developer ID Application: Example Inc (TEAMID1234)`
- `APPLE_ID`: Apple ID used for notarization
- `APPLE_TEAM_ID`: Apple Developer Team ID
- `APPLE_APP_PASSWORD`: app-specific password for notarization

Create the `.p12` from Keychain Access after installing the Developer ID Application certificate, then base64-encode it before adding it to GitHub Secrets. A tag like `v0.2.42` triggers the release workflow. The macOS job imports the certificate into a temporary keychain, signs the bundled backend, signs the desktop app, creates and signs the DMG, submits the DMG with `notarytool`, staples the notarization ticket to the DMG and `.app`, and uploads the release assets.

The ad hoc path is still available for local test packages. Test users may need to manually bypass Gatekeeper on first launch.

If you want the same Windows executable icon in a local Windows build, run:

```bash
./scripts/generate-desktop-windows-resources.sh
```
