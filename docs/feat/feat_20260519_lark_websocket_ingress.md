---
date: 2026-05-19
title: Lark 长连接 WebSocket 接入改造
status: draft
---

# Lark 长连接 WebSocket 接入改造

## 1) 背景

当前 `mistermorph lark` 通过 HTTP webhook 接收消息。

这个实现能工作，但不适合作为默认接入方式：

- 进程需要公网 HTTPS 入口。
- 本地开发需要 tunnel 或反向代理。
- 事件订阅需要配置 `verification_token`，常见情况下还要配置 `encrypt_key`。
- runtime 要维护 webhook 校验、解密、签名检查和 HTTP server 生命周期。
- 用户要同时理解飞书/Lark 后台里的公网回调 URL，以及本机的 `webhook_listen`。

飞书/Lark 已经支持事件订阅长连接。应用进程通过官方 OpenAPI SDK 主动建立 WebSocket 连接，然后在这个连接上接收事件。这个模型更适合本项目：进程主动出站连接，不需要公网回调入口。

当初先做 webhook，大概率是因为它实现小、依赖少、回调模型清楚。现在要让普通部署更简单，默认方式应该改成长连接 WebSocket。

## 2) 决定

把 Lark/飞书的默认入站方式改成 WebSocket 长连接。

删除现有 webhook 入站代码，不保留兼容模式。新接入只使用 WebSocket。这样配置和运行时都只有一条入站路径，不长期维护两套事件接收逻辑。

不做 Telegram 那种轮询模式。飞书/Lark 收 bot 消息走事件订阅。平台可用选择是：

- 官方 SDK 的 WebSocket 长连接
- 发到开发者服务器的 HTTP webhook

本项目选择前者，并移除后者。

## 3) 平台事实

2026-05-19 核对的信息：

- 飞书/Lark 事件订阅支持长连接和开发者服务器回调两种接收方式。
- 官方 Go SDK 模块是 `github.com/larksuite/oapi-sdk-go/v3`。
- 官方 Go SDK 的 WebSocket 包是 `github.com/larksuite/oapi-sdk-go/v3/ws`。
- 官方示例使用 `dispatcher.NewEventDispatcher(...).OnP2MessageReceiveV1(...)` 注册消息事件，用 `larkws.NewClient(appID, appSecret, larkws.WithEventHandler(eventHandler))` 创建客户端，再调用 `Start(ctx)` 建立长连接。
- SDK WebSocket client 默认使用飞书域名，并提供 `WithDomain(...)`；Lark global 要传 Lark 域名。
- SDK WebSocket client 默认开启自动重连。

参考：

- 飞书事件概述：https://open.feishu.cn/document/server-docs/event-subscription-guide/overview
- 飞书事件订阅方式配置：https://open.feishu.cn/document/server-docs/event-subscription-guide/event-subscription-configure-/request-url-configuration-case
- 官方 Go SDK：https://github.com/larksuite/oapi-sdk-go
- 官方 Go SDK WebSocket 包：https://github.com/larksuite/oapi-sdk-go/tree/v3_main/ws
- 官方 Go SDK WebSocket 示例：https://github.com/larksuite/oapi-sdk-go/blob/v3_main/sample/ws/sample.go

## 4) 当前实现

当前路径：

```text
飞书/Lark 事件订阅
  -> 公网 HTTPS webhook URL
  -> tunnel 或反向代理
  -> Lark webhook HTTP server
  -> 校验 / 解密 / URL challenge
  -> 归一化为 larkbus.InboundMessage
  -> inproc bus
  -> 单会话 worker
  -> runLarkTask
  -> Lark delivery adapter
  -> reply/send API
```

应该保留的现有能力：

- `larkbus.InboundMessage` 作为归一化后的事件结构。
- `larkbus.InboundAdapter.HandleInboundMessage(...)` 负责去重和发布 bus 消息。
- 现有 reply/send delivery adapter。
- 图片 `image_key` 传递和图片下载路径。
- `allowed_chat_ids` 过滤。
- 群聊触发规则。
- contacts 观察。
- 当前 task runtime。

