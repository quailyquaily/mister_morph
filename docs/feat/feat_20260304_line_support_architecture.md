---
date: 2026-03-04
title: LINE Support Architecture Plan (Aligned with Telegram/Slack)
status: implemented
---

# LINE Support Architecture Plan (Aligned with Telegram/Slack)

## Progress Snapshot (2026-03-05)

- Completed: PR-1 ~ PR-10
- Regression: `go test ./...` passed on 2026-03-05

Regression record:
- `go test ./...`
- `go test ./internal/channelruntime/telegram ./internal/channelruntime/slack ./internal/channelruntime/line`

## 1) Scope

- Add a new long-running channel runtime: `mistermorph line`.
- Keep the same core pipeline as existing Telegram/Slack:
  - inbound event -> bus -> per-conversation worker -> `run*Task` -> outbound bus -> delivery adapter.
- Support LINE **group + private chat** in V1.
- Message capability in V1: `text + message_react + image multimodal input`.
- Reuse shared group-trigger decision logic (`strict|smart|talkative`) and shared addressing LLM decision path.

## 2) Goals

- Reach feature parity baseline with existing Telegram/Slack runtime architecture.
- Avoid introducing a third special-case architecture.
- Keep bus, contacts routing, memory, and prompt profile integration consistent with current channels.
- Keep outbound robust with both `reply` and `push`.

## 3) Non-Goals (V1)

- No new generic bus backend (still inproc).
- No rich message template system (carousel/flex/sticker packs) in V1.
- No support for LINE room chat in V1.
- No audio/video/file multimodal in V1.

## 4) First-Principles Design Constraints

1. One execution core:
   Use the same `agent.Engine` + runtime task flow as Telegram/Slack.
2. One transport abstraction:
   Keep channel-specific logic in `internal/channelruntime/line` and `internal/bus/adapters/line`.
3. One trigger model:
   Group decision semantics must remain `strict|smart|talkative` with shared decision code.
4. Keep defaults conservative:
   If capability is unclear, fail safe with explicit logs and fallback behavior.

## 5) LINE Platform Facts to Respect

- Inbound is webhook-based (HTTP callback), not polling/socket.
- Outbound needs both reply/push:
  - `reply` uses webhook `replyToken` and is time-limited.
  - `push` is fallback when `reply` is unavailable or expired.
- Webhook signature verification is mandatory.
- LINE has group/room/user chat types. V1 supports `group` and `user(private)`, and ignores `room`.

Implication for architecture:
- `mistermorph line` should run an HTTP webhook listener + worker loop in one process, similar to how `mistermorph slack` runs socket + worker loop.

## 6) Target Architecture (Code-Aligned)

```text
LINE Webhook (HTTP)
  -> line inbound normalize + verify + dedupe
  -> bus.PublishValidated(chat.message)
  -> line runtime inbound handler
  -> per-conversation worker
  -> runLineTask (agent.Engine)
  -> bus outbound publish
  -> line delivery adapter (reply or push)
  -> LINE Messaging API
```

Planned modules (mirroring current channel layout):

- `cmd/mistermorph/linecmd/*`
- `internal/channelruntime/line/*`
- `internal/bus/adapters/line/inbound.go`
- `internal/bus/adapters/line/delivery.go`

Cross-module updates:

- `internal/channels/channels.go`: add `line`.
- `internal/bus/message.go` and `conversation_key.go`: add `ChannelLine` and key builder.
- `internal/chathistory/types.go`: add `ChannelLine`.
- `contacts/*` + `internal/contactsruntime/sender.go`: add line routing.
- `internal/promptprofile/prompts/block_line.md`: add LINE-specific runtime block.
- `tools/line/react_tool.go` (or equivalent): register `message_react` for LINE runtime.

## 7) Conversation and Identity Keys

V1 key rules (minimal and deterministic):

- `conversation_key`: `line:<chat_id>`
- `conversation_type`: `group|private` in V1 (`room` rejected).
- `participant_key`: raw user id string.
- `contact_id` hint format for tools/routing:
  - group/private target: `line:<chat_id>`
  - user identity (speaker/contact profile): `line_user:<user_id>` (planned profile field)

Rationale:
- Match existing style (`tg:<id>`, `slack:<...>`) and keep one routing key.

## 8) Trigger and Addressing Alignment

Group:
- same `group_trigger_mode` semantics as Telegram/Slack:
  - `strict`: explicit trigger only
  - `smart`: addressed + confidence threshold
  - `talkative`: interject + interject threshold
- reuse shared LLM decision helper under `internal/grouptrigger`.

Explicit trigger inputs for LINE group (V1):
- mention metadata when available,
- configured command prefix,
- reply-to-bot signal is reserved, but currently not enabled in runtime because webhook payload path does not provide a stable reply-target identity field.

