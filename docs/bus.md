---
date: 2026-03-04
title: Multi-Channel Messaging Architecture (Bus + Adapters)
status: implemented
---

# Multi-Channel Messaging Architecture (Code-Aligned)

## 1) Purpose and Positioning
This document is a code-aligned architecture snapshot of the current bus-based multi-channel design in `mistermorph`.

Authoring rules:
- Code is the source of truth.
- Implemented behavior and boundaries are described explicitly.
- Planned work is kept in backlog sections and not mixed into current-state sections.

## 2) Current Architecture Snapshot

### 2.1 Bus Runtime (`internal/bus`)
Current mode: `inproc` only.

Core behavior:
- Startup entrypoint: `StartInproc(...)`.
- Concurrency model: fixed shard workers + `hash(conversation_key)` routing.
- Bounded backpressure:
  - `max_inflight` controls token pool and shard queue capacity.
  - publish timeout (`context.DeadlineExceeded`) is surfaced as `QUEUE_FULL`.
- Topology freeze: once first publish happens, further subscription changes are rejected.
- Single handler per topic: duplicate subscription for a topic is an error.
- Publish APIs:
  - `PublishValidated`: boundary entrypoint with message validation
  - `Publish`: internal fast path assuming boundary validation is already done

### 2.2 Error Model (`internal/bus/errors.go`)
Implemented typed error codes:
- `BUS_CLOSED`
- `NO_SUBSCRIBER`
- `QUEUE_FULL`
- `TOPIC_ALREADY_HANDLED`
- `TOPIC_FROZEN`
- `INVALID_MESSAGE`
- `INVALID_TOPIC`

Callers use `ErrorCodeOf(err)` for uniform classification.

### 2.3 Message Model (`internal/bus/message.go`)
Current `BusMessage` shape:
- `id`
- `direction`
- `channel`
- `topic`
- `conversation_key`
- `participant_key`
- `idempotency_key`
- `correlation_id`
- `causation_id` (optional)
- `payload_base64`
- `created_at`
- `extensions` (typed extension fields)

### 2.4 Topic and Envelope
Registered topic allowlist:
- `share.proactive.v1`
- `dm.checkin.v1`
- `dm.reply.v1`
- `chat.message`

`MessageEnvelope` fields:
- `message_id`
- `text`
- `sent_at` (RFC3339)
- `session_id` (required for dialogue topics; must be UUIDv7)
- `reply_to` (optional)

### 2.5 Conversation Key Rules
Implemented constructors:
- Telegram chat: `tg:<chat_id>`
- Slack channel: `slack:<team_id>:<channel_id>`
- LINE chat: `line:<chat_id>`
- Discord channel: `discord:<channel_id>`

Notes:
- `tg:@<username>` is not a Telegram delivery conversation key (delivery requires numeric `tg:<chat_id>`).
- Slack runtime uses `BuildSlackChannelConversationKey(team_id + ":" + channel_id)` to avoid cross-team channel collisions.
- Discord constructor exists, while runtime adapter integration is not yet implemented.

### 2.6 Adapter Layer (`internal/bus/adapters/*`)

Shared inbound template:
1. validate input
2. dedupe via inbox (`channel + platform_message_id`)
3. `PublishValidated` to bus
4. persist inbox record

Implemented channel adapters:
- Telegram:
  - inbound: `internal/bus/adapters/telegram/inbound.go`
  - delivery: `internal/bus/adapters/telegram/delivery.go`
- Slack:
  - inbound: `internal/bus/adapters/slack/inbound.go`
  - delivery: `internal/bus/adapters/slack/delivery.go`
- LINE:
  - inbound: `internal/bus/adapters/line/inbound.go`
  - delivery: `internal/bus/adapters/line/delivery.go`

### 2.7 Command Wiring

`morph telegram`:
- starts inproc bus
- subscribes `AllTopics()` with one dispatcher by `direction + channel`
- Telegram poll inbound path: Telegram inbound adapter -> bus -> handler -> worker queue
- business outbound messages via bus: final text publish path, task failures, file-download failures, and plan updates
- operational/admin messages (for example `/help`, initialization/system notices) may still use direct send

`morph slack`:
- starts inproc bus
- subscribes `AllTopics()` with one dispatcher by `direction + channel`
- inbound path: Slack Socket Mode event -> Slack inbound adapter -> bus -> handler -> per-conversation worker
- task output and error replies are published as outbound bus messages, then delivered by Slack delivery adapter

`morph line`:
- starts inproc bus
- subscribes `AllTopics()` with one dispatcher by `direction + channel`
- inbound path: LINE webhook event -> LINE inbound adapter -> bus -> handler -> per-conversation worker
- task output and error replies are published as outbound bus messages, then delivered by LINE delivery adapter (`reply` first, fallback `push`)

`internal/contactsruntime/sender.go`:
- `NewRoutingSender` starts inproc bus and delivery adapters internally
- `Send(...)` routes to Telegram / Slack / LINE, then publishes outbound `BusMessage`
- `publishAndAwait(...)` uses a pending map for synchronous delivery result waiting
- fail-fast: missing `idempotency_key` is an immediate error

### 2.8 State and Idempotency (`contacts`)

