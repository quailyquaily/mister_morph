---
date: 2026-08-19
title: 移除 Memory 子系统
status: implemented
---

# 移除 Memory 子系统

## 1) 需求

MisterMorph 不再提供长期记忆和短期记忆功能。一次任务已经有输入、执行状态、结果和来源；Memory 又把其中一部分复制成 `memory/record`，再通过单独的 worker 投影为 Markdown。这个功能不再需要，应完整删除，而不是保留一个默认关闭的开关。

删除 Memory 后仍需在 task 事实中保留：

- 参与者；
- 会话 ID 和会话类型；
- Channel。

不再保存最近几条对话作为 task 或其他 journal 事件的附加副本。

## 2) 保留的行为

以下能力不是 Memory，必须继续工作：

- 当前会话的 chat history；
- context checkpoint 和 context compaction；
- task、topic、approval、Agent pairing 和非 trigger 消息的 journal 事件；
- runtime log 和 LLM usage stats；
- Contacts、workspace、persona 和 awareness task；
- task/topic API、分页和 observation。

删除 Memory 不能改变任务执行、Channel 回复、审批、重试、取消、并发控制或工具注册。

## 3) Task 来源上下文

Task 使用一个可选的来源上下文：

```go
type TaskConversation struct {
    ConversationID   string            `json:"conversation_id,omitempty"`
    ConversationType string            `json:"conversation_type,omitempty"`
    Participants     []TaskParticipant `json:"participants,omitempty"`
}

type TaskParticipant struct {
    ID       string `json:"id"`
    Nickname string `json:"nickname,omitempty"`
}
```

`TaskInfo` 增加可选的 `conversation` 字段。字段规则：

- Channel 继续使用 task journal 已有的 `payload.target` 和 envelope `trace.runtime/target`，不在 conversation 中复制。
- `conversation_id` 使用 runtime 已有的 conversation key。Telegram topic、Slack thread 等隔离范围必须保留。
- `conversation_type` 使用平台已有类型；平台没有类型时为空。
- `participants` 第一项是任务发送者，后面是 runtime 已从当前消息解析出的提及或引用对象。
- participant ID 转成字符串。nickname 只保存平台已经提供的数据，不额外请求平台 API。
- runtime 已持有当前 Agent 的平台 ID 时，将它从 participants 排除；不为此额外请求平台 API。执行方已经由 task target 和 endpoint 确定。
- 空字段不伪造默认值。

这里不增加 participant role、跨平台 identity、Contact 快照、组织关系或历史消息。当前需求不需要这些概念。

## 4) Journal 与 task projection

所有 runtime 的 task 变化都写入统一 journal。`tasks.persistence_targets` 只控制该 target 是否从 journal 恢复 task projection，以及是否把 projection 保存到 `tasks/<target>/projection.json`；它不再决定 task 事实是否写入 journal。

未配置持久化的 target：

- 当前进程仍保留现有的有界 task view；
- task upsert/update 写入统一 journal；
- 重启时不从 journal 恢复旧 task；
- 不写 task projection snapshot。

已配置持久化的 target 保持现有恢复和 snapshot 行为。

Task journal payload 继续使用现有 `TaskInfo` 快照。`conversation` 随 task 一起写入。第一版不把 task event 改成 delta，也不新增 `task_completed` 事件。

## 5) 删除范围

### 5.1 Go 后端

删除：

- `memory/`；
- `internal/memoryruntime/`；
- Memory orchestrator、projector、projection worker 和 draft resolver；
- task runtime 的 Memory hooks；
- Telegram、Slack、LINE、Lark、Console 和 awareness 的 Memory 组装与写入；
- Memory prompt injection；
- `/memory/files` API 及其文件解析代码；
- chat CLI 的 Memory 初始化和注入；
- 仅服务于 Memory 的配置、runtime option、测试和资源文件。

### 5.2 Console 前端

删除：

- Memory 路由；
- Memory 导航项；
- Memory 页面及样式；
- 只由 Memory 页面使用的文案。

`chat-draft-memory.js` 和 `chat-topic-memory.js` 是浏览器内的草稿与选中状态缓存，不属于长期 Memory。它们应保留；是否改名不属于本需求。

### 5.3 配置和文档

删除：

```yaml
memory:
  enabled: true
  dir_name: "memory"
  short_term_days: 7
  injection:
    enabled: true
    max_items: 50
```

