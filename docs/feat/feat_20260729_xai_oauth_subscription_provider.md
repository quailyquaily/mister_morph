---
date: 2026-07-29
title: xAI Grok Subscription OAuth Provider
status: in_progress
---

# xAI Grok Subscription OAuth Provider

实现状态：认证、provider、CLI、Console 和配置路径已经实现。默认使用 xAI 的 shared public OAuth client，并申请登录、刷新和推理所需的最小 scope。正式发布前仍需用符合条件的真实账户验证 entitlement、Responses、图片、tools 和 refresh。

## 1) 背景

MisterMorph 已经支持两种相关能力：

- `xai`：使用 `XAI_API_KEY` 调用 xAI API，按 API 账户计费。
- `openai_codex`：通过设备授权登录，使用订阅账户的 OAuth token。

xAI 已经公开支持用户把 Grok 订阅用于第三方 agent。xAI 在 2026-05-15 宣布 Hermes Agent 可以通过 xAI Grok OAuth 使用 Grok 订阅；xAI Grok Build 也支持浏览器登录和 RFC 8628 device code 登录。Hermes 的当前实现表明，授权完成后可以用 OAuth bearer token 调用 xAI Responses API，并在过期前刷新 token。

OpenClaw 当前使用 xAI 的 shared public OAuth client，授权页可能因此显示 Grok Build。MisterMorph 使用同一个 public client，但不复制 OpenClaw 的 token、身份资料或 credential store。public client id 不是 secret；真正的安全边界是固定认证地址、最小 scope、本地 token store 和固定推理地址。

## 2) 结论

新增一个独立 provider：

```yaml
llm:
  inference_provider: xai_oauth
  provider: xai_oauth
  model: grok-4.5
```

用户界面名称：

```text
xAI Grok OAuth
```

它与现有 `xai` 的区别如下：

| provider | 凭据 | 用量归属 | endpoint |
| --- | --- | --- | --- |
| `xai` | `XAI_API_KEY` | xAI API 账户 | xAI API |
| `xai_oauth` | xAI OAuth access/refresh token | Grok 订阅或 xAI 为该 OAuth 客户端定义的额度 | 由 xAI 为该客户端确认并固定 |

两者必须显式区分，不能按凭据是否存在自动切换。这样可以避免用户以为请求消耗订阅额度，实际却产生 API Key 费用。

首版只做原生 LLM provider，不通过 Grok CLI、ACP 或子进程转发。MisterMorph 仍运行自己的 agent loop、system prompt 和工具。

## 3) 上线前验证

正式发布前必须完成以下事项：

1. 验证 shared public client 仍允许 device authorization grant。
2. 验证 scope：`openid profile offline_access grok-cli:access api:access`。
3. 确认 OAuth bearer token 使用 `https://api.x.ai/v1`。
4. 确认哪些订阅档位具有推理 entitlement。
5. 用符合条件的真实账户验证：
   - device code 登录；
   - token refresh；
   - `grok-4.5` Responses 请求；
   - 流式输出；
   - 文本与图片混合输入；
   - function calling；
   - 多轮工具调用；
   - 订阅额度耗尽时的错误。

MisterMorph 是本地应用，不保存 OAuth client secret。若 xAI 停止允许 shared public client，后续应申请 MisterMorph 自己的 public client；不能把 confidential client secret 编译进二进制或前端。

## 4) 目标

第一版需要做到：

1. 新增 `xai_oauth` inference provider 和 protocol provider。
2. 支持 CLI 登录、状态查询和本地退出。
3. 支持 Console Setup 和 Settings 中的登录、状态查询和退出。
4. 支持 access token 自动刷新和 refresh token 轮换。
5. 支持普通对话、图片理解、流式输出、reasoning、结构化输出和 MisterMorph function tools。
6. 支持 `llm.profiles`、`llm.routes`、route fallback 和连接测试。
7. 支持 CLI、Console、Telegram、Slack、LINE、Lark 等现有 runtime。
8. token 只保存在后端本地，不进入浏览器、日志、请求 dump、stats 或模型上下文。
9. OAuth 请求只能发往固定的 xAI 认证主机，OAuth bearer token 只能发往固定的 xAI 推理主机。
10. stats 记录 token 用量，但不把公开 API 单价显示为订阅实际费用。

