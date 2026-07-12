---
date: 2026-07-12
title: AWS Secrets Manager Config References
status: draft
issue: https://github.com/quailyquaily/mistermorph/issues/64
---

# AWS Secrets Manager Config References

## 背景

当前配置引用只支持本地环境变量：

- `internal/configutil/expand.go` 在配置文件进入 viper 前，对 YAML scalar value 展开 `${ENV_VAR}`。
- `internal/cron/bash_env.go` 在 cron awareness 运行时，用 `configutil.ExpandStrictEnv` 展开 `bash_env` 的值。
- 控制台和 daemon 的 LLM 连接测试、env-managed 字段展示，有独立的 `${ENV_VAR}` 正则识别逻辑。

Issue #64 要求支持 AWS Secrets Manager：当配置引用声明来源为 AWS Secrets Manager 时，运行时应调用 AWS API 获取 secret，而不是只读取本地环境变量。

v1 只解决配置引用来源问题，不重做 auth profile、tool auth 或 secret 注入体系。

## 目标

- 保留 `${ENV_VAR}` 的现有语义。
- 新增 AWS Secrets Manager 引用语法，用在现有字符串配置字段的 YAML scalar value 中。
- 主配置加载、cron `bash_env`、控制台连接测试、daemon 连接测试使用同一套引用解析逻辑。
- AWS 后端使用 AWS SDK v2。凭证仍来自 AWS SDK 默认 credential chain；region/profile 可从 `secrets.aws_secrets_manager` 读取，也可来自 AWS SDK 标准来源，例如 `AWS_PROFILE`、`AWS_REGION`、共享配置文件、实例元数据。
- 新增 AWS Secrets Manager 代码使用 AWS SDK v2。不要为本功能新增 AWS SDK v1 调用；当前由 UniAI Bedrock provider 间接带入的 v1 依赖暂时允许保留，等后续 UniAI 升级后再清理。
- 支持 `SecretString`。
- 支持从 JSON 格式的 `SecretString` 中取一个顶层字符串字段。
- 错误、日志、HTTP 响应和 env-managed payload 不包含 secret value。
- 缺失或失败的 AWS 引用替换为空字符串，并发出 warning。

## 非目标

- 不新增通用 secret manager 平台。
- 不把 AWS Secrets Manager 接进 tool 参数，LLM 仍不能直接选择或读取 secret。
- 不改变 `auth_profiles` 的 profile-based auth 语义。
- 不在 mistermorph 配置里要求保存 AWS access key。
- 不支持 `SecretBinary`。
- 不支持 JSONPath、模板字符串或任意表达式。
- 不把 secret value 缓存在磁盘。

## 引用语法

保留本地环境变量引用：

```yaml
llm:
  api_key: "${OPENAI_API_KEY}"
```

新增 AWS Secrets Manager 引用：

```yaml
llm:
  api_key: "${aws-sm:mistermorph/openai-api-key}"
```

如果 `SecretString` 是 JSON object，可以取顶层字符串字段：

```yaml
auth_profiles:
  jsonbill:
    credential:
      kind: api_key
      secret: "${aws-sm:mistermorph/jsonbill#api_key}"
```

规则：

- `${ENV_VAR}` 仍按本地环境变量解析。
- `${aws-sm:<secret-id>}` 调用 Secrets Manager `GetSecretValue`，返回 `SecretString`。
- `${aws-sm:<secret-id>#<field>}` 把 `SecretString` 当作 JSON object，返回顶层 `<field>` 的字符串值。
- `<secret-id>` 可以是 secret name 或 ARN。
- `<field>` 只支持顶层 key，不支持嵌套路径。
- 未识别的 `${...}` 不应被当作环境变量。
- 引用只在 YAML scalar value 中展开；comments 和 mapping key 不展开。
- 配置里建议把引用放在引号内。配置加载会基于 YAML AST 展开 value，并把展开后的内存 YAML 交给 Viper；这可能规范化格式、引号或空行，但不会写回配置文件。

## 解析层

新增一个很小的引用解析层，建议放在 `internal/secref`，由 `internal/configutil` 调用。

核心职责：

- 识别 `${ENV_VAR}` 和 `${aws-sm:...}`。
- 解析一个字符串中的多个引用。
- 返回展开后的字符串。
- 返回结构化错误或缺失信息，由调用方决定是 warning 还是 error。
- 不记录、不返回用于日志的 secret value。

