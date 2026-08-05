---
date: 2026-04-24
title: Codex OAuth Provider
status: draft
---

# Codex OAuth Provider

## 1) 背景

`mistermorph` 通过 `llm.inference_provider` 选择模型服务，并保留 `llm.provider` 兼容旧配置。OpenAI 方向已经支持普通 API key 和 OpenAI-compatible endpoint，但还不支持 Codex CLI 类似的 ChatGPT OAuth 登录。

OpenAI 官方 Codex CLI 文档写明：第一次运行 Codex CLI 时，可以用 ChatGPT 账号或 API key 登录。官方模型文档也把 GPT-5.3-Codex 描述为面向 Codex 或类似 coding agent 环境的模型，并标出 Responses API 支持。

这说明从产品形态看，Codex 类模型和 coding agent 环境是匹配的。但这不等于 OpenAI 已经把“第三方客户端复用 Codex OAuth grant”定义成稳定公开接口。这个需求必须按实验性能力处理。

外部背景：

- OpenClaw、Hermes 已经做了类似 provider。
- 之前检查没有发现它们在 system prompt 里伪装自己是 Codex CLI；主要是认证和请求兼容。
- Sam 的公开表态可以作为方向信号，但不能作为接口稳定性、授权范围或安全边界的依据。

## 2) 可行性

这个需求可行，但必须收窄范围。

推荐先实现一个显式 opt-in 的实验性 provider：

```yaml
llm:
  inference_provider: openai_codex
  model: gpt-5.5
```

`openai_codex` 的含义是：

- `llm.endpoint` 和 `llm.api_key` 同时非空时，使用 API Key bearer token；否则使用 Codex OAuth。
- 默认走 Codex/Responses 兼容请求形态。
- `llm.endpoint` 为空时使用 Codex 默认 endpoint；非空时使用配置值。
- 保持 `mistermorph` 自己的 agent 身份，不在 prompt 里自称 Codex CLI。
- 不承诺免费额度、特殊额度或绕过 OpenAI 的正常计费与限流。

当前 `uniai` 已提供 `openai_codex` provider，负责 Codex 请求字段兼容和 Responses 解析。MisterMorph 的 wrapper 只保留认证选择、OAuth account header、顶层 `instructions`、`store: false` 和指令长度限制。

## 3) 目标

第一版要做到：

1. 新增 provider：`openai_codex`。
2. 新增本地登录命令：`mistermorph auth codex login`。
3. 新增状态命令：`mistermorph auth codex status`。
4. 新增退出命令：`mistermorph auth codex logout`。
5. Console Web Settings 支持 Codex OAuth 登录、状态查看和退出。
6. token 保存到 `<file_state_dir>/auth/codex.json`。
7. token 文件权限限制为当前用户可读写。
8. access token 过期前自动 refresh。
9. `llm.profiles` 和 `llm.routes` 可以使用 `openai_codex`。
10. 日志、错误、stats、Console API 中不能输出 access token、refresh token、authorization code。
11. system prompt 不因为该 provider 改成 Codex CLI 身份。

## 4) 非目标

第一版不做这些事：

1. 不做共享 token 的远端多用户 OAuth。Console 可以管理所选 remote endpoint 自己的单一 token store。
2. 不自动读取或复用官方 Codex CLI 的本地凭证。
3. 不在 system prompt、developer prompt 或工具描述里伪装成 Codex CLI。
4. 不承诺 OpenAI 会长期保持当前 Codex OAuth 行为。
5. 不实现服务端撤销 OAuth grant。
6. 不自动删除 OpenAI 侧已经生成的 API key。
7. 不改现有 `openai`、`openai_resp`、`openai_custom` 行为。
8. 不把 Codex OAuth 设为默认 provider。

