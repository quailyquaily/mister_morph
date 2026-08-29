---
date: 2026-05-23
title: UI Slots 与 LLM Inference Provider
status: draft
---

# UI Slots 与 LLM Inference Provider

## 1) 目标

新增两个扩展点：

1. UI slot。
   - 默认不渲染任何内容。
   - 如果构建时发现指定文件名的 JavaScript Vue 组件，就把该组件渲染到对应 slot。
   - V1 先加一个 slot，位置在侧栏左下角。

2. `llm.inference_provider`。
   - 顶层 `llm` 和 `llm.profiles.*` 都支持。
   - UI 让用户选择推理供应商，不再让用户填写原始 `llm.provider`。
   - 运行时通过 inference provider registry 派生 `provider` 和 `endpoint`。

`llm_overlay.yaml` 不再支持。新增 LLM profile、route 或候选项时，直接写 `config.yaml`。
MisterMorph Pro 是一等推理供应商，通过 auth store 提供 subscription API key。

## 2) 术语

- `inference_provider`：用户看到的模型供应商，例如 OpenAI、Claude AI、Deepseek。
- `provider`：内部协议和客户端选择器，例如 `openai`、`openai_resp`、`anthropic`、`bedrock`。
- `API Base`：用户语义里的 API 基础地址；当前 YAML 配置字段名是 `endpoint`。

## 3) 非目标

- 不做任意运行时 JavaScript 插件系统。
- 不做 UI slot 热加载。
- 不新增 `llm_overlay.yaml`、`providers.yaml` 或其他运行时覆盖文件。
- 不删除 `llm.provider`；它继续作为运行时兼容字段存在。
- 本迭代不改变图像模型路由。

## 4) UI Slot 设计

V1 slot：

- slot id：`sidebar.bottom_left`
- 组件文件：`web/console/src/ext/slots/SidebarBottomLeft.js`
- 渲染位置：`AppSidebar` 内部，导航列表之后，靠侧栏左下角

构建期加载使用 slot registry：

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

## 5) `inference_provider` 字段

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

- 每个 profile 配置自己的 `inference_provider`
- profile 不读取顶层 provider、endpoint、凭据、model 或其他 LLM 字段
- resolver 根据 profile 自己的 `inference_provider` 派生 `provider` 和内置 `endpoint`

Console Settings UI 显示 `inference_provider`。
不再让用户填写原始 `provider`。

对内置直连供应商，UI 隐藏 API Base 字段。
对 Compatible 供应商，UI 显示 API Base，因为 endpoint 必须由用户提供。

## 6) 内置 `inference_provider`

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
| Kimi | `kimi` | `openai` | `https://api.moonshot.cn` |
| OpenRouter | `openrouter` | `openai` | `https://openrouter.ai/api/v1` |
| Groq | `groq` | `openai` | `https://api.groq.com/openai/v1` |
| Sakana AI | `sakana` | `sakana` | `https://api.sakana.ai/v1` |
| OpenAI Chat Compatible | `openai_chat_compatible` | `openai` | 用户填写 |
| OpenAI Response Compatible | `openai_response_compatible` | `openai_resp` | 用户填写 |
| Claude AI Compatible | `anthropic_compatible` | `anthropic` | 用户填写 |

Go 侧应有一个推理供应商 registry。
这个 registry 是唯一派生入口。
所有 `provider` 和 `endpoint` 都从 registry 中的 `inference_provider` 记录得出。
Console 前端可以镜像这份列表。

## 7) MisterMorph Pro Auth

`mistermorph_pro` 是一等推理供应商。
它不通过配置文件保存 API key。

登录成功后：

- OAuth session 写入 state dir 下的 `auth/pro-oauth.json`
- session 内保存 subscription API key
- runtime 解析到 `inference_provider=mistermorph_pro` 时，从 auth store 读取 subscription API key
- config 中不需要 `llm.api_key`

入口：

- CLI：`mistermorph auth pro login/status/logout`
- Console API：`/auth/pro/status`、`/auth/pro/login/start`、`/auth/pro/login/poll`、`/auth/pro/logout`
- Console UI：Settings 和 Setup 的 LLM provider 区域显示 Pro 登录入口

注销只删除本地 session，不代表撤销服务端授权或删除服务端 subscription API key。

## 8) 兼容旧配置

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
- `provider=openai` 且 endpoint 匹配 xAI、Deepseek、Kimi、OpenRouter、Groq、Sakana AI 的已知地址 -> 对应直连供应商
- `provider=openai` 且 endpoint 是其他自定义地址 -> `openai_chat_compatible`

