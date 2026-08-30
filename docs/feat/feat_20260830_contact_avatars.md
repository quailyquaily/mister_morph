---
date: 2026-08-30
title: Contact 头像
status: proposed
---

# Contact 头像

## 1. 结论

Morph 可以从多数 Channel 获取联系人头像。头像是平台资料的派生缓存，不是 Contact 的身份数据。

第一版采用以下规则：

- Contact YAML 不保存头像 URL、文件路径或 Base64。
- 头像下载和刷新不能阻塞消息处理、runtime 启动或 Contacts API。
- Morph 把头像文件缓存在 `contacts/avatars/`，Console 只读取 Morph 自己的头像接口。
- 本地和远程 endpoint 使用同一 Runtime API；远程图片继续经过现有 Console proxy。
- 没有头像或获取失败时显示稳定的首字母占位，不显示破损图片。
- 不增加用户配置、头像数据库、消息总线字段或通用媒体服务。

## 2. 当前实现

当前 Contact 没有头像能力：

1. `contacts.Contact` 没有头像字段。
2. `contacts/ACTIVE.md` 和 `contacts/INACTIVE.md` 不保存头像。
3. Bus 的 `MessageExtensions` 只携带发送者 ID、用户名和显示名，没有头像。
4. `/contacts/list` 不返回头像状态或头像地址。
5. Contacts UI 只显示名字、Channel、ID 和资料，没有头像元素。

部分 Channel 已经做了获取资料所需的请求：

- Slack ingress 调用 `users.info`，但当前只解析用户名和显示名。
- Mixin ingress 调用 `ReadUser`，`mixinapi.User` 已经包含 `AvatarURL`，但没有使用。
- Telegram 已有 `getFile` 和文件下载能力，但没有调用 `getUserProfilePhotos`。

Contact 头像与以下图片不是同一数据：

- Persona 的 `avatar.webp`；
- endpoint health 返回的 Agent 头像；
- 群聊的 chat profile；
- 聊天消息中的图片附件。

本需求只处理人类和 Agent Contact 的个人头像。

## 3. Channel 能力

| Channel | 可用身份 | 头像来源 | 主要限制 |
| --- | --- | --- | --- |
| Telegram | numeric user ID | `getUserProfilePhotos`，再用 `getFile` 下载 | 消息本身不带头像；下载 URL 包含 Bot Token，且会过期 |
| Slack | team ID + user ID | 现有 `users.info` 响应中的 `profile.image_*` | Slack Connect 用户可能不可见；需要 `users:read` |
| Mixin | user UUID | 现有 `ReadUser` 响应中的 `avatar_url` | 用户查询有限流；应复用 ingress 已取得的 User |
| LINE | user ID | Messaging API 的 profile 或 group member profile | 群成员和好友资料的可见范围不同，接口可能返回无权限 |
| Lark | open ID | Contact user profile 中的 avatar 字段 | 依赖应用权限和用户可见范围 |
| Console | `console:user` | 无平台头像来源 | 使用占位图 |

某个平台没有权限、用户没有头像或用户对 Bot 不可见，都属于正常结果。不能因此拒绝消息或创建 Contact 失败。

## 4. 数据和文件

### 4.1 Contact 结构保持不变

不在 `contacts.Contact` 和 Contact YAML 中增加 `avatar_url`。

原因：

- Telegram 的下载地址包含 Bot Token，不能持久化或返回给浏览器。
- Telegram 文件地址至少一小时有效，不适合作为 Contact 资料。
- Slack、Mixin、LINE 和 Lark 的远端地址也可能变化。
- 用户手工编辑 Contact 时不应该维护派生文件状态。
- Base64 会明显增大 YAML 和 `/contacts/list` 响应。

### 4.2 本地缓存

缓存目录固定为：

```text
file_state_dir/contacts/avatars/
```

每个文件使用规范化 `contact_id` 的 SHA-256 作为文件名：

```text
avatars/<sha256(contact_id)>.image
```

文件名不包含用户名、平台用户 ID 或本机路径。文件 mtime 作为刷新时间，不增加 index JSON 或 metadata sidecar。

缓存写入使用现有原子文件写入方式。只接受 JPEG、PNG、WebP 和 GIF，单个文件最多 5 MiB；不接受 SVG。返回图片时根据文件内容设置 `Content-Type`，并发送 `X-Content-Type-Options: nosniff`。