其中第 2 点是安全边界。意思是：不自动读取 `~/.codex` 或其他官方 Codex CLI 保存的本地 token。官方 Codex CLI 的本地凭证属于另一个应用。即使技术上可以读，也不应该默认读，否则用户可能以为只是在使用 `mistermorph`，实际却复用了另一个应用的高权限凭证。

如果后续要支持导入，必须单独加显式命令，例如 `mistermorph auth codex import-cli-token`，并在命令输出里说明风险。

## 5) 用户流程

### 5.1 登录

用户执行：

```bash
mistermorph auth codex login
```

如果用户希望登录成功后直接把默认 LLM provider 切到 Codex OAuth，可以显式加参数：

```bash
mistermorph auth codex login --set-default
```

命令行为：

1. 向 OpenAI OAuth/device auth 端点发起登录。
2. 在终端打印登录 URL 和 user code。
3. 用户在浏览器完成授权。
4. 命令轮询 token 结果。
5. 成功后写入 `<file_state_dir>/auth/codex.json`。
6. 如果带了 `--set-default`，把默认 `llm.inference_provider` 和兼容字段 `llm.provider` 改成 `openai_codex`，并清掉旧 endpoint、API key、Azure、Bedrock 和 Cloudflare 字段。
7. 如果没有带 `--set-default`，但当前 LLM credential 配置为空，也自动写入 `openai_codex`。

第 7 点里的“空”定义为：

- 非 Cloudflare provider：没有配置 `llm.endpoint`，也没有配置 `llm.api_key`。
- Cloudflare provider：没有配置 `llm.cloudflare.account_id`，也没有配置 `llm.cloudflare.api_token`。

登录成功输出只显示：

- 登录状态
- token 大致过期时间
- 存储位置使用 `<file_state_dir>` 表示
- 是否更新了配置文件

不能输出真实 token。

### 5.2 使用

配置示例：

```yaml
llm:
  inference_provider: openai_codex
  model: gpt-5.5
  request_timeout: "120s"
```

profile 示例：

```yaml
llm:
  inference_provider: openai
  api_key: "${OPENAI_API_KEY}"
  model: gpt-5.5

  profiles:
    codex:
      inference_provider: openai_codex
      model: gpt-5.5

  routes:
    main_loop:
      profile: codex
```

### 5.3 状态

用户执行：

```bash
mistermorph auth codex status
```

输出：

- 是否已登录
- access token 是否已过期
- refresh token 是否存在
- token 文件是否权限过宽

不输出 token 内容。

### 5.4 退出

用户执行：

```bash
mistermorph auth codex logout
```

命令只删除本地 token 文件。

需要明确提示：本地 logout 不等于 OpenAI 侧撤销授权，也不等于删除 OpenAI 侧可能已经生成的 API key。用户如需完全撤销，需要去 OpenAI/ChatGPT 相关设置和 API dashboard 手动处理。

### 5.5 Console Web 登录

Console Web 第一版也要支持登录。

入口建议放在 Settings 的 LLM 配置区域：

- 当 inference provider 选择 `openai_codex` 时，显示 Codex OAuth 状态卡片。
- Local 和 remote endpoint 使用同一个入口；remote 请求转发到所选 endpoint 的 Codex OAuth API。
- 有效 endpoint 和 API Key 同时非空时，OAuth 按钮继续显示，但 disabled。
- 未登录时显示 `Sign in with OpenAI Codex`。
- 登录中显示 user code、登录 URL、过期时间和轮询状态。
- 已登录时显示 token 状态、过期时间和 `Logout`。
- 进入 LLM Settings 时，如果 access token 已过期但仍保存 refresh token，后端静默尝试续期。续期成功后显示状态按钮。
- refresh token 被明确拒绝、撤销、过期或重复使用时，删除失效 token，并显示登录按钮。
- token 不存在时显示登录按钮。前端不显示单独的 `Refreshable` 状态。
- 状态按钮和登录按钮都可打开 OAuth 对话框。只有 endpoint 和 API Key 同时非空的 Bearer 模式会禁用 OAuth 按钮。

