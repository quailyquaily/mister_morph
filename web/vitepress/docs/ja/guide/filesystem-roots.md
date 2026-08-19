---
title: ファイルシステムのルート
description: workspace_dir、file_cache_dir、file_state_dir の意味と、Mistermorph における使い分けを説明します。
---

# ファイルシステムのルート

Mistermorph では、ファイルシステム上のディレクトリを 3 種類に分けて扱います。

- `workspace_dir`: 現在の session または topic に紐づくプロジェクトディレクトリ
- `file_cache_dir`: 再生成可能なキャッシュファイル
- `file_state_dir`: 永続化すべき実行状態

この 3 つは混ぜないでください。プロジェクト本体、捨ててよい一時ファイル、保持すべき状態を分けた方が、モデルとツールの挙動が安定します。

## 3 つのルート

| ルート | 意味 | 典型的な内容 | 永続性 |
|---|---|---|---|
| `workspace_dir` | Agent が「今作業している対象」として扱うプロジェクトツリー。ランタイム文脈で渡されるもので、グローバル設定キーではありません。 | ソースコード、ドキュメント、設定ファイル、プロジェクトメモ | 通常はユーザー管理 |
| `file_cache_dir` | 実行中に作られるが、消しても再生成できるファイル | ダウンロード物、一時変換結果、取得アーティファクト、生成メディア | 削除してよい |
| `file_state_dir` | 再起動後も残すべき実行状態 | tasks、contacts、skills、guard state、workspace attachment | 永続化推奨 |

デフォルトは次の通りです。

- `file_state_dir` は `~/.morph`
- `file_cache_dir` は `~/.cache/morph`

`workspace_dir` は別物です。これはグローバル設定ではなく、現在の runtime が付与するプロジェクトディレクトリです。

## `workspace_dir` の付き方

CLI chat では次のように使います。

```bash
mistermorph chat --workspace .
```

現在の挙動:

- `mistermorph chat` はカレントディレクトリをデフォルトで `workspace_dir` として付与します
- `mistermorph chat --workspace <dir>` で明示的にディレクトリを選べます
- `mistermorph chat --no-workspace` で workspace なしで開始できます
- chat 中は `/workspace`、`/workspace attach <dir>`、`/workspace detach` で状態確認や切り替えができます

ほかの runtime もそれぞれの方法で `workspace_dir` を渡せます。重要なのは、`workspace_dir` が現在の会話に紐づくプロジェクトディレクトリであって、グローバルな state 保存先ではない点です。

## パスエイリアス

次の 3 つのエイリアスで対象ルートを明示できます。

- `workspace_dir/...`
- `file_cache_dir/...`
- `file_state_dir/...`

prompt、スクリプト、ツール引数で曖昧さを避けたいときは、エイリアスを使ってください。

## ツールごとのパス解決

### `read_file`

- `workspace_dir` がある場合、相対パスはそこを基準に解決されます
- `workspace_dir` がない場合、相対パスは `file_cache_dir` を基準に解決されます
- エイリアスを使えば対象ルートを固定できます

### `write_file`

- `workspace_dir` がある場合、相対パスはそこを基準に解決されます
- `workspace_dir` がない場合、相対パスは `file_cache_dir` を基準に解決されます
- 書き込み先は `workspace_dir`、`file_cache_dir`、`file_state_dir` の中に制限されます
- エイリアスを使えば対象ルートを固定できます

### `bash` と `powershell`

- コマンド文字列の中で `workspace_dir`、`file_cache_dir`、`file_state_dir` のエイリアスを使えます
- shell の `cwd` を省略し、`workspace_dir` がある場合、shell はそこから開始します

そのため、`read_file` や `write_file` を直接使わない場合でも、`workspace_dir` は実際のツール挙動に影響します。

## 実務上の使い分け

- Agent にプロジェクト本体として扱わせたいコードツリーは `workspace_dir` に置く
- ダウンロード物や一時ファイル、捨ててよい生成結果は `file_cache_dir` に置く
- contacts、インストール済み skills、task state など長く残すものは `file_state_dir` に置く

## 関連ページ

- [CLI フラグ](/ja/guide/cli-flags)
- [設定フィールド](/ja/guide/config-reference)
- [組み込みツール](/ja/guide/built-in-tools)
- [設定パターン](/ja/guide/config-patterns)
