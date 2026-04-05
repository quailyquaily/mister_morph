---
title: 环境变量
description: 完整环境变量模型、映射规则与兼容变量说明。
---

# 环境变量

## 优先级

生效顺序：

1. CLI flags
2. `MISTER_MORPH_*` 环境变量
3. `config.yaml`
4. 代码默认值

## 完整支持规则

所有配置键都可按同一规则映射成环境变量：

- 前缀：`MISTER_MORPH_`
- 转大写
- `.` 和 `-` 替换为 `_`

示例：

- `llm.api_key` -> `MISTER_MORPH_LLM_API_KEY`
- `tools.bash.enabled` -> `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `tools.spawn.enabled` -> `MISTER_MORPH_TOOLS_SPAWN_ENABLED`
- `mcp.servers` -> `MISTER_MORPH_MCP_SERVERS`

因此，[配置字段](/zh/guide/config-reference)中的全部字段都支持环境变量覆盖。

## 高频环境变量

- `MISTER_MORPH_CONFIG`
- `MISTER_MORPH_LLM_PROVIDER`
- `MISTER_MORPH_LLM_ENDPOINT`
- `MISTER_MORPH_LLM_MODEL`
- `MISTER_MORPH_LLM_API_KEY`
- `MISTER_MORPH_SERVER_AUTH_TOKEN`
- `MISTER_MORPH_CONSOLE_PASSWORD`
- `MISTER_MORPH_CONSOLE_PASSWORD_HASH`
- `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `MISTER_MORPH_SLACK_BOT_TOKEN`
- `MISTER_MORPH_SLACK_APP_TOKEN`
- `MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN`
- `MISTER_MORPH_LINE_CHANNEL_SECRET`
- `MISTER_MORPH_LARK_APP_ID`
- `MISTER_MORPH_LARK_APP_SECRET`
- `MISTER_MORPH_FILE_STATE_DIR`
- `MISTER_MORPH_FILE_CACHE_DIR`

## 配置内 `${ENV_VAR}` 展开

配置里的所有字符串都支持 `${ENV_VAR}` 展开。

```yaml
llm:
  api_key: "${OPENAI_API_KEY}"
mcp:
  servers:
    - name: remote
      headers:
        Authorization: "Bearer ${MCP_REMOTE_TOKEN}"
```

说明：

- 仅展开 `${NAME}` 形式
- 裸 `$NAME` 不展开
- 未设置变量会替换为空字符串并给出 warning

## 兼容/特殊环境变量

- `TELEGRAM_BOT_TOKEN`
  - 仅用于 `mistermorph telegram send` 的兼容回退
  - 优先仍是 `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `NO_COLOR`、`TERM=dumb`
  - 仅影响 CLI 颜色输出

## 实践建议

敏感值建议用 `${ENV_VAR}` 占位，并在运行环境注入真实 secret。
