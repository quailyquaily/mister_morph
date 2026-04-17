---
title: Subagents
description: "まず典型場面、その次に全体像、最後に現在の実装 details と test prompts をまとめる。"
---

# Subagents

## 典型的な場面

Subagent は主に次のような場面で使います。

- shell コマンドが長く、出力も多いので、親 loop から切り離したい。
- 処理自体は複数ステップだが、内側の実行に許すツールを絞りたい。
- 中間の生出力ではなく、最後に短い結果だけを親へ返したい。
- 子タスクを別のローカル Mister Morph loop ではなく、外部 ACP agent に委譲したい。

入口の選び方は次の通りです。

- 1 本の明確な shell コマンドなら `bash.run_in_subtask=true`。
- 内側の実行でもツール推論が必要なら `spawn`。
- 親がそのまま終えられる小さな 1 ステップ作業なら、無理に 1 層増やさない。

## Overview

Mistermorph には現在、三つの隔離タスク入口があります。

| 入口 | もう一度 LLM loop を起こすか | 向いている場面 | 返り値 |
|---|---|---|---|
| `spawn` | 起こす | 内側の agent 側でもツール利用と推論が必要 | `SubtaskResult` JSON envelope |
| `acp_spawn` | ローカル内側 Mister Morph loop は起こさず、外部 ACP session を開始する | 外部 ACP agent や adapter | `SubtaskResult` JSON envelope |
| `bash.run_in_subtask=true` | 起こさない | 1 本の shell コマンドを隔離して実行したい | `SubtaskResult` JSON envelope |

共通点:

- いずれも現状は同期実行で、親は内側の実行終了まで待ちます。
- いずれも同じ深さ制限を共有します。
- いずれも同じトップレベル envelope を返します。
- 内側の生 transcript はデフォルトで親 loop に戻しません。

これは隔離と結果回収の仕組みであって、まだバックグラウンド job システムではありません。

ACP の補足:

- `acp_spawn` も内側の agent 境界ですが、その境界は別のローカル Mister Morph engine ではなく、外部 ACP agent プロセスが担当します。

## 現在の実装

### `spawn`

`spawn` は engine スコープのツールです。agent engine が 1 回組み上がったときにだけ現れます。

引数:

- `task`: 必須。内側の agent へのプロンプト。
- `tools`: 必須。非空のツール名配列。
- `model`: 任意。内側の agent 用モデル上書き。
- `output_schema`: 任意。構造化出力ラベル。
- `observe_profile`: 任意。観測ヒント。現在は `default`、`long_shell`、`web_extract` をサポートします。

現在の挙動:

- 内側の registry は `tools` からだけ作られます。
- 未知のツール名や親 registry に存在しない名前は無視されます。
- 最終的に使えるツールが 1 つも残らなければ失敗します。
- `tools` に `spawn` を入れても、内側の agent には再公開されません。

### `acp_spawn`

`acp_spawn` も engine スコープのツールです。

引数:

- `agent`: 必須。`acp.agents` の profile 名
- `task`: 必須。外部 ACP agent へのプロンプト
- `cwd`: 任意。作業ディレクトリ上書き
- `output_schema`: 任意。構造化出力ラベル
- `observe_profile`: 任意。観測ヒント

現在の挙動:

- 1 回の呼び出しで 1 つの ACP session を作ります
- transport は現状 `stdio` だけです
- 子タスク中は ACP の permission / file / terminal callback を処理できます
- 最終結果は `spawn` と同じ `SubtaskResult` envelope に正規化されます

profile 設定と transport の詳細は [ACP](/ja/guide/acp) を参照してください。

### `bash.run_in_subtask=true`

こちらはより軽い分離実行経路です。

- `bash` の direct path を使います。
- 2 回目の LLM loop は起動しません。
- `output_schema` は `subtask.bash.result.v1` に固定です。
- 観測 profile は `long_shell` に固定です。

内側の仕事が 1 本の shell ステップで、追加のツール判断が不要ならこちらを使います。

### 深さ制限

