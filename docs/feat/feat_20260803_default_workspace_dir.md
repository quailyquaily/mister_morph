---
date: 2026-08-03
title: Default Workspace Directory
status: implemented
---

# 默认 Workspace 目录

## 1. 问题

MisterMorph 目前有两个全局文件根配置：

- `file_state_dir`：Morph 自己恢复运行所需的持久状态。
- `file_cache_dir`：可以清理和重新生成的临时文件。

`workspace_dir` 只来自一次 CLI 运行或某个 conversation/topic 的 attachment。Console 和各 channel 在没有 attachment 时，得到的 workspace 是空。

空 workspace 会产生两种不同结果：

- `read_file`、`write_file` 的相对路径使用 cache。
- shell 和 coder 没有显式 `cwd` 时会使用进程工作目录。

systemd unit 当前把进程工作目录放在持久状态区域，所以后一种情况可能把用户项目文件写进系统状态目录。

本需求解决的是：

> 允许操作者配置一个全局默认 workspace，使没有 attachment 的运行也有明确的项目目录。

本文扩展 [Workspace Attachment Across Sessions](./feat_20260420_workspace_dir.md)，不改变现有 attachment store 和 scope key 规则。

shell 和 coder 在“最终 workspace 仍为空”时不应继承进程 `cwd`，这是另一个独立问题，不与本配置功能一起修改。需要保证部署中的项目文件与 state 分离时，必须实际配置 `workspace_dir`。

## 2. 文件边界

目录按文件的所有者和生命周期划分，不按文件名里是否出现 “task” 划分。

| 根目录 | 所有者与生命周期 | 典型内容 |
|---|---|---|
| `workspace_dir` | 用户拥有，需要作为项目内容长期保留 | 源代码、输入文件、报告、项目文档、任务产物 |
| `file_cache_dir` | Morph 或工具产生，可以清理和重建 | 下载、转换文件、临时媒体、中间产物 |
| `file_state_dir` | Morph 拥有，恢复运行需要 | memory、task record、journal、checkpoint、skills、认证和 guard 状态 |

必须保持以下边界：

- 用户交付物写入 workspace。
- Morph 的 task journal、projection 和 checkpoint 仍写入 state。
- 下载和可重建文件继续写入 cache。
- 默认 workspace 不改变任何已有 state 路径。

这套规则是路径路由，不是 sandbox。目录是否位于不同磁盘、mount、volume 或备份策略，由部署配置决定。

所有没有 attachment 的 scope 共享同一个默认 workspace。配置值表示 workspace 本身，不是自动追加 topic ID 的父目录。需要按项目或 topic 隔离时，使用 attachment。

## 3. 目标

本功能必须做到：

1. 增加全局 `workspace_dir` 配置和环境变量。
2. attachment 覆盖全局默认值。
3. 每个 job 在接收时得到一个确定的 workspace，后续组件使用同一个路径。
4. 默认值不写入 attachment store。
5. Console API 和 UI 能区分 attachment、default 和 none。
6. CLI、Console、Telegram、Slack、LINE、Lark 与 integration 使用同一配置语义。
7. 现有 `file_state_dir` 与 `file_cache_dir` 职责不变。

## 4. 非目标

本功能不做下面这些事：

- 不修改无 workspace 时 shell、PowerShell 或 coder 的 `cwd` 回退行为。
- 不增加 workspace sandbox。
- 不支持一个 scope 同时绑定多个 workspace。
- 不自动为 topic 创建 workspace 子目录。
- 不自动创建配置目录。
- 不把 task journal、checkpoint、memory 等系统状态移到 workspace。
- 不迁移已经写入 state 或 cache 的旧文件。
- 不改变 attachment store 的持久化格式。
- 不增加 per-scope 的“禁用全局默认值”记录。
- 不要求 `workspace_dir`、`file_state_dir` 和 `file_cache_dir` 位于不同文件系统。

## 5. 配置协议

### 5.1 配置字段

新增顶层配置：

```yaml
# Default project directory used when the current scope has no attachment.
# The directory must already exist and be readable.
# Empty means no global default workspace.
workspace_dir: ""
```

对应环境变量：

```text
MISTER_MORPH_WORKSPACE_DIR
```

默认值为空：

```go
v.SetDefault("workspace_dir", "")
```

使用顶层 `workspace_dir`，不增加 `workspace.default_dir` 或 `default_workspace_dir`。它与现有工具 alias 和 `PathRoots.WorkspaceDir` 使用同一个名字，也与 `file_cache_dir`、`file_state_dir` 保持同一层级。

配置中的精确定义是：

