---
date: 2026-03-08
title: Runtime Prompt Message Order Refactor
status: completed
---

# Runtime Prompt Message Order Refactor

## 1) Goal

- Fix the main inbound runtime prompt shape across channel runtimes.
- Make the latest inbound user message a distinct final user turn.
- Ensure chat history contains only prior context, not the latest inbound message.
- Move runtime metadata ahead of history so the model sees execution context before conversational context.

Target canonical order:

1. `system`
2. `user`: `mister_morph_meta`
3. `user`: `chat_history_messages` for prior history only
4. `user`: `current_message` with an explicit instruction that this is the latest inbound message that must be handled now

## 2) Current Facts

Verified from the current codebase on 2026-03-08:

- `agent.Run(...)` currently builds messages in this order:
  - `system`
  - `opts.History`
  - injected `mister_morph_meta`
  - trailing raw task message unless `SkipTaskMessage` is true
- all current channel runtimes follow the same main-agent pattern:
  - append the current inbound message into history
  - pass that rendered history as `opts.History`
  - set `SkipTaskMessage: true`
- this means the current inbound message is not represented as an explicit last user turn
- Telegram and LINE additionally attach current inbound images to the history message, which means image context is currently coupled to historical context rather than the latest message
- `agent.RunOptions` only supports:
  - `History []llm.Message`
  - raw `task string`
  - `SkipTaskMessage bool`
- there is currently no structured way to pass a distinct last-turn `llm.Message` with text plus optional `Parts`

Affected runtime paths:

- `internal/channelruntime/telegram/runtime_task.go`
- `internal/channelruntime/line/runtime_task.go`
- `internal/channelruntime/slack/runtime_task.go`
- `internal/channelruntime/lark/runtime_task.go`
- global message assembly:
  - `agent/engine.go`

## 3) Problem Statement

The current shape is semantically wrong in three ways:

1. The latest inbound message is mixed into history.
   History should mean "already happened context", not "the current turn to respond to".

2. Metadata arrives after history.
   `mister_morph_meta` is out-of-band execution context and should be available before history is interpreted.

3. The last user turn is missing.
   The model has to infer which message is the one it should answer now, instead of being shown that explicitly.

For image-capable runtimes, there is a fourth problem:

4. Current inbound image parts are attached to the history message.
   Images belong to the latest inbound message, not to historical context.

## 4) Decisions Locked In

### Canonical prompt order

All normal inbound channel runtime tasks should converge to:

1. `system`
2. injected meta message
3. history context message
4. current message

### History meaning

- `chat_history_messages` contains prior conversation only
- it must not include the latest inbound message currently being handled
- it should explicitly say it is historical context only
- if there is no prior history, this message should be omitted entirely

### Current message meaning

- the final user message must explicitly state that it is the latest inbound user message
- it must explicitly instruct the model to handle that message now
- it may reuse the `ChatHistoryItem`-like field shape so speaker, time, ids, and text stay consistent with history

### Image ownership

- Telegram and LINE current inbound images move from the history message to the current message
- history context remains text-only JSON
- current message may carry `llm.Parts`

### Scope boundary

This refactor applies to the main inbound reply path for:

- Telegram
- LINE
- Slack
- Lark

This refactor does not try to redesign:

- init flow prompts
- memory draft prompts
- addressing/group-trigger prompts that already model `current_message` separately
- CLI / daemon direct run UX beyond the safe engine-level ordering change

## 5) First-Principles Constraints

1. Preserve temporal semantics.
   History is previous context. Current message is the message to handle now.

2. Keep runtime metadata out of the conversation timeline.
   Inject meta before history and current message.

3. Make the latest turn explicit.
   Do not rely on the model to infer which item in history is "the one to answer".

4. Keep images attached to the message they belong to.
   Current inbound images stay with current inbound message.

5. Keep the engine change minimal.
   Add one structured last-turn capability to `agent.RunOptions`; do not introduce a prompt DSL or a generic conversation framework.

6. Keep runtime-specific mapping at the edge.
   Each runtime still knows how to convert its inbound event into a `current_message` payload.

