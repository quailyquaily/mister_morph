---
date: 2026-04-20
title: Workspace Attachment Across Sessions
status: in_progress
---

# Workspace Attachment Across Sessions

## 1) 目标

这次要解决的是一个统一运行时能力，不是 CLI 小功能。

目标只有四件事：

- 允许把一个本地目录 attach 到当前 scope
- 让这个目录成为默认工作区
- 让 agent 和本地文件工具围绕这个目录形成一致语义
- 同时保留 `file_cache_dir` 和 `file_state_dir` 的原职责

这里真正新增的是第三类目录语义：

- `workspace_dir`
- `file_cache_dir`
- `file_state_dir`

## 2) 非目标

这版方案不解决下面这些事：

- 不做新的 sandbox
- 不做多 workspace 并存
- 这次只做 Console Local runtime 后端与 runtime API，不展开新的 UI 交互设计
- 不规定 attachment store 的具体文件格式
- 不处理 Slack thread-scoped workspace

## 3) 核心不变量

### 3.1 绑定对象不是进程，而是 scope

这次能力的最小抽象是：

- 一个稳定 scope key 最多绑定一个 workspace
- 这个 workspace 决定默认工作区语义

不是：

- 进程全局 `cwd`
- 进程全局 `workspace_dir`
- 某个 runtime 私有的临时魔法变量

### 3.2 这是工作区语义，不是权限边界

`workspace_dir` 负责：

- 默认读写路径
- 默认 shell 工作目录
- 项目文件的默认输出位置
- prompt 里的项目上下文

它不是新的安全边界。

现有安全边界仍然来自：

- guard
- deny-path / allowlist
- runtime 自己的权限约束

### 3.3 三类目录必须分开

三类目录的职责如下：

- `workspace_dir`：项目工作区
- `file_cache_dir`：下载、转换、中间产物
- `file_state_dir`：memory、TODO、contacts、skills、guard 等状态数据

不能再把：

- workspace 假装成 cache
- 状态目录假装成 workspace

### 3.4 当前 chat 的实现是错的

当前 CLI `chat` 把当前目录借道塞进 `file_cache_dir`。
这个实现本身就是错的。

做 `workspace_dir` 时，必须一起拆掉这层耦合：

- 当前目录应该进入 `workspace_dir`
- `file_cache_dir` 只做 cache

## 4) ScopeKey 规则

attachment store 的主键必须是 canonical conversation key。

不要同时接受多套主键。

第一阶段按现有 canonical key 走：

- Console: `console:<topic_id>`
- Telegram: `tg:<chat_id>`
- Slack: `slack:<team_id>:<channel_id>`
- LINE: `line:<chat_id>` 或 `line:<group_id>`
- Lark: `lark:<chat_id>`

这意味着：

- Console 不同 topic 可以绑定不同 workspace
- Console store 只认 `console:<topic_id>`
- 不使用 bus envelope 的 `session_id` 做 attachment key

## 5) 生命周期与持久化

第一阶段只需要下面这组规则：

- CLI `chat`：进程内临时状态，不进 attachment store
- CLI `run`：一次性运行参数，不进 attachment store
- Telegram / Slack / LINE / Lark：按 canonical conversation key 落 attachment store
- Console Local runtime：按 canonical conversation key 落 attachment store
- Console topic 删除时，同步删除 `console:<topic_id>` attachment

attachment store 只保存绑定关系。

最小数据模型够用即可：

```go
type WorkspaceAttachment struct {
    ScopeKey     string
    WorkspaceDir string
}
```

是否加时间戳、来源字段、JSON 还是 JSONL，都不是这版必须先定死的事情。

## 6) 命令协议

统一消息文本协议固定为：

- `/workspace`
- `/workspace attach <dir>`
- `/workspace detach`

不要再保留 `/attach` / `/detach` 这种平行命令。

这套协议适用于：

- CLI `chat`
- Telegram
- Slack
- LINE
- Lark

Console Local runtime 后端已经接入这套协议；结构化 Web API 也沿用同一组语义。

行为规则只有三条：

- `/workspace`：查看当前绑定状态
- `/workspace attach <dir>`：绑定目录；如果已有绑定，直接替换旧值，并明确回显替换结果
- `/workspace detach`：解绑

失败规则也固定下来：

- 路径不存在：失败
- 路径不可读：失败
- 当 runtime 配置了 allow roots 时，路径不在允许范围内：失败
- 不自动创建目录

## 7) 工具与路径语义

如果工具层不支持 `workspace_dir`，这个能力就只是贴皮。

所以第一阶段必须做到：

- `write_file` 支持 `workspace_dir/<path>` alias
- `read_file` 支持 `workspace_dir/<path>` alias
- 有 workspace 时，相对路径默认按 workspace 解析
- `bash` / `powershell` 默认 `cwd = workspace_dir`

