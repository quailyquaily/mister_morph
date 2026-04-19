---
date: 2026-04-20
title: Workspace Attachment Across Sessions
status: draft
---

# Workspace Attachment Across Sessions

## 1) 目标

这次需求不是 CLI 小功能。

它是一个全局能力：

- 允许把一个本地目录 attach 到当前 session
- 让这个目录进入 agent 上下文，成为默认工作区
- 让默认输出文件落到这个目录
- 同时继续保留 `file_cache_dir` 和 `file_state_dir` 的原职责

这里的关键不是“多一个路径参数”。
关键是系统需要正式承认第三类目录语义：

- `workspace_dir`
- `file_cache_dir`
- `file_state_dir`

## 2) 先纠正范围

前一版把标题写成了 `Workspace Dir for CLI Sessions`。
这个判断不对。

正确范围应该是：

- CLI `chat`
- CLI `run`
- Console Web
- Telegram runtime
- Slack runtime

后续如果要继续扩，也应该按同一个模型接入：

- LINE
- Lark
- 其他 future runtime

也就是说，这不是某个入口单独发明的新语义。
这是一个“session 可以 attach workspace”的统一运行时能力。

## 3) 第一性原理

### 3.1 attach 的对象是 session，不是进程

用户说的是“attach 到当前 session”，不是“改掉整个进程的工作目录”。

所以这次能力的最小抽象应该是：

- 一个 session 可以有零个或一个 attached workspace
- 这个 attached workspace 决定默认工作区语义

而不是：

- 进程全局 `cwd`
- 进程全局 `workspace_dir`
- 某个 runtime 的私有魔法变量

### 3.2 这是工作区语义，不是安全边界

`workspace_dir` 的作用是：

- 默认读写路径
- 默认 shell 工作目录
- 文件树展示范围
- prompt 中的项目上下文

它不是新的 sandbox。

现有安全边界仍然应该来自：

- guard
- deny-path / allowlist
- runtime 自己的权限约束

### 3.3 attach 的是“目录引用”，不是把文件搬进状态目录

attach 以后，系统保存的是：

- 这个 session 当前绑定了哪个目录

不是：

- 把 workspace 内容复制到 `file_state_dir`
- 把目录快照写成一份新状态

## 4) 三类目录职责

### 4.1 `workspace_dir`

它表示当前 session attach 的项目目录。

用途：

- 默认输出项目文件
- 默认 shell `cwd`
- 文件树展示根目录
- 当用户没给路径时，默认写到这里

### 4.2 `file_cache_dir`

它继续表示缓存和中间产物目录。

用途：

- 下载文件
- 转格式临时文件
- 二进制中间产物
- 图片、音频、PDF 等抓取结果

### 4.3 `file_state_dir`

它继续表示 morph 自己的状态目录。

用途：

- memory
- TODO
- contacts
- skills
- guard approvals / audit
- runtime 所需的持久化元数据

### 4.4 结论

这三个目录必须彻底分开。

不能再把：

- workspace 假装成 cache
- runtime 状态假装成 workspace

否则后面 Console、Telegram、Slack 接进来以后会越来越乱。

## 5) 全局数据模型

建议引入一个统一概念：`WorkspaceAttachment`。

最小形态可以是：

```go
type WorkspaceAttachment struct {
    ScopeType   string // session | conversation
    ScopeKey    string // stable runtime key
    WorkspaceDir string
    AttachedAt  time.Time
    UpdatedAt   time.Time
    Source      string // cli | console | telegram | slack
}
```

这里最重要的字段不是时间戳。
最重要的是 `ScopeKey`。

因为 attach 持久化必须绑到一个稳定键上。

## 6) attach 的稳定键应该是什么

这件事不能含糊。

“当前 session”在不同 runtime 里的稳定标识不一样。

### 6.1 CLI `chat`

CLI `chat` 是单进程内的交互 session。

如果只是当前进程内临时 attach，可以是：

