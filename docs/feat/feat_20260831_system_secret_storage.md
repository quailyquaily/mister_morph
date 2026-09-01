---
date: 2026-08-31
title: System Secret Storage
status: implemented
---

# System Secret Storage

## 1. 背景

MisterMorph 的 API key、Bot token、OAuth token 和云平台凭据目前可能来自：

- `config.yaml` 中的明文；
- `${ENV_VAR}` 环境变量引用；
- `${aws-sm:<secret-id>}` AWS Secrets Manager 引用；
- 单独的本地 token 文件，例如 OAuth token 文件。

这些入口能工作，但缺少统一的保存规则。桌面用户通过 Web UI 输入 API key 时，最自然的结果仍然是把明文写入配置文件。OAuth token 也可能使用另一套文件读写逻辑。各 Provider 如果继续自行处理凭据，读取、隐藏、更新和删除规则会逐渐分叉。

本方案解决一个具体问题：配置文件只记录 secret 的来源，运行时在一个固定边界内取得明文。Provider、工具、前端和日志都不直接处理 secret 的存储细节。

## 2. 结论

第一版采用以下规则：

1. 桌面或有登录会话的本地运行，默认把新 secret 存进系统密钥管理器。
2. 无桌面会话的 Linux 服务、容器和 CI 继续优先使用环境变量或 AWS Secrets Manager。
3. `config.yaml` 保留现有字符串字段，不把每个字段改成 `{source, id}` 对象，也不增加平行的 `api_key_ref` 字段。
4. 系统密钥管理器引用使用 `${secret:<opaque-id>}`，并且必须占满整个 scalar value。
5. 现有 `${ENV_VAR}` 和 `${aws-sm:...}` 语法保持兼容。
6. secret 只在配置快照建立时解析一次。请求路径不重复访问 Keychain、Credential Manager、Secret Service 或 AWS。
7. 已保存引用解析失败时明确报错，不自动尝试其他来源。Settings 保存新 secret 时若系统密钥管理器写入失败，则记录 warning，并按旧行为把本次提交值写入配置文件。
8. 第一版不新增直接 AWS KMS 密文后端。AWS 环境继续使用已经实现的 AWS Secrets Manager 引用；Secrets Manager 本身使用 KMS 保护 secret。

这套设计不引入通用插件系统，也不为每个 Provider 增加一层包装。现有 `internal/secref` 扩展为统一入口即可。

## 3. 目标

- 新安装的桌面用户不需要把 API key 明文写入 `config.yaml`。
- macOS、Windows 和有 Secret Service 会话的 Linux 使用操作系统提供的凭据存储。
- headless Linux、systemd、容器和 CI 保持可用。
- 本地 Console 和远程 Console 对 secret 的读取、更新和删除行为一致。
- Provider 只接收已经解析的运行时配置，不知道 secret 来自哪里。
- 配置 API 永远不返回 secret 明文。
- 日志、journal、task、tool 参数和 LLM context 不包含 secret 明文。
- 旧配置无需立即迁移，升级后仍可启动。

## 4. 非目标

- 不实现 Vault、1Password、Bitwarden 等第三方平台。
- 不让 LLM 选择 secret source 或 secret id。
- 不允许 tool 直接读取任意 secret。
- 不在启动时自动迁移或删除用户已有的明文配置。
- 不自行设计主密码、密钥派生或本地加密文件格式。
- 不承诺防御已经取得当前操作系统账户权限的恶意进程。
- 不把 AWS KMS 当成按名称保存 secret 的数据库。

## 5. 来源和适用场景

| 来源 | 配置内容 | 适用场景 | 是否可由 Morph 写入 | 主要限制 |
| --- | --- | --- | --- | --- |
| 系统密钥管理器 | opaque id | 桌面、本地交互式运行 | 是 | 依赖当前用户会话 |
| 环境变量 | 变量名 | systemd、容器、CI、运维注入 | 否 | 生命周期由父进程或服务管理器控制 |
| AWS Secrets Manager | secret name 或 ARN | AWS 上的长期服务 | 可选，第一版保持只读 | 依赖 AWS 网络和 IAM |
| 配置明文 | secret value | 旧配置兼容 | 是 | 文件泄露即 secret 泄露 |
| 直接 AWS KMS 密文 | ciphertext | 特殊的加密配置场景 | 第一版不支持 | KMS 不是 secret store，轮换和元数据需自行管理 |

