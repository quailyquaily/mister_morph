---
title: CLI フラグ
description: mistermorph が現在サポートしているコマンドラインフラグ一覧。
---

# CLI フラグ

このページは、現在の `mistermorph --help` と各サブコマンドの `--help` 出力をもとに、利用できる CLI フラグをまとめたものです。

本文で展開していないもの:

- `completion`、`help` などの Cobra 組み込み補助コマンド
- `version`。現在は専用フラグを持ちません

## グローバルフラグ

これらのフラグは大半のコマンドで継承されます。

- `--config`: 設定ファイルのパス。
- `--log-add-source`: ログにソース `file:line` を含める。
- `--log-format`: ログ形式。`text|json`。
- `--log-include-skill-contents`: ログに読み込んだ `SKILL.md` 内容を含める。
- `--log-include-thoughts`: ログに model thoughts を含める。
- `--log-include-tool-params`: ログにツール引数を含める。
- `--log-level`: ログレベル。`debug|info|warn|error`。
- `--log-max-json-bytes`: ログに出す JSON 引数の最大バイト数。
- `--log-max-skill-content-chars`: ログに出す `SKILL.md` の最大文字数。
- `--log-max-string-value-chars`: ログに出す各文字列値の最大文字数。
- `--log-max-thought-chars`: ログに出す thought の最大文字数。
- `--log-redact-key`: ログで追加マスクする引数キー。繰り返し指定可。

## `benchmark`

このコマンドは任意の位置引数 `profile-name` を受け取れます。省略した場合は、デフォルト route とすべての名前付き LLM profile を benchmark します。

- `--json`: benchmark 結果を JSON で出力する。
- `--timeout`: 選択した benchmark 全体のタイムアウト。`0` で無効。

## `run`

- `--api-key`: API key。
- `--endpoint`: provider の base URL。
- `--heartbeat`: `--task` と stdin を無視して heartbeat を 1 回だけ実行する。
- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。
- `--interactive`: Ctrl-C で一時停止し、追加コンテキストを注入できるようにする。
- `--llm-request-timeout`: LLM HTTP リクエスト単位のタイムアウト。
- `--max-steps`: tool-call の最大ステップ数。
- `--max-token-budget`: 累積 token budget 上限。
- `--model`: モデル名。
- `--parse-retries`: JSON 解析の最大リトライ回数。
- `--provider`: provider 名。
- `--skill`: 読み込む skill の名前または id。繰り返し指定可。
- `--skills-dir`: skills ルートディレクトリ。繰り返し指定可。
- `--skills-enabled`: 設定済み skills の読み込みを有効にする。
- `--task`: 実行するタスク。空なら stdin から読む。
- `--timeout`: 全体タイムアウト。
- `--tool-repeat-limit`: 同じ成功ツール呼び出しが繰り返されすぎたら final を強制する。

## `console serve`

- `--allow-empty-password`: `console.password` / `console.password_hash` なしでも console を起動できるようにする。
- `--console-base-path`: Console の base path。
- `--console-listen`: Console サーバーの listen アドレス。
- `--console-session-ttl`: Console bearer token の session TTL。
- `--console-static-dir`: SPA 静的ファイルのディレクトリ。
- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。

## `telegram`

- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。
- `--telegram-addressing-confidence-threshold`: addressing 判定を受け入れる最小 confidence。
- `--telegram-addressing-interject-threshold`: addressing 判定を受け入れる最小 interject スコア。
- `--telegram-allowed-chat-id`: 許可する chat id。繰り返し指定可。
- `--telegram-bot-token`: Telegram bot token。
- `--telegram-group-trigger-mode`: グループトリガーモード。`strict|smart|talkative`。
- `--telegram-max-concurrency`: 同時処理する chat の最大数。
- `--telegram-poll-timeout`: `getUpdates` の long polling timeout。
- `--telegram-task-timeout`: メッセージ単位の agent timeout。

## `slack`

- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。
- `--slack-addressing-confidence-threshold`: addressing 判定を受け入れる最小 confidence。
- `--slack-addressing-interject-threshold`: addressing 判定を受け入れる最小 interject スコア。
- `--slack-allowed-channel-id`: 許可する Slack channel id。繰り返し指定可。
- `--slack-allowed-team-id`: 許可する Slack team id。繰り返し指定可。
- `--slack-app-token`: Socket Mode 用 Slack app token。
- `--slack-bot-token`: Slack bot token。
- `--slack-group-trigger-mode`: グループトリガーモード。`strict|smart|talkative`。
- `--slack-max-concurrency`: 同時処理する Slack 会話の最大数。
- `--slack-task-timeout`: メッセージ単位の agent timeout。

## `line`

- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。
- `--line-addressing-confidence-threshold`: addressing 判定を受け入れる最小 confidence。
- `--line-addressing-interject-threshold`: addressing 判定を受け入れる最小 interject スコア。
- `--line-allowed-group-id`: 許可する LINE group id。繰り返し指定可。
- `--line-base-url`: LINE API base URL。
- `--line-channel-access-token`: LINE channel access token。
- `--line-channel-secret`: webhook 署名検証用の LINE channel secret。
- `--line-group-trigger-mode`: グループトリガーモード。`strict|smart|talkative`。
- `--line-max-concurrency`: 同時処理する LINE 会話の最大数。
- `--line-task-timeout`: メッセージ単位の agent timeout。
- `--line-webhook-listen`: LINE webhook サーバーの listen アドレス。
- `--line-webhook-path`: LINE webhook callback の HTTP パス。

## `lark`

- `--inspect-prompt`: prompt messages を `./dump` に保存する。
- `--inspect-request`: LLM request/response payload を `./dump` に保存する。
- `--lark-addressing-confidence-threshold`: addressing 判定を受け入れる最小 confidence。
- `--lark-addressing-interject-threshold`: addressing 判定を受け入れる最小 interject スコア。
- `--lark-allowed-chat-id`: 許可する Lark chat id。繰り返し指定可。
- `--lark-app-id`: Lark app id。
- `--lark-app-secret`: Lark app secret。
- `--lark-base-url`: Lark Open API base URL。
- `--lark-encrypt-key`: Lark event subscription encrypt key。
- `--lark-group-trigger-mode`: グループトリガーモード。`strict|smart|talkative`。
- `--lark-max-concurrency`: 同時処理する Lark 会話の最大数。
- `--lark-task-timeout`: メッセージ単位の agent timeout。
- `--lark-verification-token`: Lark event subscription verification token。
- `--lark-webhook-listen`: Lark webhook サーバーの listen アドレス。
- `--lark-webhook-path`: Lark webhook callback の HTTP パス。

## `install`

- `-y, --yes`: 確認プロンプトをスキップする。

## `skills list`

- `--skills-dir`: skills ルートディレクトリ。繰り返し指定可。

## `skills install`

- `--clean`: コピー前に既存 skill ディレクトリを削除する。
- `--dest`: 出力先ディレクトリ。
- `--dry-run`: 書き込みをせず操作だけ表示する。
- `--max-bytes`: リモート `SKILL.md` の最大ダウンロードバイト数。
- `--skip-existing`: 出力先に既に存在するファイルをスキップする。
- `--timeout`: リモート `SKILL.md` ダウンロードのタイムアウト。
- `-y, --yes`: 確認プロンプトをスキップする。

## `tools`

このコマンドは現在、専用フラグを持ちません。