- in-memory session state

如果以后要支持恢复，也需要一个显式 session id。

但这次需求下，CLI 更自然的语义还是：

- 默认临时 attach
- 退出进程后自然消失

### 6.2 CLI `run`

`run` 是 one-shot 执行。

它的 workspace 本质上就是这一次 run request 的运行参数。

所以 `run` 不需要单独持久化 attach。

### 6.3 Console Web

Console 当前天然有 topic / conversation 语义。

已有稳定键：

- `topic_id`
- `conversation_key = console:<topic_id>`

这里 attach 的自然作用域应该是：

- 当前 topic

也就是：

- 一个 topic 可以绑定一个 workspace

注意：

当前 Console bus envelope 里的 `session_id` 并不适合拿来做 attach 持久化键。
因为当前实现里，如果 `topic_id` 不是 UUIDv7，会临时生成新的 `session_id`。

所以 Console attach 的稳定键应该是：

- `conversation_key`
或
- `topic_id`

不应该是 bus envelope 的 `session_id`。

### 6.4 Telegram

Telegram 当前 memory / conversation 语义已经是：

- `tg:<chat_id>`

所以 Telegram attach 的第一阶段作用域，最自然的是：

- chat-scoped

也就是：

- 一个 Telegram chat 绑定一个 workspace

### 6.5 Slack

Slack 当前 memory session 语义是：

- `slack:<team_id>:<channel_id>`

注意这里和 thread 不是一回事。
当前 memory session 仍然是 channel scope。

所以 Slack attach 第一阶段最稳妥的做法是：

- channel-scoped

也就是：

- 一个 Slack channel 绑定一个 workspace

如果以后要做 thread-scoped attach，再单独扩。
这次不要一开始就把 thread 语义揉进去。

## 7) 不同入口的 attach 方式

### 7.1 CLI `chat`

CLI `chat` 仍然应该支持：

- `--workspace <dir>`
- `--no-workspace`
- 环境变量默认值
- 无显式配置时，当前目录作为默认 workspace

这是 CLI 的 attach surface。
不是全局功能的全部。

### 7.2 CLI `run`

`run` 同样支持：

- `--workspace <dir>`
- `--no-workspace`
- 环境变量
- 默认当前目录

但它是一次性 run 参数，不需要 attach store。

### 7.3 Console Web

Console Web 需要提供显式 UI：

- Attach workspace 按钮
- Detach workspace 按钮
- 当前 topic 的 workspace 状态展示
- Chat View 右侧 workspace 文件树侧栏

这里的文件树不是“附加小功能”。
它是 attach 成功后的直接可见结果。

没有文件树，用户很难确认：

- 现在到底 attach 到哪个目录
- agent 当前看到的工作区根是什么

### 7.4 Telegram / Slack

Telegram 和 Slack 需要提供聊天命令：

- `/attach <dir>`
- `/detach`
- `/workspace`

最少要能做三件事：

- 绑定一个目录
- 解绑
- 查看当前绑定

## 8) Console Web 的特殊问题

Console Web 虽然是“Web”，但 attach 的目标目录是 runtime host 上的本地目录。

这点必须在文档里讲清楚。

不能把两种东西混在一起：

- 浏览器客户端机器上的本地磁盘
- Console runtime 所在服务器上的本地磁盘

### 8.1 如果 Console 跑在本机

本机场景下，按钮可以更直接：

- 目录选择
- 浏览
- 文件树预览

### 8.2 如果 Console 是远程部署

远程部署场景下，UI 不能假装用户在选浏览器本地目录。

更合理的做法是：

- 让用户输入或浏览 runtime host 上的目录
- 后端返回该目录的文件树

否则会产生错误心智模型。

## 9) Telegram / Slack 的特殊问题

Telegram 和 Slack 是远程聊天入口。

所以 `/attach /path/to/project` 里的路径一定是：

- runtime host 上的本地路径

不是：

