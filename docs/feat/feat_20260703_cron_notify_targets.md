---
date: 2026-07-03
title: Cron Notify Target Chat Context
status: draft
---

# Cron Notify Target Chat Context

## 背景

cron 和 heartbeat 都通过 awareness 执行。真正发送消息时，仍由 LLM 调用 `contacts_send`，再进入 contacts service、outbox 和各平台 sender。

`cron.yaml` 的 task 已经支持可选 `chat_id`。当前实现只把它放进 awareness metadata，没有明确提示 LLM 在调用 `contacts_send` 时传入这个值。

v1 只解决一个问题：cron task 有一个目标 chat 时，把这个 chat 作为结构化上下文交给 LLM，并让 LLM 在需要通知时传给 `contacts_send`。

## 目标

- 保留现有发送链路：cron awareness 不直接发消息，仍由 LLM 调用 `contacts_send`。
- `todo_update` 继续允许写入可选 `chat_id`；为空时保持现有行为。
- cron task 有 `chat_id` 时，metadata 中提供单个 `notify_target`。
- Web Todo UI 提供 chat 下拉选择，保存时写入 `cron.yaml.tasks[].chat_id`。
- 为 `chat_id` 展示信息提供 `contacts/chat_profile.json` 缓存。
- 启动时刷新已过期的 chat profile；cron 运行时按 lazy fetch 获取缺失或过期信息。
- `contacts_send` 支持把合法 chat id 作为 chat 级发送目标。

## 非目标

- 不支持一个 cron task 多个 chat target。
- 不重做 `contacts_send` 的多人 batch 路径。
- 不让 `chat_profile.json` 绕过平台 token、allowed chat、sender 权限或平台 API 限制。
- 不改变 heartbeat 的发送语义。
- 不保存平台原始响应。

## Metadata 结构

cron awareness metadata 增加单个 `notify_target`：

```json
{
  "trigger": "cron",
  "awareness": {
    "behavior": "cron",
    "source": "cron",
    "task_id": "weekly-review",
    "scheduled_at_utc": "2026-07-03T01:00:00Z",
    "schedule": "0 10 * * 1",
    "tz": "Asia/Tokyo",
    "chat_id": "tg:-1001234567890",
    "notify_target": {
      "chat_id": "tg:-1001234567890",
      "people": [
        {
          "contact_id": "tg:@alice",
          "label": "Alice",
          "ref": "[Alice](tg:@alice)"
        }
      ],
      "chat_profile": {
        "platform": "telegram",
        "type": "supergroup",
        "name": "Project Room"
      }
    }
  }
}
```

规则：

- `notify_target.chat_id` 取自 cron task 的 `chat_id` 字段。
- `notify_target.people` 来自 task 内容中的联系人引用，例如 `[Alice](tg:@alice)`。
- `notify_target.chat_profile` 只作为上下文，不能替代 `chat_id`。
- 如果有 `chat_id` 但没有明确提到人，`people` 为空，表示 chat-level 通知。

## Prompt 要求

awareness prompt 补一条 cron 规则：

```text
IF `mister_morph_meta.trigger` is `cron`
AND `mister_morph_meta.awareness.notify_target` is present
AND you decide to send a notification
THEN call `contacts_send`.

If `notify_target.people` is not empty:
- Pass mentioned people as `contacts_send.contact_id`.
- Pass `notify_target.chat_id` exactly as `contacts_send.chat_id`.

If `notify_target.people` is empty and this is a chat-level notification:
- Pass `notify_target.chat_id` as both `contacts_send.contact_id` and `contacts_send.chat_id`.

Do not invent chat ids or contact ids.
```

这条规则只影响 LLM 如何调用工具，不改变 cron runner 的发送职责。

## chat profile 缓存

新增 `contacts/chat_profile.json`，只保存展示和 prompt 上下文需要的最小字段：

```json
{
  "version": 1,
  "items": [
    {
      "chat_id": "tg:-1001234567890",
      "platform": "telegram",
      "type": "supergroup",
      "name": "Project Room",
      "fetched_at": "2026-07-03T01:00:00Z",
      "expires_at": "2026-07-10T05:30:00Z"
    }
  ]
}
```