> 当前运行没有显式 workspace 或 scope attachment 时使用的默认值。

### 5.2 路径处理

复用现有 `workspace.ValidateDir` 语义：

1. 去掉首尾空白并展开 `~`。
2. 相对路径按 runtime 启动目录转成绝对路径。
3. 路径必须已经存在。
4. 路径必须是目录。
5. 运行用户必须能够读取目录。
6. 不自动创建目录。

长驻服务应配置绝对路径，避免路径含义受到 systemd `WorkingDirectory` 或容器启动目录影响。

不要求目录可写。只读 workspace 可以用于分析任务；需要生成文件时，由部署配置授予写权限。

软件不禁止 workspace 与 state/cache 相同或互相包含。若操作者把它们指向同一物理位置，就没有实现持久化分离。这是配置结果，不是运行时可以推断的错误。

### 5.3 加载和热更新

需要覆盖两条配置加载路径：

- 普通 CLI 和 channel 使用的 root Viper。
- Console generation 使用的独立 Viper snapshot。

非空且无效的配置使对应 runtime generation 构建失败。Console 热更新失败时继续使用当前可用 generation。

配置只在 generation 构建时校验。目录之后被删除或失去权限时，文件操作返回原始错误，不静默切换到 attachment、cache、state 或进程 `cwd`。

## 6. Workspace 解析

### 6.1 解析结果

运行时需要知道最终路径和来源。最小模型是：

```go
type Source string

const (
    SourceNone       Source = "none"
    SourceDefault    Source = "default"
    SourceAttachment Source = "attachment"
)

type Resolution struct {
    WorkspaceDir string
    Source       Source
}
```

这些 Go 名字是实现建议。对外协议只固定第 9 节的 JSON 字段和值。

### 6.2 持久 scope 的优先级

Console 和四个 channel 使用：

```text
本次请求显式 workspace
    > scope attachment
    > workspace_dir 配置
    > none
```

本次请求显式提交 workspace 时：

1. 使用现有规则校验。
2. 保存或替换该 scope 的 attachment。
3. 本次 job 使用新 attachment。

attachment 已在 attach 时校验。目录之后失效时，不跳过 attachment 改用默认值，否则任务可能写进另一个项目。

### 6.3 Store 只保存 attachment

`workspace_attachments.json` 继续只保存用户显式绑定的路径：

```json
{
  "version": 1,
  "attachments": {
    "console:topic-a": {
      "workspace_dir": "/projects/topic-a"
    }
  }
}
```

resolver 不把配置默认值写入 store。这样修改配置后，所有未绑定 scope 立即使用新默认值，也不会把旧默认值复制到每个 topic。

### 6.4 每个 job 解析一次

任务接收时完成解析：

```text
config snapshot + request + attachment
                  |
                  v
           workspace resolution
                  |
                  v
                 job
                  |
      context / prompt / files / images
```

job 保存解析后的 `WorkspaceDir`。任务执行中不再次查询 attachment store。

- 运行中的 job 不因配置热更新或 attachment 修改而切换目录。
- 新 job 使用当前 generation 和 attachment。
- 进程重启后恢复的 job 沿用现有恢复方式，按恢复时的配置和 attachment 重新解析。

## 7. 运行时传递

### 7.1 解析一次，显式传递

默认 workspace 只在 job 或一次 CLI run 创建时解析。解析结果可以传给 job context 和现有 per-run 配置，但这些消费者不能各自重新解析配置或 attachment。

工具继续通过现有 context 取得 workspace：

```go
ctx = pathroots.WithWorkspaceDir(ctx, resolution.WorkspaceDir)
```

Console 和 channel 共享的静态 tool registry base 继续保持空 workspace：

```go
pathroots.New("", paths.CacheDir, paths.StateDir)
```

不要把配置默认值写进这份共享 base。否则持久 scope 会有两个 workspace 解析点，并需要额外的 context 三态来支持 attachment 和 `--no-workspace`。

Integration 没有 attachment 或 `--no-workspace`。`Runtime.NewRegistry()` 每次返回独立 registry，因此它可以直接使用 runtime snapshot 中已校验的默认 workspace，保证 `NewRunEngineWithRegistry` 的公开用法与 `RunTask` 一致。这不会修改共享 base。

因此，本功能不修改：

- `pathroots.WorkspaceDirFromContext` 的空值语义。
- `pathroots.Resolve` 的覆盖规则。
- 无 workspace 时各工具现有的回退行为。

### 7.2 CLI

CLI 初始 workspace 的优先级是：

```text
--no-workspace
    > --workspace <dir>
    > workspace_dir 配置
    > 启动时的当前目录
```