同时保留两个边界：

- `url_fetch` 继续写 `file_cache_dir`
- TODO / memory / guard / contacts / skills 继续写 `file_state_dir`

这里要注意一点：

- `write_file` 的允许根会从两类目录变成三类目录

也就是：

- `workspace_dir`
- `file_cache_dir`
- `file_state_dir`

因此 guard、deny-path 和相关校验要一起更新。

## 8) 实现约束

实现上只需要定死下面三件事：

### 8.1 运行时要先解 scope，再解 roots

处理当前消息或任务前，runtime 需要先得到：

- 当前 canonical conversation key
- 当前 attached workspace

然后再构造这一轮运行使用的 path roots。

### 8.2 roots 要显式建模

不要再靠位置数组猜目录意义。

建议显式结构：

```go
type PathRoots struct {
    WorkspaceDir string
    FileCacheDir string
    FileStateDir string
}
```

### 8.3 命令解析要复用公用设施

`/workspace ...` 是共享文本协议。

不要让每个 runtime 自己拆字符串。
应复用现有公用命令解析设施，统一产出：

- status
- attach
- detach

## 9) 最终结论

这次需求的正确描述是：

> 为系统增加一个全局的 workspace attachment 能力，使不同 runtime 都能把本地目录附加到各自的 canonical conversation scope，并让 agent 与本地文件工具围绕这个 workspace 形成一致语义。

第一阶段明确四条规则：

- 一个 canonical conversation key 最多绑定一个 workspace
- CLI `chat` 临时保存，CLI `run` 只吃一次性参数，其他有稳定 conversation key 的 runtime 落 attachment store
- 统一命令协议是 `/workspace`、`/workspace attach <dir>`、`/workspace detach`
- 项目文件写 `workspace_dir`，临时文件写 `file_cache_dir`，系统状态写 `file_state_dir`

这一期已经覆盖：

- CLI
- 消息通道
- Console Local runtime 后端

这一期仍然不包含新的 Console workspace 专用 UI 控件。

## 10) Console Web API 与后端

### 10.1 当前边界

Console web 需要的是一个 UI 可直接调用的结构化 API。

但这不意味着要重新定义一套 workspace 语义。

当前实现仍然遵守前面已经定下来的规则：

- 绑定对象仍然是 scope，不是进程
- Console scope key 仍然是 `console:<topic_id>`
- attachment store 仍然只保存绑定关系
- 文本协议仍然是 `/workspace`、`/workspace attach <dir>`、`/workspace detach`

所以 Console web 真正新增的，不是新的业务语义，而是：

- 给浏览器一个结构化 HTTP 面
- 让 Console runtime 把 topic-scoped workspace 真正接进执行链

### 10.2 不要把 workspace 塞进 `/tasks` 或 `topic.json`

这件事要先说清楚，否则实现很容易跑偏。

不要做下面两种设计：

- 不要把 `workspace_dir` 塞进 `POST /tasks`
- 不要把 `workspace_dir` 持久化进 `tasks/console/topic.json`

原因很直接：

- task 是一次提交，不是长期绑定关系
- topic projection 是 Console 自己的查询视图，不是 attachment store
- workspace attachment 本来就应该独立存放，并以 canonical conversation key 为主键

如果把 workspace 写进 task 或 topic projection，等于又把三类目录语义和存储边界搅回去了。

### 10.3 HTTP 资源形态

这里已经采用简化后的单资源设计，不把 workspace 做成复杂的 topic 子资源。

最小 API 直接收成一个单资源：

- `GET /workspace?topic_id=<id>`
- `PUT /workspace`
- `DELETE /workspace?topic_id=<id>`

这里只保留前端真正需要的两个字段：

- `topic_id`
- `workspace_dir`

后端收到后，统一映射成 canonical key：

- `scope_key = console:<topic_id>`

浏览器不需要自己拼 `console:<topic_id>`。
store 也不需要暴露多套主键给前端。

### 10.4 返回体

`GET /workspace?topic_id=t_abc123`

```json
{
  "topic_id": "t_abc123",
  "workspace_dir": "/path/to/project"
}
```

未绑定时返回：

```json
{
  "topic_id": "t_abc123",
  "workspace_dir": ""
}
```

`PUT /workspace`

请求体：

```json
{
  "topic_id": "t_abc123",
  "workspace_dir": "/path/to/project"
}
```

响应体：

```json
{
  "topic_id": "t_abc123",
  "workspace_dir": "/path/to/project"
}
```

`DELETE /workspace?topic_id=t_abc123`

响应体：

```json
{
  "topic_id": "t_abc123",
  "workspace_dir": ""
}
```

这已经够表达三种状态：

- 有值：已绑定
- 空串：未绑定
- `PUT` 覆盖旧值：替换绑定

