---
date: 2026-08-27
title: Mixin Messenger Channel
status: in_progress
---

# Mixin Messenger Channel

## 1. 结论

Morph 增加一个 `mixin` Channel，覆盖两个主要场景：

1. 人类与 Morph Bot 私聊。
2. Morph Bot 作为成员加入群聊，接收明确 `@Bot` 的消息并回复。

第一版在 Mixin 平台能力允许的范围内对齐 Telegram Runtime。这里的“对齐”是复用相同的 Morph 行为，不是复制 Telegram API：

- 私聊直接进入任务。
- 群聊消息由 Mixin 在传输层按 mention 投递；Morph 不再重复判断 trigger。
- 支持 allowlist、每会话串行、跨会话并发。
- 支持共享命令、审批、文件输入输出、Contacts、主动发送、Agent 配对和 managed runtime。
- 不为 Mixin 重新实现 task、history、journal、bus、Contacts 或 runtime API。

Mixin Messenger 同时有钱包能力，但它与本需求无关。第一版不读取资产，不处理转账，不注册支付工具，也不使用 keystore 中的 PIN 和 `pin_token`。

## 2. 官方协议事实

### 2.1 Bot 身份和凭据

Mixin Developer Dashboard 创建的 Application 默认可以作为 Messenger Bot。Mixin 将普通用户和 Bot 都视为 user；主要区别是 Bot 的私钥由开发者保管。Bot 可以与用户私聊，也可以作为成员加入群聊。

Dashboard 生成的 Ed25519 keystore 至少包含：

- `app_id`：Bot 的 user UUID；兼容旧字段名 `client_id`。
- `session_id`：签名会话 UUID。
- `private_key`：Ed25519 私钥。
- `pin`、`pin_token`：钱包操作使用，本功能不读取。

HTTP API 使用 `https://api.mixin.one`，消息 WebSocket 使用 `wss://blaze.mixin.one`。私有请求使用 EdDSA JWT；签名摘要绑定 HTTP method、path 和 body。
每个 REST 请求生成新的 JWT，不缓存或复用 token。

### 2.2 收消息

Mixin 消息服务使用 Blaze WebSocket：

1. 使用 keystore 签名 `GET /`，连接 `wss://blaze.mixin.one/`。
2. WebSocket subprotocol 是 `Mixin-Blaze-1`。
3. 连接后先发送 `LIST_PENDING_MESSAGES`。
4. 接收 gzip 压缩的 binary frame。
5. `CREATE_MESSAGE` 提供 `conversation_id`、`user_id`、`message_id`、`category`、`data_base64`、`quote_message_id` 和时间。
6. 消息处理后发送 `READ` acknowledgement；未确认的消息会再次推送。

平台只保留七天未处理消息。因此 Morph 需要自动重连和拉取 pending messages，但无法补回七天以前从未确认的消息。

### 2.3 发消息

Bot 使用 `POST /encrypted_messages` 发消息。发送前通过 `POST /sessions/fetch` 获取每个接收者的 session public key，并为每个 recipient 加密 payload。请求由客户端生成 UUID `message_id`，可以一次发送最多 100 条，整个请求体上限为 128 KB。`quote_message_id` 可以让回复引用原消息。

私聊发送前必须存在 `CONTACT` conversation。私聊 conversation ID 由双方 user UUID 按官方算法确定；用户已经主动发消息或添加 Bot 时，会话通常已经存在。主动联系一个从未交互的用户时，先调用 `POST /conversations` 创建会话。

群聊使用 `GROUP` conversation。一个群最多 256 名参与者。群资料由 `GET /conversations/:id` 提供，包括名称、头像、公告和参与者。

### 2.4 消息类型和附件

本功能需要的官方类型如下：

| Mixin category | 用途 |
| --- | --- |
| `ENCRYPTED_TEXT` / `PLAIN_TEXT` | 普通文本；出站使用 encrypted，入站兼容两者 |
| `ENCRYPTED_POST` / `PLAIN_POST` | Markdown 长文；入站兼容两者 |
| `ENCRYPTED_IMAGE` / `PLAIN_IMAGE` | 图片；出站使用 encrypted，入站兼容两者 |
| `ENCRYPTED_AUDIO` / `PLAIN_AUDIO` | 音频；出站使用 encrypted，入站兼容两者 |
| `ENCRYPTED_DATA` / `PLAIN_DATA` | 文件；出站使用 encrypted，入站兼容两者 |
| `APP_BUTTON_GROUP` | 按钮组；是否可由 Bot 发送需要真实账号验证 |
| `SYSTEM_CONVERSATION` | 群成员和会话状态变化 |

