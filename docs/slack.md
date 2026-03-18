# Slack Setup (Socket Mode)

This document explains how to prepare credentials for `mistermorph slack`, especially when you only have `client_id/client_secret`.

## 1. Credential Types

- `client_id` / `client_secret`
  - Used for OAuth token exchange (`code -> token`).
  - Cannot be used directly to run `mistermorph slack`.
- Bot Token (`xoxb-...`)
  - Used for Web API calls (for example, `chat.postMessage`).
  - Required by `mistermorph slack`: `slack.bot_token`.
- App Token (`xapp-...`)
  - Used by Socket Mode to open the WebSocket connection (`apps.connections.open`).
  - Required by `mistermorph slack`: `slack.app_token`.

## 2. Enable Socket Mode First

In the Slack App dashboard:

1. Go to `Socket Mode`.
2. Turn on `Enable Socket Mode`.

## 3. Get the Bot Token (`xoxb-...`)

### Option A: Install from Dashboard (Recommended)

1. Go to `OAuth & Permissions`.
2. Add the minimum required bot scopes (see next section).
3. Click `Install to Workspace` (or `Reinstall` if scopes changed).
4. Copy `Bot User OAuth Token` (`xoxb-...`).

### Option B: If You Only Have `client_id/client_secret` (OAuth Exchange)

Complete OAuth authorization to get a `code`, then call:

```bash
curl -X POST https://slack.com/api/oauth.v2.access \
  -d client_id=YOUR_CLIENT_ID \
  -d client_secret=YOUR_CLIENT_SECRET \
  -d code=AUTH_CODE \
  -d redirect_uri=YOUR_REDIRECT_URI
```

The `access_token` in the JSON response (usually `xoxb-...`) is your bot token.

## 4. Get the App Token (`xapp-...`)

`xapp` cannot be obtained via OAuth exchange with `client_id/client_secret`. You must generate it in the dashboard:

1. Go to `Basic Information`.
2. Find `App-Level Tokens`.
3. Click `Generate Token and Scopes`.
4. Add scope: `connections:write`.
5. Generate and copy the `xapp-...` token.

## 5. Required Permissions (Current Runtime)

Configure permissions in two places:

- `OAuth & Permissions` -> `Bot Token Scopes` (for `xoxb-...`)
- `Basic Information` -> `App-Level Tokens` (for `xapp-...`)

Required `Bot Token Scopes`:

- `app_mentions:read`
- `channels:history`
- `groups:history`
- `im:history`
- `mpim:history`
- `chat:write`
- `users:read`

Optional `Bot Token Scopes`:

- `reactions:write` (required only if you want emoji reaction delivery)
- `emoji:read` (required if you want `message_react` to validate against workspace available emoji names)

Required `App-Level Token` scope:

- `connections:write` (on `xapp-...`)

After adding or changing any scope:

1. Click `Reinstall to Workspace`.
2. Use the newest token values (`xoxb` / `xapp`).
3. Restart `mistermorph slack`.

If you see `missing_scope`, the usual cause is one of:

- Scope was added but app was not reinstalled.
- Runtime is still using an old token.

## 6. Configure Credentials

Environment variables (recommended):

```bash
export MISTER_MORPH_SLACK_BOT_TOKEN='xoxb-...'
export MISTER_MORPH_SLACK_APP_TOKEN='xapp-...'
```

Or in config file:

```yaml
slack:
  bot_token: "xoxb-..."
  app_token: "xapp-..."
  allowed_team_ids: []
  allowed_channel_ids: []
  group_trigger_mode: "smart" # strict|smart|talkative
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s"
  max_concurrency: 3
```

## 7. Run Example

```bash
go run ./cmd/mistermorph slack \
  --slack-bot-token "$MISTER_MORPH_SLACK_BOT_TOKEN" \
  --slack-app-token "$MISTER_MORPH_SLACK_APP_TOKEN"
```

## 8. Common Errors

- `missing slack.bot_token` / `missing slack.app_token`
  - Token was not provided, or env var names are incorrect.
- `slack auth.test failed: invalid_auth`
  - `xoxb` is invalid/expired/mis-copied, or installed in the wrong workspace.
- `slack users.info failed: missing_scope`
  - Bot token is missing `users:read`, or scope changed without reinstall/token refresh.
- `slack_emoji_catalog_load_failed ... slack emoji.list failed: missing_scope`
  - Bot token is missing `emoji:read`; `message_react` will not be registered until emoji catalog can be loaded.
- `slack apps.connections.open failed: not_allowed_token_type`
  - A non-`xapp` token was used, or `xapp` is missing `connections:write`.
- Not receiving channel messages
  - Check whether the bot is in the target channel, scopes are complete, and team/channel allowlists are not blocking.

## 9. Security Notes

- Do not commit `xoxb`/`xapp` to the repository.
- In production, prefer environment variables or a secret manager.
- Avoid logging full tokens.

## 10. Thread Behavior (Bus Semantics)

In the current implementation, Slack thread data is passed through fields in bus messages, not used as an independent routing key.

- On inbound, Slack `thread_ts` is written into:
  - `MessageEnvelope.reply_to`
  - `extensions.reply_to`
  - `extensions.thread_ts`
- On outbound delivery to Slack, thread selection priority is:
  1. `extensions.thread_ts`
  2. `extensions.reply_to`
  3. `MessageEnvelope.reply_to`
- Bus ordering/sharding key is `conversation_key = slack:<team_id>:<channel_id>`.
  Thread is not part of sharding, so different threads in the same channel share the same serialized worker.

### Status: Thread-Aware Context for `smart` Group Trigger

Implemented in V1:

- Runtime history scope is now thread-aware:
- when `thread_ts` is present, history context is isolated by thread;
- when `thread_ts` is empty, behavior remains channel-scoped.
- This applies to both addressing classification context and main task execution context.
- Bus `conversation_key` remains `slack:<team_id>:<channel_id>` in V1.

Remaining limitations:

- History is thread-scoped only for messages observed after runtime start; there is no first-hit thread backfill from Slack API yet.
- Memory subject/session keys are still channel-scoped in V1.

- Implementation checklist is tracked in `docs/feat/feat_20260301_slack.md` ("Thread-Scoped History Plan (New)").

## 11. Heartbeat Delivery

`mistermorph slack` can run heartbeat together with Slack runtime when:

- `heartbeat.enabled: true`
- `heartbeat.interval > 0`

Heartbeat notification messages are sent through Slack `chat.postMessage` to channels in `slack.allowed_channel_ids`.

- If `slack.allowed_channel_ids` is empty, heartbeat still runs, but notification delivery is skipped.
- If any target channel send fails, the notifier returns that error and heartbeat logs `heartbeat_notify_error`.

## 12. Group Trigger Implementation Note

Slack group-trigger addressing now shares the same prompt-rendering and LLM decision loop with Telegram via:

- `internal/grouptrigger/addressing_prompts.go`
- `internal/grouptrigger/decision.go`
