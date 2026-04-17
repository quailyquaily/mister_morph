#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

target_goos() {
  if [[ -n "${GOOS:-}" ]]; then
    printf '%s\n' "${GOOS}"
    return
  fi
  go env GOOS
}

default_desktop_output() {
  case "$(target_goos)" in
    windows)
      printf '%s\n' "./bin/mistermorph-desktop.exe"
      ;;
    *)
      printf '%s\n' "./bin/mistermorph-desktop"
      ;;
  esac
}

default_backend_output() {
  case "$(target_goos)" in
    windows)
      printf '%s\n' "./bin/mistermorph.exe"
      ;;
    *)
      printf '%s\n' "./bin/mistermorph"
      ;;
  esac
}

DESKTOP_OUTPUT="$(default_desktop_output)"
BACKEND_OUTPUT="$(default_backend_output)"
BUILD_FRONTEND=1
BUILD_BACKEND=1
ENABLE_DEVTOOLS=1
HOST_OS="$(uname -s)"

usage() {
  cat <<'EOF'
Usage: scripts/build-desktop.sh [options]

Build the desktop app and its local backend.

Options:
  --release           Build desktop app without devtools
  --no-frontend       Skip `pnpm --dir web/console build`
  --no-backend        Skip `go build` for ./cmd/mistermorph
  --desktop-output P  Override desktop binary output path
  --backend-output P  Override backend binary output path
  -h, --help          Show this help

Default desktop build tags:
  Linux debug build:  wailsdesktop dev devtools
  Other debug build:  wailsdesktop production devtools
  Release build:      wailsdesktop production

Default outputs:
  desktop: ./bin/mistermorph-desktop (or .exe on Windows)
  backend: ./bin/mistermorph (or .exe on Windows)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release)
      ENABLE_DEVTOOLS=0
      shift
      ;;
    --no-frontend)
      BUILD_FRONTEND=0
      shift
      ;;
    --no-backend)
      BUILD_BACKEND=0
      shift
      ;;
    --desktop-output)
      DESKTOP_OUTPUT="${2:-}"
      shift 2
      ;;
    --backend-output)
      BACKEND_OUTPUT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${DESKTOP_OUTPUT}" || -z "${BACKEND_OUTPUT}" ]]; then
  echo "Output paths cannot be empty." >&2
  exit 1
fi

mkdir -p "$(dirname "${DESKTOP_OUTPUT}")" "$(dirname "${BACKEND_OUTPUT}")"

if [[ "${BUILD_FRONTEND}" == "1" ]]; then
  echo "==> Building web/console"
  pnpm --dir web/console build
fi

if [[ "${BUILD_BACKEND}" == "1" ]]; then
  echo "==> Building backend ${BACKEND_OUTPUT}"
  ./scripts/build-backend.sh --skip-frontend-build --output "${BACKEND_OUTPUT}"
fi

desktop_tags=(wailsdesktop)
if [[ "${ENABLE_DEVTOOLS}" == "1" ]]; then
  if [[ "${HOST_OS}" == "Linux" ]]; then
    # Wails v3 alpha currently has no linux+production+devtools implementation.
    desktop_tags+=(dev devtools)
  else
    desktop_tags+=(production devtools)
  fi
else
  desktop_tags+=(production)
fi

echo "==> Building desktop ${DESKTOP_OUTPUT}"
echo "    tags: ${desktop_tags[*]}"
go build -tags "${desktop_tags[*]}" -o "${DESKTOP_OUTPUT}" ./desktop/wails

echo
echo "Built:"
echo "  backend: ${BACKEND_OUTPUT}"
echo "  desktop: ${DESKTOP_OUTPUT}"
if [[ "${ENABLE_DEVTOOLS}" == "1" ]]; then
  echo "  inspector: Ctrl+Shift+F12"
fi
