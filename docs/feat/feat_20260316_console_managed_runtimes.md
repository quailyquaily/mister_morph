---
date: 2026-03-16
title: Console Managed Runtimes（Console 进程内托管多个 Runtime）
status: draft
---

# Console Managed Runtimes（Console 进程内托管多个 Runtime）

> Update 2026-03-17:
> 当前实现已经明确了持久化边界：managed runtime 不再把 task/topic 写入 Console 自己的 `ConsoleFileStore`。
> `tasks.persistence_targets` 仍按 target 生效；相关讨论以 `feat_20260315_task_persistence.md` 和 `feat_20260317_console_chat_bus.md` 为准。

## 1) 背景

当前 `mistermorph console serve` 只会内建一个 `Console Local` runtime：

- 它在同一进程内运行 agent loop。
- Console UI 通过 `/endpoints` + `/proxy` 访问 Console Local 与外部配置的 runtime。
- Telegram/Slack/LINE/Lark 如果要参与系统，通常需要各自独立启动。

这有两个现实问题：

1. 对单机使用来说，启动路径太碎。  
   用户经常想要的是“起一个 console，同时把 telegram/slack 接起来”。

2. 现在的 channel runtime 把“入站通道逻辑”和“独立 daemon runtime 进程”绑得太紧。  
   但在 `console serve` 场景里，用户真正需要的往往不是多个独立 runtime，而是“多个入站源接到同一个 console runtime”。

因此，这个 feature 的目标不是让 Console 额外托管几套并列的 daemon endpoint，而是让 Console 直接在进程内挂载多个 channel handler。

---

## 2) 目标

1. `console serve` 能通过配置同时启用多个 managed runtime。  
2. 第一阶段至少支持 `telegram` 和 `slack`。  
3. managed runtime 以 **in-process handler** 的形式接入，而不是 loopback daemon。  
4. managed runtime **不产生独立 endpoint**，而是并入 Console 自己。  
5. heartbeat 永远只由 Console 自己运行；managed runtime 不运行自己的 heartbeat。  
6. 所有入站消息最终都并入 Console 自己的 task/topic 视图，并由同一进程统一托管配置与 heartbeat。

---

## 3) 第一性原理

这件事从第一性原理看，应当先回答 3 个问题。

### 3.1 系统里到底有几个 runtime？

对用户来说，`console serve` 启动后，系统里只有一个“主 Console surface”：

- Console 自己的 task/topic store
- Console 自己的 heartbeat

Telegram/Slack 在这个模式下不是新的独立 endpoint，而是进程内托管的入站 handler。

### 3.2 UI 里是否应该出现多个 endpoint？

不应该。

`/endpoints` 的语义应该保持为：

- Console 自己
- 外部独立进程的 runtime（如果用户配置了 `console.endpoints`）

被 Console 托管的 Telegram/Slack 不属于“外部或并列 runtime”，因此不应再额外出现在 `/endpoints` 里。

### 3.3 为什么 in-process 比 loopback 更简单？

因为一旦接受“managed runtime 不会成为独立 endpoint”，loopback 方案就失去意义了。

如果还走 loopback，就会平白引入：

- 内部 HTTP server
- 内部 auth token
- 内部 endpoint 注册
- `process_group_id`
- 与 `/endpoints` 语义不一致的“伪独立 runtime”

而真正需要的只是：

- Telegram/Slack 接收消息
- 把消息转成 Console task submit
- 共享同一个 Console 视图、配置托管与 heartbeat

所以这里更简单、也更正确的方案就是 in-process handler。

---

## 4) 核心提议

### 4.1 配置项

新增：

```yaml
console:
  managed_runtimes: ["telegram", "slack"]
```

语义：

- `console.managed_runtimes` 表示“由 `console serve` 进程自己挂载的入站 runtime handler 列表”。
- 它不表示“额外启动几个 daemon endpoint”。
- 运行参数仍来自各自原有配置块：
  - `telegram.*`
  - `slack.*`
  - `line.*`
  - `lark.*`

