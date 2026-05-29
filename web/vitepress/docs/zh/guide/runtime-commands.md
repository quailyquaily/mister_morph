---
title: 命令
description: Chat、Console 和其他 Channels 支持的命令。
---

# 命令

命令是在交互式 chat、Console task 或通道 runtime 里发送的以 `/` slash 符号开头的命令。

> 在 Slack 中，由于 `/` 会触发 Slack 自己的命令，所以需要在 `/` 前面加一个空格。例如 ` /models`。
>
> Slack 群聊里，命令需要明确提到 bot。Telegram 群聊可以使用普通 bot command，例如 `/models@BotName`。

## 通用命令

这些命令在 CLI chat、Console Web、Telegram、Slack、LINE 和 Lark 中可用。

| 命令 | 作用 |
|---|---|
| `/help` | 列出当前可用的运行时命令。 |
| `/models` | 查看当前模型。 |
| `/think <task>` | 使用 `think` LLM route 运行该任务。 |
| `/skills` | 显示当前 skills。 |
| `/ctx` | 查看当前对话的上下文窗口占用。 |
| `/workspace` | 查看当前 workspace 目录。 |

其中，

`/ctx` 不调用 LLM。如果当前对话还没有记录过 agent 运行用量，会显示暂无上下文用量记录。

对于 `/workspace`，支持如下参数：

| 命令 | 作用 |
|---|---|
| `/workspace` | 无参数，查看当前 workspace 目录。 |
| `/workspace attach <dir>` | 绑定或替换 workspace 目录。 |
| `/workspace detach` | 解绑当前 workspace。 |

对于 `/models`，支持如下参数：

| 命令 | 作用 |
|---|---|
| `/models` | 查看当前模型。 |
| `/models list` | 列出已配置的模型 profile。 |
| `/models set <profile_name>` | 切换当前模型。 |
| `/models reset` | 重置为自动模型选择。 |

对于 `/think`，任务内容写在命令后面：

| 命令 | 作用 |
|---|---|
| `/think <task>` | 去掉命令前缀后，使用 `llm.routes.think`，并为本次任务临时应用 `reasoning_effort=xhigh`。 |

## CLI Chat 特有的命令

这些命令只在 `mistermorph chat` 中可用。

| 命令 | 作用 |
|---|---|
| `/exit` | 退出 chat session。 |
| `/quit` | 退出 chat session。 |
| `/reset` | 清空当前对话历史。 |
| `/memory` | 显示当前项目记忆。 |
| `/remember <content>` | 为当前项目新增一条长期记忆。 |
| `/init` | 为当前项目生成 `AGENTS.md`。 |
| `/update` | 重新生成 `AGENTS.md`，并覆盖已有文件。 |

## Telegram 特有的命令

这些命令只在 Telegram 中可用。

| 命令 | 作用 |
|---|---|
| `/id` | 显示当前 Telegram chat id 和 chat type。 |
| `/reset` | 清空该 chat 的聊天历史、sticky skills、已知 mention 和 init 状态。 |