建议接口保持小：

```go
type Resolver interface {
	ResolveEnv(name string) (string, bool)
	GetAWSSecret(ctx context.Context, ref AWSSecretRef) (string, error)
}
```

实际实现可以拆成两个后端，但调用入口只需要一个 `ResolveString(ctx, value, resolver)` 类函数。不要为现有函数只换名增加薄包装。

## 行为

### 本地环境变量

保持现状：

- 主配置加载时，缺失 `${ENV_VAR}` 替换为空字符串并 warning。
- cron `bash_env` 运行时，缺失 `${ENV_VAR}` 返回错误。
- 裸 `$VAR` 不展开。
- bcrypt hash、正则里的 `$` 不应被破坏。

### AWS Secrets Manager

AWS 引用失败时替换为空字符串，并发出 warning：

- secret 不存在，替换为空字符串并 warning。
- AWS API 返回错误，替换为空字符串并 warning。
- 无法解析 AWS config 或 credentials，替换为空字符串并 warning。
- `SecretString == nil`，替换为空字符串并 warning。
- `SecretBinary`，替换为空字符串并 warning。
- `SecretString == ""`，解析为空字符串，不额外 warning。
- JSON field 缺失或不是字符串，替换为空字符串并 warning。

warning 可以包含 source、secret id、region、profile、field 等定位信息，但不能包含 secret value。

同一次解析过程中允许内存级 memoization：同一个 AWS secret 引用多次出现时，可以只请求一次 AWS API。缓存只存在于进程内，不写入磁盘，不进入日志或 API 响应。

## AWS 后端

新增依赖：

```text
github.com/aws/aws-sdk-go-v2/service/secretsmanager
```

依赖策略：

- 新增 Secrets Manager 代码只使用 `github.com/aws/aws-sdk-go-v2/...`。
- 不在本仓库新增 `github.com/aws/aws-sdk-go` v1 import。
- 当前 `github.com/quailyquaily/uniai/providers/bedrock` 仍会间接带入 AWS SDK v1；这不是本功能要先解决的问题。
- #64 不阻塞在移除 AWS SDK v1 上。等后续 UniAI 的 Bedrock provider 升级到 AWS SDK v2 后，再用 `go mod tidy` 清掉 v1。
- 不通过另一个新依赖重新扩大 AWS SDK v1 的使用面。

加载 AWS 配置时使用 SDK 默认链，并可叠加 root `secrets` 配置中的 region/profile：

```go
awsconfig.LoadDefaultConfig(ctx, opts...)
```

配置字段：

```yaml
secrets:
  allow_profiles: []
  aws_secrets_manager:
    region: "us-east-1"
    profile: "prod"
```

规则：

- `secrets.aws_secrets_manager.region` 为空时，使用 AWS SDK 默认 region 解析。
- `secrets.aws_secrets_manager.profile` 为空时，使用 AWS SDK 默认 profile/credential 解析。
- 不新增“必须填写 AWS access key”的 mistermorph 配置。AWS access key、session token、SSO、instance metadata 等仍交给 AWS SDK 默认 credential chain。
- 因为 config ref 展开发生在完整 viper 配置读取前，region/profile 需要从原始 YAML 做一次最小 bootstrap 读取。只读取 `secrets.aws_secrets_manager.region/profile`，不要把完整配置加载逻辑复制一遍。
- bootstrap 字段可以是字面量，也可以是 `${ENV_VAR}`；不支持 `${aws-sm:...}`，避免配置解析循环。

## 需要接入的入口

### 主配置加载

`internal/configutil.ReadExpandedConfig` 应改为调用统一 resolver。

要求：

- 继续支持现有 warning callback。
- env 引用保持兼容。
- AWS 引用失败时 warning，并替换为空字符串。
- 读取失败时不要在错误里带 secret value。

### cron `bash_env`

`internal/cron.ResolveBashEnvRefs` 目前直接调用 `configutil.ExpandStrictEnv`。

要求：

- 改为使用统一 resolver。
- env 缺失仍然报错。
- AWS 引用失败时 warning，并替换为空字符串。
- 注入到 shell 的值仍只存在于运行时环境，不写入配置文件或日志。

### 控制台和 daemon 的连接测试

这些路径现在有独立的完整 `${ENV_VAR}` 匹配：

