---
date: 2026-02-08
title: Workspace Persona Context + Group Chat Rules + Contacts Profile 扩展方案
status: draft
---

# 任务细化稿（2026-02-08）

本文将需求拆成 4 条实施流：

1. 增加 `TOOLS.md`（本地工具环境笔记）
2. 增强群聊发言规则（减少“刷屏感”）
3. 扩展 `contacts` 画像字段（`pronouns` / `timezone` / 长文本偏好上下文）
4. 增加 `contacts_upsert` 内置 tool（让 agent 可直接录入/更新联系人）

## 1) 背景与目标

目标不是新增“能力”，而是把现有能力的行为边界更显式、可调、可测试：

- 通过 `TOOLS.md` 把环境特定知识从 skills/tool schema 中剥离。
- 通过群聊规则把“该说/不该说”落为可执行约束。
- 通过 `contacts` 新字段补齐联系人画像中缺失的人类语境信息。

## 2) 范围与非目标

范围：

- `assets/config/` 模板补充
- prompt 组装链路补充 block/rules
- Telegram 群聊策略增强
- contacts 数据结构、CLI、tool、文档、测试补充

非目标：

- 不改 MAEP 协议层
- 不重做 memory 架构
- 不引入新的外部存储（仍基于 markdown/json 文件存储）

## 3) 工作流 A: 增加 TOOLS.md

### A.1 设计意图

`TOOLS.md` 是“本地环境映射笔记”，不是工具定义本身。  
它记录 camera 名称、设备别名、SSH 别名、TTS 偏好等，减少 agent 猜测和反复提问。

### A.2 任务拆解

- [ ] 新增模板 `assets/config/TOOLS.md`
  - 内容定位：本地环境映射、示例、与 skills 分离原则。
- [ ] 安装流程补充 `TOOLS.md`
  - 文件：`cmd/mistermorph/install.go`
  - 补充 loader：`loadToolsTemplate()`
  - 在 `filePlans` 中新增 `TOOLS.md`（存在则跳过）。
- [ ] prompt 组装链路补充 `TOOLS.md` 注入
  - 建议方式：新增 `internal/promptprofile/context.go`（或在现有 `identity.go` 扩展），读取 `TOOLS.md` 后以 `PromptBlock` 注入，而不是覆盖 `spec.Identity`。
  - block title 建议：`Local Tool Notes`。
- [ ] 文档更新
  - `docs/prompt.md`：增加 “TOOLS.md context block” 的来源与注入时机。

### A.3 关键决策点

- [ ] 决定 `TOOLS.md` 读取路径基准（`file_state_dir` 或 workspace root）并统一到代码和安装行为。
- [ ] 约束最大注入长度（例如 4KB/8KB）避免 prompt 膨胀。

### A.4 验收标准

- 设计合理的 `TOOLS.md` 内容模板
- `TOOLS.md` 存在时能进入系统 prompt block。
- `TOOLS.md` 缺失/空文件不影响主流程。
- 安装命令可初始化该模板，且不覆盖用户已有内容。

### A.5 测试清单

- [ ] `cmd/mistermorph/install_test.go`：新增 `loadToolsTemplate` 相关测试。
- [ ] `internal/promptprofile/*_test.go`：验证 `TOOLS.md` 缺失/空/有效内容注入行为。

## 4) 工作流 B: 群聊发言规则增强

### B.1 设计意图

把“参与但不主导”的群聊行为转成明确规则，避免无价值回复和多次连发。

### B.2 任务拆解

- [ ] 提炼群聊规则并注入 Telegram prompt
  - 文件：`cmd/mistermorph/telegramcmd/command.go`
  - 注入点：`runTelegramTask(...)` 的群聊分支（现有 `isGroupChat(...)` 附近）。
  - 规则覆盖：
    - 仅在被点名/被提问/确有增量价值时发文本；
    - 无增量价值时优先 emoji reaction；
    - 同一条消息不多次碎片化回复（anti triple-tap）。
- [ ] humanlike 应该为 telegram 群聊下的默认策略
- [ ] 触发层与生成层职责分离
  - 触发层继续决定“是否进入 agent run”（现有 `groupTriggerDecision`）。
  - 生成层决定“文本回复 vs reaction”。
