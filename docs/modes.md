# Modes

This page collects the runtime entrypoints that are not covered in the top-level README.

The README focuses on:

- `mistermorph run`
- the desktop App wrapper

For the other runtime modes, use the docs below.

## Console

- Command: `mistermorph console serve`
- Purpose: local web UI backend plus in-process local runtime
- Docs: [console.md](./console.md)

## Telegram

- Command: `mistermorph telegram`
- Purpose: long-polling Telegram bot runtime
- Docs: [telegram.md](./telegram.md)

## Slack

- Command: `mistermorph slack`
- Purpose: Slack Socket Mode runtime
- Docs: [slack.md](./slack.md)

## LINE

- Command: `mistermorph line`
- Purpose: LINE webhook runtime
- Docs: [line.md](./line.md)

## Lark

- Command: `mistermorph lark`
- Purpose: Lark webhook runtime
- Docs: [lark.md](./lark.md)

## Mixin Messenger

- Command: `mistermorph mixin`
- Purpose: Mixin Blaze WebSocket bot runtime
- Docs: [mixin.md](./mixin.md)

## Note

Legacy standalone daemon mode (`mistermorph serve`) has been removed.