7. Re-check for overdesign after each chunk.
   If a helper exists only to look generic, remove it.

## 6) Proposed Prompt Shape

### History context message

Suggested user payload:

```json
{
  "chat_history_messages": [
    {
      "channel": "telegram",
      "kind": "inbound_user",
      "chat_id": "tg:-100123",
      "message_id": "101",
      "sent_at": "2026-03-08T09:00:00Z",
      "sender": {
        "user_id": "42",
        "username": "alice",
        "nickname": "Alice",
        "display_ref": "[Alice](tg:@alice)"
      },
      "text": "Can you summarize this thread?"
    }
  ],
  "note": "Historical messages only. Do not treat them as the latest inbound message."
}
```

### Current message

Suggested final user payload:

```json
{
  "current_message": {
    "channel": "telegram",
    "kind": "inbound_user",
    "chat_id": "tg:-100123",
    "message_id": "102",
    "sent_at": "2026-03-08T09:02:00Z",
    "sender": {
      "user_id": "42",
      "username": "alice",
      "nickname": "Alice",
      "display_ref": "[Alice](tg:@alice)"
    },
    "text": "Hi"
  },
  "instruction": "This is the latest inbound user message. Respond to this message now. Use chat_history_messages only as prior context."
}
```

For image-capable runtimes:

- the text part should contain the JSON payload above
- image parts should be attached to this same final `llm.Message`
- images should not be attached to the history context message

## 7) Proposed Engine Change

`agent.Run` needs one new concept:

- a structured final user message

Recommended shape:

```go
type RunOptions struct {
  Model          string
  History        []llm.Message
  Meta           map[string]any
  CurrentMessage *llm.Message
  OnStream       llm.StreamHandler
  SkipTaskMessage bool
}
```

Behavior:

- `task string` passed to `Run(...)` remains the logical task text used for:
  - `agent.Context`
  - logs
  - file-write extraction
- message assembly changes to:
  - `system`
  - meta
  - history
  - `CurrentMessage` if present
  - else raw task message if `SkipTaskMessage` is false

Why this is the minimum correct engine change:

- preserves existing non-channel callers
- lets runtimes pass a structured last-turn message
- supports image parts where needed
- avoids inventing a broader prompt-composition framework

## 8) Proposed Runtime Change

For each channel runtime main task path:

1. Stop appending current inbound message into history.
2. Build history payload from prior history only.
3. Omit the history message entirely when prior history is empty.
4. Build one explicit `CurrentMessage`.
5. Stop relying on `SkipTaskMessage: true` to suppress a duplicate raw task turn.
6. Pass `CurrentMessage` through `agent.RunOptions`.

### Telegram

- history payload from `history`
- current message payload from `job`
- current inbound images attach to `CurrentMessage`
- history message becomes text-only

### LINE

- same as Telegram for message ordering
- current inbound images attach to `CurrentMessage`

### Slack

- history payload from `history`
- current message payload from `job`
- text-only current message

### Lark

- history payload from `history`
- current message payload from `job`
- text-only current message in V1

## 9) Shared Helper Strategy

Keep shared helpers small.

Allowed shared helpers:

- render history-context JSON payload
- render current-message JSON payload

Not allowed in this refactor:

- a generic prompt-builder framework
- channel-agnostic runtime abstractions
- a large "conversation envelope" subsystem

If a helper only saves one call site, keep it local instead.

## 10) Mainline Task Breakdown

### 0. Docs and decision freeze

- [x] Write this document
- [x] Freeze canonical order:
  - [x] `system`
  - [x] `meta`
  - [x] `history_without_current`
  - [x] `current_message`
- [x] Freeze the rule that current images belong to current message, not history
- [x] Freeze the rule that the final user message must explicitly say it is the latest inbound message

Acceptance criteria:

- [x] No implementation chunk later reintroduces current inbound content into `chat_history_messages`

### 1. Engine support

- [x] Add `CurrentMessage *llm.Message` to `agent.RunOptions`
- [x] Change `agent.Run` assembly order to:
  - [x] system
  - [x] meta
  - [x] history
  - [x] current message or raw task fallback
