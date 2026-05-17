---
title: TODO と Heartbeat
description: cron.yaml と HEARTBEAT.md が、現在のチャット外の作業を追跡する仕組み。
---

# TODO と Heartbeat

MisterMorph には 2 つの awareness 仕組みがあります。Heartbeat と TODO です。Heartbeat は単純な固定間隔の起動です。TODO は、一回だけ実行する作業や、決まったルールで繰り返す作業をより正確に指定できます。

## TODO

一回限り、または定期的に繰り返す作業は TODO に保存するのが適しています。

`cron.yaml` を直接編集することも、Console GUI で編集することもできます。Agent が TODO を追加または削除する必要がある場合は、`todo_update` ツールを使います。

各 TODO には、タイトル、時間ルール、タイムゾーン、内容があります。内容は実際に agent へ渡されるテキストで、`[John](tg:@john)` のような連絡先参照を含められます。

### 期限到来時の実行

Agent は `cron.yaml` を 1 分ごとに確認します。期限が来た TODO は、`behavior=cron` の awareness task として実行されます。

詳細:

- 一回限りの TODO は、成功すると削除されます。失敗またはタイムアウトした場合は残り、後で再試行されます。
- 繰り返し TODO はリストに残ります。

> 備考:
>
> 1. Agent が複数の実行時刻を逃した場合、復帰後に逃した TODO を 1 つずつ再実行することはありません。
> 2. 同じ TODO がすでにキューに入っている、または実行中の場合、その回の起動はスキップされます。

### 設定

TODO は `file_state_dir/cron.yaml` に保存されます。

`cron.yaml` の基本構造は次のとおりです。

```yaml
version: 1
tasks:
  - id: submit-report
    title: Submit report
    at: "2026-05-12 09:00"
    tz: "Asia/Tokyo"
    content: "Remind [John](tg:@john) to submit the report."

  - id: weekly-invoice-review
    title: Invoice review
    cron: "0 10 * * 1"
    tz: "UTC+8"
    content: "Review open invoices."
```

フィールド:

- `id`: 重複排除、更新、削除に使う安定した識別子。
- `title`: TODO のタイトル。
- `at`: 一回限りの TODO の実行時刻。形式は `YYYY-MM-DD HH:mm`。
- `cron`: 繰り返し TODO の 5 フィールド cron 式。
- `tz`: IANA タイムゾーンまたは固定 UTC オフセット。例: `Asia/Tokyo`、`UTC+8`。
- `content`: 期限到来時に agent へ渡すタスク内容。
- `chat_id`: 任意のチャネル文脈。metadata としてのみ保存され、ルーティングには使われません。

`at` と `cron` は必ずどちらか一方だけを指定します。`at` は一回限りの TODO、`cron` は繰り返し TODO に使います。

## Heartbeat

設定で Heartbeat が有効な場合、heartbeat tick ごとに次の順で処理されます。

1. Agent は `HEARTBEAT.md` を読みます。
2. `HEARTBEAT.md` が空でなければ、その内容が heartbeat task になります。
3. Agent はその task を処理します。

`HEARTBEAT.md` が空なら、agent task は開始されません。

Heartbeat は TODO を読みません。TODO は cron サービスが `cron.yaml` を読んで独立に起動します。

### HEARTBEAT.md

`HEARTBEAT.md` は heartbeat ごとの固定指示です。Agent が heartbeat ごとに何を確認するかを書きます。

向いている内容:

- 期限が来た後続対応を探す。
- 定期的に確認するファイルを見る。

TODO を更新するツールは [`todo_update`](/ja/guide/built-in-tools#todo_update) を参照してください。状態ディレクトリの場所は [ファイルシステムのルート](/ja/guide/filesystem-roots) を参照してください。