- `cmd/mistermorph/consolecmd/agent_settings.go`
- `internal/daemonruntime/server_agent_settings.go`

要求：

- 连接测试用统一 resolver 解析字段值。
- `llm.api_key`、profile API key、Cloudflare token、Bedrock 字段都应支持 AWS 引用。
- AWS 引用失败时连接测试按空字符串继续走既有字段校验。
- secret 字段在响应 payload 中继续隐藏实际值。

### env-managed 展示

这些路径现在只认识 `${ENV_VAR}`：

- `cmd/mistermorph/consolecmd/agent_settings.go`
- `cmd/mistermorph/consolecmd/console_settings.go`
- `internal/daemonruntime/server_agent_settings.go`

v1 可以把 AWS 引用展示为 managed ref：

```json
{
  "source": "aws_secrets_manager",
  "raw_value": "${aws-sm:mistermorph/openai-api-key}"
}
```

要求：

- 不返回 secret value。
- env ref 的现有 `env_name` 行为保持兼容。
- 前端可继续根据 `raw_value` 保存原引用。

## 配置示例

实现时更新 `assets/config/config.example.yaml`，至少包含：

```yaml
# YAML scalar values support ${ENV_VAR}.
# AWS Secrets Manager references use ${aws-sm:<secret-id>}.
# Mapping keys and comments are not expanded.
# The AWS SDK reads credentials from the standard AWS chain.

secrets:
  allow_profiles: []
  aws_secrets_manager:
    region: "us-east-1"
    profile: "prod"

llm:
  api_key: "${aws-sm:mistermorph/openai-api-key}"

auth_profiles:
  jsonbill:
    credential:
      kind: api_key
      secret: "${aws-sm:mistermorph/jsonbill#api_key}"
```

`docs/tools.md` 中 `tools.bash.injected_env_vars` 的说明也要同步：`{name, value}` 的 value 可使用 env ref 或 AWS Secrets Manager ref，最终值只在运行时注入。

## 测试

正式实现前先补测试，再改代码。

建议测试范围：

- `internal/secref`：
  - `${ENV_VAR}` 成功。
  - 缺失 env。
  - 裸 `$VAR` 不展开。
  - bcrypt hash 和正则里的 `$` 不被破坏。
  - `${aws-sm:name}` 成功。
  - `${aws-sm:name#api_key}` 从 JSON object 取顶层字符串字段。
  - JSON field 缺失、非字符串、非法 JSON。
  - `SecretString == nil`。
  - `SecretString == ""`。
  - `SecretBinary` 返回空字符串和 warning。
  - AWS API 错误返回空字符串和 warning，且 warning 不包含 secret value。
  - 同一次解析中相同 secret 引用只请求一次 fake client。
  - `secrets.aws_secrets_manager.region/profile` 会传给 AWS config loader。
- `internal/configutil`：
  - 现有 `ReadExpandedConfig` 行为不回退。
  - AWS ref 能在 `llm.api_key` 或 `auth_profiles.<id>.credential.secret` 中展开。
  - AWS ref 失败时配置加载继续，目标字段为空字符串，并产生 warning。
- `internal/cron`：
  - `bash_env` 同时支持 env ref 和 AWS ref。
  - AWS ref 失败时注入空字符串，并产生 warning。
- 控制台和 daemon：
  - 连接测试能解析 AWS ref。
  - AWS ref 失败时按空字符串解析，不返回 secret value。
  - env-managed payload 对 AWS ref 不包含 secret value。

## 验收标准

- 现有 `${ENV_VAR}` 配置不需要修改。
- 使用 `${aws-sm:<secret-id>}` 的配置能从 AWS Secrets Manager 读取 `SecretString`。
- 使用 `${aws-sm:<secret-id>#<field>}` 时，只返回 JSON object 顶层字符串字段。
- `secrets.aws_secrets_manager.region/profile` 能配置 AWS resolver；为空时使用 AWS SDK 默认解析。
- AWS 读取失败时，目标字段变为空字符串，并给出可定位但不泄露 secret 的 warning。
- cron `bash_env`、控制台连接测试、daemon 连接测试不再各自实现 env-only 解析。
- 日志、错误、API 响应、前端 payload 中不出现 secret value。
- 本功能新增代码没有 `github.com/aws/aws-sdk-go` v1 import；Secrets Manager 使用 AWS SDK v2。
- `go test ./...` 通过。
