---
date: 2026-04-20
title: Workspace Dir for CLI Sessions
status: draft
---

# Workspace Dir for CLI Sessions

## 1) 目标

这次需求引入一个新的运行时概念：`workspace_dir`。

它表示当前 `chat` / `run` session attach 的本地目录。
这个目录进入当前上下文，成为 agent 的默认工作区。

这次只解决三件事：

- 让 agent 明确知道“当前在处理哪个本地目录”
- 让默认输出文件落到这个目录
- 保持临时文件和 morph 系统文件继续走各自原来的目录

这次不做：

- 不做 sandbox
- 不做 `chroot`
- 不把进程全局 `cwd` 改掉
- 不改 Telegram / Slack / LINE / Lark / Console 的目录语义

## 2) 现状问题

当前只有两类目录语义：

- `file_cache_dir`
- `file_state_dir`

但 CLI 的真实需求其实已经是三类：

- 项目工作目录
- 临时缓存目录
- morph 状态目录

现在的问题有几个。

### 2.1 `chat` 把工作目录和缓存目录混在了一起

`cmd/mistermorph/chatcmd/session.go` 里，`chat` 会把当前目录直接塞进 `file_cache_dir`。

这会带来两个问题：

- 名义上是 cache，实际上拿来当 workspace
- 临时文件和用户要的项目文件混在一个语义里

这已经和 `file_cache_dir` 的本义冲突了。

### 2.2 `run` 没有 workspace 语义

`run` 当前不会把当前目录传给静态工具层。
所以像 `write_file` 这类“默认写相对路径”的工具，会落到 `file_cache_dir`，而不是用户当前项目目录。

这和用户对 CLI 的直觉不一致。

### 2.3 仅改 prompt 不够

现在有些行为只是靠 prompt 暗示“当前目录是工作目录”。
这不够，因为底层工具自己并不知道 `workspace_dir`：

- `write_file` 只认识 `file_cache_dir` / `file_state_dir`
- `bash` / `powershell` 只认识 `file_cache_dir` / `file_state_dir`
- `read_file` 的相对路径仍然依赖进程当前目录

如果支持 `--workspace <dir>` 指向“不是当前 shell 所在目录”的路径，只改 prompt 会失效。

### 2.4 现有静态工具注册表已经把目录绑死了

`chat` / `run` 现在拿到的是已经构建好的静态工具注册表。
这些工具在注册时就已经把目录写死进去了。

这意味着：

- 不能继续靠“先构建全局 registry，再在 session 里临时改 `viper`”来做
- `workspace_dir` 应该在 `chat` / `run` 启动当前 session 时，作为运行时目录参数传进工具注册阶段

## 3) 设计判断

### 3.1 `workspace_dir` 是 session 级 attach，不是持久配置

从需求本身看，`workspace_dir` 更像一次会话 attach 的目录，而不是全局长期配置。

所以这次建议：

- 提供命令参数
- 提供环境变量默认值
- 不把它设计成需要长期写入 `config.yaml` 的常驻配置项

这样更符合需求本义，也更少副作用。

### 3.2 不要用 `os.Chdir`

不要把进程全局 `cwd` 改成 workspace。

原因很直接：

- `cwd` 是进程全局状态
- 会污染同进程内的其他逻辑
- 会让测试更脆
- 会让 subtask / inspector / 其他 runtime 的行为更难推断

正确做法是：

- 在 `chat` / `run` 内部先解析出一个“resolved workspace”
- 再把它显式传给工具、prompt、输出路径决策

### 3.3 `workspace_dir` 不是安全边界

这次需求定义的是“默认工作区”，不是“安全沙箱”。

也就是说：

- 它决定默认输出位置
- 它决定默认 shell 工作目录
- 它决定 prompt 里的当前项目上下文

但它不应该被描述成新的安全模型。
现有 guard、deny-path、allowlist 仍然是安全边界。

## 4) 目录语义

这次应当把三类目录语义彻底分开：

