#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CALLER_DIR="$(pwd)"
APP_BINARY_NAME="${APP_BINARY_NAME:-mistermorph-desktop}"
DISPLAY_NAME="${DISPLAY_NAME:-MisterMorph}"
VERSION="${VERSION:-0.0.0}"
ARCH="${ARCH:-amd64}"
DESKTOP_BIN="${DESKTOP_BIN:-${ROOT_DIR}/dist/mistermorph-desktop}"
BACKEND_BIN="${BACKEND_BIN:-${ROOT_DIR}/dist/mistermorph}"
BUNDLED_BACKEND_NAME="${BUNDLED_BACKEND_NAME:-mistermorph}"
ICON_PNG="${ICON_PNG:-${ROOT_DIR}/desktop/wails/packaging/appicon.png}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist}"
WORK_ROOT="${WORK_ROOT:-${OUT_DIR}/appimage-work}"
APPIMAGE_NAME="${APPIMAGE_NAME:-mistermorph-desktop-linux-${ARCH}.AppImage}"
TARBALL_PATH="${TARBALL_PATH:-${OUT_DIR}/mistermorph-desktop-linux-${ARCH}.tar.gz}"

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

for command_name in curl find ldd readelf tar; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing required command: ${command_name}" >&2
    exit 1
  fi
done

require_file "${DESKTOP_BIN}"
require_file "${BACKEND_BIN}"
require_file "${ICON_PNG}"

DESKTOP_BIN="$(abspath "${DESKTOP_BIN}")"
BACKEND_BIN="$(abspath "${BACKEND_BIN}")"
ICON_PNG="$(abspath "${ICON_PNG}")"
OUT_DIR="$(abspath "${OUT_DIR}")"
WORK_ROOT="$(abspath "${WORK_ROOT}")"
TOOLS_DIR="${WORK_ROOT}/tools"
BUILD_DIR="${WORK_ROOT}/build"

case "${ARCH}" in
  amd64)
    APPIMAGE_ARCH="x86_64"
    ;;
  arm64)
    APPIMAGE_ARCH="aarch64"
    ;;
  *)
    echo "unsupported architecture for AppImage: ${ARCH}" >&2
    exit 1
    ;;
esac

APPDIR="${BUILD_DIR}/${APP_BINARY_NAME}-${APPIMAGE_ARCH}.AppDir"
LINUXDEPLOY="${TOOLS_DIR}/linuxdeploy-${APPIMAGE_ARCH}.AppImage"
APPRUN="${APPDIR}/AppRun"
GTK_PLUGIN="${TOOLS_DIR}/linuxdeploy-plugin-gtk.sh"
DESKTOP_FILE="${WORK_ROOT}/${APP_BINARY_NAME}.desktop"
OUTPUT_PATH="${OUT_DIR}/${APPIMAGE_NAME}"

rm -rf "${WORK_ROOT}" "${OUTPUT_PATH}" "${TARBALL_PATH}"
mkdir -p "${OUT_DIR}" "${TOOLS_DIR}" "${APPDIR}/usr/bin"

cp "${DESKTOP_BIN}" "${APPDIR}/usr/bin/${APP_BINARY_NAME}"
chmod +x "${APPDIR}/usr/bin/${APP_BINARY_NAME}"

cp "${ICON_PNG}" "${APPDIR}/.DirIcon"
ln -sf ".DirIcon" "${APPDIR}/${APP_BINARY_NAME}.png"

cat > "${DESKTOP_FILE}" <<EOF
[Desktop Entry]
Type=Application
Name=${DISPLAY_NAME}
Comment=MisterMorph Desktop
Exec=${APP_BINARY_NAME}
Icon=${APP_BINARY_NAME}
Categories=Development;Utility;
Terminal=false
EOF
cp "${DESKTOP_FILE}" "${APPDIR}/"

curl -fsSL \
  "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-${APPIMAGE_ARCH}.AppImage" \
  -o "${LINUXDEPLOY}"
curl -fsSL \
  "https://github.com/AppImage/AppImageKit/releases/download/continuous/AppRun-${APPIMAGE_ARCH}" \
  -o "${APPRUN}"
curl -fsSL \
  "https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh" \
  -o "${GTK_PLUGIN}"
chmod +x "${LINUXDEPLOY}" "${APPRUN}" "${GTK_PLUGIN}"

mapfile -t gtk_files < <(find /usr -type f \( \
  -name "WebKitWebProcess" -o \
  -name "WebKitNetworkProcess" -o \
  -name "libwebkit2gtkinjectedbundle.so" \
\))
if [[ "${#gtk_files[@]}" -eq 0 ]]; then
  echo "failed to locate WebKit helper files under /usr" >&2
  exit 1
fi
for gtk_file in "${gtk_files[@]}"; do
  target_dir="${APPDIR}$(dirname "${gtk_file}")"
  mkdir -p "${target_dir}"
  cp "${gtk_file}" "${target_dir}/"
done

gtk_version=""
ldd_output="$(ldd "${APPDIR}/usr/bin/${APP_BINARY_NAME}")"
case "${ldd_output}" in
  *"libgtk-x11-2.0.so"*)
    gtk_version="2"
    ;;
  *"libgtk-3.so"*)
    gtk_version="3"
    ;;
  *"libgtk-4.so"*)
    gtk_version="4"
    ;;
esac
if [[ -z "${gtk_version}" ]]; then
  echo "unable to determine GTK version from desktop binary" >&2
  exit 1
fi

no_strip=""
for candidate in \
  /usr/lib/libgtk-3.so.0 \
  /usr/lib64/libgtk-3.so.0 \
  /usr/lib/x86_64-linux-gnu/libgtk-3.so.0
do
  if [[ -f "${candidate}" ]] && readelf -S "${candidate}" | grep -q '\.relr\.dyn'; then
    no_strip="1"
    break
  fi
done

pushd "${BUILD_DIR}" >/dev/null
PATH="${TOOLS_DIR}:${PATH}" \
LINUXDEPLOY="${LINUXDEPLOY}" \
DEPLOY_GTK_VERSION="${gtk_version}" \
NO_STRIP="${no_strip}" \
  "${LINUXDEPLOY}" --appimage-extract-and-run --appdir "${APPDIR}" --plugin gtk

cp "${BACKEND_BIN}" "${APPDIR}/usr/bin/${BUNDLED_BACKEND_NAME}"
chmod +x "${APPDIR}/usr/bin/${BUNDLED_BACKEND_NAME}"

PATH="${TOOLS_DIR}:${PATH}" \
  "${LINUXDEPLOY}" --appimage-extract-and-run --appdir "${APPDIR}" --output appimage
popd >/dev/null

shopt -s nullglob
generated_appimages=("${BUILD_DIR}"/*.AppImage)
shopt -u nullglob
if [[ "${#generated_appimages[@]}" -ne 1 ]]; then
  echo "expected exactly one generated AppImage in ${BUILD_DIR}, found ${#generated_appimages[@]}" >&2
  exit 1
fi

mv "${generated_appimages[0]}" "${OUTPUT_PATH}"
# The updater tarball should contain the runnable AppDir bundle itself,
# not an AppImage nested inside another archive.
tar -C "${BUILD_DIR}" -czf "${TARBALL_PATH}" "$(basename "${APPDIR}")"
