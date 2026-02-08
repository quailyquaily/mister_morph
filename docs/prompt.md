# Prompt Inventory

This document tracks where prompts are defined, how they are composed at runtime, and how `mister_morph_meta` is injected and consumed.

## Prompt Composition Model

- Base spec starts from `agent.DefaultPromptSpec()` in `agent/prompt.go`.
- Persona identity is optionally **overridden** by `promptprofile.ApplyPersonaIdentity(...)` in `internal/promptprofile/identity.go`.
  - If local `IDENTITY.md` / `SOUL.md` are loaded and not `status: draft`, `spec.Identity` is replaced.
- Runtime prompt blocks/rules are then appended:
  - URL/task-specific rules (`agent/prompt_rules.go`)
  - Registry-aware rules (`agent/prompt_rules.go`, e.g. `plan_create` rules only when tool exists)
  - Intent block (`agent/engine.go` + `agent/intent.go`)
  - Skills/auth-profile blocks (`internal/skillsutil/skillsutil.go`)
  - Telegram runtime rules/blocks (`cmd/mistermorph/telegramcmd/command.go`)
- For URL augmentation, duplicate rule strings are deduplicated by `appendRule(...)`.
- `BuildSystemPrompt(...)` also checks registry capabilities for response-format sections (plan format appears only with `plan_create`).

## Main Agent Prompt

### 1) Base PromptSpec

- File: `agent/prompt.go`
- Template/Renderer:
  - `agent/prompts/system.tmpl`
  - `agent/prompt_template.go`
  - `internal/prompttmpl/prompttmpl.go`
- Definitions:
  - `DefaultPromptSpec()`: base `Identity` and `Rules`
  - `BuildSystemPrompt(...)`: renders identity, blocks, available tools, response schema, and rules (template-driven, with legacy fallback)

### 2) Persona identity injection

- File: `internal/promptprofile/identity.go`
- Definition:
  - `ApplyPersonaIdentity(...)` loads local persona docs and may replace `spec.Identity`

### 3) Task-based dynamic rules (URL-aware)

- File: `agent/prompt_rules.go`
- Definition:
  - `augmentPromptSpecForTask(...)` appends URL-specific rules (prefer `url_fetch`, batch fetch, prefer `download_path`, no fabrication on fetch errors)
  - `augmentPromptSpecForRegistry(...)` appends registry-aware rules (for example `plan_create` guidance only when the tool is registered)

### 4) Intent-derived context

- Files:
  - `agent/intent.go`
  - `agent/engine.go`
- Definitions:
  - `IntentBlock(...)` injects inferred intent into prompt blocks
  - `IntentSystemMessage(...)` is used when a custom prompt builder is active

### 5) Skills/auth-profile blocks

- File: `internal/skillsutil/skillsutil.go`
- Definition:
  - `PromptSpecWithSkills(...)` appends loaded skill content and auth-profile guidance

### 6) Telegram runtime prompt mutations

- File: `cmd/mistermorph/telegramcmd/command.go`
- Definition:
  - Appends Telegram-specific rules (MarkdownV2 style limits, mention behavior, voice/reaction guidance, optional memory/group blocks)

### 7) Heartbeat task prompt template

- File: `internal/heartbeatutil/heartbeat.go`
- Definition:
  - `BuildHeartbeatTask(...)` builds the heartbeat task body (checklist + action expectations)

### 8) Runtime control prompts

- Files:
  - `agent/engine_loop.go`
  - `agent/engine_helpers.go`
- Definition:
  - Dynamic system/user control instructions during execution (parse-retry guidance, plan transition guidance, repeated-tool guardrails, forced completion guidance)

### 9) Prompt-builder override hook

- File: `agent/engine.go`
- Definition:
  - `WithPromptBuilder(...)` allows full replacement of default system prompt composition

## Sub Prompts (Independent LLM Calls)

These are prompts sent through separate `llm.Request` calls outside the main tool-using turn loop.

## Template Index (Per File)

