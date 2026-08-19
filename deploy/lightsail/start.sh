#!/usr/bin/env sh
set -eu

MODE="${MISTER_MORPH_RUN_MODE:-telegram}"
LOG_LEVEL="${MISTER_MORPH_LOG_LEVEL:-info}"
CONFIG_PATH="${MISTER_MORPH_CONFIG_FILE:-/app/config.yaml}"
STATE_DIR="${MISTER_MORPH_FILE_STATE_DIR:-/tmp/mistermorph/state}"
CACHE_DIR="${MISTER_MORPH_FILE_CACHE_DIR:-/tmp/mistermorph/cache}"
INSTALL_MARKER="${STATE_DIR}/.mistermorph_installed"
SKIP_BOOTSTRAP="${MISTER_MORPH_SKIP_BOOTSTRAP_INSTALL:-0}"

S3_STATE_BUCKET="${MISTER_MORPH_S3_STATE_BUCKET:-}"
S3_STATE_PREFIX="${MISTER_MORPH_S3_STATE_PREFIX:-default}"
S3_STATE_REGION="${MISTER_MORPH_S3_STATE_REGION:-${AWS_REGION:-}}"
S3_SYNC_INTERVAL="${MISTER_MORPH_S3_SYNC_INTERVAL:-30}"
S3_SYNC_DELETE="${MISTER_MORPH_S3_SYNC_DELETE:-1}"

S3_STATE_PREFIX="$(printf '%s' "${S3_STATE_PREFIX}" | sed 's#^/*##;s#/*$##')"
S3_ENABLED="0"
S3_STATE_URI=""
if [ -n "${S3_STATE_BUCKET}" ]; then
  S3_ENABLED="1"
  if [ -n "${S3_STATE_PREFIX}" ]; then
    S3_STATE_URI="s3://${S3_STATE_BUCKET}/${S3_STATE_PREFIX}/state"
  else
    S3_STATE_URI="s3://${S3_STATE_BUCKET}/state"
  fi
fi

case "${S3_SYNC_INTERVAL}" in
  ''|*[!0-9]*) S3_SYNC_INTERVAL="30" ;;
esac

S3_SYNC_PID="0"
APP_PID="0"

sync_state_from_s3() {
  if [ "${S3_ENABLED}" != "1" ]; then
    return 0
  fi
  mkdir -p "${STATE_DIR}" || return 1
  if [ -n "${S3_STATE_REGION}" ]; then
    aws s3 sync "${S3_STATE_URI}/" "${STATE_DIR}/" --region "${S3_STATE_REGION}" --only-show-errors
  else
    aws s3 sync "${S3_STATE_URI}/" "${STATE_DIR}/" --only-show-errors
  fi
}

sync_state_to_s3() {
  if [ "${S3_ENABLED}" != "1" ]; then
    return 0
  fi
  mkdir -p "${STATE_DIR}" || return 1
  if [ -n "${S3_STATE_REGION}" ]; then
    if [ "${S3_SYNC_DELETE}" = "1" ]; then
      aws s3 sync "${STATE_DIR}/" "${S3_STATE_URI}/" --region "${S3_STATE_REGION}" --delete --only-show-errors
    else
      aws s3 sync "${STATE_DIR}/" "${S3_STATE_URI}/" --region "${S3_STATE_REGION}" --only-show-errors
    fi
  else
    if [ "${S3_SYNC_DELETE}" = "1" ]; then
      aws s3 sync "${STATE_DIR}/" "${S3_STATE_URI}/" --delete --only-show-errors
    else
      aws s3 sync "${STATE_DIR}/" "${S3_STATE_URI}/" --only-show-errors
    fi
  fi
}

start_s3_sync_loop() {
  if [ "${S3_ENABLED}" != "1" ]; then
    return 0
  fi
  if [ "${S3_SYNC_INTERVAL}" -le 0 ]; then
    return 0
  fi
  (
    while true; do
      sleep "${S3_SYNC_INTERVAL}"
      if ! sync_state_to_s3; then
        echo "mistermorph_state_sync_up_failed uri=${S3_STATE_URI}" >&2
      fi
    done
  ) &
  S3_SYNC_PID="$!"
}

forward_term() {
  if [ "${APP_PID}" -gt 0 ] 2>/dev/null; then
    kill -TERM "${APP_PID}" 2>/dev/null || true
  fi
}

cleanup_on_exit() {
  if [ "${S3_SYNC_PID}" -gt 0 ] 2>/dev/null; then
    kill "${S3_SYNC_PID}" 2>/dev/null || true
  fi
  if [ "${S3_ENABLED}" = "1" ]; then
    if ! sync_state_to_s3; then
      echo "mistermorph_state_sync_final_failed uri=${S3_STATE_URI}" >&2
    else
      echo "mistermorph_state_sync_final_done uri=${S3_STATE_URI}" >&2
    fi
  fi
}

trap forward_term INT TERM
trap cleanup_on_exit EXIT

echo "mistermorph_boot mode=${MODE} log_level=${LOG_LEVEL} config=${CONFIG_PATH} state_dir=${STATE_DIR} cache_dir=${CACHE_DIR} s3_state_enabled=${S3_ENABLED} uid=$(id -u) gid=$(id -g)" >&2
mkdir -p "${CACHE_DIR}" || {
  echo "mistermorph_boot cache_dir_prepare_failed cache_dir=${CACHE_DIR}" >&2
  exit 1
}

if [ "${S3_ENABLED}" = "1" ]; then
  if ! command -v aws >/dev/null 2>&1; then
    echo "mistermorph_state_sync_error aws_cli_not_found" >&2
    exit 1
  fi
  echo "mistermorph_state_sync_enabled uri=${S3_STATE_URI} interval_seconds=${S3_SYNC_INTERVAL} delete=${S3_SYNC_DELETE}" >&2
  if ! sync_state_from_s3; then
    echo "mistermorph_state_sync_down_failed uri=${S3_STATE_URI}; continue_without_restore=1" >&2
  else
    echo "mistermorph_state_sync_down_done uri=${S3_STATE_URI}" >&2
  fi
  start_s3_sync_loop
fi

need_bootstrap="0"
if [ "${SKIP_BOOTSTRAP}" != "1" ]; then
  if [ ! -f "${INSTALL_MARKER}" ]; then
    need_bootstrap="1"
  fi
  if [ ! -f "${STATE_DIR}/persona/identity.yaml" ] || [ ! -f "${STATE_DIR}/persona/soul.md" ]; then
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
    /usr/local/bin/mistermorph --config "${CONFIG_PATH}" serve --log-level "${LOG_LEVEL}" &
    APP_PID="$!"
    ;;
  telegram)
    /usr/local/bin/mistermorph --config "${CONFIG_PATH}" telegram --log-level "${LOG_LEVEL}" &
    APP_PID="$!"
    ;;
  *)
    echo "Unsupported MISTER_MORPH_RUN_MODE: ${MODE} (expected: serve|telegram)" >&2
    exit 1
    ;;
esac

if wait "${APP_PID}"; then
  app_code=0
else
  app_code=$?
fi
exit "${app_code}"
