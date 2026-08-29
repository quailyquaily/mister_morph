# Mixin Messenger

Mister Morph can run as a Mixin Messenger bot in private conversations and groups. It receives messages through the Mixin Blaze WebSocket and sends replies through the Mixin Messenger API.

This runtime handles messages only. It does not read assets, transfer funds, or use the wallet fields in a Mixin keystore.

## Create the bot

1. Create an Application in Mixin Developer Dashboard.
2. Download its Ed25519 keystore JSON.
3. Keep the keystore outside the repository. On Unix, run `chmod 600 <keystore>`; Morph rejects files readable by group or other users.
4. Put the keystore path in `config.yaml` or set `MISTER_MORPH_MIXIN_KEYSTORE_FILE`.

The runtime reads `app_id`, `session_id`, and `session_private_key` from current Dashboard keystores. It also accepts the legacy names `client_id` and `private_key`. It ignores PIN, server public key, and wallet fields.

## Configuration

```yaml
mixin:
  keystore_file: "./secrets/mixin-keystore.json"
  allowed_conversation_ids: []
  task_timeout: "0s"
  max_concurrency: 3
  serve_listen: ""
```

A relative `keystore_file` is resolved from the directory containing `config.yaml`.

`allowed_conversation_ids` contains Mixin conversation UUIDs. An empty list allows every conversation. To find the current conversation UUID, send `/id` in a private conversation or mention the bot with `/id` in a group.

## Start the runtime

Run it as a standalone process:

```bash
mistermorph mixin
```

The main CLI overrides are:

```text
--mixin-keystore-file
--mixin-allowed-conversation-id
--mixin-task-timeout
--mixin-max-concurrency
```

To expose the standard remote Runtime API, set a listen address and a server token:

```yaml
server:
  auth_token: "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

mixin:
  serve_listen: "127.0.0.1:8792"
```

The API base path is `/runtime`.

## Run it inside Console

Console can manage Mixin in the same process:

```yaml
console:
  managed_runtimes: ["mixin"]

mixin:
  keystore_file: "./secrets/mixin-keystore.json"
```

The Mixin runtime then shares the Console task store and does not appear as a separate endpoint. Its configuration is available in Console Settings.

Do not start the same bot as both a managed runtime and a separate `mistermorph mixin` process. Both processes would consume the same Blaze message stream.

## Private conversations and groups

Private text messages trigger a task directly.

The official Mixin Messenger client sends readable group messages to a Bot only when the text explicitly mentions it. A plain group message is not available to Morph. Replying to a Bot message sets `quote_message_id`, but does not mention the Bot; use reply together with `@<bot_mixin_id>`.

Because every readable group message has already been addressed to the Bot by Mixin, this runtime has no group trigger mode or addressing classifier.

Mixin group commands must include the bot mention so that multiple bots do not answer the same command:

```text
@7000123456 /id
@7000123456 /models
@7000123456 /stop
```

Private conversations can use commands without the mention.

## Files and approvals

The runtime accepts images, files, and audio. Downloads are stored under `file_cache_dir/mixin/` and use the same path and size checks as the other channel runtimes.

The following tools are available inside a Mixin task:

```text
mixin_send_file
mixin_send_photo
mixin_send_audio
```

When a tool needs approval, the bot sends the reason, the complete tool parameters, and two commands:

```text
/approve <approval_id>
/deny <approval_id>
```

In a group, include the bot mention before either command.

## Contacts and Agent pairing

Mixin users are stored as `mixin:<user_uuid>`. Conversations are referenced as `mixin:<conversation_uuid>`. A user's public Mixin ID is kept for display and group mentions, but UUIDs remain the canonical identifiers.

An administrator can pair two Morph Agents through their existing private conversations. Configure the administrator with a Mixin user UUID:

```yaml
admins:
  - mixin:773e5e77-4107-45c2-b648-8fc722ed77f5
```

The target must already be an active Agent in Contacts. After pairing, private Agent messages may bypass `allowed_conversation_ids`, matching the behavior of the other channel runtimes.

## Current limits

- Mixin does not provide the typing, message editing, thread/topic, or reaction behavior used by some other channels.
- Bots cannot read ordinary group traffic. Group messages and replies must explicitly mention the Bot.
- Unsupported cards, locations, stickers, videos, wallet messages, and payment events do not enter the Agent prompt.
- Approval buttons remain disabled until their behavior is verified with a real Bot account. Text approval commands always work.
