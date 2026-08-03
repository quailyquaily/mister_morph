# Deploy MisterMorph to Cloudflare Containers

This folder contains a Cloudflare Worker + Container setup for `mistermorph serve`.

## Files

- `Dockerfile`: builds and runs `mistermorph` in daemon mode (`serve` on port `8787`)
- `config.yaml`: container-side runtime config (`--config /app/config.yaml`)
- `src/index.js`: Worker entrypoint, routes requests to container instances
- `wrangler.jsonc`: Cloudflare Containers and Durable Object bindings
- `env.example.sh`: environment variable template (`account id`, `api token`, LLM key, etc.)
- `deploy.sh`: one-command deployment helper

## Prerequisites

- Docker
- Node.js + npm
- Cloudflare account
- `wrangler` authentication completed (`npx wrangler login`)

## Deploy

```bash
cd deploy/cloudflare
cp env.example.sh env.sh
# edit env.sh and fill real values
./deploy.sh
```

Manual deploy (without helper script):

```bash
npx wrangler deploy --config ./wrangler.jsonc --env prod
```

Optional env vars:

- `MISTER_MORPH_SERVER_AUTH_TOKEN`: bearer token for `mistermorph serve` (auto-generated if omitted)
- `MISTER_MORPH_TELEGRAM_BOT_TOKEN`: if you want to run Telegram mode later
- `WRANGLER_ENV`: target wrangler environment
- `SKIP_NPM_INSTALL=1`: skip `npm install`
- `WRANGLER_CONFIG_PATH`: override wrangler config path (default: `./wrangler.jsonc`)
- `MISTER_MORPH_CONFIG_PATH`: custom `config.yaml` path (default: `./config.yaml`)
- `MISTER_MORPH_LLM_PROVIDER` / `MISTER_MORPH_LLM_ENDPOINT` / `MISTER_MORPH_LLM_MODEL`
- `MISTER_MORPH_LOG_LEVEL` / `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `MISTER_MORPH_RUN_MODE` (`serve` or `telegram`)
- `MISTER_MORPH_WORKSPACE_DIR` (optional default project directory; the directory must exist in the container)
- `MISTER_MORPH_FILE_STATE_DIR` / `MISTER_MORPH_FILE_CACHE_DIR` (defaults: `/tmp/mistermorph/state`, `/tmp/mistermorph/cache`)
- `MISTER_MORPH_SKIP_BOOTSTRAP_INSTALL` (`1` to skip `mistermorph install --yes` in entrypoint; Cloudflare default is `1`)

`config.yaml` stores non-sensitive defaults. Secrets still come from Cloudflare secrets env vars:

- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `MISTER_MORPH_LLM_API_KEY`
- `MISTER_MORPH_SERVER_AUTH_TOKEN`

`deploy.sh` will auto-load `./env.sh` if the file exists.
If `MISTER_MORPH_CONFIG_PATH` is set, `deploy.sh` will stage that file into the image for this deployment only.
If runtime override vars (above) are set in shell, `deploy.sh` passes them to Wrangler with `--var`.

Example with an external config path:

```bash
MISTER_MORPH_CONFIG_PATH="/absolute/path/to/config.yaml" ./deploy.sh
```

Run in Telegram mode:

```bash
MISTER_MORPH_RUN_MODE="telegram" \
MISTER_MORPH_TELEGRAM_BOT_TOKEN="123456:bot-token" \
./deploy.sh
```

Recommended:
- Set `telegram.allowed_chat_ids` in your `config.yaml`.
- Keep `MISTER_MORPH_TELEGRAM_BOT_TOKEN` in secret env (not in config file).

## Observe runtime status

Tail deployment/runtime logs:

```bash
npx wrangler tail --config ./wrangler.jsonc --env prod
```

Look for:
- `mistermorph_boot mode=telegram` (container entrypoint started in telegram mode)
- `telegram_start` (mistermorph telegram loop started)

Container control/state endpoints (protected by `MISTER_MORPH_SERVER_AUTH_TOKEN`):

```bash
curl -X POST -H "Authorization: Bearer <token>" "https://<worker-domain>/_mistermorph/start?instance=default"
curl -X POST -H "Authorization: Bearer <token>" "https://<worker-domain>/_mistermorph/stop?instance=default"
# hard stop + destroy container process:
curl -X POST -H "Authorization: Bearer <token>" "https://<worker-domain>/_mistermorph/stop?instance=default&hard=1"
curl -H "Authorization: Bearer <token>" "https://<worker-domain>/_mistermorph/state?instance=default"
curl -H "Authorization: Bearer <token>" "https://<worker-domain>/_mistermorph/lifecycle?instance=default"
```

In telegram mode, normal HTTP proxy endpoints are not used; requests return a mode/status JSON response.

## Common errors

- `Unexpected fields found in containers field: "disk"`:
  - This has been removed from `wrangler.jsonc`; use `instance_type` only.
- `No environment found in configuration with name "prod"`:
  - Ensure `WRANGLER_ENV=prod` only when `env.prod` exists in `wrangler.jsonc`.
- `Authentication error [code: 10000]`:
  - Usually means `CLOUDFLARE_ACCOUNT_ID`/`CLOUDFLARE_API_TOKEN` is invalid or token permissions are insufficient.
  - Quick checks:
    - `npx wrangler whoami`
    - `echo \"$CLOUDFLARE_ACCOUNT_ID\"`
    - Confirm the API token includes Workers deploy permissions for that account.
  - If you use OAuth login locally, try unsetting token env vars:
    - `unset CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID`
    - `npx wrangler login`
    - rerun `./deploy.sh`

## Routing

- Default instance: all requests
- Select instance via query param: `?instance=my-session`

Example:

```bash
curl -H "Authorization: Bearer <token>" "https://<worker-domain>/health"
```

(` /health` applies to `serve` mode. For `telegram` mode use `/_mistermorph/state`.)