## 5) 非目标

第一版不做：

1. 不自动读取 Grok Build、Hermes 或其他应用的本地 token。
2. 不导入浏览器 cookie。
3. 不导入其他应用的 token、cookie 或 credential store。
4. 不把现有 `xai` API Key provider 改成 OAuth。
5. 不在 OAuth 失败时自动回退到 `XAI_API_KEY`。
6. 不自动选择更高订阅档位或购买额外用量。
7. 不实现图片生成、Grok TTS、视频、音频/视频输入、文件上传、X Search 或 xAI server-side tools。文本与图片混合输入属于首版范围。
8. 不把 MisterMorph 的身份改成 Grok Build、Hermes 或其他 agent。
9. 不为这一个功能建立通用 OAuth framework。
10. 不把 Codex OAuth 和 xAI OAuth 合并成同一套协议代码。两者的 endpoint、响应格式、scope 和错误语义不同。
11. 不允许用户为 `xai_oauth` 配置任意推理 endpoint 或 `Authorization` header。
12. 不承诺任意 X Premium 档位都能使用。最终 entitlement 由 xAI 返回结果决定。

## 6) 订阅和账户要求

xAI 官方说明允许把 X 账户连接到 xAI 账户，xAI 会读取 X subscription status 并授予相应权益。用户流程需要明确：

1. 用户登录的是 xAI 账户，不是把 X cookie 交给 MisterMorph。
2. 使用 X 订阅时，需要先在 Grok/xAI 账户中连接对应的 X 账户。
3. UI 文案使用“需要符合条件的 Grok 订阅”，不能笼统写“X Premium 即可”。
4. 当前可验证的第三方说明包括 SuperGrok 和已连接的 X Premium+；普通 X Premium 是否可用不能由 MisterMorph 保证。
5. 登录成功只说明身份认证成功，不说明该账户一定有模型推理 entitlement。
6. entitlement 不足、地区限制、团队策略或订阅额度耗尽必须作为独立错误显示。

## 7) 用户流程

### 7.1 CLI 登录

```bash
mistermorph auth xai login
```

行为：

1. 从固定的 xAI issuer 读取 OIDC discovery document。
2. 校验 discovery 中的 issuer 和所有 OAuth endpoint。
3. 请求 device code。
4. 在终端显示 verification URL、user code 和到期时间。
5. 按服务端返回的 interval 轮询 token endpoint。
6. 正确处理 `authorization_pending`、`slow_down`、`access_denied` 和 `expired_token`。
7. 成功后原子写入 `<file_state_dir>/auth/xai.json`。
8. 不自动修改当前 LLM 配置。

用户显式要求设为默认时：

```bash
mistermorph auth xai login --set-default
```

登录成功后：

- `llm.inference_provider` 设为 `xai_oauth`；
- `llm.provider` 设为 `xai_oauth`；
- `llm.model` 设为 `grok-4.5`；
- 清空默认 LLM 的 endpoint、API key、Cloudflare 和 Bedrock credential 字段；
- 不修改 named profiles。

如果已有 `llm.model`，`--set-default` 仍统一改成 `grok-4.5`，避免保留一个属于其他 provider 的模型名。用户可以在登录后再选择其他已获 entitlement 的 xAI 模型。

### 7.2 CLI 状态

```bash
mistermorph auth xai status
```

输出只包含：

- 是否存在可用登录；
- access token 是否过期；
- refresh token 是否存在；
- token 大致过期时间；
- token 文件权限是否安全；
- 最近一次 entitlement 检查结果和时间，如该检查已经发生。

不得输出 token、账户邮箱或完整账户标识。

`status` 是只读操作，不调用 token endpoint，也不发起模型请求。access token 已过期但 refresh token 存在时，状态显示“可自动刷新”；真正使用 provider 时再刷新。

### 7.3 CLI 退出

```bash
mistermorph auth xai logout
```

行为：

1. 尽力调用 discovery 中的 revocation endpoint 撤销 refresh token。
2. 无论远端撤销成功与否，都删除本地 token 文件。
3. 远端撤销失败时给出明确提示，但不能把 token 打印出来。
4. 不修改 `llm` 配置。

