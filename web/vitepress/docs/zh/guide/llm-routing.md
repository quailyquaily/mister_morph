---
title: LLM 路由策略
description: 为不同 llm purpose 选择 profile、分流与错误回退。
---

# LLM 路由策略

Mister Morph 提供了灵活的路由策略来解决如下问题：

1. 不同 purpose 有适合该目的的 llm 配置
2. llm 请求需要分流
3. 当某个 llm 配置出问题时可以回退到备份 llm 配置

## LLM Profile

每个 Profile 就是一个 LLM 配置。顶层 `llm.*` 本身就是默认 profile，`llm.profiles.<name>` 用来声明命名 profile。

注意：
- 命名 profile 会继承顶层 `llm.*`，只覆盖自己改动的字段。
- `default` 是保留名字，表示“继续使用顶层 `llm.*`”。
- 面向用户的供应商选择写 `inference_provider`。`provider` 是派生出来的协议层 provider，主要给旧配置和高级覆盖使用。

在下面这个例子中，顶层模型是 OpenAI 的 GPT-5.4。同时还定义了两个 profile，分别是 GPT-4o mini 和 Claude Opus 4.6。

从 profile 的命名上可以看出，其中 GPT-4o mini 是为了解决一些便宜的任务，Claude Opus 4.6 是为了更深度地思考。

```yaml
llm:
  inference_provider: "openai"
  model: "gpt-5.4"
  api_key: "${OPENAI_API_KEY}"

  profiles:
    cheap:
      model: "gpt-4o-mini"
    reasoning:
      inference_provider: "anthropic"
      model: "claude-opus-4-6"
      api_key: "${CLAUDE_API_KEY}"
```

也就是说，profile 负责定义有**哪些可复用的 LLM 配置**，让接下来的路由，分流和回退功能调用。

## 路由

配置 `llm.routes.*` 中定义了如何给不同的 llm purpose 指定不同的模型配置。

除了 `main_loop` 负责 Agent 的运行以外，其他 purposes 都是独立的 llm 调用（也可以理解成简单的 sub agent）

### 目前支持的 purpose

- `main_loop`：主 Agent loop。
- `addressing`：只用于群聊或频道里的 addressing 判定。
- `awareness`：只用于定时 awareness 任务。
- `heartbeat`：`awareness` 的旧别名。
- `think`：只用于 `/think <task>` 命令前缀，并会为本次任务临时应用 `reasoning_effort=xhigh`。
- `plan_create`：只用于 `plan_create` 工具内部的计划请求。
- `memory_draft`：只用于 memory 草稿整理。

在下面这个例子中，创建计划和 `/think` 使用 reasoning 的 profile，也就是 "claude-opus-4-6"；进行群聊的 addressing 判定时，则用了便宜的 "gpt-4o-mini":

```yaml
llm:
  routes:
    plan_create: reasoning
    think: reasoning
    addressing: cheap
```

### 路由的分流

Mister Morph 支持对 LLM 请求做流量分流，用 `candidates` 字段来定义分流表。

下面的例子展示了如何把流量分到 `default_apple` 和 `default_banana`（需要在 `llm.profiles` 预先定义他们）：

```yaml
llm:
  routes:
    main_loop:
      candidates:
        - profile: "default"
          weight: 1
        - profile: "default_apple"
          weight: 1
        - profile: "default_banana"
          weight: 1
```

规则：

- `candidates.weight` 用来决定选择权重。
- 同一个 Loop 内只会使用某一个 profile，不会穿插使用（按 `run_id` 来选择）。
- 如果当前 llm 遇到可回退错误，运行时会先尝试同 route 下剩余 candidate。

### 路由的回退

除了分流，Mister Morph 支持对 LLM 请求进行错误回退。例如：

```yaml
llm:
  routes:
    plan_create:
      profile: "reasoning"
      fallback_profiles: [ "default" ]
```

如果当前 llm 遇到可回退错误，如果其他 candidate 都不可用，会挨个尝试 `fallback_profiles` 中的配置。

## 在 integration 里的写法

配置方式类似，只是把 YAML 改成 `cfg.Set(...)`：

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.routes.plan_create", "reasoning")
cfg.Set("llm.routes.addressing", map[string]any{
  "profile": "cheap",
  "fallback_profiles": []string{"default"},
})
```