删除这些 webhook 细节：

- HTTP handler。
- URL verification 响应。
- webhook token 校验。
- webhook 解密和签名检查。
- 本地 webhook server 生命周期。

## 5) 目标结构

目标路径：

```text
mistermorph lark
  -> 官方 Lark/飞书 SDK WebSocket client
  -> event dispatcher 收到 im.message.receive_v1
  -> 归一化为 larkbus.InboundMessage
  -> larkbus.InboundAdapter.HandleInboundMessage(...)
  -> inproc bus
  -> 单会话 worker
  -> runLarkTask
  -> Lark delivery adapter
  -> reply/send API
```

发消息路径不改。这个改造只处理入站消息。

## 6) Telegram 工具对齐

当前 Telegram runtime 会在每次任务运行时注册这些 channel-specific tools：

- `telegram_send_file`
- `telegram_send_photo`
- `telegram_send_voice`
- `message_react`

这次 Lark 改造要一起补齐等价能力。Lark 不应只完成 WebSocket 入站，然后继续缺少 Telegram 已有的运行时工具。

目标工具：

- `lark_send_file`：发送 `file_cache_dir` 下的本地文件到当前 Lark chat，按文件/文档消息发送。
- `lark_send_photo`：发送 `file_cache_dir` 下的本地图片到当前 Lark chat，按图片消息发送，不降级成普通文件。
- `lark_send_voice`：发送 `file_cache_dir` 下的本地语音到当前 Lark chat，按 Lark 支持的音频/语音消息发送，不降级成文本。
- `message_react`：给当前 Lark 入站消息添加表情回复，语义对齐 Telegram 和 Slack 的同名工具。

实现要求：

- 在 `tools/lark/` 下新增 Lark 工具实现，不复用 `tools/telegram/` 的类型。
- 在 `runLarkTask` 的 per-run registry 中注册这些工具，注册时绑定当前 `chat_id`、当前 `message_id`、`file_cache_dir` 和文件大小上限。
- 文件路径解析要沿用 Telegram 工具的安全约束：只允许读取 `file_cache_dir` 下的文件，拒绝越界路径。
- Lark 文件、图片、语音发送需要先上传资源再发送消息时，把上传逻辑放在 Lark API adapter 内，不让工具直接拼底层 OpenAPI 请求。
- `message_react` 使用 Lark 的消息表情回复 API；可用 emoji 集合要和 Lark 平台支持值匹配，不要直接照搬 Telegram emoji 列表。
- 如果 Lark 平台对语音或 reaction 有租户/权限限制，启动错误或工具错误要直接说明缺少哪个权限。

验收：

- Lark task registry 中能看到 `lark_send_file`、`lark_send_photo`、`lark_send_voice`、`message_react`。
- `lark_send_file`、`lark_send_photo`、`lark_send_voice` 都拒绝 `file_cache_dir` 外路径。
- `message_react` 默认作用于当前入站 `message_id`。
- 群聊和私聊里这些工具都能使用同一套当前 chat 绑定逻辑。
- `docs/lark.md` 要列出这些工具及其需要的 Lark 权限。

## 7) 配置

```yaml
lark:
  base_url: "https://open.feishu.cn/open-apis"
  app_id: ""
  app_secret: ""
  allowed_chat_ids: []
  group_trigger_mode: "smart"
  addressing_confidence_threshold: 0.6
  addressing_interject_threshold: 0.6
  task_timeout: "0s"
  max_concurrency: 3
```

规则：

- 不新增 `ingress_mode`。Lark runtime 只有 WebSocket 入站模式。
- 删除 `verification_token` 和 `encrypt_key`。
- 删除 `webhook_listen` 和 `webhook_path`。
- `base_url` 继续作为 REST API base URL。
- WebSocket SDK domain 从同一个平台选择里推出来：
  - 飞书中国区：`https://open.feishu.cn`
  - Lark global：`https://open.larksuite.com`

先不新增 `lark.region`。目前一个 `base_url` 足够。

