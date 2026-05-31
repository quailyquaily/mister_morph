---
date: 2026-05-31
title: Channel Image History Metadata
status: draft
---

# Channel Image History Metadata

## 背景

当前 channel runtime 收到图片后，会把下载后的本地路径放进本轮 job 的 `ImagePaths`，并在当前 LLM 请求中作为 image parts 发送给支持多模态的模型。

这个字段只服务于本轮执行。task 完成后，chat history 只保存文本、sender、message id 等信息，没有结构化保存图片路径、类型、尺寸，也没有把模型本轮对图片的理解回写到图片记录里。

这会带来几个问题：

1. 后续对话只能看到“之前有图片”或普通文本路径，不能稳定知道图片元信息。
2. group addressing、memory、history prompt 无法使用结构化图片上下文。
3. 多张图片进入同一轮时，没有稳定 id 可以描述和追踪每张图片。

## 目标

在 channel chat history 中保存入站图片的结构化元信息。

每条 inbound history item 可以包含多张图片，每张图片有稳定 `image_id`：

```go
type ChatHistoryImage struct {
    ID                 string `json:"image_id"`
    Path               string `json:"path,omitempty"`
    MIMEType           string `json:"mime_type,omitempty"`
    Width              int    `json:"width,omitempty"`
    Height             int    `json:"height,omitempty"`
    Bytes              int64  `json:"bytes,omitempty"`
    ContentSHA256      string `json:"content_sha256,omitempty"`
    SourceMessageID    string `json:"source_message_id,omitempty"`
    SourceAttachmentID string `json:"source_attachment_id,omitempty"`
    Description        string `json:"description,omitempty"`
    DescriptionSource  string `json:"description_source,omitempty"`
}
```

`ChatHistoryItem` 增加：

```go
Images []ChatHistoryImage `json:"images,omitempty"`
```

## 非目标

1. 不把图片二进制、base64、OCR 原文大块内容写进 history。
2. 不无条件把历史图片重新作为 image parts 发送给模型。
3. 不新增独立 caption 调用。第一版只使用本轮 agent final output 作为整体 description。
4. 不保存本机绝对路径。

## 当前可用匹配键

各 channel 已有稳定 message/task 标识：

1. Telegram：`chat_id` + `message_thread_id` + `message_id`，task id 来自 `telegramTaskID(...)`。
2. Slack：`team_id` + `channel_id` + `message_ts`，task id 来自 `slackTaskID(...)`。
3. LINE：`chat_id` + `message_id`，task id 来自 `lineTaskID(...)`。
4. Lark：`chat_id` + `message_id`，task id 来自 `larkTaskID(...)`。

正常 task 的 inbound history 目前在 task 完成后追加。完成回调同时持有：

1. 本轮 `job`。
2. 本轮 `finalOutput`。
3. job 中的 `ImagePaths`。
4. channel message id / task id。

因此第一版不需要回头查找之前的 history item。可以在 task 完成时直接构造带 `Images` 的 inbound history item。

## `image_id` and content hash

每张图片必须有稳定内容 id。`image_id` 使用图片文件内容 SHA-256 派生：

```text
content_sha256 = sha256(image_bytes)
image_id = img_<content_sha256[:16]>
```

完整 hash 同时写入 `content_sha256`。

要求：

1. 同一张图片内容在不同消息、不同 channel 或不同下载路径下得到相同 id。
2. 可以通过 `content_sha256` 对比 history 中是否已有同一张图片。
3. 不依赖本机绝对路径。
4. 不依赖 alias path。
5. 不依赖 channel 原生 attachment id。

`source_message_id` 和 `source_attachment_id` 仍保存来源身份，用来追踪图片来自哪条消息和哪个平台附件。`source_attachment_id` 使用 channel 原生图片标识：

1. Telegram：document/photo 的 `file_id`。如果后续取得 `file_unique_id`，优先使用 `file_unique_id`。
2. Slack：file `id`。
3. LINE：没有单独 attachment id 时，使用 `image`。
4. Lark：`image_key`。

如果 channel 原生 attachment id 不存在，`source_attachment_id` 留空。内容 SHA-256 不可用时，整张图片不写入 `Images`。

## path alias

history 中的 `path` 必须是 root alias，不保存绝对路径。

下载目录规则：

1. 如果当前 task 有 `workspace_dir`，channel inbound 图片应下载到 `workspace_dir/.mistermorph/images/<channel>/...`。
2. 如果当前 task 没有 `workspace_dir`，继续下载到 `file_cache_dir/<channel>/...`。
3. 下载目录必须通过 secure child dir 校验，不能被路径穿越。

优先级：

1. 如果图片位于当前 `workspace_dir` 下，保存 `workspace_dir/...`。
2. 否则如果图片位于 `file_cache_dir` 下，保存 `file_cache_dir/...`。
3. 如果无法转换成 alias，不保存 `path`，也不回退为绝对路径。

路径统一使用 `/` 分隔。

## 元信息采集

对每个本地图片文件采集：

1. `path`：alias path。
2. `mime_type`：优先从文件内容或已知下载元信息判断；没有时从扩展名推断。
3. `width` / `height`：使用图片 header 读取，不解码整张图。
4. `bytes`：文件大小。
5. `content_sha256`：图片文件内容的 SHA-256 hex。

如果某项读取失败，只跳过该字段，不影响整条 history 写入。

实现上应提供一个统一 builder，例如：

