---
date: 2026-06-22
title: 人类审批流程需求
status: draft
---

# 人类审批流程需求

## 1) 背景

现在代码里已经有审批的底层结构，但还不是一个用户可用的功能。

已有能力：

- `guard` 能生成 `ApprovalRecord`，写入 `guard_approvals.json`，并把 guard audit 写入审计日志。
- `agent.Engine` 在 guard 返回 `require_approval` 时，会保存 resume state，并返回 `PendingOutput`。
- `agent.Engine.Resume(...)` 能在审批通过后继续执行同一个 agent run。
- `daemonruntime.TaskInfo` 已有 `TaskPending`、`PendingAt`、`ResumedAt`、`ApprovalRequestID` 字段。
- Console local runtime 能识别 pending final，并把 task 标记为 `pending`。

缺失能力：

- 没有公开的 approvals API。
- Console 没有 pending approval 列表、详情、approve/deny 按钮。
- Telegram、Slack runtime 没有 pending 分支，也不会把审批请求发给人。
- Telegram、Slack 发送层目前主要发文本，没有审批按钮回调。
- `taskruntime.Runtime` 只暴露 `Run`，没有 resume 入口。
- pending task 重启后会被取消，不是可跨进程恢复的长期挂起任务。

所以这次需求不是重写 guard。正确方向是把已有的 guard pending/resume 链路接到 runtime、HTTP API、Console UI 和聊天渠道。

第一版按现有边界做减法：

- 不新增数据库。
- 不新增独立 approval service package。
- 不要求 `ApprovalStore` 先支持全量列表查询。
- 不做进程重启后的 pending resume。
- 不做聊天命令审批。

## 2) 目标

第一版实现一个可以实际使用的人类审批流程：

1. 当 guard 需要审批时，任务进入 `pending`。
2. 系统把审批请求展示给人，而不是只把 `approval_request_id` 放进 final 文本。
3. 人可以在 Console、Telegram 或 Slack 用按钮批准或拒绝。
4. 批准后，系统恢复原 task，并继续执行原来的 agent run。
5. 拒绝后，系统结束原 task，状态为 `canceled`，错误原因是 `Approval denied. Task canceled.`。
6. 审批请求和审批结果都写入 audit，保留 `approval_request_id`、`task_id`、`run_id`、审批人和时间。
7. 通知内容只展示可读摘要和原因，不展示完整 raw params、secret 或大段工具输入。

审批按钮是本需求的一部分：

- Console 使用页面按钮。
- Telegram 使用 inline keyboard 按钮。
- Slack 使用 Block Kit button。
- 第一版不添加 `/approve`、`/deny` 这类聊天命令。
- 如果某个 channel 的按钮回调暂时不可用，该 channel 只发送通知和 Console 审批入口，不用命令兜底。

## 3) 非目标

- 不做多审批人投票。
- 不做复杂审批策略语言。
- 不把审批做成开放式问答。
- 不在第一版支持进程重启后的 pending task 恢复。
- 不把 raw tool args 全量发到聊天渠道。
- 不新增数据库。
- 不新增聊天命令审批入口。
- 不实现 Slack modal 或 Telegram 多步表单。
- 不把所有“不确定”都塞进审批流程。

“模型缺信息，需要问用户”与“某个动作需要授权”是两件事。第一版只处理后者。前者应该走正常对话，或者后续单独做 `pending_input` / `ask_human`，不要和审批状态混在一起。

## 4) 第一性原则

1. 审批是对一个确定动作授权。  
   审批对象必须有稳定的 `approval_request_id` 和 `action_hash`。不能靠自然语言猜人同意了什么。

2. guard 负责判断是否需要审批，runtime 负责把审批交给人。  
   guard 不应该知道 Telegram、Slack 或 Console UI。

3. approve/deny 必须只生效一次。  
   已批准、已拒绝、已过期的审批请求不能再次改变状态。

4. resume 必须使用等价的 runtime 上下文。  
   不能用一个缺少 channel tool、cwd、persona、skills 或 guard store 的新 engine 去恢复旧任务。

