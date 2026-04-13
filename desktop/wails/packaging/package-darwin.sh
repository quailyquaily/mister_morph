#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
APP_BUNDLE_NAME="${APP_BUNDLE_NAME:-mistermorph-desktop}"
APP_DISPLAY_NAME="${APP_DISPLAY_NAME:-MisterMorph}"
APP_EXECUTABLE_NAME="${APP_EXECUTABLE_NAME:-mistermorph-desktop}"
BUNDLE_ID="${BUNDLE_ID:-io.quaily.mistermorph}"
VERSION="${VERSION:-0.0.0}"
ARCH="${ARCH:-arm64}"
DESKTOP_BIN="${DESKTOP_BIN:-${ROOT_DIR}/dist/mistermorph-desktop}"
BACKEND_BIN="${BACKEND_BIN:-${ROOT_DIR}/dist/mistermorph}"
BUNDLED_BACKEND_NAME="${BUNDLED_BACKEND_NAME:-mistermorph}"
ICON_PNG="${ICON_PNG:-${ROOT_DIR}/desktop/wails/packaging/appicon.png}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist}"
APP_DIR="${OUT_DIR}/${APP_BUNDLE_NAME}.app"
DMG_PATH="${DMG_PATH:-${OUT_DIR}/mistermorph-desktop-darwin-${ARCH}.dmg}"
TARBALL_PATH="${TARBALL_PATH:-${OUT_DIR}/mistermorph-desktop-darwin-${ARCH}.tar.gz}"
ICONSET_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/mistermorph-iconset.XXXXXX")"
ICONSET_DIR="${ICONSET_ROOT}/mistermorph.iconset"
ICNS_PATH="${OUT_DIR}/mistermorph.icns"

cleanup() {
  rm -rf "${ICONSET_ROOT}"
}
trap cleanup EXIT

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    echo "missing required file: ${path}" >&2
    exit 1
  fi
}

for command_name in hdiutil iconutil sips tar; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing required command: ${command_name}" >&2
    exit 1
  fi
done

require_file "${DESKTOP_BIN}"
require_file "${BACKEND_BIN}"
require_file "${ICON_PNG}"

mkdir -p "${OUT_DIR}" "${APP_DIR}/Contents/MacOS" "${APP_DIR}/Contents/Resources"
rm -rf "${APP_DIR}" "${DMG_PATH}" "${TARBALL_PATH}" "${ICNS_PATH}"
mkdir -p "${APP_DIR}/Contents/MacOS" "${APP_DIR}/Contents/Resources"
mkdir -p "${ICONSET_DIR}"

render_icon() {
  local size="$1"
  local filename="$2"
  sips -z "${size}" "${size}" "${ICON_PNG}" --out "${ICONSET_DIR}/${filename}" >/dev/null
}

render_icon 16 icon_16x16.png
render_icon 32 icon_16x16@2x.png
render_icon 32 icon_32x32.png
render_icon 64 icon_32x32@2x.png
render_icon 128 icon_128x128.png
render_icon 256 icon_128x128@2x.png
render_icon 256 icon_256x256.png
render_icon 512 icon_256x256@2x.png
render_icon 512 icon_512x512.png
render_icon 1024 icon_512x512@2x.png
iconutil -c icns "${ICONSET_DIR}" -o "${ICNS_PATH}"

cp "${ICNS_PATH}" "${APP_DIR}/Contents/Resources/mistermorph.icns"
cp "${DESKTOP_BIN}" "${APP_DIR}/Contents/MacOS/${APP_EXECUTABLE_NAME}"
# Keep desktop and backend executable names lowercase and distinct.
cp "${BACKEND_BIN}" "${APP_DIR}/Contents/MacOS/${BUNDLED_BACKEND_NAME}"
chmod +x "${APP_DIR}/Contents/MacOS/${APP_EXECUTABLE_NAME}" "${APP_DIR}/Contents/MacOS/${BUNDLED_BACKEND_NAME}"

cat > "${APP_DIR}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>${APP_DISPLAY_NAME}</string>
  <key>CFBundleExecutable</key>
  <string>${APP_EXECUTABLE_NAME}</string>
  <key>CFBundleIconFile</key>
  <string>mistermorph.icns</string>
  <key>CFBundleIdentifier</key>
  <string>${BUNDLE_ID}</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>${APP_DISPLAY_NAME}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key>
  <string>10.15</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
EOF

CODESIGN_IDENTITY="${CODESIGN_IDENTITY:-}"
APPLE_ID="${APPLE_ID:-}"
APPLE_TEAM_ID="${APPLE_TEAM_ID:-}"
APPLE_APP_PASSWORD="${APPLE_APP_PASSWORD:-}"

if [[ -n "${CODESIGN_IDENTITY}" ]]; then
  echo "signing with identity: ${CODESIGN_IDENTITY}"
  codesign --deep --force --options runtime \
    --sign "${CODESIGN_IDENTITY}" \
    --timestamp \
    "${APP_DIR}/Contents/MacOS/${BUNDLED_BACKEND_NAME}"
  codesign --deep --force --options runtime \
    --sign "${CODESIGN_IDENTITY}" \
    --timestamp \
    "${APP_DIR}/Contents/MacOS/${APP_EXECUTABLE_NAME}"
  codesign --deep --force --options runtime \
    --sign "${CODESIGN_IDENTITY}" \
    --timestamp \
    "${APP_DIR}"
else
  echo "no CODESIGN_IDENTITY set; applying ad-hoc signature"
  codesign --deep --force --sign - "${APP_DIR}"
fi

if [[ -n "${CODESIGN_IDENTITY}" && -n "${APPLE_ID}" && -n "${APPLE_TEAM_ID}" && -n "${APPLE_APP_PASSWORD}" ]]; then
  echo "submitting app bundle for notarization..."
  xcrun notarytool submit "${APP_DIR}" \
    --apple-id "${APPLE_ID}" \
    --team-id "${APPLE_TEAM_ID}" \
    --password "${APPLE_APP_PASSWORD}" \
    --wait
  echo "stapling notarization ticket to app bundle..."
  xcrun stapler staple "${APP_DIR}"
fi

tar -C "${OUT_DIR}" -czf "${TARBALL_PATH}" "${APP_BUNDLE_NAME}.app"

hdiutil create \
  -volname "${APP_BUNDLE_NAME}" \
  -srcfolder "${APP_DIR}" \
  -ov \
  -format UDZO \
  "${DMG_PATH}" >/dev/null

if [[ -n "${CODESIGN_IDENTITY}" && -n "${APPLE_ID}" && -n "${APPLE_TEAM_ID}" && -n "${APPLE_APP_PASSWORD}" ]]; then
  echo "submitting DMG for notarization..."
  xcrun notarytool submit "${DMG_PATH}" \
    --apple-id "${APPLE_ID}" \
    --team-id "${APPLE_TEAM_ID}" \
    --password "${APPLE_APP_PASSWORD}" \
    --wait
  echo "stapling notarization ticket..."
  xcrun stapler staple "${DMG_PATH}"
fi
