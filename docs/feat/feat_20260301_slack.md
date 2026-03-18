---
date: 2026-03-01
title: Slack vs Telegram Feature Gap Checklist
status: draft
---

# Slack vs Telegram Feature Gap Checklist

## 1) Scope

- Baseline for comparison: capabilities currently implemented in `mistermorph telegram`.
- Target for alignment: current `mistermorph slack` implementation.
- This document lists only *remaining or tracked alignment items* and does not repeat already-finished foundations (for example bus pipeline, contacts routing, baseline group-trigger classification, baseline memory integration).

## 2) Latest Priority (Reconfirmed on 2026-03-01)

### Mainline (Execute in Order)

- [x] 0. Bus identity enrichment: fill Slack inbound `username` / `display_name`
- Goal:
  - Align with Telegram-level sender identity richness, improve readability for contacts/history/memory.
- Current anchors:
  - Slack inbound events are user-id centric: `internal/channelruntime/slack/socket_events.go`
  - Bus extensions already include `FromUsername` / `FromDisplayName`: `internal/bus/adapters/slack/inbound.go`

- [x] 1. Emoji reaction parity (`message_react`)
- Goal:
  - Provide channel-tool reaction capability aligned with Telegram `message_react`.
- Current anchors:
  - Telegram reaction + lightweight path: `internal/channelruntime/telegram/trigger.go`, `tools/telegram/react_tool.go`
  - Slack reaction tool + runtime wiring: `tools/slack/react_tool.go`, `internal/channelruntime/slack/runtime_task.go`

- [x] 2. Slack presentation for `plan_create`
- Goal:
  - Align with Telegram plan-progress visibility while using Slack-appropriate rendering.
- Current anchors:
  - Telegram has `WithPlanStepUpdate(...)` + progress publishing: `internal/channelruntime/telegram/runtime_task.go`
  - Slack now has plan progress hook wiring: `internal/channelruntime/slack/runtime_task.go`

- [x] 3. Channel-specific prompt block (Slack)
- Goal:
  - Add Slack-specific policy block (thread etiquette, mention semantics, output constraints).
- Current anchors:
  - Prompt block assembly: `internal/promptprofile/prompt_blocks.go`
  - Slack block template: `internal/promptprofile/prompts/block_slack.md`

- [x] 4. Heartbeat support for Slack
- Goal:
  - Support combined channel runtime + heartbeat operation, same as Telegram mode.
- Current anchors:
  - Telegram command includes optional heartbeat combined run: `cmd/mistermorph/telegramcmd/command.go`
  - Slack command includes optional heartbeat combined run: `cmd/mistermorph/slackcmd/command.go`

### P2 (Deferred)

- [ ] Inbound attachment processing (file-only messages, download, task injection).
- [ ] Slack command parity (`/reset` / `/id` / help) and init-flow parity.
- [ ] Further convergence of group-trigger boundaries (for example, thread-message edge behavior).
- [ ] Outbound text delivery improvements (long-message splitting, formatting strategy, rich-content degradation).
- [ ] Edited-message event handling.

## 3) Mainline Task Breakdown (Executable)

### 0. Bus identity enrichment: `username` / `display_name`

- Task breakdown:
  - [x] Add Slack API user profile lookup (`users.info`) with lightweight cache (`team_id:user_id`).
  - [x] Fill `username` / `display_name` on Slack inbound pipeline (fail-close on enrichment failure; do not publish to bus).
  - [x] Write enriched values to inbound extensions (`FromUsername` / `FromDisplayName`).
  - [x] Confirm contacts observation path can consume these fields (prefer `display_name` for nickname).
- Acceptance criteria:
  - [ ] Subsequent messages from same user in same team should not trigger high-frequency repeated `users.info` calls.
  - [ ] Bus inbound payload should stably contain `from_username` or `from_display_name` (when available).
  - [ ] On Slack API failure, inbound message should not enter bus and should remain observable via logs/hooks.
- Test suggestions:
  - [ ] Add profile lookup + cache-hit tests under `internal/channelruntime/slack/*_test.go`.
  - [ ] Add nickname write/update cases under `contacts/*_test.go`.

### 1. Emoji reaction parity (`message_react`)

- Task breakdown:
  - [x] Add Slack channel tool: `message_react` (at minimum `emoji` parameter).
  - [x] Add `reactions.add` call in Slack API layer.
  - [x] Inject tool in `runSlackTask(...)` with current `channel_id` + `message_ts` context.
  - [x] Record reaction outbound history (aligned with Telegram outbound-reaction semantics).
- Acceptance criteria:
  - [ ] When model calls `message_react`, reaction is applied to target message.
  - [x] Invalid emoji / permission errors are safely returned without runtime crash.
  - [x] Reaction trail is visible in task history (for memory/audit).
- Test suggestions:
  - [x] Cover schema and execute path in `tools/slack/*_test.go`.
  - [ ] Add runtime registration/invocation tests in `internal/channelruntime/slack/runtime*_test.go`.

### 2. Slack presentation for `plan_create`

- Recommended V1:
  - Use thread incremental progress messages instead of message-editing first, to keep complexity low and behavior stable.