删除 Contact 时同时删除对应头像文件。Contact 从 active 迁移到 inactive 时保留头像。

## 5. 获取和刷新

### 5.1 不进入消息关键路径

创建或更新 Contact 后，Channel runtime 异步刷新该发送者的头像。消息发布、Contact 更新和任务触发都不等待头像请求。

Channel runtime 启动后，可以异步检查本 Channel 的已有 Contact，补齐缺失头像。这个过程：

- 不延迟 runtime ready；
- 每个 runtime 同时最多发起一个头像请求；
- 同一 `contact_id` 在队列中只保留一次；
- 只处理本 Channel 能识别的 Contact；
- 进程退出时直接取消，不要求排空队列。

这只需要一个小型有界队列，不需要 scheduler、持久化 job 或新的事件类型。

### 5.2 固定刷新规则

头像文件缺失时获取一次。文件存在且 mtime 未超过七天时直接复用；超过七天后异步刷新。

七天是内部缓存策略，不增加配置项。头像不是授权或业务状态，过期几天不会影响正确性。

结果处理：

- 成功取得新头像：原子替换缓存文件。
- 平台明确表示用户没有头像：删除旧缓存。
- 请求失败、超时、限流或无权限：保留旧缓存并写 DEBUG log。
- 没有旧缓存时失败：继续使用 UI 占位图。

不能因为列表页面打开而为所有 Contact 同步请求平台 API。Contacts 数量增长后，这会使页面速度取决于外部 API，并容易触发平台限流。

### 5.3 复用现有资料请求

优先复用 Channel 已有结果，避免重复请求：

- Slack 在 `users.info` 返回时同时读取合适尺寸的 `image_*`。
- Mixin 使用 ingress `ReadUser` 已返回的 `AvatarURL`。
- LINE 和 Lark 如果 ingress 已读取 profile，则复用同一响应。
- Telegram 的 update 不带头像，只能额外调用 `getUserProfilePhotos`。

远端头像 URL 只在进程内传给下载逻辑。不能写入 Contact YAML、log 或 Bus 消息。

## 6. Runtime API

新增只读接口：

```http
GET /contacts/avatar?contact_id=<contact_id>
```

行为：

- 已有缓存：返回图片内容。
- 没有缓存：返回 `404`，不在请求内访问平台 API。
- Contact 不存在：返回 `404`。
- 缺少或无效 `contact_id`：返回 `400`。
- 只支持 `GET`。

`/contacts/list` 中的每个 item 增加可选的 endpoint-relative `avatar_url`。只有缓存文件存在时才返回：

```json
{
  "contact_id": "tg:1234",
  "nickname": "Alice",
  "avatar_url": "/contacts/avatar?contact_id=tg%3A1234&v=1788076800"
}
```

`v` 使用文件 mtime，供浏览器刷新缓存。它不是 Contact 字段，也不写回 YAML。

生成列表时只读取一次 `contacts/avatars/` 目录并建立文件名集合，不能对每个 Contact 分别读取图片或访问平台。目录不存在等同于没有头像。

Console 前端必须使用 endpoint-aware URL helper 包装相对地址：

- local endpoint 读取 local Runtime API；
- remote endpoint 经过 `/api/proxy` 读取对应 remote Runtime API；
- 切换 endpoint 后不能继续使用前一个 endpoint 的头像 URL。

## 7. UI

Contacts 列表和详情页显示圆形头像：

- 列表头像使用固定尺寸，避免图片加载后改变 item 高度。
- 详情页可以使用更大尺寸，但读取同一个 `avatar_url`。
- 图片为空、404 或加载失败时，显示联系人名字的首字符。
- Agent 和 Human 使用相同头像规则；kind 继续由现有文字和图标表达。
- 图片使用懒加载，不预读列表以外的 Contact。

头像只是辅助识别信息。名字、Channel 和 contact ID 仍是可访问文本，不能把名字替换为只有图片的按钮。

## 8. 性能和安全边界

### 8.1 性能

- `/contacts/list` 不等待平台 API。
- `/contacts/avatar` 只读本地小文件。
- 浏览器不接收 Base64 图片。
- 平台头像下载失败不能拖慢 ingress。
- 同一 Contact 的并发刷新必须合并，不能产生请求风暴。
- 缓存不在启动时全部读入内存；Contact 数量可能长期增长，操作系统文件缓存已经足够。

