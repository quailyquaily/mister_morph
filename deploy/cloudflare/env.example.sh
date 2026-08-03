#!/usr/bin/env bash
# Copy this file to ./env.sh and fill in real values.
# Do not commit your env.sh with real secrets.

# Cloudflare auth for Wrangler (required in CI; optional locally if you already ran `wrangler login`).
export CLOUDFLARE_ACCOUNT_ID="your-cloudflare-account-id"
export CLOUDFLARE_API_TOKEN="your-cloudflare-api-token"

# Required: LLM key used by mistermorph.
export MISTER_MORPH_LLM_API_KEY="your-openai-api-key"

# Optional: fixed auth token for `mistermorph serve`.
# If left empty, deploy.sh will generate one during deployment.
export MISTER_MORPH_SERVER_AUTH_TOKEN=""

# Optional: Telegram bot token (only if you need telegram mode later).
export MISTER_MORPH_TELEGRAM_BOT_TOKEN=""

# Optional: Wrangler target environment (for example: "prod", "staging").
export WRANGLER_ENV=""

# Optional: custom config.yaml path (absolute or relative).
# Example: /home/you/morph/config.prod.yaml
export MISTER_MORPH_CONFIG_PATH=""

# Optional runtime overrides (deploy.sh passes them to wrangler via --var).
export MISTER_MORPH_LLM_PROVIDER=""
export MISTER_MORPH_LLM_ENDPOINT=""
export MISTER_MORPH_LLM_MODEL=""
export MISTER_MORPH_LOG_LEVEL=""
export MISTER_MORPH_TOOLS_BASH_ENABLED=""
# Optional run mode: "serve" (default) or "telegram".
export MISTER_MORPH_RUN_MODE=""
# Optional runtime paths. Leave empty to use defaults:
# - MISTER_MORPH_WORKSPACE_DIR has no default; its directory must already exist
# - MISTER_MORPH_FILE_STATE_DIR=/tmp/mistermorph/state
# - MISTER_MORPH_FILE_CACHE_DIR=/tmp/mistermorph/cache
export MISTER_MORPH_WORKSPACE_DIR=""
export MISTER_MORPH_FILE_STATE_DIR=""
export MISTER_MORPH_FILE_CACHE_DIR=""
# Optional: set to 0 to enable bootstrap install in container entrypoint.
# Default in Cloudflare deployment is 1 (skip).
export MISTER_MORPH_SKIP_BOOTSTRAP_INSTALL=""

# Optional: set to 1 to skip npm install.
export SKIP_NPM_INSTALL=0
