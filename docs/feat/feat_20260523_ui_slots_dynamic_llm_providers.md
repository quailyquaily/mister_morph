---
date: 2026-05-23
title: UI Slots 与动态 LLM Provider
status: draft
---

# UI Slots 与动态 LLM Provider

## 1) 目标

新增两个小扩展点：

1. UI slot。
   - 默认不渲染任何内容。
   - 如果构建时发现指定文件名的 JavaScript Vue 组件，就把该组件渲染到对应 slot。
   - V1 先加一个 slot，位置在侧栏左下角。

2. 动态 LLM 配置。
   - 如果 `<file_state_dir>/internal/llm_overlay.yaml` 存在且有效，其中的 `llm_overlay` 条目会成为默认 LLM route 的候选项。
   - 条目使用现有 `llm.*` 基础字段，不包含 `profiles` 和 `routes`。

同时新增 `llm.inference_provider`，顶层 `llm` 和 `llm.profiles.*` 都支持。
当前 `llm.provider` 实际表达的是连接协议和客户端实现。
新的 `llm.inference_provider` 表达模型推理供应商。

## 2) 术语

- `inference_provider`：用户看到的模型供应商，例如 OpenAI、Claude AI、Deepseek。
- `provider`：内部协议和客户端选择器，例如 `openai`、`openai_resp`、`anthropic`、`bedrock`。
- `api_base`：用户语义里的 API 基础地址；当前 YAML 配置字段名是 `endpoint`。

UI 应该让用户选择 `inference_provider`，不要让用户填写 `provider`。
运行时在创建 LLM client 前，仍然必须解析出明确的 `provider`。

## 3) 非目标

- 不做任意运行时 JavaScript 插件系统。
- 不做 UI slot 热加载。
- V1 不监听 `<file_state_dir>/internal/llm_overlay.yaml` 的变化。
- 动态 provider 文件不能定义 `llm.routes` 或嵌套 `llm.profiles`。
- 不删除 `llm.provider`；它继续作为运行时兼容字段存在。
- 本迭代不改变图像模型路由。

## 4) UI Slot 设计

V1 slot：

- slot id：`sidebar.bottom_left`
- 组件文件：`web/console/src/ext/slots/SidebarBottomLeft.js`
- 渲染位置：`AppSidebar` 内部，导航列表之后，靠侧栏左下角

构建期加载使用一个很小的 slot registry：

```js
const modules = import.meta.glob("./*.js", { eager: true });

export const uiSlots = {
  "sidebar.bottom_left": modules["./SidebarBottomLeft.js"]?.default ?? null,
};
```

渲染规则：

- registry 值为 `null` 时，不渲染任何内容
- 组件存在时，通过 Vue 动态组件渲染
- 组件缺失时，不显示占位文案，也不显示空的可见容器

建议传给组件的 props：

- `currentPath`
- `selectedEndpointItem`
- `locale`
- `t`

slot 组件如果需要数据，可以直接导入现有 store 或 API helper。
slot 契约保持窄范围；V1 不需要额外事件总线。

slot 组件使用当前 Console 已有的 JavaScript Vue component 写法。
不为这个需求新增 Vue SFC 构建支持。

## 5) 动态 Provider 文件

路径：

```text
<file_state_dir>/internal/llm_overlay.yaml
```

V1 结构：

```yaml
llm_overlay:
  default:
    candidates:
      - profile: default
        weight: 1
      - profile: kimi-fast
        weight: 1

  providers:
    kimi-fast:
      inference_provider: kimi
      model: kimi-k2
      api_key: "${KIMI_API_KEY}"
      request_timeout: "90s"

    work-openai-compatible:
      inference_provider: openai_chat_compatible
      endpoint: "https://llm.example.com/v1"
      model: "custom-model"
      api_key: "${WORK_LLM_API_KEY}"
```

`llm_overlay.providers` 下的 map key 是动态 profile 名。
每个 value 是一份 LLM 基础配置。
`default` 是可选字段，用来显式覆盖默认 main route。
`main_loop` 也是可选字段，语义和 `default` 相同，名字更贴近现有 `llm.routes.main_loop`。
`default` 和 `main_loop` 不应同时出现；同时出现时报错，避免配置含义不清。

允许字段应和现有 chat LLM 配置保持一致：

- `inference_provider`
- `provider`，只用于旧配置或手工兼容
- `endpoint`
- `api_key`
- `model`
- `context_window_tokens`
- `headers`
- `cache_ttl`
- `cache_key_prefix`
- `request_timeout`
- `tools_emulation_mode`
- `temperature`
- `reasoning_effort`
- `reasoning_budget_tokens`
- `azure`
- `bedrock`
- `cloudflare`

