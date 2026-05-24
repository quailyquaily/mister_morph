---
date: 2026-05-22
title: LLM 上下文窗口元数据
status: in_progress
---

# LLM 上下文窗口元数据

## 1) 目标

给每个 topic 提供可读、可查、可展示的上下文窗口状态：

1. LLM 配置里允许填写模型上下文窗口大小。
2. 配置不填写时，用归一化后的 model name 查询内置窗口表。
3. 每次主循环 LLM 请求完成后，用 response usage 更新当前 topic 的窗口占用。
4. Telegram、Slack、Lark、LINE、Console 支持 `/ctx`，显示当前 topic 的窗口占比。
5. Daemon API 提供 topic metadata，包含 workspace 和上下文窗口状态，Console Chat 侧栏用一个 API 读取。

不做这些事：

1. 不在运行时抓取供应商文档。
2. 不引入 tokenizer 估算作为 V1 的默认路径。
3. 不把所有 usage 统计改成 topic 维度报表。
4. 不改变现有 `/model` 和 `/workspace` 命令语义。
5. 不把历史请求 token 消费累计值当成上下文窗口占用。

## 2) 定义

上下文窗口上限：

模型单次请求可容纳的 token 数。来源是显式配置或内置模型表。

当前窗口占用：

当前 topic 最近一次 agent 主循环 LLM 请求的 `input_tokens`。这是 prompt、历史消息、工具结果、memory、workspace 提示等进入模型输入后的实际 token 数。

缓存输入：

`cached_input_tokens` 是 `input_tokens` 的子集。它仍然占用上下文窗口，只是计费或延迟语义不同。窗口占比不能把 `cached_input_tokens` 再加到 `input_tokens` 上。

窗口占比：

```text
used_input_tokens / context_window_tokens
```

如果窗口上限未知，显示 token 数，不显示百分比。

## 3) 事实来源

窗口上限的事实来源：

1. 当前生效 LLM profile 的 `context_window_tokens`。
2. 顶层 `llm.context_window_tokens`。
3. 内置 model context window catalog。
4. 未命中时为 unknown。

窗口占用的事实来源：

1. `llm.Result.Usage.InputTokens`。
2. `llm.Result.Usage.Cache.CachedInputTokens` 只作为 breakdown 展示。
3. 不从 stream delta 累加。
4. 不从历史 message 字符数估算。

消费统计和窗口占用必须分开：

1. 消费统计可以累计所有请求。
2. 窗口占用只能取最近一次主循环请求。
3. 将同一个 topic 的所有 request `input_tokens` 或 `total_tokens` 相加会重复计算历史上下文，不能用于窗口占比。

## 4) 配置

新增配置字段：

```yaml
llm:
  model: "gpt-5.5"
  context_window_tokens: 0
  profiles:
    backup:
      model: "gpt-5.4"
      context_window_tokens: 400000
```

规则：

1. `0`、空值、未配置都表示 unset。
2. profile 字段优先于顶层字段。
3. 显式配置优先于内置 model window catalog。
4. 小于 0 的值非法。
5. 大于 0 的值必须按整数 token 处理。

需要改动的配置路径：

1. `llm.context_window_tokens`
2. `llm.profiles.<name>.context_window_tokens`

Console Settings 需要读写该字段：

1. 顶层 LLM 设置显示 Context Window。
2. 每个 profile 显示 Context Window。
3. 输入为空保存为 unset，不写 `0`。
4. 外部 endpoint 仍通过 runtime proxy 读取和保存。

## 5) 内置 Model Context Window Catalog

新增一个 YAML catalog，只保存我们明确知道的常用 online 推理模型窗口。

建议文件：

```text
llm/model_context_windows.yaml
```

示例：

```yaml
version: 1
models:
  - model: gpt-5.5
    context_window_tokens: 1050000
    provider: openai
    aliases:
      - openai/gpt-5.5
    sources:
      - url: https://developers.openai.com/api/docs/models/gpt-5.5
        checked_at: "2026-05-22"
```

字段规则：

1. `model` 是归一化后的主键。
2. `context_window_tokens` 是单次请求上下文窗口 token 数。
3. `provider` 只用于维护和显示，不参与权限判断。
4. `aliases` 保存明确等价的模型名。
5. `sources[].url` 必须指向 provider 官方文档或官方模型列表。
6. `sources[].checked_at` 使用 `YYYY-MM-DD`。