默认选择只由运行环境决定：

- macOS 桌面：Keychain。
- Windows 桌面：Credential Manager。
- Linux 桌面：Secret Service。
- Linux 服务、容器、CI：环境变量或 AWS Secrets Manager。
- 系统密钥管理器不可用时：启动时记录 warning；Settings 保存回退到 0600 配置文件。服务部署仍建议使用环境变量或 AWS Secrets Manager。

### 5.1 Secret 字段清单

第一版自动写入和隐藏以下 Console 可编辑字段：

| 配置字段 | 说明 |
| --- | --- |
| `llm.api_key` | 默认 LLM API key |
| `llm.bedrock.aws_key`、`llm.bedrock.aws_secret` | Bedrock 静态凭据 |
| `llm.cloudflare.api_token` | Cloudflare API token |
| `llm.profiles.<name>` 下的同名字段 | Named profile 凭据 |
| `telegram.bot_token` | Telegram Bot token |
| `slack.bot_token`、`slack.app_token` | Slack Bot 与 Socket Mode token |
| `line.channel_access_token`、`line.channel_secret` | LINE Channel 凭据 |
| `lark.app_secret` | Lark app secret |

交互式 `install` 采集到的 LLM API key 或 Cloudflare token 也写入系统密钥管理器。非交互式安装不读取系统密钥管理器，生成的模板继续使用环境变量引用。

引用解析本身不限制字段名。因此 `llm.image.api_key`、`auth_profiles.<name>.credential.secret`、`console.password` 和其他 scalar secret 可以手工使用 `${secret:...}`、`${ENV_VAR}` 或 `${aws-sm:...}`。第一版不为这些没有对应编辑界面的字段增加专用写入 API。

下列凭据有独立的生命周期，暂不迁移：

- Codex、xAI 和 MisterMorph Pro OAuth token 文件；
- Mixin keystore 文件中的 Ed25519 私钥。

OAuth token 会自动刷新，Mixin keystore 也不是单一 scalar。它们应分别修改原认证模块和 Mixin credential loader，不能只把文件路径替换成一个 secret reference。

## 6. 配置语法

### 6.1 系统密钥管理器

系统密钥管理器引用使用：

```yaml
llm:
  api_key: "${secret:01K4E2Q4S2V5N9X5J6R4M8A7TC}"
```

规则：

- `secret:` 后面是 Morph 生成的 opaque id。
- id 至少包含 128 bit 不可预测随机数据；可使用 16 bytes 以上随机值的 base64url 编码。
- id 不包含 Provider 名、用户名称、配置路径、endpoint 地址或 secret 的一部分。
- 引用必须是 scalar 的完整内容，不支持 `prefix-${secret:...}-suffix`。
- 同一个引用可被多个运行时字段使用，但 UI 默认每次保存都生成新 id，避免无意共享生命周期。
- 复制 `config.yaml` 到另一台机器不会复制 secret。目标机器应报告引用不存在，而不是得到空字符串。

操作系统中的记录至少包含：

- service：`com.mistermorph`；
- account：opaque id；
- secret：原始字节；
- label：`MisterMorph · <config-path.key-name>`，仅供系统 UI 展示，不参与定位。

例如 `llm.profiles.reasoning.api_key` 对应 `MisterMorph · llm.profiles.reasoning.api_key`。label 不包含 secret、完整 endpoint 或本机路径。系统只读写 `com.mistermorph` service。

### 6.2 环境变量

现有语法保持不变：

```yaml
llm:
  api_key: "${OPENAI_API_KEY}"
```

环境变量属于文本替换。当前实现允许它出现在 scalar 的一部分中：

```yaml
some_value: "prefix-${DEPLOYMENT_NAME}"
```

对于 secret 字段，仍建议让引用占满整个 scalar：

```yaml
api_key: "${OPENAI_API_KEY}"
```

不要新增 `$OPENAI_API_KEY` 裸变量语法。`${...}` 可以避免破坏 bcrypt hash、正则表达式和普通美元符号。

### 6.3 AWS Secrets Manager

