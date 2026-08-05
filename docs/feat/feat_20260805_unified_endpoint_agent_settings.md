---
date: 2026-08-05
title: Local 与 Remote Endpoint 统一 Agent Settings
status: implemented
---

# Local 与 Remote Endpoint 统一 Agent Settings

## 1. 决定

Local 和 remote 只表示请求如何到达 runtime，不决定设置是否可写。

可写 endpoint 使用相同的 Agent Settings 机制：

1. 从当前有效配置生成设置视图。
2. 标明由环境变量或 Secret Manager 管理的字段。
3. 校验 patch，并原子写入该 runtime 自己的配置文件。
4. 监听配置文件变化，为后续任务建立新 generation。

Console 访问 local endpoint 时直接调用共享 handler；访问 remote endpoint 时经 `/proxy` 转发到 remote 的同一个 handler。Console 不解释 remote 配置，也不写 remote 主机的文件。

只有 runtime 没有提供写入能力时才返回只读。只读状态和原因由 runtime 返回，Web UI 不根据 endpoint 类型猜测。

## 2. 边界

### 2.1 在线生效的设置

本功能只在线重载 Agent Settings 对应的三个顶层配置域：

| 配置域 | 在线生效 |
| --- | --- |
| `llm`，包括 profiles 和 routes | 是 |
| `skills` | 是 |
| `tools` | 是 |
| channel 凭据和连接参数 | 否，重启生效 |
| 路径、日志、全局限制 | 否，重启生效 |
| MCP、guard、memory、cron 等其他配置 | 否，重启生效 |

配置监听器虽然观察整个配置文件，但会把 boot-only 配置固定为进程启动时的值。只修改 boot-only 配置不会建立无意义的新 generation。

这条边界避免了两类问题：

- channel token 或监听参数在线变化，却没有相应的连接重启协议；
- 路径、MCP 或 guard 等进程资源在任务运行中被半更新。

需要在线修改其他配置域时，应先为该资源定义完整的建立、切换和清理语义，再把它加入可重载范围。

### 2.2 配置来源

有效值可以来自 defaults、配置文件、环境变量、启动参数、Secret Manager 或宿主程序。来源优先级不因 local 或 remote 改变。

Agent Settings API 只修改 runtime 管理的配置文件。它不反向修改环境变量、启动参数或 Secret Manager。外部来源覆盖的字段继续显示当前有效值，并附带管理来源；API 不能替换或删除这些字段。

### 2.3 runtime 所有权

每个独立 endpoint 只管理自己的配置文件和 generation：

- Console Local 管理 Console 进程的配置。
- 独立 Telegram、Slack、LINE 或 Lark 进程管理各自的配置。
- Console managed runtime 与 Console Local 共用进程和配置，不另建 settings endpoint。
- 嵌入式宿主可以提供可写 owner、只读 owner，或不注册 Agent Settings route。

本功能不做跨进程配置同步，也不建立配置分发服务。

## 3. API 契约

### 3.1 `GET /settings/agent`

Local 与 remote 返回相同字段：

- `llm`
- `env_managed`
- `skills`
- `tools`
- `config_path`
- `config_exists`
- `config_valid`
- `config_source`
- `read_only`
- `read_only_reason`，仅只读时需要

可写 endpoint 明确返回：

```json
{
  "read_only": false
}
```

只读 endpoint 返回：

```json
{
  "read_only": true,
  "read_only_reason": "runtime settings are read-only: settings writer is unavailable"
}
```

前端只根据 `read_only` 控制整个表单，再根据 `env_managed` 控制具体字段。

### 3.2 `PUT /settings/agent`

Local 与 remote 接受相同的 patch：

1. 解析 JSON。
2. 读取 endpoint 自己的 YAML 文档。
3. 保留未修改字段和其他配置域。
4. 保护外部管理字段和敏感字段。
5. 校验结果。
6. 原子写入 endpoint 自己的配置路径。
7. 返回持久化后的设置视图。

`200 OK` 只表示配置已持久化，不表示新 generation 已完成切换。这与 Console 现有保存语义一致。手工修改配置文件和 API 写入都由同一个监听器处理。

错误语义：

- 非法 patch 或配置：`400 Bad Request`
- 修改外部管理字段：`409 Conflict`
- 只读 owner：`405 Method Not Allowed`
- 身份验证失败：`401 Unauthorized`
- 文件系统错误：返回真实服务端错误，不伪装成 remote 只读

### 3.3 Models 和连接测试

`/settings/agent/models` 与 `/settings/agent/test` 也使用共享 handler。它们合并请求内候选值和 endpoint 当前有效配置，不要求先保存。

## 4. 最小内部结构

### 4.1 `internal/agentsettings`

该包只负责共享的设置语义：

- typed request、patch 和 view；
- YAML 读取与更新；
- provider、profile 和 route 处理；
- 外部管理字段保护和 secret 脱敏；
- 配置校验；
- models 与连接测试；
- HTTP handler。

Console 和 daemon 不再各自维护一份实现。Console 只保留 route 接入和测试依赖注入。

### 4.2 三种配置能力

三个依赖不能按名称合并，因为生命周期不同：

| 能力 | 职责 |
| --- | --- |
| `AgentSettingsOwner` | 为 HTTP 控制面提供当前 view 和持久化更新 |
| `AgentSettingsReader` | 某一 generation 的不可变配置快照，也供其他 daemon route 读取 |
| `RuntimeConfigSource` | 读取候选配置，并在 generation 成功切换后更新 current reader |

