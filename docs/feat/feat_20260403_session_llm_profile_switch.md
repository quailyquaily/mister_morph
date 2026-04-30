---
date: 2026-04-03
title: LLM Main Selection Switching (V1)
status: draft
---

# LLM Main Selection Switching (V1)

## 1) 目标

实现一套“main_loop 选择状态可切换”的能力。

这次需求只做三件事：

1. 列出当前正在使用的 LLM profile
2. 列出所有可用的 LLM profiles
3. 将当前 main_loop 切换到某个 profile

同时覆盖三类入口：

- Telegram 聊天命令
- Slack 聊天命令
- Console Web Chat 聊天命令

以及一个对外能力：

- `integration` 包为第三方调用方提供同等的 `current/list/set` 方法

这次明确不做持久化。

- selection 只保存在当前宿主的内存里
- first-party 进程重启后恢复为配置默认状态
- `integration.Runtime` 重建后恢复为配置默认状态

## 2) 现有基础

仓库里已经有两块可以直接复用的基础能力。

### 2.1 已有 `llm.profiles`

当前系统已经支持：

- 顶层 `llm.*` 作为 implicit default profile
- `llm.profiles.<name>` 作为命名 profile
- profile 继承顶层 `llm.*`

这部分继承和 provider-specific 归一化逻辑已经存在于：

- `internal/llmutil/routes.go`
- `internal/llmutil/llmutil.go`

因此这次不应该重新定义 profile schema。

### 2.2 当前 runtime 还不支持“按请求切 profile”

当前 `taskruntime.Runtime` 在 bootstrap 时就固定了：

- `MainRoute`
- `MainClient`
- `MainModel`

这意味着现有 `RunRequest.Model` 只能改“模型名字符串”，不能切换：

- provider
- endpoint / api_base
- api key
- headers
- provider-specific credential fields

结论：

> 这次需求不能通过复用现有 `SubmitTaskRequest.Model` 或 `RunRequest.Model` 直接实现。

必须把“当前 main_loop selection”接入到 main-loop route/client 解析链里。

## 3) 语义定义

### 3.1 这次切的是哪条路由

V1 只切换“主要 LLM”，也就是 `main_loop`。

不会受影响的 route：

- `addressing`
- `plan_create`
- `heartbeat`
- `memory_draft`

原因很简单：

- 用户命令 `/model set <profile>` 表达的是“当前对话主模型”
- 不应在这次需求里顺手改掉内部辅助链路

### 3.2 First-Party Scope

对 first-party 宿主，作用域是 process-global：

- `cmd/mistermorph` 下的 console local runtime
- Telegram / Slack / LINE / Lark channel daemon
- 其他同进程、共享同一套 runtime state 的 first-party 入口

也就是说：

- Telegram 里执行一次 `/model set cheap`
- 同进程里的 Slack / Console 后续 main loop 也都会改用 `cheap`
- 只要进程不重启，这个选择就持续有效

### 3.3 Integration Scope

对 `integration.Runtime`，作用域应是 runtime-instance scoped，而不是 process-global。

也就是说：

- 一个宿主程序只创建一个 `integration.Runtime` 时，效果接近“全局”
- 如果宿主创建多个 `integration.Runtime`，它们的 main selection 必须互不影响
- `integration` 不应该偷偷依赖进程级单例状态

### 3.4 状态模型

V1 建议只保留一个最小状态模型：

```go
type MainSelection struct {
    Mode          string // auto | manual
    ManualProfile string
}
```

语义：

- `auto`
  - 跟随配置里的 `llm.routes.main_loop`
- `manual`
  - 忽略 `main_loop.profile` / `main_loop.candidates`
  - 强制主 profile 为 `ManualProfile`

这次不需要引入更重的内部 machinery，例如：

- profile client catalog
- prepared profile lifecycle
- 一层新的大 service

### 3.5 初始值

当前 selection 在 runtime 初始化时默认为：

```go
MainSelection{Mode: "auto"}
```

因此初始行为是“按配置正常跑”。

如果配置是：

- implicit default
- 固定 `main_loop.profile`
- `main_loop.candidates`

都可以被 `auto` 模式自然表达，不需要单独兼容分支。

### 3.6 `default` 的含义

`default` 是保留 profile 名，表示顶层 `llm.*` 隐式 profile。

它必须满足：

- 会出现在 `list` 结果里
- 可以被 `set`
- 可以是 `current`

## 4) 用户交互契约

### 4.1 命令集合

建议命令形态如下：

```text
/model
/model list
/model set <profile_name>
/model reset
```

语义定义：

- `/model`
  - 显示当前 main selection
  - 同时返回简短 usage