### 8.2 安全

- Telegram Bot Token 不出现在文件、JSON、HTML、URL、journal 或 log 中。
- 不接受 Contact YAML 提供的任意头像 URL，避免把 Contacts 页面变成通用 URL fetcher。
- 只下载已认证平台 API 返回的 HTTPS 头像地址。
- 下载遵守固定超时、响应大小限制和受限重定向。
- 图片接口不能接受任意文件路径，只能由 `contact_id` 推导缓存文件名。
- 不渲染 SVG。

## 9. 错误和日志

成功刷新不需要 INFO log。以下情况使用 DEBUG：

- 平台没有头像；
- 用户资料不可见；
- 使用旧缓存；
- 头像请求超时或限流。

无法创建缓存目录、原子写入失败或读取到不支持的图片格式使用 WARN。日志包含 Channel、规范化 `contact_id`、耗时和错误，不记录平台头像 URL、认证信息或图片内容。

头像事件不写 journal。它们不是用户消息、任务状态或 Contact 关系变化。

## 10. 测试

正式实现前增加测试：

| 范围 | Case |
| --- | --- |
| cache | `contact_id` 稳定映射到文件名，不允许路径穿越 |
| cache | 原子替换头像，失败时保留旧文件 |
| cache | 拒绝 SVG、非图片和超过大小限制的响应 |
| refresh | 缺失时获取，七天内不刷新，过期后刷新 |
| refresh | 平台无头像时删除旧缓存，请求失败时保留旧缓存 |
| refresh | 同一 Contact 的重复请求被合并 |
| Telegram | 取得 profile photo 后通过已有文件下载能力保存，Token 不出现在输出中 |
| Slack | 从现有 `users.info` 选择合适的 `image_*` |
| Mixin | 复用 `ReadUser.AvatarURL`，不重复查询用户 |
| Runtime API | list 只为已有缓存返回 `avatar_url`，avatar endpoint 正确处理 200/400/404 |
| remote | endpoint-relative avatar URL 读取正确的 remote endpoint |

UI 视觉和图片 fallback 不新增脆弱的像素级测试，使用前端构建和人工检查验证。

## 11. 验收条件

1. 收到支持平台的联系人消息后，头像可以异步出现在 Contacts 列表和详情页。
2. 平台 API 慢或失败时，消息处理和 `/contacts/list` 耗时不受影响。
3. Telegram Bot Token 不会通过头像功能暴露。
4. 本地和远程 endpoint 的联系人头像行为一致。
5. Contact YAML schema 保持不变，人工昵称不会被头像刷新覆盖。
6. 没有头像时 UI 有稳定占位，不出现布局跳动或破损图片。
7. 不增加配置项、数据库、定时任务或 Base64 列表字段。

## 12. 非目标

- 编辑、上传或裁剪 Contact 头像。
- 把 Contact 头像同步回 Channel。
- 保存历史头像。
- 群聊头像和群聊 chat profile。
- Persona 或 endpoint Agent 头像。
- 为没有平台身份的手工 Contact 猜测头像。
- 在 Agent prompt、task、journal 或 chat history 中注入头像。

## 13. 实现 Checklist

- [ ] 增加 Contact 头像文件缓存和安全校验。
- [ ] 增加异步刷新队列和固定七天刷新规则。
- [ ] 接入 Telegram 头像获取。
- [ ] 复用 Slack `users.info` 的头像字段。
- [ ] 复用 Mixin `ReadUser.AvatarURL`。
- [ ] 接入 LINE profile 头像。
- [ ] 接入 Lark profile 头像。
- [ ] 增加 `/contacts/avatar` 和 `/contacts/list.avatar_url`。
- [ ] 让 remote endpoint 的头像经过 endpoint-aware proxy。
- [ ] 在 Contacts 列表和详情页显示圆形头像及 fallback。
- [ ] 更新 Contacts 和 Channel 文档。
- [ ] 运行相关 Go tests 和 Console build。

## 14. 参考

- [Telegram Bot API: getUserProfilePhotos and getFile](https://core.telegram.org/bots/api)
- [Slack users.info](https://api.slack.com/methods/users.info)
- [Mixin Search User](https://developers.mixin.one/docs/api/users/search)
- [LINE Messaging API: Get user profile](https://developers.line.biz/en/docs/messaging-api/receiving-messages)
