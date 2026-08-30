# Lark (Feishu) Runtime

This document defines the implemented `mistermorph lark` runtime shape.

Status on 2026-03-06:
- implemented for `private + group` text messaging
- initial webhook ingress, token exchange, bus runtime, delivery adapter, contacts integration, and manual sender routing were added

Status on 2026-04-30:
- inbound image messages can be downloaded and sent to image-capable models
- V1 still excludes cards, generic file browsing, video, and extra identity namespaces

Status on 2026-05-19:
- Feishu/Lark event subscription supports two receive modes at the platform level: long connection through the official SDK WebSocket client, and HTTP webhook to a developer server.
- The current `mistermorph lark` runtime uses the official SDK WebSocket long connection.
- No Telegram-style polling mode is documented for receiving bot message events. Inbound messages are received through event subscription.
- Webhook ingress was removed. There is no webhook compatibility mode.
- Lark runtime tools are aligned with Telegram's current channel tools: send file, send photo, send voice, and `message_react`.

## 1. Verified Platform Facts

Checked against official Feishu docs on 2026-03-06:

- self-built apps exchange `app_id` + `app_secret` for `tenant_access_token`
- `tenant_access_token` is valid for 2 hours
- bot apps can subscribe to the message receive event and get messages from both private and group chats
- outbound delivery has both a general send API and a dedicated reply API
- message resources, including images, are fetched through the message resource API with the app tenant token
- the same API family is exposed under both:
  - `open.feishu.cn`
  - `open.larksuite.com`

Checked again on 2026-05-19:

- event subscription receive mode can be either:
  - long connection: the app process uses the official SDK to establish a WebSocket connection to the open platform
  - developer server: the open platform sends event payloads to the configured public HTTP webhook URL
- long connection does not require a public inbound URL, `verification_token`, or `encrypt_key`, because connection auth and transport are handled by the SDK path
- this repository uses long connection as the only Lark inbound path
- webhook parsing, verification, decrypt, URL challenge, and local webhook server code were deleted