- `/model list`
  - 显示所有可用 profiles
  - 每条至少包含：
    - `name`
    - `provider`
    - `model_name`
    - `api_base`（仅非空时显示）
  - 当前项应有明确标记
- `/model set <profile_name>`
  - 将当前 main selection 切到 `manual`
  - 并把 `ManualProfile` 设为指定 profile
  - 返回切换后的 selection 摘要
- `/model reset`
  - 清除当前 manual override
  - 将当前 main selection 切回 `auto`
  - 返回 reset 后的 selection 摘要

V1 命令 contract 仍保持最小集：

- `/model`
- `/model list`
- `/model set <profile_name>`
- `/model reset`

### 4.2 `/model` 展示的是“当前策略”

当 `Mode=manual` 时，`/model` 可以直接返回当前 profile 摘要，例如：

```text
mode: manual
profile: cheap
provider: openai
model_name: gpt-4.1-mini
api_base: https://api.openai.com
```

当 `Mode=auto` 且 `main_loop` 使用固定 profile 或 implicit default 时，也可以返回单一 resolved profile，但应明确标明这是 `auto`：

```text
mode: auto
main_loop: profile=default
provider: openai
model_name: gpt-5.2
api_base: https://api.openai.com
```

当 `Mode=auto` 且 `main_loop.candidates` 生效时，`/model` 不能伪装成“当前 profile”，而应返回当前策略，例如：

```text
mode: auto
main_loop: candidates
- default weight=80
- cheap weight=20
fallback_profiles:
- reasoning
```

因此 V1 不应让 `/model` 在 candidates 模式下不可用。

它应该返回“当前策略”，而不是硬凑一个假的当前 profile。

### 4.3 Chat 文案 vs Integration 返回

这里需要明确区分两种输出层：

- Telegram / Slack / Console chat
  - 返回人类可读文案
  - 目标是让用户直接看懂当前策略或切换结果
- `integration`
  - 返回结构化对象
  - 目标是让第三方宿主稳定消费数据，而不是解析聊天文案

也就是说：

- 聊天命令的返回文案不是协议
- `integration` 的返回 struct 才是对外 contract

### 4.4 `list` 展示的是“最终生效值”

`list` 不应输出原始 override 草稿，而应输出 resolved profile。

也就是：

- `provider` 为继承后的最终 provider
- `model_name` 为 provider 归一化后的最终模型名
- `api_base` 为最终 endpoint，仅非空时显示

例如：

```text
Current profile: cheap

- default
  provider: openai
  model_name: gpt-5.2
  api_base: https://api.openai.com

- cheap (current)
  provider: openai
  model_name: gpt-4.1-mini
  api_base: https://api.openai.com

- reasoning
  provider: xai
  model_name: grok-4.1-fast-reasoning
```

### 4.5 错误处理

`/model set <profile_name>` 需要明确处理：

- profile 不存在
- profile 名为空

`/model reset` 不需要额外参数。

推荐错误文案：

```text
unknown llm profile "foo"
```

```text
usage: /model set <profile_name>
```

```text
usage: /model reset
```

## 5) Internal State And Helpers

V1 建议保持最小内部结构：

1. 一个线程安全的 `MainSelection` 状态
2. 少量纯函数 helper

建议放在一个较轻的内部包里，例如：

- `internal/llmselect`

建议 helper 分成两类：

```go
func CurrentSelection(values llmutil.RuntimeValues, sel MainSelection) (CurrentSelectionView, error)
func ListProfiles(values llmutil.RuntimeValues) ([]ProfileInfo, error)
func ResolveMainRoute(values llmutil.RuntimeValues, sel MainSelection) (llmutil.ResolvedRoute, error)
```

其中：

- `CurrentSelection(...)`
  - 负责把 `auto/manual` 状态转成用户可展示的当前策略
- `ListProfiles(...)`
  - 负责列出所有 resolved profiles
- `ResolveMainRoute(...)`
  - 负责把 `MainSelection` 落成最终 `main_loop` route

实现要求：

- 线程安全
- 只使用进程内内存
- 不读写磁盘
- 不绕开现有 `llmutil` 继承与 provider 归一化逻辑

### 5.1 `ResolveMainRoute(values, sel)` 语义

它的职责不是“重新发明路由”，而是：

1. 当 `sel.Mode == auto` 时：
   - 直接按当前 `llm.routes.main_loop` 配置解析
2. 当 `sel.Mode == manual` 时：
   - 以 `sel.ManualProfile` 作为 primary profile
   - fallback 仍沿用配置里的 `main_loop.fallback_profiles`

也就是说：

- `auto` 保留原有 `main_loop` 语义
- `manual` 只是对 main primary profile 做 override
- `addressing / plan_create / heartbeat / memory_draft` 仍走原有 resolver

## 6) 对 `llmutil` 的最小补充

