# Prompt Inventory

This document tracks where prompts are defined today, how they are composed at runtime, and how `mister_morph_meta` is injected.

## Main System Prompt Composition

### 1) Base spec and rendering

- `DefaultPromptSpec()` provides the base `PromptSpec`.
- `BuildSystemPrompt(...)` renders the final system prompt.
- `PromptBlock` now only contains `Content`; there is no `Title` field.
- Block labels should be written directly in block template content (for example `[[ Telegram Policies ]]`).

### 2) Skill metadata

- `PromptSpecWithSkills(...)` discovers/loads skill frontmatter and appends `spec.Skills`.
- Skills are rendered into the system prompt under `## Available Skills`.

### 3) Persona identity

- `ApplyPersonaIdentity(...)` loads local persona docs and may replace `spec.Identity`.
- If persona docs are `status: draft`, they are skipped.

### 4) Runtime prompt blocks

Runtime block appenders:

- `AppendPlanCreateGuidanceBlock(...)`
- `AppendLocalToolNotesBlock(...)`
- `AppendTelegramRuntimeBlocks(...)`
- `AppendSlackRuntimeBlocks(...)`

Runtime message injectors:

- `RunOptions.MemoryContext` injects retrieved memory as a dedicated runtime context message after meta and before history/current turns.

These blocks are applied in the major runtime task flows:

- `runHeartbeatTask(...)`
- `runTelegramTask(...)`
- `runSlackTask(...)`
- `runOneTask(...)`
- CLI run path inside `runcmd.New(...)`

## Group Trigger / Addressing

### Decision behavior

- `Decide(...)` is the shared group-trigger decision function.
- If `ExplicitMatched=true`, it accepts immediately and does not call addressing LLM.
- Otherwise, in `smart` / `talkative` modes, it may call addressing LLM.

### Telegram addressing

- Trigger entry: `groupTriggerDecision(...)`
- Addressing classifier: `addressingDecisionViaLLM(...)`
- Prompt rendering: `renderTelegramAddressingPrompts(...)`
- Lightweight reaction path can use `message_react`.

### Slack addressing

- Trigger entry: `decideSlackGroupTrigger(...)`
- Addressing classifier: `slackAddressingDecisionViaLLM(...)`
- Slack addressing prompt is currently assembled inline (not a separate template file).
- Lightweight reaction path can use `message_react`.

## Template Index

Template directories only:

- Main system prompt templates: `agent/prompts/`
- Runtime block templates: `internal/promptprofile/prompts/`
- Telegram sub-prompt templates (init/memory/addressing): `internal/channelruntime/telegram/prompts/`

## Sub Prompts (Independent `llm.Request` Calls)

These are LLM calls outside the main tool-using loop.

- Plan generation: `Execute(...)` (`plan_create`)
- Telegram init question generation: `buildInitQuestions(...)`
- Telegram init field filling: `buildInitFill(...)`
- Telegram post-init greeting: `generatePostInitGreeting(...)`
- Telegram soul polish: `polishInitSoulMarkdown(...)`
- Telegram memory draft: `BuildMemoryDraft(...)`
- Telegram addressing classification: `addressingDecisionViaLLM(...)`
- Slack addressing classification: `slackAddressingDecisionViaLLM(...)`
- TODO reference resolution: `ResolveAddContent(...)`
- TODO complete semantic match: `MatchCompleteIndex(...)`
- Generic semantic dedup: `SelectDedupKeepIndices(...)`

## `mister_morph_meta`

### Purpose

`mister_morph_meta` is runtime metadata, not user instruction text.

### Injection path

- `Run(...)` adds metadata as a dedicated message before task content.
- `WithRuntimeClockMeta(...)` enriches metadata with runtime clock fields.
- `buildInjectedMetaMessage(...)` wraps it into:

```json
{"mister_morph_meta": <Meta>}
```

- Oversized payloads are truncated with fallback envelope.

### Common meta producers

- CLI heartbeat path in `runcmd.New(...)` uses `BuildHeartbeatMeta(...)`.
- Daemon heartbeat scheduler in `NewServeCmd(...)` uses `BuildHeartbeatMeta(...)`.
- Heartbeat channel runtime uses `BuildHeartbeatMetaFromDeps(...)`.
- Telegram task runtime sets channel meta in `runTelegramTask(...)`.
- Slack task runtime sets channel meta in `runSlackTask(...)`.

## Notes

- Older references under `cmd/mistermorph/telegramcmd/prompts/*` are obsolete.
- Telegram/Slack runtime policy blocks are now injected via prompt-profile block templates.
- `PromptBlock.Title` has been removed; block headers belong in template/body content.
