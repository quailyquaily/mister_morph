---
date: 2026-05-14
title: Image Generation and Editing Tools
status: draft
---

# Morph 图片生成与编辑能力

## 1) Scope

给 agent 增加图片生成和图片编辑能力，底层复用 `uniai` 的 images API。

本轮实现包含 V1 工具能力和 V2 多轮图片状态。

V1 只做这些事：

- 文本生成图片。
- 用一个本地图片文件做 prompt-based edit。
- 把生成结果写成本地图片文件。
- 在工具结果里返回路径、MIME、大小、usage 和 cost。
- 让 agent 自己决定是否继续调用 Telegram、Slack、console 文件下载等现有能力发送或展示图片。

V1 不做这些事：

- 不支持 mask-based 局部编辑。
- 不支持 image variation。
- 不自动把生成图片发送到当前聊天渠道。
- 不把远程 URL 直接作为编辑输入。需要先用 `url_fetch` 下载到本地。
- 不把图片 base64 写进 chat history、memory 或工具文本输出。

## 2) Current State

现在 morph 已经有图片输入路径：

- Telegram、LINE、Slack、Lark 可以把入站图片下载到 `file_cache_dir`。
- 入站图片会被转成 `llm.PartTypeImageBase64`，随当前用户消息发给支持图片输入的模型。
- `read_file`、`write_file`、`bash`、`url_fetch` 已经有 `workspace_dir`、`file_cache_dir`、`file_state_dir` 这套路径语义。
- Telegram 已经有 `telegram_send_photo`，Slack 有文件发送能力。
- Console 已经支持从 `file_cache_dir` 下载文件。

缺口是反方向能力：

- agent 没有工具可以请求模型生成图片。
- agent 没有工具可以把本地图片交给 image edit API。
- agent 收到用户图片后，当前 prompt 里通常只有图片内容，没有稳定的本地路径提示，因此难以把“这张图”传给 edit 工具。

## 3) First Principles

1. 图片生成是工具能力，不是 chat completion 的隐式副作用。
   Agent 应该显式调用工具。这样成本、文件写入、失败都可观察。

2. Provider 协议属于 `uniai`。
   Morph 不应该复制 OpenAI、Gemini、Cloudflare 的请求格式，只做参数、路径、文件和工具语义。

3. 工具输出应该是文件路径，不是大段 base64。
   Base64 会污染上下文，也会进入日志和 history。工具只返回可读的 JSON 元数据。

4. 图片文件默认是 cache artifact。
   默认写到 `file_cache_dir/images/`。如果用户明确要项目资产，工具可以写到 `workspace_dir` 下的显式路径。

5. 渠道发送保持解耦。
   生成图片以后，Telegram、Slack、console 或其他渠道怎么发送，仍由现有工具和运行时决定。

6. 编辑输入必须是本地受控文件。
   V1 只接受本地路径，路径必须在允许的 root 内。远程 URL 先用 `url_fetch` 下载。

7. 不为命名而加薄包装。
   如果已有 helper 能解决路径解析或 MIME 判断，就复用。只有在多个工具都需要同一段实质逻辑时才抽共享函数。

## 4) Uniai Assumption

实现前需要把 `github.com/quailyquaily/uniai` 升级到包含以下 API 的版本：

- `Client.Image(...)`
- `Client.EditImage(...)`
- `ImageResult.Images`
- `ImageAsset`
- `InputImage`
- image usage cost

如果当前 tag 还没有 `EditImage`，先等 `uniai` 发布包含该 API 的 tag，再升级 `go.mod`。

Morph 侧不 vendor `uniai`，也不复制 provider adapter。

## 5) Agent Tools

新增两个内置工具。

### 5.1 `image_generate`

用途：根据文本 prompt 生成图片并保存为本地文件。

参数：

| Name | Type | Required | Meaning |
| --- | --- | --- | --- |
| `prompt` | string | yes | 图片生成 prompt |
| `output_path` | string | no | 输出路径。支持 `workspace_dir/...` 或 `file_cache_dir/...` |

只生成一张图。`output_path` 为空时，工具写入默认路径。

### 5.2 `image_edit`

用途：用 prompt 编辑一个本地图片。

参数：