Lightweight/reaction rule in V1:
- register `message_react` in both addressing and main task flow (same shape as Telegram/Slack).
- LINE prompt block must include reaction rules and valid emoji guidance.

## 9) Outbound Policy

Delivery adapter behavior:

- Support both `reply` and `push`.
- Prefer `reply` when valid reply token exists.
- Fallback to `push` when reply token is absent/expired or reply call fails with token-related error.
- Keep this choice in adapter, not in agent logic.

Bus behavior:

- Same as Slack/Telegram: canonical outbound content goes through bus.
- No stream-delta transport over bus in V1.

## 10) Configuration Surface (Planned)

```yaml
line:
  base_url: "https://api.line.me"
  channel_access_token: ""
  channel_secret: ""
  webhook_listen: "127.0.0.1:18080"
  webhook_path: "/line/webhook"
  allowed_group_ids: []
  group_trigger_mode: "smart"
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s"
  max_concurrency: 3
```

Notes:
- `allowed_group_ids` follows the same allowlist spirit used by Telegram/Slack.
- timeout/concurrency defaults should match existing channel defaults unless LINE requires stricter limits.
- LINE image caching uses global `file_cache_dir` (stored under `file_cache_dir/line`).
- LINE image input is default-on.

## 11) Prompt Profile Integration

- Add a LINE runtime block template:
  - `internal/promptprofile/prompts/block_line.md`
- Register via prompt profile appender in line runtime task (same pattern as Telegram/Slack block injection).
- Include LINE-specific instructions for:
  - group/private operation,
  - `message_react`,
  - reply/push behavior expectations.

## 12) Implementation Plan

### Phase A: Foundations

- Add channel constants, key builders, and minimal config defaults.
- Add bus validation support for `line` channel.
- Add line inbound/outbound adapter skeletons with tests.

Acceptance:
- Compile + tests for channel constants and bus key handling pass.

### Phase B: Runtime loop + webhook ingest

- Add `cmd/mistermorph line` command.
- Implement webhook listener -> inbound adapter -> bus publish.
- Implement `internal/channelruntime/line` worker loop and `runLineTask`.
- Implement outbound delivery adapter with `reply` + `push` support and fallback.

Acceptance:
- End-to-end text echo path works in local mock integration.

### Phase C: Group trigger parity

- Wire group trigger mode and addressing thresholds for LINE.
- Reuse shared decision helper and history injection strategy.
- Add LINE prompt block injection.
- Register `message_react` in both addressing flow and main task flow.

Acceptance:
- Trigger behavior matches documented `strict|smart|talkative` semantics.
- Lightweight reaction flow works without forcing text output.

### Phase D: Multimodal image input

- Parse LINE inbound image messages and cache locally (reuse file-cache patterns).
- Build `llm.Message.Parts` (`text + image_base64`) in line task history message.
- Reuse existing model capability gate.

Acceptance:
- Supported models can read inbound LINE images in group tasks.
- Image size/count guardrails follow existing runtime policy.

### Phase E: Contacts + sender parity

- Extend contacts observe/routing to include line.
- Extend `contactsruntime` sender to publish outbound line bus messages.
- Support `chat_id=line:<chat_id>` in routing paths where applicable.

Acceptance:
- contacts outbound path can route to LINE same as Telegram/Slack.

## 13) Test Plan

- Unit:
  - line webhook signature validation
  - inbound normalization and idempotency key mapping
  - delivery adapter reply/push fallback selection
  - reaction tool schema + execution path
  - conversation key and participant key rules
- Runtime:
  - worker serialization by `conversation_key`
  - group trigger mode matrix
  - prompt block injection matrix
  - multimodal image-to-parts conversion
  - task timeout/concurrency fallback behavior
- Cross-channel regression:
  - `go test ./...` with no behavior regressions on Telegram/Slack.

## 14) Risks and Controls

- Risk: webhook replay/duplicate events.
  - Control: keep inbox dedupe (`channel + platform_message_id`) strict.
- Risk: reply context expires before async handling.
  - Control: adapter fallback to push with explicit token-expired classification logs.
- Risk: image payload too large.
  - Control: enforce size/count limits before LLM upload.
- Risk: group trigger behavior drift across channels.
  - Control: reuse shared decision path and shared threshold semantics.

## 15) Deliverable Checklist

- [x] `mistermorph line` command runnable.
- [x] line inbound adapter + delivery adapter implemented.
- [x] line runtime task integrated with engine and bus.
- [x] group trigger parity (`strict|smart|talkative`).
- [x] `message_react` available in LINE addressing + main task path.
- [x] LINE inbound image multimodal path implemented.
- [x] contacts routing parity for line.
- [x] docs update: `docs/arch.md`, `docs/bus.md`, `docs/tools.md` (line sections).

