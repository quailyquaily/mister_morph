---
status: draft
---

# Secret Manager / Credential Provider — Review & TODO

Date: 2026-02-01

## 1. 需求 Review（技术合理性）

你提出的方向整体是正确的：把 secret（API Key / Token）与 skill 定义、日志、以及 LLM 上下文彻底隔离，并按“最小暴露面（least exposure）”在运行时注入到真实请求中。

这套设计能显著降低以下风险面：

- **Prompt injection/套话泄漏**：模型永远“看不见”明文 key，即使被诱导也吐不出来。
- **工具调用记录泄漏**：tool params / traces / debug logs 不应携带 key（只出现 `secret_ref` 这种引用）。
- **不受信任 skill 的扩散风险**：skill 只声明自己“需要什么 ref”，不掌握 secret 本体。
- **意外复制/提交**：避免 key 出现在 `SKILL.md`、配置模板、或者运行日志中。

同时，这里也有几个需要补强/澄清的点（建议纳入 TODO）：

- **`secret_ref` 本身也需要权限边界**：如果任意用户输入都能让模型指定 `secret_ref=ANYTHING`，仍可能导致“越权取其他 secret”。因此需要“允许列表/绑定关系”（skill ↔ secret_ref ↔ tool）。
- **不要让 LLM 拼完整 HTTP 请求**：你已指出这一点。落地时需要把“headers/body 的拼装权”收回到宿主层（tool 实现或 middleware）。
- **日志红action要覆盖常见 header 命名**：当前日志红action偏向 `api_key`/`authorization` 等；像 `X-API-Key`（含连字符）这类容易漏掉，需要补齐。

## 2. 建议的架构拆分（适配 mister_morph）

### 2.1 概念与数据模型（最小必要）

本方案选择 **profile-based auth** 作为唯一推荐路径：skill/LLM **只引用 profile id**，不直接声明 `secret_ref`、不声明 header 名、不声明注入位置。

这样才能从机制上实现“最小暴露面 + 最小可变输入面”，避免 prompt injection 把注入细节/目标域名改成攻击者控制的值。

#### Skill 侧（只声明需要的 profile）

Skill frontmatter（示例）：

```yaml
---
name: jsonbill-client
description: Call JSONBill API safely via url_fetch + auth_profile.
auth_profiles: ["jsonbill"]
---
```

在 LLM/tool 调用层，只允许传：

- `auth_profile`: `"jsonbill"`（字符串 id）

### 2.2 Secret Resolver / Credential Provider（宿主侧）

新增一个 Secret Resolver（或 Credential Provider）接口：

- 输入：`secret_ref`（例如 `JSONBILL_API_KEY`）
- 输出：**仅在内存中短生命周期存在**的明文 secret（string/bytes）

MVP 落地：`EnvSecretResolver`

- 默认策略：`secret_ref` 直接映射到环境变量名（例如 `JSONBILL_API_KEY`）
- 可选策略：支持配置别名（`secret_ref -> ENV_NAME`），便于迁移/复用
- 失败策略：默认 fail-closed（找不到就报错，不降级为“让 LLM 继续猜”）

### 2.3 Tool 注入点（只在 HTTP 层用）

对需要鉴权的工具，建议采用下面两条硬规则：

1) **LLM 只能传 `auth_profile`，不能传 `secret_ref`，也不能传任何鉴权值（header/query/body/subprotocol 等）**
2) **最终鉴权注入在 tool 内部或 tool middleware 内完成，且不出现在 observation/logs**

在本仓库里，优先改造/扩展 `url_fetch`（`tools/builtin/url_fetch.go`）来承载“安全 HTTP”能力，因为：

- 现在 `url_fetch` 允许 LLM 直接传 `headers`，这会鼓励把 key 放进 tool params（高风险）。
- `agent/logging.go` 虽然会按 key 名 redaction，但对 `X-API-Key` 这类命名存在漏网风险。

建议的 tool call（示例，仅传 profile）：

```json
{
  "url": "https://api.example.com/v1/xxx",
  "method": "GET",
  "auth_profile": "jsonbill"
}
```