- Task breakdown:
  - [x] Wire `agent.WithPlanStepUpdate(...)` in Slack task runtime.
  - [x] Publish concise step-progress message in same thread (`correlation_id` marked as `slack:plan:*`).
  - [x] Add `plan_progress` outbound kind classification for UI/log distinction.
- Acceptance criteria:
  - [ ] For planned tasks, each completed step yields one progress message in thread.
  - [ ] No noisy progress messages when plan is missing/empty.
  - [ ] Main response and progress updates remain readable and non-overwriting.
- Test suggestions:
  - [x] Add plan-hook behavior tests in `internal/channelruntime/slack/runtime_task_test.go`.
  - [x] Add outbound kind classification tests in `internal/channelruntime/slack/runtime_outbound_test.go`.

### 3. Channel-specific prompt block (Slack)

- Task breakdown:
  - [x] Add Slack policy template block.
  - [x] Add `AppendSlackRuntimeBlocks(...)` in `promptprofile`.
  - [x] Inject Slack runtime blocks in Slack task runtime (different behavior by `im/group`).
  - [x] Add Slack mention-users block (parallel to Telegram group-username block).
- Acceptance criteria:
  - [x] Slack runtime request contains Slack-specific policy blocks.
  - [ ] Does not affect Telegram/other runtime prompt assembly.
- Test suggestions:
  - [x] Add block-injection tests under `internal/promptprofile/*_test.go`.
  - [ ] Add Slack runtime prompt-spec inclusion checks in `internal/channelruntime/slack/runtime_task_test.go`.

### 4. Heartbeat support for Slack

- Task breakdown:
  - [x] Reuse Telegram pattern and add `runSlackWithOptionalHeartbeat(...)` combined startup logic.
  - [x] Implement Slack heartbeat notifier (based on `chat.postMessage`).
  - [x] Define target behavior: V1 uses `slack.allowed_channel_ids` as heartbeat delivery targets.
  - [x] Update README/config docs (enablement conditions and caveats).
- Acceptance criteria:
  - [ ] With heartbeat enabled, configured Slack channels receive periodic heartbeat messages.
  - [x] Correct linked shutdown behavior when either Slack runtime or heartbeat exits.
  - [x] No panic when no target channels are configured (warn and degrade to no-op delivery).
- Test suggestions:
  - [x] Add combined-run tests under `cmd/mistermorph/slackcmd/*_test.go`.
  - [x] Add Slack notifier adaptation tests under `internal/channelruntime/heartbeat/*_test.go`.

## 4) Suggested PR Split

- [x] PR-1: Mainline 0 (bus identity enrichment)
- [x] PR-2: Mainline 1 (emoji / `message_react`)
- [x] PR-3: Mainline 2 (plan progress rendering)
- [x] PR-4: Mainline 3 (Slack prompt block)
- [x] PR-5: Mainline 4 (heartbeat integration)

## 5) Thread-Scoped History Plan (New)

### Objective

- [x] When inbound message has `thread_ts`, use thread-scoped history/context for addressing + task execution.
- [x] When inbound message has empty `thread_ts`, keep existing channel-scoped behavior.
- [x] Keep bus `conversation_key` unchanged (`slack:<team_id>:<channel_id>`) in V1.

### Design Constraints

- [ ] Do not change `internal/bus/adapters/slack/*` conversation-key grammar in this PR.
- [ ] Do not change outbound thread delivery priority (`extensions.thread_ts` -> `extensions.reply_to` -> envelope `reply_to`).
- [ ] Keep worker serialization scope channel-level in V1 to minimize behavior risk; change only history scope.

### Implementation Checklist

- [x] Add helper: `buildSlackHistoryScopeKey(teamID, channelID, threadTS string)`.
- [x] Keep `slackJob` minimal: do not add duplicated history-scope state.
- [x] Derive history scope key on demand from `team_id` + `channel_id` + `thread_ts`.
- [x] Replace history map indexing from channel conversation key to `HistoryScopeKey`:
- [x] path: worker pre-run snapshot (`history[...]` read).
- [x] path: worker post-run append (`history[...]` write).
- [x] path: ignored inbound append in talkative mode.
- [x] Replace sticky-skills map indexing to `HistoryScopeKey` so thread and channel contexts do not cross-contaminate.
- [x] Keep `ConversationKey` unchanged for bus/hook/daemon metadata.
- [x] Keep `ReplyToMessageID = ThreadTS` in history items (no schema change).

### Test Checklist

- [x] Unit test for scope-key builder:
- [x] no-thread case -> `slack:<team>:<channel>`
- [x] threaded case -> `slack:<team>:<channel>:thread:<thread_ts>`
- [x] Runtime behavior tests:
- [x] same channel + different `thread_ts` -> isolated history contexts
- [x] same `thread_ts` -> shared thread history
- [x] no `thread_ts` -> unchanged channel history behavior
- [x] Regression tests:
- [x] outbound replies still target correct thread
- [x] `slack.group_trigger_mode=smart` still works in both thread and non-thread messages

### Explicit Non-Goals (V1)

- [ ] Do not change memory subject/session key scope (keep current channel scope).
- [ ] Do not add Slack API thread backfill (`conversations.replies`) in this PR.

### Follow-up (V2 Candidate)

- [ ] Add optional thread backfill on first-seen thread to improve cold-start thread context.