同时删除配置默认值、配置说明、Memory 使用指南和安装时复制的 Memory 模板。

历史 feature 文档作为设计记录保留，但需要在本文件中明确它们已经失效，不逐份重写历史内容。

## 6) 已有数据

升级时不删除用户数据：

- 已有 `memory/` 目录保持原样；
- journal 中已有的 `domain=memory` 事件保持原样；
- 新版本不再读取、写入、迁移或展示这些数据。

统一 journal reader 已经允许领域消费者跳过其他 domain，因此不需要为旧 Memory 事件保留解析器或兼容包。

旧配置中的 `memory.*` 不应导致启动失败。Viper 会忽略不再读取的键；不增加 deprecated warning、迁移状态或兼容开关。

## 7) 明确删除后的语义

- 不存在 `memory.enabled`。
- 所有成功任务都不再产生 `domain=memory,type=record`。
- 不再调用 LLM 生成 Memory draft。
- 不再自动创建或更新 `memory/index.md`、日期目录或 projection checkpoint。
- prompt 不再注入长期或短期记忆。
- Console 不再提供 Memory 文件编辑入口。
- task journal 仍记录 task 来源，但不记录附加历史。

## 8) 实现顺序

每个正式代码阶段先更新测试，再实现。

1. 为 task conversation schema、规范化、journal round-trip 和非持久化 target 的 journal 写入增加测试。
2. 实现 task conversation，并把 task journal 写入与 projection persistence 分开。
3. 接入 Console、Telegram、Slack、LINE 和 Lark 的 task 创建路径。
4. 删除各 runtime 的 Memory hooks 和 Memory runtime。
5. 删除 Memory API、Console 页面、配置、资源和当前文档入口。
6. 删除只验证已移除功能的测试，修正仍保护 task、Channel 和 runtime 边界的测试。
7. 运行 Go 测试、Go vet、Console 测试和生产构建，并搜索残留符号。

## 9) 需求审阅

按第一性原理检查后，结论如下：

1. Memory 不是需要保留的兼容边界。目标是删除功能，因此不能留下关闭状态的 runtime、空接口、deprecated 配置或前端隐藏路由。
2. `memory/record` 不是独立事实。它由一次已完成 task 自动派生；删除 Memory 后没有继续保存这种事件的理由。
3. 不应把 `memory/record` 改名成 `interaction` 或 `conversation_turn`。当前没有第二个消费者需要这种事件，改名只会保留原有重复。
4. 参与者和会话范围属于 task 来源，应放进现有 task 快照。最近历史既不是 task 状态，也没有保留需求，应删除。
5. Channel 已由 task target 表达。conversation 再保存 channel 会形成第二份相同状态，因此删除该字段。
6. task journal 写入必须和 task projection persistence 分开。否则默认只持久化 Console 时，删除 Memory 会让其他 Channel 不再留下任何已接受 task 事实。
7. 不需要迁移旧 Memory 数据。保留文件、忽略旧事件即可；自动删除或转换都会增加风险和一次性代码。
8. 不保留未来扩展点。若以后重新引入某种记忆能力，应从当时的具体需求重新设计。

审阅没有发现需要保留 Memory 子系统的现行契约。唯一明确的对外破坏是配置、HTTP API 和 Console 页面消失，这正是本需求要求的结果。

## 10) 验收条件

1. 仓库不再包含可执行的长期或短期 Memory 代码。
2. 配置示例、设置 API 和 Console 中不再出现 Memory 功能。
3. 所有 runtime 的新 task journal 事件包含可获得的 conversation 和 participants。
4. task context 不包含最近历史，也不触发额外平台请求。
5. `tasks.persistence_targets` 未包含某个 runtime 时，该 runtime 的 task 仍写 journal，但重启后不恢复旧 task projection。
6. 旧 Memory 文件和旧 journal 事件不会被删除，且不会影响新版本启动和 task replay。
7. chat history、context checkpoint/compaction、task/topic、approval、Agent pairing、非 trigger journal、logs 和 stats 继续工作。
8. 全仓测试、静态检查和 Console 构建通过。

## 11) 非目标

- 把 chat history 迁入统一 journal；
- 从 task journal 恢复 prompt history；
- 增加新的 retention、privacy 或 capture 开关；
- 迁移、导出或删除旧 Memory 数据；
- 改造 task event 为 delta；
- 重命名浏览器内的 draft/topic 缓存文件；
- 为未来重新加入 Memory 保留接口。