并增加约束：

- `headers` 允许，但必须经过 **安全 header 名 allowlist（P0）**；同时对敏感/代理类 header 名做 denylist（P0）
- 输出中不回显 request headers（目前 `url_fetch` 没回显 headers，保持即可）

### 2.4 Profile 结构的可扩展性（面向未来工具）

注意：`auth_profile` 的核心价值是 **把“凭据是什么”和“如何使用它”从 LLM 的可控输入里拿走**，而不是把“url_fetch 的 header 注入方式”固化成整个系统的唯一形态。

未来可能出现的工具（例：`websocket_fetch`）未必通过 HTTP header 传 key：可能需要 query param、WebSocket subprotocol、握手 header、或者某种签名流程。因此建议把 profile 的配置拆成两层：

- **Credential（凭据本体引用）**：`secret_ref` + 类型（api_key/bearer/…），只负责“从哪里取明文”，不包含注入细节
- **Bindings（工具绑定/注入策略）**：按 tool 维度声明“这个 tool 如何使用该 credential”（例如 header/query/subprotocol），由宿主配置固定

这样：

- skill/LLM 仍然只传 `auth_profile="jsonbill"`
- 具体用 header 还是 query、header 名是什么、是否允许 redirect/哪些 host/path 允许访问，全部由宿主配置决定

为了同时满足“可扩展”和“可校验”，建议 `bindings.<tool>` 的注入规则保持 **结构化且可枚举**：

- 采用统一的注入原语（MVP）：`inject.location` + `inject.name` + `inject.format`
- 为未来预留扩展枚举（例如 `subprotocol`、`cookie`、`signed` 等），但**每个工具只能接受它明确支持的子集**；不支持则报错（fail-closed）
- `inject.format` 必须是枚举（例如 `raw` / `bearer` / `basic`），禁止自由模板字符串（例如 `format: "X-API-Key: {{secret}}"`）这类不可控拼接方式

### 2.5 权限边界（防“模型随便取 ref”）

需要把 “谁能用哪些 profile” 固化为可审计的 policy，而不是完全交给 LLM：

- **Skill 级声明**：skill 的 frontmatter 声明 `auth_profiles: ["jsonbill"]`
- **运行时 allowlist**：配置/启动参数指定本次 run 允许的 `auth_profile` 集合
- **Tool 级约束**：仅允许某些 tool 使用 `auth_profile`（如 `url_fetch`）；`bash` 不允许承载带鉴权 HTTP（见 TODO）

如果允许“用户任务文本”影响 `auth_profile`，务必做校验：

- `auth_profile` 必须匹配严格正则（如 `^[a-z][a-z0-9_.-]{1,63}$`）
- 必须在 allowlist 中
- 必须是 skill 声明过的 profile（当此次 tool call 归因于某个 skill 时）

## 3. 实现 TODO（按优先级）

### P0（必须）

- 新增 `secrets`（或 `credentials`）包：定义 `Resolver` 接口 + `EnvResolver` 实现（仅从 env 取值，不落盘）。
- 在 Engine/工具构造阶段注入 resolver（避免让 tool 自己 `os.Getenv` 到处读）。
- 引入 **profile-based auth（唯一支持路径）**：
  - `url_fetch` 新增 `auth_profile` 参数：LLM/skill 只能传 `profile_id`，不能描述注入细节与 `secret_ref`。
  - 运行时从配置加载 `auth_profiles`（宿主定义）：
    - `credential`: 绑定的 `secret_ref` + `kind`（api_key/bearer/…）
    - `allow`: `allowed_hosts`/`allowed_schemes`/（可选）`allowed_path_prefixes`、`follow_redirects`
    - `bindings`: 按 tool 的注入规则（例如 `bindings.url_fetch.inject.location=header` + `inject.name=Authorization` + `inject.format=bearer`）
  - `url_fetch` 执行时：先校验 URL 是否符合 `allow` 约束，再按 `bindings.url_fetch` 的规则通过 resolver 取 secret 并注入；禁止把注入信息传播到 redirect（推荐默认：禁用 redirect）。
  - 明确不支持 `auth.secret_ref`（避免绕开 profile 约束的“后门 API”）。