明确拒绝：

- `profiles`
- `routes`
- `image`

校验规则：

- 文件不存在：无影响
- `llm_overlay.providers` 为空：无影响
- YAML 无法解析：创建运行时配置快照失败，并返回清楚的错误
- 动态 profile 名必须非空，且不能是 `default`
- 与静态 `llm.profiles` 重名：启动时报错
- `default` 或 `main_loop` route 只能引用 `default`、静态 profile、动态 profile
- 单个条目无效：启动时报错，错误里带上动态 profile 名
- 字符串值继续支持现有 `${ENV_VAR}` 展开

动态条目不继承顶层身份字段：

- 不继承 `provider`
- 不继承 `endpoint`
- 不继承 `api_key`
- 不继承 `model`

`request_timeout` 这类中性默认值可以沿用顶层 `llm.*` 的默认值路径。

## 6) Route 集成

动态 provider 条目会作为 state 来源的 named profile 加入运行时 LLM values。

V1 route 行为：

- 如果 `llm_overlay.yaml` 没有 `llm_overlay.default` 或 `llm_overlay.main_loop`，只有隐式 `main_loop` route 会自动加入动态候选项
- 自动候选项为 `default` 加所有动态 provider 条目
- 自动候选项权重均为 `1`
- 如果 `llm_overlay.yaml` 显式配置了 `llm_overlay.default` 或 `llm_overlay.main_loop`，它覆盖默认 main route
- `llm_overlay.yaml` 的显式 main route 可以覆盖 `config.yaml` 里的 `llm.routes.main_loop`
- `default` 和 `main_loop` 是同一件事；建议用户写 `default`，实现中把它归一化为 `main_loop`
- 即使存在显式 main route，动态 profile 仍然可以被列出，供 UI 展示或后续手工使用

这个功能只增加 route candidates，不增加重试语义。
瞬时错误回退仍由现有 `fallback_profiles` 控制。

隐式结果示例：

```yaml
llm:
  routes:
    main_loop:
      candidates:
        - profile: default
          weight: 1
        - profile: kimi-fast
          weight: 1
        - profile: work-openai-compatible
          weight: 1
```

这是内存里的解析结果。
不要把它写回 `config.yaml`。

显式覆盖示例：

```yaml
llm_overlay:
  default:
    profile: kimi-fast

  providers:
    kimi-fast:
      inference_provider: kimi
      model: kimi-k2
      api_key: "${KIMI_API_KEY}"
```

## 7) `inference_provider` 字段

新增配置：

```yaml
llm:
  inference_provider: openai
  model: "gpt-5.4"
  api_key: "${OPENAI_API_KEY}"

  profiles:
    reasoning:
      inference_provider: xai
      model: "grok-4.1-fast-reasoning"
      api_key: "${XAI_API_KEY}"
```

Profile 规则：

- profile 级 `inference_provider` 覆盖顶层值
- profile 未设置时，沿用当前 profile 解析路径继承顶层值
- 继承完成后，resolver 再派生 `provider` 和 `endpoint`

Console Settings UI 显示 `inference_provider`。
不再让用户填写原始 `provider`。

对内置直连供应商，UI 隐藏 API base 字段。
对三个 Compatible 供应商，UI 显示 API base，因为 endpoint 必须由用户提供。

## 8) 内置 `inference_provider`

规范配置值：

| 显示名 | `inference_provider` | 派生 `provider` | 派生 `endpoint` |
| --- | --- | --- | --- |
| OpenAI | `openai` | `openai_resp` | `https://api.openai.com` |
| OpenAI Codex | `openai_codex` | `openai_codex` | Codex 默认 endpoint |
| Google Gemini | `gemini` | `gemini` | `https://generativelanguage.googleapis.com` |
| Claude AI | `anthropic` | `anthropic` | `https://api.anthropic.com` |
| AWS Bedrock | `bedrock` | `bedrock` | 空；Bedrock 使用 region/model ARN |
| Cloudflare | `cloudflare` | `cloudflare` | `https://api.cloudflare.com/client/v4` |
| MisterMorph Pro | `mistermorph_pro` | `openai` | `https://router.mistermorph.com/api/v1` |
| xAI | `xai` | `xai` | `https://api.x.ai` |
| Deepseek | `deepseek` | `deepseek` | `https://api.deepseek.com` |
| Kimi | `kimi` | `openai_custom` | `https://api.moonshot.cn` |
| OpenRouter | `openrouter` | `openai_custom` | `https://openrouter.ai/api/v1` |
| Groq | `groq` | `openai_custom` | `https://api.groq.com/openai/v1` |
| OpenAI Chat Compatible | `openai_chat_compatible` | `openai_custom` | 用户填写 |
| OpenAI Response Compatible | `openai_response_compatible` | `openai_resp` | 用户填写 |
| Claude AI Compatible | `anthropic_compatible` | `anthropic` | 用户填写 |