現在の深さ制限は `1` です。

- ルート run は 1 層だけ分離実行に入れます。
- すでにその層にいる run は次の層へ進めません。

### `output_schema`

`output_schema` は契約ラベルであり、組み込み JSON Schema レジストリではありません。

`spawn` でこれを指定すると:

- 内側の agent には JSON の最終出力を要求します。
- runtime は最終出力が JSON、または JSON として解釈できる文字列であることを要求します。
- 結果 envelope には同じ識別子が `output_schema` として返ります。

ただし Mistermorph 自体は実在する schema 定義でオブジェクト検証までは行いません。

### 返り値 Envelope

三つの入口はいずれも最後は次の形の JSON を返します。

```json
{
  "task_id": "sub_123",
  "status": "done",
  "summary": "subtask completed",
  "output_kind": "text",
  "output_schema": "",
  "output": "child result",
  "error": ""
}
```

各フィールド:

- `status`: 現状は主に `done` または `failed`。
- `summary`: この分離実行の短い状態文。
- `output_kind`: `text` または `json`。
- `output_schema`: テキスト出力なら空、構造化出力なら渡した識別子。
- `output`: 結果本体。
- `error`: 失敗時だけ入ります。

`bash.run_in_subtask=true` の場合、`output` は `exit_code`、切り捨てフラグ、`stdout`、`stderr` を含む構造化 JSON です。

### Test Prompts

最小の smoke test として使いやすい例です。前提は `spawn` と `bash` が有効なことです。

#### Prompt 1: `spawn` + `bash`, 1 行だけ返す

```text
You must call the spawn tool. Do not answer directly. Allow the inner agent to use only bash. Have it run `printf 'alpha\nbeta\ngamma\n' | sed -n '2p'`. Return only the second line.
```

期待結果: `beta`

#### Prompt 2: `spawn` + `bash`, 構造化 JSON を返す

```text
You must call the spawn tool and set output_schema to `subagent.demo.echo.v1`. Allow the inner agent to use only bash. Have it run `echo '{"ok":true,"value":42}'`. Return structured JSON only, with no explanation.
```

期待結果:

```json
{"ok":true,"value":42}
```

#### Prompt 3: `bash.run_in_subtask=true`

```text
Call the bash tool and set `run_in_subtask` to true. Run `printf 'one\ntwo\nthree\n' | tail -n 1`. Do not explain anything. Return only the last line.
```

期待結果: `three`

#### Prompt 4: 少し長い shell 実行

```text
Call the bash tool and set `run_in_subtask` to true. Run `sleep 1; echo SUBAGENT_BASH_OK`. Reply with stdout only.
```

期待結果: `SUBAGENT_BASH_OK`

### 設定と組み込み

- `tools.spawn.enabled` が制御するのは明示的な `spawn` ツール入口だけです。
- `tools.acp_spawn.enabled` が制御するのは明示的な `acp_spawn` ツール入口だけです。
- ACP profile は `acp.agents` に置きます。
- `tools.spawn.enabled=false` でも、`bash.run_in_subtask=true` のような direct path は動きます。
- `integration.Config.BuiltinToolNames` には `spawn` と `acp_spawn` を含めることも外すこともできます。
- `agent.New(...)` で直接 engine を作る場合、`spawn` はデフォルトで有効、`acp_spawn` はデフォルトで無効です。必要なら `agent.WithSpawnToolEnabled(...)`、`agent.WithACPSpawnToolEnabled(...)`、`agent.WithACPAgents(...)` を使います。

例:

```go
cfg := integration.DefaultConfig()
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "spawn", "acp_spawn"}
cfg.Set("tools.spawn.enabled", true)
cfg.Set("tools.acp_spawn.enabled", true)
```

あわせて読む:

- [組み込みツール](/ja/guide/built-in-tools)
- [ACP](/ja/guide/acp)
- [自分の AI Agent を作る：上級編](/ja/guide/build-your-own-agent-advanced)
- [設定フィールド](/ja/guide/config-reference)