### 7.4 Console

Setup 和 Settings 中增加 `xAI Grok OAuth` 登录入口，交互与现有 Codex device auth 保持一致：

1. 后端创建短期登录 session。
2. 前端只拿到 opaque session id、verification URL、user code、轮询间隔和过期时间。
3. 前端按后端给出的间隔轮询。
4. token 交换、保存和刷新全部发生在后端。
5. 登录完成后可以选择“设为默认”。
6. 已登录状态提供“重新登录”和“退出登录”。

建议 API：

```text
GET  /api/auth/xai/status
POST /api/auth/xai/login/start
POST /api/auth/xai/login/poll
POST /api/auth/xai/logout
```

这些 API 使用现有 Console session 鉴权。API 响应不得包含：

- access token；
- refresh token；
- device code；
- OAuth client id；
- discovery 原文；
- xAI 内部账户标识。

## 8) 配置行为

默认配置示例：

```yaml
llm:
  inference_provider: xai_oauth
  provider: xai_oauth
  model: grok-4.5
  reasoning_effort: high
```

profile 和 route 示例：

```yaml
llm:
  inference_provider: openai
  provider: openai_resp
  model: gpt-5.4

  profiles:
    grok:
      inference_provider: xai_oauth
      provider: xai_oauth
      model: grok-4.5
      reasoning_effort: high

  routes:
    main_loop:
      profile: grok
      fallback_profiles: ["default"]
```

规则：

1. `xai_oauth` 不读取 `llm.api_key`、`XAI_API_KEY` 或 profile 继承得到的 API key。
2. `xai_oauth` 不读取用户配置的 endpoint。
3. `xai_oauth` 不允许 `headers.Authorization`、大小写变体或同义认证 header。
4. named profile 可以只覆盖 inference provider 和 model，其余非凭据字段按现有继承规则处理。
5. route fallback 到 `xai` 或其他 provider 只按用户显式配置执行。
6. Setup readiness 要区分“配置已选中”和“OAuth 已登录”。
7. 连接测试是显式推理请求，会消耗订阅额度，UI 必须沿用现有连接测试提示。
8. model picker 与现有 `xai` 共享 xAI 模型元数据和图标，但认证方式仍显示为不同 provider。

## 9) OAuth 协议要求

### 9.1 固定信任边界

已知 xAI OIDC issuer 为：

```text
https://auth.x.ai
```

实现应优先读取：

```text
https://auth.x.ai/.well-known/openid-configuration
```

并执行以下校验：

1. discovery 的 `issuer` 必须精确等于预期值。
2. 实际使用的 device authorization、token 和 revocation endpoint 必须是 HTTPS。
3. endpoint 主机必须在编译时白名单内。
4. 禁止跟随会把 POST body 或 bearer token 转发到非白名单主机的 redirect。
5. HTTP client 不能继承用户 LLM endpoint、proxy header 或自定义 Authorization。

开发和测试可通过仅在测试代码中注入 HTTP client 与 endpoint。正式配置不暴露 OAuth issuer、client id 或 endpoint override。

### 9.2 Scope

首版固定申请：

```text
openid profile offline_access grok-cli:access api:access
```

MisterMorph 申请基础 profile，但不需要邮箱，因此不申请 `email`。首版也不为 TTS、Imagine、文件、workspace 或 server-side tools 申请额外 scope。

### 9.3 Device code 轮询

实现必须遵守 RFC 8628：

- 使用服务端返回的 `interval`；
- 收到 `slow_down` 后增加轮询间隔；
- device code 过期后停止；
- context 取消后立即停止；
- Console 登录 session 过期后清除内存状态；
- 进程重启导致未完成登录丢失是可接受行为。

### 9.4 Token refresh

1. access token 到期前预留安全时间刷新。
2. 同一 token store 的并发 refresh 必须串行。
3. refresh token 轮换后，access token 和 refresh token 一次原子写回。
4. refresh 响应不返回新 refresh token 时，保留旧值，前提是 xAI 明确允许。
5. `invalid_grant`、撤销或确定性的 4xx 错误标记为需要重新登录。
6. 网络错误和 5xx 不删除原 token。
7. 推理返回 401 时只允许强制 refresh 并重试一次，防止循环。
8. 403 不触发反复 refresh，因为它通常表示 entitlement 或策略拒绝。