- [x] Keep existing non-runtime callers backward compatible
- [x] Keep `SkipTaskMessage` as compatibility glue for callers that still need raw task suppression

Acceptance criteria:

- [x] A unit test proves meta appears before history
- [x] A unit test proves `CurrentMessage` becomes the final user message
- [x] Existing direct run paths still work without passing `CurrentMessage`

### 2. Shared render helpers

- [x] Add a small shared helper for history-context payload rendering
- [x] Add a small shared helper for current-message payload rendering
- [x] Ensure the instruction string is explicit and stable

Acceptance criteria:

- [x] Rendered history payload explicitly says it is historical only
- [x] Rendered current payload explicitly says it is the latest inbound message to handle now

### 3. Telegram migration

- [x] Remove `historyWithCurrent` from `runTelegramTask`
- [x] Build history from prior history only
- [x] Build `CurrentMessage` from `telegramJob`
- [x] Move image parts from history message to current message
- [x] Stop using `SkipTaskMessage: true` in this path once `CurrentMessage` is in place

Acceptance criteria:

- [x] Telegram unit tests prove current text is not inside history
- [x] Telegram unit tests prove current text is the last user message payload
- [x] Telegram unit tests prove current images are attached to current message only

### 4. LINE migration

- [x] Remove `historyWithCurrent` from `runLineTask`
- [x] Build `CurrentMessage` from `lineJob`
- [x] Move image parts from history message to current message

Acceptance criteria:

- [x] LINE unit tests prove current text is not inside history
- [x] LINE unit tests prove current images are attached to current message only

### 5. Slack migration

- [x] Remove `historyWithCurrent` from `runSlackTask`
- [x] Build `CurrentMessage` from `slackJob`

Acceptance criteria:

- [x] Slack unit tests prove current text is not inside history
- [x] Slack unit tests prove current text is the last user message payload

### 6. Lark migration

- [x] Remove `historyWithCurrent` from `runLarkTask`
- [x] Build `CurrentMessage` from `larkJob`

Acceptance criteria:

- [x] Lark unit tests prove current text is not inside history
- [x] Lark unit tests prove current text is the last user message payload

### 7. Tests and regression coverage

- [x] Add `agent.Run` ordering tests
- [x] Add per-runtime tests for payload separation
- [x] Add image-placement tests for Telegram and LINE
- [x] Re-run `go test ./...`

Acceptance criteria:

- [x] No runtime test still assumes current message is embedded in history
- [x] Image-capable runtime tests prove images moved to current message

## 11) Implementation Outcome

Implemented on 2026-03-08:

- `agent.RunOptions` now supports `CurrentMessage`
- `agent.Run` now assembles messages as:
  - `system`
  - `mister_morph_meta`
  - `history`
  - `current_message` or raw task fallback
- shared `internal/chathistory` helpers now render:
  - history-context payloads
  - current-message payloads
- empty history now results in no history message at all
- Telegram / LINE / Slack / Lark main runtime task paths now:
  - keep history free of the current inbound message
  - pass an explicit final current-message user turn
- Telegram and LINE now attach current inbound image parts to the current message instead of the history message

## 12) Risks

- Prompt token distribution will change.
  Main-agent behavior may improve, but threshold-sensitive behavior could shift.

- Some implicit prompt assumptions may be relying on the old malformed layout.
  Prompt inspection should be used during rollout.

- Telegram and LINE image handling is easy to regress.
  If image parts are dropped while moving them off history, multimodal behavior will silently degrade.

## 13) Compression Review

This refactor is bigger than a one-file patch, but it is still the minimum necessary shape.

Why it is not overdesigned:

- one engine-level capability is missing today: a structured last-turn message
- four runtimes share the same prompt-ordering bug
- image ownership must be corrected where applicable

What to avoid while implementing:

- do not invent a prompt pipeline framework
- do not create per-runtime custom message-order options
- do not redesign addressing or memory flows in the same PR

Current judgment:

- document first: yes
- global runtime fix: yes
- broad abstraction layer: no
- final implementation: still minimal, no prompt framework introduced