Go 侧应有一个推理供应商 registry。
这个 registry 是唯一派生入口。
所有 `provider` 和 `endpoint` 都从 registry 中的 `inference_provider` 记录得出。
Console 前端可以镜像这份列表。
测试应尽量防止两边列表漂移。

## 9) 兼容旧配置

没有 `llm.inference_provider` 的旧配置必须继续工作。

启动时，如果 `llm.inference_provider` 为空，根据有效的旧配置反推。
每个 resolved profile 也执行同样规则。

建议反推规则：

- `provider=openai_codex` -> `openai_codex`
- `provider=gemini` -> `gemini`
- `provider=anthropic` 且 endpoint 是 Anthropic 默认地址 -> `anthropic`
- `provider=anthropic` 且 endpoint 是自定义地址 -> `anthropic_compatible`
- `provider=bedrock` -> `bedrock`
- `provider=cloudflare` -> `cloudflare`
- `provider=openai` 且 endpoint 是 `https://router.mistermorph.com/api/v1` -> `mistermorph_pro`
- `provider=xai` -> `xai`
- `provider=deepseek` -> `deepseek`
- `provider=openai_resp` 且 endpoint 是 OpenAI 默认地址 -> `openai`
- `provider=openai_resp` 且 endpoint 是自定义地址 -> `openai_response_compatible`
- `provider=openai` 且 endpoint 是 OpenAI 默认地址 -> `openai`
- `provider=openai` 或 `provider=openai_custom`，且 endpoint 匹配 xAI、Deepseek、Kimi、OpenRouter、Groq 的已知地址 -> 对应直连供应商
- `provider=openai` 或 `provider=openai_custom`，且 endpoint 是其他自定义地址 -> `openai_chat_compatible`

V1 列表没覆盖到的旧 provider 不能被改写成其他协议。
这些配置应报错，并要求配置明确的 `llm.inference_provider`。

写配置时：

- 过渡期可以同时写 `inference_provider` 和派生出来的旧字段 `provider`/`endpoint`
- UI 仍把 `inference_provider` 当作用户侧主字段
- 运行时创建 client 前，总是解析出明确的 `provider`/`endpoint`

## 10) Console API 变化

Agent settings payload 需要包含 `inference_provider`：

- 顶层 `llm`
- `llm.profiles` 里的每个 profile
- list API 返回的动态 provider

Profile list response 建议包含：

- `name`
- `inference_provider`
- `provider`
- `model_name`
- `api_base`
- `source`：`config` 或 `state`

`source` 用来让 UI 区分静态配置和动态 state 配置。
动态 provider 不应被暗示为存储在 `config.yaml` 里。

## 11) 安全要求

`<file_state_dir>/internal/llm_overlay.yaml` 可能包含凭据或环境变量引用。

必须做到：

- 日志和 Console response 中隐藏 secret 值
- 保留现有环境变量展开行为
- 默认不要在 agent prompt 中提到这个文件
- 不新增让 agent 编辑这个文件的工具或 prompt 路径

该文件属于运行人员维护的配置。

## 12) 实施计划

1. 新增 Console slot registry，并渲染 `sidebar.bottom_left`。
2. 新增 `web/console/src/ext/slots/`，默认没有可见内容。
3. 在运行时结构、config reader、settings payload、profile 解析中加入 `llm.inference_provider`。
4. 新增推理供应商 registry，用于派生 `provider` 和 `endpoint`。
5. 新增从旧 `provider`/`endpoint` 反推 `inference_provider` 的逻辑。
6. 新增 `<file_state_dir>/internal/llm_overlay.yaml` loader，读取顶层 `llm_overlay`。
7. 把有效动态条目合并到运行时 LLM values，标记为 state 来源 profile。
8. 在没有 `llm_overlay.yaml` 显式 `llm_overlay.default`/`llm_overlay.main_loop` 时，把动态 profile 加入默认 route candidates。
9. 在 `llm_overlay.yaml` 显式配置 `llm_overlay.default`/`llm_overlay.main_loop` 时，用它覆盖默认 main route。
10. 调整 Console settings：显示推理供应商，隐藏原始 provider。
11. 实现完成后更新配置示例和用户文档。