Console Web 登录流程：

1. 前端调用当前所选 endpoint 的 start API。
2. 后端发起 device auth，保存短期登录 session。
3. 后端只把登录 URL、user code、过期时间、轮询间隔和 opaque session id 返回给前端。
4. 前端打开登录 URL 或提供复制按钮。
5. 前端按轮询间隔调用 poll API。
6. 后端轮询 OpenAI 端点，成功后把 token 写入 `<file_state_dir>/auth/codex.json`。
7. 前端刷新状态。

Web 登录只更新所选 endpoint 的 OAuth token，不改 LLM 设置。用户仍通过当前表单保存 inference provider、endpoint、API key 和 model。这样从 profile 发起登录不会改默认 LLM，自定义 endpoint 也不会在登录后丢失。

登录 session 绑定发起登录时的 endpoint。用户切换 endpoint 后，前端停止旧 session 的轮询，不能把旧 endpoint 的状态应用到新 endpoint。

Console Web 不应该拿到：

- access token
- refresh token
- authorization code
- device auth 内部 id
- code verifier

这些值只应存在于后端内存或 token store 中。

建议 Console API：

```text
GET  /api/auth/codex/status
POST /api/auth/codex/refresh
POST /api/auth/codex/login/start
POST /api/auth/codex/login/poll
POST /api/auth/codex/logout
```

这些 API 必须走现有 Console session 鉴权。未登录 Console 的请求返回 `401`。

Remote daemon 暴露相同语义的 `/auth/codex/*` 路由，使用 daemon bearer token 鉴权。Console 的 `/proxy` 只负责转发；Local Console 和 remote daemon 复用同一个 Codex OAuth HTTP handler。

## 6) 配置需求

配置使用 inference provider 名称：

```yaml
llm:
  inference_provider: openai_codex
  model: gpt-5.5
```

默认规则：

- token 文件：`<file_state_dir>/auth/codex.json`
- endpoint：`llm.endpoint` 为空时使用 Codex 默认 endpoint
- API Key：`llm.endpoint` 和 `llm.api_key` 同时非空时使用；其他情况使用 OAuth
- model：沿用 `llm.model`
- headers：仍允许通过 `llm.headers` 加额外 header，但不能覆盖 `Authorization`
- OAuth client id 和 device auth endpoint：内置在代码中，作为实验性实现细节，不放进默认配置模板

API Key 模式示例：

```yaml
llm:
  inference_provider: openai_codex
  endpoint: "https://codex.example.com"
  api_key: "${CODEX_API_KEY}"
  model: gpt-5.5
```

只配置其中一项不会启用 API Key 模式，也不会在 OAuth 失败后自动切换认证方式。

## 7) 实现边界

建议新增一个小的 auth 包，职责只包含：

- 发起 device auth 登录
- 轮询授权结果
- 交换 access token / refresh token
- 刷新 access token
- 读写 token store
- 校验 token store 文件权限

不要把 LLM request 逻辑放进 auth 包。

Console Web 需要复用同一个 auth 包，不另写一套 OAuth 逻辑。

Console 后端需要再加一层短期 login session store：

- 保存 device auth 内部 id、code verifier、过期时间和轮询状态。
- 对前端只暴露 opaque session id。
- session 过期后自动清理。
- 进程重启后登录中 session 丢失是可接受行为，用户重新点登录即可。

client 创建路径：

1. `llmutil` 解析出 provider 为 `openai_codex`。
2. endpoint 和 API Key 同时非空时直接使用 API Key；否则调用 Codex auth resolver 获取 OAuth access token。
3. 创建 Codex wrapper，内部使用 `uniai` 的 `openai_codex` provider。
4. OAuth 模式发送 token 中的 account header；API Key 模式不发送该 header。
5. wrapper 添加顶层 `instructions`、`store: false` 和指令长度限制；协议字段兼容由 `uniai` 处理。

