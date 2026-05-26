---
title: Commands
description: Commands supported by chat, Console, and channel runtimes.
---

# Commands

Commands are messages that start with `/` inside interactive chat, Console tasks, or channel runtimes.

> In Slack, `/` triggers Slack's own command system, so add a leading space before `/`, for example ` /models`.
>
> In Slack group chats, commands must explicitly address the bot. In Telegram group chats, normal bot commands such as `/models@BotName` are supported.

## Common Commands

These commands are available in CLI chat, Console Web, Telegram, Slack, LINE, and Lark.

| Command | What it does |
|---|---|
| `/help` | Lists currently available commands. |
| `/models` | Shows the current model. |
| `/skills` | Shows current skills. |
| `/ctx` | Shows context-window usage for the current conversation. |
| `/workspace` | Shows the current workspace directory. |

`/ctx` does not call the LLM. If no agent turn has recorded usage yet, it says no context usage has been recorded.

For `/workspace`, these forms are supported:

| Command | What it does |
|---|---|
| `/workspace` | Shows the current workspace directory. |
| `/workspace attach <dir>` | Attaches or replaces the workspace directory. |
| `/workspace detach` | Detaches the current workspace. |

For `/models`, these forms are supported:

| Command | What it does |
|---|---|
| `/models` | Shows the current model. |
| `/models list` | Lists configured model profiles. |
| `/models set <profile_name>` | Switches the current model. |
| `/models reset` | Resets model selection to automatic mode. |

## CLI Chat Only

These commands are available in `mistermorph chat`.

| Command | What it does |
|---|---|
| `/exit` | Exits the chat session. |
| `/quit` | Exits the chat session. |
| `/reset` | Clears the current conversation history. |
| `/memory` | Displays the current project memory. |
| `/remember <content>` | Adds a long-term memory item for the current project. |
| `/init` | Generates an `AGENTS.md` file for the current project. |
| `/update` | Regenerates `AGENTS.md` and overwrites the existing file. |

## Telegram Only

These commands are only available in Telegram.

| Command | What it does |
|---|---|
| `/id` | Shows the current Telegram chat id and chat type. |
| `/reset` | Clears chat history, sticky skills, known mentions, and init state for that chat. |