- `bindings` 的校验与失败策略：
  - 未配置 `bindings.<tool>`：该 tool 使用该 `auth_profile` 时直接报错（避免“悄悄不带 auth”）
  - `inject.location`/字段名不被该 tool 支持：直接报错（fail-closed）
- `url_fetch.headers` 的强约束（P0：header 名约束；P1：header 值模式检测/脱敏）：
  - 允许（allowlist，建议默认）：`Accept`、`Content-Type`、`User-Agent`、`If-None-Match`、`If-Modified-Since`、（可选）`Range`
  - 禁止（denylist，至少）：`Authorization`、`Cookie`、`Host`、`Proxy-*`、`X-Forwarded-*`
  - 额外规则：header 名匹配 `(?i).*api[-_]?key.*`、`(?i).*token.*` 一律拒绝（可配置，避免误伤时允许更细的例外规则）
  - 规范化：判断前先把 header 名规范化（trim + lower + 去掉 `-`/`_`），避免大小写/分隔符绕过
- Redirect 策略（P0，硬规则）：
  - 默认 `follow_redirects=false`
  - 若开启 redirect：
    - 只允许同源（同 scheme + host + port）
    - 严格限制次数（例如 ≤3）
    - 对 301/302/303/307/308 一致做 allow 校验；特别注意 307/308 会保留 method/body
    - 每一跳都重新校验 allow，并“每跳重新注入 auth（仅同源）”，不要依赖 net/http 自动复用 header
- 日志与 trace 防泄漏：
  - 扩展 redaction 规则覆盖 `api-key`（连字符）/`x-api-key` 等常见命名。
  - 确保任何 error/observation 不拼出包含 secret 的字符串（包括“调试回显请求”这类习惯用法）。

### P1（强烈建议）

- 增强 skill frontmatter schema：支持声明 `auth_profiles: ["..."]`（仅 profile id，不含注入细节）。
- 增加 policy 配置（建议加在 `config.example.yaml`）：
  - `secrets.enabled: true|false`
  - `secrets.allow_refs: [...]`（可选：若希望进一步限制可用的 secret_ref 集合）
  - `secrets.aliases: {JSONBILL_API_KEY: "SOME_ENV_NAME"}`（可选）
  - `secrets.allow_profiles: ["jsonbill", "..."]`（推荐：以 profile 为主的 allowlist）
- 对 `bash` 的默认策略（建议写死为安全默认）：
  - 当 `secrets.enabled=true` 时，默认允许 `bash`（本地自动化仍有价值），但默认拒绝 `curl`（以及其它可外发 HTTP 的子命令），避免“bash + curl”承载带鉴权的 HTTP；需要 HTTP 时用 `url_fetch + auth_profile`。
  - 如果确实需要 curl 特性，优先新增一个“结构化的 subprocess 工具”（例如 `exec`/`curl_fetch`），支持 `profile_id + argv + stdin`，由宿主注入 secret 并提供最小环境。
- 增加 `auth_profiles` 配置（建议加在 `config.example.yaml`）：
  - `auth_profiles.<id>.credential.secret_ref`：profile 绑定的 secret
  - `auth_profiles.<id>.credential.kind`：api_key/bearer/...
  - `auth_profiles.<id>.allow.allowed_hosts`/`allowed_schemes`/`allowed_methods`/`allowed_ports`/`allowed_path_prefixes`
  - `auth_profiles.<id>.allow.follow_redirects`：默认 false
  - `auth_profiles.<id>.bindings.<tool>`：每个工具的注入规则（url_fetch/websocket_fetch/...）
  - `auth_profiles.<id>.bindings.<tool>.allow_user_headers`：默认 false（避免 LLM 自行带敏感 header）
- 在 tool 执行前做统一校验：
  - `auth_profile` 是否允许（`secrets.allow_profiles` + skill 声明）
  - URL 是否满足 profile 限制（host/scheme/path + redirect 策略）