图片、音频和文件都先调用 `POST /attachments` 获取 `upload_url` 和 `attachment_id`，上传后再发送消息。接收附件时通过 `GET /attachments/:id` 获取 `view_url`，再下载文件。

公开 Message Category 文档仍写着 Bot 只支持 `PLAIN_`，但官方 Go SDK 和 Mixin Safe 已在 2026-08-25 至 2026-08-27 接入 encrypted message。Morph 以最新官方实现为准，同时保留 `PLAIN_*` 入站兼容。按钮能力仍需真实 Bot smoke test；审批先提供完整的纯文本命令。

### 2.5 提及和引用

官方 Mixin Messenger 客户端的普通群消息使用 `SIGNAL_*` 群组加密，Bot API 不提供这类消息的群组解密会话。客户端只从正文中的 `@<identity_number>` 提取 Bot user ID，写入 Blaze 的 `mentions` 字段。Mixin 据此向 Bot 提供可读消息。

`quote_message_id` 和 `mentions` 是两个独立字段。回复 Bot 只设置引用，不会自动提及 Bot，因此 Bot 收不到仅 reply 的群消息。用户需要同时 `@Bot`。Morph 收到可读群消息后直接视为已寻址，并从正文移除自己的 mention。

## 3. Telegram 对齐范围

| 能力 | Mixin 第一版 | 说明 |
| --- | --- | --- |
| 人类私聊 | 支持 | 有效私聊消息直接进入 main run |
| 群聊 | 部分支持 | 只接收明确 @Bot 的可读消息 |
| `allowed_chat_ids` 等价能力 | 支持 | 使用 `allowed_conversation_ids` |
| 明确 @Bot | 支持 | 匹配 Bot 的 `identity_number` |
| 回复 Bot | 部分支持 | reply 必须同时 @Bot |
| 每会话串行 | 支持 | key 为 Mixin `conversation_id` |
| 上下文、sticky skills、context checkpoint | 支持 | 复用 channel task runtime |
| 共享 slash commands | 支持 | 私聊可直接使用；群聊必须同时提及 Bot |
| `/stop` 和 steering | 支持 | 复用现有任务控制 |
| 审批 | 支持 | 完整文本和命令必定可用；验证后增加 `APP_BUTTON_GROUP` |
| 图片输入 | 支持 | 下载到 `file_cache_dir/mixin/` 后进入现有 image input |
| 文件、图片、音频输出 | 支持 | 增加 Mixin 对应 runtime tools |
| `contacts_send` / `agent_send` | 支持 | 使用 Mixin user UUID 或 conversation UUID 路由 |
| Agent 配对 | 支持 | 复用 `internal/agentpair`，只改平台身份和发送实现 |
| Cron Notify Chat | 支持 | chat ID 使用 `mixin:<conversation_uuid>` |
| chat profile | 支持 | 群名写现有 chat profile；Bot 名称和头像缓存在 runtime health |
| managed runtime | 支持 | `console.managed_runtimes` 可包含 `mixin` |
| Runtime API | 支持 | 继续使用 `/runtime`，不新增 Mixin 专用管理 API |
| typing 状态 | 不支持 | 官方 Bot API 没有已文档化的等价接口 |
| reaction | 不支持 | 官方消息类型和 Go Bot SDK 未提供已文档化的 reaction API；不注册 `message_react` |
| Telegram topic/thread | 不支持 | Mixin conversation 没有等价层级 |
| Telegram HTML/Markdown 气泡 | 不完全对齐 | 普通回复使用 `ENCRYPTED_TEXT`；不把每条回答都变成 POST |
| 编辑进度消息 | 不支持 | 不用多条临时消息模拟 typing 或 edit，避免刷屏 |

不支持的平台能力不能用额外轮询、伪 reaction 或频繁状态消息模拟。这样会增加延迟和噪声，却不能得到等价体验。

## 4. 配置

新增最小配置：