当前 `internal/llmutil` 已经有可复用的私有逻辑，但对这个需求还少两个公开 helper：

1. 列出所有 resolved profiles
2. 解析某个 profile 为最终 client config

建议新增类似能力：

```go
type ResolvedProfile struct {
    Name         string
    Values       RuntimeValues
    ClientConfig llmconfig.ClientConfig
}

func ResolveProfile(values RuntimeValues, profileName string) (ResolvedProfile, error)
func ListProfiles(values RuntimeValues) ([]ResolvedProfile, error)
```

这样可以保证：

- Telegram / Slack / Console / Integration 不会各自复制一套 profile 继承逻辑
- `list/current/set` 与真正 run 时使用的是同一套解析结果

## 7) 对 `taskruntime` 的影响

这是本需求真正的实现关键。

### 7.1 当前问题

当前 `taskruntime.Runtime` 在 `Bootstrap` 时固定了：

- `MainRoute`
- `MainClient`
- `MainModel`

因此如果当前 main selection 被修改，已有 runtime 不会自动跟着变。

### 7.2 建议改法: 晚绑定

V1 更小的修法不是“预构建 profile catalog”，而是把 `main_loop` 改成每次 `Run()` 现算一次。

建议在 `taskruntime.Runtime` 里新增一个 helper，语义类似：

```go
func (rt *Runtime) resolveMainForRun() (llmutil.ResolvedRoute, llm.Client, string, error) {
    route, err := depsutil.ResolveLLMRouteFromCommon(rt.commonDeps, llmutil.RoutePurposeMainLoop)
    if err != nil {
        return llmutil.ResolvedRoute{}, nil, "", err
    }
    client, err := depsutil.CreateClientFromCommon(rt.commonDeps, route)
    if err != nil {
        return llmutil.ResolvedRoute{}, nil, "", err
    }
    if rt.ClientDecorator != nil {
        client = rt.ClientDecorator(client, route)
    }
    model := strings.TrimSpace(route.ClientConfig.Model)
    return route, client, model, nil
}
```

然后 `Run()` 改成：

1. 每次先 `resolveMainForRun()`
2. `RegisterRuntimeTools()` 用这次的 main client/model
3. `PromptSpecFromCommon()` 用这次的 main client/model
4. `agent.New(...)` 用这次的 main client

而 `plan_create` 继续保持 bootstrap 缓存，正好符合“只切 main_loop”的需求。

### 7.3 晚绑定的完整含义

只改 `Run()` 还不够。

还要把“默认模型的决定时机”后移到真正执行时。

当前已有几个调用方在进入 `Run()` 前，就把 `rt.MainModel` 提前塞进了 `RunRequest.Model`：

- `internal/channelruntime/telegram/runtime_task.go`
- `internal/channelruntime/slack/runtime_task.go`
- `internal/channelruntime/line/runtime_task.go`
- `internal/channelruntime/lark/runtime_task.go`

这些地方如果不是“显式用户指定模型”，都应该传空字符串，让 `Run()` 自己按当前 main selection 决定默认模型。

Console 还更早一步把默认 model 写进 task 持久化数据：

- `cmd/mistermorph/consolecmd/local_runtime.go`
- `cmd/mistermorph/consolecmd/local_runtime_bus.go`

因此 Console 也要改成：

- 只有用户显式选 model 时才保留 `req.Model`
- 否则留空
- 真正执行时再按当前 main selection 解析

这才是“晚绑定”的完整定义。

## 8) Integration Public Contract

`integration` 包需要直接暴露这三个方法给第三方调用方。

但要明确：这里的作用域是 `runtime-instance scoped`。

建议新增：

```go
type LLMProfileInfo struct {
    Name      string
    Provider  string
    ModelName string
    APIBase   string
}

type LLMCandidateInfo struct {
    Name   string
    Weight int
}

type LLMMainSelection struct {
    Mode          string
    ManualProfile string
    Current       *LLMProfileInfo
    Candidates    []LLMCandidateInfo
    Fallbacks     []string
}

func (rt *Runtime) GetLLMProfileSelection() (LLMMainSelection, error)
func (rt *Runtime) ListLLMProfiles() ([]LLMProfileInfo, error)
func (rt *Runtime) SetLLMProfile(profileName string) (LLMMainSelection, error)
func (rt *Runtime) ResetLLMProfile() (LLMMainSelection, error)
```

`integration` 现有：

- `NewRunEngine(...)`
- `RunTask(...)`

只要内部 main-loop client 解析改成读取“该 `Runtime` 实例自己的当前 main selection”，这两个入口就会天然继承最新设置。

## 9) 三个入口的落点

### 9.1 Telegram

Telegram 已经有 slash command 解析。

实现落点建议在：