### 4.2 与 `console.endpoints` 的关系

保留现有 `console.endpoints`，但语义更明确：

- `console.endpoints`：外部独立 runtime
- `console.managed_runtimes`：Console 进程内托管的入站 handler

两者不是一回事。

因此：

- managed runtime 不需要被自动注册到 `/endpoints`
- 也不需要 `submit_endpoint_ref` 映射
- 也不需要和 `console.endpoints` 做“重复 endpoint”层面的兼容设计

### 4.3 第一阶段支持范围

第一阶段只支持：

- `telegram`
- `slack`

原因：

- 这两条路径最常用
- 当前 channel runtime 代码也最值得先拆出“inbound handler”和“standalone daemon”两层职责

对于其他值：

- 如果出现在 `console.managed_runtimes` 中，启动直接报错

---

## 5) 架构方案

### 5.1 总体形态

`console serve` 变成一个“单 Console surface + 多 managed runtime”的进程：

```text
console server
  -> console agent runtime
  -> console heartbeat
  -> managed runtime supervisor
       -> telegram handler
       -> slack handler
  -> external configured endpoints
```

这里的关键点是：

- Telegram/Slack handler 不再拥有自己的 daemon API
- Telegram/Slack handler 不再暴露自己的 endpoint
- Telegram/Slack 任务会写入 Console 自己的 task/topic store
- 第一阶段继续复用各自现有的 in-process 执行路径，而不是再走 loopback

### 5.2 ASCII 图

```text
                +----------------------------------+
                | Browser / Console SPA            |
                +----------------+-----------------+
                                 |
                                 v
                +----------------+-----------------+
                | console serve backend            |
                | /auth /endpoints /proxy          |
                +----------------+-----------------+
                                 |
       +-------------------------+--------------------------+
       |                         |                          |
       v                         v                          v
+------+-------+     +-----------+-----------+    +---------+---------+
| Console Self |     | Managed Runtime Supv |    | External Endpoints |
| agent/store  |     | telegram/slack       |    | console.endpoints  |
| heartbeat    |     | inbound handlers     |    | existing behavior  |
+------+-------+     +-----------+-----------+    +---------+---------+
       ^                         |
       |                         |
       +-------------------------+
                 task submit / event ingress
```

### 5.3 任务流转

当前实现下，managed runtime 的任务流转是：

1. Telegram/Slack 收到入站消息
2. 解析 channel metadata、sender、thread/conversation id
3. 把任务写入 Console 自己的 task/topic store
4. 在同一进程内运行各自现有的 telegram/slack task loop
5. 将结果继续写回 Console 视图并回发到对应 channel

这意味着：

- 任务归属和可见性都在 Console
- task/topic 视图统一
- heartbeat 统一
- `Agent Settings` 变更后通过 reload/restart 在同一进程内统一生效

---

## 6) Managed Runtime 的正确边界

### 6.1 它是什么

在本方案里，managed runtime 本质上是一个 **channel ingress adapter**：

- 负责与 Telegram/Slack 建立连接
- 负责收消息、ack webhook/polling
- 负责把入站消息转给 Console
- 负责把 agent 输出结果回发到对应 channel

### 6.2 它不是什么

它不是：

- 一个独立 daemon server
- 一个独立 `/health` `/tasks` `/overview` API 提供者
- 一个独立 Console surface
- 一个独立 heartbeat owner
- 一个独立 endpoint

### 6.3 对现有代码的含义

这要求我们把当前 channel runtime 里混在一起的两层职责拆开：

1. **channel transport / handler**
2. **standalone runtime daemon wrapper**

`console serve` 只需要前者。  
独立 `telegram run` / `slack run` 这类命令才需要后者。

---

## 7) Heartbeat 策略