```yaml
mixin:
  # Developer Dashboard 生成的 Ed25519 keystore 文件。
  # 推荐通过 MISTER_MORPH_MIXIN_KEYSTORE_FILE 设置。
  keystore_file: ""

  # 可选的 conversation UUID allowlist。空数组允许所有会话。
  allowed_conversation_ids: []

  # 0 使用顶层 timeout。
  task_timeout: "0s"

  # 不同 conversation 可并发；同一 conversation 保持串行。
  max_concurrency: 3

  # Mixin standalone runtime 的管理 API 地址。
  serve_listen: ""
```

同时增加以下 CLI 参数：

```text
--mixin-keystore-file
--mixin-allowed-conversation-id
--mixin-task-timeout
--mixin-max-concurrency
```

不增加以下配置：

- API base URL 和 Blaze URL：正式运行固定使用官方地址；测试通过依赖注入替换。
- reconnect interval、profile cache TTL、ack batch size：使用固定且保守的内部值。
- attachment enabled：与 Telegram 一样，文件处理始终启用并受现有固定大小限制。
- wallet、PIN、asset 或 payment 配置。

相对 `keystore_file` 按 `config.yaml` 所在目录解析。Unix 下 keystore 不能向 group 或 other 开放任何权限，推荐使用 `0600`。启动时只解析新版字段 `app_id`、`session_id`、`session_private_key`，并兼容旧字段名 `client_id`、`private_key`。不得把 keystore 内容、JWT 或附件下载 URL 写入 log、journal、task metadata 或 API response。

Console 模式配置如下：

```yaml
console:
  managed_runtimes: ["mixin"]

mixin:
  keystore_file: "/path/to/keystore.json"
```

也可以单独启动：

```bash
mistermorph mixin --mixin-keystore-file /path/to/keystore.json
```

### 4.1 首次启用

1. 在 Mixin Developer Dashboard 创建 Application，保存 Ed25519 keystore。
2. 配置 `mixin.keystore_file`，首次启动时将 `allowed_conversation_ids` 留空。
3. 私聊 Bot，发送 `Hi`，确认 Morph 收到并回复。
4. 把 Bot 加入测试群，使用客户端的成员选择器生成 Bot mention，然后发送 `@<bot_identity_number> /id`。
5. `/id` 返回当前 conversation UUID。需要 allowlist 时，把实际使用的私聊和群聊 UUID 写入 `allowed_conversation_ids`，再重启 managed runtime。

不要手工拼接群 conversation UUID。私聊 conversation UUID 虽可由双方 user UUID 按官方算法确定，也应优先使用实际收到消息中的 `conversation_id`。

## 5. 身份和路由

### 5.1 Canonical ID

| 对象 | Morph 表示 |
| --- | --- |
| Mixin user / Bot | `mixin:<user_uuid>` |
| Mixin conversation | `mixin:<conversation_uuid>` |
| Bus channel | `mixin` |
| Chat history channel | `mixin` |

user UUID 和 conversation UUID 都是 UUID，但使用位置不同：

- `contact_id` 指 user。
- `chat_id` 和 `conversation_key` 指 conversation。

不把 `identity_number` 当 canonical ID。它只用于用户可读展示、群聊提及和必要的查找；内部身份、去重、allowlist 和配对都使用 UUID。

### 5.2 Contacts

`contacts.Contact` 增加：

```text
MixinUserID
MixinIdentityNumber
MixinChatIDs
```

收到消息后：

- `ContactID = mixin:<user_uuid>`。
- `ContactNickname` 取 `full_name`，为空时用 `@<identity_number>`，最后才回退到 UUID。
- 用户资料中 `app_id` 非空时标记为 `KindAgent`；不能仅凭昵称或 `identity_number` 推断。
- 当前 conversation 追加到 `MixinChatIDs`，去重保存。

Mixin 私聊 conversation ID 可以由双方 user UUID 确定，因此给已知 Contact 主动发私聊不要求预先保存 private chat ID。群聊发送必须有明确的 `chat_id`。

### 5.3 Agent 配对和 allowlist

Mixin Agent 使用 `mixin:<user_uuid>` 参与现有配对协议。`/pair` 的目标必须是 Contacts 中已有的 Agent reference，不按昵称猜测，也不在普通消息路径调用受限的用户搜索 API。

规则与 Telegram、Slack 一致：

- admin 发出的配对命令在普通 allowlist 之前处理。
- 已配对 Agent 的私聊可以绕过 conversation allowlist。
- 未配对 Agent 从 allowlist 外发来的私聊静默丢弃并确认，不发送“无权限”回复，避免两个 Bot 互相回复形成循环。
- 配对控制消息进入现有 journal；普通未授权私聊不进入 journal。