## 8) 开发者后台配置

WebSocket 模式：

1. 创建自建应用。
2. 复制 `App ID` 和 `App Secret`。
3. 启用机器人能力。
4. 打开事件订阅设置。
5. 选择长连接模式。
6. 订阅 `im.message.receive_v1`。
7. 授予群聊和私聊的消息接收权限。
8. 授予发送/回复消息权限。
9. 如果启用图片输入，授予消息资源权限，常见权限名是 `im:resource`。
10. 发布新版本。
11. 在租户里安装或刷新应用。
12. 把 bot 加入目标群聊，或直接和 bot 私聊。

WebSocket 模式不需要公网回调 URL。

## 9) 实现计划

### Phase 1: 依赖和配置

- 增加依赖：`github.com/larksuite/oapi-sdk-go/v3`。
- 从 config loading、run options 和 CLI flags 中删除 webhook-only 字段：
  - `webhook_listen`
  - `webhook_path`
  - `verification_token`
  - `encrypt_key`
- 删除 webhook HTTP server 启动路径。
- 删除 `cmd/mistermorph/larkcmd` 里的 webhook flags。
- 删除 `internal/channelopts` 里的 webhook config 字段和读取逻辑。
- 删除 `internal/channelruntime/lark` run options 里的 webhook 字段。
- 更新 `assets/config/config.example.yaml`。
- 检查 Console 设置页和 runtime support，确认没有残留 Lark webhook 表单、文案或默认值。
- 实现完成后更新 `docs/lark.md`。

验收：

- 缺少 `app_id` 或 `app_secret` 时，仍然在 runtime 启动前报清楚。
- help 和 config example 不再出现 webhook-only 字段。
- `rg "lark-webhook|lark.webhook|lark.verification_token|lark.encrypt_key"` 不应再命中运行时代码、配置模板或 Console UI。

旧配置处理：

- 旧 `config.yaml` 里残留的 webhook-only keys 不作为兼容路径读取。
- 文档迁移步骤要求用户删除这些 keys。
- 如果实现里保留启动时提示，只能提示“这些配置已废弃，请删除”，不能用它们启动 webhook。

### Phase 2: SDK 事件入站

- 在 Lark runtime package 下增加 WebSocket 入站文件。
- 用 SDK event dispatcher 注册 `im.message.receive_v1`。
- 把 SDK event 转成现有 `larkbus.InboundMessage`。
- 复用现有文本和图片 content 解析逻辑。
- 复用现有 chat type 归一化、mention 提取和 `allowed_chat_ids` 过滤。
- 保留现有图片 fallback 文案。
- 通过 `larkbus.InboundAdapter.HandleInboundMessage(...)` 发布 bus 消息。

验收：

- 私聊文本消息进入 bus，字段和现有归一化结果一致。
- 群聊文本消息进入 bus，字段和现有归一化结果一致。
- 图片事件在启用图片识别时保留 `ImageKeys`。
- `allowed_chat_ids` 在发布 bus 前生效。
- 去重仍然由 inbound adapter 处理。

### Phase 3: runtime 启动

- `runLarkLoop` 只启动 SDK WebSocket client，直到 context 取消或出现 fatal error。
- bus、delivery adapter、task workers、contacts service、runtime API server 继续共用。
- 启动日志里记录 SDK domain、base URL、allowlist 数量和关键运行参数。

验收：

- runtime 不再绑定 `webhook_listen`。
- 启动日志足够定位 app/domain 配错的问题。
- context 取消时进程干净退出。
- webhook 相关测试被删除，或改写为 WebSocket event conversion 测试。

### Phase 4: Lark 工具对齐

- 新增 `tools/lark` package。
- 新增 Lark tool API adapter，覆盖文件、图片、语音、reaction。
- 在 `runLarkTask` 注册 `lark_send_file`、`lark_send_photo`、`lark_send_voice`、`message_react`。
- 更新 Lark prompt block，让模型知道可用工具和轻量回复时可以用 `message_react`。
- 更新 `docs/lark.md` 的权限说明。