- [ ] 更新文档
  - `docs/prompt.md`：补充 Telegram 群聊行为规则来源。
  - `docs/feat/feat_20260205_telegram_reactions.md`：补充与 reaction 策略的关系。

### B.3 验收标准

- 群聊中“低价值消息”显著减少文本回复占比。
- 对被明确点名/提问的消息，回复率不下降。
- 不出现同一消息多次连续回复/反应的退化行为。

### B.4 测试清单

- [ ] `groupTriggerDecision` 相关单测补充（点名/回复/普通闲聊路径）。
- [ ] prompt rules 单测：群聊时规则存在，私聊时不注入群聊规则。
- [ ] reaction 相关回归：可 reaction 时不发送冗余文本。

## 5) 工作流 C: contacts 画像字段扩展

> 用户请求中的 `pronous` 统一按 `pronouns` 实现。

### C.1 设计意图

`contacts` 目前偏“互动评分与路由画像”，缺少部分“人类语境字段”。  
新增字段用于提升称呼礼貌、时区相关决策、长期偏好对齐：

- `pronouns`
- `timezone`
- `preference_context`（长文本偏好上下文）

### C.2 数据模型任务

- [ ] 扩展 `contacts.Contact` 结构
  - 文件：`contacts/types.go`
  - 新字段建议：
    - `Pronouns string \`json:"pronouns,omitempty"\``
    - `Timezone string \`json:"timezone,omitempty"\``
    - `PreferenceContext string \`json:"preference_context,omitempty"\``
- [ ] 规范化与约束
  - 文件：`contacts/file_store.go`
  - 在 `normalizeContact(...)` 增加 trim 与长度限制：
    - `pronouns`：短文本（建议 <=64 chars）
    - `timezone`：IANA 时区校验（非法值保留原值或置空，需明确策略）
    - `preference_context`：长文本（建议 <=2000 chars）

### C.3 字段填充场景（口径）

| 字段 | 主要填充入口 | 允许自动提取 | 覆盖策略 |
|---|---|---|---|
| `pronouns` | `contacts upsert`（CLI）/ 后续 `contacts_upsert` tool 明确写入 | 否（默认不猜测） | 仅在明确声明时覆盖 |
| `timezone` | `contacts upsert`（CLI）/ 后续 `contacts_upsert` tool 明确写入 | 否（默认不猜测） | 仅合法 IANA 时区可写入 |
| `preference_context` | `contacts upsert`（CLI 或 tool）+ 会话后画像提取流程 | 是（从会话与记忆提取） | 低频更新，保留人工覆盖优先级 |

说明：

- 自动观察入口（Telegram/MAEP contact observe）默认不写 `pronouns/timezone`，避免猜测性写入。
- `preference_context` 允许来自 LLM 摘要，但需要长度限制与隐私边界。

### C.4 CLI 与运维任务

- [ ] `contacts upsert` 增加参数
  - 文件：`cmd/mistermorph/contactscmd/contacts.go`
  - 新参数建议：
    - `--pronouns`
    - `--timezone`
    - `--preference-context`
    - 可选：`--preference-context-file`
- [ ] `contacts list` 输出优化
  - 默认简版保持不变，`--json` 返回完整字段即可。

### C.5 新增 `contacts_upsert` tool（agent 可调用）

- [ ] 新增 tool 实现
  - 文件：`tools/builtin/contacts_upsert.go`
  - 语义：单联系人 upsert，支持 partial patch（不传字段默认保留旧值）。
- [ ] 注册到默认工具表
  - 文件：`cmd/mistermorph/registry.go`
  - 在 `tools.contacts.enabled=true` 时注册。
- [ ] 参数 schema 设计
  - 必选建议：`contact_id`（或 `subject_id/node_id/peer_id` 至少一个）。
  - 可选字段：`kind/status/contact_nickname/persona_brief/persona_traits/pronouns/timezone/preference_context/topic_weights/...`。
- [ ] 日志安全摘要补充
  - 文件：`agent/engine_helpers.go`
  - 为 `contacts_upsert` 增加参数摘要，避免长文本原文落日志。
- [ ] 文档更新
  - 文件：`docs/tools.md`
  - 增加 `contacts_upsert` 用途、参数、约束与示例。

### C.6 Tool / Prompt 联动任务