- 增加输出 redaction（不仅按 key 名，还按值模式）：
  - JWT、常见 token 前缀、`-----BEGIN ... PRIVATE KEY-----` 等
  - 对 HTTP 响应体进行最小化 redaction（避免接口回传 token 时直接进入上下文）
  - （可选）对 `url_fetch.headers` 的 value 做模式检测：如果疑似 token/JWT/私钥块，直接拒绝或强制脱敏（按你的风险偏好）

### P2（后续演进）

- Secret “短生命周期”强化：
  - 可选：resolver 返回结构体（`Value + ExpiresAt + RedactHint`），支持缓存与过期
  - 禁止写入 memory/历史上下文（工程侧保证，不依赖“提示词约束”）
- 为更多工具提供 `auth_profile` 支持（而不是鼓励 `bash + curl`）：
  - 新增专用工具（例如 `api_request`）并内置常见鉴权模板
- 与 Guard 模块对接：
  - pre-tool：阻断敏感 header 直传、阻断未知 `auth_profile`、阻断高风险外发域名
  - post-tool：对响应做 redaction，再进入上下文

### Implementation TODO（详细清单）

> 目标：落地 profile-based auth，并确保 secret **不会**出现在：`SKILL.md` / tool params / tool logs / tool observation / LLM 上下文 / 进程长期内存。

**Config & Wiring（cmd/）**

- [x] 在 `cmd/mister_morph/defaults.go` 的默认项中补齐以下默认值（fail-closed）：
  - [x] `secrets.enabled=false`
  - [x] `secrets.allow_profiles=[]`（默认空：意味着“全部禁用 profile”）
  - [x] `secrets.aliases={}`
  - [x] `auth_profiles={}`
- [x] 在 `config.example.yaml` 增加 `secrets:` 与 `auth_profiles:` 的完整示例与注释（并明确 bash 默认关闭、secrets 下 curl 默认拒绝）。
- [x] 在 `cmd/mister_morph/registry.go`：
  - [x] 从 viper 读取并构建 `EnvSecretResolver`（支持 `secrets.aliases`）
  - [x] 从 viper 读取并校验 `auth_profiles`（见下方 schema），构建一个只读的 `ProfileStore`（不合法的 profile 直接丢弃）
  - [x] 当 `secrets.enabled=true` 时：允许继续注册 `bash`，但默认拒绝 `curl`（以及其它可外发 HTTP 的子命令）来避免“bash + curl”承载带鉴权的 HTTP
  - [x] 把 `ProfileStore + Resolver` 注入到 `url_fetch` tool 的构造函数

**Profile Schema（建议 Go struct + viper unmarshal）**

- [x] 定义 `AuthProfile` 结构（新包：`secrets/`）：
  - [x] `ID`（map key）
  - [x] `Credential`：`kind` + `secret_ref`
  - [x] `Allow`（fail-closed 缺省）：
    - [x] `allowed_hosts`（P0：不允许通配符）
    - [x] `allowed_schemes`
    - [x] `allowed_methods`（统一 upper）
    - [x] `allowed_ports`（可选；为空时仅允许默认端口）
    - [x] `allowed_path_prefixes`（使用 `path.Clean` 规范化做前缀匹配）
    - [x] `follow_redirects`
    - [x] `allow_proxy`（默认 false）
    - [x] `deny_private_ips`（默认 true；MVP 仅拒绝 literal IP/localhost，不做 DNS 解析）
  - [x] `Bindings`：`map[string]ToolBinding`
- [x] 定义 `ToolBinding`：
  - [x] `inject.location` 枚举（MVP：`header`）
  - [x] `inject.name`（header 名）
  - [x] `inject.format` 枚举（`raw|bearer|basic`）
  - [x] `allow_user_headers`（默认 false）
  - [x] `user_header_allowlist`（当 `allow_user_headers=true` 时生效）