全局 `admins` 接受 canonical Mixin user reference：

```yaml
admins:
  - mixin:773e5e77-4107-45c2-b648-8fc722ed77f5
```

## 6. 收消息流程

```text
Blaze WebSocket
  -> decode gzip CREATE_MESSAGE
  -> validate UUID/category/base64
  -> ignore own outbound echo and wallet/system events
  -> load cached sender profile and conversation profile
  -> allowlist / pair control
  -> normalize to BusMessage
  -> check persistent inbox dedupe by (mixin, message_id)
  -> publish to in-process bus
  -> persist inbox seen record
  -> send READ acknowledgement
  -> per-conversation worker
       -> private: command or main run
       -> group: command or group trigger
            -> accepted: main run
            -> rejected: optional untriggered journal
  -> outbound bus
  -> POST /messages
```

### 6.1 ACK 和去重

Mixin 是至少一次投递。沿用现有 inbound adapter 时，正确顺序是：

1. 验证消息，并查询 `contacts/bus_inbox.json` 中的 `(channel, platform_message_id)`。
2. 未处理的消息发布到进程内 bus。
3. 发布成功后写入 seen record。
4. seen record 写入成功，或消息原本已经处理后，发送 `READ` acknowledgement。

不能等 LLM task 完成后才 ACK。任务可能持续很久，期间连接重建会反复收到同一消息。也不能在 seen record 写入前 ACK，否则进程崩溃时会永久丢消息。

这套顺序保证正常重投不会创建第二个 task，但不承诺 exactly-once：如果进程在 bus publish 成功后、seen record 写入前退出，平台重投后可能再次处理。要消除这个很小的窗口，需要把 durable queue、去重记录和消费确认做成一个事务，现有 Morph 架构并不具备该条件。第一版保留 at-least-once，不为 Mixin 单独增加一套消息队列。

Bus inbox 的记录保留与清理由独立方案定义，见 [Bus Inbox Seen Record Retention](./feat_20260827_bus_inbox_retention.md)。它不是 Mixin Channel 的实现前提。

无效或不支持的 category 记录一次结构化日志后 ACK，避免 poison message 无限重放。临时下载、存储或 bus 错误不 ACK，让平台稍后重投。

### 6.2 WebSocket 生命周期

- 每次连接都生成新的 JWT，并立即发送 `LIST_PENDING_MESSAGES`。
- 普通断线使用带 jitter 的指数退避，1 秒起步，最大 30 秒；这些值不开放配置。
- HTTP/WebSocket 401 视为凭据错误，停止 runtime，不做无限重试。
- read loop 只负责解码和把消息放入有界队列，不执行 LLM。
- 同一 conversation 的入站预处理保持顺序；不同 conversation 可以并发。
- context cancel 必须关闭 socket、停止 reconnect 并等待 worker 退出。

### 6.3 Profile 获取不能进入 API 热路径

启动时只同步调用一次 `GET /me`，得到 Bot 的 user UUID、Mixin ID、昵称和头像。结果保存在内存，并用于 endpoint avatar、mention 判断和 runtime health。

发送者资料和 conversation 资料按 UUID 首次读取后缓存在内存：

- 第一条未知发送者消息允许做一次 `GET /users/:id`；失败时用 UUID 继续处理，不丢消息。
- conversation 的 `GROUP` / `CONTACT` 和名称首次读取一次；现有 chat profile 契约不保存群头像。
- `SYSTEM_CONVERSATION` 事件使对应 conversation cache 失效。
- chat profile 名称持久化沿用 `contacts/chat_profile.json`，不在 API 读取路径刷新。
- `/api/endpoints`、`/todo/tasks`、`/contacts/chat-profile` 和 health check 不同步调用 Mixin API。

这样可避免已有 Channel 曾出现的“展示头像或列出任务时等待远端 profile API”问题。

## 7. 群聊消息

官方客户端只向 Bot 提供明确提及它的可读群消息。Morph 不再重复执行 trigger 判断，也不提供 `strict`、`smart`、`talkative`、addressing 阈值或 `record_untriggered`。这些选项无法扩大 Mixin 实际投递给 Bot 的消息范围。