## 10) Token store

路径：

```text
<file_state_dir>/auth/xai.json
```

至少保存：

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "scope": "...",
  "expires_at": "...",
  "created_at": "...",
  "updated_at": "..."
}
```

要求：

1. 目录权限 `0700`，文件权限 `0600`。
2. 使用现有 `fsstore` 原子写入能力。
3. token 字段读取后去除首尾空白。
4. 不依赖 JWT 一定可本地解码；优先使用 OAuth 响应中的 `expires_in`。
5. 不把邮箱、姓名、头像或完整账户资料持久化。
6. 首版不需要 ID token；即使 token response 返回，也不持久化。
7. 不自动迁移其他应用的 credential store。
8. 每个 `<file_state_dir>` 只有一个有效 xAI OAuth 登录；新登录成功后才原子替换旧 token。

认证代码放在独立的 `internal/xaiauth` 包。该包只负责 OAuth、token store 和状态，不负责构造 LLM prompt 或解析 Responses 流。

不要为了复用少量结构而把 `internal/codexauth` 改成通用 OAuth framework。可以直接复用已有的 `fsstore`、时间处理和测试工具。

## 11) LLM transport

第一版复用 `uniai` 已有的 OpenAI Responses 传输，不新增完整 xAI client。

推理地址固定为 `https://api.x.ai/v1`，并过滤用户配置的认证 header。Go 默认 HTTP client 不会把 `Authorization` 转发到无关域名；完全禁止推理 redirect 可以作为后续加固，但不是发布前置条件。不能修改进程级 `http.DefaultClient`，也不能复制 Responses request mapping 和 stream parser。

`xai_oauth` client 必须增加这些实际行为，因此不是只改名字的薄包装：

1. 每次请求前解析或刷新 OAuth token。
2. 固定并校验推理 endpoint。
3. 注入 bearer token，并移除用户可控认证 header。
4. 强制使用 Responses API 兼容路径。
5. 在无 `OnStream` 的 runtime 中也能正确完成请求。
6. 对 401 做一次刷新重试。
7. 把 403 映射为 entitlement/策略错误。
8. 去掉 API Key 计费价格，保留 token usage。

不得复制 `uniai` 已有的 request mapping、stream parser 或 tool-call parser。如果真实验证发现 `uniai` 不兼容，先给 `uniai` 补通用的 xAI Responses 能力，再在 MisterMorph 中使用。

模型默认值使用 `grok-4.5`。这是 xAI 当前推荐的通用和 agentic 模型。若 OAuth entitlement 不包含该模型，必须返回明确错误；不能静默换成其他模型。

## 12) Agent、工具和多轮请求

1. system/developer prompt 保持 MisterMorph 原样。
2. function tool schema 使用当前 `llm.Request` 转换路径。
3. tool result 和 assistant tool call 必须能在下一轮正确回放。
4. 若 xAI Responses 返回 encrypted reasoning item，必须验证多轮回放规则。
5. 不把 reasoning 明文写入普通 assistant message。
6. `ForceJSON`、streaming、取消和 request timeout 沿用现有语义。
7. 支持现有 `llm.Part` 的 `image_base64` 和 `image_url` 输入，由 `uniai` 映射到 xAI Responses 的图片输入。
8. `grok-4.5` 默认声明 `supports_image_parts: true`；自定义模型仍可通过 profile 显式覆盖。
9. 图片输入必须经过真实 OAuth entitlement smoke test；验证失败视为 Phase 0 未完成，不能静默丢弃图片后只发送文字。

## 13) 用量和费用

xAI 订阅采用共享用量池时，一次请求消耗的不是公开 API 价格对应的直接账单金额。因此：

1. stats 继续记录 request count、input/output/cache/reasoning token，以及响应提供的 input image token。
2. provider 记录为 `xai_oauth`，与 `xai` 分开。
3. `xai_oauth` 的 monetary cost 为空，不使用 xAI API pricing catalog 估算成“实际费用”。
4. 不把订阅月费或周用量百分比折算为 token 单价。
5. 若 xAI 后续返回明确的本次实际扣费字段，再单独设计，不从公开价格推导。
6. Console Stats 对缺失费用显示 `-`，不能显示 `$0.00`。

