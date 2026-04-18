---
title: 環境変数
description: 完全な環境変数モデル、マッピング規則、互換変数。
---

# 環境変数

## 優先順位

適用順序:

1. CLI フラグ
2. `MISTER_MORPH_*` 環境変数
3. `config.yaml`
4. コードデフォルト

## 完全対応ルール

すべての設定キーは次の規則で env 上書きできます。

- 接頭辞: `MISTER_MORPH_`
- 大文字化
- `.` と `-` を `_` に変換

例:

- `llm.api_key` -> `MISTER_MORPH_LLM_API_KEY`
- `tools.bash.enabled` -> `MISTER_MORPH_TOOLS_BASH_ENABLED`
- `tools.powershell.enabled` -> `MISTER_MORPH_TOOLS_POWERSHELL_ENABLED`
- `tools.spawn.enabled` -> `MISTER_MORPH_TOOLS_SPAWN_ENABLED`
- `mcp.servers` -> `MISTER_MORPH_MCP_SERVERS`

つまり、[設定フィールド](/ja/guide/config-reference)の全キーが env 対応です。

## 利用頻度の高い変数

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

## config 内の `${ENV_VAR}` 展開

config の全 string 値で `${ENV_VAR}` 展開が使えます。

```yaml
llm:
  api_key: "${OPENAI_API_KEY}"
mcp:
  servers:
    - name: remote
      headers:
        Authorization: "Bearer ${MCP_REMOTE_TOKEN}"
```

注意:

- `${NAME}` 形式のみ展開
- 裸の `$NAME` は展開しない
- 未設定変数は空文字に置換され warning が出る

## 互換 / 特殊環境変数

- `TELEGRAM_BOT_TOKEN`
  - `mistermorph telegram send` のみの互換フォールバック
  - 推奨は `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `NO_COLOR`、`TERM=dumb`
  - CLI の色表示挙動のみ変更

## 実務パターン

機密値は `${ENV_VAR}` を使い、実値は実行環境から注入する運用を推奨します。