| Name | Type | Required | Meaning |
| --- | --- | --- | --- |
| `prompt` | string | yes | 编辑说明 |
| `input_path` | string | no | 本地输入图片路径。支持 `workspace_dir/...`、`file_cache_dir/...`。为空时可配合 `use_active_image` 使用 |
| `use_active_image` | boolean | no | 使用当前会话 active image 作为输入 |
| `output_path` | string | no | 输出路径 |

V1 不接受 `mask_path`。以后如果要支持 mask，应作为 `image_edit` 的可选参数加入，而不是新增第三个工具。

只接受一张输入图，只输出一张图。更复杂的参考图、多图拼合、风格迁移可以后续再扩展。

### 5.3 Tool Result

两个工具返回同一种 JSON：

```json
{
  "image": {
    "path": "file_cache_dir/images/20260514-120001-image.png",
    "mime_type": "image/png",
    "bytes": 123456,
    "revised_prompt": ""
  },
  "model": "gpt-image-2",
  "provider": "openai",
  "active_image_id": "img_01J...",
  "usage": {
    "input_tokens": 12,
    "input_text_tokens": 12,
    "input_image_tokens": 0,
    "output_tokens": 416,
    "total_tokens": 428,
    "cost": {
      "currency": "USD",
      "estimated": true,
      "total": 0.01
    }
  }
}
```

工具结果不返回 base64。

## 6) Config

图片模型配置放在 `llm.image`。工具开关和限制放在 `tools.image_generate`、`tools.image_edit`。

```yaml
llm:
  image:
    # Empty provider/api_key/model can inherit usable top-level LLM values.
    # endpoint is inherited only when the resolved image provider matches the top-level provider.
    provider: ""
    endpoint: ""
    api_key: ""
    model: ""
    request_timeout: "180s"
    options:
      openai: {}
      gemini: {}
      cloudflare: {}

tools:
  image_generate:
    enabled: true
  image_edit:
    enabled: true
```

Defaults:

- Tools are enabled by default, but registered only when the current task has image intent or retained image-tool state.
- Tools register only when image config is usable:
  - `llm.image` has a supported provider and matching credentials; or
  - `llm.image` inherits top-level `llm.provider` and `llm.api_key`, and the top-level provider is exactly `openai` or `gemini`.
- `openai_codex` does not make image tools available through Codex auth. To use images while chat uses Codex auth, set an explicit `llm.image.api_key` or full `llm.image` config.
- `llm.image.model` still falls back to the current runtime model, usually `llm.model`.
- `llm.image.endpoint` is inherited only when the resolved image provider matches the top-level provider. It is not inherited from `openai_codex`.
- Provider/model mismatch follows the same rule as chat LLM config: morph passes the configured values through and returns the provider error if the pair is invalid.
- Cloudflare account fields keep using existing `llm.cloudflare.*`.
- Pricing keeps using existing `llm.pricing_file`.
- Count is fixed at 1.
- Edit input image count is fixed at 1.
- Prompt, input image, and output image byte limits are fixed constants in code: prompt 8 KiB, input image 20 MiB, output image 50 MiB.

This avoids routing image calls through `llm.routes` in V1. If image generation later needs profile fallback or weighted routing, add a single image route then.

## 7) Internal Design

### 7.1 Do Not Extend `llm.Client`

Keep `llm.Client` as the chat interface:

```go
type Client interface {
    Chat(ctx context.Context, req Request) (Result, error)
}
```

Add a separate image interface:

```go
type ImageClient interface {
    GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
    EditImage(ctx context.Context, req ImageEditRequest) (ImageResult, error)
}
```

This keeps Codex and other chat-only clients valid. It also prevents image code from leaking into the main loop.

### 7.2 Shared LLM Image Types

Add small image request/result types under `llm` or a focused internal package.

Required fields:

- request: provider, model, prompt, options
- edit request: one input image with filename, MIME type, bytes
- result: images with bytes or base64 normalized to bytes, MIME type, revised prompt, usage

Do not expose `uniai` types outside the provider adapter.

### 7.3 Provider Adapter

Extend `providers/uniai.Client` with image methods:

- `GenerateImage(...)` maps to `client.Image(...)`
- `EditImage(...)` maps to `client.EditImage(...)`
- maps `uniai.ImageResult.Images` into morph image result
- maps usage and cost into existing `llm.Usage` / `llm.UsageCost` shape where possible

The adapter should pass provider-specific options through `uniai.ImageOptions`.

### 7.4 Tool Registration

