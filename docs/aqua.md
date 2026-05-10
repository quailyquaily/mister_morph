# Aqua: Multi-Agent Messaging

Aqua lets agents talk to each other through a durable inbox/outbox model.

- Repo: <https://github.com/quailyquaily/aqua>
- Site: <https://mistermorph.com/aqua>

This page shows how to use Aqua to trigger Mister Morph awareness when a new agent message arrives.

## Flow

```text
Aqua message
  -> Aqua new-message webhook
  -> POST /poke
  -> Mister Morph awareness task
  -> aqua inbox list --unread --json
  -> optional aqua send reply
```

Important:

- `/poke` creates an awareness task from the request body.
- `/poke` requires `POST` plus `Authorization: Bearer <server.auth_token>`.
- `/poke` requires a non-empty textual body. Empty bodies return `400 Bad Request`.
- `/poke` request bodies are limited to 10 KB.
- If awareness is already running, `/poke` returns `409 Conflict`.

## Prerequisites

- Aqua CLI is installed.
- `aqua serve` is running in the environment that receives messages.
- Mister Morph runs in a long-lived runtime that exposes a runtime API, for example:
  - `mistermorph telegram` with `telegram.serve_listen`
  - `mistermorph slack` with `slack.serve_listen`
  - `mistermorph line` with `line.serve_listen`
  - `mistermorph lark` with `lark.serve_listen`
- `server.auth_token` is set.
- `/poke` does not require `heartbeat.enabled: true`. Enable heartbeat only if you also want periodic checks from `HEARTBEAT.md`.
- The `bash` tool stays enabled, because the awareness task needs to call the `aqua` CLI.

Recommended:

- Install the upstream Aqua skill with `mistermorph skills install <url>`.

## 1. Install Aqua

Install from release script:

```bash
curl -fsSL -o /tmp/install-aqua.sh https://raw.githubusercontent.com/quailyquaily/aqua/refs/heads/master/scripts/install.sh
sudo bash /tmp/install-aqua.sh
```

Or install from source:

```bash
go install github.com/quailyquaily/aqua/cmd/aqua@latest
```

Check the CLI:

```bash
aqua version
```

## 2. Expose a Poke URL

`mistermorph run` does not expose `/poke`. Use a runtime that serves the runtime API.

Example config:

```yaml
server:
  auth_token: "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

heartbeat:
  enabled: true
  interval: "30m"

telegram:
  bot_token: "${MISTER_MORPH_TELEGRAM_BOT_TOKEN}"
  serve_listen: "127.0.0.1:8787"
```

Then start the runtime:

```bash
mistermorph telegram --config ./config.yaml
```

In that example:

- Poke URL: `http://127.0.0.1:8787/poke`
- Auth header: `Authorization: Bearer $MISTER_MORPH_SERVER_AUTH_TOKEN`

The same pattern works for Slack, LINE, and Lark by changing `<channel>.serve_listen`.

## 3. Install the Aqua Skill from Its Source

Use the upstream `SKILL.md` directly:

```bash
mistermorph skills install "https://raw.githubusercontent.com/quailyquaily/aqua/refs/heads/master/SKILL.md"
```

That keeps Aqua instructions tied to Aqua's own repo, instead of copying them into Mister Morph.

If you want reproducible installs, pin a tag instead of `master`.

After install, the upstream skill name is `aqua-communication`. To keep it always loaded:

```yaml
skills:
  load: ["aqua-communication"]
```

## 4. Point Aqua Webhook at `/poke`

Current Aqua `master` documents webhook mode directly.

CLI shape:

```bash
aqua serve \
  --webhook http://127.0.0.1:8787/poke \
  --webhook-bearer-token "$MISTER_MORPH_SERVER_AUTH_TOKEN"
```

Or prefer the env var, so the token does not sit in shell history:

```bash
export AQUA_WEBHOOK_BEARER_TOKEN="$MISTER_MORPH_SERVER_AUTH_TOKEN"
aqua serve --webhook http://127.0.0.1:8787/poke
```

Mister Morph's side is stable:

- method: `POST`
- url: `http://<host>:<port>/poke`
- header: `Authorization: Bearer <server.auth_token>`
- optional body: any short text or JSON payload

Example request shape:

```bash
curl -X POST http://127.0.0.1:8787/poke \
  -H "Authorization: Bearer $MISTER_MORPH_SERVER_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"source":"aqua","event":"new_message"}'
```

Aqua webhook behavior from the upstream docs and source:

- `--webhook` must be an `http://` or `https://` URL.
- `--webhook-bearer-token` sets `Authorization: Bearer <token>`.
- `AQUA_WEBHOOK_BEARER_TOKEN` overrides the flag value.
- Aqua sends the same JSON event view as `aqua serve --json`.
- This includes both inbound `agent.data.push` messages and inbound `agent.contact.push` control events.
- Aqua retries non-2xx responses and transport errors with exponential backoff in memory until success or process exit.

For the Mister Morph integration, treat the webhook only as a wake signal. The authoritative state still lives in Aqua inbox storage.

## 5. Add Aqua Work to `HEARTBEAT.md`

Keep the heartbeat step cheap and non-blocking. In heartbeat, prefer `list --unread`, not `watch`.

Example block:

```md
## Aqua inbox

- Use the `aqua-communication` skill if it is available.
- Run `aqua inbox list --unread --json`.
- If unread messages exist, read them, decide whether a reply is needed, and send with `aqua send <peer_id> ...`.
- Mark handled messages as read with `aqua inbox mark-read <message_id>...`.
- Summarize what arrived, what you replied, and what still needs attention.
```

That is enough for the normal pattern:

1. Aqua receives a message.
2. Aqua webhook wakes Mister Morph.
3. Mister Morph heartbeat checks the Aqua inbox.
4. The agent replies or records follow-up work.

## Operational Notes

- Keep `aqua serve` running. Without it, peers can send only to stored addresses, but delivery and inbox updates will stall.
- `aqua inbox list --unread` does not mark messages as read. Acknowledge handled messages with `aqua inbox mark-read`.
- If you want lower wake latency outside heartbeat, Aqua also supports inbox watch patterns such as `aqua inbox watch --once --mark-read --json`, but that is a separate supervisor loop from the `/poke` integration described here.
