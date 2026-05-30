#!/usr/bin/env sh
set -eu

MODE="${MISTER_MORPH_RUN_MODE:-telegram}"
LOG_LEVEL="${MISTER_MORPH_LOG_LEVEL:-info}"
CONFIG_PATH="${MISTER_MORPH_CONFIG_FILE:-/app/config.yaml}"
STATE_DIR="${MISTER_MORPH_FILE_STATE_DIR:-/tmp/mistermorph/state}"
CACHE_DIR="${MISTER_MORPH_FILE_CACHE_DIR:-/tmp/mistermorph/cache}"
INSTALL_MARKER="${STATE_DIR}/.mistermorph_installed"
SKIP_BOOTSTRAP="${MISTER_MORPH_SKIP_BOOTSTRAP_INSTALL:-0}"

echo "mistermorph_boot mode=${MODE} log_level=${LOG_LEVEL} config=${CONFIG_PATH} state_dir=${STATE_DIR} cache_dir=${CACHE_DIR} uid=$(id -u) gid=$(id -g)" >&2
mkdir -p "${CACHE_DIR}" || {
  echo "mistermorph_boot cache_dir_prepare_failed cache_dir=${CACHE_DIR}" >&2
  exit 1
}

need_bootstrap="0"
if [ "${SKIP_BOOTSTRAP}" != "1" ]; then
  if [ ! -f "${INSTALL_MARKER}" ]; then
    need_bootstrap="1"
  fi
  if [ ! -f "${STATE_DIR}/persona/identity.yaml" ] || [ ! -f "${STATE_DIR}/persona/soul.md" ] || [ ! -f "${STATE_DIR}/memory/index.md" ]; then
    need_bootstrap="1"
  fi
fi

if [ "${need_bootstrap}" = "1" ]; then
  echo "mistermorph_bootstrap start state_dir=${STATE_DIR}" >&2
  if ! mkdir -p "${STATE_DIR}"; then
    echo "mistermorph_bootstrap state_dir_prepare_failed state_dir=${STATE_DIR}; continue_without_bootstrap=1" >&2
  elif ! /usr/local/bin/mistermorph --config "${CONFIG_PATH}" install --yes; then
    code=$?
    echo "mistermorph_bootstrap install_failed exit_code=${code}; continue_without_bootstrap=1" >&2
  elif ! touch "${INSTALL_MARKER}"; then
    code=$?
    echo "mistermorph_bootstrap marker_write_failed marker=${INSTALL_MARKER} exit_code=${code}; continue_without_bootstrap=1" >&2
  else
    echo "mistermorph_bootstrap done state_dir=${STATE_DIR}" >&2
  fi
fi

case "${MODE}" in
  serve)
    exec /usr/local/bin/mistermorph --config "${CONFIG_PATH}" serve --log-level "${LOG_LEVEL}"
    ;;
  telegram)
    exec /usr/local/bin/mistermorph --config "${CONFIG_PATH}" telegram --log-level "${LOG_LEVEL}"
    ;;
  *)
    echo "Unsupported MISTER_MORPH_RUN_MODE: ${MODE} (expected: serve|telegram)" >&2
    exit 1
    ;;
esac