`ResolveInitialWorkspace` 增加配置默认值输入即可，不需要修改 pathroots context。

`chat` 中：

- `/workspace attach <dir>` 设置 session override。
- `/workspace detach` 删除 session override；配置默认值存在时重新使用默认值。
- 使用 `--no-workspace` 启动时，该 session 保持显式禁用；attach 后再 detach 仍回到 none。
- 没有配置时，保持当前目录作为初始 workspace；用户 detach 后仍按现有行为变成 none。

### 7.3 Console、Channel 与 Integration

下面这些入口都必须在创建 job 前调用统一 resolver：

| 入口 | 需要使用解析结果的位置 |
|---|---|
| Console | 新任务、恢复任务、prompt、file references、图片输入 |
| Telegram | 入队、任务运行、命令、入站媒体和图片历史 |
| Slack | 入队、任务运行、状态事件、命令和图片历史 |
| LINE | 入队、任务运行、命令和图片历史 |
| Lark | 入队、任务运行、命令和图片历史 |
| Awareness / cron | 创建目标 scope 的后台 job |
| Integration | run context、per-run tool roots 和 workspace prompt |

各 channel job 已有 `WorkspaceDir` 字段，继续保存最终路径。不要增加第二套 default workspace 字段。

### 7.4 工具和系统路径

有效 workspace 非空时，现有 context 机制已经让下面组件使用它：

- `read_file`
- `write_file`
- `bash`
- `powershell`
- `coder`
- workspace alias
- 图片输入和图片历史
- context compaction

只要最终 workspace 非空，就注入现有 `workspace.PromptBlock`。默认值和 attachment 对模型及工具具有相同含义，prompt 不需要说明来源。

这些路径不改变：

- `url_fetch` 和可重建下载继续使用 cache。
- memory、task record、journal、checkpoint、skills、guard、认证、统计和日志继续使用 state。

## 8. Workspace 命令

`/workspace` 必须说明当前来源：

```text
workspace: /projects/topic-a (attachment)
workspace: /srv/morph-workspace (default)
workspace: (none)
```

`/workspace attach <dir>`：

- 校验并保存 attachment。
- attachment 覆盖默认值。

`/workspace detach`：

- 只删除 attachment。
- 有默认配置时，删除后重新使用 default。
- 没有默认配置时，删除后变为 none。
- 当前只有 default 时，不修改 store，并明确说明正在使用默认值。

第一版不持久化 per-scope disable 标记，所以 Console 和 channel topic 不能通过 detach 屏蔽全局默认值。CLI 一次运行可以使用 `--no-workspace`。

## 9. Console API 与 UI

### 9.1 API

当前 API 只返回路径。默认值加入后，UI 必须知道它是不是 attachment，因此响应增加 `source`：

```json
{
  "topic_id": "topic-a",
  "workspace_dir": "/srv/morph-workspace",
  "source": "default"
}
```

`source` 只允许：

- `attachment`
- `default`
- `none`

行为：

- GET 返回当前有效 workspace 和 source。
- PUT 校验并保存 attachment，返回 `attachment`。
- DELETE 删除 attachment 后重新解析，返回 `default` 或 `none`，不再硬编码空路径。
- topic metadata 返回相同的 workspace 数据。

内部 callback 如何传递 `Resolution` 是实现细节，不属于外部协议。

### 9.2 UI

Console 必须：

- 显示当前有效 workspace。
- 区分 default 与 attachment。
- 只在 source 为 attachment 时提供 detach。
- 允许从 default 切换到 attachment。
- detach 后立即显示重新出现的 default。
- 不把 default 写入 attachment store。
- 使用 default 提交任务时，不发送显式 `SubmitTaskRequest.workspace_dir`。

文件树、上传、下载、preview、artifact preview 和 file reference 校验使用有效 workspace。

新 topic 尚无 `topic_id` 时，上传根优先级是：

```text
本次请求显式 workspace > workspace_dir 配置 > file_cache_dir
```

已有 topic 时，在配置默认值之前增加 topic attachment。使用 default 上传文件不能创建 attachment。

目录浏览器 `browse` 继续只负责选择目录，不把浏览起点当作 topic workspace。

## 10. 兼容性与部署

### 10.1 兼容性

`workspace_dir` 默认空，因此：

- 现有 attachment 不需要迁移。
- attachment store 格式不变。
- 没有新配置的 Console 和 channel 保持现有行为。
- 没有新配置的 CLI 继续使用当前目录。
- state 和 cache 中已有文件位置不变。
- 本功能不移动旧文件。

