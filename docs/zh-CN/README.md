# Mister Morph

统一 Agent CLI 与可复用的 Go Agent 核心。

## 目录

- [为什么选择 Mister Morph](#why-mistermorph)
- [快速开始](#quickstart)
- [支持模型](#supported-models)
- [守护进程模式](#daemon-mode)
- [Telegram 机器人模式](#telegram-bot-mode)
- [嵌入到其他项目](#embedding-to-other-projects)
- [内置工具](#built-in-tools)
- [Skills（技能）](#skills)
- [安全性](#security)
- [故障排查](#troubleshoots)
- [调试](#debug)
- [配置](#configuration)

<a id="why-mistermorph"></a>
## 为什么选择 Mister Morph

这个项目值得关注的原因：

- 🧩 **可复用的 Go 核心**：既能把 Agent 当 CLI 运行，也能以库或子进程的方式嵌入到其他应用。
- 🔒 **严肃的默认安全策略**：基于 profile 的凭据注入、Guard 脱敏、出站策略控制、带审计轨迹的异步审批（见 [../security.md](../security.md)）。
- 🧰 **实用的 Skills 系统**：可从 `file_state_dir/skills` 发现并注入 `SKILL.md`，支持简单的 on/off 控制（见 [../skills.md](../skills.md)）。
- 📚 **对新手友好**：这是一个以学习为导向的 Agent 项目；`docs/` 里有详细设计文档，也提供了 `--inspect-prompt`、`--inspect-request` 等实用调试工具。

<a id="quickstart"></a>
## 快速开始

### 第 1 步：安装

方案 A：从 GitHub Releases 下载预构建二进制（生产环境推荐）：

```bash
curl -fsSL -o /tmp/install-mistermorph.sh \
  https://raw.githubusercontent.com/quailyquaily/mistermorph/main/scripts/install-release.sh
bash /tmp/install-mistermorph.sh v0.1.0
```

安装脚本支持：

- `bash install-release.sh <version-tag>`
- `INSTALL_DIR=$HOME/.local/bin bash install-release.sh <version-tag>`

方案 B：使用 Go 从源码安装：

```bash
go install github.com/quailyquaily/mistermorph@latest
```

### 第 2 步：安装 Agent 运行所需文件与内置技能

```bash
mistermorph install
# 或
mistermorph install <dir>
```

`install` 命令会把必需文件和内置技能安装到 `~/.morph/skills/`（或你通过 `<dir>` 指定的目录）。

### 第 3 步：配置 API Key

不需要 `config.yaml` 也可以直接运行，推荐先用环境变量：

```bash
export MISTER_MORPH_LLM_API_KEY="YOUR_OPENAI_API_KEY_HERE"
# 可选：显式指定默认 provider/model
export MISTER_MORPH_LLM_PROVIDER="openai"
export MISTER_MORPH_LLM_MODEL="gpt-5.2"
```

Mister Morph 也支持 Azure OpenAI、Anthropic Claude、AWS Bedrock 等（更多配置见 `../../assets/config/config.example.yaml`）。如果你更喜欢文件配置，也可以使用 `~/.morph/config.yaml`。

### 第 4 步：首次运行

```bash
mistermorph run --task "Hello!"
```

<a id="supported-models"></a>
## 支持模型

> 模型支持情况可能因具体模型 ID、provider endpoint 能力和 tool-calling 行为而变化。

| Model family | Model range | Status |
|---|---|---|
| GPT | `gpt-5*` | ✅ Full |
| GPT-OSS | `gpt-oss-120b` | ✅ Full |
| Grok | `grok-4+` | ✅ Full |
| Claude | `claude-3.5+` | ✅ Full |
| DeepSeek | `deepseek-3*` | ✅ Full |
| Gemini | `gemini-2.5+` | ✅ Full |
| Kimi | `kimi-2.5+` | ✅ Full |
| MiniMax | `minimax* / minimax-m2.5+` | ✅ Full |
| GLM | `glm-4.6+` | ✅ Full |
| Cloudflare Workers AI | `Workers AI model IDs` | ⚠️ Limited (no tool calling) |

<a id="telegram-bot-mode"></a>
## Telegram 机器人模式

通过长轮询运行 Telegram 机器人，这样你就可以直接在 Telegram 里和 Agent 对话：

编辑 `~/.morph/config.yaml`，设置 Telegram bot token：

```yaml
telegram:
  bot_token: "YOUR_TELEGRAM_BOT_TOKEN_HERE"
  allowed_chat_ids: [] # 在这里加入允许的 chat id
```

```bash
mistermorph telegram --log-level info
```

说明：
- 使用 `/id` 获取当前 chat id，并把它加入 `allowed_chat_ids` 白名单。
- 在群聊里，回复机器人消息或提及 `@BotUsername` 会触发响应。
- 你可以发送文件；文件会下载到 `file_cache_dir/telegram/`，Agent 可以处理它。Agent 也能通过 `telegram_send_file` 回传缓存文件，通过 `telegram_send_photo` 回传缓存图片，还可以通过 `telegram_send_voice` 发送位于 `file_cache_dir` 的本地语音文件。
- 每个 chat 会保留最近一次加载的 skill（sticky），后续消息不会“忘记” `SKILL.md`；可用 `/reset` 清除。
- `telegram.group_trigger_mode=smart` 会让每条群消息都进入 addressing LLM 判定；触发需满足 `addressed=true`，且 `confidence >= telegram.addressing_confidence_threshold`、`interject > telegram.addressing_interject_threshold`。
- `telegram.group_trigger_mode=talkative` 也会让每条群消息进入 addressing LLM 判定，但不要求 `addressed=true`（仍受 confidence/interject 阈值控制）。
- 可在 chat 中使用 `/reset` 清空对话历史。
- 默认支持多 chat 并发处理，但单个 chat 内按串行处理（配置项：`telegram.max_concurrency`）。

<a id="daemon-mode"></a>
## 守护进程模式（已废弃）

这是旧的本地 HTTP 守护进程模式。更推荐使用各 channel 自己的 runtime endpoint，并通过显式 `mistermorph submit --server-url ...` 提交任务。

启动守护进程：

```bash
export MISTER_MORPH_SERVER_AUTH_TOKEN="change-me"
mistermorph serve --server-listen 127.0.0.1:8787 --log-level info
```

提交任务：

```bash
mistermorph submit --server-url http://127.0.0.1:8787 --auth-token "$MISTER_MORPH_SERVER_AUTH_TOKEN" --wait \
  --task "Summarize this repo and write to ./summary.md"
```

<a id="embedding-to-other-projects"></a>
## 嵌入到其他项目

请参见 [../integration.md](../integration.md) 获取嵌入模式与示例。

<a id="built-in-tools"></a>
## 内置工具

Agent 可用的核心工具：

- `read_file`：读取本地文本文件。
- `write_file`：将文本文件写入 `file_cache_dir` 或 `file_state_dir`。
- `bash`：执行 shell 命令（默认禁用）。
- `url_fetch`：发起 HTTP 请求（可选 auth profile）。
- `web_search`：网页搜索（DuckDuckGo HTML）。
- `plan_create`：生成结构化计划。

频道运行时工具：

- `telegram_send_file`：在 Telegram 发送文件（仅 Telegram）。
- `telegram_send_photo`：在 Telegram 发送图片（仅 Telegram）。
- `telegram_send_voice`：在 Telegram 发送语音消息（仅 Telegram）。
- `message_react`：在消息上添加 emoji reaction（Telegram/Slack 运行时，参数随频道不同）。

详细工具文档请见 [../tools.md](../tools.md)。

<a id="skills"></a>
## Skills（技能）

`mistermorph` 可以在 `file_state_dir/skills` 下递归发现 skills，并将选中的 `SKILL.md` 内容注入 system prompt。

默认情况下，`run` 使用 `skills.enabled=true`；`skills.load=[]` 表示加载全部已发现技能，未知技能名会被忽略。

文档： [../skills.md](../skills.md)。

```bash
# 列出可用 skills
mistermorph skills list
# 在 run 命令中使用指定 skill
mistermorph run --task "..." --skills-enabled --skill skill-name
# 安装远程 skill
mistermorph skills install <remote-skill-url>
```

### Skills 的安全机制

1. 安装审计：安装远程 skill 时，Mister Morph 会先预览技能内容，并做基础安全审计（例如扫描脚本中的危险命令），再请求用户确认。
2. Auth profiles：skill 可以在 `auth_profiles` 字段声明依赖的认证配置。这里的声明只用于提示和上下文，不构成权限边界。真正的授权只来自宿主机配置里的 `secrets.allow_profiles` 和 `auth_profiles`（见 `../../assets/skills/moltbook` 以及配置文件相关部分）。

<a id="security"></a>
## 安全性

推荐的 systemd 加固与密钥管理方式： [../security.md](../security.md)。

<a id="troubleshoots"></a>
## 故障排查

已知问题与规避建议： [../troubleshoots.md](../troubleshoots.md)。

<a id="debug"></a>
## 调试

### 日志

可通过参数 `--log-level` 设置日志级别与格式：

```bash
mistermorph run --log-level debug --task "..."
```

### 导出内部调试数据

可用 `--inspect-prompt` / `--inspect-request` 这两个参数导出内部状态，便于调试：

```bash
mistermorph run --inspect-prompt --inspect-request --task "..."
```

这些参数会把最终 system/user/tool prompt，以及完整 LLM 请求/响应 JSON，以纯文本形式输出到 `./dump` 目录。

<a id="configuration"></a>
## 配置

`mistermorph` 使用 Viper，因此你可以通过 flags、环境变量或配置文件进行配置。

- 配置文件：`--config /path/to/config.yaml`（支持 `.yaml/.yml/.json/.toml/.ini`）
- 环境变量前缀：`MISTER_MORPH_`
- 嵌套键：将 `.` 和 `-` 替换为 `_`（例如 `tools.bash.enabled` → `MISTER_MORPH_TOOLS_BASH_ENABLED=true`）

### CLI 参数

**全局（所有命令）**
- `--config`
- `--log-level`
- `--log-format`
- `--log-add-source`
- `--log-include-thoughts`
- `--log-include-tool-params`
- `--log-include-skill-contents`
- `--log-max-thought-chars`
- `--log-max-json-bytes`
- `--log-max-string-value-chars`
- `--log-max-skill-content-chars`
- `--log-redact-key`（可重复）

**run**
- `--task`
- `--provider`
- `--endpoint`
- `--model`
- `--api-key`
- `--llm-request-timeout`
- `--interactive`
- `--skills-dir`（可重复）
- `--skill`（可重复）
- `--skills-enabled`
- `--max-steps`
- `--parse-retries`
- `--max-token-budget`
- `--timeout`
- `--inspect-prompt`
- `--inspect-request`

**serve**（已废弃）
- `--server-listen`
- `--server-auth-token`
- `--server-max-queue`

**submit**
- `--task`
- `--server-url`
- `--auth-token`
- `--model`
- `--submit-timeout`
- `--wait`
- `--poll-interval`
