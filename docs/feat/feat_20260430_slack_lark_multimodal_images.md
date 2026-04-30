---
date: 2026-04-30
title: Slack and Lark Multimodal Image Input
status: draft
---

# Slack and Lark Multimodal Image Input

## 1) Scope

Add inbound image understanding for Slack and Lark runtime messages.

V1 should only support images that arrive with the current user message. It should not add generic attachment browsing, arbitrary file reading, video, audio, OCR-specific tools, or outbound rich media changes.

The target behavior:

- If the runtime source is enabled in `multimodal.image.sources`, inbound images are downloaded to the runtime file cache and passed to the main LLM request as image parts.
- If the source is disabled, the runtime should produce a clear text-only fallback prompt, aligned with LINE.
- If the selected model does not support image parts, the message should still run as text-only.

## 2) Current State

Telegram and LINE already have the working shape:

- Inbound runtime stores image paths on the job.
- `build*PromptMessages(...)` receives `ImageRecognitionEnabled`.
- Image files are converted into `llm.PartTypeImageBase64` parts.
- Size, count, and MIME checks happen before building the LLM message.

Slack does not yet have this path:

- `slackEvent`, `slackInboundEvent`, `slackJob`, and `slackbus.InboundMessage` only carry text and message metadata.
- `BuildSlackRunOptions` does not read `multimodal.image.sources`.
- `buildSlackPromptMessages(...)` always builds plain text messages.

Lark does not yet have this path:

- `inboundMessageFromWebhookEvent(...)` only accepts text messages.
- `larkbus.InboundMessage` and `larkJob` do not carry image paths.
- `BuildLarkRunOptions` reads `multimodal.image.sources` for LINE only; Lark has no image flag.
- `buildLarkPromptMessages(...)` always builds plain text messages.

The config example currently lists `slack` as a supported image source, but Slack is not implemented. Lark is not listed.

## 3) First Principles

1. Runtime input decides whether an image exists.
   The LLM layer should only receive local image paths or ready-made `llm.Part` values. It should not know Slack or Lark file APIs.

2. Download belongs at the channel edge.
   Slack token handling and Lark token handling must stay in their runtime API layers.

3. Image handling should converge after download.
   After a platform image is stored in `file_cache_dir/<runtime>/`, Slack and Lark should reuse the same kind of local-image-to-LLM-parts logic used by Telegram and LINE.

4. No image content in memory by default.
   Chat history and memory should record that an image was present, but should not store base64 image data.

5. Failing to read an image should not crash the runtime.
   The user task can continue with a short text note such as "image attachment could not be read" unless the whole inbound event is malformed.

## 4) Config

Use the existing setting:

```yaml
multimodal:
  image:
    sources: ["telegram", "line", "slack", "lark"]
```

Update `assets/config/config.example.yaml` so the documented supported values match implementation.

Runtime behavior:

- `slack` present: Slack inbound image recognition enabled.
- `lark` present: Lark inbound image recognition enabled.
- Missing source: do not download image for LLM input; provide a text fallback when the user sent only images.

No new channel-specific config is needed in V1.

## 5) Shared Data Model

The smallest useful shape is still `[]string` of local image paths.

Add image path fields to channel-specific inbound/job structs:

- Slack:
  - `slackInboundEvent.ImagePaths []string`
  - `slackbus.InboundMessage.ImagePaths []string`
  - `slackJob.ImagePaths []string`

- Lark:
  - `larkbus.InboundMessage.ImagePaths []string`
  - `larkJob.ImagePaths []string`

If a platform needs delayed download, add `ImagePending bool` only where it is actually needed. LINE needs it because webhook processing and image download are split by message content API timing. Do not copy `ImagePending` into Slack or Lark unless their event flow needs the same delay.

For bus messages, reuse `MessageExtensions.ImagePaths`.

History items can remain text-first. If needed, add a short textual marker to the rendered current message:

```text
[image attachments: 2]
```

Do not add base64 to `ChatHistoryItem`.

## 6) Shared Image Builder

Telegram and LINE currently duplicate local image conversion rules. Slack and Lark should not add two more copies.

Add a small shared helper under a runtime-neutral internal package, for example:

```go
func BuildImageMessage(baseText string, model string, imagePaths []string, opts ImageMessageOptions) (llm.Message, error)
```

Suggested options:

- max images: `3`
- max bytes per image: `5 MiB`
- supported MIME types: PNG, JPEG, WebP where provider/model supports it
- optional WebP conversion hook only for Telegram if still needed

Keep this helper about local files and `llm.Part` construction only. It should not download remote files and should not import Slack, Lark, Telegram, or LINE packages.

If this extraction becomes too noisy, implement Slack/Lark with a minimal local helper first, then collapse duplication in a follow-up PR. Do not block image support on a broad refactor.

## 7) Slack Plan

### 7.1 Parse Image Metadata

Extend Slack event parsing to capture image files from message events.

Required fields should be the minimum needed to download and validate:

- file id
- MIME type or mimetype
- private download URL
- size when present
- filename when present

Ignore non-image files in V1.

### 7.2 Download to Cache

Add Slack API method for authenticated file download.

Rules:

- Use the bot token.
- Save under `file_cache_dir/slack/`.
- Enforce max image bytes before or during download.
- Only accept image MIME types.
- Use secure child directory creation, matching existing cache rules.

### 7.3 Runtime Wiring

Add `ImageRecognitionEnabled` to Slack run options and runtime task options.

In `BuildSlackRunOptions`, compute it with:

