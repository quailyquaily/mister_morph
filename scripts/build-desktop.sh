#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

DESKTOP_OUTPUT="./bin/mistermorph-desktop"
BACKEND_OUTPUT="./bin/mistermorph"
BUILD_FRONTEND=1
BUILD_BACKEND=1
ENABLE_DEVTOOLS=1

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
  wailsdesktop production [webkit2_41 when available] devtools
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

detect_webkit_tag() {
  if command -v pkg-config >/dev/null 2>&1; then
    if pkg-config --exists webkit2gtk-4.1; then
      printf '%s\n' "webkit2_41"
      return 0
    fi
    if pkg-config --exists webkit2gtk-4.0; then
      printf '%s\n' ""
      return 0
    fi
  fi
  echo "Missing WebKitGTK development package. Need pkg-config entry for webkit2gtk-4.1 or webkit2gtk-4.0." >&2
  exit 1
}

mkdir -p "$(dirname "${DESKTOP_OUTPUT}")" "$(dirname "${BACKEND_OUTPUT}")"

if [[ "${BUILD_FRONTEND}" == "1" ]]; then
  echo "==> Building web/console"
  pnpm --dir web/console build
fi

if [[ "${BUILD_BACKEND}" == "1" ]]; then
  echo "==> Building backend ${BACKEND_OUTPUT}"
  go build -o "${BACKEND_OUTPUT}" ./cmd/mistermorph
fi

webkit_tag="$(detect_webkit_tag)"
desktop_tags=(wailsdesktop production)
if [[ -n "${webkit_tag}" ]]; then
  desktop_tags+=("${webkit_tag}")
fi
if [[ "${ENABLE_DEVTOOLS}" == "1" ]]; then
  desktop_tags+=("devtools")
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
