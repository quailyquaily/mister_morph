# MisterMorph Desktop (Wails v2)

This directory contains the Wails desktop host for `mistermorph`.

## Current MVP wiring

- Runs a local `mistermorph console serve` subprocess on a random loopback port.
- Enables setup mode (`--console-setup-mode=true`) so first-launch bootstrap works.
- Proxies the Wails WebView traffic to the local console server.
- Exposes a Go binding `App.RestartApp()` for setup-complete restart.

## Dev prerequisites

- Go (same version as repository)
- Wails v2 dependencies installed for your OS
- Built console assets under `web/console/dist`

Build console assets first:

```bash
pnpm --dir web/console build
```

Run desktop app from source:

```bash
go run -tags wailsdesktop ./desktop/wails
```

Build desktop binary:

```bash
go build -tags wailsdesktop -o ./bin/mistermorph-desktop ./desktop/wails
```

## Asset path override

If the desktop host cannot find console assets automatically, set:

```bash
export MISTERMORPH_DESKTOP_CONSOLE_STATIC_DIR=/absolute/path/to/web/console/dist
```

## Config file forwarding

If you start desktop app with `--config <path>`, that path is forwarded to the internal `console serve` subprocess.