- Telegram 用户手机上的路径
- Slack 用户电脑上的路径

这个边界必须说透。

### 9.1 这带来两个设计后果

第一，路径 attach 需要权限控制。

第二，attach 状态不能只放内存里。
否则 runtime 重启以后，当前 chat/channel 的 workspace 会丢。

### 9.2 第一阶段建议

Telegram / Slack attach 应该持久化。

而且应当持久化到一个统一 attachment store，而不是散落在各自 runtime 的私有内存里。

## 10) 持久化设计

### 10.1 不要把 attach 状态塞进 memory 或 TODO

workspace attach 不是：

- memory
- TODO
- contacts
- task result

所以不应该借用这些现有文件格式。

### 10.2 建议新增独立 attachment store

建议在 `file_state_dir` 下单独建立 workspace attachment 存储，例如：

```text
file_state_dir/
  workspaces/
    attachments.json
```

或者：

```text
file_state_dir/
  workspaces/
    attachments.jsonl
```

核心要求只有两个：

- 独立
- 可按稳定 `ScopeKey` 查找

### 10.3 为什么更倾向统一 store

Console 其实可以把 attach 状态放进 topic metadata。
Telegram / Slack 则必须另找地方。

但如果每个 runtime 各放一套，就会马上出现分叉：

- Console 一套格式
- Telegram 一套格式
- Slack 一套格式

这不是好路子。

更稳的设计是：

- 统一 attachment store
- 不同 runtime 只负责提供自己的 `ScopeKey`

### 10.4 `run` 可以例外

CLI `run` 是 one-shot。

它完全可以不写 attachment store，只把 workspace 当成当前 run request 的运行参数。

这不构成例外混乱，因为它本来就没有长生命周期 session。

## 11) 工具层必须支持 `workspace_dir`

这条不能退。

如果工具层不支持 `workspace_dir`，这个需求就只是 UI / prompt 贴皮。

### 11.1 `write_file`

必须支持：

- `workspace_dir/<path>` alias
- 相对路径默认写入 workspace

规则：

- 有 workspace 时，相对路径默认写到 `workspace_dir`
- 无 workspace 时，保持当前行为

### 11.2 `read_file`

必须支持：

- `workspace_dir/<path>` alias
- 有 workspace 时，相对路径按 workspace 解析

这里不需要顺手把 `read_file` 改成新沙箱。
这次目标只是默认工作区语义。

### 11.3 `bash` / `powershell`

必须支持：

- `workspace_dir` alias
- 默认 `cwd = workspace_dir`
- 相对 `cwd` 按 workspace 解析

### 11.4 `url_fetch`

继续保持当前语义：

- `download_path` 仍然写到 `file_cache_dir`

不要跟着 workspace 走。

### 11.5 morph 系统文件

继续保持当前语义：

- TODO 在 `file_state_dir`
- memory 在 `file_state_dir`
- guard / contacts / skills 在 `file_state_dir`

## 12) prompt 和 agent 上下文

attach 以后，模型需要知道三件事：

- 当前 attached workspace 是什么
- 默认项目文件输出到哪里
- cache 和 state 目录分别干什么

这件事不能只在 CLI 做。

应该在所有支持 attach 的 runtime 里统一注入：

- CLI `chat`
- CLI `run`
- Console
- Telegram
- Slack

## 13) Console UI 需求

Console Chat View 建议增加一块右侧 workspace pane。

它至少要展示：

- 当前 attached workspace 路径
- 根目录文件树
- 当前选中文件
- 刷新
- detach

这块 UI 的目标不是做完整 IDE。
只是让用户和 agent 对“当前工作区”有同一个可见事实。

### 13.1 左右栏职责

当前 Chat View 左侧已经是 topic 侧栏。

workspace pane 放右边更合理：

- 左边是 conversation/topic
- 中间是 chat
- 右边是 workspace

这三个维度刚好分开。

## 14) CLI 仍然是第一批入口，但不是全部

