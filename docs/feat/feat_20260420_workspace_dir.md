---
date: 2026-04-20
title: Workspace Attachment Across Sessions
status: draft
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
- 第一期不做任何 console 相关前后端
- 不规定 Console 的具体 UI 形态
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

- Console: `console:<topic_id>`，留给第二期
- Telegram: `tg:<chat_id>`
- Slack: `slack:<team_id>:<channel_id>`
- LINE: `line:<chat_id>` 或 `line:<group_id>`
- Lark: `lark:<chat_id>`

这意味着：

- 第二期做 console 时，不同 topic 可以绑定不同 workspace
- 第二期做 console 时，store 只认 `console:<topic_id>`
- 不使用 bus envelope 的 `session_id` 做 attachment key

## 5) 生命周期与持久化

第一阶段只需要下面这组规则：

- CLI `chat`：进程内临时状态，不进 attachment store
- CLI `run`：一次性运行参数，不进 attachment store
- Telegram / Slack / LINE / Lark：按 canonical conversation key 落 attachment store
- Console：第二期接入时按 canonical conversation key 落 attachment store

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

Console 相关前后端放到第二期，但仍沿用同一套协议。

行为规则只有三条：

- `/workspace`：查看当前绑定状态
- `/workspace attach <dir>`：绑定目录；如果已有绑定，直接替换旧值，并明确回显替换结果
- `/workspace detach`：解绑

失败规则也固定下来：

- 路径不存在：失败
- 路径不可读：失败
- 路径不在允许范围内：失败
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

这一期的范围只含 CLI 和消息通道，不含任何 console 相关前后端。

## 10) Checklist

### 10.1 路径模型

- [x] 引入显式 `PathRoots`
- [x] 在运行时构造 `PathRoots{WorkspaceDir, FileCacheDir, FileStateDir}`
- [x] 去掉当前 CLI `chat` 对 `file_cache_dir = cwd` 的错误耦合

### 10.2 attachment store

- [x] 新增 workspace attachment store
- [x] store 主键固定为 canonical conversation key
- [x] store value 最小只保存 `WorkspaceDir`
- [x] `attach` 时覆盖旧值，不保留双绑定
- [x] `detach` 时删除当前绑定

### 10.3 scope key 接线

- [x] Telegram 统一使用 `tg:<chat_id>`
- [x] Slack 统一使用 `slack:<team_id>:<channel_id>`
- [x] LINE 统一使用 `line:<chat_id>` 或 `line:<group_id>`
- [x] Lark 统一使用 `lark:<chat_id>`

### 10.4 命令解析

- [x] 复用现有公用命令解析设施
- [x] 支持 `/workspace`
- [x] 支持 `/workspace attach <dir>`
- [x] 支持 `/workspace detach`
- [x] 非法语法返回稳定错误

### 10.5 CLI

- [x] `chat` 支持 `--workspace <dir>`
- [x] `chat` 支持 `--no-workspace`
- [x] `chat` 默认当前目录作为 `workspace_dir`
- [x] `chat` 进程内保存当前 workspace
- [x] `run` 支持 `--workspace <dir>`
- [x] `run` 支持 `--no-workspace`
- [x] `run` 默认当前目录作为 `workspace_dir`
- [x] `run` 不写 attachment store

### 10.6 Console（二期）

- [ ] Console 前后端接入 `/workspace` 文本协议
- [ ] Console 统一使用 `console:<topic_id>`
- [ ] 一个 topic 可绑定一个 workspace
- [ ] 不同 topic 可绑定不同 workspace
- [ ] 切 topic 时切换当前 workspace
- [ ] 刷新后能从 attachment store 恢复绑定

### 10.7 Channel runtimes

- [x] Telegram 接入 `/workspace` 文本协议
- [x] Slack 接入 `/workspace` 文本协议
- [x] LINE 接入 `/workspace` 文本协议
- [x] Lark 接入 `/workspace` 文本协议
- [x] 这些 runtime 重启后能从 attachment store 恢复绑定

### 10.8 工具层

- [x] `write_file` 支持 `workspace_dir/<path>` alias
- [x] `read_file` 支持 `workspace_dir/<path>` alias
- [x] 有 workspace 时，相对路径默认按 workspace 解析
- [x] `bash` 默认 `cwd = workspace_dir`
- [x] `powershell` 默认 `cwd = workspace_dir`
- [x] `url_fetch` 继续写 `file_cache_dir`
- [x] TODO / memory / guard / contacts / skills 继续写 `file_state_dir`
- [x] guard 与 deny-path 校验覆盖三类目录

### 10.9 返回语义

- [x] `/workspace` 返回当前绑定状态
- [x] 首次 attach 明确返回绑定成功
- [x] 替换 attach 明确返回旧目录到新目录的切换结果
- [x] `detach` 明确返回解绑成功
- [x] 路径不存在时返回失败
- [x] 路径不可读时返回失败
- [x] 路径不在允许范围内时返回失败
- [x] 不自动创建目录

### 10.10 最小测试

- [x] attachment store 按 canonical key 读写正常
- [x] 同一 key 重复 attach 只保留最新值
- [x] detach 后绑定消失
- [x] `/workspace` 三种语法解析正确
- [ ] CLI `chat` 不再把 cwd 塞进 `file_cache_dir`
- [ ] Telegram / Slack / LINE / Lark 重启后能恢复绑定
- [x] `write_file` / `read_file` / shell 工具按 workspace 生效