这件事必须写死，不做“第一阶段先这样、以后再看”的模糊表述：

- managed runtime 不运行自己的 heartbeat
- Console 自己运行 heartbeat
- 以后也保持这个策略

原因：

1. heartbeat 是系统级行为，不是 channel 级行为。  
2. 如果每个 managed runtime 都带 heartbeat，会产生重复任务和重复通知。  
3. 在“单 Console runtime + 多入站 handler”的模型里，heartbeat 的所有产物本来就应该归属于 Console。

因此：

- managed 模式下，Telegram/Slack 的 heartbeat 接线必须被禁用
- `console.managed_runtimes` 不承载 heartbeat 语义
- 不再规划 `console.managed_heartbeat` 之类的配置

---

## 8) Endpoint 呈现

在本方案中：

- `ep_console_local` 继续存在
- `console.endpoints` 里的外部 endpoint 继续存在
- managed Telegram/Slack **不新增 endpoint**

换句话说，UI 里不会出现：

- `ep_managed_telegram`
- `ep_managed_slack`

因为它们已经被合并进 Console 自己。

如果后续需要区分任务来源，应当通过：

- task trigger
- channel/source metadata
- task/topic 过滤能力

而不是通过制造额外 endpoint。

---

## 9) 设置与重载

这个架构下，`Agent Settings` 的语义是：

- `PUT /api/settings/agent` 先 reload Console Local runtime
- 再 restart managed Telegram/Slack

原因：

- 它们虽然没有独立 endpoint，但第一阶段仍复用各自现有的执行 loop
- 要让新的 `llm/multimodal/tools` 配置立刻对新入站任务生效，restart 是最稳妥的做法

需要区分的是：

- `Agent Settings`：影响 Console Local 和 managed Telegram/Slack
- channel 自身配置（如 bot token、app token、webhook/polling 参数）：不属于这次范围；若后续要做运行时热更新，再单独设计

---

## 10) 代码组织建议

### 10.1 先拆“channel handler”和“standalone wrapper”

当前 Telegram/Slack/LINE/Lark 的实现更像是“完整 runtime”。

本次应把它们拆成两层：

```text
internal/channelruntime/<kind>/
  handler.go        # 入站/出站 handler，可嵌入 console
  standalone.go     # 独立 daemon runtime 包装
```

或者等价的包内结构。

关键是边界要清楚：

- handler 层：可被 console 直接挂载
- standalone 层：只给 `telegramcmd/slackcmd` 这类命令用

### 10.2 Console 侧需要的接口

Console 不需要 channel runtime 全套对象，只需要一个最小 supervisor 接口，例如：

```go
type ManagedRuntime interface {
    Start(ctx context.Context) error
    Kind() string
}
```

### 10.3 不需要的基础设施

采用本方案后，这些东西都不再需要为 managed runtime 新增：

- `process_group_id`
- loopback ephemeral listen
- 内部 auth token
- `StartServer(...)` 返回 bound addr 的改造
- managed endpoint auto-registration

---

## 11) 配置示例

```yaml
server:
  auth_token: "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

console:
  listen: "127.0.0.1:9080"
  base_path: "/"
  managed_runtimes: ["telegram", "slack"]
  endpoints:
    - name: "Remote Main"
      url: "http://127.0.0.1:8787"
      auth_token: "${MISTER_MORPH_ENDPOINT_MAIN_TOKEN}"

telegram:
  bot_token: "${MISTER_MORPH_TELEGRAM_BOT_TOKEN}"

slack:
  bot_token: "${MISTER_MORPH_SLACK_BOT_TOKEN}"
  app_token: "${MISTER_MORPH_SLACK_APP_TOKEN}"
```

说明：

- `telegram.*` / `slack.*` 仍在原位置配置
- `managed_runtimes` 只表示 Console 是否挂载它们
- 这些 managed runtime 不再需要自己的 `server.listen` / `server.auth_token`

