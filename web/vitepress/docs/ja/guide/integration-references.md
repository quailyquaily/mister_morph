---
title: Integration API
description: integration パッケージの公開関数、メソッド、構造体フィールド、引数、戻り値、用途を一覧化する。
---

# Integration API

このページでは `github.com/quailyquaily/mistermorph/integration` の公開 API をまとめます。

`integration.Config` の設定方法、`PreparedRun` の使い方、Telegram / Slack 接続を先に見たい場合は、[自分の AI Agent を作る：上級編](/ja/guide/build-your-own-agent-advanced) を参照してください。

## トップレベル関数

### `ApplyViperDefaults(v *viper.Viper)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `v *viper.Viper`：デフォルト値を書き込む viper インスタンス。`nil` の場合は `viper.GetViper()` にフォールバック |
| 戻り値 | なし |
| 説明 | `integration` とファーストパーティ runtime で共有しているデフォルト設定を viper に書き込みます。ホストアプリ側も viper で設定管理している場合にだけ必要です。 |

### `DefaultFeatures() Features`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `integration.Features` |
| 説明 | デフォルトの feature フラグを返します。現在は `PlanTool`、`Guard`、`Skills` が有効です。 |

### `DefaultConfig() Config`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `integration.Config` |
| 説明 | デフォルト設定を返します。`Overrides` は空 map、`Features` は `DefaultFeatures()`、`Inspect` はゼロ値です。 |

### `New(cfg Config) *Runtime`

| 項目 | 内容 |
| --- | --- |
| 引数 | `cfg integration.Config`：ホストプログラムが組み立てた明示設定 |
| 戻り値 | `*integration.Runtime` |
| 説明 | 再利用可能な runtime を構築し、生成時にデフォルト値と override をスナップショット化します。 |

## 設定関連の型

### `type Features struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `PlanTool` | `bool` | `plan_create` 関連の runtime 補助ツールを登録するか。 |
| `Guard` | `bool` | runtime 内で guard を有効にするか。 |
| `Skills` | `bool` | prompt 構築時に skills 読み込みを有効にするか。 |

### `type InspectOptions struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Prompt` | `bool` | prompt をファイル出力するか。 |
| `Request` | `bool` | request / response をファイル出力するか。 |
| `DumpDir` | `string` | 出力先ディレクトリ。 |
| `Mode` | `string` | 出力を区別する inspect モード名。 |
| `TimestampFormat` | `string` | ファイル名に使う時刻フォーマット。 |

### `type Config struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Overrides` | `map[string]any` | 最終 override を viper key で保持する map。優先度は最上位。 |
| `Features` | `integration.Features` | どの runtime 機能を接続するかを制御。 |
| `PromptBlocks` | `[]string` | system prompt の `Additional Policies` に追加される静的 block。 |
| `BuiltinToolNames` | `[]string` | 組み込みツールの allowlist。空なら全て有効。 |
| `Inspect` | `integration.InspectOptions` | prompt / request 出力関連のデバッグ設定。 |

### `(*Config).Set(key string, value any)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `key string`：viper の設定キー。`value any`：上書き値 |
| 戻り値 | なし |
| 説明 | `Overrides` に単一の override を書き込みます。空 key は無視されます。 |

### `(*Config).AddPromptBlock(content string)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `content string`：追加する prompt block |
| 戻り値 | なし |
| 説明 | 静的 prompt block を 1 つ追加します。空白文字だけの文字列は無視され、内容は順番通り runtime に適用されます。 |

## Runtime と実行

### `type Runtime struct`

`Runtime` はサードパーティ埋め込みの主入口です。フィールドは公開せず、主にメソッド経由で使います。

### `type PreparedRun struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Engine` | `*agent.Engine` | 実行準備済みの engine。 |
| `Model` | `string` | 現在のメイン route から解決された model 名。 |
| `Cleanup` | `func() error` | inspect 出力や MCP 接続などの一時リソースを解放する関数。 |

### `type RunTaskOptions struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Agent` | `agent.RunOptions` | `Engine.Run` に渡す agent 実行オプション。 |
| `LLMProfile` | `string` | この task だけに適用する任意の LLM profile。空なら runtime 設定の route に従い、runtime の共有選択は変更しない。 |
| `TaskID` | `string` | 任意の永続化 task id。空なら `Agent.Meta["task_id"]`、それも空なら自動生成。 |
| `TopicID` | `string` | 任意の topic id。空なら `Agent.Meta["topic_id"]`、それも空なら空のまま。 |
| `TraceID` | `string` | 任意の外部 trace / correlation id。空なら `Agent.Meta["trace_id"]`、それも空なら空のまま。 |
| `PersistTask` | `bool` | one-shot の queued/running/done/failed ライフサイクルを task journal に書く。 |