mention 匹配必须有 token 边界，不能让 Bot ID `7000` 误匹配 `@70001`。进入 main run 前只移除指向当前 Bot 的 mention，不删除对其他人的引用。

### 7.1 群命令

私聊中可直接发送 `/help`、`/models`、`/skills`、`/ctx`、`/workspace`、`/think`、`/reset`、`/stop` 和 `/id`。

Mixin 没有 Telegram 的 `/command@bot_username` 语法。为了避免一个群里的多个 Bot 同时响应，群命令必须带 Bot mention，例如：

```text
@7000123456 /id
@7000123456 /models
```

命令解析先移除当前 Bot mention，再交给共享 `chatcommands`。bare `/id` 不会由 Mixin 投递给 Bot。

## 8. 消息和附件映射

### 8.1 文本

- `ENCRYPTED_TEXT` / `PLAIN_TEXT`：encrypted payload 先解密；trim 后为空则忽略并 ACK，否则作为普通文本。
- `ENCRYPTED_POST` / `PLAIN_POST`：把解密和解码后的 UTF-8 payload 作为用户文本，不在 Morph 内渲染或转换格式。
- 普通 Agent 回复使用 `ENCRYPTED_TEXT`。不在 encrypted 发送失败时降级为 plain。
- 单条回复最多使用 64 KiB UTF-8 文本，超过时按段落和 rune 安全分片。只有第一片引用 inbound message。
- 每个分片使用由 bus idempotency key 和分片序号稳定生成的 UUID，超时重试时复用同一个 message ID。

不把普通回复自动转换为 `PLAIN_POST`。POST 是独立的长文卡片，不是普通聊天气泡的格式化版本。

### 8.2 入站附件

| category | 第一版处理 |
| --- | --- |
| `ENCRYPTED_IMAGE` / `PLAIN_IMAGE` | 下载原图，校验大小和 MIME，进入现有 multimodal image input |
| `ENCRYPTED_DATA` / `PLAIN_DATA` | 下载到安全缓存，向 Agent 提供文件名、MIME 和本地路径说明 |
| `ENCRYPTED_AUDIO` / `PLAIN_AUDIO` | 下载到安全缓存，作为音频文件提供；不新增语音转写服务 |
| `ENCRYPTED_VIDEO` / `PLAIN_VIDEO` | 只记录类型和基本 metadata，不下载，不触发仅视频消息 |
| `ENCRYPTED_STICKER` / `PLAIN_STICKER` | 不触发，也不保存 sticker 内容 |
| contact/location/live/card | 不触发 main run，只做受限日志并 ACK |
| transfer/snapshot/wallet system | 永不进入 Agent prompt，ACK 后丢弃 |

附件下载沿用现有固定大小限制和安全缓存规则：

- 目录为 `file_cache_dir/mixin/`。
- 本地文件名由 Morph 生成，不能直接使用消息里的路径。
- 同时检查 payload size、HTTP `Content-Length` 和实际读取字节数。
- `view_url` 只用于当前下载，不持久化。
- 下载失败不 ACK，让平台重投；明确 404/已过期则记录后 ACK，避免无限重试。

### 8.3 出站附件工具

Mixin runtime 注册：

```text
mixin_send_file
mixin_send_photo
mixin_send_audio
```

它们与 Telegram/Lark 对应工具采用相同的路径边界和大小限制，但调用 Mixin attachment API。没有 Mixin runtime context 时不注册。第一版不增加 card、location、sticker、video 或 payment tool。

## 9. 审批

审批信息结构与 Telegram/Web UI 保持一致：先发一条完整文本，包含审批对象、原因、完整工具参数和可复制的命令：

```text
/approve <approval_id>
/deny <approval_id>
```

群聊命令带 Bot mention：

```text
@<bot_identity_number> /approve <approval_id>
@<bot_identity_number> /deny <approval_id>
```

如果真实 Bot smoke test 确认 `APP_BUTTON_GROUP` 可发送，再在同一条审批后增加按钮：

```text
Approve -> input:@<bot_identity_number> /approve <approval_id>
Deny    -> input:@<bot_identity_number> /deny <approval_id>
```

点击按钮后，Mixin 客户端会以点击者身份发送对应文本。按钮只是命令的快捷入口；Runtime 无论有没有按钮都复用相同的 command、审批状态和审计逻辑，并验证真实 sender user UUID。

规则：

