---
title: Prompt Architecture
description: Introduces how the Agent's prompt mechanism works.
---

# Prompt Architecture

In Mister Morph's main loop, the only purpose of the prompt is to assemble a reasonable state for the Agent.

> Skills, identity, soul, todo, and memory may look like separate systems, but in the end they are all ways of maintaining that state. They are mostly syntax sugar around the same thing.

In Mister Morph, these layers are assembled around `agent/prompts/system.md`.

## Main Loop

### Static Prompt Skeleton

This template roughly looks like this:

```md
## Persona
{{ identity }}

## Available Skills
{{ skills }}

## Reference Format
{{ internal reference conventions }}

## Additional Policies
{{ large extra policy blocks }}

## Response Format
{{ expected output format }}

## Rules
{{ built-in rules and additional rules }}
```

### Runtime Prompt

Once a concrete run starts, the runtime keeps adding current context.

Common sources include:

- local persona files such as `IDENTITY.md` and `SOUL.md`
- metadata for the skills enabled in this run
- local script notes such as `SCRIPTS.md`
- extra policy blocks
- memory summary

This layer changes with the current task, current channel, and current local state. The final prompt used in CLI, Telegram, and Slack does not have to be identical.

### Message Orchestration Outside the Prompt

After the final system prompt is ready, the main Agent still arranges a message stack in the request.

Runtime metadata is injected as a user-role JSON envelope under `mister_morph_meta`. Typical fields include trigger or correlation data when present, runtime clock fields such as `now_utc` / `now_local` / `now_local_weekday`, and host facts such as `host_os`.

The order can be understood like this:

```text
[system] final system prompt
   ->
[user] runtime metadata
   ->
[history] history messages
   ->
[user] current message or raw task
```

## Independent Prompts

Mister Morph also has another class of calls that does not first build the main Agent's full system prompt, and may not enter the multi-step tool loop at all. Instead, it builds a smaller dedicated prompt and makes a single `llm.Chat` call.

Independent prompts are used for:

- deciding how to step into a group chat
- task planning
- memory consolidation
- some narrow semantic judgment or matching tasks

### Relationship to the Main Loop

You can think of the two paths like this:

```text
Main Agent main loop
  -> full system prompt
  -> runtime metadata / history / current message
  -> can run multiple steps and call tools

Independent llm.Chat
  -> dedicated system prompt / user prompt
  -> single call
  -> solves one narrow problem only
```

For example, when the Agent decides how to enter a group chat, it checks:

- whether this message is addressing it
- whether it should interject now
- whether a lightweight response, such as an emoji, is enough

That decision happens before the main loop, so a smaller dedicated prompt is more suitable than the full main system prompt.