| Template | Role | Purpose |
|---|---|---|
| `agent/prompts/system.tmpl` | system | Renders the main system prompt (Identity, Blocks, Tools, response format, Rules). |
| `agent/prompts/intent_system.tmpl` | system | Constrains intent inference output schema (JSON contract). |
| `agent/prompts/intent_user.tmpl` | user | Carries `task/history` plus built-in intent inference rules. |
| `telegramcmd/prompts/memory_draft_system.tmpl` | system | Defines the output contract for single-session memory draft generation. |
| `telegramcmd/prompts/memory_draft_user.tmpl` | user | Carries session context, dialogue snippets, existing tasks/follow-ups, and summarization rules. |
| `telegramcmd/prompts/memory_merge_system.tmpl` | system | Defines the output contract for same-day short-term memory merge. |
| `telegramcmd/prompts/memory_merge_user.tmpl` | user | Carries existing/incoming memory content and merge rules. |
| `telegramcmd/prompts/memory_task_match_system.tmpl` | system | Defines the output contract for task mapping (`update_index/match_index`). |
| `telegramcmd/prompts/memory_task_match_user.tmpl` | user | Carries existing tasks, updates, and matching rules. |
| `telegramcmd/prompts/memory_task_dedup_system.tmpl` | system | Defines the output contract for semantic task deduplication. |
| `telegramcmd/prompts/memory_task_dedup_user.tmpl` | user | Carries tasks and deduplication rules. |
| `telegramcmd/prompts/maep_feedback_system.tmpl` | system | Defines the output contract for MAEP feedback classification. |
| `telegramcmd/prompts/maep_feedback_user.tmpl` | user | Carries recent turns, inbound text, allowed actions, and signal bounds for MAEP feedback classification. |
| `telegramcmd/prompts/telegram_addressing_system.tmpl` | system | Defines the output contract for Telegram addressing classification. |
| `telegramcmd/prompts/telegram_addressing_user.tmpl` | user | Carries bot username, aliases, and incoming message for addressing classification. |
| `telegramcmd/prompts/reaction_category_system.tmpl` | system | Defines the output contract for reaction category classification. |
| `telegramcmd/prompts/reaction_category_user.tmpl` | user | Carries intent/task payload, category candidates, and rules for reaction category classification. |

### 1) Intent inference

- File/Function: `agent/intent.go` / `InferIntent(...)`
- Templates:
  - `agent/prompts/intent_system.tmpl`
  - `agent/prompts/intent_user.tmpl`
  - Renderer: `agent/intent_template.go` (via `internal/prompttmpl`)
- Purpose: infer structured user intent and ambiguity level
- Primary input: current `task`, trimmed recent `history` (rules are embedded in the template)
- Output: `Intent{goal, deliverable, constraints, ambiguities, ask}`
- JSON required: **Yes** (`ForceJSON=true`)

### 2) Plan generation tool

- File/Function: `tools/builtin/plan_create.go` / `Execute(...)`
- Purpose: generate a structured execution plan
- Primary input: `task`, optional `max_steps/style/model`, available tools
- Output: tool observation JSON string containing `plan`
- JSON required: **Yes** (`ForceJSON=true`)

### 3) Skill router selection

- File/Function: `skills/select.go` / `Select(...)`
- Purpose: select a bounded skill subset for loading
- Primary input: `task`, discovered skill catalog, `max_skills`
- Output: `Selection{skills_to_load, reasoning}`
- JSON required: **Yes** (`ForceJSON=true`)

### 4) Remote SKILL.md security review

- File/Function: `cmd/mistermorph/skillscmd/skills_install_builtin.go` / `reviewRemoteSkill(...)`
- Purpose: extract safe download directives from untrusted remote `SKILL.md`
- Primary input: source URL, raw markdown, target output schema
- Output: `remoteSkillReview{skill_name, skill_dir, files[], risks[]}`
- JSON required: **Yes** (`ForceJSON=true`)

### 5) Contacts candidate-feature extraction

- File/Function: `contacts/llm_features.go` / `EvaluateCandidateFeatures(...)`
- Purpose: score semantic overlap and explicit history linkage for share candidates
- Primary input: contact profile + candidate list payload
- Output: map of `item_id -> CandidateFeature`
- JSON required: **Yes** (`ForceJSON=true`)

### 6) Contacts preference-feature extraction

- File/Function: `contacts/llm_features.go` / `EvaluateContactPreferences(...)`
- Purpose: infer stable topic affinities and persona traits
- Primary input: contact profile + candidate list payload
- Output: `PreferenceFeatures{topic_affinity, persona_brief, persona_traits, confidence}`
- JSON required: **Yes** (`ForceJSON=true`)

### 7) Contacts nickname suggestion