- [x] 在加载配置时做 fail-closed 校验：
  - [x] `allowed_hosts`/`allowed_schemes`/`allowed_methods` 不能为空
  - [x] `bindings.url_fetch` 必须存在
  - [x] `inject.name` 严格校验（header token 语法）

**Tool: url_fetch（tools/builtin/url_fetch.go）**

- [x] 更新 `ParameterSchema()`：
  - [x] 增加 `auth_profile`（string，可选）
  - [x] `headers` 允许但必须走 allowlist：`Accept`/`Content-Type`/`User-Agent`/`If-None-Match`/`If-Modified-Since`/`Range`
  - [x] 明确拒绝敏感 header：`Authorization`/`Cookie`/`Host`/`Proxy-*`/`X-Forwarded-*` 以及 `*api[-_]?key*`、`*token*`
- [x] 更新 `Execute()`：
  - [x] 若 params 包含 `auth_profile`：
    - [x] 校验 `secrets.enabled=true`
    - [x] 校验 `auth_profile` 在 `secrets.allow_profiles` 且存在于 `auth_profiles`
    - [x] 校验 URL 满足 profile allow 约束（scheme/host/port/path 前缀）
    - [x] 按 `bindings.url_fetch.inject` 注入 secret（MVP：仅支持 header 注入）
  - [x] Redirect 策略：
    - [x] 默认不 follow redirect（`follow_redirects=false`）
    - [x] 若允许 redirect：限制次数（≤3），且仅允许同源（scheme+host+port 一致）
    - [x] 每跳都重新校验 allow；每跳都重新注入 auth
  - [x] 严格拒绝敏感 headers（无论是否启用 profile）
- [x] 调整输出（observation）避免泄漏：
  - [x] 输出中的 `url:` 会清洗常见敏感 query key（防止未来 query 注入时泄漏）
  - [x] 响应体 `body:` 进入上下文前做 redaction（JWT / Bearer / PrivateKey block / 常见 key=value）

**Logging Redaction（agent/logging.go）**

- [x] 改进 `shouldRedactKey`：
  - [x] key 规范化（lower + 去掉 `-`/`_`）再判断
  - [x] 覆盖 `x-api-key` / `api-key` / `set-cookie` 等常见形式
- [ ] 确保任何日志路径都不会打印“最终请求 headers”（包括 debug 模式）

**Skills（skills/）**

- [x] 在 `skills/skills.go` 增加对 SKILL frontmatter 的轻量解析（只提取 `auth_profiles: []`）
- [x] 在 skill 被加载时，记录“本次 run 载入了哪些 skill 声明的 profiles”（用于审计与可选的二次校验）
- [x] 增加可选强制校验：`secrets.require_skill_profiles=true` 时，`url_fetch(auth_profile=...)` 必须属于本次已加载 skills 声明的 profiles（仍会再叠加 `secrets.allow_profiles`）
- [ ] 明确规则：skill 声明的 profile 只能“提出需求”，最终可用范围仍由 `secrets.allow_profiles` 决定（防止 prompt injection 诱导自动加载 skill 来扩大权限）

**Tests（建议新增 *_test.go）**

- [x] `tools/builtin/url_fetch*_test.go`：
  - [x] `auth_profile` 注入 header 生效
  - [x] 不在 allowlist 的 profile 被拒绝
  - [x] redirect 情况下只允许同源且每跳重新注入（覆盖 307）
  - [x] `headers` 里显式传 `X-API-Key` 被拒绝
  - [x] 响应体 redaction 生效（JWT 不进入输出）
- [x] `agent/logging*_test.go`：
  - [x] `X-API-Key` / `api-key` / `set-cookie` 等 key 名会被 redaction

## 4. 验收标准（MVP）

- 任意情况下：SKILL、日志、tool params、LLM 上下文中都不出现明文 secret。
- `url_fetch` 可在不暴露明文 key 的情况下访问需要鉴权的 HTTP API（通过 `auth_profile` 注入）。
- 未在 allowlist 的 `auth_profile` 必须失败（fail-closed），且错误信息不包含 secret 值。
- 打开 `logging.include_tool_params=true` 也不会泄漏：敏感 header 名/值被拒绝或被 redaction。
- 当 `secrets.enabled=true`：禁止用 `bash + curl` 承载带鉴权 HTTP（默认允许 bash 但拒绝 curl；或至少需要显式审批策略）。