行为：

- 启动时检查已过期 item，并尝试刷新平台信息。
- 启动时如果缓存不存在或没有 item，直接从 `contacts/ACTIVE.md` 的联系人可达会话字段中提取候选 chat id，尝试预取一次。
- 刷新成功后更新 item，并把 `expires_at` 设置到约一周以后，带小幅偏移。
- 刷新失败不阻止 runtime 启动。保留旧信息，并把 `expires_at` 推迟一个短时间，例如 6 小时，避免重启反复请求。
- cron 构造 `notify_target` 时，如果 task 有 `chat_id`，读取缓存。
- 缓存命中且未过期时直接使用。
- 缓存缺失或过期时请求平台 API，成功后写回。
- 获取失败不阻止 cron task 执行。失败时省略 `chat_profile`，保留 `chat_id`。

平台信息来源：

- Telegram：Bot API `getChat`。
- Slack：`conversations.info`。
- LINE：群使用 group summary；room 没有等价实现时保留旧缓存并短时间后重试。
- Lark：使用 Lark/Feishu IM chat API。

## Web UI 行为

Console 的 Todo 页面增加一个 chat 下拉。

规则：

- 只在普通 cron task 编辑时展示；heartbeat 系统 task 不展示。
- 下拉有空选项，表示“不指定 chat”。
- 下拉只展示 `contacts/chat_profile.json` 中已有完整信息且有 `name` 的 item。
- 不展示裸 `chat_id`。
- `/todo/tasks` 会从 `contacts/ACTIVE.md` 中提取候选 chat id，尝试补齐 `chat_profile.json` 后再返回 `chat_options`。
- 当前 task 已有 `chat_id` 但缓存里没有完整信息时，UI 保留内部值，但不把裸 id 显示成选项。用户没有改 chat 时，保存仍保留原值。
- 用户选择空选项时，保存后清空 `chat_id`。
- 用户选择某个 chat 时，保存 `/todo/tasks` 后写入 `cron.yaml.tasks[].chat_id`。

UI 不直接请求平台 API。平台信息由 `/todo/tasks` 后端、启动流程和 cron lazy fetch 写入缓存。

## contacts_send chat 级目标

cron task 有 `chat_id` 但没有 `people` 时，LLM 会把同一个 chat id 同时传给 `contacts_send.contact_id` 和 `contacts_send.chat_id`：

```json
{
  "contact_id": "tg:-1001234567890",
  "chat_id": "tg:-1001234567890",
  "message_text": "..."
}
```

v1 需要补齐这一条语义：

- 当 `contact_id` 是合法 chat id，且 `chat_id` 为空或与它相同，允许直接把它作为目标 chat 发送。
- 这条能力仍要经过现有平台 sender 和权限限制。
- 仅支持单个 chat-level target；不改多人 batch 路径。

## 验收标准

- cron task 没有 `chat_id` 时，metadata 和行为保持兼容。
- cron task 有 `chat_id` 时，metadata 中存在 `awareness.notify_target.chat_id`。
- task 内容中提到的人会出现在 `awareness.notify_target.people`。
- prompt 明确要求 LLM 把 `notify_target.chat_id` 传给 `contacts_send.chat_id`。
- `contacts/chat_profile.json` 支持读取、原子写入和启动刷新过期 item。
- chat profile fetch 失败时，cron task 仍能执行。
- Todo Web UI 能选择已知 chat，并在保存后写入 `cron.yaml.tasks[].chat_id`。
- Todo Web UI 不展示裸 `chat_id`。
- `contacts_send` 支持单个 `contact_id` 为 chat id 的 chat-level 通知。

## 实现备注

- 正式实现前先补测试，再改代码。
- `notify_target` 的构造放在 cron awareness metadata 构造附近。
- `chat_profile` 缓存放在 contacts 目录下，不混进 `ACTIVE.md` / `INACTIVE.md`。
- metadata 有 4KB 注入上限，`chat_profile` 必须保持短小。