CLI 相关设计保留，但应该降级为“其中一个接入面”。

### 14.1 CLI 默认值

对 `chat` / `run`：

- 默认当前目录作为 workspace
- `--no-workspace` 可关闭
- `--workspace <dir>` 可覆盖
- 环境变量可提供默认值

### 14.2 CLI inspect 输出

有 workspace 时，调试输出更自然地放到：

- `workspace_dir/dump/`

无 workspace 时，再保持当前相对路径行为。

## 15) Telegram / Slack 命令面设计

建议最小命令集：

- `/attach <dir>`
- `/detach`
- `/workspace`

### 15.1 attach 反馈

命令返回时应该至少包含：

- 是否 attach 成功
- 当前绑定目录
- 当前作用域 key

例如：

- Telegram: `tg:<chat_id>`
- Slack: `slack:<team_id>:<channel_id>`

### 15.2 detach 反馈

应明确告诉用户：

- 当前 session 已解绑
- 之后默认不再写入 workspace

## 16) runtime wiring 建议

### 16.1 不要继续把目录语义绑死在全局 registry

当前静态工具注册时就把目录根写进去了。
这对全局 workspace attach 不够用。

需要做的不是继续改全局 `viper`。
需要做的是：

- runtime 在处理当前 session/task 前，先解析当前 attached workspace
- 再用当前 runtime roots 构建当前 run 的工具 registry

### 16.2 建议显式引入 path roots 结构

不要再用位置数组猜目录意义。

建议改成显式结构，例如：

```go
type PathRoots struct {
    WorkspaceDir string
    FileCacheDir string
    FileStateDir string
}
```

这对所有入口都更清楚：

- CLI
- Console
- Telegram
- Slack

## 17) 测试建议

### 17.1 通用 attachment store

- 能按 `ScopeKey` 写入和读取
- 更新同一 scope 不会产生脏重复
- detach 后能正确删除或清空

### 17.2 CLI

- `chat` / `run` 默认当前目录
- `--workspace`
- `--no-workspace`
- 环境变量

### 17.3 Console

- topic attach 后刷新页面仍能恢复
- Chat View 右侧文件树正确展示
- topic 切换时 workspace 正确切换

### 17.4 Telegram

- `/attach`
- `/workspace`
- `/detach`
- runtime 重启后 attach 状态仍在

### 17.5 Slack

- `/attach`
- `/workspace`
- `/detach`
- runtime 重启后 attach 状态仍在

### 17.6 工具层

- `workspace_dir` alias
- 默认相对路径写入 workspace
- shell 默认 `cwd`
- `url_fetch` 继续走 cache
- morph 系统文件继续走 state

## 18) 还没定死的点

这次有几个点要明确标成 open question。

### 18.1 Console attach store 是共用还是 topic metadata 内嵌

我更倾向统一 attachment store。
因为这样 Telegram / Slack / Console 同一个模型。

### 18.2 Slack 要不要 thread-scoped attach

当前系统已有 thread 语义，但 memory session 仍偏 channel-scoped。

第一阶段建议先不做 thread-scoped attach。

### 18.3 Console 远程部署时如何选目录

这是 UI/产品问题，不是底层模型问题。

但文档里必须明确：

- 选的是 runtime host 上的目录
- 不是浏览器客户端自己的磁盘

## 19) 最终结论

这次需求的正确描述应该是：

> 为系统增加一个全局的 workspace attachment 能力，使不同 runtime 都能把本地目录附加到当前 session 或 conversation，并让 agent、工具层、UI 与持久化都围绕这个 workspace 形成一致语义。

这件事不是 CLI 专属。

第一阶段至少应覆盖：

- CLI `chat`
- CLI `run`
- Console Web
- Telegram
- Slack

同时坚持三个边界：

- 项目文件默认写到 `workspace_dir`
- 临时文件继续写到 `file_cache_dir`
- morph 系统文件继续写到 `file_state_dir`
