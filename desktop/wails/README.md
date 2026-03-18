# MisterMorph Desktop (Wails v2)

This directory contains the Wails desktop host for `mistermorph`.

## Current MVP wiring

- Runs a local `mistermorph console serve` subprocess on a random loopback port.
- The subprocess is the same desktop binary in an internal mode (`--desktop-console-serve`).
- If no `mistermorph` backend binary is found, desktop host tries to download a matching release binary first.
- Proxies the Wails WebView traffic to the local console server at root path `/`.
- Exposes a Go binding `App.RestartApp()` for setup-complete restart.

## Dev prerequisites

- Go (same version as repository)
- Wails v2 dependencies installed for your OS
- Built console assets under `web/console/dist`

On Ubuntu/Debian with WebKitGTK 4.1 (for example Ubuntu 24.04), install the Linux desktop build deps first:

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
```

Build console assets first:

```bash
pnpm --dir web/console build
```

Run desktop app from source:

```bash
go run -tags 'wailsdesktop production webkit2_41' ./desktop/wails
```

Build desktop binary:

```bash
go build -tags 'wailsdesktop production webkit2_41' -o ./bin/mistermorph-desktop ./desktop/wails
```

## Asset path override

If the desktop host cannot find console assets automatically, set:

```bash
export MISTERMORPH_DESKTOP_CONSOLE_STATIC_DIR=/absolute/path/to/web/console/dist
```

## Config file forwarding

If you start desktop app with `--config <path>`, that path is forwarded to the internal `console serve` subprocess.

## Backend binary discovery/download

Backend binary candidate order:

1. `MISTERMORPH_DESKTOP_BACKEND_BIN`
2. `./bin/mistermorph` (or `.exe` on Windows)
3. sibling paths near desktop executable
4. `PATH` lookup (`mistermorph`)
5. download from GitHub releases (enabled by default)

Optional envs:

- `MISTERMORPH_DESKTOP_BACKEND_AUTO_DOWNLOAD=true|false` (default `true`)
- `MISTERMORPH_DESKTOP_BACKEND_VERSION=latest|vX.Y.Z` (default `latest`)
- `MISTERMORPH_DESKTOP_BACKEND_CACHE_DIR=/abs/path` (default: user cache dir under `mistermorph/desktop/backend`)
- `MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY=ondemand|always|never` (Linux only, default `ondemand`)
