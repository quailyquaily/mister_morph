#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

STAGED_CONFIG_PATH="${SCRIPT_DIR}/config.runtime.yaml"

WRANGLER_ENV="${WRANGLER_ENV:-}"
SKIP_NPM_INSTALL="${SKIP_NPM_INSTALL:-0}"
WRANGLER_CONFIG_PATH="${WRANGLER_CONFIG_PATH:-${SCRIPT_DIR}/wrangler.jsonc}"
MISTER_MORPH_CONFIG_PATH="${MISTER_MORPH_CONFIG_PATH:-${SCRIPT_DIR}/config.yaml}"

cleanup() {
  rm -f "${STAGED_CONFIG_PATH}"
}

trap cleanup EXIT

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Missing required command: ${cmd}" >&2
    exit 1
  fi
}

wrangler() {
  npx --yes wrangler --config "${WRANGLER_CONFIG_PATH}" "$@"
}

put_secret() {
  local name="$1"
  local value="$2"
  local -a cmd=(secret put "${name}")

  if [[ -n "${WRANGLER_ENV}" ]]; then
    cmd+=(--env "${WRANGLER_ENV}")
  fi

  printf '%s' "${value}" | wrangler "${cmd[@]}" >/dev/null
  echo "Set Cloudflare secret: ${name}"
}

add_optional_var() {
  local name="$1"
  local value="${!name:-}"
  if [[ -n "${value}" ]]; then
    DEPLOY_VAR_FLAGS+=(--var "${name}:${value}")
    echo "Set Cloudflare var from shell env: ${name}"
  fi
}

require_cmd npm
require_cmd docker

if [[ "${SKIP_NPM_INSTALL}" != "1" ]]; then
  npm install
fi

if ! wrangler whoami >/dev/null 2>&1; then
  echo "Wrangler is not authenticated. Run: npx wrangler login" >&2
  exit 1
fi

if [[ -z "${MISTER_MORPH_LLM_API_KEY:-}" ]]; then
  echo "MISTER_MORPH_LLM_API_KEY is required." >&2
  echo "Example: MISTER_MORPH_LLM_API_KEY=... ./deploy.sh" >&2
  exit 1
fi

if [[ ! -f "${MISTER_MORPH_CONFIG_PATH}" ]]; then
  echo "Config file not found: ${MISTER_MORPH_CONFIG_PATH}" >&2
  exit 1
fi

cp "${MISTER_MORPH_CONFIG_PATH}" "${STAGED_CONFIG_PATH}"
echo "Using config file: ${MISTER_MORPH_CONFIG_PATH}"

GENERATED_SERVER_TOKEN=0
if [[ -z "${MISTER_MORPH_SERVER_AUTH_TOKEN:-}" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    MISTER_MORPH_SERVER_AUTH_TOKEN="$(openssl rand -hex 24)"
  else
    MISTER_MORPH_SERVER_AUTH_TOKEN="$(date +%s%N | sha256sum | cut -c1-48)"
  fi
  GENERATED_SERVER_TOKEN=1
fi

put_secret "MISTER_MORPH_LLM_API_KEY" "${MISTER_MORPH_LLM_API_KEY}"
put_secret "MISTER_MORPH_SERVER_AUTH_TOKEN" "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

if [[ -n "${MISTER_MORPH_TELEGRAM_BOT_TOKEN:-}" ]]; then
  put_secret "MISTER_MORPH_TELEGRAM_BOT_TOKEN" "${MISTER_MORPH_TELEGRAM_BOT_TOKEN}"
fi

DEPLOY_VAR_FLAGS=()
OPTIONAL_VAR_KEYS=(
  MISTER_MORPH_LLM_PROVIDER
  MISTER_MORPH_LLM_ENDPOINT
  MISTER_MORPH_LLM_MODEL
  MISTER_MORPH_LOG_LEVEL
  MISTER_MORPH_TOOLS_BASH_ENABLED
  MISTER_MORPH_RUN_MODE
  MISTER_MORPH_WORKSPACE_DIR
  MISTER_MORPH_FILE_STATE_DIR
  MISTER_MORPH_FILE_CACHE_DIR
  MISTER_MORPH_SKIP_BOOTSTRAP_INSTALL
)
for key in "${OPTIONAL_VAR_KEYS[@]}"; do
  add_optional_var "${key}"
done

echo "Deploying Cloudflare Worker + Container..."
if [[ -n "${WRANGLER_ENV}" ]]; then
  wrangler deploy --env "${WRANGLER_ENV}" "${DEPLOY_VAR_FLAGS[@]}" "$@"
else
  wrangler deploy "${DEPLOY_VAR_FLAGS[@]}" "$@"
fi

echo
if [[ "${GENERATED_SERVER_TOKEN}" == "1" ]]; then
  echo "Generated MISTER_MORPH_SERVER_AUTH_TOKEN and stored it as a Cloudflare secret (value hidden)."
  echo "If you need to call protected endpoints manually, deploy with an explicit token:"
  echo 'MISTER_MORPH_SERVER_AUTH_TOKEN="<your-token>" ./deploy.sh'
  echo
fi

if [[ "${MISTER_MORPH_RUN_MODE:-serve}" == "telegram" ]]; then
  echo "Telegram mode deployed. Example status check (replace worker domain):"
  echo 'curl -H "Authorization: Bearer $MISTER_MORPH_SERVER_AUTH_TOKEN" https://<worker-domain>/_mistermorph/state'
else
  echo "Serve mode deployed. Example health check (replace worker domain):"
  echo 'curl -H "Authorization: Bearer $MISTER_MORPH_SERVER_AUTH_TOKEN" https://<worker-domain>/health'
fi