配置非空表示操作者主动选择新默认值：

- 没有 attachment 的 scope 使用该目录。
- CLI 没有 workspace flag 时优先使用该目录。
- 现有 attachment 继续覆盖配置。

### 10.2 部署

长驻服务需要同时配置路径和文件权限。例如：

```ini
Environment=MISTER_MORPH_WORKSPACE_DIR=/srv/morph-workspace
ReadWritePaths=/srv/morph-workspace
```

目录由操作者提前创建。部署文档必须说明：

- workspace、state 和 cache 应使用不同目录。
- systemd 或容器必须允许运行用户访问 workspace。
- `ProtectSystem`、`ProtectHome` 和 bind mount 可能影响访问。
- `WorkingDirectory` 不代表 workspace。
- 没有配置 workspace 时，shell/coder 继承进程 `cwd` 的现有问题仍然存在，应单独修复。

## 11. 实现范围

最小实现包括：

1. 在 `internal/configdefaults` 增加空的 `workspace_dir` 默认值。
2. 在普通 root 配置和 Console generation 配置中展开、校验该字段。
3. 让 runtime snapshot 能读取配置默认值。
4. 在 `internal/workspace` 增加统一 resolver，不修改 attachment store 格式。
5. Console、四个 channel、CLI 和 integration 在创建 job/run 时解析并传入 workspace。
6. daemon workspace API 和 topic metadata 返回 source。
7. Console UI 保存并显示 source，默认值不作为显式 attachment 提交。
8. 更新配置示例、filesystem roots、CLI 与部署文档。

明确不需要修改：

- Console 和 channel 共享静态 tool registry base 的 workspace root。
- `pathroots` context 三态。
- shell、PowerShell、coder 的空 workspace 回退。
- task、journal、checkpoint、memory 等 state 路径。
- attachment store schema。
- 系统诊断和额外 workspace 日志。

## 12. 测试要求

正式实现前补测试。

### 12.1 配置

- 默认值为空。
- YAML 与 `MISTER_MORPH_WORKSPACE_DIR` 生效。
- `~` 和相对路径沿用现有校验语义。
- 不存在、非目录、不可读路径失败。
- Console 热更新遇到无效值时保留当前 generation。

### 12.2 Resolver

- 显式请求覆盖 attachment。
- attachment 覆盖 default。
- 没有 attachment 时使用 default。
- 两者都没有时返回 none。
- default 不写入 attachment store。
- attachment 失效时不静默使用 default。

### 12.3 Runtime

- CLI flag 与配置优先级正确。
- `--no-workspace` 压制配置。
- Console 和四个 channel 的 job 得到默认值。
- attachment 覆盖默认值。
- job 接收后修改配置或 attachment，不改变该 job。
- integration 的 tools 与 prompt 使用同一 workspace。

### 12.4 API 与 UI

- GET 返回正确路径和 source。
- PUT 返回 attachment。
- DELETE 后返回 default 或 none。
- topic metadata 与 workspace API 一致。
- UI 不把 default 作为显式 workspace 提交。
- 新 topic 使用 default 上传文件时不创建 attachment。

### 12.5 状态边界

- task、journal、checkpoint、memory、skills、auth 和 stats 路径不变。
- attachment store 格式不变。
- 配置默认值不会生成 attachment 记录。

## 13. 验收标准

1. 配置 `workspace_dir` 后，没有 attachment 的 Console 和四个 channel 使用该目录。
2. CLI 优先级是 `--no-workspace > --workspace > 配置 > 当前目录`。
3. prompt、文件工具、shell、coder、文件 API 和图片处理得到同一个有效 workspace。
4. attachment 覆盖默认值；detach 后重新使用默认值。
5. Console UI 能区分 default、attachment 和 none。
6. 默认值不写入 attachment store。
7. Morph 系统状态继续写入 `file_state_dir` 的现有路径。
8. 配置为空时不需要迁移数据，也不改变现有无 workspace 行为。

## 14. 不采用的方案

### 14.1 把默认值复制到 attachment store

这会制造重复状态，并让配置修改无法作用于已有 topic。

### 14.2 同时把默认值放进共享静态 roots

job 已经解析并传递最终 workspace。再设置静态默认值会形成第二个解析点，并迫使 context 支持显式空值覆盖。

### 14.3 第一版增加 per-scope disable

这需要新的持久状态和 UI 语义。当前需求只需要 default 与 attachment 两层。

### 14.4 同时修改 shell/coder 的空 workspace 回退

这是合理的独立 bug 修复，但会改变未配置 workspace 的旧行为。它不应扩大本配置功能的回归范围。
