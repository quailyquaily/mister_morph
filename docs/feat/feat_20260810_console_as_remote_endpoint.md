---
date: 2026-08-10
title: Console 作为 Remote Endpoint
status: implemented
---

# Console 作为 Remote Endpoint

## 1. 需求

两个独立进程都运行 `mistermorph console serve` 时，一个 Console 应能把另一个 Console 的 `Console Local` 当作普通 remote endpoint。现有 endpoint 页面应能查看和操作它的数据、提交任务，并修改允许在线修改的 Agent Settings。

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
| `<base_path>/api/*` | Console session | 登录、Console 设置、endpoint 列表和浏览器代理 |
| `<base_path>/runtime/*` | `server.auth_token` | 当前进程的 `Console Local` runtime |

Console A 管理 Console B 时，请求路径为：

```text
Browser -> Console A /api/proxy -> Console B /runtime/* -> Console B daemonruntime handler
```

`/api/proxy` 继续只作为浏览器访问 endpoint 的代理，不增加机器认证，也不作为另一个 Console 的 endpoint URL。

## 3. 兼容性

独立 runtime 现有的根路径继续作为兼容别名：

```text
/health           # 旧地址
/runtime/health   # 标准地址
```

两条路径调用同一个 handler，不复制 route 实现。现有 endpoint 配置和 health check 不会失效；新文档、示例配置和安装向导一律生成带 `/runtime` 的 URL。

Console 不提供根路径兼容别名，因为 `/health` 和 `/` 已属于 Console server。它只在 `<base_path>/runtime/*` 暴露 Local runtime。

这项改动不修改 runtime route、`console.endpoints` 数据结构、remote endpoint client 或 Web UI。

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

远端 Console 暴露完整的现有 `daemonruntime` contract，不单独维护 route 列表。Agent Settings 仍只在线修改 `llm`、`skills` 和 `tools`；写入和 generation 更新都发生在目标 runtime。

以下内容属于 Console API，不属于 runtime endpoint：

- Console 的登录密码和 session；
- `console.listen`、`console.base_path`、`console.endpoints` 和 `console.managed_runtimes`；
- Console Settings 和 auto-update 设置；
- 进程启动、停止或重启；
- 该 Console 自己连接的其他 remote endpoints。

managed runtimes 仍合并在本进程的 `Console Local` 中，不作为独立 endpoints 暴露。远端任务继续使用现有 remote endpoint 的轮询方式；本功能不代理另一个 Console 的 WebSocket。

## 7. 实现

1. 独立 runtime 的 HTTP server 把同一个 `daemonruntime` handler 挂到 `/runtime/*`，并保留根路径兼容入口。
2. Console 在有效 `server.auth_token` 非空时，把 `localRuntime.currentHandler()` 挂到 `<base_path>/runtime/*`。
3. 两处都只去掉公开前缀后调用现有 handler，不复制 route。
4. Console 每次请求读取当前 handler，配置 reload 后不会继续使用旧 generation。
5. 更新 `docs/console.md`、示例配置和安装向导中的 endpoint URL。

不增加 package、配置字段、client 类型、认证方式或前端分支。

## 8. 验收

1. 独立 runtime 的 `/runtime/health` 和受保护 route 可用，旧根路径仍返回相同结果。
2. Console A 能通过 Console B 的 `/runtime` 发现 Agent、读写 runtime 数据并提交任务。
3. 缺少或使用错误 token 时，受保护 route 返回 `401`；Console 未显式配置 token 时，`<base_path>/runtime/*` 返回 `404`。
4. 非默认 `console.base_path` 可用；Agent Settings reload 后，新请求使用新 generation。
5. 现有 `/api/*`、endpoint 配置、独立 runtime 根路径和 Console 本地功能保持兼容。

自动化测试覆盖标准路径、旧路径兼容、开启条件、认证、base path、generation 切换，以及一个 Console 经 `/runtime` 对另一个 Console 执行 health check 和提交任务。