## 13) 测试

需要覆盖：

- slot 组件缺失时，侧栏不出现额外可见元素
- 构建时存在 `SidebarBottomLeft.js` 时，侧栏左下角能渲染该组件
- 动态 provider 文件缺失时，route 行为不变
- 有效动态 provider 文件能加入 route candidates
- `llm_overlay.yaml` 中 `llm_overlay.default` 或 `llm_overlay.main_loop` 能覆盖默认 main route
- `llm_overlay.yaml` 同时设置 `llm_overlay.default` 和 `llm_overlay.main_loop` 时报错
- 动态 provider 文件无效时，创建运行时配置快照失败，并给出可定位错误
- 动态 profile 名和静态 profile 重名时报错
- `llm.inference_provider` 能正确派生 `provider` 和 `endpoint`
- 旧配置能正确反推出 `inference_provider`
- profile 级 `inference_provider` 能覆盖顶层值
- 显式 `llm.routes.main_loop` 只有在 `llm_overlay.yaml` 显式配置 `llm_overlay.default` 或 `llm_overlay.main_loop` 时才被覆盖

## 14) 完成定义

- Console 支持 `sidebar.bottom_left` 构建期 slot。
- 没有 slot 组件时，默认构建的界面保持不变。
- `<file_state_dir>/internal/llm_overlay.yaml` 可以加入有效 LLM route candidates。
- Console 和运行时都暴露 `inference_provider`。
- 用户不需要在 UI 里编辑原始 `provider`。
- 没有 `llm.inference_provider` 的旧配置可以正常启动并反推出兼容值。
- 实现完成后，`go test ./...` 和 Console 的 `pnpm build` 通过。

## 15) Checklist

- [ ] 定义 Go 侧 `inference_provider` registry，包含显示名、配置值、派生 `provider`、默认 `endpoint`、是否需要用户填写 API Base。
- [ ] 在 `llmutil.RuntimeValues` 和 `llmutil.ProfileConfig` 中加入 `InferenceProvider` 字段。
- [ ] 实现 `inference_provider` 到 `provider`/`endpoint` 的解析函数。
- [ ] 实现旧配置反推 `inference_provider` 的函数。
- [ ] 在 profile 解析流程中支持 profile 级 `inference_provider` 覆盖顶层值。
- [ ] 更新 `resolvedClientConfig`，确保创建 client 前拿到派生后的 `provider` 和 `endpoint`。
- [ ] 新增 `<file_state_dir>/internal/llm_overlay.yaml` loader，读取顶层 `llm_overlay`。
- [ ] 校验动态 provider 文件：YAML 格式、保留字段、profile 名、静态 profile 重名、route 引用目标。
- [ ] 把动态 provider 合并为 state 来源 profile。
- [ ] 在没有 `llm_overlay.yaml` 显式 `llm_overlay.default`/`llm_overlay.main_loop` 时，为隐式 main route 加入动态 candidates。
- [ ] 在 `llm_overlay.yaml` 显式配置 `llm_overlay.default`/`llm_overlay.main_loop` 时，用它覆盖默认 main route。
- [ ] 禁止 `llm_overlay.yaml` 同时配置 `llm_overlay.default` 和 `llm_overlay.main_loop`。
- [ ] 扩展 profile list/selection response，返回 `inference_provider` 和 `source`。
- [ ] 更新 Console settings payload，支持顶层和 profile 级 `inference_provider`。
- [ ] 更新 Console 推理供应商选项列表。
- [ ] 调整 Console LLM 表单：显示推理供应商，隐藏原始 `provider`。
- [ ] 对直连供应商隐藏 API Base；对三个 Compatible 供应商显示 API Base。
- [ ] 新增 `web/console/src/ext/slots/` 和 slot registry。
- [ ] 在 `AppSidebar` 渲染 `sidebar.bottom_left` slot。
- [ ] 为 slot 传入 `currentPath`、`selectedEndpointItem`、`locale`、`t`。
- [ ] 确认 slot 缺失时没有额外可见 DOM。
- [ ] 更新 `assets/config/config.example.yaml` 中的 LLM 配置注释。
- [ ] 更新 Console/VitePress 用户文档中关于 provider、API Base、profile list 的说明。
- [ ] 增加 Go 单测：`inference_provider` 派生、旧配置反推、profile 覆盖、动态 `llm_overlay` loader、route 覆盖规则。
- [ ] 增加 Console 测试或构建验证：slot registry、LLM 表单字段显示规则。
- [ ] 运行 `go test ./...`。
- [ ] 运行 `pnpm build`（目录：`web/console`）。
