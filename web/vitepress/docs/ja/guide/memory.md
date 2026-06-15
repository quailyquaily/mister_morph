---
title: Memory
description: journal ベースの memory アーキテクチャ、投影、注入ルールを説明します。
---

# Memory

Mister Morph の memory システムは、受理したイベントを先に統一 journal へ書き、その後で非同期投影します。

## Journal

受理した memory event は JSONL として発生順に記録されます。統一 journal が事実の source of truth です。

journal は `<file_state_dir>/journal/` 配下にあります。

現在の segment が設定サイズに達すると、次の append は `events.000000000000000002.jsonl` のような新しい安定 segment に書き込みます。

## 投影

Mister Morph は journal event から次の 2 つへ単純な投影を行います。

- `memory/index.md`（長期記憶）
- `memory/YYYY-MM-DD/*.md`（短期記憶）
  - 短期記憶ファイルは Channel ごとに分離されます。例えば Telegram では別々のグループチャット間で記憶は共有されません。

投影とは、journal event を読み、LLM で要約し、対応する対象ファイルへ書き出すことです。

投影の記録点ファイルは `memory/projection_checkpoint.json` で、中身はおおよそ次のようになります。

```json
{
  "file": "events.000000000000000001.jsonl",
  "line": 18,
  "byte": 4096,
  "updated_at": "2026-02-28T06:30:12Z"
}
```

## 注入

次の設定が有効なとき、一部の記憶投影が prompt に注入されます。

- `memory.enabled = true`
- `memory.injection.enabled = true`

`memory.injection.max_items` は注入する項目数の上限を制御します。

## 備考

1. Memory 投影が壊れても journal から再構築できます。`memory/projection_checkpoint.json` を削除して Agent を継続実行すればよいです。
2. 本番環境では、memory を含む実行状態を維持するために `file_state_dir` を永続ストレージへ置いてください。
