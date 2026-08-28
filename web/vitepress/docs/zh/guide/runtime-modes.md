---
title: 运行模式总览
description: 看看 Mister Morph 支持的运行模式。
---

# 运行模式总览

## 一次性任务

如果只需要在命令行调用 Mister Morph 完成一次性任务，可以使用这个模式。

```bash
mistermorph run --task "..."
```

## Chat CLI

如果你想在终端里保持一个持续的交互式会话，可以使用 `chat` 命令。

```bash
mistermorph chat
```

## Console

提供一个功能完备的 Web UI。除了可以跟 Agent 进行交互以外，还可以用来监控不同的其他 Mister Morph 实例。

```bash
mistermorph console serve
```

## Telegram Bot

单独运行连接到 Telegram channel，并在 Telegram 里提供交互。

```bash
mistermorph telegram --log-level info
```

## Slack Bot

和 Telegram 模式类似，只不过在 Slack 里边提供交互。

```bash
mistermorph slack --log-level info
```

## Mixin Messenger Bot

通过 Blaze WebSocket 单独运行 Mixin Messenger runtime：

```bash
mistermorph mixin --log-level info
```

`mixin.keystore_file` 指向 Mixin Developer Dashboard 生成的 Ed25519 keystore。配置方法见 [Mixin Messenger 文档](https://github.com/quailyquaily/mistermorph/blob/master/docs/mixin.md)。