- File/Function: `contacts/llm_nickname.go` / `SuggestNickname(...)`
- Purpose: suggest a short stable nickname
- Primary input: contact profile payload + nickname rules
- Output: `{nickname, confidence, reason}` (then normalized to return values)
- JSON required: **Yes** (`ForceJSON=true`)

### 8) Telegram init question generation

- File/Function: `cmd/mistermorph/telegramcmd/init_flow.go` / `buildInitQuestions(...)`
- Purpose: generate onboarding questions for persona bootstrap
- Primary input: draft `IDENTITY.md`, draft `SOUL.md`, user text, required target fields
- Output: `{"questions": [...]}`
- JSON required: **Yes** (`ForceJSON=true`)

### 9) Telegram init field filling

- File/Function: `cmd/mistermorph/telegramcmd/init_flow.go` / `buildInitFill(...)`
- Purpose: fill persona fields from onboarding answers
- Primary input: draft identity/soul markdown, questions, user answer, telegram context
- Output: `initFillOutput` (identity + soul field values)
- JSON required: **Yes** (`ForceJSON=true`)

### 10) Telegram init question message rewriting

- File/Function: `cmd/mistermorph/telegramcmd/init_flow.go` / `generateInitQuestionMessage(...)`
- Purpose: rewrite question list into natural conversational Telegram text
- Primary input: user text + normalized questions
- Output: plain text message
- JSON required: **No** (`ForceJSON=false`)

### 11) Telegram post-init greeting generation

- File/Function: `cmd/mistermorph/telegramcmd/init_flow.go` / `generatePostInitGreeting(...)`
- Purpose: generate immediate natural greeting after persona bootstrap
- Primary input: finalized identity/soul markdown + init context
- Output: plain text message
- JSON required: **No** (`ForceJSON=false`)

### 12) Telegram memory draft generation

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `BuildMemoryDraft(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/memory_draft_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/memory_draft_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/memory_prompts.go`
- Purpose: convert one session into structured short-term memory draft
- Primary input: session context, conversation, existing tasks/follow-ups
- Output: `memory.SessionDraft`
- JSON required: **Yes** (`ForceJSON=true`)

### 13) Telegram semantic merge for short-term memory

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `SemanticMergeShortTerm(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/memory_merge_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/memory_merge_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/memory_prompts.go`
- Purpose: semantically merge same-day short-term memory
- Primary input: existing content + incoming draft
- Output: merged `memory.ShortTermContent` + summary string
- JSON required: **Yes** (`ForceJSON=true`)

### 14) Telegram semantic task matching

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `semanticMatchTasks(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/memory_task_match_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/memory_task_match_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/memory_prompts.go`
- Purpose: map incoming task updates onto existing tasks
- Primary input: existing task list + update task list
- Output: `[]taskMatch{update_index, match_index}`
- JSON required: **Yes** (`ForceJSON=true`)

### 15) Telegram semantic task deduplication

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `semanticDedupTaskItems(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/memory_task_dedup_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/memory_task_dedup_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/memory_prompts.go`
- Purpose: deduplicate semantically equivalent task items
- Primary input: task list
- Output: deduplicated `[]memory.TaskItem`
- JSON required: **Yes** (`ForceJSON=true`)

### 16) Telegram plan-progress message rewriting

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `generateTelegramPlanProgressMessage(...)`
- Purpose: rewrite step progress into short casual Telegram updates
- Primary input: task text, plan summary/progress stats, completed/next step info
- Output: plain text message
- JSON required: **No** (`ForceJSON=false`)

### 17) MAEP feedback classifier

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `classifyMAEPFeedback(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/maep_feedback_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/maep_feedback_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/maep_prompts.go`
- Purpose: classify inbound feedback signals and next conversational action
- Primary input: recent turns + inbound text
- Output: `maepFeedbackClassification{signal_positive, signal_negative, signal_bored, next_action, confidence}`
- JSON required: **Yes** (`ForceJSON=true`)

### 18) Telegram addressing classifier

- File/Function: `cmd/mistermorph/telegramcmd/command.go` / `addressingDecisionViaLLM(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/telegram_addressing_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/telegram_addressing_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/addressing_prompts.go`
- Purpose: decide whether a message is actually addressed to the bot
- Primary input: bot username, aliases, incoming message text
- Output: `telegramAddressingLLMDecision{addressed, confidence, task_text, reason}`
- JSON required: **Yes** (`ForceJSON=true`)

### 19) Telegram reaction-category classifier