Existing tool registration has four shapes:

- base registry: built from config before a run; includes ordinary built-ins such as `read_file`, `write_file`, `bash`, `url_fetch`, `web_search`, and `contacts_send`
- runtime registration: runs once per task after the base registry is cloned; currently adds tools that need the current LLM client, such as `plan_create` and `todo_update`
- engine registration: `agent.Engine` adds engine-local tools such as `spawn` and `acp_spawn`
- channel filtering: Telegram, Slack, LINE, and Lark clone the base registry and remove tools that do not make sense for that chat context

Image tools need a new per-task registration state. They do not belong in the base registry. They also cannot be handled by the current runtime registration alone, because a previous image-intent task can keep the tools available for later follow-up turns.

The registration point is still after the base registry is cloned and before `agent.New(...)`, but the decision uses both:

- the current task text
- a per-topic or per-session image tool retention state

Register image tools only when:

- the corresponding tool config is enabled
- an image client can be built
- `file_cache_dir` is configured
- the current task text has explicit image generation or image editing intent, or the current topic/session has active retention

The registration path should pass an `llm.ImageClient` into the tool. The tool should not read global config at execution time.

### 7.5 Intent Gate

Image tools should not be loaded for every task. Before registering `image_generate` and `image_edit` for a run, check the current task text for a small keyword allowlist covering Chinese, Japanese, and English.

Rules:

- Match only the current task or current inbound message, not old chat history.
- Normalize case and width where practical.
- Do not treat `image`、`图片`、`画像` alone as enough. The text must also imply create, draw, edit, modify, or redraw.
- If a V2 active image exists, follow-up edit words such as “再亮一点” can also load `image_edit`.

Initial keyword groups:

| Language | Generation examples | Editing examples |
| --- | --- | --- |
| Chinese | `画图`, `作图`, `做图`, `生成图片`, `生成一张图`, `画一张`, `图片生成`, `生成海报`, `生成插画` | `修图`, `改图`, `编辑图片`, `修改图片`, `重绘`, `换背景`, `去背景`, `调亮`, `调暗` |
| Japanese | `画像生成`, `画像を生成`, `画像作成`, `絵を描`, `描いて`, `作画`, `イラストを作` | `画像編集`, `編集して`, `修正して`, `描き直`, `背景を変え`, `明るくして`, `暗くして` |
| English | `generate image`, `create image`, `make an image`, `draw me`, `draw a`, `draw an`, `draw the`, `draw image`, `draw picture`, `draw illustration`, `create a poster`, `create an illustration` | `edit image`, `modify image`, `change the image`, `redraw`, `change background`, `remove background`, `make it brighter`, `make it darker` |

The allowlist should live in code as a small table with unit tests. It is not a configurable rule engine.

### 7.6 Retention

When a task triggers the image intent gate, keep image tools registered for later turns in the same conversation scope.

Rules:

- Console web: once a topic triggers image tools, keep them available for that topic.
- CLI `chat`: once a chat session triggers image tools, keep them available for the rest of that session.
- Channel runtimes: Telegram, Slack, Lark, and LINE keep image tools available for the next 16 conversation turns in that session. A future Discord runtime should use the same rule.
- If another task triggers image intent before the 16-turn counter expires, refresh the counter back to 16.
- A conversation turn means one inbound task that is handed to the agent runtime.
- Retention is in process for V1. It does not need to survive restart.

The state should be small:

```go
type ImageToolRetention struct {
    Enabled     bool
    TurnsLeft   int
    TriggeredAt time.Time
}
```

Suggested scope keys:

- Console: topic id
- CLI chat: one in-memory session key
- Telegram: chat id plus thread/message topic id when present
- Slack: channel id plus thread ts when present
- Lark: chat id plus root/thread message id when present
- LINE: user id, group id, or room id

Do not put this state in chat history. It is tool availability state, not user-visible conversation content.

## 8) File Semantics

Default output directory:

```text
file_cache_dir/images/
```

Default filename:

```text
<timestamp>-<short-id>.<ext>
```

Rules:

