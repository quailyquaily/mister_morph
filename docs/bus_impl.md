---
date: 2026-02-08
title: Bus Implementation Notes (Consolidated)
status: implemented
---

# Bus Implementation Notes (Code-Aligned Consolidation)

## 1) Scope

In scope:
- `internal/bus` runtime, validation, and error model
- `internal/bus/adapters/*` inbound and delivery adapters
- Bus wiring in `cmd/mistermorph/telegramcmd` and `cmd/mistermorph/slackcmd`
- Bus inbox/outbox state in `contacts/*`
- Outbound bus path in `internal/contactsruntime/sender.go`

Out of scope:
- Multi-channel architecture roadmap and product-level planning (see `docs/bus.md`)

## 2) Bus Runtime (`internal/bus`)

### 2.1 Execution Model
- Entry point: `StartInproc(BootstrapOptions)`.
- Runtime: in-process bus only (`inproc`).
- Ordering: shard workers route by `hash(conversation_key)`; messages in the same `conversation_key` are processed in order.
- Backpressure: bounded token pool + bounded shard queues controlled by `max_inflight`.
- Topology constraints:
  - one handler per topic
  - topology freezes after first publish (`Subscribe` after start is rejected)
- Publish APIs:
  - `PublishValidated(ctx, msg)` for boundary calls
  - `Publish(ctx, msg)` for internal fast path

### 2.2 Typed Error Model
Implemented error codes:
- `BUS_CLOSED`
- `NO_SUBSCRIBER`
- `QUEUE_FULL`
- `TOPIC_ALREADY_HANDLED`
- `TOPIC_FROZEN`
- `INVALID_MESSAGE`
- `INVALID_TOPIC`

`ErrorCodeOf(err)` is the canonical extraction API for call sites.

### 2.3 Topic / Envelope / Message
- Topic allowlist:
  - `share.proactive.v1`
  - `dm.checkin.v1`
  - `dm.reply.v1`
  - `chat.message`
- `MessageEnvelope` validation:
  - required: `message_id`, `text`, `sent_at`
  - for dialogue topics, `session_id` is required and must be UUIDv7
- `BusMessage` fields:
  - `id`, `direction`, `channel`, `topic`
  - `conversation_key`, `participant_key`
  - `idempotency_key`, `correlation_id`, `causation_id`
  - `payload_base64`, `created_at`
  - `extensions` (typed extension fields)

### 2.4 Conversation Key Rules
- Generic constructor: `BuildConversationKey(channel, id)`.
- Implemented specialized constructors:
  - `BuildTelegramChatConversationKey`
  - `BuildSlackChannelConversationKey`
  - `BuildDiscordChannelConversationKey`
- `tg:@<username>` is not a Telegram delivery conversation key (delivery requires numeric `tg:<chat_id>`).

## 3) Adapter Layer (`internal/bus/adapters`)

### 3.1 Shared Inbound Template
`InboundFlow` standardizes inbound processing:
1. validate input
2. dedupe via inbox (`channel + platform_message_id`)
3. publish with `PublishValidated`
4. persist inbox record

### 3.2 Telegram Adapters
- Inbound (`telegram/inbound.go`): maps Telegram inbound updates to `BusMessage` (`inbound/telegram/chat.message`).
- Delivery (`telegram/delivery.go`): consumes `outbound/telegram` and sends text via resolved target.
- Idempotency key strategy: `idempotency.MessageEnvelopeKey(message_id)`.

### 3.3 Slack Adapters
- Inbound (`slack/inbound.go`): maps Slack Socket Mode events to `BusMessage`.
- Delivery (`slack/delivery.go`): consumes `outbound/slack` and sends message text to target channel/thread.

### 3.4 Demo Adapter
`adapters/demo` remains as a template validation path built on `InboundFlow`.

## 4) Command Wiring

### 4.1 `morph telegram`
- Starts inproc bus using `bus.max_inflight`.
- Subscribes `AllTopics()` and dispatches by `direction + channel`.
- Inbound paths:
  - Telegram polling -> Telegram inbound adapter -> bus -> handler -> worker queue
- Outbound paths:
  - `outbound/telegram` -> Telegram delivery adapter
- Business outputs (task output, task failure, file-download failure, plan updates) are on bus outbound.
- Operational/admin responses (`/help`, bootstrap guidance, etc.) may still use direct send.

### 4.2 `morph slack`
- Starts inproc bus using `bus.max_inflight`.
- Subscribes `AllTopics()` and dispatches by `direction + channel`.
- Inbound path: Slack Socket Mode events -> Slack inbound adapter -> bus -> handler -> worker queue.
- Outbound path: `outbound/slack` -> Slack delivery adapter.

### 4.3 `internal/contactsruntime/sender.go`
- `RoutingSender` starts inproc bus and both delivery adapters internally.
- `Send(...)` always publishes outbound `BusMessage`.
- `publishAndAwait(...)` synchronously waits for delivery completion via a pending map.
- Fail-fast behavior: missing `idempotency_key` is an immediate error; there is no fallback direct-send path.

## 5) State and Idempotency (`contacts`)

### 5.1 Inbox / Outbox Files
- `bus_inbox.json`
  - key semantics: `channel + platform_message_id`
- `bus_outbox.json`
  - key semantics: `channel + idempotency_key`
  - statuses: `pending | sent | failed`
  - fields include `attempts`, `accepted`, `deduped`, `last_error`, `last_attempt_at`, `sent_at`

Note:
- Separate `bus_delivery` storage is removed; delivery state is merged into outbox.

### 5.2 Outbox State Machine
All transitions are centralized in `NextOutboxRecord(...)`:
- `start_attempt`
- `mark_sent`
- `mark_failed`

Fail-fast invariants:
- cannot `start_attempt` from `sent`
- `mark_sent` and `mark_failed` are only valid from `pending`
- `mark_failed` requires non-empty `error_text`
- identity mismatch (`channel/idempotency_key`) is rejected

### 5.3 Idempotency Key Builders
`internal/idempotency/key.go`:
- `ManualContactKey`
- `ProactiveShareKey`
- `MessageEnvelopeKey`

## 6) Simplifications Applied (First-Principles)
- Removed thin bus router wrapper; kept one direct inproc path.
- Reduced bus config surface to `bus.max_inflight` only.
- Topic validation is strict allowlist-based.
- Unified error semantics through typed error codes.
- Replaced untyped metadata map with typed `extensions`.
- Merged outbox/delivery state to avoid dual-source drift.

## 7) Completion Status
Implemented:
- Bus runtime semantics (ordering, backpressure, topology freeze, typed errors)
- Telegram and Slack inbound adapters
- Telegram and Slack delivery adapters
- Bus wiring in Telegram and Slack command paths
- Outbound publish path in `contactsruntime` sender
- Contacts outbox state machine and idempotent send flow
- Typed error code propagation in caller-side logs (`bus_error_code`)
- Cross-channel regression path: Telegram inbound -> Slack outbound

Not implemented yet:
- Discord adapter integration into main runtime path
- External bus backend (for example Redis Streams)
- Retry / DLQ workers