这个 provider 不应该改 agent 层 prompt。agent 仍然是 `mistermorph`，不是 Codex CLI。

### 7.1 技术验证结论

2026-04-24 做了代码和 fake server 验证。

本地验证结果：

- `uniai` 的 `openai_resp` 使用 OpenAI Go SDK 的 Responses client。
- `OpenAIAPIBase` 可以设置成非 `api.openai.com` 的 base URL。
- 当 base URL 是 `<server>/backend-api/codex` 时，实际请求路径是 `POST /backend-api/codex/responses`。
- `OpenAIAPIKey` 会变成 `Authorization: Bearer <token>`。
- `ChatHeaders` 会附加到请求上。

这说明 `uniai openai_codex` 的传输层、bearer token 和路径拼接可以复用。

不能直接复用的原因：

- Codex backend 不是标准 OpenAI Platform Responses API 的完全等价实现。
- 外部实测显示 `https://chatgpt.com/backend-api/codex/responses` 要求 `instructions` 字段存在且非空。
- 外部实测显示 `instructions` 字段过大时会返回 `400 Bad Request`。
- 外部实测显示 `input` 必须是 message array，不能是普通字符串。
- 外部实测显示 `store` 必须是 `false`。
- 外部实测显示 `stream` 必须是 `true`。
- `mistermorph` 的 CLI、Telegram、Slack、LINE、Lark 路径默认不传 `OnStream`，所以不能依赖调用方天然走 streaming。
- `mistermorph` 当前会把 system message 放进 input message list，但 Codex backend 还要求顶层 `instructions`。

当前实现按这个方向收敛：

1. OAuth wrapper 内部使用 `uniai` 的 `openai_codex` provider。
2. endpoint 默认是 `https://chatgpt.com/backend-api/codex`，允许通过 `llm.endpoint` 覆盖。
3. endpoint 和 API Key 同时非空时使用 API Key bearer；否则使用 OAuth。
4. 固定 `store: false`。
5. 强制使用 streaming；调用方没有 `OnStream` 时，provider 内部用 no-op stream handler 触发 `uniai` streaming 路径。
6. 从 system messages 生成顶层 `instructions`，并避免重复发送同一份 system prompt。
7. 对 `instructions` 做大小限制；超过限制时放回 input 或走压缩策略。
8. 保留 tool calling 和流式解析的兼容测试。

结论：MisterMorph wrapper 负责认证选择和 agent 请求边界；协议字段兼容和 Responses 解析由 `uniai openai_codex` 负责。

## 8) 安全要求

Codex OAuth refresh token 是高价值 secret。实现必须遵守：

1. token 文件使用 `0600`。
2. token 目录使用 `0700`。
3. 日志、错误、debug 输出全部做 token redaction。
4. `mistermorph auth codex status` 只展示状态，不展示 secret。
5. refresh 失败时 fail closed，不回退到空 token 或匿名请求。
6. provider 错误信息要能区分本地未登录、token 过期、refresh 失败、OpenAI 拒绝访问。
7. 不默认导入 Codex CLI 凭证。
8. 不在文档里暗示这是 OpenAI 官方支持的第三方 OAuth 集成。
9. Console API 只返回状态和登录辅助信息，不返回 OAuth secret。
10. Console Web 的登录 session id 只代表一次短期登录轮询，不能用于读取 token。

官方帮助文档里对 Codex CLI 登录权限的描述很重：相关授权可能让 CLI 获取 refresh token，并进一步生成 API key、消耗 credits、管理 API organization。实现和文档都必须把它当作高权限凭证处理。

## 9) 失败模式

需要给出清晰错误：

