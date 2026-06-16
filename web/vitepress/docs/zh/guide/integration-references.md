---
title: Integration API
description: 列出 integration 包的导出函数、方法、结构体字段，以及它们的参数、返回值和用途。
---

# Integration API

这页专门列出 `github.com/quailyquaily/mistermorph/integration` 的导出 API。

如果你主要想看怎么配置 `integration.Config`、怎么使用 PreparedRun、怎么接 Telegram / Slack，先看 [创建自己的 AI Agent：进阶](/zh/guide/build-your-own-agent-advanced)。

## 顶层函数

### `ApplyViperDefaults(v *viper.Viper)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `v *viper.Viper`：要写入默认值的 viper 实例；传 `nil` 时会退回全局 `viper.GetViper()` |
| 返回值 | 无 |
| 说明 | 把 integration 与第一方 runtime 共用的默认配置写进 viper。只有当你的宿主程序自己也基于 viper 组织配置时才需要它。 |

### `DefaultFeatures() Features`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `integration.Features` |
| 说明 | 返回默认 feature 开关；当前 `PlanTool`、`Guard`、`Skills` 都是开启状态。 |

### `DefaultConfig() Config`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `integration.Config` |
| 说明 | 返回默认配置。`Overrides` 为空 map，`Features` 使用 `DefaultFeatures()`，`Inspect` 为空值。 |

### `New(cfg Config) *Runtime`

| 项目 | 内容 |
| --- | --- |
| 参数 | `cfg integration.Config`：宿主程序组装好的显式配置 |
| 返回值 | `*integration.Runtime` |
| 说明 | 构造一个可复用的 runtime，并在构造时把默认值与覆盖项快照化。 |

## 配置相关类型

### `type Features struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `PlanTool` | `bool` | 是否注册 `plan_create` 相关运行时辅助工具。 |
| `Guard` | `bool` | 是否在 runtime 中接入 guard。 |
| `Skills` | `bool` | 是否在 prompt 构造阶段启用 skills 加载。 |

### `type InspectOptions struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Prompt` | `bool` | 是否落盘 prompt。 |
| `Request` | `bool` | 是否落盘 request / response。 |
| `DumpDir` | `string` | 落盘目录。 |
| `Mode` | `string` | inspect 模式名，用于区分输出。 |
| `TimestampFormat` | `string` | 文件命名里的时间格式。 |

### `type Config struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Overrides` | `map[string]any` | 最终覆盖项，按 Viper key 写入，优先级最高。 |
| `Features` | `integration.Features` | 控制哪些运行时能力会被接入。 |
| `PromptBlocks` | `[]string` | 追加到 system prompt `Additional Policies` 的静态 block。 |
| `BuiltinToolNames` | `[]string` | 内置工具白名单；留空表示全部接入。 |
| `Inspect` | `integration.InspectOptions` | prompt / request 落盘调试相关选项。 |

### `(*Config).Set(key string, value any)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `key string`：Viper 配置键；`value any`：要覆盖的值 |
| 返回值 | 无 |
| 说明 | 向 `Overrides` 中写入单个覆盖项。空 key 会被忽略。 |

### `(*Config).AddPromptBlock(content string)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `content string`：要追加的 prompt block 文本 |
| 返回值 | 无 |
| 说明 | 追加一个静态 prompt block。空白字符串会被忽略，内容会按顺序应用到 runtime。 |

## Runtime 与执行

### `type Runtime struct`

`Runtime` 是第三方宿主复用 `integration` 的主入口。它本身不暴露字段，主要通过方法工作。

### `type PreparedRun struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Engine` | `*agent.Engine` | 已经准备好的可运行引擎。 |
| `Model` | `string` | 当前主路由解析出来的模型名。 |
| `Cleanup` | `func() error` | 释放 inspect / MCP 等临时资源。 |

### `type RunTaskOptions struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Agent` | `agent.RunOptions` | 传给 `Engine.Run` 的 agent 运行参数。 |
| `TaskID` | `string` | 可选的持久化 task id。留空时先读 `Agent.Meta["task_id"]`，再自动生成。 |
| `TopicID` | `string` | 可选 topic id。留空时先读 `Agent.Meta["topic_id"]`，再保持为空。 |
| `TraceID` | `string` | 可选外部 trace / correlation id。留空时先读 `Agent.Meta["trace_id"]`，再保持为空。 |
| `PersistTask` | `bool` | 把 one-shot 生命周期事件 queued/running/done/failed 写入 task journal。 |

