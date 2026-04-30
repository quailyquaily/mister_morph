# Lark (Feishu) Runtime

This document defines the implemented `mistermorph lark` runtime shape.

Status on 2026-03-06:
- implemented for `private + group` text messaging
- webhook ingress, token exchange, bus runtime, delivery adapter, contacts integration, and manual sender routing are in place

Status on 2026-04-30:
- inbound image messages can be downloaded and sent to image-capable models when `lark` is enabled in `multimodal.image.sources`
- V1 still excludes cards, generic file browsing, outbound rich media, reactions, and extra identity namespaces

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

See [References](#12-references).

Practical interpretation for this project:

- Feishu and Lark use the same core developer model for this runtime shape
- but they are still separate app environments, credentials, and webhook endpoints
- do not assume one registered app can serve both environments

Current confidence boundary on 2026-03-06:

- Feishu CN implementation is aligned with official docs
- Lark global is expected to work with the same runtime shape and configurable `base_url`
- but Lark global has not yet been smoke-tested against a live Lark tenant in this repository

## 2. Scope

- Add `mistermorph lark` as a long-running webhook runtime.
- Support `private + group` text conversations.
- Support inbound image understanding for the current user message when `lark` is listed in `multimodal.image.sources`.
- Reuse the existing channel pipeline:
  - inbound event -> bus -> per-conversation worker -> `run*Task` -> outbound bus -> delivery adapter
- Reuse shared group-trigger logic: `strict | smart | talkative`.
- Keep contacts, memory, prompt profile, and bus semantics aligned with Telegram, Slack, and LINE.
- Support one configured app per runtime process.

## 3. Non-Goals (V1)

- No cards, rich post bodies, generic file browsing, video, audio, or outbound rich media.
- No `message_react` parity in V1.
- No app-store or multi-tenant install flow in V1.
- No separate execution architecture just for Lark.

## 4. Runtime Architecture

```text
Lark Event Subscription webhook
  -> lark inbound verify/handshake + normalize + dedupe
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

V1 simplifications:

- do not register `message_react` for Lark yet
- do not attempt thread-specific routing in the first pass
- use "mention present" as the explicit-trigger heuristic until bot self identity is loaded into runtime

## 7. Outbound Policy

- Prefer `reply message` when processing an inbound event with a usable `message_id`.
- Use `send message` for proactive outbound delivery and as fallback when reply is not available.
- Start with `msg_type=text` only.
- Keep reply/send selection in the delivery adapter, not in agent logic.

## 8. CLI and Config Surface

Run command:

```bash
go run ./cmd/mistermorph lark \
  --lark-app-id "$MISTER_MORPH_LARK_APP_ID" \
  --lark-app-secret "$MISTER_MORPH_LARK_APP_SECRET" \
  --lark-webhook-listen 127.0.0.1:18081 \
  --lark-webhook-path /lark/webhook
```

Config:

```yaml
lark:
  # Feishu CN default. Keep overridable so global Lark can later use open.larksuite.com.
  base_url: "https://open.feishu.cn/open-apis"
  app_id: ""
  app_secret: ""
  webhook_listen: "127.0.0.1:18081"
  webhook_path: "/lark/webhook"
  verification_token: ""
  encrypt_key: ""
  allowed_chat_ids: []
  group_trigger_mode: "smart"
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s"
  max_concurrency: 3

multimodal:
  image:
    sources: ["telegram", "line", "lark"]
```

Field notes:

- `app_id` + `app_secret` are required to obtain `tenant_access_token`.
- `verification_token` and `encrypt_key` come from the event subscription settings.
- `allowed_chat_ids`: empty means allow every chat the bot receives; if non-empty, drop all other chats after normalization.
- `base_url` should remain overridable for mocks and for switching between Feishu CN and Lark global environments.
- typical deployment shape:
  - Feishu app -> one `mistermorph lark` instance with Feishu `base_url`
  - Lark app -> another `mistermorph lark` instance with Lark `base_url`
- when `lark` is listed in `multimodal.image.sources`, inbound image messages are downloaded under `file_cache_dir/lark/` and passed to image-capable models as image parts
- the current runtime accepts PNG, JPEG, and WebP images, keeps at most 3 images per message, and rejects images larger than 5 MiB each

Environment note:

- the current default is Feishu CN:
  - `https://open.feishu.cn/open-apis`
- if running against Lark global, set the matching Lark developer API base URL explicitly in config for that instance

## 9. Permissions and App Setup Expectations

The implementation should assume:

- a self-built app with bot capability enabled
- event subscription enabled for message receive events
- message send and reply permissions granted
- message resource download permissions granted when image input is enabled
- the app has been added to the target group chats or users can reach it in private chat

Set this up in the Feishu/Lark developer console:

- Enable bot capability.
- Subscribe to event `im.message.receive_v1`.
- Grant message send/reply permission. In current Feishu/Lark consoles this is usually `im:message` or the equivalent send-as-bot message permission.
- Grant receive-message permissions for the conversations you expect to handle. For group messages, use the console permission corresponding to `im:message.group_msg:readonly`; for private messages, use the corresponding P2P message permission when your tenant requires it.
- Grant message resource permission for images. Search for `im:resource` or the current console label for "get/upload image or file resources".
- Publish a new app version after changing permissions, then reinstall or refresh the app in the tenant.

If image download fails with a "lack of permissions" style error, check `im:resource`, group/P2P message read permission, app publication status, and whether the bot is in the chat.

## 10. Implementation Plan

Phase 1: foundations

- done: `lark` channel constant, config struct, CLI command scaffold, docs, and prompt block
- done: `conversation_key` and bus validation support

Phase 2: webhook ingress + auth

- done: tenant token client (`app_id/app_secret` -> `tenant_access_token`) with one shared caching primitive
- done: webhook challenge/verification path and inbound event normalization
- done: publish normalized inbound messages to the existing bus

Phase 3: runtime + delivery

- done: `internal/channelruntime/lark`
- done: outbound delivery adapter with `reply`-first and `send` fallback
- done: unit tests for token refresh, normalization, delivery fallback, webhook, contacts, sender, and todo refs

Phase 4: group trigger + contacts

- done: `strict|smart|talkative` decision flow
- done: `lark_user` contact identity handling and outbound sender routing
- done: prompt block injection and request/prompt dump naming

## 11. Open Questions

- Whether V1 should keep `allowed_chat_ids` or split into `allowed_group_ids` plus always-on private chats.
- Whether to support message cards in V1. The platform supports them, but plain text is the right first milestone.
- Whether to expose an explicit `region` field later instead of overloading `base_url`.

## 12. References

Official docs checked on 2026-03-06:

- [Self-built app: tenant_access_token](https://open.feishu.cn/document/server-docs/authentication-management/access-token/tenant_access_token_internal)
- [Receive message event](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)
- [Send message](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [Reply message](https://open.feishu.cn/document/server-docs/im-v1/message/reply)
- [Lark Developer: Send message](https://open.larksuite.com/document/server-docs/im-v1/message/create)
- [Lark Developer Home](https://open.larksuite.com/)
