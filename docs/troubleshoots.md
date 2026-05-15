# Troubleshoots

This document tracks currently known issues and suggested workarounds.

## 1. Cloudflare provider: tool calling is currently unavailable

Symptoms:

- With `llm.provider=cloudflare`, tool calls are not returned reliably even when tools are provided.
- Setting `llm.tools_emulation_mode=force` does not fix the issue.

Status:

- Root cause is still under investigation. Treat this as a known limitation.

Workarounds:

- Do not use `cloudflare` for tool-calling workloads for now.
- Switch to another provider (for example `openai` or `gemini`) when tool execution is required.

## 2. Gemini: use the native provider

Recommendation:

- Use `llm.provider=gemini` (native Gemini provider).
- Do not use OpenAI-compatible mode for Gemini (for example: `llm.provider=openai_custom` with a Gemini-compatible endpoint).

Example:

```yaml
llm:
  provider: gemini
  model: "gemini-2.5-pro"
  api_key: "${GEMINI_API_KEY}"
```

## 3. Telegram supergroup topics: messages may be skipped without `@bot`

Symptoms:

- In supergroup topic/thread chats, incoming messages include `reply_to_message` even when the user did not explicitly reply.
- Group messages without `@bot` can be skipped before `groupTriggerDecision` runs.

Status:

- Known behavior gap. Current reply prefilter treats `reply_to_message` as explicit reply context.

Workarounds:

- Mention the bot in message body (for example `@your_bot`).
- Do not use Telegram topics/threads for now.

## 4. Desktop on Linux Wayland: child window position may be ignored

Symptoms:

- Desktop child windows, such as RAW JSON and Poke, may not open at the requested position.
- `center` and manual `x/y` placement can both be ignored.

Status:

- This is a Linux Wayland window-manager limitation, not a dialog layout bug.
- Wails uses GTK for Linux windows. On Wayland, applications cannot reliably set absolute top-level window coordinates.
- Wails' Linux modal attachment is currently not implemented, so the app cannot rely on a native parent-attached child window to force centering.

Workarounds:

- Treat desktop child window position as best-effort on Linux Wayland.
- Use an X11 session or XWayland backend when precise child window placement is required.
