# Modes

This page collects the runtime entrypoints that are not covered in the top-level README.

The README focuses on:

- `morph run`
- the desktop App wrapper

For the other runtime modes, use the docs below.

## Console

- Command: `morph console serve`
- Purpose: local web UI backend plus in-process local runtime
- Docs: [console.md](./console.md)

## Telegram

- Command: `morph telegram`
- Purpose: long-polling Telegram bot runtime
- Docs: [telegram.md](./telegram.md)

## Slack

- Command: `morph slack`
- Purpose: Slack Socket Mode runtime
- Docs: [slack.md](./slack.md)

## LINE

- Command: `morph line`
- Purpose: LINE webhook runtime
- Docs: [line.md](./line.md)

## Lark

- Command: `morph lark`
- Purpose: Lark webhook runtime
- Docs: [lark.md](./lark.md)

## Mixin Messenger

- Command: `morph mixin`
- Purpose: Mixin Blaze WebSocket bot runtime
- Docs: [mixin.md](./mixin.md)

## Note

Legacy standalone daemon mode (`morph serve`) has been removed.