`RuntimeConfigSource` 是显式依赖。不能通过对 owner 做类型断言来偷偷启用热更新。静态或嵌入式 runtime 可以只提供 reader；只读 endpoint 可以只提供只读 owner。

同一个文件 owner 可以同时实现 `AgentSettingsOwner` 和 `RuntimeConfigSource`，但这是一个对象提供两种能力，不是两份配置状态。

### 4.3 Generation 切换

独立 channel runtime 使用以下流程：

```text
配置文件变化
  -> 读取完整文件
  -> 只把 llm / skills / tools 覆盖到启动快照
  -> 与当前快照比较
  -> 构建候选 generation
  -> 原子切换 current generation
  -> 旧任务释放引用后清理旧 generation
```

具体规则：

1. 候选配置无效或构建失败时，当前 generation 不变。
2. 候选配置与当前配置等价时，不构建新 generation。
3. 已开始的任务持有旧 generation；新任务捕获 current generation。
4. generation 清理会取消其后台 context，并关闭它拥有的 memory、LLM 和 MCP 等资源。
5. HTTP server 属于进程，不属于 generation；切换不会中断控制面请求。

## 5. Web UI

Web UI 删除了 `remote endpoint => read only` 规则。加载任意 endpoint 时：

1. 先进入 loading，避免编辑上一 endpoint 的表单。
2. 请求当前 endpoint 的 `GET /settings/agent`。
3. 使用响应中的 `read_only` 和 `read_only_reason`。
4. 保存时向当前 endpoint 发送同一个 `PUT /settings/agent` payload。
5. Codex OAuth 的 status、refresh、login、poll 和 logout 请求也发送到当前 endpoint；remote 请求经 `/proxy` 转发。
6. Codex OAuth 登录 session 固定到发起登录时的 endpoint。切换 endpoint 会停止旧 session 的前端轮询并清空旧状态。
7. `openai_codex` 的 OAuth 按钮不按 local 或 remote 隐藏。有效 endpoint 和 API Key 同时非空时，按钮保留但 disabled。
8. OAuth 登录只更新当前 endpoint 的 token。LLM 设置仍由表单保存，不因登录改默认 provider 或清空 endpoint。
9. inference provider 来自环境变量等外部来源时，认证按钮仍然显示。

请求失败时显示真实错误，不能把失败解释成 remote 只读。

## 6. 安全

1. Remote Agent Settings 使用 daemon 现有认证。
2. Console proxy 只转发已授权请求，不增加权限。
3. API 响应不返回受管理 secret 的实际值。
4. 日志不记录 API key、token、Secret Manager 返回值或完整设置请求。
5. remote 不是权限。若以后需要读写 scope，应同时适用于 local 和 remote。
6. Codex OAuth token 始终保存在处理请求的 runtime；Console proxy 不读取或返回 token。
7. Console proxy 标记 upstream 响应。Remote 返回 `401` 时，Web UI 不清除本地 Console session。

## 7. 失败处理

- 配置文件不存在：按统一配置路径规则创建。
- 配置文件不可写：返回实际 I/O 错误。
- YAML 无效：GET 返回 defaults 视图并标明配置无效；PUT 必须先生成有效文档。
- generation 构建失败：继续使用最后一份有效 generation，并记录错误。
- 外部来源继续覆盖文件值：响应通过 `env_managed` 显示该事实。

## 8. 非目标

- 不修改环境变量、启动参数或 Secret Manager。
- 不同步不同 endpoint 的配置。
- 不让 Console 访问 remote 文件系统。
- 不为 boot-only 配置补在线重启协议。
- 不建立适用于所有配置域的通用框架。

## 9. 验收

1. 可写 remote endpoint 能修改 LLM、profiles、skills 和 tools。
2. Local 与 remote 使用相同 payload、校验和错误语义。
3. Remote 只写自己的配置文件，刷新和进程重启后设置仍存在。
4. 配置变更只影响新任务；旧任务继续使用原 generation。
5. 无效配置或候选构建失败不影响当前 runtime。
6. 真正只读的 endpoint 返回具体原因。
7. Web UI 不包含 Agent Settings 专用的 local/remote 可写性分支。
8. Remote endpoint 的 Codex OAuth 状态、静默续期、登录、轮询和退出操作只影响该 endpoint。
9. Endpoint 和 API Key 同时非空时，Codex OAuth 按钮可见但不可点击。
10. OAuth 登录不修改当前表单中的 endpoint、profile 或默认 LLM。
11. Go 测试、静态检查和 Console 构建通过。

## 10. 实现结果

- Local 与 daemon route 已使用共享 handler 和 owner。
- Local Console 与 daemon 的 Codex OAuth route 已使用共享 handler；token 写入各 runtime 自己的 `file_state_dir`。
- 独立 Telegram、Slack、LINE、Lark 已提供文件 owner 和显式 runtime config source。
- daemon 在没有 owner 时使用只读 owner，并返回具体原因。
- Console 的重复 Agent Settings 实现已删除。
- generation reload 已限制在 `llm / skills / tools`，支持等价配置跳过和旧 generation 延迟清理。
- Web UI 已改为相信 endpoint 返回的能力。
- Web UI 的 Codex OAuth 登录不再隐式写 LLM 设置，环境托管 provider 时也保留认证入口。
- Console proxy 区分本地认证失败和 remote upstream 的 `401`。