## 14) 错误语义

必须给出可执行的错误信息：

| 情况 | 行为 |
| --- | --- |
| 未登录 | 提示运行 `mistermorph auth xai login` |
| device code 待批准 | 保持轮询，不作为失败 |
| device code 到期或被拒绝 | 停止轮询，提示重新登录 |
| refresh token 缺失或被撤销 | 提示重新登录 |
| 401 | 强制 refresh 后重试一次；仍失败则提示重新登录 |
| 403 | 提示订阅 entitlement、地区或团队策略可能不允许，不反复刷新 |
| 429 | 展示 xAI 的限流或订阅用量耗尽信息和可用的 retry time |
| 模型不可用 | 显示配置的模型名，提示选择账户可用模型 |
| OAuth endpoint 校验失败 | fail closed，提示升级或检查 xAI 服务状态 |
| 推理 schema 不兼容 | 提示 provider 需要升级，不改用 API Key |

错误、日志和 debug dump 中的响应 body 必须先做 secret redaction，并限制长度。

## 15) 安全要求

1. access token、refresh token 和 device code 都按 secret 处理；收到但不使用的 ID token 立即丢弃。
2. 所有日志层统一识别并遮盖 bearer token 和 OAuth token 字段。
3. Console 前端和浏览器存储中不能出现 token。
4. token 不能进入模型 messages、tool parameters、memory、checkpoint 或 request dump。
5. 用户自定义 header 不能覆盖认证 header。
6. OAuth token 不能发送到 route fallback provider。
7. endpoint host 校验在发请求前执行，不能只依赖默认配置。
8. OAuth 请求拒绝 redirect；推理请求始终从固定的 xAI HTTPS endpoint 发起。
9. 状态接口不返回可用于识别用户的账户资料。
10. 本地 logout 尽力远端撤销；远端撤销失败仍清除本地 secret。

## 16) 实现范围

预期涉及：

- `internal/xaiauth`：device flow、discovery 校验、refresh、store、status。
- `providers/xaioauth`：token resolution、固定 transport、401 retry、错误和 usage 语义。
- `internal/llmutil`：provider registry、配置解析、profile/route、client build。
- `cmd/mistermorph`：`auth xai` CLI。
- `cmd/mistermorph/consolecmd`：Console auth API 和 readiness。
- `web/console`：Setup/Settings 登录 UI 和 provider 表单状态。
- 配置示例和用户文档。

Console 可以复用现有 device auth 对话框的通用视觉组件，但不能复制一整套页面。只有当共享组件能同时表达 Codex 和 xAI 的真实状态时才提取；不得增加只做改名的函数或组件。

## 17) 测试需求

按照测试先行实现。

### 17.1 Auth 单元测试

1. discovery issuer 和 endpoint 主机校验。
2. device code 成功响应和缺字段响应。
3. `authorization_pending`、`slow_down`、拒绝和过期。
4. refresh 成功、token 轮换、缺失 refresh token。
5. 并发 refresh 只发生一次。
6. 401 强制 refresh 只重试一次。
7. 403 不 refresh。
8. token store 权限和原子写入。
9. logout 的远端撤销和本地删除语义。
10. 所有返回错误不包含 secret。

### 17.2 Provider 和配置测试

1. `xai_oauth` 能被 inference provider registry 解析。
2. profile 和 route 能选择 `xai_oauth`。
3. 不读取或继承 API key。
4. 不接受 endpoint override。
5. 不接受 Authorization header override。
6. route fallback 不会收到 OAuth token。
7. fake Responses server 覆盖图片输入、streaming、tool call、tool result 和多轮回放。
8. runtime 没有 stream callback 时仍能返回完整结果。
9. `xai_oauth` usage 有 token 数但没有 monetary cost。
10. setup readiness 能区分未登录和已登录。

### 17.3 CLI 和 Console 测试

