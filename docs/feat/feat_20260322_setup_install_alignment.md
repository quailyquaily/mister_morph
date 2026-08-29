---
date: 2026-03-22
title: Console Setup 与 CLI Install 对齐方案
status: superseded
---

# Console Setup 与 CLI Install 对齐方案

> 注：本文的安装产物列表已被 2026-05-26 的 Awareness 调度清理方案修正。`SCRIPTS.md` 不再创建，周期任务文件为 `cron.yaml`。

## 1) 结论

`mistermorph install` 和 Console Web `/setup` 本质上是同一个 onboarding 流程的两个入口。

这次对齐的目标不是保留两套不同语义，而是把它们收敛成同一份 contract：

- 同样的输入范围
- 同样的 provider 集合
- 同样的完成判断
- 同样的初始化产物

唯一保留的交互差异：

- CLI `install` 在编辑自定义 `SOUL.md` 时调用系统编辑器
- Console `/setup` 在浏览器内编辑 `SOUL.md`

---

## 2) 统一后的范围

两条流程都只负责两类东西：

1. LLM settings
2. 标准 markdown 文件集合

标准 markdown 文件集合收敛为：

- `HEARTBEAT.md`
- `cron.yaml`
- `IDENTITY.md`
- `SOUL.md`

明确不再属于这条流程的内容：

- built-in skills 安装
- `contacts/ACTIVE.md`
- `contacts/INACTIVE.md`
- `memory/index.md`
- Telegram/Slack/Console host 的首次配置

也就是说，`install` 不再是“工作区全量 bootstrap”，而是“首次可用 onboarding”。

---

## 3) 已确认的前提

### 3.1 Contacts 可以按需初始化

确认成立。

- `contacts.FileStore.Ensure()` 会创建 `contacts` 目录
- `loadContactsMarkdownLocked()` 在 `ACTIVE.md` / `INACTIVE.md` 不存在时返回空列表，不报错
- `contacts.Service.UpsertContact()` / `SendDecision()` 会先调用 `Ensure()`

涉及代码：

- `contacts/file_store.go`
- `contacts/service.go`

结论：

> install/setup 不需要预创建 contacts 文件；在真正首次使用 contacts 时再创建即可。

### 3.2 Memory 可以按需初始化

确认成立。

- `memory.NewManager(statepaths.MemoryDir(), ...)` 不要求目录预存在
- journal / update 路径在写入时会自行 `MkdirAll`
- console memory API 在目录不存在时把它当成空 memory，而不是报错

涉及代码：

- `memory/manager.go`
- `memory/journal.go`
- `memory/update.go`
- `internal/channelruntime/core/memory.go`
- `internal/daemonruntime/server.go`

结论：

> install/setup 不需要预创建 `memory/index.md`；在 memory 真正开始落盘时再创建即可。

---

## 4) 目标模型

## 4.1 Shared Setup Contract

`install` 和 `/setup` 共用同一套 stage：

1. `llm`
2. `persona`
3. `soul`
4. `ready`

其中：

- `llm` 负责让当前实例达到 `can_submit`
- `persona` 负责确认 `IDENTITY.md`
- `soul` 负责确认 `SOUL.md`
- `ready` 表示当前实例已经能正常进入聊天

`HEARTBEAT.md` / `cron.yaml` 不单独占 stage，但属于 required seed files。

## 4.2 Ready 语义

两条流程统一遵守当前 setup 的 ready 语义，不引入内容感知判定。

也就是：

- LLM 已配置到当前实例可 submit
- `IDENTITY.md` 存在
- `SOUL.md` 存在

则进入 `ready`。

明确不属于 ready gate：

- Telegram 已配置
- Slack 已配置
- contacts 已初始化
- memory 已初始化

## 4.3 缺失文件自动补模板

两条流程都新增同一条规则：

> 如果 required markdown 文件不存在，就先用模板补上。

这条规则适用于：

- `install`
- Console `/setup`

推荐做法：

- 在进入 evaluator 前先执行一次 `ensureRequiredMarkdownFiles()`
- 缺失则从 `assets/config/` 写入模板
- 已存在则不覆盖

这样可以保证：