归一化规则：

1. trim 空白。
2. 转小写。
3. 去掉常见 provider/path 前缀，例如 `openai/`、`anthropic/`、`google/`、`xai/`、`moonshot/`、`vendor/models/`。
4. 先匹配完整 snapshot 名。
5. 再把可识别的 dated snapshot 映射到 alias，例如 `gpt-5.5-2026-04-23` 映射到 `gpt-5.5`。
6. 不做模糊猜测。没有明确规则就返回 unknown。

实现建议：

1. 放在 `llm` 包内，和现有 model capability 判断放在同一层。
2. 用 `go:embed` 读取 `model_context_windows.yaml`。
3. 暴露一个有实际语义的函数，例如 `ResolveModelContextWindow(model string)`.
4. 返回值包含 `context_window_tokens`、`normalized_model`、`provider`、`sources`、`ok`。
5. 添加 table-driven tests，覆盖 provider prefix、snapshot、unknown model。

不要写只改名的薄包装。

维护规则：

1. 内置窗口表跟随 uniai pricing catalog 的更新节奏检查。
2. 更新 `github.com/quailyquaily/uniai` 版本时，必须同时检查新增或变更的 online 推理模型是否需要补窗口大小。
3. pricing catalog 只提供价格，不作为窗口大小的直接来源；它是维护提醒。
4. 如果 uniai 增加新模型价格但我们没有可靠窗口来源，窗口表不要猜测，保持 unknown。
5. 版本更新 PR 或 commit 需要说明是否检查过窗口表。
6. 补窗口大小时必须在 YAML 里写 provider 官方 URL，方便后续复查。

## 6) Usage 捕获

当前已有 `llmstats.WrapRuntimeClient` 会在 LLM 请求成功后记录 usage。V1 复用这个捕获点，不再新增一层只为了观察 response 的 client wrapper。

需要给 usage 记录路径补充一次 topic context observe 调用，不再新增一层 client wrapper。

sample 至少包含：

```json
{
  "run_id": "task id",
  "conversation_key": "runtime conversation key",
  "topic_id": "console topic id when available",
  "scene": "console.loop",
  "provider": "openai",
  "api_base": "https://api.example.invalid",
  "model": "gpt-5.5",
  "normalized_model": "gpt-5.5",
  "context_window_tokens": 1050000,
  "context_window_source": "builtin",
  "input_tokens": 123456,
  "cached_input_tokens": 23456,
  "cache_creation_input_tokens": 1000,
  "updated_at": "2026-05-22T00:00:00Z"
}
```

过滤规则：

1. 只记录 agent 主循环请求。
2. Console scene 是 `console.loop`。
3. Telegram、Slack、Lark、LINE 主循环 scene 需要统一命名，建议使用 `<runtime>.loop`。
4. addressing、topic title、memory draft、settings benchmark、image generation 不更新 topic window。
5. 失败请求没有可靠 usage，不更新。
6. usage 为 0 时不覆盖已有样本。

## 7) Topic Context Store

新增 topic context store，用 conversation key 做主键：

```json
{
  "version": 1,
  "items": {
    "console:<topic_id>": {
      "conversation_key": "console:<topic_id>",
      "topic_id": "<topic_id>",
      "runtime": "console",
      "model": "gpt-5.5",
      "normalized_model": "gpt-5.5",
      "context_window_tokens": 1050000,
      "context_window_source": "builtin",
      "used_input_tokens": 123456,
      "cached_input_tokens": 23456,
      "cache_creation_input_tokens": 1000,
      "usage_ratio": 0.117577,
      "last_run_id": "<task_id>",
      "last_origin_event_id": "<origin_event_id>",
      "updated_at": "2026-05-22T00:00:00Z"
    }
  }
}
```

设计约束：

1. store 放在 `file_state_dir` 下。
2. 写入使用现有 `fsstore` atomic write 和 lock 模式。
3. key 用 `conversation_key`，因为 Telegram topic、Slack thread、Lark chat、LINE chat 的会话身份都不是单纯的 console `topic_id`。
4. API 可以接受 `topic_id`，但内部要转换为 `conversation_key`。
5. workspace attachment 继续由 workspace store 保存；metadata API 只聚合读取，不把两个存储合并成一个文件。