- `workspace_dir`
  - 当前 session attach 的项目目录
  - 用户没有指定路径时，默认输出到这里
- `file_cache_dir`
  - 临时文件、下载文件、转格式中间产物
- `file_state_dir`
  - morph 系统文件，例如 memory、TODO、HEARTBEAT、guard、contacts、skills

### 4.1 具体例子

应该落到 `workspace_dir` 的：

- 用户说“生成 README 草稿”
- 用户说“在项目里新建 AGENTS.md”
- 用户说“输出一份修复后的配置示例到当前项目目录”

应该继续落到 `file_cache_dir` 的：

- `url_fetch.download_path`
- 图片、音频、PDF 的下载结果
- 临时转格式的中间文件

应该继续落到 `file_state_dir` 的：

- `TODO.md`
- `TODO.DONE.md`
- memory
- `HEARTBEAT.md`
- guard approvals / audit
- contacts / skills

## 5) CLI 交互约定

### 5.1 适用命令

这次只影响：

- `mistermorph chat`
- `mistermorph run`

### 5.2 新参数

建议新增：

- `--workspace <dir>`
- `--no-workspace`

### 5.3 环境变量

建议新增：

- `MISTER_MORPH_WORKSPACE_DIR`

### 5.4 优先级

建议解析顺序如下：

1. `--workspace <dir>`
2. `--no-workspace`
3. `MISTER_MORPH_WORKSPACE_DIR`
4. `chat` / `run` 的默认值：当前目录

这里有两个补充约定：

- 如果同时传了 `--workspace` 和 `--no-workspace`，应直接报错
- `--no-workspace` 的作用是“不要 attach workspace”；不是把 CLI 变成 sandbox

### 5.5 路径解析

建议统一做这几步：

- `TrimSpace`
- 展开 `~`
- `filepath.Clean`
- 转绝对路径
- 校验目标存在且是目录

如果校验失败，命令直接返回错误，不进入 agent 主循环。

## 6) 工具层行为定义

这部分是这次需求的核心。

如果工具层不认识 `workspace_dir`，这个需求就只是“提示词换皮”，不会真正落地。

### 6.1 不要继续用位置编码的 `BaseDirs []string`

现在多个工具把目录根写成位置数组，语义靠下标约定：

- 第一个是 cache
- 第二个是 state

这套结构加第三个目录会变得很脆。

最小但清晰的改法是改成显式结构，例如：

```go
type PathRoots struct {
    WorkspaceDir string
    FileCacheDir string
    FileStateDir string
}
```

原因很简单：

- `workspace_dir` 不是“再加一个可选 base dir”这么简单
- 它既是新别名，又是默认输出目录，又是默认 shell `cwd`
- 继续靠 `bases[0]` / `bases[1]` / `bases[2]` 会让逻辑更绕

### 6.2 `write_file`

`write_file` 建议改成下面的语义：

- 新增 alias：`workspace_dir/<path>`
- 相对路径默认写入：
  - 有 workspace 时：`workspace_dir`
  - 无 workspace 时：保持当前行为，默认写入 `file_cache_dir`
- 绝对路径只允许落在这三个根目录之一：
  - `workspace_dir`
  - `file_cache_dir`
  - `file_state_dir`

文档和 schema 也应同步更新。

### 6.3 `read_file`

`read_file` 建议改成下面的语义：

- 新增 alias：`workspace_dir/<path>`
- 相对路径解析：
  - 有 workspace 时：相对 `workspace_dir`
  - 无 workspace 时：保持现状
- 绝对路径行为保持现状，不把这次需求扩成新的读文件沙箱

这里要强调一点：

- 这次需求主要解决“默认工作区”
- 不建议顺手把 `read_file` 改成“只能读 workspace”

那会把需求扩大成访问控制改造，不符合这次范围。

### 6.4 `bash` / `powershell`

这两个工具需要支持三个变化：

- 新增 alias：`workspace_dir`
- 当未显式给 `cwd` 时：
  - 有 workspace：默认 `cwd = workspace_dir`
  - 无 workspace：保持现状
