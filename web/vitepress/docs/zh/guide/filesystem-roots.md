---
title: 文件系统根目录
description: 解释 workspace_dir、file_cache_dir、file_state_dir 三个概念，以及 Mistermorph 如何使用它们。
---

# 文件系统根目录

Mistermorph 把文件系统上的目录分成三类：

- `workspace_dir`：当前会话或当前 topic 绑定的项目目录。
- `file_cache_dir`：可重建的缓存文件目录。
- `file_state_dir`：需要持久保存的运行状态目录。

这三个概念不要混用。项目树、临时文件、运行状态混在一起，模型和工具的行为都会变得不稳定。

## 三个根目录分别是什么

| 根目录 | 含义 | 常见内容 | 持久性 |
|---|---|---|---|
| `workspace_dir` | Agent 当前应该当作“正在处理的项目”的目录树。它是运行时上下文，不是全局配置字段。 | 源代码、文档、配置文件、项目笔记 | 通常由用户自己管理 |
| `file_cache_dir` | 运行过程中产生、但可以重建的文件 | 下载文件、临时转换结果、抓取产物、生成的媒体文件 | 可以清理 |
| `file_state_dir` | 重启后也应该保留的运行状态 | tasks、contacts、skills、guard 状态、workspace 附着信息 | 应持久化 |

默认情况下：

- `file_state_dir` 是 `~/.morph`
- `file_cache_dir` 是 `~/.cache/morph`

`workspace_dir` 不一样。它不是一个全局配置项，而是由当前运行时附着进来的项目目录。

## `workspace_dir` 是怎么来的

在 CLI chat 里：

```bash
mistermorph chat --workspace .
```

当前行为是：

- `mistermorph chat` 默认会把当前工作目录附着成 `workspace_dir`
- `mistermorph chat --workspace <dir>` 可以显式指定目录
- `mistermorph chat --no-workspace` 会在无 workspace 的情况下启动
- 进入 chat 后，可以用 `/workspace`、`/workspace attach <dir>`、`/workspace detach` 查看或切换附着目录

其他运行时也可以各自提供 `workspace_dir`。关键点只有一个：`workspace_dir` 表示当前对话绑定的项目目录，不是全局状态目录。

## 路径别名

下面三个别名可以显式指定根目录：

- `workspace_dir/...`
- `file_cache_dir/...`
- `file_state_dir/...`

在 prompt、脚本或工具参数里，想避免歧义时，直接写别名更稳妥。

## 工具如何解析路径

### `read_file`

- 有 `workspace_dir` 时，相对路径默认落到 `workspace_dir`
- 没有 `workspace_dir` 时，相对路径默认落到 `file_cache_dir`
- 也可以用别名强制指定根目录

### `write_file`

- 有 `workspace_dir` 时，相对路径默认落到 `workspace_dir`
- 没有 `workspace_dir` 时，相对路径默认落到 `file_cache_dir`
- 写入只能发生在 `workspace_dir`、`file_cache_dir`、`file_state_dir` 之内
- 也可以用别名强制指定根目录

### `bash` 和 `powershell`

- 命令文本里可以使用 `workspace_dir`、`file_cache_dir`、`file_state_dir` 别名
- 如果 shell 的 `cwd` 没显式传入，且当前有 `workspace_dir`，shell 默认会从那里启动

所以即使你没有直接调用 `read_file` 或 `write_file`，`workspace_dir` 仍然会影响工具行为。

## 实际使用规则

- 要让 Agent 当作项目本体处理的代码树，放进 `workspace_dir`
- 下载物、临时文件、可丢弃的生成结果，放进 `file_cache_dir`
- contacts、已安装 skills、任务状态和其他长期状态，放进 `file_state_dir`

## 相关页面

- [命令行参数](/zh/guide/cli-flags)
- [配置字段](/zh/guide/config-reference)
- [内置工具](/zh/guide/built-in-tools)
- [配置模式](/zh/guide/config-patterns)