- [x] `contacts_list` 文档更新
  - 文件：`docs/tools.md`
  - 明确返回字段包含 `pronouns` / `timezone` / `preference_context`。
- [x] LLM 特征提取输入补充上下文
  - 文件：`contacts/llm_features.go`
  - 将 `preference_context` 纳入输入，提升 topic/persona 提取准确性。
- [ ] 隐私边界
  - 公共聊天上下文默认不主动暴露长文本 `preference_context` 原文。
  - 仅在相关任务中按需最小披露。

### C.7 存储兼容性与迁移

- [ ] 兼容策略确认：使用 `omitempty` + JSON 解码天然前向兼容，默认无需批量迁移。
- [ ] 可选补偿脚本：仅在需要批量填充 timezone/pronouns 时提供。

### C.8 验收标准

- 新字段可通过 CLI 与 `contacts_upsert` tool 写入、通过 `contacts_list` 读取。
- `active.md` / `inactive.md` 旧数据可直接读取，无崩溃/丢字段。
- LLM 特征提取可消费新字段，且不影响旧流程。
- agent 可在会话中调用 `contacts_upsert` 完成联系人画像补录。

### C.9 测试清单

- [ ] `contacts` 存储 roundtrip 测试（含新字段）。
- [ ] `contacts upsert` 参数解析与写入测试。
- [ ] `tools/builtin/contacts_upsert.go` 单测（创建、更新、partial patch、非法参数）。
- [ ] `contacts_list` 输出 JSON 字段回归测试。
- [ ] `llm_features` payload 组装测试。

## 6) 推荐实施顺序

建议按低风险到高影响顺序分 4 个 PR：

1. PR-1: `TOOLS.md` 模板 + 安装 + prompt block 注入  
2. PR-2: 群聊规则注入 + 配置开关 + 回归测试  
3. PR-3: contacts 字段扩展 + CLI + extractor 联动
4. PR-4: `contacts_upsert` tool + registry + docs + tests

## 7) 风险与回滚

风险：

- `TOOLS.md` 注入过长导致 token 消耗上升
- 群聊规则过严导致漏回
- contacts 新字段泄露边界处理不当

回滚策略：

- `TOOLS.md` 注入可通过开关禁用
- 群聊策略可退回 `strict/smart` 旧路径
- contacts 字段为可选字段，回滚可仅停止写入新字段

## 8) 开放问题（实现前确认）

- [ ] `TOOLS.md` 与 `IDENTITY/SOUL` 的统一路径基准最终选哪一个？
- [ ] `timezone` 非法值策略：拒绝写入 vs 容忍写入并打告警？
- [ ] `preference_context` 是否需要单独“摘要字段”供公共场景安全注入？

## 9) 执行 TODO 明细（按 PR 顺序）

### PR-1: `TOOLS.md` 模板 + 安装 + Prompt 注入

- [ ] 新增文件 `assets/config/TOOLS.md`，写入本地环境笔记模板（含示例与边界说明）。
- [ ] 更新 `cmd/mistermorph/install.go`：
  - [ ] 增加 `loadToolsTemplate()`。
  - [ ] 在 `filePlans` 中增加 `TOOLS.md`。
  - [ ] 保持“已存在即跳过”行为与现有模板一致。
- [ ] 更新 `cmd/mistermorph/install_test.go`：
  - [ ] 增加 `loadToolsTemplate()` 返回非空与标题校验。
- [ ] 更新 prompt 组装逻辑（建议新建 `internal/promptprofile/context.go`）：
  - [ ] 读取 `TOOLS.md`（与 `IDENTITY/SOUL` 同路径基准）。
  - [ ] 当文件非空时注入 `PromptBlock{Title: "Local Tool Notes"}`。
  - [ ] 增加注入内容长度上限（避免 prompt 膨胀）。
- [ ] 更新 `docs/prompt.md`：
  - [ ] 增加 `TOOLS.md` 注入来源与时机说明。
- [ ] 验证：
  - [ ] `go test ./cmd/mistermorph/... ./internal/promptprofile/...`

### PR-2: 群聊规则增强（触发层/生成层分离）

- [ ] 明确触发层职责（只决定是否 run）：
  - [ ] 在 `cmd/mistermorph/telegramcmd/command.go` 注释/文档化 `groupTriggerDecision(...)`。
  - [ ] 禁止触发层承载“文本还是 reaction”的策略。