## 8) `/ctx` 命令

新增 slash command：

```text
/ctx
```

返回内容示例：

```text
Context
model: gpt-5.5
window: 1,050,000 tokens (builtin)
used: 123,456 input tokens
cached: 23,456 input tokens
ratio: 11.8%
updated: 2026-05-22 00:00 UTC
```

窗口未知时：

```text
Context
model: unknown
window: unknown
used: 123,456 input tokens
cached: 23,456 input tokens
```

行为规则：

1. `/ctx` 不调用 LLM。
2. `/ctx` 不写入 agent prompt history。
3. 没有样本时显示 `No context usage recorded for this topic yet.`。
4. runtime 需要用当前 inbound message 的 conversation key 查询。
5. Telegram group topic 要使用带 thread 的 conversation key。
6. Slack thread、Lark chat、LINE chat 使用各自已有 conversation key。

实现建议：

1. 在 `internal/chatcommands` 增加 context command handler。
2. Slack、Lark、LINE 通过 `NewRuntimeRegistry` 注册。
3. Telegram 现在有专门 switch，先在 switch 里加 `/ctx`，不要为了一个命令重构整个 Telegram command flow。
4. Console Local 的 synthetic command 路径也支持 `/ctx`，方便 web chat 中直接输入。

## 9) Topic Metadata API

新增读 API：

```http
GET /topic/:topic_id/metadata
```

response：

```json
{
  "topic_id": "<topic_id>",
  "conversation_key": "console:<topic_id>",
  "workspace": {
    "workspace_dir": ""
  },
  "context": {
    "available": true,
    "model": "gpt-5.5",
    "normalized_model": "gpt-5.5",
    "context_window_tokens": 1050000,
    "context_window_source": "builtin",
    "used_input_tokens": 123456,
    "cached_input_tokens": 23456,
    "cache_creation_input_tokens": 1000,
    "usage_ratio": 0.117577,
    "last_run_id": "<task_id>",
    "last_origin_event_id": "<origin_event_id>",
    "updated_at": "2026-05-22T00:00:00Z"
  }
}
```

错误规则：

1. 路径缺少 topic id 返回 400。
2. metadata store 不可用返回 503。
3. 没有上下文样本时，`context.available=false`，HTTP 仍为 200。
4. 没有 workspace 时，`workspace.workspace_dir=""`。

和现有 `/workspace` 的关系：

1. Console Chat 读侧栏信息改用 `/topic/:topic_id/metadata`。
2. `GET /workspace?topic_id=...` 保留兼容。
3. `PUT /workspace` 和 `DELETE /workspace` 保留为 workspace 写接口。
4. V1 不迁移 workspace 写接口，避免扩大改动面。

## 10) Console UI

Chat 侧栏增加 Context 信息：

1. 显示 model。
2. 显示窗口上限和来源。
3. 显示 used input tokens。
4. 显示百分比。
5. 如 provider 返回缓存明细，显示 cached input tokens。
6. unknown 时隐藏进度条，只显示 token。

请求策略：

1. 进入 topic 时请求一次 `/topic/:topic_id/metadata`。
2. task 完成后刷新一次 metadata。
3. 切换 workspace 后刷新一次 metadata。
4. 不再为 Chat 侧栏单独请求 `GET /workspace?topic_id=...`。

## 11) 验收标准

配置：

1. `llm.context_window_tokens: 100000` 时，任意未配置 profile 默认窗口为 100000。
2. profile 配置 `context_window_tokens: 200000` 时，profile 优先。
3. 空值保存后 YAML 不写 `context_window_tokens: 0`。
4. 负数保存失败，错误信息包含字段名。

内置 catalog：

1. `gpt-5.5` 返回 1050000。
2. `openai/gpt-5.5` 返回 1050000。
3. `vendor/models/gpt-5.5-2026-04-23` 返回 1050000。
4. `gpt-5.5` 的 catalog item 包含 provider 官方 URL。
5. `unknown-model` 返回 unknown。

usage 捕获：