## 5. Samples（配置与调用示例）

### 5.1 config.yaml（示例片段）

说明：这里的 `config.yaml` 指的是 **mister_morph 的主配置文件**（通过 `--config` 指定，或你本地约定的路径），不建议放在某个 skill 目录里。

- Skill 目录应该只包含 `SKILL.md` / `scripts/` / `references/` 等“可分享的内容”，不要放任何环境相关配置，更不要放 secret 明文。
- 如果想让 skill 自带“如何配主配置”的示例，建议把示例作为文本放在 `SKILL.md` 或 `references/`（例如 `references/config_snippet.yaml`），由用户手动合并到自己的主配置里。

```yaml
secrets:
  enabled: true
  allow_profiles: ["jsonbill"]
  aliases:
    JSONBILL_API_KEY: "JSONBILL_API_KEY"

auth_profiles:
  jsonbill:
    credential:
      kind: api_key
      secret_ref: JSONBILL_API_KEY
    allow:
      allowed_schemes: ["https"]
      allowed_methods: ["POST"]
      allowed_hosts: ["api.jsonbill.com"]
      allowed_ports: [443]
      allowed_path_prefixes: ["/tasks/docs"]
      follow_redirects: false
      allow_proxy: false
      deny_private_ips: true
    bindings:
      url_fetch:
        inject:
          location: header
          name: Authorization
          format: bearer
        allow_user_headers: true
        user_header_allowlist: ["Accept", "Content-Type", "User-Agent"]

tools:
  url_fetch:
    enabled: true
```

### 5.2 tool_call（示例）

```json
{
  "url": "https://api.jsonbill.com/tasks/docs",
  "method": "POST",
  "auth_profile": "jsonbill"
}
```

### 5.3 SKILL.md frontmatter（示例）

```yaml
---
name: jsonbill
description: Call JSONBill API safely.
auth_profiles: ["jsonbill"]
---
```

## 6. 可能遗漏的点（建议补充）

- **Redirect 安全**：Go 的 HTTP redirect 可能会把自定义 header（如 `X-API-Key`）带到跳转后的地址；建议在 `url_fetch` 里禁用 redirect 或在 `CheckRedirect` 中对任何 redirect 都移除所有 auth 注入的 header（或只允许同源）。  
- **`bash` 工具的环境变量继承**：即使 LLM 不拿 key，只要 `bash` 继承了进程环境，就可能通过 `env`/`printenv`/`/proc/self/environ` 读到并外发；建议默认“最小环境”执行（env allowlist），并在 Guard 中对相关命令/路径强拦截。  
- **LLM Provider 的 key（`llm.api_key`）也纳入同一策略**：它同样属于 secret（虽不会进 prompt），但会进日志/trace 的风险面；建议明确：永不在日志打印 provider 的鉴权 header/参数，并考虑是否也用 `secret_ref` 统一管理。  
- **Secret 的“可用范围”模型**：除 allowlist 外，建议定义 `secret_ref` 的 scope（per-run / per-skill / per-tool / per-domain），避免“同一次任务里拿到 ref 就到处用”。  
- **Profile 的“可用范围”模型**：除 allowlist 外，建议定义 `auth_profile` 的 scope（per-run / per-skill / per-tool / per-domain），避免“同一次任务里拿到 profile 就到处用”。  
- **Redirect/代理/TLS 等网络边界**：是否允许代理（`HTTP_PROXY`）、是否允许自定义 `Host`、以及证书校验策略都可能影响“最小暴露面”；建议明确默认值与禁用项。  
- **测试用例**：至少加单测覆盖：拒绝敏感 header 直传、`auth_profile` 注入生效但不会出现在 observation/log、redirect 不带 auth、以及响应体 redaction 的基本规则。