5. 聊天渠道按钮只能携带最小 callback payload。  
   payload 只放 approval id 和 decision，不放 raw params。

6. 通知默认发给配置的审批目标。  
   不默认让原始请求者在公共群里自批。是否通知原 conversation 应显式配置。

7. pending 不是永久队列。  
   第一版沿用已有过期时间。进程重启后的 pending task 仍按现有任务恢复规则取消。

## 5) 当前架构事实

### 5.1 Guard

`guard/approvals.go` 定义：

- `ApprovalRecord`
- `ApprovalStore`
- `RequestApproval`
- `ResolveApproval`

文件实现位于 `guard/approvals_file.go`。当前 store 能按 id 读写，但缺少适合 Console 列表页的 pending 查询能力。

guard 已经写审计事件。审批功能应继续复用它，而不是在 runtime 里重建另一套审批事实。

### 5.2 Agent

`agent/engine_loop.go` 在工具调用前做 guard pre-check。遇到 `DecisionRequireApproval` 时：

- 序列化 resume state。
- 调用 `RequestApproval`。
- 返回 `Final{Output: PendingOutput{...}}`。

`agent/engine_resume.go` 已经实现 `Engine.Resume(ctx, approvalID)`：

- 读取 approval record。
- 要求状态为 `approved`。
- 校验 action hash。
- 用保存的 resume state 继续执行。

这部分是核心能力，不需要重写。

### 5.3 Runtime

`internal/channelruntime/taskruntime.Runtime` 负责构造 engine 并执行 task，但只有 `Run`，没有 `Resume`。

Console local runtime 已经能把 pending final 转成 pending task。Telegram、Slack runtime 现在没有 pending 分支：它们会把最终输出当普通结果处理，不会停在 pending，也不会通知审批人。

这意味着第一版必须补两个点：

- runtime 需要统一识别 pending final。
- runtime 需要能用同一套上下文恢复 pending task。

### 5.4 HTTP / Console

daemon HTTP 已经有 task API，但没有 approval API。

Console 目前可以看到 task 状态，但没有：

- pending approval 列表。
- approval 详情。
- approve/deny 操作。
- 审批后 task 恢复状态。

### 5.5 Telegram / Slack

Telegram、Slack 已有发送文本消息的路径，也有 runtime 收消息的路径。审批按钮需要新增的是：

- outbound message 支持按钮。
- inbound event 支持按钮 callback。
- callback payload 解析和权限校验。
- 根据审批结果更新原消息或发一条短反馈。

不要用 `/approve`、`/deny` 命令替代按钮。命令容易被复制、误发，也不适合携带防重放信息。

## 6) 设计

### 6.1 Pending approval 来源

第一版不扩展 `guard.ApprovalStore` 做通用列表查询。原因是产品入口是“待处理任务”，而不是“所有历史审批记录”。

`GET /approvals` 的数据来源：

- 从 task store 读取 `status=pending` 且带 `ApprovalRequestID` 的 task。
- 对每个 task 调 `ApprovalStore.Get(approval_request_id)` 取审批记录。
- 跳过已经过期的记录，或在响应里标为 `expired`。
- 按 `PendingAt` 或 approval 创建时间倒序返回。

这样不用新增 approval 索引，也不用把 `ApprovalStore` 扩成查询系统。后续如果需要审计所有审批历史，再给 store 增加列表能力。

### 6.2 Runtime pending registry

第一版在 runtime host 内维护一个很小的内存表：

```text
approval_request_id -> pending task handle
```

handle 保存：

- `task_id`
- `runtime`
- `target`
- `resume(ctx, approval_request_id)`
- `cancel(reason)`

任务进入 pending 时注册 handle。审批通过时调用 `resume`；审批拒绝或过期时调用 `cancel`。

这个内存表符合当前事实：pending task 重启后本来就会被取消。第一版不要为了一个当前不支持的恢复语义，把 resume 状态做成跨进程队列。

如果 `Run` 和 `resume` 需要相同的 engine 构造逻辑，可以抽一个内部 helper。这个 helper 只有在两条路径真实复用时才加，不能只是换名字。