- Output paths may target `file_cache_dir` or `workspace_dir`.
- Relative output paths resolve under `file_cache_dir/images/`.
- Input paths for edit may read from `workspace_dir` or `file_cache_dir`.
- `image_generate` and `image_edit` accept and write PNG/JPEG only in V1. Do not advertise WebP support for these tools.
- V1 should not write generated images to `file_state_dir`.
- Enforce fixed byte limits before writing.
- If `output_path` has no extension, append one from the returned MIME type.
- If `output_path` has an extension that conflicts with the returned MIME type, replace the extension with the one for the returned MIME type. Do not silently transcode.
- Create parent directories with private permissions.
- Reject symlink escapes and paths outside allowed roots.
- Detect output extension from MIME type when possible.

## 9) Editing Images From Current Messages

For image edit to be useful, the agent must know the local path of user-provided images.

When a channel runtime downloads an inbound image, add a current-turn-only note to the prompt text:

```text
[attached image 1: file_cache_dir/telegram/abc.png]
```

Rules:

- The note should use root aliases, not absolute paths.
- The note should only describe images attached to the current user message.
- Persistent history can keep a generic marker such as `[image attached]`; it does not need stale cache paths.
- Image parts still go to the LLM as they do today.

This lets the agent call:

```json
{
  "input_path": "file_cache_dir/telegram/abc.png",
  "prompt": "Make the background white and keep the product unchanged."
}
```

## 10) Multi-Turn Image State

多轮图片编辑可以支持，但不要依赖 provider 记状态。`uniai.EditImage` 是无状态 API；morph 应该保存“当前图片指针”，下一轮编辑时把上一轮输出作为新的输入图。

目标行为：

- 用户先让 agent 生成或编辑图片。
- 工具成功后，把输出图片注册为当前会话的 active image。
- 下一轮用户说“再亮一点”“把背景换成白色”时，agent 可以直接调用 `image_edit`，使用 active image 作为输入。
- 每次成功编辑后，active image 指向新输出。

### 10.1 State Scope

State key 按会话隔离：

- Console: topic id
- Telegram: chat id，加 thread/message topic id（如果有）
- Slack: channel id，加 thread ts（如果有）
- Lark: chat id，加 thread/root message id（如果有）
- LINE: user id、group id 或 room id

不要做全局 active image。否则两个会话会互相污染。

### 10.2 State Store

新增一个很小的 image session store，放在 `file_state_dir`：

```json
{
  "version": 1,
  "scope": {
    "runtime": "telegram",
    "conversation_id": "tg:12345"
  },
  "active_image_id": "img_01J...",
  "images": [
    {
      "id": "img_01J...",
      "path": "file_cache_dir/images/20260514-120001-image-1.png",
      "mime_type": "image/png",
      "bytes": 123456,
      "source": "image_edit",
      "parent_ids": ["img_01H..."],
      "created_at": "2026-05-14T12:00:01Z"
    }
  ]
}
```

Rules:

- Store root aliases, not absolute paths.
- Store metadata only. Do not store image bytes in the manifest.
- Keep a short revision chain so the agent can refer to earlier outputs when needed.
- If the underlying image file is missing, ignore that entry and clear active image if it points to the missing file.

### 10.3 File Lifetime

An image that is needed for future edits is no longer ordinary temporary output.

V2 uses the first rule:

1. Store session-linked images under `file_cache_dir/images/` and make cache cleanup skip files referenced by active image manifests.
2. Or store session-linked images under `file_state_dir/image_sessions/` and update channel send tools to allow sending from that root.

The first rule has the smaller implementation cost because existing send/download paths already work with `file_cache_dir`. It does require cleanup to read the image manifests before deleting files.

### 10.4 Tool Semantics

Extend `image_edit` with one optional parameter:

| Name | Type | Required | Meaning |
| --- | --- | --- | --- |
| `use_active_image` | boolean | no | If true, use the current session active image as input |

Rules:

- `input_path` wins when provided.
- If `use_active_image` is true and there is no active image, return a clear error.
- If `use_active_image` is true and the active image file is missing, clear the active pointer and return a clear error.
- If both `input_path` and `use_active_image` are absent, keep the V1 behavior and require `input_path`.
- A successful `image_generate` or `image_edit` sets active image to the new output when the runtime has a conversation scope.

Do not add a separate `image_continue_edit` tool. It would only rename `image_edit`.

### 10.5 Prompt Injection

At the start of a turn, inject a short current-state note when active image exists:

```json
{
  "active_image": {
    "id": "img_01J...",
    "path": "file_cache_dir/images/20260514-120001-image-1.png",
    "mime_type": "image/png",
    "note": "Use image_edit with use_active_image=true for follow-up edits."
  }
}
```