- File/Function: `cmd/mistermorph/telegramcmd/reactions.go` / `classifyReactionCategoryViaIntent(...)`
- Templates:
  - `cmd/mistermorph/telegramcmd/prompts/reaction_category_system.tmpl`
  - `cmd/mistermorph/telegramcmd/prompts/reaction_category_user.tmpl`
  - Renderer: `cmd/mistermorph/telegramcmd/reaction_prompts.go`
- Purpose: choose lightweight emoji-reaction category from inferred intent/task
- Primary input: inferred intent fields, task text, allowed categories
- Output: normalized `reactionMatch{Category, Source}`
- JSON required: **Yes** (`ForceJSON=true`)

## `mister_morph_meta`

### Purpose

`mister_morph_meta` is run-context metadata for the model, not user intent text.

It is used to:
- carry trigger/channel/runtime context (for example heartbeat vs telegram chat)
- drive behavior without polluting task text
- keep orchestration hints structured and machine-parseable

### Injection path

- `agent/engine.go` (`Run(...)`)
  - if `RunOptions.Meta` is non-empty, one metadata `user` message is injected before the task message
- `agent/metadata.go`
  - wraps payload as:

```json
{"mister_morph_meta": <Meta>}
```

  - enforces 4KB max payload with truncation fallback

### How the LLM is guided to understand and use it

Guidance is provided through both message placement and explicit rules.

1. Placement in message order
- Metadata is injected as a dedicated message immediately before the task.
- This keeps metadata separate from task text while preserving recency.

2. Explicit base prompt rules
- `agent/prompt.go` rules explicitly instruct the model to:
  - treat `mister_morph_meta` as run context metadata
  - not treat metadata as an action request by itself
  - when `mister_morph_meta.heartbeat` exists, return a concise check/action summary and avoid placeholder outputs

3. Safety for oversized payloads
- `agent/metadata.go` truncates oversized metadata to a parseable stub with `truncated=true` and optionally `trigger` / `correlation_id`.

4. Regression coverage
- `agent/metadata_test.go` verifies metadata injection order and the existence of prompt rules referencing `mister_morph_meta`.

### Current meta sources and payloads

1. CLI heartbeat run (`mistermorph run --heartbeat`)

- File: `cmd/mistermorph/runcmd/run.go`
- Current `RunOptions.Meta` payload:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "trigger": "heartbeat",
    "heartbeat": {
      "source": "cli",
      "scheduled_at_utc": "...",
      "interval": "...",
      "checklist_path": "...",
      "checklist_empty": true
    }
  }
}
```

Note: this path currently nests one heartbeat envelope inside `heartbeat`.

2. Daemon scheduled heartbeat

- File: `cmd/mistermorph/daemoncmd/serve.go`
- Produced by `BuildHeartbeatMeta(...)` in `internal/heartbeatutil/heartbeat.go`:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "source": "daemon",
    "scheduled_at_utc": "...",
    "interval": "...",
    "checklist_path": "...",
    "checklist_empty": true,
    "queue_len": 3,
    "failures": 1,
    "last_success_utc": "...",
    "last_error": "..."
  }
}
```

3. Telegram normal chat run (default path)

- File: `cmd/mistermorph/telegramcmd/command.go`
- Default payload when `job.Meta == nil`:

```json
{
  "trigger": "telegram",
  "telegram_chat_id": 123,
  "telegram_message_id": 456,
  "telegram_chat_type": "private",
  "telegram_from_user_id": 789
}
```

4. Telegram scheduled heartbeat

- File: `cmd/mistermorph/telegramcmd/command.go`
- Heartbeat worker payload from `buildHeartbeatMeta(...)`:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "source": "telegram",
    "scheduled_at_utc": "...",
    "interval": "...",
    "checklist_path": "...",
    "checklist_empty": true,
    "telegram_chat_id": 123,
    "telegram_chat_type": "group",
    "telegram_from_user_id": 789,
    "telegram_from_username": "alice",
    "telegram_from_name": "Alice",
    "queue_len": 0,
    "last_success_utc": "..."
  }
}
```

5. MAEP inbound auto-reply run

- File: `cmd/mistermorph/telegramcmd/command.go`
- Payload:

```json
{
  "trigger": "maep_inbound"
}
```

### Paths that currently do not pass meta

- Normal CLI `run` (non-heartbeat) does not set `RunOptions.Meta` by default.
- Daemon `/tasks` user-submitted tasks do not set `RunOptions.Meta` by default.