`workspace attached/replaced/detached` 这种文本回显，继续留给 `/workspace ...` 文本协议即可，不必塞进结构化 API。

### 10.5 错误语义

当前 Console Local backend 的错误语义如下：

- `400 Bad Request`
  - `topic_id` 非法
  - 请求体缺 `workspace_dir`
  - 路径不存在
  - 路径不是目录
  - 路径不可读
- `503 Service Unavailable`
  - 当前 runtime 不支持 topic-scoped workspace
- `500 Internal Server Error`
  - store 读写失败
  - runtime 内部错误

仍然保持：

- 不自动创建目录
- 不接受前端直接提交 canonical key
- 不接受“顺手帮我新建 topic + attach workspace”这种复合魔法动作

当前实现也没有强制做 `404 topic not found`。

原因不是不能做，而是没必要：

- attachment 的本体是 `scope_key -> workspace_dir`
- 当前通用 runtime 抽象里也没有统一的 `GetTopic()` 能力
- 为了一个 `404` 去扩 topic 接口，收益很低

更简单的做法是：

- `/workspace` 只关心 attachment store
- topic 删除成功时，顺手删掉 `console:<topic_id>` 的 attachment

这样职责更清楚，也更省实现成本。

补一条实现层面的事实：

- 当前 Console Local runtime 在 `PUT /workspace` 时调用的是 `workspace.ValidateDir(..., nil)`
- 也就是会检查存在、可读、目录类型，但还没有额外加 Console 自己的 allow-roots 约束
- 如果以后 Console 引入显式 allow-roots，再把“路径不在允许范围内”并入同一个 `400` 即可

### 10.6 文本协议仍然保留

Console web 做了结构化 API，并不意味着 `/workspace ...` 文本协议可以删掉。

当前后端已经同时保留两条入口：

- Chat 输入框里直接输入 `/workspace`
- UI 上通过结构化 API 做 attach / detach / status

两条入口共用的是同一份 store、同一套 scope key 规则和同一组 workspace 语义，但代码路径不完全相同：

- 文本协议路径：
  - `chatcommands.ParseCommand(...)`
  - `workspace.ExecuteStoreCommand(...)`
- HTTP 路径：
  - `workspaceDirForTopic(...)`
  - `setWorkspaceDirForTopic(...)`
  - `deleteWorkspaceDirForTopic(...)`

也就是说：

- 结构化 API 是给 UI 控件用的
- 文本协议是给 chat 入口和跨 runtime 一致性用的

### 10.7 当前后端接线

现在已经落下来的接线有四段。

第一段是 submit path：

- `consoleLocalRuntime.submitTask()` 会先判断输入是不是 `/workspace ...`
- 命中后不走 LLM 任务
- 直接执行 workspace 命令并返回 synthetic task result

第二段是 API path：

- `routesOptions(...)` 已经挂上 `WorkspaceGet`、`WorkspacePut`、`WorkspaceDelete`
- `/workspace` runtime API 已经可以直接读写 topic-scoped attachment

第三段是 accept/run path：

- `acceptTask(...)` 会按 `console:<topic_id>` 读取当前 attachment
- bus fallback 重建 job 时也会补回 `WorkspaceDir`
- 执行任务时会设置 `pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)`
- prompt augment 会 prepend `workspace.PromptBlock(job.WorkspaceDir)`

第四段是清理 path：

- topic 删除成功时，会同步删除 `console:<topic_id>` attachment

如果不做这段接线，Web UI 即使能 attach 成功，也不会真正影响：

- `read_file`
- `write_file`
- `bash`
- `powershell`
- prompt 里的项目上下文

那这个 API 只是摆设。

### 10.8 与现有前端调用方式的关系

当前 Console web 的 runtime 数据面已经统一走：

- `GET /api/proxy?endpoint=<ref>&uri=<runtime-path>`

所以不需要再造一个 Console 专用“workspace 总控 API”。

正确做法是：

- 把 `/workspace` 做成 runtime API
- 浏览器仍然通过现有 `/api/proxy` 转发

这样：

- `Console Local` 可直接支持
- 未来如果出现远端 console-like runtime，也能复用同一条路
- Console backend 不需要额外复制一遍 attachment 业务逻辑

### 10.9 UI 约束

当前后端实现仍然按“workspace 只绑定到已存在 topic”处理。

这意味着：

- 当前 topic 未创建时，不提供 attach 操作
- 前端在 `creatingTopic=true` 且没有真实 `topic_id` 时，应禁用 workspace 控件
- 用户先发第一条消息创建 topic，再 attach workspace

这是当前最小方案。

如果以后明确需要“先 attach，再开始第一条消息”，那是下一步单独讨论的事。
到那时再决定是否补：

- `POST /topics`
- 或 `POST /tasks` 支持显式创建空 topic