### `type RunTaskResult struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Final` | `*agent.Final` | agent 最终输出。 |
| `Context` | `*agent.Context` | agent 运行上下文。 |
| `TaskID` | `string` | 本次 one-shot run 使用的 task id。 |
| `RunID` | `string` | 日志和 LLM stats 使用的 run id，默认等于 `TaskID`。 |
| `TopicID` | `string` | 显式提供时，写入 metadata 和 task 持久化的 topic id。 |
| `TraceID` | `string` | 显式提供时使用的外部 trace id。 |

### `type LLMProfile struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Name` | `string` | profile 名称。 |
| `InferenceProvider` | `string` | 已知时，为面向用户的推理供应商。 |
| `Provider` | `string` | 解析后的 provider 名。 |
| `ModelName` | `string` | 解析后的 model 名。 |
| `APIBase` | `string` | 若存在则为解析后的 API base。 |

### `type LLMProfileCandidate struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `LLMProfile` | `integration.LLMProfile` | 候选 profile 信息。 |
| `Weight` | `int` | `llm.routes.main_loop.candidates` 里的权重。 |

### `type LLMProfileSelection struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Mode` | `string` | `auto` 或 `manual`。 |
| `ManualProfile` | `string` | 当 `Mode` 为 `manual` 时的手动覆盖目标。 |
| `RouteType` | `string` | `profile` 或 `candidates`。 |
| `Current` | `*integration.LLMProfile` | 当策略是单一 profile 时，表示当前解析出的 profile。 |
| `Candidates` | `[]integration.LLMProfileCandidate` | 当策略是 `candidates` 时，列出加权候选项。 |
| `FallbackProfiles` | `[]integration.LLMProfile` | 路由解析出的 fallback profiles。 |

### `(*Runtime).NewRegistry() *tools.Registry`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `*tools.Registry` |
| 说明 | 基于当前 runtime 快照构建默认 registry。要加自定义工具时，通常从这里开始。 |

### `(*Runtime).NewRunEngine(ctx context.Context, task string) (*PreparedRun, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `ctx context.Context`：准备阶段上下文；`task string`：当前任务文本 |
| 返回值 | `*integration.PreparedRun`、`error` |
| 说明 | 用默认 registry 准备一个可复用的引擎。 |

### `(*Runtime).NewRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry) (*PreparedRun, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `ctx context.Context`：准备阶段上下文；`task string`：当前任务文本；`baseReg *tools.Registry`：基础 registry |
| 返回值 | `*integration.PreparedRun`、`error` |
| 说明 | 在你提供的 registry 基础上准备引擎。若你既想保留内置工具又想注册自定义工具，通常应该先调用 `rt.NewRegistry()` 再往里注册。 |

### `(*Runtime).RunTask(ctx context.Context, task string, opts agent.RunOptions) (*agent.Final, *agent.Context, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `ctx context.Context`：运行上下文；`task string`：任务文本；`opts agent.RunOptions`：本次运行参数 |
| 返回值 | `*agent.Final`、`*agent.Context`、`error` |
| 说明 | 一次性便捷入口。内部会临时准备引擎，执行后自动 `Cleanup()`。 |

### `(*Runtime).RunTaskWithOptions(ctx context.Context, task string, opts RunTaskOptions) (RunTaskResult, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `ctx context.Context`：运行上下文；`task string`：任务文本；`opts integration.RunTaskOptions`：one-shot 执行和持久化参数 |
| 返回值 | `integration.RunTaskResult`、`error` |
| 说明 | 带显式运行 id 和可选 task journal 持久化的一次性入口。它会注入 `task_id` / `run_id`，只在调用方提供时使用 `trace_id` 和 `topic_id`，不会写 memory journal。 |

### `(*Runtime).GetLLMProfileSelection() (LLMProfileSelection, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `integration.LLMProfileSelection`、`error` |
| 说明 | 返回当前 runtime 实例的 `main_loop` selection 视图。若当前策略是 `candidates`，这里返回的是加权策略，而不是强行给出一个单一 profile。 |

### `(*Runtime).ListLLMProfiles() ([]LLMProfile, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `[]integration.LLMProfile`、`error` |
| 说明 | 列出所有已配置的 LLM profiles，包含 `name`、可选的 `inference_provider`、解析后的 `provider`、`model_name` 和可选的 `api_base`。 |