验收：

- Lark 工具覆盖 Telegram 当前 channel-specific tools 的能力。
- 工具注册只发生在 Lark runtime task 中，不影响 console、Telegram、Slack、LINE。
- 工具单测覆盖成功路径、越界路径、缺少 API adapter、缺少当前 message id。

### Phase 5: 测试和人工验证

新增单测：

- SDK event 转换覆盖现有归一化行为：
  - 私聊文本
  - 带 mention 的群聊文本
  - 图片消息
  - 非 user sender
  - 不支持的 message type
  - 缺少 chat/message/user ID
- domain 选择能正确区分飞书和 Lark。

人工验证：

- 飞书中国区应用使用长连接收到私聊消息并回复。
- 飞书中国区应用收到群聊 mention 并回复。
- 如果有 Lark global 凭据，做一次 Lark global smoke test。
- 图片消息能进入支持图片的模型。
- 在 Lark 私聊中用 `lark_send_file`、`lark_send_photo`、`lark_send_voice` 各发送一次。
- 在 Lark 群聊中用 `message_react` 给当前消息加一次表情。
- `mistermorph lark --help` 不再出现 webhook flags。
- `go test ./internal/channelruntime/lark ./internal/channelopts ./cmd/mistermorph/...` 通过。

### Phase 6: 清理

随实现一起完成：

- 删除 webhook handler、webhook tests 和 webhook-only config。
- 删除 `docs/lark.md` 中的 webhook 接入说明，替换成长连接接入说明。
- 删除旧部署里关于公网 callback URL/tunnel 的推荐。
- 在 release notes 或 PR 描述中明确写出破坏性变化：Lark webhook ingress 被删除，开发者后台必须切到长连接。

## 10) 代码位置

保持改动小：

- 不做完整 Lark SDK wrapper。
- 不新增 channel 抽象。
- 不手写 WebSocket 协议。
- 不复制 task runtime。

建议涉及文件：

- `internal/channelruntime/lark/websocket.go`
- `internal/channelruntime/lark/websocket_test.go`
- `internal/channelruntime/lark/webhook.go` 删除
- `internal/channelruntime/lark/webhook_test.go` 删除
- `internal/channelruntime/lark/runtime_options.go`
- `internal/channelruntime/lark/runtime.go`
- `internal/channelruntime/lark/tool_adapter.go`
- `cmd/mistermorph/larkcmd/command.go`
- `internal/channelopts` 里的 Lark config builder
- `tools/lark/`
- `assets/config/config.example.yaml`
- `cmd/mistermorph/root_config_test.go`
- `cmd/mistermorph/consolecmd/runtime_support.go`
- `web/console/src/views/SettingsView.*`，如果后续出现 Lark webhook 表单
- `docs/lark.md`

在边界处使用官方 SDK，尽早转成 `larkbus.InboundMessage`。进入 bus 后，不应该再关心底层事件传输方式。

## 11) Domain 处理

当前配置使用 REST base URL：

- 飞书中国区 REST base：`https://open.feishu.cn/open-apis`
- Lark global REST base：`https://open.larksuite.com/open-apis`

SDK WebSocket client 需要平台根域名：

- 飞书中国区 SDK domain：`https://open.feishu.cn`
- Lark global SDK domain：`https://open.larksuite.com`

从 `lark.base_url` 推导 SDK domain：

- 如果末尾是 `/open-apis`，去掉它。
- 保留 scheme 和 host。
- 空值或非法 URL 在启动 WebSocket 模式前报错。

不要让用户配置两个域名。

## 12) 错误处理

启动错误应该提示可能原因：

- app credentials 错误。
- 飞书/Lark domain 配错。
- 应用没有配置成长连接模式。
- 事件订阅缺少 `im.message.receive_v1`。
- 缺少消息接收权限。
- 应用没有在租户里安装或刷新。

运行时行为：

- 先使用 SDK 内置自动重连。
- 不先加外层重连循环，除非测试确认 `Start(ctx)` 会在临时断线时返回。
- 如果 `Start(ctx)` 返回 fatal error，`runLarkLoop` 直接返回这个错误。
- 不提供 webhook fallback。