现有语法保持不变：

```yaml
llm:
  api_key: "${aws-sm:mistermorph/openai-api-key}"
```

JSON `SecretString` 的顶层字段仍使用 `#field`：

```yaml
auth_profiles:
  jsonbill:
    credential:
      secret: "${aws-sm:mistermorph/jsonbill#api_key}"
```

`secret-id` 是 Secrets Manager 的 name 或 ARN。该语法表示远端查找，不包含 secret value 或 ciphertext。

AWS 凭据继续来自 AWS SDK 默认 credential chain。`config.yaml` 不新增 AWS access key。现有 bootstrap 配置继续有效：

```yaml
secrets:
  aws_secrets_manager:
    region: "us-east-1"
    profile: "prod"
```

### 6.4 明文兼容

旧配置仍可读取：

```yaml
llm:
  api_key: "sk-..."
```

Web UI 应标记它为 `file` source。只有用户再次提交该字段时，后端才尝试把新值写入系统密钥管理器。启动时不自动修改文件，也不自动删除明文。

新安装在系统密钥管理器可用时，不应生成新的明文 secret。

## 7. 环境变量、AWS Secrets Manager 和 AWS KMS 的语义差异

这三者不能只用不同前缀包装成同一种东西。

### 7.1 环境变量是进程输入

```yaml
api_key: "${OPENAI_API_KEY}"
```

配置里保存的是变量名。secret value 由 shell、systemd、容器平台或父进程放入环境。Morph 只读取当前进程环境，不负责保存、轮换或删除它。

### 7.2 Secrets Manager 是远端 secret store

```yaml
api_key: "${aws-sm:mistermorph/openai-api-key}"
```

配置里保存的是 secret name 或 ARN。Morph 调用 `GetSecretValue` 取得当前值。Secret 的版本、轮换、访问策略和 KMS 加密由 AWS Secrets Manager 管理。

AWS 官方文档说明，Secrets Manager 使用 KMS 生成和保护 data key，再用 data key 对 secret value 做 envelope encryption，而不是直接让 KMS `Encrypt` 保存整个 secret：