See [References](#12-references).

Practical interpretation for this project:

- Feishu and Lark use the same core developer model for this runtime shape
- but they are still separate app environments, credentials, and event subscription settings
- do not assume one registered app can serve both environments

Current confidence boundary on 2026-03-06:

- Feishu CN implementation is aligned with official docs
- Lark global is expected to work with the same runtime shape and configurable `base_url`
- but Lark global has not yet been smoke-tested against a live Lark tenant in this repository

## 2. Scope

- Add `mistermorph lark` as a long-running WebSocket runtime.
- Support `private + group` text conversations.
- Support inbound image understanding for the current user message.
- Reuse the existing channel pipeline:
  - inbound event -> bus -> per-conversation worker -> `run*Task` -> outbound bus -> delivery adapter
- Reuse shared group-trigger logic: `strict | smart | talkative`.
- Keep contacts, prompt profile, and bus semantics aligned with Telegram, Slack, and LINE.
- Support one configured app per runtime process.

## 3. Non-Goals (V1)

- No cards, rich post bodies, generic file browsing, or video.
- No app-store or multi-tenant install flow in V1.
- No separate execution architecture just for Lark.

## 4. Runtime Architecture

```text
Lark Event Subscription long connection
  -> official SDK WebSocket client
  -> SDK dispatcher receives im.message.receive_v1
  -> normalize + dedupe
  -> inproc bus (chat.message, inbound)
  -> lark dispatcher (per conversation key)
  -> runLarkTask (agent.Engine)
  -> inproc bus (chat.message, outbound)
  -> lark delivery adapter
     |- reply message API (preferred)
     `- send message API (fallback / proactive send)
  -> Lark IM API
```

## 5. Identity and Routing

- `conversation_key`: `lark:<chat_id>`
- `conversation_type`: map Lark chat type to `private|group`
- speaker/contact identity: `lark_user:<open_id>`
- direct outbound route: `chat_id=lark:<chat_id>`

Rationale:

- `chat_id` is the natural bus sharding key.
- `open_id` is app-scoped and suitable for speaker identity without leaking tenant-wide identifiers into routing.
- If we later need cross-app identity, add enrichment fields such as `user_id` or `union_id` rather than changing the routing key.
- one runtime instance is intentionally bound to one app credential set; if both Feishu and Lark are needed, run two instances.

First-principles guardrails:

- model only the two objects the platform already gives us in V1: chats and people
- keep only two internal ref namespaces in V1: `lark:` and `lark_user:`
- do not add `lark_union:*`, tenant-level identity merging, or alternate routing keys before a real requirement exists
- after each implementation chunk, ask whether the current design is still the minimum needed to route chats and identify speakers

## 6. Trigger Rules

Private chats:

- always trigger the main task

Group chats:

- explicit trigger if the event carries one or more mentions
- explicit trigger if text starts with `/`
- otherwise use shared addressing decision:
  - `strict`: explicit trigger only
  - `smart`: require `addressed=true` and confidence threshold
  - `talkative`: allow interjection threshold path

Runtime simplifications:

- do not attempt thread-specific routing in the first pass
- use "mention present" as the explicit-trigger heuristic until bot self identity is loaded into runtime

## 7. Outbound Policy

- Prefer `reply message` when processing an inbound event with a usable `message_id`.
- Use `send message` for proactive outbound delivery and as fallback when reply is not available.
- Keep reply/send selection in the delivery adapter, not in agent logic.

Channel tools registered in Lark task runs:

- `lark_send_file`: uploads a local file under `file_cache_dir` and sends it as a Lark file message.
- `lark_send_photo`: uploads a local image under `file_cache_dir` and sends it as a Lark image message.
- `lark_send_voice`: uploads a local OPUS audio file under `file_cache_dir` and sends it as a Lark audio message.
- `message_react`: adds a Lark message reaction to the current inbound `message_id`.

All file tools reject paths outside `file_cache_dir`.

## 8. CLI and Config Surface

Run command:

```bash
go run ./cmd/mistermorph lark \
  --lark-app-id "$MISTER_MORPH_LARK_APP_ID" \
  --lark-app-secret "$MISTER_MORPH_LARK_APP_SECRET"
```

Config:

```yaml
console:
  managed_runtimes: ["lark"]

lark:
  # Feishu CN default. Keep overridable so global Lark can later use open.larksuite.com.
  base_url: "https://open.feishu.cn/open-apis"
  app_id: ""
  app_secret: ""
  allowed_chat_ids: []
  group_trigger_mode: "smart"
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s"
  max_concurrency: 3
```

Field notes:

- `app_id` + `app_secret` are required to obtain `tenant_access_token`.
- `console.managed_runtimes: ["lark"]` runs the Lark WebSocket runtime inside `mistermorph console serve`; omit it when running `mistermorph lark` as a separate process.
- `verification_token`, `encrypt_key`, `webhook_listen`, and `webhook_path` are not used and should be removed from old configs.
- `allowed_chat_ids`: empty means allow every chat the bot receives; if non-empty, drop all other chats after normalization.
- `base_url` should remain overridable for mocks and for switching between Feishu CN and Lark global environments.
- WebSocket SDK domain is derived from `base_url` by removing `/open-apis`.
- typical deployment shape:
  - Feishu app -> one `mistermorph lark` instance with Feishu `base_url`
  - Lark app -> another `mistermorph lark` instance with Lark `base_url`
- inbound image messages are downloaded under `file_cache_dir/lark/` and passed to image-capable models as image parts
- the current runtime accepts PNG, JPEG, and WebP images, keeps at most 3 images per message, and rejects images larger than 5 MiB each

Environment note:

- the current default is Feishu CN:
  - `https://open.feishu.cn/open-apis`
- if running against Lark global, set the matching Lark developer API base URL explicitly in config for that instance:
  - `https://open.larksuite.com/open-apis`

Developer console setup for the current WebSocket runtime:

1. Choose one platform environment.
   - Feishu CN developer console: `https://open.feishu.cn/app`
   - Lark global developer console: `https://open.larksuite.com/app`
2. Create a self-built/custom app in that console.
3. Open the app's credentials/basic information page and copy:
   - `App ID` -> `lark.app_id` or `MISTER_MORPH_LARK_APP_ID`
   - `App Secret` -> `lark.app_secret` or `MISTER_MORPH_LARK_APP_SECRET`
4. Enable the bot capability for the app.
5. Open the event subscription or events/callbacks page.
6. Select long connection mode.
7. Add the message receive event:
   - `im.message.receive_v1`
8. Open permission management and grant the app the message permissions required by your tenant.
    - message send/reply: commonly `im:message` or `im:message:send_as_bot`
    - group message receive/read: commonly `im:message.group_msg:readonly` or the current console equivalent
    - private message receive/read: grant the current P2P message permission if the tenant requires it
    - image/resource download: commonly `im:resource`, needed for Lark image input
    - file/image upload: grant the console permissions for uploading message files and images
    - message reaction: grant the console permission for message reactions
9. Create and publish a new app version after changing permissions or events.
10. Install or refresh the app in the tenant, then add the bot to target group chats or open a private chat with it.

WebSocket mode does not need a public callback URL.

## 9. Permissions and App Setup Expectations

The implementation should assume:

- a self-built app with bot capability enabled
- event subscription enabled for message receive events
- message send and reply permissions granted
- message resource download permissions granted for image input
- image/file upload permissions granted when `lark_send_photo`, `lark_send_file`, or `lark_send_voice` are used
- message reaction permission granted when `message_react` is used
- the app has been added to the target group chats or users can reach it in private chat

Set this up in the Feishu/Lark developer console:

- Enable bot capability.
- Subscribe to event `im.message.receive_v1`.
- Grant message send/reply permission. In current Feishu/Lark consoles this is usually `im:message` or the equivalent send-as-bot message permission.
- Grant receive-message permissions for the conversations you expect to handle. For group messages, use the console permission corresponding to `im:message.group_msg:readonly`; for private messages, use the corresponding P2P message permission when your tenant requires it.
- Grant message resource permission for images. Search for `im:resource` or the current console label for "get/upload image or file resources".
- Grant image/file upload permissions for runtime tools that send local files.
- Grant message reaction permission for `message_react`.
- Publish a new app version after changing permissions, then reinstall or refresh the app in the tenant.

If image download fails with a "lack of permissions" style error, check `im:resource`, group/P2P message read permission, app publication status, and whether the bot is in the chat.

## 10. Implementation Plan

Phase 1: foundations

- done: `lark` channel constant, config struct, CLI command scaffold, docs, and prompt block
- done: `conversation_key` and bus validation support

Phase 2: WebSocket ingress + auth

- done: tenant token client (`app_id/app_secret` -> `tenant_access_token`) with one shared caching primitive
- done: official SDK WebSocket ingress and inbound event normalization
- done: publish normalized inbound messages to the existing bus

Phase 3: runtime + delivery

- done: `internal/channelruntime/lark`
- done: outbound delivery adapter with `reply`-first and `send` fallback
- done: unit tests for token refresh, normalization, delivery fallback, contacts, sender, tools, and todo refs

Phase 4: group trigger + contacts

- done: `strict|smart|talkative` decision flow
- done: `lark_user` contact identity handling and outbound sender routing
- done: prompt block injection and request/prompt dump naming
- done: Lark channel tools for file, photo, voice, and message reaction
- done: asynchronous Contact avatar refresh from the sender's visible Contact profile

Avatar lookup uses the sender's `open_id` after an authorized inbound message. Missing Contact permissions or visibility only disables the avatar; it does not reject the message or change Contact YAML.

## 11. Open Questions

- Whether V1 should keep `allowed_chat_ids` or split into `allowed_group_ids` plus always-on private chats.
- Whether to support message cards in V1. The platform supports them, but plain text is the right first milestone.
- Whether to expose an explicit `region` field later instead of overloading `base_url`.

Resolved on 2026-05-19:

- The official SDK WebSocket client replaced webhook ingress.
- Webhook ingress was deleted without compatibility mode.
- Lark gained Telegram-equivalent channel tools in the same work: `lark_send_file`, `lark_send_photo`, `lark_send_voice`, and `message_react`.

## 12. References

Official docs checked on 2026-03-06 and 2026-05-19:

- [Event overview](https://open.feishu.cn/document/server-docs/event-subscription-guide/overview)
- [Configure event subscription method](https://open.feishu.cn/document/server-docs/event-subscription-guide/event-subscription-configure-/request-url-configuration-case)
- [Self-built app: tenant_access_token](https://open.feishu.cn/document/server-docs/authentication-management/access-token/tenant_access_token_internal)
- [Receive message event](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)
- [Send message](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [Reply message](https://open.feishu.cn/document/server-docs/im-v1/message/reply)
- [Official Go SDK WebSocket client](https://github.com/larksuite/oapi-sdk-go/tree/v3_main/ws)
- [Official Go SDK WebSocket sample](https://github.com/larksuite/oapi-sdk-go/blob/v3_main/sample/ws/sample.go)
- [Lark Developer: Send message](https://open.larksuite.com/document/server-docs/im-v1/message/create)
- [Lark Developer: Add message reaction](https://open.larksuite.com/document/server-docs/im-v1/message-reaction/create)
- [Lark Developer Home](https://open.larksuite.com/)