1. login/status/logout 不泄露 secret。
2. `--set-default` 写入正确 provider/model 并清除凭据字段。
3. Console auth API 需要有效 Console session。
4. start/poll API 只返回公开登录辅助信息。
5. login session 到期后不可继续使用。
6. logout 后状态立即变为未登录。

### 17.4 手工 smoke test

1. 符合条件的 SuperGrok 账户。
2. 已连接 X 账户的 X Premium+ 订阅。
3. 不具备 entitlement 的账户。
4. `grok-4.5` 普通对话、图片理解和工具调用。
5. Console 与至少一个 channel runtime 的图片附件。
6. 多轮工具调用和长上下文。
7. access token 到期后的自动 refresh。
8. Console、CLI 和至少一个 channel runtime。
9. 订阅额度耗尽或 429。

## 18) 发布阶段

### Phase 0：xAI 协议验证

- [x] 使用 xAI shared public OAuth client。
- [x] 固定 scope，不申请 email。
- [ ] 确认 entitlement 和推理 endpoint。
- [ ] 验证 `grok-4.5`、Responses、图片输入、streaming、tools、refresh。
- [ ] 记录真实错误响应，但不提交 token 或账户资料。

### Phase 1：后端和 CLI

- [x] 先增加 auth、provider、配置和安全测试。
- [x] 实现 `internal/xaiauth`。
- [x] 实现 `xai_oauth` provider。
- [x] 实现 CLI login/status/logout。
- [x] 支持 profiles、routes、fallback 和 stats。

### Phase 2：Console

- [x] 增加 Console auth API。
- [x] 增加 Setup 和 Settings 登录 UI。
- [x] 补 readiness 和连接测试行为。
- [x] 完成中、英、日文案。
- [ ] 用真实 OAuth token 验证并启用 model picker；确认前保留可编辑 model 字段和 `grok-4.5` 默认值。

### Phase 3：发布验证

- [ ] 完成真实账户 smoke test。
- [x] `go test ./...` 通过。
- [x] `go vet ./...` 通过。
- [x] `pnpm build` 通过。
- [x] 更新配置示例和用户文档。
- [x] 标注 xAI entitlement 由 xAI 控制。

## 19) 验收标准

功能完成时必须满足：

1. MisterMorph 使用 xAI shared public OAuth client，或未来由 xAI 分配的专用 public client。
2. `mistermorph auth xai login/status/logout` 可用。
3. Console 可完成登录、查看状态、重新登录和退出。
4. `xai_oauth` 能使用 `grok-4.5` 完成图片理解、流式对话和 function tool loop。
5. access token 可自动刷新，并正确处理 refresh token 轮换。
6. profiles 和 routes 能显式选择 `xai_oauth`。
7. `xai` API Key provider 行为不变。
8. OAuth token 不出现在浏览器、日志、dump、stats、memory 或错误中。
9. OAuth token 只发送到 xAI 批准并固定的主机。
10. 订阅请求不显示按公开 API 单价计算的实际费用。
11. entitlement 不足时错误明确，不自动回退到 API Key。
12. 所有自动测试和构建通过。

## 20) 参考资料

- xAI, Connect Grok to Hermes Agent: https://x.ai/news/grok-hermes
- xAI, Grok FAQ: https://docs.x.ai/grok/faq
- xAI, Grok Build overview: https://docs.x.ai/build/overview
- xAI, Grok Build enterprise authentication: https://docs.x.ai/build/enterprise
- xAI OIDC discovery: https://auth.x.ai/.well-known/openid-configuration
- xAI Grok 4.5 model: https://docs.x.ai/developers/models/grok-4.5
- xAI Models: https://docs.x.ai/developers/models
- Hermes xAI Grok OAuth guide: https://hermes-agent.nousresearch.com/docs/guides/xai-grok-oauth
- Hermes OAuth implementation: https://github.com/NousResearch/hermes-agent/blob/main/hermes_cli/auth.py
- OpenClaw xAI provider: https://docs.openclaw.ai/providers/xai
- OpenClaw xAI OAuth implementation: https://github.com/openclaw/openclaw/blob/main/extensions/xai/xai-oauth.ts
- OAuth 2.0 Device Authorization Grant, RFC 8628: https://www.rfc-editor.org/rfc/rfc8628
