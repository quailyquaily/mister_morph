---
date: 2026-08-10
title: Console 作为 Remote Endpoint
status: implemented
---

# Console 作为 Remote Endpoint

## 1. 需求

两个独立进程都运行 `mistermorph console serve` 时，一个 Console 应能把另一个 Console 的 `Console Local` 当作普通 remote endpoint。现有 endpoint 页面应能查看和操作它的数据、提交任务，并修改目标 Console 在本地 UI 中可修改的同一组设置。

此前不能这样使用。`Console Local` 的 `daemonruntime` handler 只在进程内调用，没有 TCP 路由。`console.endpoints` 则要求目标地址直接提供 `/health`、`/tasks`、`/settings/agent` 等 runtime API。另一个 Console 的 `/api/proxy` 使用短期 Console session，也不是稳定的 runtime endpoint。

## 2. API 设计

所有 MisterMorph runtime 使用 `/runtime` 作为标准公开前缀：

| 进程 | 标准 runtime API base URL |
| --- | --- |
| 独立 Telegram、Slack、LINE、Lark runtime | `<scheme>://<host>:<port>/runtime` |
| Console Local | `<scheme>://<host>:<port>/<optional-base-path>/runtime` |

runtime handler 内部的相对路由保持不变：

```text
<endpoint-url>/health
<endpoint-url>/tasks
<endpoint-url>/settings/agent
```

`console.endpoints[].url` 始终表示完整的 runtime API base URL。client 不自行追加或判断 `/runtime`。

Console 同一个 listener 上的两类 API 使用不同命名空间：

| 路径 | 认证 | 职责 |
| --- | --- | --- |
| `<base_path>/api/*` | Console session | 浏览器登录、endpoint 列表、代理，以及当前 Console 的设置入口 |
| `<base_path>/runtime/*` | `server.auth_token` | 当前进程的 runtime 数据和目标 Console 设置入口 |

Console A 管理 Console B 时，请求路径为：

```text
Browser -> Console A /api/proxy -> Console B /runtime/* -> Console B daemonruntime handler
```

`/api/proxy` 继续只作为浏览器访问 endpoint 的代理，不增加机器认证，也不作为另一个 Console 的 endpoint URL。

Console 自己拥有的设置和账号路由同时注册到 `/api` 与 `/runtime`。两边调用同一组 handler，只使用不同认证：

```text
/settings/agent
/settings/agent/models
/settings/agent/test
/settings/console
/settings/auto-update
/settings/auto-update/check
/auth/codex/*
/auth/xai/*
/auth/pro/*
```

## 3. 兼容性

独立 runtime 现有的根路径继续作为兼容别名：

```text
/health           # 旧地址
/runtime/health   # 标准地址
```

两条路径调用同一个 handler，不复制 route 实现。现有 endpoint 配置和 health check 不会失效；新文档、示例配置和安装向导一律生成带 `/runtime` 的 URL。

Console 不提供根路径兼容别名，因为 `/health` 和 `/` 已属于 Console server。它只在 `<base_path>/runtime/*` 暴露 Local runtime。

这项改动不修改 `console.endpoints` 数据结构或 remote endpoint client。

## 4. 开启与认证

只有有效配置中的 `server.auth_token` 非空时，Console 才注册 `<base_path>/runtime/*`。没有显式 token 时：

- `Console Local` 仍使用自动生成的进程内 token；
- `<base_path>/runtime/*` 不注册，对外返回 `404`；
- 本机 Console 的现有功能不受影响。

不增加单独的 enable 开关。`server.auth_token` 已经是 runtime API 的认证配置；再增加一个布尔值只会产生无效组合。

- `/runtime/health` 保持现有 daemon health 语义，不要求认证。
- 其他 runtime route 继续校验 `Authorization: Bearer <server.auth_token>`。
- Console 登录密码和 Console session token 不能替代 `server.auth_token`。
- Console A 在服务端保存并附加 endpoint token，浏览器不会取得该 token。
- 跨主机连接应使用 HTTPS 或可信私网。

`server.auth_token`、`console.listen` 和 `console.base_path` 都按启动配置处理，修改后需要重启进程。

## 5. 配置示例

Console B 暴露自己的 runtime：

```yaml
server:
  auth_token: "${MISTER_MORPH_SERVER_AUTH_TOKEN}"

console:
  listen: "0.0.0.0:9081"
```