### `type RunTaskResult struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Final` | `*agent.Final` | agent の最終出力。 |
| `Context` | `*agent.Context` | agent 実行 context。 |
| `TaskID` | `string` | この one-shot run で使った task id。 |
| `RunID` | `string` | logs と LLM stats で使う run id。デフォルトは `TaskID`。 |
| `TopicID` | `string` | 指定された場合に metadata と task 永続化へ入る topic id。 |
| `TraceID` | `string` | 指定された場合の外部 trace id。 |

### `type LLMProfile struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Name` | `string` | profile 名。 |
| `InferenceProvider` | `string` | 分かっている場合のユーザー向け inference provider。 |
| `Provider` | `string` | 解決後の provider 名。 |
| `ModelName` | `string` | 解決後の model 名。 |
| `APIBase` | `string` | 存在する場合の API base。 |

### `type LLMProfileCandidate struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `LLMProfile` | `integration.LLMProfile` | 候補 profile の情報。 |
| `Weight` | `int` | `llm.routes.main_loop.candidates` に設定された重み。 |

### `type LLMProfileSelection struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Mode` | `string` | `auto` または `manual`。 |
| `ManualProfile` | `string` | `Mode` が `manual` のときの上書き対象 profile。 |
| `RouteType` | `string` | `profile` または `candidates`。 |
| `Current` | `*integration.LLMProfile` | 単一 profile 戦略のときの現在 profile。 |
| `Candidates` | `[]integration.LLMProfileCandidate` | `candidates` 戦略のときの重み付き候補一覧。 |
| `FallbackProfiles` | `[]integration.LLMProfile` | route から解決された fallback profiles。 |

### `(*Runtime).NewRegistry() *tools.Registry`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `*tools.Registry` |
| 説明 | 現在の runtime スナップショットからデフォルト registry を構築します。カスタムツールを足す場合は通常ここから始めます。 |

### `(*Runtime).NewRunEngine(ctx context.Context, task string) (*PreparedRun, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `ctx context.Context`：準備フェーズの context。`task string`：現在の task テキスト |
| 戻り値 | `*integration.PreparedRun`、`error` |
| 説明 | デフォルト registry で再利用可能な engine を準備します。 |

### `(*Runtime).NewRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry) (*PreparedRun, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `ctx context.Context`：準備フェーズの context。`task string`：現在の task テキスト。`baseReg *tools.Registry`：基底 registry |
| 戻り値 | `*integration.PreparedRun`、`error` |
| 説明 | 渡した registry を基底に engine を準備します。組み込みツールを残したままカスタムツールを足したい場合は、先に `rt.NewRegistry()` を呼んでから追加登録するのが普通です。 |

### `(*Runtime).RunTask(ctx context.Context, task string, opts agent.RunOptions) (*agent.Final, *agent.Context, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `ctx context.Context`：実行 context。`task string`：task テキスト。`opts agent.RunOptions`：今回の実行オプション |
| 戻り値 | `*agent.Final`、`*agent.Context`、`error` |
| 説明 | 一回限りの便利 API です。内部で一時的に engine を準備し、実行後に自動で `Cleanup()` します。 |

### `(*Runtime).RunTaskWithOptions(ctx context.Context, task string, opts RunTaskOptions) (RunTaskResult, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `ctx context.Context`：実行 context。`task string`：task テキスト。`opts integration.RunTaskOptions`：one-shot 実行と永続化のオプション |
| 戻り値 | `integration.RunTaskResult`、`error` |
| 説明 | 明示的な実行 id と任意の task journal 永続化を持つ one-shot API。`task_id` / `run_id` を注入し、`trace_id` と `topic_id` は指定された場合だけ使います。memory journal には書き込みません。 |

### `(*Runtime).GetLLMProfileSelection() (LLMProfileSelection, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `integration.LLMProfileSelection`、`error` |
| 説明 | この runtime インスタンスにおける現在の `main_loop` selection を返します。`candidates` 戦略では単一 profile を返すのではなく、重み付き戦略そのものを返します。 |

### `(*Runtime).ListLLMProfiles() ([]LLMProfile, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `[]integration.LLMProfile`、`error` |
| 説明 | 設定済みの LLM profiles を一覧で返します。各要素には `name`、任意の `inference_provider`、解決後の `provider`、`model_name`、必要に応じて `api_base` が含まれます。 |

