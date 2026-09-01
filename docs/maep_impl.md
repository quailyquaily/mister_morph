---
date: 2026-02-08
title: MAEP v1 Implementation Notes (Code-Aligned)
status: implemented
---

# MAEP v1 Implementation Details (Code-Aligned)

## 1) Scope
This document records the current implemented behavior of MAEP v1.

Scope:
- `maep/*` protocol and storage implementation.
- `cmd/mistermorph/maepcmd` runtime wiring.
- MAEP inbound integration points with the in-process bus.

Out of scope:
- Generic multi-channel bus architecture and its evolution plan (see `docs/bus.md`).

## 2) Protocol Behavior Snapshot

### 2.1 Implemented protocol surface
- Protocol IDs:
  - `/maep/hello/1.0.0`
  - `/maep/rpc/1.0.0`
- Allowed methods:
  - `agent.ping`
  - `agent.capabilities.get`
  - `agent.data.push`
- RPC framing: one stream per request (write request, half-close, read one response).

### 2.2 `agent.data.push` constraints
- Required params: `topic`, `content_type`, `payload_base64`, `idempotency_key`.
- `content_type` must start with `application/json`.
- `payload_base64` must be base64url (no padding), then decode to envelope JSON.
- Dialogue topics (`share.proactive.v1`, `dm.checkin.v1`, `dm.reply.v1`, `chat.message`) require `session_id`, and `session_id` must be UUIDv7.
- No transport fallback:
  - no auto conversion from plain text to envelope
  - no auto-generated `session_id`

### 2.3 Limits and guards
- `max_rpc_request_bytes = 256 KiB`
- `max_payload_bytes = 128 KiB`
- `hello_timeout = 3s`
- `rpc_timeout_default = 10s`
- `rate_limit_data_push_per_peer = 120/min`
- Dedupe:
  - key tuple: `(from_peer_id, topic, idempotency_key)`
  - default TTL: 7 days
  - default cap: 10k records (oldest evicted first)

### 2.4 Error behavior
- RPC parsing/validation errors map to `ERR_*` symbols.
- For invalid RPC request:
  - if a valid `id` can be best-effort extracted, respond with `ERR_*`
  - otherwise log only, no response.

## 3) Storage Abstraction
Code location: `maep/store.go`

Store interfaces implemented:
- identity/contact CRUD
- audit append/list
- inbox/outbox append/list
- dedupe get/put/prune
- protocol history get/put

Design constraint:
- Protocol logic depends on the `Store` interface, enabling future backend replacement without changing protocol semantics.

## 4) File Backend
Code location: `maep/file_store.go`

Default directory: `file_state_dir/maep` (or `--dir`).

Current files:
- `identity.json`
- `contacts.json`
- `audit_events.jsonl`
- `inbox_messages.jsonl`
- `outbox_messages.jsonl`
- `dedupe_records.json`
- `protocol_history.json`

Implementation notes:
- Directory permission `0700`, file permission `0600`.
- JSON files are written via atomic replacement.
- JSONL files are append-oriented.

## 5) Runtime Integration (MAEP + Bus)

### 5.1 `mistermorph maep serve`
Current wiring:
1. start MAEP node
2. start in-process bus (`bus.max_inflight`)
3. `OnDataPush -> maep inbound adapter -> bus.PublishValidated`
4. bus handler converts back to event and executes command-level behavior (printing + optional contacts sync)

### 5.2 `morph telegram --with-maep`
Current wiring:
1. embedded MAEP node enabled by `--with-maep`
2. `OnDataPush -> maep inbound adapter -> bus`
3. bus dispatcher routes by `direction + channel`

### 5.3 State-plane separation
There are two independent state planes:
- MAEP protocol state (`maep/*` store): RPC-level inbox/outbox/dedupe/protocol history.
- Multi-channel bus state (`contacts/*` store): `bus_inbox` and `bus_outbox` for channel adapters.

They are intentionally separate and serve different layers.

## 6) CLI Surface
Code location: `cmd/mistermorph/maepcmd/maep.go`

Implemented commands:
- `maep init`
- `maep id`
- `maep card export`
- `maep contacts list/import/show/verify`
- `maep audit list`
- `maep inbox list`
- `maep outbox list`
- `maep serve`
- `maep hello`
- `maep ping`
- `maep capabilities`
- `maep push`

## 7) Completion And Backlog

### 7.1 Implemented
- identity generation/persistence
- contact card signing/verification/import validation
- trust state transitions and conflict marking
- hello negotiation and rpc handling
- `agent.data.push` strict envelope validation
- dedupe/rate-limit enforcement
- inbox/outbox/audit persistence and query commands
- MAEP inbound bus integration in `maep serve` and `telegram --with-maep`

### 7.2 Backlog
- automatic relay discovery and selection policy
- persistent address quality / priority (`last_ok_at`-style ranking)
- optional stricter operational tooling around downgrade alerts

## 8) Compatibility Notes
- MAEP v1 protocol constraints remain strict and fail-fast.
- Unknown-field compatibility policy remains protocol-level design guidance; no fallback behavior should weaken required field checks.
- Bus evolution (backend/retry/DLQ/Slack/Discord adapters) is tracked in the bus architecture doc, not this file.
