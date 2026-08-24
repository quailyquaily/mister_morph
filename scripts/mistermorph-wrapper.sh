#!/usr/bin/env bash
set -euo pipefail

# Edit these defaults for this Morph instance. Each value may also be
# overridden with an environment variable of the same name.
MORPH_CLI_PATH="${MORPH_CLI_PATH:-mistermorph}"
MORPH_FILE_STATE_DIR="${MORPH_FILE_STATE_DIR:-${HOME}/.morph}"
# The install command creates config.yaml inside MORPH_FILE_STATE_DIR.
MORPH_CONFIG_PATH="${MORPH_CONFIG_PATH:-${MORPH_FILE_STATE_DIR}/config.yaml}"

export MISTER_MORPH_FILE_STATE_DIR="${MORPH_FILE_STATE_DIR}"

if [[ $# -eq 0 ]]; then
  set -- chat
fi

if [[ "$1" == "install" ]]; then
  exec "${MORPH_CLI_PATH}" "$@"
fi

exec "${MORPH_CLI_PATH}" --config "${MORPH_CONFIG_PATH}" "$@"
