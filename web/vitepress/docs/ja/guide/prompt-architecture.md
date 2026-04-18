---
title: Prompt 設計
description: Agent の Prompt の仕組みを説明します。
---

# Prompt 設計

Mister Morph の主 Loop では、Prompt の唯一の目的は Agent にとって妥当な状態を組み立てることです。

> skill、identity、soul、todo、memory は別々の仕組みに見えますが、本質的にはどれもその状態を維持するためのものです。多くは同じものに対する syntax sugar です。

Mister Morph では、これらは `agent/prompts/system.md` を骨格として組み立てられます。

## 主 Loop

### 静的 Prompt 骨格

このテンプレートは概ね次のような形です。

```md
## Persona
{{ identity }}

## Available Skills
{{ skills }}

## Reference Format
{{ 内部参照の約束 }}

## Additional Policies
{{ 追加の大きなポリシーブロック }}

## Response Format
{{ 出力形式の約束 }}

## Rules
{{ 組み込みルールと追加ルール }}
```

### 実行時 Prompt

具体的な run が始まると、runtime は現在の文脈をさらに足していきます。

よくある入力元は次の通りです。

- ローカル persona ファイルである `IDENTITY.md` と `SOUL.md`
- その run で有効になっている skill のメタ情報
- `SCRIPTS.md` のようなローカルスクリプト説明
- 追加のポリシーブロック
- memory summary

この層は現在の task、現在の channel、現在のローカル状態によって変わります。CLI、Telegram、Slack で使われる最終 prompt は完全に同じとは限りません。

### Prompt 以外のメッセージ編成

最終 system prompt の準備ができたあと、主 Agent はリクエスト内でメッセージ列も編成します。

実行時 metadata は、`mister_morph_meta` を外側 key に持つ user ロールの JSON envelope として注入されます。代表的な項目は、存在する場合の trigger / correlation 情報、`now_utc` / `now_local` / `now_local_weekday` のような runtime clock の値、そして `host_os` のようなホスト環境情報です。

順序は次のように理解できます。

```text
[system] 最終 system prompt
   ->
[user] 実行時 metadata
   ->
[history] 履歴メッセージ
   ->
[user] current message または raw task
```

## 独立 Prompt

Mister Morph には、主 Agent の完全な system prompt を先に組み立てず、場合によってはツール付きの多段 Loop にも入らず、より小さな専用 prompt を作って `llm.Chat` を 1 回だけ呼ぶ別系統の処理もあります。

この独立 Prompt は次のような用途で使われます。

- グループチャットにどう介入するかの判断
- タスク計画
- Memory 整理
- 一部の狭い意味判定や意味マッチング

### 主 Loop との関係

2 つの経路は次のように考えると分かりやすいです。

```text
主 Agent の主 Loop
  -> 完全な system prompt
  -> runtime metadata / history / current message
  -> 多段実行でき、ツールも呼べる

独立 llm.Chat
  -> 専用 system prompt / user prompt
  -> 単発呼び出し
  -> 1つの狭い問題だけを解く
```

例えば Agent がグループチャットにどう入るかを判断する時には、次のようなことを見ます。

- このメッセージは自分に向けられているか
- 今ここで割り込むべきか
- 絵文字のような軽い反応で足りるか

この判断は主 Loop より前に起きるので、主 system prompt 全体を使うよりも、より小さく専用の Prompt の方が適しています。