Console A 连接 Console B：

```yaml
console:
  endpoints:
    - name: "Morph B"
      url: "https://morph-b.example.com/runtime"
      auth_token: "${MISTER_MORPH_ENDPOINT_B_TOKEN}"
```

`MISTER_MORPH_ENDPOINT_B_TOKEN` 的值必须等于 Console B 的 `server.auth_token`。Console B 的登录密码和 Console session token 不参与 endpoint 认证。

若 Console B 配置了：

```yaml
console:
  base_path: "/morph"
```

则 endpoint URL 为：

```text
https://morph-b.example.com/morph/runtime
```

独立 Telegram runtime 的新配置使用相同形式：

```yaml
console:
  endpoints:
    - name: "Telegram"
      url: "http://telegram.example.com:8787/runtime"
      auth_token: "${MISTER_MORPH_ENDPOINT_TELEGRAM_TOKEN}"
```

同一台机器运行两个 Console 时，两者必须使用不同的 `console.listen`、配置文件和 `file_state_dir`，避免端口冲突及并发读写同一份状态。

## 6. 范围

远端 Console 暴露完整的现有 `daemonruntime` contract，并暴露本地 Console 已有的目标设置能力：

- Agent、Tools、Skills；
- Persona；
- Channels、Managed Runtimes、Guard；
- auto-update 设置和版本检查；
- Codex、xAI、MisterMorph Pro 的账号状态、登录和退出。

所有写入都发生在目标 Console。Agent Settings 的 generation 更新也只发生在目标 runtime。Channels、Managed Runtimes、Guard 等需要重启的配置仍保持原有生效时机；remote 不改变它们的生命周期。

以下内容不属于所选 runtime endpoint 的设置：

- Console 的登录密码和 session；
- 语言等浏览器偏好；
- `console.listen`、`console.base_path` 和 `console.endpoints`；
- 进程启动、停止或重启；
- 该 Console 自己连接的其他 remote endpoints。

这些内容在本地 Settings UI 中也不是所选 endpoint 的可写设置。退出登录始终退出当前浏览器连接的 Console A，不会退出 Console B。

managed runtimes 仍合并在本进程的 `Console Local` 中，不作为独立 endpoints 暴露。远端任务继续使用现有 remote endpoint 的轮询方式；本功能不代理另一个 Console 的 WebSocket。

## 7. 实现

1. 独立 runtime 的 HTTP server 把同一个 `daemonruntime` handler 挂到 `/runtime/*`，并保留根路径兼容入口。
2. Console 在有效 `server.auth_token` 非空时，把 `localRuntime.currentHandler()` 挂到 `<base_path>/runtime/*`。
3. Console-owned settings 使用同一份注册表挂到 `/api` 和 `/runtime`；前者校验 Console session，后者校验 `server.auth_token`。
4. 其他 runtime route 只去掉公开前缀后调用现有 handler。Console 每次请求读取当前 handler，配置 reload 后不会继续使用旧 generation。
5. Web UI 根据 endpoint 返回的 `mode` 判断它是否为 Console，不根据 local 或 remote 隐藏设置或账号操作。
6. 更新 `docs/console.md`、示例配置和安装向导中的 endpoint URL。

不增加 package、配置字段、client 类型或认证方式。

## 8. 验收

1. 独立 runtime 的 `/runtime/health` 和受保护 route 可用，旧根路径仍返回相同结果。
2. Console A 能通过 Console B 的 `/runtime` 发现 Agent、读写 runtime 数据并提交任务。
3. Console A 选择 Console B 后，Agent、Tools、Skills、Persona、Channels、Managed Runtimes、Guard、auto-update 和账号操作与选择本地 Console 时一致，写入只影响 B。
4. 缺少或使用错误 token 时，受保护 route 返回 `401`；Console 未显式配置 token 时，`<base_path>/runtime/*` 返回 `404`。
5. 非默认 `console.base_path` 可用；Agent Settings reload 后，新请求使用新 generation。
6. 现有 `/api/*`、endpoint 配置、独立 runtime 根路径和 Console 本地功能保持兼容。

自动化测试覆盖标准路径、旧路径兼容、开启条件、认证、base path、generation 切换、Console-owned settings 路由，以及一个 Console 经 `/runtime` 对另一个 Console 执行 health check 和提交任务。
