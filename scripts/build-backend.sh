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

OUTPUT="$(default_backend_output)"
EMBED_FRONTEND=1
BUILD_FRONTEND=1

usage() {
  cat <<'EOF'
Usage: scripts/build-backend.sh [options]

Build the backend binary used by CLI and desktop packaging.

Options:
  --output PATH           Override backend binary output path
  --no-embed-frontend     Build backend without embedded Console SPA assets
  --skip-frontend-build   Reuse existing web/console/dist instead of rebuilding it
  -h, --help              Show this help

Notes:
  - Default behavior embeds the Console frontend into the backend binary.
  - Default output path is ./bin/mistermorph (or ./bin/mistermorph.exe on Windows).
  - --no-embed-frontend adds the Go build tag: noembedconsole
  - When embedding the frontend, this script stages web/console/dist into
    cmd/mistermorph/consolecmd/static before go build.
  - CGO defaults to disabled for this backend build; override with CGO_ENABLED=1
    if you intentionally need a cgo-enabled backend binary.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      OUTPUT="${2:-}"
      shift 2
      ;;
    --no-embed-frontend)
      EMBED_FRONTEND=0
      shift
      ;;
    --skip-frontend-build)
      BUILD_FRONTEND=0
      shift
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

if [[ -z "${OUTPUT}" ]]; then
  echo "Output path cannot be empty." >&2
  exit 1
fi

mkdir -p "$(dirname "${OUTPUT}")"

build_tags=()
if [[ "${EMBED_FRONTEND}" == "1" ]]; then
  if [[ "${BUILD_FRONTEND}" == "1" ]]; then
    echo "==> Building web/console"
    pnpm --dir web/console build
  fi

  echo "==> Staging console assets"
  ./scripts/stage-console-assets.sh
else
  build_tags+=(noembedconsole)
fi

echo "==> Building backend ${OUTPUT}"
if [[ ${#build_tags[@]} -gt 0 ]]; then
  echo "    tags: ${build_tags[*]}"
  CGO_ENABLED="${CGO_ENABLED:-0}" go build -tags "${build_tags[*]}" -o "${OUTPUT}" ./cmd/mistermorph
else
  CGO_ENABLED="${CGO_ENABLED:-0}" go build -o "${OUTPUT}" ./cmd/mistermorph
fi

echo
echo "Built backend: ${OUTPUT}"
if [[ "${EMBED_FRONTEND}" == "1" ]]; then
  echo "Console frontend: embedded"
else
  echo "Console frontend: external (--console-static-dir required for SPA serving)"
fi