- [Secret encryption and decryption in AWS Secrets Manager](https://docs.aws.amazon.com/secretsmanager/latest/userguide/security-encryption.html)
- [AWS Secrets Manager best practices](https://docs.aws.amazon.com/secretsmanager/latest/userguide/best-practices.html)

因此，想让 AWS KMS 保护 MisterMorph secret 时，默认做法仍是 `${aws-sm:...}`，并为该 Secrets Manager secret 选择合适的 KMS key。

### 7.3 AWS KMS 是加解密服务

KMS 没有“用名称取 secret value”的能力。它接收 plaintext 或 ciphertext blob，并执行加解密。直接使用 KMS 时，ciphertext 必须由配置文件、数据库或对象存储另行保存。

所以，不应定义下面这种有误导性的语法：

```yaml
# 不采用：KMS 中不存在名为 openai-api-key 的 secret record。
api_key: "${aws-kms:openai-api-key}"
```

如果未来确有直接 KMS 的需求，语法必须表达“这是一个加密后的 literal”，而不是“这是一个远端名称”。可保留如下格式，但第一版不实现：

```yaml
llm:
  api_key: "secret://aws-kms/v1/openai-api-key/<base64url-ciphertext>"
```

其中：

- `v1` 是密文封装格式版本；
- `openai-api-key` 是非敏感 logical id；
- 最后一段是 KMS `Encrypt` 返回的 `CiphertextBlob`，使用无 padding 的 base64url；
- 整个引用必须占满 scalar，不允许插入到其他字符串中；
- encryption context 固定包含 `mistermorph:format=v1` 和 `mistermorph:secret-id=openai-api-key`；
- decrypt 时必须提供完全相同且大小写一致的 encryption context；
- region、profile 和用于 encrypt 的 key id 由 `secrets.aws_kms` bootstrap config 或 AWS SDK 默认来源提供；
- 运行时至少需要 `kms:Decrypt`；通过 UI 写入时还需要 `kms:Encrypt`；
- KMS `Encrypt` 只适合小块数据。AWS 当前 API 对 `SYMMETRIC_DEFAULT` plaintext 的限制是 4096 bytes。

相关约束见：

- [AWS KMS Encrypt API](https://docs.aws.amazon.com/kms/latest/APIReference/API_Encrypt.html)
- [AWS KMS Decrypt API](https://docs.aws.amazon.com/kms/latest/APIReference/API_Decrypt.html)

### 7.4 语法和行为对照

| 项目 | 环境变量 | AWS Secrets Manager | 直接 AWS KMS（预留，不实现） |
| --- | --- | --- | --- |
| 示例 | `${OPENAI_API_KEY}` | `${aws-sm:name#field}` | `secret://aws-kms/v1/id/ciphertext` |
| 配置保存的内容 | 变量名 | secret name/ARN | 加密后的 secret value |
| 值保存在哪里 | 进程环境的来源方 | Secrets Manager | 配置文件中的 ciphertext |
| 能否嵌入普通字符串 | 保持现有兼容，可以 | 当前 resolver 可以，但 secret 字段不建议 | 不可以 |
| 是否支持 JSON field | 不适用 | 支持顶层字符串字段 | 不支持 |
| 轮换方式 | 修改进程环境并 reload/restart | 新 secret version | 重新 Encrypt 并改写配置 |
| 删除方式 | 由服务管理器删除 | 删除或停用 secret | 删除配置中的 ciphertext |
| 远端依赖 | 无 | Secrets Manager API | KMS API |
| AWS KMS 的角色 | 无 | 保护 Secrets Manager data key | 直接解密 ciphertext blob |

## 8. 解析顺序

配置值的选择和 secret 的解析是两个步骤。

### 8.1 先决定有效配置值

沿用 Viper 的有效优先级。显式环境覆盖，例如 `MISTER_MORPH_LLM_API_KEY`，可以覆盖 `config.yaml` 中的字段。

这里的环境覆盖和 `${OPENAI_API_KEY}` 不是同一件事：

- `MISTER_MORPH_LLM_API_KEY` 覆盖整个配置字段；
- `${OPENAI_API_KEY}` 是所选配置值内部的引用。

### 8.2 再解析引用

对于最终选中的 secret 字段：

1. `${secret:...}`：从系统密钥管理器读取；
2. `${ENV_VAR}`：从当前进程环境读取；
3. `${aws-sm:...}`：从 AWS Secrets Manager 读取；
4. 其他值：按旧版明文处理。

这些不是逐级 fallback。一个被选中的引用解析失败后，不得继续尝试其他来源。

### 8.3 建立运行时快照

解析结果只写入内存中的 runtime config snapshot。Provider 得到普通字符串，不得到 reference 或 storage client。

同一个 snapshot 内：

- 相同引用只读取一次；
- 请求处理不再次访问 secret backend；
- reload 时建立新 snapshot，并重新解析；
- 旧 snapshot 在没有使用者后释放；
- Go string 无法可靠原地清零，因此不承诺内存取证防护，但要避免额外复制和长时间缓存。

现有 `${ENV_VAR}` 和 `${aws-sm:...}` 在 YAML 读取前展开。实现系统密钥管理器时，应逐步把 secret 字段的解析移动到“有效配置已经确定”之后，避免读取最终没有被选中的引用。普通非 secret 字段的 `${ENV_VAR}` 兼容行为不必同时重写。

## 9. 最小代码结构

复用并扩展 `internal/secref`，不要另建一套平行 resolver。

需要保留的概念只有三个：

- `Ref`：解析后的引用种类和 locator；
- `Resolver`：把引用解析成 runtime value；
- `OSStore`：系统密钥管理器的读、写、删能力。

建议能力边界：

```go
type OSStore interface {
    Get(ctx context.Context, id string) ([]byte, error)
    Put(ctx context.Context, id, configKey string, value []byte) error
    Delete(ctx context.Context, id string) error
}
```

不需要：

- Provider-specific secret store；
- secret backend 注册中心；
- 通用 plugin 接口；
- 每个字段一组 `GetAPIKey`、`SetBotToken` 薄包装；
- 让只读环境变量伪装成可写 `Store`。

`Resolver` 继续负责 env 和 AWS Secrets Manager，并增加 `${secret:...}`。写操作只用于 OS store，不需要把所有 source 强行抽象成同一个可写接口。

## 10. 操作系统实现

### 10.1 macOS

使用登录用户的 Keychain。Morph 进程直接调用 Security framework，不启动 `/usr/bin/security` 子进程。Keychain 因而按 Morph 应用身份执行访问控制；模型通过 bash 启动的普通命令不能直接继承这项权限。当前用户账户或 Morph 进程本身被控制仍不在本方案的防护范围内。

参考：[Apple Keychain Services](https://developer.apple.com/documentation/security/keychain-services)

### 10.2 Windows

使用 Windows Credential Manager。记录属于运行 Morph 的 Windows account。不要把 DPAPI 密文另存为自定义数据库，除非 Credential Manager 的容量限制形成真实问题。

参考：[Microsoft password handling guidance](https://learn.microsoft.com/en-us/windows/win32/secbp/handling-passwords)

### 10.3 Linux

使用 Freedesktop Secret Service，经当前用户 session D-Bus 访问 GNOME Keyring、KWallet 等实现。

参考：[Secret Service API Specification](https://specifications.freedesktop.org/secret-service/latest-single/)

常见的 headless systemd service 没有用户 D-Bus session，keyring 也可能没有解锁。此时系统密钥管理器应返回明确的 unavailable error。用户改用：

- systemd credentials 或环境变量；
- 容器 secret；
- AWS Secrets Manager；
- 明确接受风险后的 0600 配置文件。

程序不会创建独立的明文 fallback file。Settings 保存遇到这种情况时，会记录 warning，并继续使用原有的 0600 `config.yaml` 保存路径。

### 10.4 桌面应用身份

桌面安装包统一注册 `com.mistermorph`：

- macOS 使用相同的 `CFBundleIdentifier`，应用包继续携带 MisterMorph 图标；
- Linux 的 desktop file id、`Icon` 和 GTK program name 使用相同值，deb 和 AppImage 都携带相同图标；
- Windows manifest 使用相同的 assembly identity，exe 继续携带 MisterMorph 图标。

Linux Secret Service 记录同时使用 `com.mistermorph` 作为 schema name。密码管理器是否显示应用图标取决于其自身是否支持按桌面应用身份关联；不支持关联时仍显示可读 label。

## 11. 读取、写入、更新和删除

### 11.1 读取

1. 读取原始配置和环境覆盖；
2. 确定最终字段值；
3. 识别 reference；
4. 从对应 source 读取；
5. 建立 runtime snapshot；
6. API 只暴露 `configured` 和 `source`。

### 11.2 首次保存

系统密钥管理器可用时：

1. 后端生成新的 opaque id；
2. 把 secret 写入 OS store；
3. 原子更新 `config.yaml`，把字段改成 `${secret:<id>}`；
4. 重新读取并验证配置；
5. 生成新的 runtime snapshot。

如果第 3 步失败，应删除第 2 步刚写入但尚未被引用的记录。

如果生成 id 或第 2 步失败，应删除本次保存已经写入的其他新记录，保留请求中的明文值，记录 `os_secret_store_write_failed` warning，然后继续原有的配置保存流程。同一次 Settings 保存不会产生一部分 OS 引用、一部分明文的结果。

### 11.3 更新

不要覆盖旧记录后再修改配置。使用可恢复的轮换顺序：

1. 生成新 id；
2. 写入新 secret；
3. 原子替换 config reference；
4. 验证新 snapshot；
5. 删除旧 id。

如果第 5 步失败，记录 warning 并保留旧记录。它是可清理的 orphan，不影响当前配置。

### 11.4 清除

1. 从配置中删除 reference；
2. 验证新配置不再使用该 id；
3. 删除 OS store 记录。

只有在确认没有其他字段引用该 id 后才能删除记录。

### 11.5 OAuth refresh token

OAuth token 会变化，不能只实现启动时读取。负责刷新 token 的认证模块应通过同一个 OS store 更新原记录，或者使用新 id 做轮换。

Provider 仍不应直接调用系统密钥管理器。OAuth credential manager 是认证生命周期的一部分，可以持有受限的 OS store 依赖。

## 12. Console API 和 UI

### 12.1 读取

secret 字段返回统一状态，不返回 raw secret：

```json
{
  "configured": true,
  "source": "os",
  "editable": true
}
```

允许的 `source`：

- `os`
- `env`
- `aws_secrets_manager`
- `file`

`editable` 表示当前 Console 能否修改该来源：

- `os`：系统 store 可写时为 `true`；
- `env`：`false`；
- `aws_secrets_manager`：第一版为 `false`；
- `file`：`true`。

不要向前端返回：

- resolved value；
- OS store id；
- AWS secret ARN；
- KMS ciphertext；
- OAuth refresh token。

### 12.2 写入

前端提交新 secret 时，后端按目标 endpoint 的环境保存。HTTP payload 中包含一次 plaintext 是不可避免的，因此：

- 远程 Console 必须使用 TLS；
- request body 不进入 access log、debug log 或 dump；
- 响应不回显输入；
- 写入结束后前端清空输入框；
- 不把 secret 放进 URL、query string 或 toast。

### 12.3 远程 endpoint

`/e/<endpoint>/setup` 和 settings API 修改的是目标 endpoint。

当 Console A 管理 Morph B：

- secret 写入 B 所在机器、B 所在用户的系统密钥管理器；
- B 的 `config.yaml` 保存 B 的 `${secret:...}` reference；
- A 不保存 B 的 secret 或 reference；
- B 的系统 store 不可用时，B 记录 warning，并把本次提交值保存到 B 的配置文件；
- 不能退回写入 A 的 Keychain。

## 13. 错误和日志

第一版提供以下稳定错误分类：

- `secret_ref_invalid`
- `secret_not_found`
- `secret_store_unavailable`
- `secret_resolve_failed`

不同平台对 locked、permission denied 和会话不可用的错误表达不一致，第一版将它们统一为 `secret_store_unavailable`，不把 backend 原始错误返回给 API。只有出现需要不同恢复操作的实际场景后，再增加更细的分类。

日志可包含：

- source；
- operation；
- endpoint ref；
- opaque id 的短 hash；
- duration；
- error class。

日志不得包含：

- secret value；
- 完整 reference；
- KMS ciphertext；
- request body；
- provider Authorization header。

不要把 secret 缺失解析成空字符串后继续请求 Provider。这样会把本地配置错误伪装成上游 401。运行时应在启动、reload 或第一次使用该可选 profile 时给出明确错误。

## 14. 旧配置

### 14.1 不自动迁移

升级后：

- 明文仍按旧行为读取；
- `${ENV_VAR}` 保持原样；
- `${aws-sm:...}` 保持原样；
- OAuth token file 保持原样，直到对应认证流程接入 OS store。

启动时先探测系统密钥管理器是否可访问。失败只记录 `os_secret_store_unavailable` warning，不写配置、不删文件，也不阻止不依赖 OS 引用的运行时启动。

系统不提供单独的迁移动作。用户在 Settings 中重新提交某个 secret 后，该字段按正常保存规则写入系统密钥管理器；未提交的旧明文字段保持不变。

### 14.2 orphan 清理

第一版不扫描系统 store，也不依赖枚举能力。每次配置更新都比较提交前后的 `${secret:...}` 引用集合，并删除不再被配置引用的旧记录。删除失败不会回滚已经验证并提交的配置；残留记录不会影响当前配置，可由用户在系统密钥管理器中删除。

### 14.3 统一排障页

启动检查失败时，Console 进入 `/e/<endpoint>/troubleshooting`。接口返回稳定的问题代码、资源路径和相关配置字段，页面据此显示能解决当前问题的操作：

- 系统密钥不存在：补回原引用对应的密钥、移除引用或编辑源文件；
- 系统密钥管理器不可用：重试或编辑源文件；
- `${secret:...}` 格式无效：编辑源文件；
- 配置、Identity 或 Soul 内容无效：编辑源文件或重新配置；
- 文件无法读取：重试或重新配置。

补回密钥只更新系统密钥管理器，不改写 `config.yaml`。移除引用只删除对应配置字段，不把空值写回。页面不提供通用的“自动修复”动作，也不把所有故障都导向 Setup。

## 15. 安全边界

系统密钥管理器主要减少以下风险：

- 配置文件被误提交；
- 配置备份泄露；
- 普通文件读取暴露 secret；
- Web UI GET API 回传明文。

它不能解决：

- 当前用户账户已经被控制；
- Morph 进程内存被读取；
- 恶意 Provider 或恶意网络代理取得请求中的 credential；
- 有权限使用同一 keyring item 的本地程序；
- 远程 Console 没有 TLS。

运行时拿到 secret 后必然能使用它。设计重点是缩短暴露路径，不是声称明文永远不会进入内存。

## 16. 实现阶段

正式代码实现前，每一阶段先增加或更新测试，再改实现。

### Phase 1：引用和系统 store

- [x] 为 `internal/secref` 增加 `${secret:<id>}` 解析测试。
- [x] 增加 malformed ref、完整 scalar 限制和未知 source 测试。
- [x] 定义最小 `OSStore` 接口。
- [x] 接入 macOS Keychain、Windows Credential Manager 和 Linux Secret Service。
- [x] 使用 `com.mistermorph` application id、可读配置路径 label 和桌面应用图标关联。
- [x] 系统 store 不可用时返回稳定错误；Settings 写入失败时记录 warning 并回退到原配置保存行为。
- [x] runtime 启动时探测系统 store，可用性失败只记录 warning。
- [x] 同一 snapshot 内只读取一次相同引用。

### Phase 2：配置快照

- [x] 列出所有 secret-bearing 字段，包括 LLM profiles、Channel tokens、OAuth 和 auth profiles。
- [x] 在有效配置值确定后统一解析 secret 字段。
- [x] Provider 和 Channel runtime 只接收 resolved snapshot。
- [x] reload 重新解析；请求路径不访问 store。
- [x] 保持普通 scalar 的现有 `${ENV_VAR}` 兼容行为。

### Phase 3：Console

- [x] GET 只返回 `configured/source/editable`。
- [x] PUT 使用新 id 写入、原子更新配置、验证后删除旧 id。
- [x] clear 在确认无引用后删除记录。
- [x] local 和 remote endpoint 使用相同行为。
- [x] 系统 store 写入失败时清理本次新记录、记录 warning，并保存本次提交的明文值。

### Phase 4：安装和旧配置兼容

- [x] 桌面 setup 默认选择 OS store。
- [x] headless setup 保持 env 或 AWS Secrets Manager 配置路径。
- [x] 旧明文保持可读；字段再次保存时使用正常的 OS store 写入路径。
- [ ] OAuth token 按认证模块逐个迁移，不一次重写所有认证流程。
- [x] 更新 `assets/config/config.example.yaml` 和用户文档。
- [x] 将文件修复页改为统一排障页，并为密钥引用故障提供精确操作。

### Phase 5：清理

- [ ] 删除迁移完成后不再使用的 Provider-specific secret 文件逻辑。
- [ ] 删除重复的 env-managed / secret-presence 判断。
- [x] 保留一个 reference parser 和一个 API payload 语义。
- [ ] 检查日志、dump、journal 和错误链路没有明文。

直接 AWS KMS 不在以上 checklist 内。只有出现“必须把 ciphertext 与配置一起分发，且不能使用 Secrets Manager”的实际需求后，才单独评估。届时应先验证密文封装、encryption context、IAM、key rotation 和 4096-byte 限制，而不是只增加一个 resolver case。

## 17. 验收标准

- 新桌面安装可在 `config.yaml` 不含明文 secret 的情况下运行。
- macOS、Windows 和 Linux 桌面使用各自的系统密钥管理器。
- headless Linux 不依赖 Secret Service，能继续使用 env 或 AWS Secrets Manager。
- `${secret:...}` 只在完整 scalar 中有效。
- `${ENV_VAR}` 和 `${aws-sm:...}` 的现有配置无需修改。
- secret reference 解析失败时不会变成空 credential 请求，也不会静默写入明文。
- 本地和远程 Console 只能读取 secret 状态，不能读取 secret value。
- 更新 secret 时，配置与系统 store 不会因中途失败进入不可恢复状态。
- Provider、Channel 和 tool 不包含系统 store 分支。
- 日志、dump、journal、task 和 LLM context 不出现 secret value。
- 第一版没有直接 AWS KMS 后端、第三方 secret manager 插件系统或自制加密文件。
