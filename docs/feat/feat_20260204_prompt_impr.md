---
date: 2026-02-04
title: Prompt Improvements for URL Summarization Tasks
status: draft
---

# Prompt Improvement Notes (2026-02-04)

## 1) Reduce Redundant Tool Schema in System Prompt
- Current system prompt embeds full JSON schemas for each tool.
- The request already passes the same schemas via the structured `tools` field.
- This duplicates tokens and increases the chance of the model emitting mixed JSON/text.

**Suggestion**
- In the system prompt, keep only tool names + short descriptions.
- Leave full JSON schema only in the `tools` field.

TODO
- [x] Add a short tool description formatter (name + description only) in `tools/registry.go`.
- [x] Update `BuildSystemPrompt` (`agent/prompt.go`) to use the short tool descriptions instead of full schemas.
- [x] Ensure the LLM request continues sending full tool schemas via `llm.Request.Tools` (confirmed by existing request dumps that still include full tool schemas).

## 2) Clarify Tool Priority for URL Tasks (Dynamic Rule)
- The task is explicitly "visit URL and summarize".
- The model may still consider `web_search` instead of `url_fetch`.

**Decision**
- Add the rule only when a direct URL is detected in the user task:
  "When a user provides a direct URL, prefer `url_fetch` and skip `web_search`."

TODO
- [x] Add a URL detection helper (e.g. `agent/urls.go`) to extract direct URLs from a task.
- [x] In `agent/engine.go`, clone `PromptSpec` for this run and append the URL-priority rule when a direct URL is found.
- [x] Add a test that verifies the rule is injected only when a URL is present.

## 3) Batch Tool Calls for Multiple URLs
- If the user provides multiple URLs, a single round-trip can fetch them.

**Suggestion**
- Add a rule: when multiple URLs are present, emit a batch of `url_fetch` tool calls in one response.

TODO
- [x] Reuse URL extraction to count URLs; when count > 1, append a batch-tool-calls rule to the prompt.
- [x] Add a test that the batch rule appears only for multi-URL tasks.

## 4) Binary/Large File Heuristic for download_path
- Directly dumping large/binary responses into tool output bloats context.

**Suggestion**
- Add a rule: when URL looks like binary or large content (e.g., file extensions like .pdf, .zip, .png, .jpg, .mp4), prefer `download_path` instead of inline body.
- If unsure, allow a lightweight `HEAD` or small-range fetch to confirm content type before downloading.

TODO
- [x] Add a helper to detect binary/large extensions from URL paths (e.g., `.pdf`, `.zip`, `.png`, `.jpg`, `.mp4`).
- [x] Append a dynamic rule to prefer `download_path` when the URL matches those extensions.
- [x] Consider adding a short rule encouraging `HEAD` or `Range` checks when file type is uncertain.

## 5) Error-Handling Rule for url_fetch Failures
- Guard blocks, timeouts, or non-2xx responses should not trigger hallucinated summaries.

**Suggestion**
- Add a rule: on `url_fetch` failure, surface the error and ask for updated allowlist/params rather than fabricating content.

TODO
- [x] Append a rule to the prompt that forbids fabrication when `url_fetch` fails and instructs to report the error and request updated allowlist/params.
- [x] Add a test that the rule is present for URL tasks.