- `internal/channelruntime/telegram/runtime.go`

做法：

- 复用当前 Telegram runtime 的命令分支
- 新增 `/model` 分支
- `/model` 不进入 agent run
- `/model` 不写入聊天 history
- `/model` 不触发 memory record

执行结果会影响整个进程，不只是当前 `chat_id`。

### 9.2 Channel runtimes

Channel runtime 在入队 agent job 前先处理 `/model`。

实现落点建议在：

- `internal/channelruntime/slack/runtime.go`
- `internal/channelruntime/line/runtime.go`
- `internal/channelruntime/lark/runtime.go`

位置上应在 enqueue agent job 之前。

做法：

- 收到文本后先 parse `/model`
- 如果命中，直接回复到当前 channel / thread
- 跳过 `runner.Enqueue(...)`
- 不进入 history / memory / task run

虽然命令是在某个 thread 里发出的，但切换结果是全进程共享的。

### 9.3 Console Web Chat

Console chat 当前通过 `/tasks` 提交消息，并期待拿到 task id 再轮询结果。

因此 V1 最省改动的做法不是前端新建一套 API，而是：

- 在 `consoleLocalRuntime.submitTask(...)` 里先识别 `/model`
- 命中后生成一个“立即完成”的 synthetic task
- 把命令结果作为该 task 的最终输出返回给 ChatView

这样可以保持：

- 现有 ChatView 提交流程不变
- 不需要为 V1 新增独立 console transport contract

同时需要注意：

- 该 synthetic task 不应调用 agent
- 不应写 memory
- 不应参与 topic title 自动生成

虽然命令是在某个 topic 下执行，但切换结果是全进程共享的。

## 10) 命令解析建议

建议把 `/model` 的解析抽成一层共享 helper，而不是在各 runtime 里重复实现。

例如新增：

- `internal/chatcommand`
- 或 `internal/llmprofile/command`

建议能力：

```go
type ModelCommand struct {
    Action      string // current | list | set
    ProfileName string
}

func ParseModelCommand(text string) (ModelCommand, bool, error)
```

规则：

- `/model` => `current`
- `/model list` => `list`
- `/model set <name>` => `set`
- `/model reset` => `reset`
- 其他 `/model ...` => parse error

## 11) 测试覆盖

至少需要补这些测试：

1. `llmutil` / selection helper
   - `default` 出现在 list 结果里
   - profile 继承 provider/model/endpoint 正确
   - `set` 到不存在 profile 返回错误
   - `auto + main_loop.candidates` 场景能返回当前策略视图
   - `manual` 能覆盖 `main_loop.candidates`

2. `taskruntime`
   - 切换 main selection 后，后续 main loop 使用新的 provider/model/endpoint
   - `plan_create` route 不受 main selection 影响
   - `Run()` 晚绑定后，main_loop 每次执行都会重新 resolve
   - channel runtimes 默认不再提前把 `rt.MainModel` 填进 `RunRequest.Model`

3. Telegram
   - `/model`
   - `/model list`
   - `/model set cheap`
   - `/model reset`
   - `/model set missing`

4. Slack
   - `/model` 命令不会进入正常 agent queue
   - 在 Slack thread 里切换后，其他 first-party 入口后续 run 也会使用新的 selection

5. Console
   - `/model` 命中 synthetic task 路径
   - synthetic task 不触发 topic title 自动生成
   - 未显式指定 model 的 task 不应在提交阶段写死默认 model

6. Integration
   - `GetLLMProfileSelection`
   - `ListLLMProfiles`
   - `SetLLMProfile`
   - `ResetLLMProfile`
   - 不同 `integration.Runtime` 实例的 selection 彼此隔离
   - `RunTask` 真正使用该实例当前 selection

## 12) 非目标

这次明确不做：

- 不持久化 main selection
- 不新增 Settings UI 里的 profile picker
- 不把 `addressing / plan_create / heartbeat` 也做成可切换 selection
- 不引入更重的 prepared client catalog / lifecycle machinery
- 不重做 `llm.routes` 设计

## 13) 结论

这次需求的核心不是“新增一个命令”，而是：

> 在不改 profile schema 的前提下，为 main loop 引入一个可切换的 selection 状态，并把“默认 route/client/model 的决定时机”后移到真正执行时。

如果按上面的边界实现，改动范围是可控的：

- profile schema 不变
- 现有 `llmutil` 继承逻辑复用
- first-party 与 `integration` 的作用域边界清晰
- Telegram / Slack / LINE / Lark / Console 都能用统一的 `/model` 语义

同时这份设计刻意保持窄范围：

- 只做 main loop
- 只做内存态
- 只做最小状态模型 + 晚绑定

这是当前阶段最小、最稳妥的实现面。