### 6.3 审批处理入口

第一版不新增独立 approval service package。审批处理可以先放在现有 daemon/runtime 代码里，逻辑保持清楚即可：

Approve:

- 读取 approval record。
- 校验 pending、未过期、调用者有权限。
- 调 `guard.ResolveApproval(... approved ...)`。
- 查 pending registry。
- 调 handle.resume。
- 更新 task 状态。

Deny:

- 读取 approval record。
- 校验 pending、未过期、调用者有权限。
- 调 `guard.ResolveApproval(... denied ...)`。
- 查 pending registry。
- 调 handle.cancel，task 变为 `canceled`。

只有当 Console、Telegram、Slack 三处实现开始重复同一段代码时，再把它抽成内部 package。

### 6.4 HTTP API

新增 API：

```text
GET  /approvals?status=pending&limit=50
POST /approvals/{approval_request_id}/approve
POST /approvals/{approval_request_id}/deny
```

请求体：

```json
{
  "actor": "console:user",
  "note": "optional"
}
```

响应至少包含：

```json
{
  "approval_request_id": "apr_...",
  "status": "approved",
  "task_id": "task_...",
  "resumed": true
}
```

如果 resume 启动失败，approval 仍然已经是 approved，但响应必须明确：

```json
{
  "status": "approved",
  "resumed": false,
  "error": "..."
}
```

这种情况要写 audit，方便排障。

### 6.5 Console UI

Console 第一版需要：

- 在 task 详情或 audit/approval 页面显示 pending approval。
- 显示 action 摘要、原因、过期时间、task id、runtime。
- 提供 `Approve` 和 `Deny` 按钮。
- 点击后调用 approval API。
- approve 后显示 task 已恢复或恢复失败。
- deny 后显示 task 已取消。

不要要求用户复制 approval id，也不要要求用户输入命令。

### 6.6 Telegram / Slack 按钮

新增 channel approval notification：

- Telegram 发 inline keyboard：
  - `Approve`
  - `Deny`
- Slack 发 Block Kit message：
  - `Approve`
  - `Deny`

按钮 callback payload 使用短字符串，不使用聊天命令，也不把完整 JSON 塞进 Telegram `callback_data`：

```text
ap:<a|d>:<approval_request_id>
```

第一版不加额外 `mac`。理由：

- Slack interactive request 已有平台签名校验。
- Telegram callback 来自 bot 发出的 inline button。
- approval record 本身是 single-use，并且有过期时间。

实现时要保证 Telegram payload 不超过平台限制。若 approval id 太长，可以保存一个短 nonce 到 approval record 或内存 pending map，再由 nonce 找回 approval id。因为第一版 pending task 不跨进程恢复，内存 nonce 可以接受。

按钮点击后：

- 校验 approval record 是否存在、pending、未过期。
- 调用审批处理入口。
- 更新原审批消息，或发一条短反馈。

反馈建议：

- approved: `Approved. Task resumed.`
- denied: `Denied. Task canceled.`
- expired: `Approval expired.`
- already resolved: `Already approved by ...` 或 `Already denied by ...`

### 6.7 审批目标

审批只需要一个开关：

```yaml
guard:
  approvals:
    enabled: false
```

规则：

- `enabled=false` 时不触发审批流程。
- Telegram 审批按钮发回触发任务的原 chat/thread。
- Slack 审批按钮发回触发任务的原 channel/thread。
- Console API/UI 跟随 `enabled`，不需要单独配置。

第一版不做角色系统。聊天渠道审批权限沿用对应 channel runtime 的访问控制。Console 权限沿用现有 Console 访问控制。

## 7) 状态流

### 7.1 请求审批

```text
running
  -> guard require_approval
  -> approval record pending
  -> task pending
  -> notify approvers
```

### 7.2 批准

```text
approval pending
  -> approved
  -> task running
  -> agent resume
  -> task done / failed / canceled
```

### 7.3 拒绝

```text
approval pending
  -> denied
  -> task canceled
```

### 7.4 过期