- [ ] 明确生成层职责（只决定响应形式）：
  - [ ] 在 `runTelegramTask(...)` 增加群聊规则 prompt 条款：
    - [ ] 仅在有增量价值时发文本。
    - [ ] 无增量价值优先 reaction。
    - [ ] 禁止同消息多次碎片化回复（anti triple-tap）。
- [ ] 增加配置项：
  - [ ] `telegram.group.reply_policy`（建议：`strict|humanlike`）。
  - [ ] 默认设为 `humanlike`（群聊）。
- [ ] 更新文档：
  - [ ] `docs/prompt.md` 补充群聊行为规则注入点。
  - [ ] `docs/feat/feat_20260205_telegram_reactions.md` 补充与 reaction 的职责边界。
- [ ] 测试：
  - [ ] 补充 `groupTriggerDecision` 触发路径测试。
  - [ ] 补充“群聊规则只在群聊注入”的测试。
  - [ ] 回归 reaction 场景，确保不发送冗余文本。
- [ ] 验证：
  - [ ] `go test ./cmd/mistermorph/telegramcmd/...`

### PR-3: `contacts` 字段扩展（`pronouns/timezone/preference_context`）

- [x] 扩展模型 `contacts/types.go`：
  - [x] 增加 `Pronouns` 字段。
  - [x] 增加 `Timezone` 字段。
  - [x] 增加 `PreferenceContext` 字段。
- [x] 更新 `contacts/file_store.go`：
  - [x] 在 `normalizeContact(...)` 中新增字段 trim。
  - [x] 对 `preference_context` 增加长度上限。
  - [x] 对 `timezone` 增加合法性校验策略（按开放问题结论实施）。
- [x] 更新 CLI `cmd/mistermorph/contactscmd/contacts.go`：
  - [x] `contacts upsert` 增加 `--pronouns`。
  - [x] `contacts upsert` 增加 `--timezone`。
  - [x] `contacts upsert` 增加 `--preference-context`。
  - [ ] 可选：增加 `--preference-context-file`。
- [x] 更新 `contacts/llm_features.go`：
  - [x] 在提取输入 payload 中加入 `preference_context`。
  - [x] 保持输出契约不破坏兼容。
- [x] 更新文档：
  - [x] `docs/tools.md` 更新 `contacts_list` 返回字段说明。
- [ ] 测试：
  - [ ] `contacts` 存储 roundtrip（新字段读写）。
  - [ ] CLI 参数解析与写入测试。
  - [ ] `llm_features` payload 组装测试。
- [x] 验证：
  - [x] `go test ./contacts/... ./cmd/mistermorph/contactscmd/...`

### PR-4: 新增 `contacts_upsert` 内置 Tool

- [x] 新增 `tools/builtin/contacts_upsert.go`：
  - [x] 定义工具名、描述、参数 schema。
  - [x] 实现“partial patch”语义（未提供字段保留旧值）。
  - [x] 至少支持 `contact_id|subject_id` 的最小识别策略。
- [x] 更新 `cmd/mistermorph/registry.go`：
  - [x] 在 `tools.contacts.enabled=true` 时注册 `contacts_upsert`。
- [x] 更新 `agent/engine_helpers.go`：
  - [x] 为 `contacts_upsert` 增加安全日志摘要（不输出长文本全文）。
- [x] 更新文档：
  - [x] `docs/tools.md` 增加 `contacts_upsert` 章节（用途、参数、约束）。
- [x] 测试：
  - [x] 新增 `tools/builtin/contacts_upsert_test.go`。
  - [x] 场景覆盖：创建、更新、partial patch、非法 kind/status、缺失标识字段。
  - [x] 更新 `agent/engine_helpers_test.go`，覆盖新工具参数摘要。
- [x] 验证：
  - [x] `go test ./tools/builtin/... ./agent/...`

### 收尾（合并前统一检查）

- [x] 全量单测：`go test ./...`
- [x] 静态检查：`go vet ./...`
- [ ] 文档自检：
  - [ ] `docs/tools.md` 与实际注册工具一致。
  - [ ] `docs/prompt.md` 与实际注入路径一致。
  - [ ] 本文档里所有 TODO 的文件路径可直接定位到仓库。