- 未登录：提示运行 `mistermorph auth codex login`
- refresh token 缺失：提示重新登录
- refresh 暂时失败：保留原 token，返回错误，允许后续请求重试
- refresh token 被明确拒绝、撤销、过期或重复使用：删除失效 token，提示重新登录
- 401 / 403：提示检查 ChatGPT/Codex 账号权限、workspace 限制和 OpenAI 侧授权状态
- endpoint schema 变化：明确报错，提示 provider 需要升级，不自动降级到 OpenAI API key，不自动切换 endpoint，不输出原始 token
- 非交互环境登录：login 命令应失败并说明需要交互式终端或浏览器

## 10) 测试需求

单元测试：

1. token store 写入权限。
2. token store 读取和缺字段错误。
3. access token 过期判断。
4. refresh 成功后原子写回。
5. refresh 暂时失败不清空原 token。
6. refresh token 被永久拒绝后删除失效 token。
7. provider 名称 `openai_codex` 可以被 route/profile 解析。
8. `Authorization` 不能被 `llm.headers` 覆盖。
9. logger redaction 覆盖 access token、refresh token、authorization code。
10. prompt 构造不因 provider 变化而加入 Codex CLI 身份。

集成测试：

1. 使用 fake OAuth server 跑完整 login polling。
2. 使用 fake Codex/Responses server 验证 request header 和 body。
3. `mistermorph auth codex status` 不泄露 secret。
4. `mistermorph auth codex logout` 只删除本地 token 文件。
5. Console start API 不返回 token、authorization code、device auth 内部 id 或 code verifier。
6. Console poll API 成功后只返回状态，不返回 token。
7. 未登录 Console session 时，Codex OAuth Console API 返回 `401`。

手工测试：

1. 登录真实 OpenAI 账号。
2. 用 `openai_codex` 跑一个最小任务。
3. 模拟 access token 过期，确认自动 refresh。
4. 删除 token 文件后确认错误可读。

## 11) 验收标准

第一版完成时应满足：

1. `mistermorph auth codex login/status/logout` 可用。
2. Console Web Settings 可完成 Codex OAuth login/status/logout。
3. `llm.inference_provider: openai_codex` 可跑通 main loop。
4. profile 和 route 可选择 `openai_codex`。
5. 未登录时错误信息可直接指导用户下一步。
6. token 不出现在日志、错误、stats、Console API 返回里。
7. system prompt 不出现 Codex CLI 身份伪装。
8. `go test ./...` 通过。
9. `pnpm build` 通过。
10. 文档明确标注这是实验性 provider。

## 12) 需要实现前确认的问题

已确认：

1. provider 名称定为 `openai_codex`。
2. 内置 Codex OAuth client id 和 device auth endpoint。
3. 第一版包含 Console Web 登录。
4. 不默认读取官方 Codex CLI 的本地凭证。
5. `uniai openai_resp` 可以复用底层传输，但需要 Codex 兼容层。
6. 示例和默认建议模型使用 `gpt-5.5`。
7. OpenAI 修改 Codex OAuth 流程或 backend schema 时，provider 明确报错，不自动降级。

当前没有待确认项。

## 13) 参考资料

- OpenAI Codex CLI: https://developers.openai.com/codex/cli
- OpenAI GPT-5.3-Codex model: https://developers.openai.com/api/docs/models/gpt-5.3-codex
- OpenAI codex-mini-latest model: https://developers.openai.com/api/docs/models/codex-mini-latest
- OpenAI Help Center, Codex CLI and Sign in with ChatGPT: https://help.openai.com/en/articles/11381614
- OpenClaw OpenAI provider docs: https://docs.openclaw.ai/providers/openai
- OpenClaw model providers docs: https://docs.openclaw.ai/concepts/model-providers
- OpenClaw issue on Codex request shape: https://github.com/openclaw/openclaw/issues/67740
- OpenClaw issue on Codex instructions size: https://github.com/openclaw/openclaw/issues/57930
- OpenClaw issue on Codex Cloudflare 403: https://github.com/openclaw/openclaw/issues/62142
- Hermes issue on Codex stream backfill: https://github.com/NousResearch/hermes-agent/issues/5883

