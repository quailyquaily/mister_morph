# LINE Setup and Configuration

This document explains how to configure `mistermorph line` end-to-end.

Current runtime capability:
- Webhook ingress with signature verification (`X-Line-Signature`)
- Text message handling for group and private chats
- Image message handling (download to cache + multimodal parts)
- Main agent execution (`runLineTask`)
- Group trigger modes: `strict | smart | talkative`
- No `message_react` tool in LINE runtime (LINE responses are text-only)
- Outbound delivery with `reply` first, fallback to `push` when reply token is invalid/expired

## Runtime Architecture (ASCII)

```text
LINE user/group message
        |
        v
LINE Messaging API webhook
        |
        v
line runtime HTTP handler
(verify signature + normalize event)
        |
        v
line inbound adapter
        |
        v
inproc bus (topic: chat.message, inbound)
        |
        v
line runtime dispatcher
(per-conversation worker queue)
        |
        v
runLineTask
(trigger decision -> prompt build -> agent.Engine)
        |
        v
inproc bus (topic: chat.message, outbound)
        |
        v
line delivery adapter
  |- reply (when replyToken is valid)
  `- push  (fallback on token missing/expired/invalid)
        |
        v
LINE Messaging API send
```

## 1. LINE Console Step-by-Step (Do This First)

Follow this sequence in LINE Developers Console so you can actually start `mistermorph line`:

1. Create a LINE Messaging API channel.
- Go to LINE Developers Console.
- Create/select a Provider.
- Create a `Messaging API` channel (not LIFF-only).

2. Get `Channel secret`.
- Open the channel.
- Go to `Basic settings`.
- Copy `Channel secret`.
- Map it to `line.channel_secret`.

3. Get `Channel access token`.
- Open the channel.
- Go to `Messaging API`.
- Issue/copy a channel access token.
- Map it to `line.channel_access_token`.

4. Start local runtime first.
- Run `mistermorph line` locally and expose webhook port:
```bash
go run ./cmd/mistermorph line \
  --line-channel-access-token "$MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN" \
  --line-channel-secret "$MISTER_MORPH_LINE_CHANNEL_SECRET" \
  --line-webhook-listen 127.0.0.1:18080 \
  --line-webhook-path /line/webhook
```

5. Expose local webhook to public internet.
- LINE must reach your webhook over public HTTPS.
- Example with `ngrok`:
```bash
ngrok http 18080
```
- If ngrok gives `https://abc123.ngrok-free.app`, your webhook URL is:
  `https://abc123.ngrok-free.app/line/webhook`

6. Configure webhook in LINE Console.
- Open channel -> `Messaging API`.
- Set `Webhook URL` to your public URL above.
- Turn on `Use webhook`.
- Click `Verify` and ensure it succeeds.

7. Enable group join.
- Open channel -> `Messaging API`.
- Turn on `Allow bot to join group chats`.
- If this is off, adding bot to a group may immediately show `left the group`.

8. Do a real message test.
- Send a private message to the bot account.
- You should see runtime log `line_task_enqueued`.
- If this works, token/secret/webhook wiring is correct.

## 2. Required Config

You must set both:
- `line.channel_access_token`
- `line.channel_secret`

You can set them via environment variables:

```bash
export MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN='...'
export MISTER_MORPH_LINE_CHANNEL_SECRET='...'
```

Or in config:

```yaml
line:
  channel_access_token: "..."
  channel_secret: "..."
```

## 3. Recommended Full Config

```yaml
line:
  base_url: "https://api.line.me"
  channel_access_token: "..."
  channel_secret: "..."
  webhook_listen: "127.0.0.1:18080"
  webhook_path: "/line/webhook"
  allowed_group_ids: [] # empty means allow all groups
  group_trigger_mode: "smart" # strict|smart|talkative
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s" # 0 means use top-level timeout
  max_concurrency: 3
```

Field notes:
- `base_url`: keep default unless you are using a test/mocked LINE API endpoint.
- `webhook_listen`: local bind address for the webhook HTTP server.
- `webhook_path`: must match the path in LINE Console webhook URL.
- `allowed_group_ids`: applies to group chats only; private chats are still accepted.
- image recognition is enabled by default; inbound images are downloaded and attached as multimodal parts.
- `group_trigger_mode`:
  - `strict`: only explicit triggers (mention/command prefix) in groups.
  - `smart`: use addressing LLM and require addressed+confidence.
  - `talkative`: use addressing LLM and allow proactive interjection.

## 4. Group Trigger Rules

In group chats, LINE runtime currently treats these as explicit triggers:
- bot user id is in `mention_users`
- message text starts with `/` (command-style prefix)

Notes:
- when explicit trigger matches, message enters main task directly.
- otherwise behavior depends on `group_trigger_mode` (`strict|smart|talkative`).
- `reply-to-bot` explicit signal is not used yet in LINE because webhook payload in current path does not provide a stable reply-target identity field.

## 5. Run Command

```bash
go run ./cmd/mistermorph line \
  --line-channel-access-token "$MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN" \
  --line-channel-secret "$MISTER_MORPH_LINE_CHANNEL_SECRET" \
  --line-webhook-listen 127.0.0.1:18080 \
  --line-webhook-path /line/webhook
```

If your webhook is exposed through reverse proxy/tunnel:
- public URL should be `https://<public-host>/line/webhook`
- proxy forwards to your local `webhook_listen` address

## 6. How to Verify Configuration

Startup log checks:
- `line_start` appears
- `bot_user_id_present=true` is preferred (used for mention trigger detection)

Inbound checks:
- send a private message to bot, expect `line_task_enqueued`
- send a group message, expect either `line_group_trigger` or `line_group_ignored`

Outbound checks:
- bot replies successfully in chat
- no LINE reaction log should appear (`message_reaction_applied` is Telegram/Slack only)
- if reply token is expired, fallback log `line_reply_failed_fallback_push` should appear and message is sent via push

## 7. Prompt/Request Dump Inspection

To inspect LINE prompt and request payloads, run with:

```bash
go run ./cmd/mistermorph line \
  --line-channel-access-token "$MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN" \
  --line-channel-secret "$MISTER_MORPH_LINE_CHANNEL_SECRET" \
  --inspect-prompt \
  --inspect-request
```

Generated files:
- `dump/prompt_line_YYYYMMDD_HHmmss.md`
- `dump/request_line_YYYYMMDD_HHmmss.md`

Use this when checking:
- whether LINE block/profile text was injected into the final prompt
- whether multimodal `Parts` were built for inbound images

## 8. Common Misconfigurations

- `missing line.channel_access_token` or `missing line.channel_secret`
  - credentials are not set or env var names are wrong.
- `invalid signature` on webhook
  - `line.channel_secret` does not match the channel used by webhook URL.
- no group messages processed
  - check `allowed_group_ids` and whether the incoming group id is in allowlist.
- webhook receives events but no reply
  - verify outbound token permission and bot friendship/invite state in LINE.