```text
approval pending
  -> expired
  -> task canceled
```

第一版可以在读取、列表、approve/deny 时处理过期，不需要单独后台 sweep。

## 8) 审计与观测

审批相关 audit 至少包含：

- `approval_requested`
- `approval_approved`
- `approval_denied`
- `approval_expired`
- `approval_resume_failed`

字段：

- `approval_request_id`
- `task_id`
- `run_id`
- `trace_id`
- `runtime`
- `target`
- `actor`
- `time`
- `reason`
- `action_hash`

不要把完整 raw params 写进面向聊天渠道的通知。audit 文件本身按敏感数据处理。

## 9) 安全规则

1. approve/deny 必须校验审批请求仍是 pending。
2. approve 后 resume 必须校验 action hash。
3. Telegram/Slack callback 来源必须在配置的审批目标内。
4. Slack callback 必须通过平台签名校验。
5. 未配置审批目标时，不允许任意 chat/channel 审批。
6. 审批按钮过期后不能继续生效。
7. 审批通知不得包含 secret、完整环境变量、完整 shell script 或大型文件内容。

## 10) 实现 Checklist

### Phase 1: Pending approval API

- [x] 测试：`GET /approvals` 从 pending task 反查 approval record。
- [x] 测试：过期 approval 不作为可审批项返回。
- [x] 测试：`POST /approve` 会 resolve approval 并调用 pending handle resume。
- [x] 测试：`POST /deny` 会 resolve approval 并调用 pending handle cancel。
- [x] 实现 approvals API。

### Phase 2: Runtime pending registry

- [x] 测试：task pending 时注册 `approval_request_id -> handle`。
- [x] 测试：approved approval 会恢复原 task。
- [x] 测试：denied approval 会把原 task 标为 `canceled`。
- [ ] 测试：进程内找不到 pending handle 时返回清楚错误。
- [x] 实现 runtime pending registry。
- [x] 只有在 `Run` 和 `resume` 真实共用时，才抽 engine 构造 helper。

### Phase 3: Console UI

- [x] 增加 pending approval 展示。
- [x] 增加 approve/deny 按钮。
- [x] approve/deny 后刷新 task 状态。
- [x] 失败时显示可读错误。

### Phase 4: Telegram 按钮

- [ ] 测试：pending task 会发送带按钮的审批通知。
- [ ] 测试：callback approve 会 resolve 并 resume。
- [ ] 测试：callback deny 会 resolve 并 cancel。
- [ ] 测试：非法 chat id 被拒绝。
- [x] 实现 inline keyboard 发送。
- [x] 实现 callback query 解析。

### Phase 5: Slack 按钮

- [ ] 测试：pending task 会发送带按钮的审批通知。
- [ ] 测试：interactive callback approve 会 resolve 并 resume。
- [ ] 测试：interactive callback deny 会 resolve 并 cancel。
- [ ] 测试：非法 channel id 被拒绝。
- [ ] 测试：非审批 action_id 不会触发审批。
- [x] 实现 Block Kit button 发送。
- [x] 实现 interactive callback 解析。

### Phase 6: 文档和配置

- [x] 更新 `assets/config/config.example.yaml`。
- [ ] 更新用户文档，说明如何开启审批。
- [x] 说明第一版 pending task 不跨进程恢复。
- [x] 说明聊天渠道只支持按钮审批，不支持审批命令。

## 11) 需要避免的设计

- 不要新增一套 approval JSON 文件。`guard.ApprovalStore` 已经是事实源。
- 不要让 Telegram/Slack 直接改 task store。它们应调用审批处理入口。
- 不要靠解析 final 文本判断 pending。应使用 `PendingOutput`。
- 不要把 approve/deny 做成普通 agent tool。审批是 runtime 控制动作，不是模型动作。
- 不要实现聊天命令审批。按钮能绑定 id 和 decision，也能减少误操作。
- 不要为了第一版新增 callback `mac`。平台签名、callback 来源校验、single-use approval 和过期时间已经够用。
- 不要为第一版做重启后 resume。当前 task store 已经会取消非终态 task，先保持一致。