- 只接受原 conversation 中、当前有权审批的用户。
- approval ID 必须处于 pending 状态且未过期。
- 重复点击返回已结束状态，不重复恢复任务。
- 群按钮的命令带 Bot mention，避免其他 Bot 响应。
- Mixin 不支持编辑原按钮消息。审批结束后发送结果文本；如果发过按钮，旧按钮继续可见，但 token 已不可复用。

不增加 WebView callback server。文本命令已经满足审批；`input:` 只减少输入步骤。

## 10. 代码边界

新增代码集中在：

```text
cmd/mistermorph/mixincmd/
internal/channelruntime/mixin/
internal/bus/adapters/mixin/
internal/mixinapi/
tools/mixin/
```

`internal/mixinapi` 只实现消息所需协议：

- keystore 解析和 EdDSA JWT。
- `GET /me`、`GET /users/:id`、`GET /conversations/:id`。
- `POST /conversations`、`POST /messages`。
- attachment create/read/upload/download。
- Blaze connect、gzip frame、pending list 和 acknowledgement。

第一版不引入完整的 Mixin Go SDK。Morph 已经依赖 `gorilla/websocket`，标准库提供 Ed25519；只需要直接使用已有依赖树中的 `golang-jwt/jwt/v5`。完整 SDK 同时覆盖 wallet、Kernel、Safe、PIN 和资产数据，会带来本功能不需要的依赖与工具链约束。

这个决定不意味着复制整个 Mixin SDK。`internal/mixinapi` 不暴露通用 Mixin client，不实现任意 endpoint，也不设计 plugin 接口。每个方法必须对应本文件中的一个实际调用点。

现有共享组件继续负责：

- `internal/channelruntime/core`：bootstrap、conversation runner、untriggered recorder、审批生命周期。
- `internal/grouptrigger`：三种群聊 trigger mode。
- `internal/bus`：inbound/outbound、会话顺序和消息校验。
- `contacts`：联系人、inbox/outbox 去重和发送路由。
- `internal/chathistory`、`internal/contextcheckpoint`：prompt history。
- `internal/channelruntime/taskruntime`：Agent run、tools、tasks 和 runtime API。

不要从 Telegram package 复制一份完整 runtime 再改字段。只复制平台协议无法共享的部分；共享行为接到现有 core。

## 11. CLI、Console 和 Integration

### 11.1 CLI

新增：

```bash
mistermorph mixin
```

其日志 mode、request dump 名称和 task persistence target 使用 `mixin`。启动时：

1. 校验 keystore 文件权限和三个必需字段。
2. 调用一次 `GET /me` 验证凭据并缓存 Bot profile。
3. 初始化 bus、Contacts、journal、task runtime 和 runtime API。
4. 连接 Blaze，进入 pending/reconnect loop。

### 11.2 Console managed runtime

`console.managed_runtimes` 增加 `mixin`。Console Settings 读写：

- keystore file path。
- allowed conversation IDs。
- group trigger mode。

Console API 永不返回 keystore 内容。修改配置后沿用现有 managed runtime restart 行为。

`/api/endpoints` 使用启动时缓存的 Bot 名称和头像。Blaze 是否在线只影响运行状态，不触发同步 profile fetch。

### 11.3 Integration API

公开 Integration Runtime 增加：

```go
Runtime.NewMixinBot(MixinOptions)
```

它与 `NewTelegramBot`、`NewSlackBot` 一样返回 `BotRunner`。`MixinOptions` 只接收解析后的消息凭据、allowlist、任务超时和并发数；不接收钱包或群 trigger 字段。

## 12. 错误处理和可观测性

主要日志：

```text
mixin_runtime_start
mixin_profile_loaded
mixin_blaze_connected
mixin_blaze_disconnected
mixin_blaze_reconnect_scheduled
mixin_message_received
mixin_message_deduped
mixin_message_acked
mixin_message_unsupported
mixin_profile_fetch_failed
mixin_attachment_download_failed
mixin_message_send_failed
mixin_runtime_stop
```

规则：

- 正常每条消息不打印完整正文；debug 模式才允许受长度限制的 text preview。
- 不记录 `data_base64`、JWT、private key、upload URL、view URL 或完整 attachment payload。
- 401 和 keystore 解析错误使 runtime 失败，错误信息不能包含私钥。
- 429 尊重 `Retry-After`；没有该 header 时使用有上限的退避。
- 单个 user/conversation profile 失败不阻止其他会话。
- 发送超时属于结果不确定，必须使用原 message UUID 重试，不能生成新 UUID。