For multiple recent candidates, include at most the last few images:

```json
{
  "recent_images": [
    {"id": "img_01J1", "path": "file_cache_dir/images/a.png", "active": false},
    {"id": "img_01J2", "path": "file_cache_dir/images/b.png", "active": true}
  ]
}
```

This note belongs in current-turn context. Do not append it permanently to normal chat history text.

### 10.6 User Control

The first pass does not need a new management tool. The agent can use explicit `input_path` from `recent_images` when the user says “use the first one” or “go back to the previous version”.

Add a separate state management tool only if real usage shows a need for commands like clear, pin, rename, or delete. Until then, state mutation happens through successful image tool calls.

## 11) Provider Options

Provider options come from `llm.image.options`, not from tool parameters.

- OpenAI settings live under `llm.image.options.openai`.
- Gemini settings live under `llm.image.options.gemini`.
- Cloudflare settings live under `llm.image.options.cloudflare`.

The tools should not expose a generic `provider_options` object. If a real need appears for a common parameter such as exact size, add one explicit cross-provider parameter later.

## 12) Observability

Log one event per image tool call:

- tool name
- provider
- model
- duration
- whether an input image was used
- output path
- active image id when state is updated
- usage token counts
- estimated cost when present

Do not log prompt text by default. Log prompt byte length instead.

Tool result returns usage and cost so the agent can report it if useful. Image generate and edit calls are also recorded through `internal/llmstats` with separate operation names, so the existing stats projection includes their request counts, token usage, duration, and returned cost.

## 13) Error Behavior

Return clear tool errors for:

- image tools disabled
- missing image model
- missing prompt
- missing input image
- input file outside allowed roots
- unsupported or unreadable input image
- provider returns no image
- provider returns URL-only output when V1 cannot save it
- active image is missing when `use_active_image` is requested
- output would exceed byte limit

Do not silently fall back from edit to generate.

## 14) Implementation Checklist

- [x] Upgrade `uniai` to a version that includes `Client.EditImage`.
- [x] Add image config parsing for `llm.image`.
- [x] Add tool config parsing for `tools.image_generate` and `tools.image_edit`.
- [x] Add `llm.ImageClient` and minimal image request/result types.
- [x] Add image methods to the `providers/uniai` adapter.
- [x] Add image client construction in `internal/llmutil`.
- [x] Add per-task image tool registration after base registry clone and before `agent.New(...)`.
- [x] Add in-process image tool retention state.
- [x] Keep image tools for console web topics after first trigger.
- [x] Keep image tools for CLI chat session after first trigger.
- [x] Keep image tools for channel sessions for 16 turns, refreshing on each trigger.
- [x] Implement `image_generate`.
- [x] Implement `image_edit`.
- [x] Hardcode one output image per call.
- [x] Hardcode one edit input image per call.
- [x] Hardcode prompt, input image, and output image byte limits.
- [x] Save generated files under `file_cache_dir/images/` by default.
- [x] Support explicit output paths under `workspace_dir` and `file_cache_dir`.
- [x] Normalize output path extensions to returned MIME types.
- [x] Keep image tool input and output formats limited to PNG/JPEG.
- [x] Add current-turn image path notes for inbound images.
- [x] Register image tools only when enabled, configured, and the task has explicit image intent or retained state.
- [x] Add Chinese, Japanese, and English tests for the image tool intent gate.
- [x] Update `assets/config/config.example.yaml`.
- [x] Update tool docs.
- [x] Add unit tests for config parsing.
- [x] Add unit tests for path resolution and file writing.
- [x] Add unit tests for tool schemas and validation errors.
- [x] Add provider adapter tests using fake `uniai` responses.
- [x] Record image generate and edit usage through `internal/llmstats`.
- [x] Run `go test ./...`.

V2 state checklist:

- [x] Add image session manifest store under `file_state_dir`.
- [x] Define conversation scope keys for console and channel runtimes.
- [x] Record successful image tool outputs in the session manifest.
- [x] Inject current active image state into current-turn prompt context.
- [x] Add `use_active_image` to `image_edit`.
- [x] Clear stale active image pointers when referenced files are missing.
- [x] Update file cache cleanup to skip files referenced by active image manifests.
- [x] Add tests for state isolation across topics/chats.
- [x] Add tests for follow-up edit using active image.