## 16) Concrete Work Checklist (Task Split)

### PR-1: Channel and config plumbing

- [x] Add `line` channel constant and expose it in shared channel enums:
  - `internal/channels/channels.go`
  - `internal/bus/message.go` (`ChannelLine` + validation allowlist)
  - `internal/chathistory/types.go` (`ChannelLine`)
- [x] Add config defaults:
  - `cmd/mistermorph/defaults.go` (`line.*` defaults)
  - `assets/config/config.example.yaml` (`line` section)
- [x] Add config parsing/builders:
  - `internal/channelopts/options.go` (`LineConfig`, `LineInput`, `BuildLineRunOptions`)
  - `internal/channelopts/options_test.go`

### PR-2: Bus conversation key + adapter contracts

- [x] Add line conversation key helper:
  - `internal/bus/conversation_key.go` (`BuildLineGroupConversationKey` or equivalent)
  - `internal/bus/conversation_key_test.go`
- [x] Add adapter package skeleton:
  - `internal/bus/adapters/line/inbound.go`
  - `internal/bus/adapters/line/delivery.go`
  - `internal/bus/adapters/line/*_test.go`
- [x] Enforce V1 chat-type guard in inbound adapter:
  - accept `group|private`
  - drop `room` with explicit handling

### PR-3: LINE webhook runtime loop

- [x] Add command entry:
  - `cmd/mistermorph/linecmd/*`
  - register in `cmd/mistermorph/root.go`
- [x] Implement HTTP webhook server:
  - signature verification
  - event normalization
  - inbound adapter publish
- [x] Implement runtime worker loop:
  - per-conversation serialization by `conversation_key`
  - bounded queue/backpressure behavior aligned with Telegram/Slack runtime patterns.

### PR-4: LINE outbound delivery (`reply` + `push`)

- [x] Implement LINE API client methods for:
  - reply message
  - push message
- [x] Delivery adapter policy:
  - prefer `reply` when reply token exists
  - fallback to `push` on token-missing/token-expired/token-invalid class errors
- [x] Add error classification + logging fields for fallback path.

### PR-5: Main task runtime + LINE prompt block

- [x] Implement `runLineTask`:
  - prompt assembly
  - agent engine execution
  - outbound bus publish path
- [x] Add LINE runtime prompt template:
  - `internal/promptprofile/prompts/block_line.md`
- [x] Add prompt block appender wiring in line runtime task.
- [x] Add run metadata keys (for logs/prompt context), for example:
  - `trigger=line`
  - `line_chat_id`
  - `line_user_id`
  - `line_message_id`

### PR-6: Group trigger parity (`strict|smart|talkative`)

- [x] Add LINE trigger flow module (mirroring telegram/slack trigger wiring).
- [x] Reuse `internal/grouptrigger/decision.go` and shared addressing prompt rendering.
- [x] Add explicit trigger extraction for LINE group events:
  - mention signal
  - command prefix signal
  - reply-to-bot signal is intentionally not enabled yet (current webhook path has no stable reply-target identity field)
- [x] Add trigger-mode matrix tests for LINE group messages.

### PR-7: `message_react` support (addressing + main task)

- [x] Implement LINE reaction tool:
  - `tools/line/react_tool.go`
  - tool name must stay `message_react` for cross-channel prompt consistency
- [x] Register reaction tool in LINE main task registry.
- [x] Register reaction tool in LINE addressing flow (pre-run addressing LLM path).
- [x] Add tests:
  - tool schema and execute path
  - lightweight reaction behavior in addressing/main run paths
  - reaction + text publish policy compatibility.

### PR-8: LINE image multimodal input

- [x] Inbound image handling:
  - fetch image bytes from LINE content endpoint
  - store in file cache
  - enforce size/count limits before upload
- [x] Build history/user message `llm.Parts` (`text + image_base64`) in `runLineTask`.
- [x] Enable LINE image input by default.
- [x] Reuse existing model capability gate and add LINE runtime tests.

### PR-9: Contacts and sender parity

- [x] Extend contacts observe path for line inbound bus messages:
  - `contacts/bus_observe.go` (+ tests)
- [x] Extend contacts profile parsing/store for line identifiers (group + user mapping fields).
- [x] Extend sender routing:
  - `internal/contactsruntime/sender.go`
  - support `chat_id=line:<chat_id>`
- [x] Add end-to-end sender bus tests for line route selection.

### PR-10: Documentation and regression closure

- [x] Update docs:
  - `docs/arch.md`
  - `docs/bus.md`
  - `docs/tools.md`
  - `docs/line.md` (already added and updated; keep syncing with runtime changes)
- [x] Add/refresh dump-based examples for LINE prompt/request inspection.
- [x] Run and record regression suite:
  - `go test ./...`
  - focused channel suites (telegram/slack/line) with no regressions.
