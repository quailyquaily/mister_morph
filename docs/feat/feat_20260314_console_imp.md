---
date: 2026-03-14
title: Console Iteration（Setup 流程 + Contacts 视图 + Chat 视图）
status: draft
---

# Console Iteration（Setup 流程 + Contacts 视图 + Chat 视图）

## 1) 背景

当前 Console 已具备基础管理能力，但存在三个明显问题：
- 初次配置与启动步骤偏多，门槛高。
- `Files` 视图把联系人文件和其他状态文件混在一起，且只展示原始文本，不利于管理联系人。
- 缺少一个“直接给当前 agent 发任务”的会话入口，任务交互链路不完整。

本迭代按以下目标推进。

## 2) 迭代目标

1. 优化 Console setup 流程。  
2. 将联系人从 `Files` 中拆分为独立侧栏入口 `Contacts`，并将 `ACTIVE.md` / `INACTIVE.md` 解析后按联系人列表展示。  
3. 新增侧栏第一个入口 `Chat`，可直接向当前 endpoint 的 agent 发任务，并以聊天形式展示 `ChatHistoryItems`。  
4. 精简 `Settings` 页面：移除监控/诊断类内容，仅保留语言切换与登出。  
5. 调整导航位置：
   - `Settings` 不再放在侧栏，入口放到右上角（位于 endpoint 切换器右侧）。
   - `Runtime` 调整为侧栏最后一个入口。  

## 3) 非目标

- 本迭代不重做 daemon task 执行模型（仍基于 `/tasks` 提交 + 查询）。
- 本迭代不引入实时流式传输（WebSocket/SSE），先用轮询闭环。
- 本迭代不改变 contacts markdown 存储格式（仍使用现有 `ACTIVE.md` / `INACTIVE.md`）。

## 4) 现状摘要

- 侧栏当前主要入口为：Runtime / Tasks / Stats / Audit / Memory / Files / Settings。  
- `Files` 视图当前统一编辑：`TODO.md`、`TODO.DONE.md`、`ACTIVE.md`、`INACTIVE.md`、`IDENTITY.md`、`SOUL.md`、`HEARTBEAT.md`。  
- `Settings` 当前承载系统配置快照与诊断信息，同时提供语言切换与登出。  
- daemon 端已有：  
  - `POST /tasks`（提交任务）  
  - `GET /tasks/{id}`（查任务详情）  
  - `GET/PUT /contacts/files/*`（联系人原始文件读写）  

## 5) 方案设计

### 5.1 Setup 流程优化

目标：从“手工拼配置 + 手工验证”收敛为“向导式最小可用配置 + 可验证连接”。

建议落地：
- 在安装/初始化流程中增加 Console 配置引导（可挂在 `install` 流程里，也可单独提供 `console setup` 子命令）。
- setup 至少采集：
  - `console.listen`
  - `console.base_path`
  - `console.password` 或 `console.password_hash`
  - 至少一个 endpoint（`name`、`url`、`auth_token` 的 `${ENV_VAR}` 引用）
- setup 完成后输出：
  - 写入的配置片段
  - 需要设置的环境变量清单
  - 一次 endpoint 健康检查结果（成功/失败 + 原因）

验收：
- 新用户在一次交互内可完成 Console 必要配置。
- setup 结束后可直接启动并进入登录页。

### 5.2 Files 拆分 Contacts

目标：联系人管理从“文件编辑器思维”升级为“结构化联系人视图”。

信息架构调整：
- `Files` 保留：`TODO.md`、`TODO.DONE.md`、`IDENTITY.md`、`SOUL.md`、`HEARTBEAT.md`。
- 新增侧栏 `Contacts`（独立入口，不再放在 `Files` 下）。

后端接口建议（daemon 路由，经 console `/proxy` 透传）：
- 新增 `GET /contacts/list?status=active|inactive|all`
  - 服务端直接复用现有 contacts 解析逻辑读取 markdown 并返回结构化联系人。
- 保留 `GET/PUT /contacts/files/{name}` 作为“原始文件高级编辑”兜底能力。

`Contacts` 视图展示要求：
- 列表字段（按语义展示，而不是原样 YAML）：
  - 主标识：`nickname`（优先）/ `contact_id`
  - `status`（active/inactive）
  - `kind`（human/agent）
  - `channel`
  - Reachability 相关字段（按渠道展示）
  - `persona_brief`
  - `topic_preferences`（tag）
  - `cooldown_until`
  - `last_interaction_at`
- 建议交互：
  - 状态筛选（全部/Active/Inactive）
  - 渠道筛选（telegram/slack/line/lark）
  - 联系人详情抽屉（展示完整结构化字段）
  - “查看原始文件”入口（跳转或侧栏打开 raw 内容）

验收：
- 用户无需阅读 markdown，即可看懂联系人状态与可达信息。
- `ACTIVE.md`/`INACTIVE.md` 变更后，Contacts 视图可刷新并正确反映。