### `(*Runtime).SetLLMProfile(profileName string) error`

| 項目 | 内容 |
| --- | --- |
| 引数 | `profileName string`：`main_loop` に強制適用する profile 名 |
| 戻り値 | `error` |
| 説明 | この runtime インスタンスを `manual` モードに切り替え、`main_loop` だけを上書きします。`plan_create` など他の purpose はそのままです。 |

### `(*Runtime).ResetLLMProfile()`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | なし |
| 説明 | この runtime インスタンスの `main_loop` 手動 override を解除し、設定済みの route policy に戻します。 |

### `(*Runtime).RequestTimeout() time.Duration`

| 項目 | 内容 |
| --- | --- |
| 引数 | なし |
| 戻り値 | `time.Duration` |
| 説明 | 現在の runtime スナップショットから解決された LLM request timeout を返します。 |

## Channel Runner

### `type BotRunner interface`

| メソッド | 引数 | 戻り値 | 説明 |
| --- | --- | --- | --- |
| `Run` | `ctx context.Context` | `error` | 長寿命の channel bot を起動する。 |
| `Close` | なし | `error` | runner を能動的に閉じる。 |

### `type TelegramOptions struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `BotToken` | `string` | Telegram bot token。 |
| `AllowedChatIDs` | `[]int64` | 許可する chat の allowlist。 |
| `PollTimeout` | `time.Duration` | Telegram polling timeout。 |
| `TaskTimeout` | `time.Duration` | 1 task あたりの実行 timeout。 |
| `MaxConcurrency` | `int` | 最大同時実行数。 |
| `GroupTriggerMode` | `string` | グループ trigger mode。 |
| `AddressingConfidenceThreshold` | `float64` | addressing 判定の閾値。 |
| `AddressingInterjectThreshold` | `float64` | interject 判定の閾値。 |
| `Hooks` | `integration.TelegramHooks` | イベント callback。 |

### `type SlackOptions struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `BotToken` | `string` | Slack bot token。 |
| `AppToken` | `string` | Slack app token。 |
| `AllowedTeamIDs` | `[]string` | 許可する team の allowlist。 |
| `AllowedChannelIDs` | `[]string` | 許可する channel の allowlist。 |
| `TaskTimeout` | `time.Duration` | 1 task あたりの実行 timeout。 |
| `MaxConcurrency` | `int` | 最大同時実行数。 |
| `GroupTriggerMode` | `string` | グループ trigger mode。 |
| `AddressingConfidenceThreshold` | `float64` | addressing 判定の閾値。 |
| `AddressingInterjectThreshold` | `float64` | interject 判定の閾値。 |
| `Hooks` | `integration.SlackHooks` | イベント callback。 |

### `type TelegramHooks struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `OnInbound` | `func(TelegramInboundEvent)` | 入站イベント受信時に呼ばれる。 |
| `OnOutbound` | `func(TelegramOutboundEvent)` | 出站イベント送信時に呼ばれる。 |
| `OnError` | `func(TelegramErrorEvent)` | runtime エラーイベント callback。 |

### `type SlackHooks struct`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `OnInbound` | `func(SlackInboundEvent)` | 入站イベント受信時に呼ばれる。 |
| `OnOutbound` | `func(SlackOutboundEvent)` | 出站イベント送信時に呼ばれる。 |
| `OnError` | `func(SlackErrorEvent)` | runtime エラーイベント callback。 |

### `(*Runtime).NewTelegramBot(opts TelegramOptions) (BotRunner, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `opts integration.TelegramOptions` |
| 戻り値 | `integration.BotRunner`、`error` |
| 説明 | Telegram runner を構築します。`BotToken` が空なら即座に error を返します。 |

### `(*Runtime).NewSlackBot(opts SlackOptions) (BotRunner, error)`

| 項目 | 内容 |
| --- | --- |
| 引数 | `opts integration.SlackOptions` |
| 戻り値 | `integration.BotRunner`、`error` |
| 説明 | Slack runner を構築します。`BotToken` または `AppToken` が空なら即座に error を返します。 |

## イベント alias 型

これらの公開 alias 型は主に hook の関数シグネチャで使います。

- `TelegramInboundEvent`
- `TelegramOutboundEvent`
- `TelegramErrorEvent`
- `SlackInboundEvent`
- `SlackOutboundEvent`
- `SlackErrorEvent`

業務ロジック側では、通常は `Hooks` の中でこれらを受け取れば十分で、下位 runtime を直接触る必要はありません。