### `(*Runtime).SetLLMProfile(profileName string) error`

| 项目 | 内容 |
| --- | --- |
| 参数 | `profileName string`：要强制给 `main_loop` 使用的 profile 名称 |
| 返回值 | `error` |
| 说明 | 把当前 runtime 实例切到 `manual` 模式，仅覆盖 `main_loop`。像 `plan_create` 这样的其他 route purpose 不受影响。 |

### `(*Runtime).ResetLLMProfile()`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | 无 |
| 说明 | 清除当前 runtime 实例上的 `main_loop` 手动覆盖，回到配置里的 route policy。 |

### `(*Runtime).RequestTimeout() time.Duration`

| 项目 | 内容 |
| --- | --- |
| 参数 | 无 |
| 返回值 | `time.Duration` |
| 说明 | 返回当前 runtime 快照解析出的 LLM request timeout。 |

## Channel Runner

### `type BotRunner interface`

| 方法 | 参数 | 返回值 | 说明 |
| --- | --- | --- | --- |
| `Run` | `ctx context.Context` | `error` | 启动一个长生命周期 channel bot。 |
| `Close` | 无 | `error` | 主动关闭 runner。 |

### `type TelegramOptions struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `BotToken` | `string` | Telegram bot token。 |
| `AllowedChatIDs` | `[]int64` | 允许接入的 chat 白名单。 |
| `PollTimeout` | `time.Duration` | Telegram 轮询超时。 |
| `TaskTimeout` | `time.Duration` | 单条任务的运行超时。 |
| `MaxConcurrency` | `int` | 最大并发任务数。 |
| `GroupTriggerMode` | `string` | 群聊触发模式。 |
| `AddressingConfidenceThreshold` | `float64` | addressing 命中阈值。 |
| `AddressingInterjectThreshold` | `float64` | interject 阈值。 |
| `Hooks` | `integration.TelegramHooks` | 事件回调。 |

### `type SlackOptions struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `BotToken` | `string` | Slack bot token。 |
| `AppToken` | `string` | Slack app token。 |
| `AllowedTeamIDs` | `[]string` | 允许接入的 team 白名单。 |
| `AllowedChannelIDs` | `[]string` | 允许接入的 channel 白名单。 |
| `TaskTimeout` | `time.Duration` | 单条任务的运行超时。 |
| `MaxConcurrency` | `int` | 最大并发任务数。 |
| `GroupTriggerMode` | `string` | 群聊触发模式。 |
| `AddressingConfidenceThreshold` | `float64` | addressing 命中阈值。 |
| `AddressingInterjectThreshold` | `float64` | interject 阈值。 |
| `Hooks` | `integration.SlackHooks` | 事件回调。 |

### `type TelegramHooks struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `OnInbound` | `func(TelegramInboundEvent)` | 收到入站事件时触发。 |
| `OnOutbound` | `func(TelegramOutboundEvent)` | 发出出站事件时触发。 |
| `OnError` | `func(TelegramErrorEvent)` | 运行时错误事件。 |

### `type SlackHooks struct`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `OnInbound` | `func(SlackInboundEvent)` | 收到入站事件时触发。 |
| `OnOutbound` | `func(SlackOutboundEvent)` | 发出出站事件时触发。 |
| `OnError` | `func(SlackErrorEvent)` | 运行时错误事件。 |

### `(*Runtime).NewTelegramBot(opts TelegramOptions) (BotRunner, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `opts integration.TelegramOptions` |
| 返回值 | `integration.BotRunner`、`error` |
| 说明 | 构造 Telegram runner。`BotToken` 为空会直接返回错误。 |

### `(*Runtime).NewSlackBot(opts SlackOptions) (BotRunner, error)`

| 项目 | 内容 |
| --- | --- |
| 参数 | `opts integration.SlackOptions` |
| 返回值 | `integration.BotRunner`、`error` |
| 说明 | 构造 Slack runner。`BotToken` 或 `AppToken` 为空会直接返回错误。 |

## 事件别名类型

这些类型本身是导出别名，主要用于 hooks 的函数签名：

- `TelegramInboundEvent`
- `TelegramOutboundEvent`
- `TelegramErrorEvent`
- `SlackInboundEvent`
- `SlackOutboundEvent`
- `SlackErrorEvent`

如果你要写业务逻辑，通常只需要在 `Hooks` 中接这些事件，而不需要自己直接操作底层 runtime。