### 5.3 新增 Chat 视图（侧栏第一个入口）

目标：在 Console 内形成“提交任务 -> 观察执行 -> 获得结果”的聊天式闭环。

路由与导航：
- 新增路由：`/chat`
- 侧栏排序中将 `Chat` 放在第一个入口。

交互流程：
1. 在 `Chat` 输入任务并发送。  
2. 前端调用 `POST /tasks`（通过 `runtimeApiFetch`，绑定当前 endpoint）。  
3. 任务提交后进入轮询 `GET /tasks/{id}`。  
4. 按状态更新消息条目，直到 `done/failed/canceled`。  

`ChatHistoryItems` 数据模型（Console 视图层）建议：
- `id`
- `role`：`user | assistant | system`
- `text`
- `task_id`
- `status`：`queued | running | pending | done | failed | canceled`
- `created_at` / `finished_at`
- `meta`（可选）：`model`、`steps_count`、`error`

展示要求：
- 用户消息、系统状态消息、助手回复消息分栏展示。
- `pending/running` 有明显状态标识。
- 当任务完成时，优先展示 `result.final.output`（兼容已有结果结构），并可展开查看 steps/metrics。

验收：
- 用户可从 Chat 视图直接发任务给当前 endpoint 对应 agent。
- Chat 视图内存在连续的 `ChatHistoryItems`，能完整表达一轮会话（提问、处理中、完成/失败）。

### 5.4 Settings 与导航重排

目标：收敛设置职责，减少侧栏噪音，提升高频操作可达性。

信息架构调整：
- 侧栏入口顺序建议改为：
  - `Chat`（第一）
  - `Tasks`
  - `Stats`
  - `Audit`
  - `Memory`
  - `Contacts`
  - `Files`
  - `Runtime`（最后）
- `Settings` 从侧栏移除。
- 在顶部栏 endpoint 切换控件右侧增加 `Settings` 入口按钮（齿轮图标/文案均可）。

`Settings` 页面内容收敛：
- 保留：
  - 语言切换
  - 登出（danger）
- 移除：
  - 系统配置快照展示
  - 诊断/监控相关板块（health、diagnostics、runtime checks 等）

验收：
- 用户从任意业务视图可在右上角快速进入 Settings。
- Settings 页面只包含语言切换和登出两个动作区。
- 侧栏中 `Runtime` 为最后一项，`Settings` 不再出现。

## 6) 实施分批建议

### Phase A：导航与路由重构
- 新增 `/chat`、`/contacts` 路由。
- 调整侧栏顺序：`Chat` 置顶，`Runtime` 置底。
- 移除侧栏 `Settings` 入口，并在 topbar endpoint 切换器右侧新增 `Settings` 入口。
- 收缩 `Files` 文件集合（移除 contacts 文件项）。

### Phase B：Contacts 结构化接口 + 页面
- daemon 增加 `GET /contacts/list`。
- 前端新增 `ContactsView`，实现列表/筛选/详情。
- 保留原始文件编辑兜底入口。

### Phase C：Chat 页面
- 前端新增 `ChatView`。
- 接入 `/tasks` 提交 + 轮询。
- 落地 `ChatHistoryItems` 视图模型与状态渲染。

### Phase D：Setup 流程优化
- 增加/完善 setup 向导中的 console 配置部分。
- 增加 endpoint 连接校验与环境变量提示。
- 文档同步到 `docs/console.md` 与 `assets/config/config.example.yaml` 注释。

### Phase E：Settings 页面收敛
- 精简 `SettingsView`：仅保留语言切换和登出。
- 删除与监控/系统诊断相关展示区块及其前端依赖请求。
- 回归验证：设置入口位置、语言切换、登出流程。

## 7) 风险与控制

- 风险 1：联系人文件格式异常导致列表解析失败。  
控制：接口返回可诊断错误；视图提供 raw 文件回退入口。

- 风险 2：Chat 轮询导致请求频率过高。  
控制：固定轮询间隔（例如 1-2s）+ 任务终态后立即停止。

- 风险 3：setup 改造影响现有 install 默认流程。  
控制：保持兼容模式；仅在需要时触发交互，不破坏 `--yes` 与非交互场景。

## 8) DoD（完成定义）

- `Chat` 成为侧栏第一入口，可提交任务并看到 `ChatHistoryItems`。  
- `Contacts` 成为独立入口，展示结构化联系人列表。  
- `Files` 不再承担联系人主入口职责。  
- `Runtime` 位于侧栏最后一项。  
- `Settings` 从侧栏移除，入口位于右上角 endpoint 切换器右侧。  
- `Settings` 页面仅保留语言切换与登出。  
- setup 流程可以引导完成 Console 所需最小配置并给出验证结果。  
- 相关单元测试与文档同步完成。  
