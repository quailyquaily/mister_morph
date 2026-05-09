---
title: TODO と Heartbeat
description: TODO ファイルと HEARTBEAT.md が、現在のチャット外の作業を追跡する仕組み。
---

# TODO と Heartbeat

Heartbeat は awareness runtime のタイマー動作です。各 heartbeat run は `HEARTBEAT.md` から新しい runtime task を作り、チャット履歴は含みません。

`POST /poke` は同じ awareness runtime の再入防止状態を使いますが、別の動作です。Poke は `HEARTBEAT.md` を読みません。リクエスト body が task の本文になります。空の body は `400 Bad Request` になります。

## Heartbeat の流れ

heartbeat tick ごとに、次の順で処理されます。

1. Runtime は `HEARTBEAT.md` を読みます。
2. `HEARTBEAT.md` が空でなければ、その内容が heartbeat task になります。
3. heartbeat task に内容があれば、agent は通常のツールで処理します。

`HEARTBEAT.md` が空なら、agent task は開始されません。

Heartbeat は `TODO.md` を読まず、`TODO.RECUR.md` も展開しません。

## Poke の流れ

認証済みの `POST /poke` は次の順で処理されます。

1. Server はリクエスト body の短いテキスト preview を読みます。
2. body が空、またはテキストとして使えない場合は `400 Bad Request` を返します。
3. Poke body が task の本文になります。
4. Task にはチャット履歴も `HEARTBEAT.md` も含まれません。

## HEARTBEAT.md

`HEARTBEAT.md` は heartbeat ごとの固定指示です。具体的な一回限りのユーザー依頼ではなく、agent が何を確認するかを書きます。

向いている内容:

- 期限が来た後続対応を探す。
- 定期的に確認するファイルを見る。
- heartbeat の指示で明示的に読むファイルやサービス状態に基づいて通知する。

一回限りのタスクを直接 `HEARTBEAT.md` に書くのは避けます。一回限りのタスクは `TODO.md` に、繰り返すタスクは `TODO.RECUR.md` に書きます。

## TODO の流れ

TODO ファイルは具体的な作業を保存します。`todo_update` ツールは、TODO workflow を含む通常の agent task で TODO 記録の追加と完了を扱います。Heartbeat と poke task には TODO workflow は自動では含まれません。

TODO ファイルは 3 つあります。

### TODO.md

`TODO.md` は一回だけ実行すればよい作業を保存します。

```text
- [ ] [Created](2026-05-01 12:41), [ChatID](tg:-100123) | Remind [John](tg:@john) to submit report.
```

一回限りのリマインダーやタスクには `TODO.md` を使います。

### TODO.DONE.md

`TODO.DONE.md` は完了した一回限りの TODO を保存します。`todo_update` が `TODO.md` の項目を完了すると、その記録はここへ移動します。

繰り返し TODO は `TODO.DONE.md` へ移動しません。

### TODO.RECUR.md

`TODO.RECUR.md` は繰り返しルールを保存します。

```text
- [ ] [Next](2026-05-07 15:00), [Repeat](weekly), [TZ](Asia/Tokyo) | Play tennis.
- [ ] [Next](2026-05-02 09:00), [Repeat](every 6 hours) | Check the report queue.
```

対応している `Repeat` 値:

- `daily`
- `weekly`
- `every N days`
- `every N hours`

`TZ` は任意です。省略すると runtime のローカルタイムゾーンが使われます。

繰り返し記録は `TODO.RECUR.md` に残ります。Heartbeat は現在、期限が来た繰り返し記録を `TODO.md` にコピーしません。

## どのファイルを使うか

| 必要なこと | ファイル |
|---|---|
| heartbeat ごとに agent が確認することを書く | `HEARTBEAT.md` |
| 一回だけ実行する | `TODO.md` |
| 完了した一回限りの TODO を残す | `TODO.DONE.md` |
| 繰り返し実行する | `TODO.RECUR.md` |

TODO ファイルを更新するツールは [`todo_update`](/ja/guide/built-in-tools#todo_update) を参照してください。状態ディレクトリの場所は [ファイルシステムのルート](/ja/guide/filesystem-roots) を参照してください。