- 两端永远看到同一组最小文件面
- Web 编辑器不会因为文件缺失而出现 404
- CLI install 结束后文件集完整

---

## 5) Provider 对齐

两端 UI/CLI 只暴露 4 类 provider：

1. `openai-compatible`
2. `gemini`
3. `anthropic`
4. `cloudflare`

其中：

- `xai`
- `deepseek`
- 其他 OpenAI 兼容网关

都收敛到 `openai-compatible` 这一路径，不再在 setup/install UI 中单独列出。

实现层建议：

- 抽一个共享 provider registry，供 CLI wizard 和 Web setup 共用
- UI label 使用 `openai-compatible`
- 持久化时可按 endpoint 归一化到现有 provider 字段
  - 官方 OpenAI base: `openai`
  - 其他 OpenAI Chat 兼容 endpoint: `openai`
- `gemini` / `anthropic` / `cloudflare` 保持显式 provider

这能同时满足两点：

- 产品层面 provider surface 收敛
- 后端兼容现有 provider 解析逻辑

---

## 6) CLI Install 调整

CLI `install` 调整后应当做这些事：

- 采集 LLM settings
- 确保标准 markdown 文件集合存在
- 走与 Web setup 相同的 `llm -> persona -> soul -> ready` 语义

CLI `install` 不再强制要求：

- Telegram bot token
- Telegram group trigger mode

CLI `install` 也不再负责：

- 安装 built-in skills
- 初始化 contacts
- 初始化 memory

### CLI 唯一保留的差异

在 `soul` 阶段，如果用户选择自定义 `SOUL.md`：

- 调用系统编辑器打开 `SOUL.md`
- 用户保存并退出后，该步骤即完成

如果用户选择 preset/template，则按普通模板写入路径完成。

---

## 7) Console Setup 调整

Console `/setup` 调整后应当做这些事：

- 使用与 CLI 相同的 provider registry
- 使用与 CLI 相同的 required markdown registry
- 在判定 stage 前自动补齐缺失模板
- 继续使用当前的 ready 语义

Console `/setup` 不需要新增这些步骤：

- Telegram 配置
- Slack 配置
- contacts/memory 初始化
- skills 安装

---

## 8) 推荐实现拆分

### Phase 1: 抽共享注册表

- 抽 shared provider registry
- 抽 shared required markdown registry
- 抽 `ensureRequiredMarkdownFiles()`

### Phase 2: 收缩 CLI install

- 删除 Telegram required prompts
- 删除 built-in skills install
- 删除 contacts/memory 模板写入
- install 仅写 `config.yaml` 与标准 markdown 文件集合

### Phase 3: 对齐 Web setup

- setup provider 列表改接 shared registry
- stage 前执行 markdown ensure
- ready 判定继续走当前 setup 语义

### Phase 4: 统一 SOUL 交互

- Web 保持内置编辑器
- CLI 自定义模式接系统编辑器
- 二者写回同一个 `SOUL.md`

---

## 9) 验收标准

- 新工作区执行 `mistermorph install` 后，会得到：
  - `config.yaml`
  - `HEARTBEAT.md`
  - `cron.yaml`
  - `IDENTITY.md`
  - `SOUL.md`
- 新工作区不会再强制创建：
  - `contacts/ACTIVE.md`
  - `contacts/INACTIVE.md`
  - `memory/index.md`
  - `skills/*`
- CLI install 不再要求 Telegram
- Web setup 与 CLI install 暴露相同的 provider 列表
- Web setup 与 CLI install 对同一工作区给出相同 stage 结论
- 缺失 required markdown 时，两边都会自动补模板
- CLI 的 custom `SOUL.md` 通过系统编辑器完成

---

## 10) 冻结建议

本次建议冻结为以下方向：

1. `install` 与 `/setup` 视为同一个 onboarding flow 的两个入口
2. 统一范围为 `llm settings + 标准 markdown 文件集合`
3. 不再把 Telegram 作为 install/setup 的 required gate
4. provider surface 收敛为 `openai-compatible / gemini / anthropic / cloudflare`
5. completion 继续沿用当前 setup 的存在性语义
6. 两端都在缺文件时自动补模板
7. contacts 与 memory 保持按需初始化，不再提前创建
