---
title: 実行モード総覧
description: Mister Morph がサポートする実行モードを確認する。
---

# 実行モード総覧

## 単発タスク

コマンドラインから Mister Morph を 1 回だけ呼び出してタスクを実行したい場合は、このモードを使います。

```bash
mistermorph run --task "..."
```

## Chat CLI

ターミナルで対話セッションを継続したい場合は、`chat` コマンドを使います。

```bash
mistermorph chat
```

## Console

機能の揃った Web UI を提供します。Agent と対話できるだけでなく、ほかの Mister Morph インスタンスを監視する用途にも使えます。

```bash
mistermorph console serve
```

## Telegram Bot

Telegram channel に接続した standalone runtime を起動し、Telegram 上で対話できます。

```bash
mistermorph telegram --log-level info
```

## Slack Bot

Telegram モードとほぼ同じですが、対話先が Slack になります。

```bash
mistermorph slack --log-level info
```

## Mixin Messenger Bot

Blaze WebSocket を使う Mixin Messenger runtime を単独で起動します。

```bash
mistermorph mixin --log-level info
```

`mixin.keystore_file` には Mixin Developer Dashboard で生成した Ed25519 keystore を指定します。設定方法は [Mixin Messenger ドキュメント](https://github.com/quailyquaily/mistermorph/blob/master/docs/mixin.md)を参照してください。