```go
sourceEnabled(cfg.MultimodalImageSources, "slack")
```

In the worker path:

- Parse Slack file metadata from the inbound event.
- If enabled, download images before enqueueing/running the job.
- Put local paths on `slackJob.ImagePaths`.
- Pass image paths into `buildSlackPromptMessages(...)`.

### 7.4 Prompt Message

Change:

```go
buildSlackPromptMessages(history, job)
```

to include model and image flag, aligned with Telegram/LINE:

```go
buildSlackPromptMessages(history, job, model, imageRecognitionEnabled, logger)
```

Use image parts only for the current message, not old history.

## 8) Lark Plan

### 8.1 Accept Image Messages

Extend webhook parsing beyond `message_type == "text"`.

V1 should support:

- text messages
- image messages
- image message with optional user text if Lark provides it in the event content

If the message is image-only and image recognition is enabled, synthesize a small task text:

```text
User sent an image.
```

If image recognition is disabled, use a clear fallback text similar to LINE:

```text
User sent an image, but image recognition is disabled in the current Lark runtime. Reply briefly and ask the user to describe the image in text or enable lark in multimodal.image.sources.
```

### 8.2 Download to Cache

Add the minimum Lark API method needed to download image binary content from the message content identifier.

Rules:

- Use the existing tenant token client.
- Save under `file_cache_dir/lark/`.
- Enforce max image bytes.
- Only accept image MIME types.
- Keep the API method local to Lark runtime; do not create a broad Lark SDK.

### 8.3 Runtime Wiring

Add `ImageRecognitionEnabled` to Lark run options and runtime task options.

In `BuildLarkRunOptions`, compute it with:

```go
sourceEnabled(cfg.MultimodalImageSources, "lark")
```

Add `ImagePaths` to `larkbus.InboundMessage` and `larkJob`, then pass them to prompt message building.

### 8.4 Prompt Message

Change:

```go
buildLarkPromptMessages(history, job)
```

to:

```go
buildLarkPromptMessages(history, job, model, imageRecognitionEnabled, logger)
```

Use image parts only for the current message.

## 9) Error Handling

Use text fallback instead of hard failures for normal media issues:

- unsupported MIME type
- image too large
- download failed
- model does not support image parts

Hard failure is acceptable for malformed runtime state:

- missing Slack channel/message identifiers
- missing Lark chat/message identifiers
- invalid configured cache directory

Log enough context to debug:

- channel
- message id
- image count
- skipped count
- reason

Do not log private download URLs or base64 image data.

## 10) Tests

### Slack

- Parse Slack message events with image files.
- Ignore non-image files.
- `BuildSlackRunOptions` enables image recognition when `slack` is in `multimodal.image.sources`.
- Download helper rejects non-image MIME and oversized images.
- `buildSlackPromptMessages` adds `llm.PartTypeImageBase64` for supported image models.
- Unsupported image models degrade to text-only.
- Bus adapter preserves `ImagePaths`.

### Lark

- Parse Lark text event as before.
- Parse Lark image event into inbound message.
- Ignore unsupported message types.
- `BuildLarkRunOptions` enables image recognition when `lark` is in `multimodal.image.sources`.
- Download helper rejects non-image MIME and oversized images.
- `buildLarkPromptMessages` adds image parts for supported image models.
- Unsupported image models degrade to text-only.
- Bus adapter preserves `ImagePaths`.

### Shared

- Shared image builder covers:
  - max image count
  - max bytes
  - MIME detection
  - base64 part generation
  - empty image list

## 11) Suggested PR Split

1. Shared local image builder extraction.
   No Slack/Lark behavior change.

2. Slack image input.
   Event parsing, download, config flag, prompt message parts, tests.

3. Lark image input.
   Webhook parsing, download, config flag, prompt message parts, tests.

4. Docs/config cleanup.
   Update user docs and `config.example.yaml` supported source list.

If the shared helper extraction starts to pull too much code around, split it after Slack and Lark work instead. The feature is the image input path, not a new media framework.

## 12) Acceptance Criteria

- With `multimodal.image.sources` containing `slack`, a Slack user can send an image and ask a question about it; the selected image-capable model receives an image part.
- With `multimodal.image.sources` containing `lark`, a Lark user can send an image and ask a question about it; the selected image-capable model receives an image part.
- With the source disabled, the runtime replies with a short text fallback instead of silently ignoring the image.
- Existing text-only Slack and Lark tests continue to pass.
- Telegram and LINE image behavior remains unchanged.

## 13) Implementation Tasks

1. Add shared local image message building.
   Extract only the file-to-`llm.Part` path after an image is already local. Keep platform download code outside this helper.

2. Wire Slack image metadata through runtime structs.
   Parse Slack file metadata, add image paths to inbound messages and jobs, and keep non-image files out of the image path.

3. Add Slack authenticated image download.
   Download only supported image MIME types into `file_cache_dir/slack/`, enforce size limits, and pass local paths into the current LLM message.

4. Wire Lark image metadata through runtime structs.
   Accept text and image message events, add image paths to inbound messages and jobs, and keep history text-only.

5. Add Lark authenticated image download.
   Download only supported image MIME types into `file_cache_dir/lark/`, enforce size limits, and pass local paths into the current LLM message.

6. Update config support lists.
   Add `lark` where the UI or config template lists supported image sources.

7. Add focused tests.
   Cover parsing, config flags, download validation, bus image path preservation, prompt image parts, disabled image fallback, and text-only model fallback.
