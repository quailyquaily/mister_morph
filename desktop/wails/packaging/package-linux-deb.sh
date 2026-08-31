#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CALLER_DIR="$(pwd)"
PACKAGE_NAME="${PACKAGE_NAME:-mistermorph-desktop}"
APP_BINARY_NAME="${APP_BINARY_NAME:-mistermorph-desktop}"
DISPLAY_NAME="${DISPLAY_NAME:-MisterMorph}"
APPLICATION_ID="${APPLICATION_ID:-com.mistermorph}"
VERSION="${VERSION:-0.0.0}"
ARCH="${ARCH:-amd64}"
DESKTOP_BIN="${DESKTOP_BIN:-${ROOT_DIR}/dist/mistermorph-desktop}"
BACKEND_BIN="${BACKEND_BIN:-${ROOT_DIR}/dist/mistermorph}"
BUNDLED_BACKEND_NAME="${BUNDLED_BACKEND_NAME:-mistermorph}"
ICON_PNG="${ICON_PNG:-${ROOT_DIR}/desktop/wails/packaging/appicon.png}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist}"
WORK_ROOT="${WORK_ROOT:-${OUT_DIR}/deb-work}"
DEB_NAME="${DEB_NAME:-mistermorph-desktop-linux-${ARCH}.deb}"
INSTALL_DIR="${INSTALL_DIR:-/opt/mistermorph}"
ICON_THEME_SIZE="${ICON_THEME_SIZE:-512x512}"
DESKTOP_FILE_NAME="${DESKTOP_FILE_NAME:-${APPLICATION_ID}.desktop}"

abspath() {
  local path="$1"
  if [[ "${path}" == /* ]]; then
    printf '%s\n' "${path}"
    return
  fi
  printf '%s\n' "${CALLER_DIR}/${path}"
}

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    echo "missing required file: ${path}" >&2
    exit 1
  fi
}

for command_name in dpkg-deb find install ln; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing required command: ${command_name}" >&2
    exit 1
  fi
done

case "${ARCH}" in
  amd64)
    DEB_ARCH="amd64"
    ;;
  arm64)
    DEB_ARCH="arm64"
    ;;
  *)
    echo "unsupported architecture for deb: ${ARCH}" >&2
    exit 1
    ;;
esac

if [[ ! "${VERSION}" =~ ^[0-9][A-Za-z0-9.+:~-]*$ ]]; then
  echo "invalid Debian package version: ${VERSION}" >&2
  exit 1
fi

require_file "${DESKTOP_BIN}"
require_file "${BACKEND_BIN}"
require_file "${ICON_PNG}"

DESKTOP_BIN="$(abspath "${DESKTOP_BIN}")"
BACKEND_BIN="$(abspath "${BACKEND_BIN}")"
ICON_PNG="$(abspath "${ICON_PNG}")"
OUT_DIR="$(abspath "${OUT_DIR}")"
WORK_ROOT="$(abspath "${WORK_ROOT}")"
OUTPUT_PATH="${OUT_DIR}/${DEB_NAME}"
PACKAGE_ROOT="${WORK_ROOT}/${PACKAGE_NAME}"

rm -rf "${WORK_ROOT}" "${OUTPUT_PATH}"
mkdir -p \
  "${OUT_DIR}" \
  "${PACKAGE_ROOT}/DEBIAN" \
  "${PACKAGE_ROOT}${INSTALL_DIR}" \
  "${PACKAGE_ROOT}/usr/bin" \
  "${PACKAGE_ROOT}/usr/share/applications" \
  "${PACKAGE_ROOT}/usr/share/doc/${PACKAGE_NAME}" \
  "${PACKAGE_ROOT}/usr/share/icons/hicolor/${ICON_THEME_SIZE}/apps" \
  "${PACKAGE_ROOT}/usr/share/pixmaps"

install -m 0755 "${DESKTOP_BIN}" "${PACKAGE_ROOT}${INSTALL_DIR}/${APP_BINARY_NAME}"
install -m 0755 "${BACKEND_BIN}" "${PACKAGE_ROOT}${INSTALL_DIR}/${BUNDLED_BACKEND_NAME}"
install -m 0644 "${ICON_PNG}" "${PACKAGE_ROOT}/usr/share/icons/hicolor/${ICON_THEME_SIZE}/apps/${APPLICATION_ID}.png"
install -m 0644 "${ICON_PNG}" "${PACKAGE_ROOT}/usr/share/pixmaps/${APPLICATION_ID}.png"
install -m 0644 "${ROOT_DIR}/LICENSE" "${PACKAGE_ROOT}/usr/share/doc/${PACKAGE_NAME}/copyright"
ln -s "${INSTALL_DIR}/${APP_BINARY_NAME}" "${PACKAGE_ROOT}/usr/bin/${APP_BINARY_NAME}"

cat > "${PACKAGE_ROOT}/usr/share/applications/${DESKTOP_FILE_NAME}" <<EOF
[Desktop Entry]
Type=Application
Name=${DISPLAY_NAME}
Comment=MisterMorph Desktop
Exec=${INSTALL_DIR}/${APP_BINARY_NAME}
Icon=${APPLICATION_ID}
Categories=Development;Utility;
Terminal=false
StartupWMClass=${APPLICATION_ID}
EOF

cat > "${PACKAGE_ROOT}/DEBIAN/control" <<EOF
Package: ${PACKAGE_NAME}
Version: ${VERSION}
Section: utils
Priority: optional
Architecture: ${DEB_ARCH}
Maintainer: MisterMorph Maintainers <noreply@quaily.com>
Depends: libgtk-4-1, libwebkitgtk-6.0-4, ca-certificates
Description: MisterMorph Desktop
 Desktop application for MisterMorph.
EOF

cat > "${PACKAGE_ROOT}/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi
EOF

cat > "${PACKAGE_ROOT}/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi
EOF

chmod 0644 "${PACKAGE_ROOT}/DEBIAN/control" "${PACKAGE_ROOT}/usr/share/applications/${DESKTOP_FILE_NAME}"
chmod 0755 "${PACKAGE_ROOT}/DEBIAN/postinst" "${PACKAGE_ROOT}/DEBIAN/postrm"
find "${PACKAGE_ROOT}" -type d -exec chmod 0755 {} +
dpkg-deb --build --root-owner-group "${PACKAGE_ROOT}" "${OUTPUT_PATH}"