## 13) 迁移步骤

已有 webhook 部署：

1. 升级 binary。
2. 从 config 中删除 `webhook_listen`、`webhook_path`、`verification_token`、`encrypt_key`。
3. 在飞书/Lark 开发者后台把事件订阅切到长连接。
4. 保留 `im.message.receive_v1` 和原有权限。
5. 删除部署里的公网 callback URL/tunnel。
6. 启动 `mistermorph lark`。
7. 给 bot 发一条私聊消息。
8. 在群里 mention bot。

回退只能回退 binary 和配置到旧版本。本改造后的版本不提供 webhook 模式。

## 14) 风险

- SDK 在飞书中国区和 Lark global 的行为可能有差异。能拿到凭据时，两边都要测。
- SDK event struct 可能没有暴露当前 webhook JSON 里的全部字段。如果缺字段，只为这个字段读取 SDK raw event body，不保留两套完整解析器。
- 如果开发者后台还配置成 webhook callback，runtime 收不到事件。
- 部分租户需要显式授予 P2P/group 读权限，消息事件才会下发。
- 直接删除 webhook 会影响已有 webhook 部署。迁移文档必须写清楚后台事件订阅要改成长连接。
- Lark 的文件、图片、语音和 reaction 权限可能和消息收发权限分开。工具实现前要按官方文档核对权限名。
- Lark reaction 的 emoji 类型可能不是 Unicode emoji 原文。`message_react` 要做平台值映射，不能把 Telegram 的 emoji 列表原样塞给 Lark API。

## 15) 完成标准

- 新 Lark/飞书接入不需要公网 webhook URL。
- `mistermorph lark` 能通过 WebSocket 长连接接收私聊和群聊消息。
- 回复仍然使用现有 Lark send/reply API。
- Lark runtime 具备 Telegram 当前 channel-specific tools 的等价能力：发送文件、发送图片、发送语音、消息 reaction。
- memory、contacts、群聊触发、图片输入、task runtime 行为不变。
- 代码、config example、CLI help 和文档里不再保留 webhook 入站模式。

## 16) 任务拆解 Checklist

- [x] 添加官方 Lark/飞书 Go SDK 依赖。
- [x] 删除 Lark webhook-only CLI flags、config 字段、run options 和配置模板。
- [x] 删除 Lark webhook HTTP handler、webhook server 启动路径和 webhook tests。
- [x] 新增 Lark WebSocket ingress，使用 SDK long connection 接收 `im.message.receive_v1`。
- [x] Console 托管 runtime 支持 `console.managed_runtimes: ["lark"]`。
- [x] 将 SDK event 归一化为现有 `larkbus.InboundMessage`，覆盖文本、图片、mention、chat type、sender、event id。
- [x] 从 `lark.base_url` 推导 SDK WebSocket domain，支持飞书中国区和 Lark global。
- [x] 保持现有 bus、contacts、memory、group trigger、image input 和 send/reply delivery 行为。
- [x] 新增 `tools/lark`，实现 `lark_send_file`、`lark_send_photo`、`lark_send_voice`、`message_react`。
- [x] 在 `runLarkTask` per-run registry 注册 Lark channel tools，并绑定当前 chat/message 上下文。
- [x] 扩展 Lark REST API adapter：上传文件、上传图片、发送文件/图片/语音消息、添加消息 reaction。
- [x] 更新 Lark prompt block，说明可用 channel tools 和轻量回复 reaction 规则。
- [x] 更新 `docs/lark.md`，改成长连接接入说明，删除 webhook 接入说明。
- [x] 更新测试：WebSocket event conversion、domain 推导、config 删除、Lark tools、runtime task registry。
- [x] 运行 `gofmt`。
- [x] 运行相关 Go 测试。
- [x] 确认 `rg "lark-webhook|lark.webhook|lark.verification_token|lark.encrypt_key"` 不命中运行时代码、配置模板或 Console UI。
