---
title: 创建自己的 AI Agent：进阶
description: 聚焦 integration.Config、Registry、PreparedRun 与 Channel 接入；完整 API 清单单独放到 Integration API。
---

# 创建自己的 AI Agent：进阶


## 配置层

`integration.Config` 是 `integration` 包唯一的显式配置入口。

宿主程序负责读取环境变量、配置文件或数据库，把最终值写进 `Config`，再交给 `integration.New(cfg)`。

### 示例

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.inference_provider", "openai")
cfg.Set("llm.model", "gpt-5.4")
cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))
```

### 覆盖默认配置

Mister Morph 自己虽然使用命令行参数，环境变量，和 config.yaml 文件来进行配置。
第三方可以使用自己喜欢的方式，然后使用 `Set(key, value)` 用来覆盖任意默认配置。所有 `config.yaml` 中的字段都可以这样设置，可参考 [配置字段](/zh/guide/config-reference)。

### 开关功能特性

`Features.*` 用于开关一些功能特性，目前支持如下几个功能：

- `PlanTool`：是否注册运行时辅助工具 `plan_create`。
- `Guard`：是否在 runtime 内注入 guard。
- `Skills`：是否在 prompt 构造阶段启用 skills 加载。

### 内置工具

`BuiltinToolNames` 用来开启内置工具，留空表示接入全部内置工具。

### 自定义 prompt

如果你想在 `integration` 层做 prompt 定制，用 `cfg.AddPromptBlock(...)`。

这些 block 会自动应用到 system prompt 的最后。

### 探查器

Mister Morph 提供了 inspector 的机制，帮你去探查大模型运作的底层信息：

```go
cfg.Inspect.Prompt = true
cfg.Inspect.Request = true
cfg.Inspect.DumpDir = "./dump"
```

这样的配置会在 dump 目录生成对应的 prompt 和　request 的详细过程。

### LLM 路由策略

类似地，也可以直接覆盖 `llm.routes.*` 来定制不同的 llm 套路由策略。

可以通过 `llm.profiles` 定义一系列不同的 profiles，然后使用类似这样的写法，来要求某个功能使用某个指定 LLM：

```go
// 要求创建计划的时候使用名为 reasoning 的 LLM profile
cfg.Set("llm.routes.plan_create", "reasoning")
```

完整的规则和写法，见 [LLM 路由策略](/zh/guide/llm-routing)。

### 运行时切换主 LLM Profile

如果你的宿主程序需要做模型选择器，`integration.Runtime` 也支持在运行时查看和覆盖当前 `main_loop` profile。

```go
profiles, err := rt.ListLLMProfiles()
if err != nil {
  panic(err)
}

selection, err := rt.GetLLMProfileSelection()
if err != nil {
  panic(err)
}

fmt.Println("mode:", selection.Mode)

if len(profiles) > 0 {
  if err := rt.SetLLMProfile(profiles[0].Name); err != nil {
    panic(err)
  }
}

rt.ResetLLMProfile()
```

需要注意：

- 这个状态只作用于单个 `integration.Runtime` 实例。
- `SetLLMProfile(...)` 只覆盖 `main_loop`。
- `ResetLLMProfile()` 会回到配置里的 `llm.routes.main_loop` 策略。
- 如果 `main_loop` 配的是加权 `candidates`，`GetLLMProfileSelection()` 返回的是当前策略视图，而不是伪造一个固定 profile。

### 配置生效

配置完成以后，使用 `integration.New(cfg)` 快照化，创建 agent runtime。

## 自定义工具

如果你想保留 `integration` 组好的内置工具，又想加入自定义工具，可以使用 Runtime 的方法 `rt.NewRegistry()` 创建一个工具注册表。

### 自定义工具示例

下面这个例子展示了如何自定义一个 echo 工具并将其注册到 agent：

```go
package main

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "strings"

  "github.com/quailyquaily/mistermorph/agent"
  "github.com/quailyquaily/mistermorph/integration"
)

type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo_text" }

func (t *EchoTool) Description() string {
  return "Echoes input text as JSON."
}

func (t *EchoTool) ParameterSchema() string {
  return `{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Text to echo."}
  },
  "required": ["text"]
}`
}

func (t *EchoTool) Execute(_ context.Context, params map[string]any) (string, error) {
  text, _ := params["text"].(string)
  text = strings.TrimSpace(text)
  if text == "" {
    return "", fmt.Errorf("text is required")
  }
  b, _ := json.Marshal(map[string]any{"text": text})
  return string(b), nil
}

func main() {
  cfg := integration.DefaultConfig()
  cfg.Set("llm.inference_provider", "openai")
  cfg.Set("llm.model", "gpt-5.4")
  cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))

  rt := integration.New(cfg)
  reg := rt.NewRegistry()
  reg.Register(&EchoTool{})

  task := "Call tool echo_text with text 'hello from tool', then answer with that text."

  prepared, err := rt.NewRunEngineWithRegistry(context.Background(), task, reg)
  if err != nil {
    panic(err)
  }
  defer prepared.Cleanup()

  final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{Model: prepared.Model})
  if err != nil {
    panic(err)
  }

  fmt.Println("Agent:", final.Output)
}
```

## Runtime 运行方式

### Prepared Engine 方式

适合你想把生命周期控制权拿回来，自己做会话、复用、资源释放或上层调度的时候。

- 生命周期可控：你可以明确在何时 `Cleanup()`，适合接入你自己的进程管理。
- 可复用：同一个 `prepared.Engine` 可以多次 `Run(...)`，避免重复准备。
- 运行参数可变：每次 `Run` 都可传不同 `RunOptions`（如 `History`、`Meta`、`OnStream`）。
- 便于编排：你能直接拿到 `prepared.Model` 与 `Engine`，更适合做上层会话/调度封装。

```go
prepared, err := rt.NewRunEngine(context.Background(), task)
if err != nil {
  panic(err)
}
defer prepared.Cleanup()

final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{
  Model: prepared.Model,
})
```

### 便捷方式

适合一次性任务；如果你要自定义 registry、复用 engine、或者自己控制生命周期，更适合用 `PreparedRun`。

```go
final, runCtx, err := rt.RunTask(context.Background(), task, agent.RunOptions{})
_ = final
_ = runCtx
_ = err
```

## 接入 Channels

除了 Web UI，Mister Morph 支持不同的 channel 作为沟通界面，例如，Telegram 和 Slack。

接入方法非常简单：

### 接入 Telegram

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN")})
_ = tg
```

如果你想在宿主程序里接 Telegram 的入站、出站和错误事件，可以在 `TelegramOptions.Hooks` 里传 `TelegramHooks`：

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{
  BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN"),
  Hooks: integration.TelegramHooks{
    OnInbound: func(ev integration.TelegramInboundEvent) {
      fmt.Printf("telegram inbound: %+v\n", ev)
    },
    OnOutbound: func(ev integration.TelegramOutboundEvent) {
      fmt.Printf("telegram outbound: %+v\n", ev)
    },
    OnError: func(ev integration.TelegramErrorEvent) {
      fmt.Printf("telegram error: %+v\n", ev)
    },
  },
})
_ = tg
```

### 接入 Slack

```go
sl, _ := rt.NewSlackBot(integration.SlackOptions{
  BotToken: os.Getenv("MISTER_MORPH_SLACK_BOT_TOKEN"),
  AppToken: os.Getenv("MISTER_MORPH_SLACK_APP_TOKEN"),
})
_ = sl
```