Inbox:
- storage: `bus_inbox.json`
- key: `channel + platform_message_id`

Outbox (single state machine):
- storage: `bus_outbox.json`
- key: `channel + idempotency_key`
- statuses: `pending | sent | failed`
- fields include: `attempts`, `accepted`, `deduped`, `last_error`, `sent_at`, `last_attempt_at`

### 2.9 Slack Thread Handling

Thread metadata is carried in bus message payload/extension fields, not in routing keys.

Inbound mapping (`slack event -> bus`):
- source: Slack `thread_ts`
- writes to:
  - `MessageEnvelope.reply_to`
  - `BusMessage.extensions.reply_to`
  - `BusMessage.extensions.thread_ts`

Outbound mapping (`runtime output -> bus -> slack delivery`):
- publish side writes both `extensions.thread_ts` and `extensions.reply_to` (and `envelope.reply_to`).
- delivery side resolves thread target by priority:
  1. `extensions.thread_ts`
  2. `extensions.reply_to`
  3. `envelope.reply_to`
- resolved value is sent as Slack `thread_ts`.

Ordering/routing boundary:
- bus sharding is by `conversation_key` (`slack:<team_id>:<channel_id>`).
- thread is not part of `conversation_key`.
- therefore different threads in the same Slack channel are serialized on the same per-conversation worker.

## 3) ASCII Architecture Views

### 3.1 End-to-End Runtime View

```text
Telegram poll/getUpdates ----\
Slack Socket Mode ------------+--> inbound adapters (telegram/slack/line)
LINE Webhook (HTTP) ---------/
                                           v
                                +----------------------+
                                | inproc bus runtime   |
                                | shards by            |
                                | hash(conversation)   |
                                +----------+-----------+
                                           |
                                           v
                    +----------------------+----------------------+
                    |                                             |
         inbound bus handler + worker queue             outbound bus handler
                    |                                             |
                    v                                             v
              run task / agent                            delivery adapters
                    |                           (telegram / slack / line)
                    +-------------------------+-------------------+
                                              |
                                              v
                            Telegram / Slack / LINE send
```

### 3.2 Shared Inbound Path (`InboundFlow`)

```text
platform event
  -> normalize adapter input
  -> validate required fields
  -> dedupe check by inbox key: (channel, platform_message_id)
  -> PublishValidated(bus message)
  -> persist inbox seen record
```

### 3.3 Shared Outbound Path

```text
business output / proactive sender
  -> build outbound BusMessage (+ idempotency_key)
     (slack thread is carried by thread_ts/reply_to fields)
  -> PublishValidated
  -> bus handler routes by channel
  -> DeliveryAdapter.Deliver(...)
  -> caller records outbox state (pending/sent/failed)
```

Telegram note:
- Telegram outbound messages are currently delivered through canonical bus-driven send paths.

## 4) Design Evaluation (Current State)

### 4.1 Why this is reasonable now
- Telegram / Slack inbound paths are unified through bus; LINE inbound webhook path is now on the same bus flow.
- Canonical outbound messages are bus-driven.
- Ordering is explicit and enforceable per `conversation_key`.
- Backpressure is bounded and observable via typed error (`QUEUE_FULL`).
- Idempotency is explicit via inbox/outbox keyed records.
- Validation is fail-fast across boundary and state transitions.

### 4.2 Explicit boundaries (intentional for this stage)
- Single-process runtime bus (not a cross-process durable message system).
- Single handler per topic (no fan-out multi-consumer topology).
- No built-in retry worker / DLQ.
- Discord is still partially prepared (channel constants + key builder), not integrated into runtime path.

## 5) Delivery Status

### 5.1 Completed
- Telegram inbound/delivery adapters
- Slack inbound/delivery adapters
- LINE inbound/delivery adapters
- outbound bus migration in `contactsruntime sender`
- Slack runtime bus path (`morph slack`) with inbound publish and outbound delivery
- LINE runtime bus path (`morph line`) with webhook inbound publish and outbound delivery
- main-path test coverage expansion
- Telegram business outbound via bus for canonical events
- typed error code propagation in call-site logging

### 5.2 Verified test set
Verified locally on `2026-03-04`:
- `go test ./internal/bus/...`
- `go test ./internal/bus/adapters/...`
- `go test ./internal/contactsruntime/... ./contacts/...`
- `go test ./cmd/mistermorph/telegramcmd ./cmd/mistermorph/slackcmd ./cmd/mistermorph/linecmd`
- `go test ./...`

## 6) Backlog (Separated from Current State)

### 6.1 Near-term
- Add fuller end-to-end runbook and verification notes for `contacts proactive tick --send`.
- Add sampling strategy for high-frequency debug log points.
- Evaluate making inbox optional for outbound-only deployment modes.

### 6.2 Mid/Far-term
- External bus backend option (candidate: Redis Streams).
- Retry / DLQ worker model.
- Discord runtime adapters and full-path integration.

## 7) Configuration
Current bus config surface:

```yaml
bus:
  max_inflight: 1024
```

---

Update policy for this file:
When implementation changes, update Section 2 (current snapshot) and Section 6 (backlog) first to keep architecture docs code-aligned.
