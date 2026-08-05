---
date: 2026-08-05
title: Independent LLM Profiles
status: implemented
---

# Independent LLM Profiles

## 目标

命名 profile 是一份独立的 LLM 配置，不再是顶层 `llm` 配置的局部覆盖。

`default` 仍表示顶层 `llm` 配置。选择命名 profile 时，resolver 只读取该 profile 自己的连接字段。字段留空不会再回退到 `default`。

## 配置边界

命名 profile 自己负责这些字段：

- `inference_provider` / `provider`
- `endpoint`
- `api_key`
- `model`
- `supports_image_parts`
- `context_window_tokens`
- `headers`
- `cache_ttl` / `cache_key_prefix`
- `request_timeout`
- `tools_emulation_mode`
- `temperature`
- `reasoning_effort` / `reasoning_budget_tokens`
- `azure.*`
- `bedrock.*`
- `cloudflare.*`

以下配置仍是所有 route 共用的运行上下文，不属于 profile 继承：

- `llm.pricing_file`
- `llm.image.*`
- `config` 和 `file_state_dir`

`pricing_file` 是统一定价目录，`llm.image` 是独立的图像客户端配置，状态目录用于 OAuth 凭证。它们没有 profile 级覆盖字段，因此继续由进程统一提供。

## 解析规则

1. `default` 直接使用顶层 `llm` 配置。
2. 命名 profile 从空的 LLM 配置开始，只复制 `llm.profiles.<name>` 中存在的字段。
3. resolver 再根据该 profile 的 `inference_provider` 派生底层 provider 和内置 endpoint。
4. provider 自带的默认值仍然有效。例如 `openai_codex` 和 `xai_oauth` 可以使用各自的默认 model 或 endpoint。这是 provider 默认值，不是顶层继承。
5. `MISTER_MORPH_LLM_*` 只覆盖顶层默认 LLM。命名 profile 如需读取环境变量，应在自己的字段中显式写 `${ENV_NAME}`。

推荐每个命名 profile 至少明确配置 `inference_provider` 和可用的 model，并在需要时配置自己的 endpoint 与凭据：

```yaml
llm:
  inference_provider: anthropic
  model: claude-sonnet-4-5
  api_key: "${ANTHROPIC_API_KEY}"

  profiles:
    cheap:
      inference_provider: openai
      model: gpt-4.1-mini
      api_key: "${OPENAI_API_KEY}"
      request_timeout: "60s"

    reasoning:
      inference_provider: xai
      model: grok-4.1-fast-reasoning
      api_key: "${XAI_API_KEY}"
      reasoning_effort: high
```

在这个例子里，`cheap` 和 `reasoning` 都不会得到顶层 Anthropic 的 API key、timeout、headers 或其他字段。

## Settings 与连接测试

Console Settings 对 profile 使用同一语义：

- profile 表单必须选择自己的 inference provider；
- OAuth 状态、API Base、凭据校验、model picker 和 benchmark 只读取当前 profile；
- profile benchmark 只发送并解析目标 profile 草稿；
- 顶层默认 LLM 的未保存草稿不会影响 profile benchmark；
- 非目标 profile 的无效字段不会阻塞当前 benchmark。

Settings API 读写 profile 时，也不再借用顶层 provider 来过滤或解释 provider-specific 字段。

## 迁移

旧配置中只写差异字段的 profile 需要补全。例如：

```yaml
# 旧配置：依赖顶层 provider、endpoint 和 api_key
llm:
  profiles:
    cheap:
      model: gpt-4.1-mini
```

应改成：

```yaml
llm:
  profiles:
    cheap:
      inference_provider: openai
      model: gpt-4.1-mini
      api_key: "${OPENAI_API_KEY}"
```

如果 profile 需要自定义 endpoint、headers、timeout、cache 或 reasoning 配置，也必须在该 profile 中明确写出。