- 如果传入的 `cwd` 是相对路径：
  - 有 workspace：相对 `workspace_dir` 解析
  - 无 workspace：保持现状

同时，命令字符串里的 alias 展开也要支持：

- `workspace_dir`
- `file_cache_dir`
- `file_state_dir`

### 6.5 `url_fetch`

`url_fetch.download_path` 继续保持原语义：

- 下载结果仍然写到 `file_cache_dir`

这条不要跟着 workspace 改。
因为它本来就是“下载 / 中间文件”语义。

### 6.6 `todo_update`、memory、guard、contacts、skills

这些都不应该迁到 workspace。

它们继续使用：

- `file_state_dir`

这也是这次需求里“morph 系统文件依然放到 `file_state_dir`”的直接体现。

## 7) `chat` 命令需要改什么

### 7.1 去掉“把 workspace 假装成 cache”这条旧逻辑

`chat` 当前最不合理的地方，是直接把当前目录塞进 `file_cache_dir`。

这条逻辑应该删掉。

原因：

- 它让 cache 失去 cache 的意义
- 它会把临时文件和项目文件混在一起
- 它让后续 `workspace_dir` 的语义始终不干净

### 7.2 session 内要显式保存 resolved workspace

建议把当前 `chatSession` 里的相关字段从“伪 cache”改成真实 workspace 语义。

例如：

- `chatFileCacheDir` 这类命名应改掉
- 改成明确的 `workspaceDir`

### 7.3 prompt 需要注入 workspace 上下文

`chat` 当前已经有一段“当前工作目录”的提示词。

这段应该保留，但要改成真正的 workspace 语义：

- 它描述的是 attached workspace，不是 cache
- 它告诉 agent：默认项目文件输出到 `workspace_dir`
- 只有 morph 系统文件才用 `file_state_dir`

### 7.4 `/init` 和 `/update`

这两个命令当前会把 `AGENTS.md` 写到 `chatFileCacheDir`。

改完后应明确写到：

- `workspace_dir/AGENTS.md`

### 7.5 chat 记忆的 subject 应跟随 workspace

`chat` 现在的 project memory subject 是根据那个“伪 cache dir”算出来的。

这次应该改成：

- subject 由 resolved `workspace_dir` 决定

memory 文件本身仍然继续写到 `file_state_dir`。

### 7.6 头部显示

chat 启动横幅当前打印的是 `file_cache_dir=...`。

这在新语义下会误导人。

建议改成：

- 有 workspace 时打印 `workspace_dir=...`
- 不要再把项目目录显示成 `file_cache_dir`

## 8) `run` 命令需要改什么

### 8.1 `run` 也要有相同的 workspace 解析逻辑

`run` 不能再继续“没有 workspace 语义”。

因为用户已经明确要求：

- `chat` / `run` 默认都把当前目录作为 workspace 传入

### 8.2 `run` 也要注入 workspace prompt

仅让工具认识 workspace 还不够。
`run` 还需要把这个上下文告诉模型。

否则模型仍然可能把输出理解成“写到 cache”或“写到任意地方”。

建议给 `run` 也加一段短的 workspace prompt block，内容只说三件事：

- 当前 attach 的 workspace 是什么
- 默认项目文件输出到 `workspace_dir`
- 临时文件去 `file_cache_dir`，系统文件去 `file_state_dir`

### 8.3 inspect 输出目录建议跟随 workspace

`run --inspect-prompt` 和 `run --inspect-request` 会生成本地文件。

这类文件不是 morph 系统文件，也不是下载缓存。
更像当前任务的调试产物。

所以建议：

- 有 workspace 时：输出到 `workspace_dir/dump/`
- 无 workspace 时：保持现状，仍然是 `./dump`

这样更符合“如果有文件需要输出，就输出到该目录”的要求。

## 9) registry 和 wiring 方案

### 9.1 不能继续只复用 `RegistryFromViper()`

当前 `chat` / `run` 依赖的是已经构建好的 registry。