## 14) 任务拆分和实际情况

- [x] 创建 `feat/codex_oauth` 分支。
- [x] 在 `docs/feat` 增加需求文档。
- [x] 验证 `uniai openai_resp` 的传输、base URL、bearer token 和 header 复用方式。
- [x] 增加 Codex OAuth auth 包，支持 device login、poll、refresh 和 token 解析。
- [x] 增加本地 token store，使用 `0700` 目录和 `0600` 文件权限。
- [x] 增加 `mistermorph auth codex login/status/logout`。
- [x] `auth codex login --set-default` 支持登录后把默认 inference provider 写成 `openai_codex`。
- [x] 当前 LLM credential 配置为空时，`auth codex login` 自动写入 `openai_codex`。
- [x] 增加 `openai_codex` provider 兼容层，不改 agent 身份 prompt。
- [x] 支持从 system/developer messages 生成顶层 `instructions`，并避免重复发送。
- [x] 限制顶层 `instructions` 大小，超出部分放回 input message，避免 Codex backend 返回 `400 Bad Request`。
- [x] 禁止用户配置 header 覆盖 `Authorization`。
- [x] Codex backend 默认使用官方 endpoint，并允许显式配置自定义 endpoint。
- [x] endpoint 和 API Key 同时非空时使用 API Key bearer，否则使用 OAuth。
- [x] 对进程内 token refresh 做串行处理，避免并发使用同一个 refresh token。
- [x] 对齐 Codex HTTP 请求形态：不在 HTTP Responses 请求上发送 WebSocket beta header，也不发送 prompt cache 参数。
- [x] 由 `uniai openai_codex` 过滤显式 cache、temperature、token limit 和 reasoning budget 字段。
- [x] 由 `uniai openai_codex` 处理 JSON object mode 和 `JSON` 上下文要求。
- [x] 升级到 `uniai v0.1.20`，使用上游 `openai_resp` streaming result 累积逻辑。
- [x] 移除 Codex provider 内本地 streaming 文本回填代码，避免重复实现上游协议解析。
- [x] 复核 Codex OAuth 实现，移除无用 Config 字段、OAuth 状态字段和 Console 前端临时状态。
- [x] 让 `uniai` 透传 OpenAI raw options，用于设置 `store: false` 等请求参数。
- [x] 修正 Codex backend base URL，不追加标准 OpenAI `/v1` 路径。
- [x] 支持 `llm.profiles` 和 `llm.routes` 使用 `openai_codex`。
- [x] Console 后端增加 Codex OAuth status、refresh、start、poll、logout API。
- [x] Console API 只返回状态和登录辅助信息，不返回 OAuth secret。
- [x] Console Web Settings 增加 Codex OAuth 登录、状态和退出 UI。
- [x] Console Web Settings 将 Codex OAuth 操作路由到当前 endpoint，不区分 local 和 remote。
- [x] Console Web 登录只更新当前 endpoint 的 OAuth token，不改 LLM 表单设置。
- [x] 环境变量托管 inference provider 时仍显示 OAuth 操作。
- [x] Endpoint 和 API Key 同时非空时保留并禁用 OAuth 按钮。
- [x] 进入 LLM Settings 时静默续期过期的 access token，并将失效 refresh token 转成登录状态。
- [x] `openai_codex` 选择后显示可选 endpoint 和 API Key，并隐藏无关 credential 输入。
- [x] 更新默认配置模板中的 provider 说明。
- [x] 增加 auth、provider、base URL 相关单元测试。
- [x] `pnpm build` 已通过。
- [x] `go test ./...` 最终通过。
- [ ] 真实 OpenAI 账号登录 smoke test；需要用户在浏览器完成授权。
- [ ] 真实 `openai_codex` 最小任务 smoke test；需要可用账号和网络。
- [ ] 真实 token refresh smoke test；需要可控的过期 token 或等待 token 到期。