---

## 12) 风险与控制

- 风险：当前 channel runtime 代码把 transport、task runtime、daemon server 耦合得太紧。  
  控制：本次先把 Telegram/Slack 拆出 handler 层，再接入 console。

- 风险：继续沿用“独立 runtime”思维，结果又把 managed handler 做成伪 endpoint。  
  控制：文档和实现都明确，managed runtime 不产生独立 endpoint。

- 风险：heartbeat 被 channel 侧重复拉起。  
  控制：managed 模式下显式禁用 Telegram/Slack 自己的 heartbeat 接线。

- 风险：`Agent Settings` 修改后，managed runtime 继续用旧配置。  
  控制：settings save 后 reload Console Local，并 restart managed runtimes。

---

## 13) 非目标

- 第一阶段不把 managed runtime 暴露为独立 endpoint
- 第一阶段不走 loopback daemon
- 第一阶段不为 managed runtime 增加独立 `/health` `/tasks` `/overview`
- 第一阶段不支持 `line` / `lark`
- 第一阶段不处理 channel 自身配置的运行时热更新

---

## 14) 实施 Checklist

### Phase 0: 设计收敛

- [x] 配置键确定为 `console.managed_runtimes`
- [x] managed runtime 明确定义为 in-process handler，而不是 loopback daemon
- [x] managed runtime 不产生独立 endpoint，而是合并进 Console
- [x] heartbeat 归 Console 独占，managed runtime 永不自带 heartbeat
- [x] 第一阶段支持范围确定为 `telegram` / `slack`

### Phase 1: Channel 边界拆分

- [ ] 从 Telegram runtime 中拆出可嵌入的 handler 层
- [ ] 从 Slack runtime 中拆出可嵌入的 handler 层
- [ ] 将 standalone daemon 包装与 handler 层解耦
- [ ] 确保 managed 模式下不会接入各自 heartbeat

### Phase 2: Managed Runtime 托管边界

- [ ] 明确 managed Telegram/Slack 继续使用各自 runtime 执行路径
- [ ] 将 managed runtime 的 task/topic 视图并入 Console store
- [ ] 确保 managed runtime 不暴露独立 daemon API
- [ ] 确保 managed runtime 不出现在 `/endpoints`

### Phase 3: Managed Runtime Supervisor

- [x] 在 `consolecmd` 中增加 managed runtime supervisor
- [x] 启动时解析 `console.managed_runtimes`
- [x] 启动时校验 Telegram/Slack 所需配置
- [x] 在 Console 启动期挂载 managed runtimes
- [x] 任一 managed runtime 初始化失败时 fail-fast 终止 `console serve`

### Phase 4: UI 与可观测性语义

- [x] `/endpoints` 继续只返回 Console 自己和 external endpoints
- [x] 确保 managed runtime 不会出现在 endpoint 列表
- [x] 通过 task trigger / source metadata 保留 Telegram/Slack 来源信息
- [ ] `docs/console.md` 明确说明 managed runtime 的 UI 呈现方式

### Phase 5: 设置与回归测试

- [x] 验证 `PUT /api/settings/agent` 会 reload Console Local 并 restart managed runtimes
- [x] supervisor 启动成功/失败测试
- [ ] managed 模式下 heartbeat 不重复运行测试
- [ ] Telegram/Slack 入站消息接入 Console 的集成测试
- [x] `assets/config/config.example.yaml` 增加配置示例

---

## 15) DoD

- `console serve` 可通过 `console.managed_runtimes` 同时挂载 `telegram` 和 `slack`
- Telegram/Slack 在 managed 模式下不运行自己的 heartbeat
- UI 中不新增 managed runtime endpoint；它们被合并进 Console
- Telegram/Slack 入站消息会进入 Console 自己的 task/topic/store 流程
- `Agent Settings` 更新后，Console Local reload + managed runtime restart 后会对其新任务生效
