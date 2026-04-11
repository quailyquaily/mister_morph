---
title: ACP
description: acp_spawn で外部 ACP agent を使う。
---

# ACP

Mister Morph は、隔離した 1 つの子タスクを外部 ACP agent に委譲できます。

現在の実装は絞っています。

- Mister Morph は ACP client であり、ACP server ではありません。
- transport は `stdio` だけです。
- `acp_spawn` 1 回につき、同期 session を 1 つ作り、prompt turn も 1 回です。
- 外部 agent プロセスは `acp.agents` から起動します。

## ACP を使う場面

子タスクを「別のローカル Mister Morph loop」ではなく、「外部 agent の実行スタック」で走らせたいときに使います。

典型例:

- ACP adapter 経由で Codex を使う
- 別の ACP 対応 coding agent を使う
- 親 loop は調停だけ行い、ファイル操作やコマンド実行は外部 agent に任せる

別のローカル Mister Morph loop で十分なら、[Subagents](/ja/guide/subagents) の `spawn` を使ってください。

## 現在サポートしているもの

- `authenticate`
- `session/new`
- `session/new` が宣言した option id に対する `session/set_config_option`
- `session/prompt`
- `session/request_permission`
- `fs/read_text_file`
- `fs/write_text_file`
- 最小の `terminal/*`

まだ未対応:

- MCP passthrough
- session 再利用
- HTTP / SSE transport

## 設定

必要なのは 2 点です。

1. 明示的なツール入口を有効にする
2. 1 つ以上の ACP profile を定義する

```yaml
tools:
  acp_spawn:
    enabled: true

acp:
  agents:
    - name: "codex"
      enable: true
      type: "stdio"
      command: "codex-acp"
      args: []
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        mode: "auto"
        reasoning_effort: "low"
```

補足:

- `tools.acp_spawn.enabled` が制御するのは `acp_spawn` 入口だけです。
- `session_options` はまず `session/new._meta` に渡します。
- ACP agent が config option id を宣言した場合、同名キーは `session/set_config_option` でも送ります。

## Prompt パターン

親 agent には `acp_spawn` を明示的に使わせます。

例:

```text
Only call acp_spawn. Use the codex agent. Read ./README.md and summarize it in exactly 5 Chinese sentences. Do not call spawn. Do not read the file yourself.
```

`acp_spawn` の主な引数:

- `agent`
- `task`
- `cwd`
- `output_schema`
- `observe_profile`

返り値は既存の `SubtaskResult` envelope と同じ形です。

## 実行時の流れ

1 回の `acp_spawn` は次の順です。

1. wrapper プロセスを起動
2. `initialize`
3. 必要なら `authenticate`
4. `session/new`
5. 宣言済み option に `session/set_config_option`
6. `session/prompt`
7. turn 中の file / permission / terminal callback を処理
8. 最終テキストを回収

## セキュリティ上の注意

ACP の permission request だけが境界ではありません。

実際の制限は、実装済み client メソッド側で行われます。

- 許可したファイル root
- 許可した terminal の作業ディレクトリ
- ローカルの書き込み・プロセス実行ルール

もう 1 点重要なのは、wrapper 自体もローカル子プロセスだということです。ACP callback の制限は、wrapper 自身の任意の直接動作まで自動的にサンドボックス化しません。

## Adapter 経由の Codex

現在の Codex 連携は `codex-acp` のような ACP adapter を前提にしています。

確認ポイント:

1. まず `codex` 単体で動くこと
2. `mistermorph tools` に `acp_spawn` が出ること
3. ACP profile の `command` が `codex-acp` を指していること

関連ページ:

- [Subagents](/ja/guide/subagents)
- [組み込みツール](/ja/guide/built-in-tools)
- [設定フィールド](/ja/guide/config-reference)