Runtime health 至少区分：

- configured：keystore 已解析。
- running：runtime goroutine 存活。
- connected：Blaze 当前连接可用。

health check 只读内存状态，不请求 Mixin API。

## 13. 实现顺序和测试

正式代码按阶段先写测试，再实现。

### Phase 1：协议客户端

- [x] keystore 只提取消息凭据，忽略钱包字段。
- [x] JWT header、claims、request digest 和 Ed25519 签名符合官方样例。
- [x] Blaze gzip encode/decode、`LIST_PENDING_MESSAGES`、`CREATE_MESSAGE` 和 ACK。
- [x] 401 停止；普通断线退避重连；cancel 能立即退出。
- [x] REST error、429、body limit 和 secret redaction。
- [x] encrypted message session 获取、内存缓存、session 失效重试和入站解密。

测试使用本地 HTTP/WebSocket server，不连接真实 Mixin，不运行网上下载的二进制。

### Phase 2：Channel 基础行为

- [x] 注册 `mixin` channel、conversation key、bus inbound/delivery adapter。
- [x] 私聊文本进入 main run。
- [x] 群聊 @Bot 消息直接进入 main run。
- [x] allowlist、per-conversation ordering、cross-conversation concurrency。
- [x] bus inbox 去重成功后才 ACK；重投不产生第二个 task。
- [x] commands、`/stop`、history、context checkpoint、sticky skills。

### Phase 3：附件、审批和 Contacts

- [x] 图片、文件、音频输入的安全下载和大小限制。
- [x] `mixin_send_file`、`mixin_send_photo`、`mixin_send_audio`。
- [x] 审批文本命令、sender 校验、重复和过期处理。
- [ ] 真实 Bot 验证 `APP_BUTTON_GROUP` 后再启用审批 buttons；不支持时保持纯文本命令。
- [x] Contacts profile、主动私聊、群 chat hint 和 chat profile。
- [x] Agent 识别、配对、paired allowlist bypass 和无回声拒绝。
- [x] Cron Notify Chat 通过现有 `contacts_send` 发送。

### Phase 4：入口和文档

- [x] `mistermorph mixin`。
- [x] Console Settings 和 `managed_runtimes: [mixin]`。
- [x] endpoint avatar、runtime health 和 remote console 管理。
- [x] `Runtime.NewMixinBot`。
- [x] 更新 `assets/config/config.example.yaml`、CLI help、Channel 文档和 VitePress 导航。
- [x] 运行 `go test ./...` 和 `go vet ./...`。
- [x] 用真实测试 Bot确认普通群消息和仅 reply 不会投递，群 mention 可以进入 main run。
- [ ] 用真实测试 Bot 各做一次私聊、附件、审批和断线重连 smoke test。

## 14. 验收条件

1. 用户添加 Bot 后发送私聊文本，Morph 创建一个 task，并在同一 conversation 回复。
2. Bot 加入群后，只处理明确提及 Bot 的可读消息；仅 reply 不触发。
3. bare 群命令不会投递给 Bot；带当前 Bot mention 的命令正常执行。
4. allowlist 为空时允许所有 conversation；非空时只允许列出的 UUID，已配对 Agent 私聊除外。
5. 同一 conversation 顺序执行，不同 conversation 的并发不超过 `max_concurrency`。
6. 正常运行时，Blaze 重投同一 message ID 不创建重复 task；seen record 写入后才 ACK。进程退出窗口保持明确的 at-least-once 语义。
7. 断线自动恢复并请求 pending messages；401 不无限重试。
8. 图片、文件、音频遵守路径和大小限制，临时 URL 不落盘。
9. 审批显示完整对象、原因、工具参数和文本命令；按钮可用时绑定真实 sender，重复执行命令不重复恢复任务。
10. Contacts、`contacts_send`、`agent_send`、Cron Notify Chat 和 chat profile 可以使用 Mixin identity。
11. Console 可以启动、停止和远程管理 Mixin runtime；settings、endpoint avatar 和 health 不因远端 profile 请求变慢。
12. 代码没有 Mixin 钱包、PIN、资产、支付或交易入口。

## 15. 非目标