1. 一次 `console.loop` 返回 `input_tokens=1000`、`cached_input_tokens=300` 后，store 中 used 为 1000，cached 为 300。
2. 两次请求分别为 1000 和 1300 后，store used 为 1300，不是 2300。
3. addressing 请求不会更新 topic context。
4. usage 全 0 不覆盖已有样本。

`/ctx`：

1. Telegram、Slack、Lark、LINE、Console 都能返回当前 topic 的 context。
2. `/ctx` 不触发 LLM usage record。
3. `/ctx` 不追加到 agent prompt history。
4. 没有样本时返回明确提示。

API：

1. `GET /topic/:topic_id/metadata` 返回 workspace 和 context。
2. 没有 workspace 时仍返回 200。
3. 没有 context 样本时 `context.available=false`。
4. Console Chat 进入 topic 只需要一个 metadata 读请求拿到 workspace 和 context。

UI：

1. 有窗口上限时显示百分比。
2. 窗口未知时不显示错误态。
3. task 完成后侧栏 context 数字更新。
4. `pnpm build` 通过。

## 12) 实施任务

### Task 1: 配置字段

- [x] 在 runtime values 和 profile config 增加 `context_window_tokens`。
- [x] 在 config example 增加注释。
- [x] 在 Console Settings payload、读写、测试里支持该字段。

验收：

- [x] 顶层和 profile 都能读写。
- [x] 空值保存为 unset。
- [x] 负数被拒绝。

### Task 2: 内置 model window catalog

- [x] 新增 `llm/model_context_windows.yaml`。
- [x] 实现 model name 归一化。
- [x] 实现内置窗口查询。
- [x] 添加 table-driven tests。
- [ ] 在 uniai 版本更新流程里加入窗口表检查。

验收：

- [x] `gpt-5.5`、provider prefix、snapshot 三类测试通过。
- [x] catalog item 至少包含一个 provider 官方 URL。
- [x] unknown model 不猜测。
- [ ] 更新 uniai 依赖时，commit 或 PR 描述说明窗口表检查结果。

### Task 3: usage 到 topic context sample

- [x] 给主循环请求 context 补充 conversation key、topic id、runtime。
- [x] 在现有 usage 捕获路径追加 observe 调用。
- [x] 只让主循环 scene 更新 topic context。

验收：

- [x] 连续请求取最新 input tokens。
- [x] 辅助 LLM 请求不更新。
- [x] 失败请求不覆盖。

### Task 4: topic context store

- [x] 新增 store。
- [x] 使用 atomic write 和 lock。
- [x] 提供 get/update 方法。

验收：

- [x] 并发写测试通过。
- [x] 空 store 查询返回 unavailable。

### Task 5: `/ctx`

- [x] 增加 shared command handler。
- [x] 接入 Slack、Lark、LINE registry。
- [x] 接入 Telegram switch。
- [x] 接入 Console Local synthetic command。

验收：

- [ ] 五个入口输出一致格式。
- [x] `/ctx` 不调用 LLM。

### Task 6: topic metadata API

- [x] 增加 `GET /topic/:topic_id/metadata`。
- [x] 聚合 workspace store 和 topic context store。
- [x] 保留现有 `/workspace` 兼容读写。

验收：

- [x] API tests 覆盖有 workspace、有 context、两者都没有。
- [x] 400 和 503 行为明确。

### Task 7: Console Chat 侧栏

- [x] Chat 侧栏改读 metadata API。
- [x] 增加 context 展示。
- [x] task 完成后刷新 metadata。

验收：

- [x] 进入 topic 不再单独请求 `GET /workspace?topic_id=...`。
- [x] context 数字在 task 完成后更新。
- [x] `pnpm build` 通过。

## 13) 风险

1. 部分 provider 不返回 usage。处理方式：显示 unknown 或旧样本，不估算。
2. 同一 topic 切换 model 后，旧样本可能来自旧 model。处理方式：样本里保存 model 和 updated_at，下一次主循环请求后更新。
3. fallback route 可能实际使用 fallback model。处理方式：以成功请求记录的 model 为准。
4. `cached_input_tokens` 仍占用上下文窗口。V1 只把它作为 `input_tokens` 的 breakdown 展示，不参与额外加法。
