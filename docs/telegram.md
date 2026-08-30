# Telegram Runtime: Trigger, Reaction, and Text Policy

This document describes how Telegram runtime handles one inbound message.

When an authorized sender is observed, Morph asynchronously reads that user's profile photo with `getUserProfilePhotos` and `getFile`. Morph stores the numeric sender ID as `tg_user_id`, so active Contacts can also be prewarmed when the Telegram runtime starts. The downloaded image is stored in the local Contact avatar cache for seven days. This does not delay message processing, and Bot Token file URLs are never written to Contact YAML or returned to Console.

Code areas:
- `internal/channelruntime/telegram/runtime.go`
- `internal/channelruntime/telegram/trigger.go`
- `internal/channelruntime/telegram/runtime_task.go`
- `tools/telegram/react_tool.go`
- `internal/grouptrigger/decision.go`
- `internal/grouptrigger/addressing_prompts.go`

## 1) Mental Model

There are three separate decisions:

1. Trigger decision: should this group message enter the main agent run?
2. Reaction execution: did `message_react` actually run successfully?
3. Text publish decision: should runtime publish `final.output` as text?

These are related, but not the same switch.

### 1.1) Group Delivery Prerequisite

Disabling Group Privacy in `@BotFather` does not update groups that the bot has
already joined. Remove the bot from each existing group and add it again after
disabling privacy. Alternatively, make the bot a group administrator. Telegram
documents this requirement in [Privacy Mode](https://core.telegram.org/bots/features#privacy-mode).

This commonly appears as private messages working while group `@mentions`
produce no Morph log at all. No restart is required. After adding the bot again:

1. Send `/id@bot_username` in the group.
2. Add the returned group ID to `telegram.allowed_chat_ids`, or leave the list
   empty to allow every chat.
3. Send `@bot_username ping` from a human account.

## 2) End-to-End Flow

```text
Inbound Telegram message (group path shown)
  -> prefilter (reply/mention guard)
  -> addressing LLM + grouptrigger.Decide(...)
     -> may call message_react in pre-run
     -> decide trigger yes/no
  -> if triggered: runTelegramTask (main run)
     -> main run may call message_react again
     -> final.is_lightweight decides whether text is publishable
```

### 2.1 Runtime Delivery Path (ASCII)

```text
                           +-------------------------+
                           | Telegram inbound update |
                           +------------+------------+
                                        |
                           +------------v------------+
                           | prefilter + grouptrigger|
                           +------+-------------------+
                                  |
                     +------------+------------+
                     | trigger no             | trigger yes
                     |                        |
             +-------v------+         +-------v------------------+
             | ignore/end   |         | runTelegramTask          |
             +--------------+         | agent.Engine + OnStream  |
                                      +-----+--------------------+
                                            |
                        +-------------------+-------------------+
                        |                                       |
              +---------v-----------+                 +---------v-----------+
              | main run output     |                 | final output        |
              +---------+-----------+                 +---------+-----------+
                        |                                       |
                        +-------------------+-------------------+
                                            |
                                +-----------v-------------------+
                                | publish outbound bus          |
                                | final text                    |
                                +-----------+-------------------+
                                            |
                                +-----------v-------------+
                                | telegram delivery       |
                                | adapter -> sendMessage  |
                                +-------------------------+
```

### 2.2 Pre Filter: `shouldSkipGroupReplyWithoutBodyMention`

Before addressing LLM, runtime applies a prefilter in group chats:

- function: `shouldSkipGroupReplyWithoutBodyMention`
- file: `internal/channelruntime/telegram/trigger.go`

Current skip rule:

- message is from a human (not bot)
- message has `ReplyTo` (looks like a reply)
- reply target is not this bot
- message body does not mention this bot (`groupBodyMentionReason(...) == false`)

If all match, runtime skips this message before `groupTriggerDecision(...)`.

This prefilter is exactly where topic/thread reply-shape edge cases can affect triggering in supergroups.

Examples:

- Will be skipped by prefilter:
  - Message is a reply to another human message.
  - Body has no `@bot_username`.
  - Example: replying to Alice with "ok got it" (no bot mention).

- Will NOT be skipped by this prefilter:
  - Message is not a reply (`ReplyTo == nil`), even if body mentions someone else.
  - Example: `@ballcatcat I do not have access to many EC2 instances in cp; please grant me a few.`
  - This case continues into `groupTriggerDecision(...)`, and in `smart`/`talkative` mode may still trigger by LLM judgment.

## 3) Trigger Stage (Pre-Run)

### 3.1 Modes

`group_trigger_mode` behavior:

- `strict`: only explicit triggers (reply/mention/command path) run.
- `smart`: requires `addressed=true` and `confidence >= threshold`.
- `talkative`: requires `wanna_interject=true` and `interject > threshold`.

### 3.2 Explicit signals

Explicit signals include:

- reply to bot
- mention entity
- body `@bot_username` mention

If explicit match is true, trigger is accepted without LLM addressing threshold checks.

### 3.3 Pre-run `message_react`

Addressing stage can now receive `message_react` as a callable tool.
Runtime creates a temporary `ReactTool` bound to the current inbound `chat_id` and `message_id`.

If addressing LLM calls it successfully:

- Telegram reaction is sent immediately.
- Runtime logs `telegram_group_addressing_reaction_applied`.

Important:

- Pre-run reaction does not terminate the pipeline.
- Trigger decision still continues.
- If trigger is accepted, main run still starts.

### 3.4 Pre-run Lightweight Fallback Rule (Current Implementation)

Addressing pre-run (in shared `grouptrigger.DecideViaLLM(...)`) tracks whether `message_react` has been executed successfully in the tool-call loop.

After parsing addressing JSON, runtime applies this fallback:

- only when `is_lightweight == true`
- and `reaction` is non-empty
- and no successful pre-run `message_react` call happened yet

then runtime executes one extra `message_react` with that `reaction` emoji.

Important edge case:

- if model returns `is_lightweight == true` but omits `reaction` (or returns empty string), pre-run does **not** execute `message_react` fallback.
- with `tool_choice=auto`, model may also choose not to emit a tool call.
- so "lightweight=true but no pre-run reaction" is possible in current behavior.

### 3.5 Trigger output metadata

`Decision.Addressing` contains fields like:

- `addressed`
- `confidence`
- `wanna_interject`
- `interject`
- `impulse`
- `is_lightweight`
- `reason`

`Decision.Addressing.is_lightweight` is metadata from addressing classification.
It is not the final text publish switch.

## 4) Main Run (Generation Stage)

Inside `runTelegramTask(...)`, runtime builds a fresh tool registry and registers `message_react` again (if API and `message_id` are available).

Telegram draft streaming is currently not enabled in runtime.

Reason:

- Telegram Bot API `sendMessageDraft` behaves like preview UI rather than a same-bubble final message primitive.
- In practice this caused either disappearing replies or unacceptable duplicate-shadow windows in private chat.
- Current runtime therefore keeps Telegram on `typing` + canonical final `sendMessage`.

After `agent.Engine.Run(...)`, runtime reads `reactTool.LastReaction()`:

- `nil`: no successful main-run reaction
- non-`nil`: successful main-run reaction, log `message_reaction_applied`, append outbound reaction history

This `LastReaction()` reflects only the main-run `ReactTool` instance.

## 5) Text Publish Rule

Text publishing is controlled only by `final.is_lightweight`:

- `final.is_lightweight == true` -> do not publish text
- `final.is_lightweight == false` -> publish text

Runtime function:

- `shouldPublishTelegramText(final) == !final.IsLightweight`

Pre-run reaction does not change this rule.

Text delivery path when `final.is_lightweight == false`:

- runtime publishes the final text through the normal outbound bus text path (delivery adapter -> `sendMessage`)
- Telegram draft streaming is intentionally disabled in the current Bot API implementation
- private chats still get typing/status feedback through the normal runtime ticker, but not inline draft preview

## 6) Pre-Run vs Main-Run Reaction Interaction

Pre-run and main-run use different `ReactTool` instances.

Result:

- pre-run may react once
- main-run may react again
- same inbound message may receive two reaction API calls

This is expected with current design.

## 7) Logging Signals

Useful logs for debugging:

- `telegram_group_ignored_reply_without_at_mention`
- `telegram_group_ignored`
- `telegram_group_trigger`
- `telegram_group_addressing_reaction_applied`
- `message_reaction_applied`

## 8) Quick Behavior Matrix

- pre-run reacted, main-run no reaction, `final.is_lightweight=false`: reaction + text
- pre-run reacted, main-run reacted, `final.is_lightweight=false`: possibly two reactions + text
- pre-run reacted, main-run no reaction, `final.is_lightweight=true`: reaction only
- pre-run no reaction, main-run reacted, `final.is_lightweight=true`: reaction only
- pre-run no reaction, main-run no reaction, `addressing.is_lightweight=true`: possible when addressing JSON has empty/missing `reaction` and no pre-run tool call

## 9) Telegram Bus Boundary

- Telegram outbound text remains canonical bus-driven delivery.
- bus carries outbound events in Telegram runtime:
  - plan progress
  - error/file-download-error replies
  - final text publish path
- draft streaming is not enabled in the current runtime.
