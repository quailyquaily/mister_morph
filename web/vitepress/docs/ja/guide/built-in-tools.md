---
title: 組み込みツール
description: 静的ツール、ランタイム注入ツール、チャネル専用ツール。
---

# 組み込みツール

Mistermorph のツールは、最初からすべてを一括登録するわけではありません。実行環境に応じて段階的に注入されます。

1. 静的ツール: 設定とディレクトリ文脈だけで作られるもの。
2. Engine ツール: 1 回の agent engine 組み立て時に登録されるもの。
3. ランタイムツール: LLM client/model や task 文脈が必要なもの。
4. 専用ツール: Telegram や Slack など特定 runtime の中でだけ現れるもの。

## ツール分類の早見表

| 分類 | いつ使えるか | ツール |
|---|---|---|
| 静的ツール | 設定だけで利用可能 | `read_file`、`write_file`、`bash`、`url_fetch`、`web_search`、`contacts_send` |
| Engine ツール | agent engine が 1 回組み上がると利用可能 | `spawn`、`acp_spawn` |
| ランタイムツール | LLM や必要な文脈が利用可能なとき | `plan_create`、`todo_update` |
| チャネル専用ツール | 現在の Channel が Telegram / Slack などの具体的 runtime のとき | `telegram_send_voice`、`telegram_send_photo`、`telegram_send_file`、`message_react` |

## 静的ツール（設定駆動）

### `read_file`

ローカルのテキストファイルを読み込みます。Agent は設定ファイル、ログ、キャッシュ結果、`SKILL.md`、状態ファイルの確認に使います。

- 主な制約: `tools.read_file.deny_paths` の制約を受けます。`file_cache_dir/...` と `file_state_dir/...` のエイリアスに対応します。

### `write_file`

ローカルファイルへ上書きまたは追記で書き込みます。中間結果の保存、状態ファイルの更新、ダウンロード結果の保存に使います。

- 主な制約: 書き込み先は `file_cache_dir` / `file_state_dir` 配下に制限されます。相対パスはデフォルトで `file_cache_dir` 配下になります。サイズは `tools.write_file.max_bytes` に制限されます。

### `bash`

ローカルの `bash` コマンドを実行します。既存 CLI の利用、一時的な変換処理、スクリプト実行、環境確認に使います。

- 主な制約: `tools.bash.enabled` で無効化できます。`deny_paths` と内部 deny-token ルールの制約を受け、子プロセスには許可済み環境変数だけが渡されます。
- 現在の分離実行挙動: `run_in_subtask=true` を明示すると、コマンドは 1 層の direct boundary で実行されます。runtime が stream sink を持つ場合、stdout/stderr はコマンド終了前にプレビューへ流れます。

### `url_fetch`

HTTP(S) リクエストを実行して応答を返すか、応答をローカルキャッシュファイルへ保存します。`GET/POST/PUT/PATCH/DELETE`、`download_path`、`auth_profile` に対応します。

- 主な制約: 機密性の高いリクエストヘッダはブロックされます。リクエストは Guard のネットワークポリシーの対象です。

### `web_search`

Web 検索を行い、構造化された検索結果を返します。手がかり、候補ページ、公開情報の入口を探すのに向いています。

- 主な制約: 返るのは検索結果の要約であり、ページ全文ではありません。件数は `tools.web_search.max_results` とコード側の上限で制御されます。

### `contacts_send`

単一の連絡先に外向きメッセージを送ります。実際の送信経路は、連絡先プロフィールに登録された Telegram / Slack / LINE などから選ばれます。

- 主な制約: 一部の group/supergroup 文脈ではデフォルトで隠されます。

## Engine ツール

これらのツールは 1 回の agent engine 組み立て時に登録されます。現在の engine 状態に依存するため、静的な base registry には含まれません。

### `spawn`

独立した文脈と明示的なツールホワイトリストを持つ Subagent を起動します。親 agent は内側の実行終了まで同期で待ち、構造化された JSON envelope を受け取ります。

