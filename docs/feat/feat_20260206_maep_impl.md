---
date: 2026-02-06
title: MAEP v1 Implementation Notes (File Store First)
status: draft
---

# MAEP v1 Implementation Details (In Progress)

## 1) Current Scope
Without introducing a database, this phase delivers MAEP v1 core infrastructure first:
- Identity generation and persistence.
- JCS signing and verification for contact cards.
- Contact import validation (`peer_id` / `node_id` / terminal `/p2p/<peer_id>` in multiaddr).
- Basic trust-state transitions (`tofu -> verified`, conflict marked as `conflicted`).
- `/maep/hello/1.0.0` and `/maep/rpc/1.0.0` (one stream per request).
- Minimal RPC method set: `agent.ping` / `agent.capabilities.get` / `agent.data.push`.
- File backend for `dedupe_records` / `protocol_history` / `inbox_messages` / `outbox_messages` / `audit_events`.
- CLI management entry points.

## 2) Storage Abstraction
Code location: `maep/store.go`

Interfaces:
- `Ensure(ctx)`
- `GetIdentity(ctx)` / `PutIdentity(ctx, identity)`
- `GetContactByPeerID(ctx, peerID)`
- `GetContactByNodeUUID(ctx, nodeUUID)`
- `PutContact(ctx, contact)`
- `ListContacts(ctx)`
- `AppendAuditEvent(ctx, event)`
- `ListAuditEvents(ctx, peerID, action, limit)`
- `AppendInboxMessage(ctx, message)`
- `ListInboxMessages(ctx, fromPeerID, topic, limit)`
- `AppendOutboxMessage(ctx, message)`
- `ListOutboxMessages(ctx, toPeerID, topic, limit)`
- `GetDedupeRecord(ctx, fromPeerID, topic, idempotencyKey)`
- `PutDedupeRecord(ctx, record)`
- `PruneDedupeRecords(ctx, now, maxEntries)`
- `GetProtocolHistory(ctx, peerID)`
- `PutProtocolHistory(ctx, history)`

Design intent:
- Protocol logic (`maep/service.go`) depends only on interfaces.
- Adding SQLite/Badger/remote store later must not change business validation paths.

## 3) File Backend
Code location: `maep/file_store.go`

Directory: defaults to `file_state_dir/maep` (override via CLI `--dir`).

Files:
- `identity.json`
- `contacts.json`
- `audit_events.jsonl`
- `inbox_messages.jsonl`
- `outbox_messages.jsonl`
- `dedupe_records.json`
- `protocol_history.json`

Implementation notes:
- File permission `0600`, directory permission `0700`.
- Writes use atomic replacement (temp file + rename) to reduce corruption risk.
- `contacts.json` uses a fixed `version` field for future migrations.
- `inbox/outbox` use a unified envelope shape: `message_id/topic/content_type/payload_base64/idempotency_key/session_id/reply_to + timestamp` (`received_at` for inbox, `sent_at` for outbox).

## 4) Identity And Contact Card
Code locations:
- `maep/identity.go`
- `maep/contact_card.go`
- `maep/jsonprofile.go`

Implemented rules:
- Identity keys: Ed25519.
- `peer_id`: derived from public key via libp2p SDK (custom hash forbidden).
- `node_id = "maep:" + peer_id`.
- `identity_pub_ed25519`: base64url (no padding), must decode to 32 bytes.
- JCS: RFC8785 canonicalization (`jsoncanonicalizer.Transform`).
- Signing input: `"maep-contact-card-v1\n" + canonical_payload`.
- Strict JSON profile: reject `null`, floating-point, and duplicate keys.
- Error-symbol conventions: JSON-RPC parsing violations map to `ERR_INVALID_JSON_PROFILE`; contact-card import structural/semantic violations map to `ERR_INVALID_CONTACT_CARD`.

## 5) CLI Entry Points
Code location: `cmd/mistermorph/maepcmd/maep.go`

Commands:
- `mistermorph maep init`
- `mistermorph maep id`
- `mistermorph maep card export --address ...`
- `mistermorph maep contacts list`
- `mistermorph maep contacts import <contact_card.json|->`
- `mistermorph maep contacts show <peer_id>`
- `mistermorph maep contacts verify <peer_id>`
- `mistermorph maep audit list --limit 100`
- `mistermorph maep inbox list --limit 50`
- `mistermorph maep outbox list --limit 50`
- `mistermorph maep serve`
- `mistermorph maep hello <peer_id>`
- `mistermorph maep ping <peer_id>`
- `mistermorph maep capabilities <peer_id>`
- `mistermorph maep push <peer_id> --text ...`

## 6) Completed / Next
Completed:
- [x] Store abstraction interface
- [x] File store
- [x] Identity generation and persistence
- [x] Contact-card JCS signing and verification
- [x] Contact import validation and conflict marking
- [x] hello negotiation (`/maep/hello/1.0.0`)
- [x] RPC handling (`/maep/rpc/1.0.0`, one request per stream)
- [x] `agent.ping` / `agent.capabilities.get` / `agent.data.push`
- [x] Dedupe file backend (`dedupe_records.json`)
- [x] Protocol-history file backend (`protocol_history.json`)
- [x] Inbox file backend (`inbox_messages.jsonl`)
- [x] Outbox file backend (`outbox_messages.jsonl`)
- [x] Actual `ERR_RATE_LIMITED` enforcement (per peer per minute)
- [x] Local inbox query CLI for `agent.data.push` (`maep inbox list`)
- [x] Local outbox query CLI for `agent.data.push` (`maep outbox list`)
- [x] Dial priority: direct first, relay second (classified by `/p2p-circuit`)
- [x] Audit logs for trust-state/contact operations (`audit_events.jsonl` + `maep audit list`)
- [x] Network and CLI baseline commands
- [x] On RPC parse failure, reply with `ERR_*` if a valid `id` is best-effort extractable; log-only with no response if `id` cannot be extracted

Next phase:
- [ ] Automatic relay discovery and policy-based selection (currently only explicit relay addresses from contacts)
- [ ] Persist connection-quality and address-priority (`last_ok_at`)

## 7) Compatibility Notes
Current implementation aligned with `docs/feat/feat_20260206_maep.md` on:
- `peer_id` / `node_id` definitions.
- Key contact-card import validation checks.
- JCS + domain-separator signing.
- JSON profile restrictions (no `null`, no floating-point).

Current implementation trade-offs:
- Storage lands on files first, with no DB dependency; SQLite remains a future pluggable backend.
- Dedupe uses a rolling window of "7-day TTL + global max 10k records" (processing may happen again after eviction/expiry).
- Protocol constraint: dialogue topics (`share.proactive.v1` / `dm.checkin.v1` / `dm.reply.v1` / `chat.message`) must include `session_id`; `session_id` must be UUIDv7 (plain UUID string, no topic/prefix/suffix). Current behavior validates only format and requiredness, without enforcing session-continuity semantics.
- Storage no longer auto-derives `session_id` from topic, avoiding pseudo-session splits.
- `agent.data.push` is strict envelope-only: `content_type` must start with `application/json`; payload must be envelope JSON (at least `message_id`, `text`, `sent_at`); no transport-layer fallback filling.
- Protocol validation failures are always rejected (`ERR_INVALID_PARAMS`): no automatic plain-text conversion to envelope and no automatic `session_id` generation.