但这不是这次 Console workspace API 的最小范围。

## 11) Checklist

### 11.1 路径模型

- [x] 引入显式 `PathRoots`
- [x] 在运行时构造 `PathRoots{WorkspaceDir, FileCacheDir, FileStateDir}`
- [x] 去掉当前 CLI `chat` 对 `file_cache_dir = cwd` 的错误耦合

### 11.2 attachment store

- [x] 新增 workspace attachment store
- [x] store 主键固定为 canonical conversation key
- [x] store value 最小只保存 `WorkspaceDir`
- [x] `attach` 时覆盖旧值，不保留双绑定
- [x] `detach` 时删除当前绑定

### 11.3 scope key 接线

- [x] Telegram 统一使用 `tg:<chat_id>`
- [x] Slack 统一使用 `slack:<team_id>:<channel_id>`
- [x] LINE 统一使用 `line:<chat_id>` 或 `line:<group_id>`
- [x] Lark 统一使用 `lark:<chat_id>`

### 11.4 命令解析

- [x] 复用现有公用命令解析设施
- [x] 支持 `/workspace`
- [x] 支持 `/workspace attach <dir>`
- [x] 支持 `/workspace detach`
- [x] 非法语法返回稳定错误

### 11.5 CLI

- [x] `chat` 支持 `--workspace <dir>`
- [x] `chat` 支持 `--no-workspace`
- [x] `chat` 默认当前目录作为 `workspace_dir`
- [x] `chat` 进程内保存当前 workspace
- [x] `run` 支持 `--workspace <dir>`
- [x] `run` 支持 `--no-workspace`
- [x] `run` 默认当前目录作为 `workspace_dir`
- [x] `run` 不写 attachment store

### 11.6 Console Local backend

- [x] Console 聊天输入可透传 `/workspace` 文本协议
- [x] runtime API 新增 `GET /workspace?topic_id=<id>`
- [x] runtime API 新增 `PUT /workspace`
- [x] runtime API 新增 `DELETE /workspace?topic_id=<id>`
- [x] Console 统一使用 `console:<topic_id>`
- [x] 一个 topic 可绑定一个 workspace
- [x] 不同 topic 可绑定不同 workspace
- [x] 切 topic 时切换当前 workspace
- [x] 刷新后能从 attachment store 恢复绑定
- [x] topic 删除成功时同步删除 `console:<topic_id>` attachment
- [x] Console runtime 在 run path 接入 workspace lookup + `pathroots.WithWorkspaceDir`
- [x] Console runtime 在 prompt augment 接入 `workspace.PromptBlock`
- [ ] Console workspace 专用 UI 控件

### 11.7 Channel runtimes

- [x] Telegram 接入 `/workspace` 文本协议
- [x] Slack 接入 `/workspace` 文本协议
- [x] LINE 接入 `/workspace` 文本协议
- [x] Lark 接入 `/workspace` 文本协议
- [x] 这些 runtime 重启后能从 attachment store 恢复绑定

### 11.8 工具层

- [x] `write_file` 支持 `workspace_dir/<path>` alias
- [x] `read_file` 支持 `workspace_dir/<path>` alias
- [x] 有 workspace 时，相对路径默认按 workspace 解析
- [x] `bash` 默认 `cwd = workspace_dir`
- [x] `powershell` 默认 `cwd = workspace_dir`
- [x] `url_fetch` 继续写 `file_cache_dir`
- [x] TODO / memory / guard / contacts / skills 继续写 `file_state_dir`
- [x] guard 与 deny-path 校验覆盖三类目录

### 11.9 返回语义

- [x] `/workspace` 返回当前绑定状态
- [x] 首次 attach 明确返回绑定成功
- [x] 替换 attach 明确返回旧目录到新目录的切换结果
- [x] `detach` 明确返回解绑成功
- [x] 路径不存在时返回失败
- [x] 路径不可读时返回失败
- [x] 当 runtime 配置了 allow roots 时，路径不在允许范围内返回失败
- [x] 不自动创建目录

### 11.10 最小测试（当前）

- [x] attachment store 按 canonical key 读写正常
- [x] 同一 key 重复 attach 只保留最新值
- [x] detach 后绑定消失
- [x] `/workspace` 三种语法解析正确
- [x] Console `/workspace` 文本协议返回 synthetic task result
- [x] Console `acceptTask` 能从 attachment store 恢复 workspace
- [x] Console topic 删除会清理 workspace attachment
- [x] Console `/workspace` runtime API 的 `GET` / `PUT` / `DELETE` 已覆盖测试
- [ ] CLI `chat` 不再把 cwd 塞进 `file_cache_dir`
- [ ] Telegram / Slack / LINE / Lark 重启后能恢复绑定
- [x] `write_file` / `read_file` / shell 工具按 workspace 生效