```go
type ChatHistoryImageInput struct {
    SourceMessageID    string
    SourceAttachmentID string
    LocalPath          string
    MIMEType           string
    Description        string
    DescriptionSource  string
}
```

各 channel 在下载阶段保留 `source_message_id`、`source_attachment_id`、下载元信息和本地路径，完成回调中统一构造 `Images`。

## description 回填

第一版不做逐图 caption。规则：

1. 如果本轮有图片，并且模型产生了可发布的文本 `finalOutput`，把同一个 `finalOutput` 写入这些图片的 `description`。
2. 同时写入：

```json
"description_source": "agent_final"
```

含义：

1. 这是模型对本轮“消息 + 图片”的整体理解或回答。
2. 多图场景下不保证它是逐图 caption。
3. 如果 final 是 lightweight reaction 或没有可用文本，不写 description。

以后如果需要准确的逐图 caption，可以在这个字段上新增来源，例如 `vision_caption`，但这不是第一版目标。

## Prompt 展示

history prompt 只展示结构化元信息和 description。

示例：

```json
{
  "text": "latest",
  "images": [
    {
      "image_id": "img_abc123",
      "path": "file_cache_dir/telegram/chat_1/tg_100_photo.jpg",
      "mime_type": "image/jpeg",
      "width": 1280,
      "height": 720,
      "bytes": 245100,
      "content_sha256": "0123456789abcdef...",
      "source_message_id": "100",
      "source_attachment_id": "telegram_file_id",
      "description": "The image shows ...",
      "description_source": "agent_final"
    }
  ]
}
```

约束：

1. `RenderHistoryContext` 和 `RenderCurrentMessage` 都可以输出 `images`。
2. 历史消息不附带 image parts。
3. 当前消息仍按现有逻辑把图片作为 image parts 传给支持多模态的模型。
4. 如果当前 Telegram 消息 quote 了带图片的历史消息，runtime 可以把被 quote 的图片路径恢复成当前请求的 image part。
5. 当前消息的文本说明中应包含 `image_id` 与 alias path，方便模型在回复里引用图片。

## Channel 行为

### Telegram

Telegram 当前会下载 message 或 reply message 中的文件，并从下载结果筛选图片。

要求：

1. 使用下载后的图片路径构造 `Images`。
2. 如果下载结果带 MIME 信息，优先写入 `mime_type`。
3. `source_message_id` 保留图片实际来源 message id。
4. `source_attachment_id` 使用 Telegram file id。后续如果取到了 `file_unique_id`，优先使用 `file_unique_id`。
5. `source_message_id` 使用图片来源 message id，而不是当前触发 message id。
6. 如果新消息 quote 的历史消息包含图片，且该图片已有 alias path，则把这张历史图片恢复为当前请求的 image part。
7. 如果当前消息和 quote 历史消息都有图片，当前消息图片优先，quote 图片其次，仍受现有图片数量上限限制。

### Slack

Slack 当前在图片识别启用时下载 image file 到 cache。

要求：

1. 使用 `event.ImagePaths` 构造 `Images`。
2. `source_message_id` 使用 `message_ts`。
3. `source_attachment_id` 使用 Slack file id。
4. history text 中的 `[image attachments: N]` 可以保留，但结构化 `images` 是主要信息。

### LINE

LINE 当前 image event 会先标记 pending，再下载图片到 cache。

要求：

1. 下载成功后使用 `inbound.ImagePaths` 构造 `Images`。
2. `source_message_id` 使用 LINE message id。
3. `source_attachment_id` 使用 `image`。

### Lark

Lark 当前通过 image key 下载图片到 cache。

要求：

1. 使用 `inbound.ImagePaths` 构造 `Images`。
2. `source_message_id` 使用 Lark message id。
3. `source_attachment_id` 使用 Lark `image_key`。
4. history text 中的 `[image attachments: N]` 可以保留，但结构化 `images` 是主要信息。

## Memory

Memory record 的 `SourceHistory` 应保留 `Images`。

要求：

1. memory journal 可以看到图片元信息和 description。
2. memory summary 不需要保存二进制图片。
3. 如果 memory prompt 使用 history JSON，`images` 应按同一格式出现。

## 验收

1. Telegram / Slack / LINE / Lark 收到图片并完成一轮多模态任务后，inbound history item 包含 `images`。
2. 每张图片都有稳定的内容派生 `image_id`。
3. 有 workspace 时图片下载到 `workspace_dir/.mistermorph/images/<channel>/...`。
4. history 中的图片路径是 `workspace_dir/...` 或 `file_cache_dir/...`，没有绝对路径。
5. history prompt 中能看到图片 path、mime type、尺寸、bytes、content hash 和 description。
6. 历史图片不会无条件作为 image parts 重新发送；Telegram quote 带图历史消息时可以重发被 quote 的图片。
7. 多图消息中每张不同内容的图片都有独立 `image_id`，第一版 description 可以相同，来源为 `agent_final`。

## Implementation Checklist

- [x] Write the feature requirement.
- [x] Add chat history image schema and prompt rendering.
- [x] Add image metadata builder for stable IDs, alias paths, MIME type, dimensions, and byte size.
- [x] Download channel inbound images under the workspace when one exists.
- [x] Preserve source message and attachment identifiers for Telegram, Slack, LINE, and Lark images.
- [x] Store content SHA-256 and derive image IDs from image bytes.
- [x] Write image metadata into inbound chat history and memory source history.
- [x] Add image IDs and alias paths to current-message image notes.
- [x] Restore quoted Telegram history images as current request image parts.
- [x] Run Go tests for the touched packages.