- Mixin 钱包、转账、支付、资产查询、TIP PIN 或 Safe。
- Bot WebView、OAuth、首页入口或 JS Bridge。
- 创建、管理或邀请群成员。
- sticker、location、live、video、contact card 和 app card tools。
- 用临时消息模拟 typing、reaction 或编辑中的回答。
- 新的通用 Channel framework 或重写现有 Telegram runtime。
- 将 Mixin API base、reconnect、cache 和 ack 细节全部变成配置项。

## 16. 调研来源

调研日期为 2026-08-27。实现前如官方协议有更新，应重新核对对应页面。

- [Mixin Applications](https://developers.mixin.one/docs/app/mixin-applications)：Application 与 Messenger Bot 的关系。
- [Create an App](https://developers.mixin.one/docs/app/getting-started/create-app)：Developer Dashboard 和 Ed25519 keystore 字段。
- [API Guide](https://developers.mixin.one/docs/api/guide)：HTTP/Blaze 地址、JWT 和 EdDSA 签名。
- [Generate Authentication Token](https://developers.mixin.one/docs/app/guide/generate-jwt-token)：Bot JWT 的 request digest 和 claims。
- [Handle Message Loop](https://developers.mixin.one/docs/app/guide/message-loop)：Blaze、pending messages、gzip frame 和 ACK。
- [Send and Receive Messages](https://developers.mixin.one/docs/app/getting-started/messages)：七天 pending retention、重连和发送前的会话要求。
- [Send Messages](https://developers.mixin.one/docs/api/messages/send)：`POST /messages`、批量上限、body 上限和 quote。
- [Message Category](https://developers.mixin.one/docs/api/messages/category)：文本、图片、音频、文件、POST、按钮和系统消息结构。
- [Upload Attachments](https://developers.mixin.one/docs/api/messages/attachment-upload) 与 [Download Attachment](https://developers.mixin.one/docs/api/messages/attachment-download)：附件上传和下载流程。
- [Create Conversations](https://developers.mixin.one/docs/api/conversations/create)：`CONTACT` / `GROUP`、参与者上限和私聊 conversation ID 算法。
- [Read Conversations](https://developers.mixin.one/docs/api/conversations/read)：群资料和参与者结构。
- [Manage Groups](https://developers.mixin.one/docs/api/conversations/group)：群成员、管理员和系统变化。
- [Read User](https://developers.mixin.one/docs/api/users/user) 与 [User Introduction](https://developers.mixin.one/docs/api/users/intro)：user profile、Mixin ID 以及普通用户和 Bot 的关系。
- [User Interaction](https://developers.mixin.one/docs/app/design/user-interaction)：Bot 私聊、群成员和 `input:` button 行为。
- [Official Go Bot SDK](https://github.com/MixinNetwork/bot-api-go-client) 与 [Blaze implementation](https://github.com/MixinNetwork/bot-api-go-client/blob/master/blaze.go)：协议常量、message view 和 SDK 能力边界。
- [Official encrypted message implementation](https://github.com/MixinNetwork/bot-api-go-client/commit/147cae2bd41c89f6cf048b272b50813a24ac0833) 与 [Mixin Safe migration](https://github.com/MixinNetwork/safe/commit/fd2e823758186588a103ff15e4615bb8b3f31646)：encrypted endpoint、recipient sessions、session cache 和迁移实例。
- [Official Flutter client sending job](https://github.com/MixinNetwork/flutter-app/blob/main/lib/workers/job/sending_job.dart) 与 [Signal protocol](https://github.com/MixinNetwork/flutter-app/blob/main/lib/crypto/signal/signal_protocol.dart)：从正文生成 `mentions`，并将 `mentions` 和 `quote_message_id` 作为独立字段发送。

## 17. 实施进度

- [x] 完成官方协议调研和 Telegram 行为对齐设计。
- [x] Phase 1：Mixin keystore、JWT、REST、Blaze 和 attachment client。
- [x] Phase 2：Channel 注册、Bus adapters、私聊、群聊 trigger、commands 和 task runtime。
- [x] Phase 3：附件、审批、Contacts、主动发送和 Agent 配对。
- [x] Phase 4：CLI、managed runtime、Console Settings、Integration API 和用户文档。
- [x] encrypted message 出站、入站解密和 `PLAIN_*` 入站兼容。
- [x] 完成定向测试、`go test ./...` 和 `go vet ./...`。
- [ ] 完成真实 Mixin Bot 的私聊、群聊、附件、审批和断线重连 smoke test。