V1 列表没覆盖到的旧 provider 不能被改写成其他协议。
这些配置应报错，并要求配置明确的 `llm.inference_provider`。

## 9) Console API 变化

Agent settings payload 需要包含 `inference_provider`：

- 顶层 `llm`
- `llm.profiles` 里的每个 profile

Profile list response 建议包含：

- `name`
- `inference_provider`
- `provider`
- `model_name`
- `api_base`

## 10) 实施计划

1. 新增 Console slot registry，并渲染 `sidebar.bottom_left`。
2. 新增 `web/console/src/ext/slots/`，默认没有可见内容。
3. 在运行时结构、config reader、settings payload、profile 解析中加入 `llm.inference_provider`。
4. 新增推理供应商 registry，用于派生 `provider` 和 `endpoint`。
5. 新增从旧 `provider`/`endpoint` 反推 `inference_provider` 的逻辑。
6. 调整 Console settings：显示推理供应商，隐藏原始 provider。
7. 实现 MisterMorph Pro auth store 和 UI/API/CLI 登录入口。
8. 实现完成后更新配置示例和用户文档。

## 11) 测试

需要覆盖：

- slot 组件缺失时，侧栏不出现额外可见元素
- 构建时存在 `SidebarBottomLeft.js` 时，侧栏左下角能渲染该组件
- `llm.inference_provider` 能正确派生 `provider` 和 `endpoint`
- 旧配置能正确反推出 `inference_provider`
- profile 只根据自己的 `inference_provider` 派生 provider 和 endpoint
- MisterMorph Pro 只从 auth store 读取 subscription API key

## 12) 完成定义

- Console 支持 `sidebar.bottom_left` 构建期 slot。
- 没有 slot 组件时，默认构建的界面保持不变。
- Console 和运行时都暴露 `inference_provider`。
- 用户不需要在 UI 里编辑原始 `provider`。
- 没有 `llm.inference_provider` 的旧配置可以正常启动并反推出兼容值。
- `llm_overlay.yaml` 不再被读取，也不作为支持的配置入口。
- 实现完成后，`go test ./...` 和 Console 的 `pnpm build` 通过。

## 13) Checklist

- [ ] 定义 Go 侧 `inference_provider` registry，包含显示名、配置值、派生 `provider`、默认 `endpoint`、是否需要用户填写 API Base。
- [ ] 在 `llmutil.RuntimeValues` 和 `llmutil.ProfileConfig` 中加入 `InferenceProvider` 字段。
- [ ] 实现 `inference_provider` 到 `provider`/`endpoint` 的解析函数。
- [ ] 实现旧配置反推 `inference_provider` 的函数。
- [ ] 在 profile 解析流程中根据 profile 自己的 `inference_provider` 派生 provider 和 endpoint。
- [ ] 更新 `resolvedClientConfig`，确保创建 client 前拿到派生后的 `provider` 和 `endpoint`。
- [ ] 扩展 profile list/selection response，返回 `inference_provider`。
- [ ] 更新 Console settings payload，支持顶层和 profile 级 `inference_provider`。
- [ ] 更新 Console 推理供应商选项列表。
- [ ] 调整 Console LLM 表单：显示推理供应商，隐藏原始 `provider`。
- [ ] 对直连供应商隐藏 API Base；对 Compatible 供应商显示 API Base。
- [ ] 新增 `web/console/src/ext/slots/` 和 slot registry。
- [ ] 在 `AppSidebar` 渲染 `sidebar.bottom_left` slot。
- [ ] 为 slot 传入 `currentPath`、`selectedEndpointItem`、`locale`、`t`。
- [ ] 确认 slot 缺失时没有额外可见 DOM。
- [ ] 更新 `assets/config/config.example.yaml` 中的 LLM 配置注释。
- [ ] 更新 Console/VitePress 用户文档中关于 provider、API Base、profile list 的说明。
- [ ] 增加 Go 单测：`inference_provider` 派生、旧配置反推、profile 独立解析。
- [ ] 增加 Console 测试或构建验证：slot registry、LLM 表单字段显示规则。
- [ ] 新增 MisterMorph Pro auth store，保存 state dir 下的 `auth/pro-oauth.json`。
- [ ] 新增 `mistermorph auth pro login/status/logout`。
- [ ] 新增 Console `/auth/pro/*` API 和 Settings/Setup 登录入口。
- [ ] runtime 解析 `mistermorph_pro` 时从 auth store 读取 subscription API key。
- [x] 删除 `llm_overlay.yaml` loader 和相关测试。
- [ ] 运行 `go test ./...`。
- [ ] 运行 `pnpm build`（目录：`web/console`）。