- 主な制約: `tools.spawn.enabled` で無効化できます。内側の agent が使えるのは引数 `tools` で明示したツールだけです。生 transcript はデフォルトで親 loop に戻しません。
- 現在の観測ヒント: `spawn` は任意で `observe_profile` を受け取れます。`default` は実行中プレビューを保守的に扱い、`long_shell` は長いシェル出力やログ向き、`web_extract` は生の高ノイズ出力をいったん抑えます。

引数の詳細、返り値 envelope の各フィールド、test prompt、`bash.run_in_subtask=true` との違いは [Subagents](/ja/guide/subagents) を参照してください。

### `acp_spawn`

設定済み profile を通して外部 ACP agent を起動します。親 agent は同期で待ちますが、内側の実行は別のローカル Mister Morph loop ではなく ACP 経由です。

- 主な制約: `tools.acp_spawn.enabled` で無効化できます。対応する profile が `acp.agents` に必要です。現在の transport は `stdio` のみです。
- 現在の挙動: 1 回の `acp_spawn` は 1 つの ACP session を作り、file / terminal callback を処理し、他の分離実行と同じ `SubtaskResult` envelope を返します。

profile 設定、実行時の流れ、Codex adapter の注意点は [ACP](/ja/guide/acp) を参照してください。

## ランタイムツール

これらのツールは、Agent 実行中に動的に注入されます。

### `plan_create`

構造化された実行計画 JSON を生成します。通常は複雑なタスク分解で使われます。

- 主な制約: ステップ数は `tools.plan_create.max_steps` に制限されます。

### `todo_update`

`file_state_dir` 配下の `TODO.md` / `TODO.DONE.md` を更新し、TODO の追加と完了を扱います。

- 主な制約: `add` では `people` が必要です。`complete` は意味的マッチングを使うため、候補がない場合や曖昧な場合はエラーになります。

## 専用ツール

これらのツールは、通常の CLI や汎用 embedding では存在しません。対応するチャネル runtime に十分な文脈があるときだけ注入されます。

### `telegram_send_voice`

ローカルの音声ファイルを現在の Telegram チャットへ送ります。

- 主な制約: ローカルファイル送信のみ対応します。通常は `file_cache_dir` 配下のファイルが前提で、インラインの text-to-speech 生成は行いません。

### `telegram_send_photo`

ローカル画像を Telegram にインライン写真として送ります。

- 主な制約: これは写真送信であり、文書送信ではありません。添付ファイルとして届けたい場合は `telegram_send_file` を使います。

### `telegram_send_file`

ローカルのキャッシュファイルを Telegram にドキュメントとして送ります。

- 主な制約: 送れるのはローカルのキャッシュファイルだけです。ディレクトリは無効で、ファイルサイズ上限もあります。

### `message_react`

現在のメッセージに軽量な emoji reaction を付けます。確認、賛同、既読に近いリアクションなど、わざわざ別のテキスト返信を出すほどではない場面に向いています。

- Telegram 版: Telegram メッセージに emoji reaction を付け、`is_big` も使えます。
- Slack 版: Slack メッセージに reaction を付けます。raw Unicode emoji ではなく Slack の emoji 名を使います。
- 主な制約: 引数の形はチャネルごとに異なります。対象メッセージ文脈がない場合、このツールは現れないか、明示的な対象指定が必要になります。

## Core 組み込み時のホワイトリスト

`integration.Config.BuiltinToolNames` で組み込みツールを制限できます。

```go
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "todo_update"}
```

空配列は全組み込みツールを意味します。

## 設定セクション

```yaml
tools:
  read_file: ...
  write_file: ...
  spawn: ...
  acp_spawn: ...
  bash: ...
  url_fetch: ...
  web_search: ...
  contacts_send: ...
  todo_update: ...
  plan_create: ...
```

Console の Setup / Settings 画面と `/api/settings/agent` の `tools` payload も、`tools.spawn.enabled` や `tools.acp_spawn.enabled` のような同じ入れ子構造を使います。

完全な設定は [設定フィールド](/ja/guide/config-reference.md) を参照してください。
