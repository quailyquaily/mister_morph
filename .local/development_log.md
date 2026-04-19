
## 2026-04-19 15:45 - Backup 分支剩余代码合并完成

### 合并内容

从 `backup-main-20260419` 分支移植剩余未合并代码到 `main`：

#### 1. ACP gemini_oauth 实时进度（PR #7，3 commits）

| 提交 | 说明 |
|------|------|
| `4eed426` | feat: add real-time progress feedback for gemini_oauth ACP provider |
| `1e4a7da` | feat: route gemini_oauth ACP progress events to Telegram and Slack |
| `efe5a0c` | feat(acp_llm): add structured logging for ACP tool events |

**涉及文件：**
- `internal/llmutil/acp_llm_client.go` (新增)
- `internal/llmutil/llmutil.go`
- `internal/toolsutil/static_register.go`
- `internal/acpclient/client.go`
- `internal/channelruntime/telegram/runtime_task.go`
- `internal/channelruntime/slack/runtime_task.go`
- `providers/gemini/client.go` (新增)
- `assets/config/config.example.yaml`
- `cmd/mistermorph/runcmd/run.go`

**处理：**
- 移除了 cherry-pick 带来的文档变更（`web/vitepress/docs/*/acp.md`）
- 移除了意外提交的 npm tarball (`nsalerni-gemini-acp-0.1.18.tgz`)
- 移除了 `help/gemini-cli-auth.md`（文档合并不在本次范围）
- `cmd/mistermorph/clicmd/cli.go` 冲突：main 中已重构为 `chatcmd/`，直接删除

#### 2. Compact Mode（PR #8，部分）

| 提交 | 说明 |
|------|------|
| `1f750c7` | feat(cli): add compact mode for minimal prompt/output display |

**注意：**
- `696e0b4`（green bullet prompt）只修改了 `clicmd/cli.go`，该文件在 main 中已删除
- main 中 `chatcmd/ui.go` 的 `buildUserPrompt()` 已使用绿色圆点 (`•`) 替代绿色背景 `>`，功能等价
- `configdefaults` 中已添加 `cli.compact_mode` 默认值

### 未合并（按用户要求跳过）

- **文档合并** (`fc98225`): `help/` → `docs/` 的文档结构调整，用户明确不要
- **部署脚本** (`c898e94`): `.local/scripts/deploy-opencode2api.sh` 已存在于 main

### 验证

- `go build ./cmd/mistermorph` ✅
- `go test ./...` ✅ 全部通过

### 当前状态

- `main` 分支已包含所有 backup 分支的功能代码
- 本地 `main` 领先 `origin/main` 14 commits
- 剩余未推送 PR：`feat/bedrock-aws-cli`（需创建 upstream PR）
- `pr-chat`（PR #35）等待 lyricat review