但那个 registry 的静态工具在创建时就已经绑定了目录。

这意味着：

- 如果直接 clone 那个 registry，再改 prompt，是不够的
- 如果继续全局改 `viper`，也很脆

### 9.2 建议做法

建议把 `chat` / `run` 的工具构建改成两段：

1. 读取共享配置
2. 用当前 session 的目录根重新构建静态工具
3. 再把 runtime tools 叠上去
4. 最后再追加 MCP tools

这里真正需要“每个 session 单独覆盖”的，其实只是目录根：

- `workspace_dir`
- `file_cache_dir`
- `file_state_dir`

其他配置仍然可以复用现有加载逻辑。

### 9.3 结果目标

目标不是重写 registry 架构。

目标只是让 `chat` / `run` 具备“同一套工具能力，但目录根是当前 session 解析结果”的能力。

## 10) 兼容性与范围控制

### 10.1 这次不改其他 runtime

不在这次范围内的入口：

- Telegram
- Slack
- LINE
- Lark
- Console

这些入口没有“attach 本地目录”的需求，不要顺手改。

### 10.2 保持现有 alias 可用

现有 alias 继续保留：

- `file_cache_dir`
- `file_state_dir`

这次只是新增：

- `workspace_dir`

### 10.3 `--no-workspace` 不是彻底隔离模式

需要明确写进实现说明：

- `--no-workspace` 只是“不 attach workspace”
- 它不是新的安全模式
- 不应承诺任何类似 sandbox 的效果

## 11) 测试建议

至少补这几类测试。

### 11.1 CLI 解析

- `--workspace` 生效
- `MISTER_MORPH_WORKSPACE_DIR` 生效
- `--no-workspace` 能压过环境变量
- `--workspace` 和 `--no-workspace` 同时出现时报错
- `chat` / `run` 在无显式参数时默认取当前目录

### 11.2 `write_file`

- 相对路径默认写入 workspace
- `workspace_dir/<path>` 能写
- `file_cache_dir/<path>` 仍然写 cache
- `file_state_dir/<path>` 仍然写 state
- 绝对路径越界时被拒绝

### 11.3 `read_file`

- `workspace_dir/<path>` 能读
- 有 workspace 时相对路径按 workspace 解析
- 无 workspace 时保持现状

### 11.4 `bash` / `powershell`

- 默认 `cwd` 跟随 workspace
- `cwd: "subdir"` 在有 workspace 时按 workspace 解析
- 命令里的 `workspace_dir/...` alias 能展开
- `file_cache_dir` / `file_state_dir` 旧 alias 不回归

### 11.5 特例不回归

- `url_fetch.download_path` 仍然落到 `file_cache_dir`
- TODO / memory 仍然落到 `file_state_dir`
- `chat /init` 生成的 `AGENTS.md` 落到 `workspace_dir`
- `run --inspect-*` 在有 workspace 时落到 `workspace_dir/dump/`

## 12) 建议实现顺序

为了降低回归面，建议按这个顺序做：

1. 先加 `workspace_dir` 解析逻辑与 CLI 参数
2. 再改工具层的路径根模型和 alias 解析
3. 再改 `chat` / `run` 的 registry 构建
4. 最后补 prompt、header、`/init`、inspect 输出这些用户可见行为

这样每一步都能单独验证，不容易把问题揉在一起。

## 13) 最终结论

这次需求本质上不是“给 CLI 多加一个路径参数”。

它真正要求的是：

- 把 `workspace_dir` 从 `file_cache_dir` 里拆出来
- 让 `chat` / `run` 都拥有一致的工作区语义
- 同时继续保持 `file_cache_dir` 和 `file_state_dir` 的职责边界

如果只改 prompt 或只改 `chat`，最后都会留下半套语义。

最小可用且干净的方案是：

- 为 `chat` / `run` 引入 session 级 `workspace_dir`
- 工具层显式支持 `workspace_dir`
- 默认项目输出走 workspace
- 临时文件继续走 cache
- morph 系统文件继续走 state
