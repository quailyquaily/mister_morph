---
title: 自分の AI Agent を作る：上級編
description: integration.Config、カスタムツール、実行モード、チャネル接続に絞って説明する。
---

# 自分の AI Agent を作る：上級編

## 設定レイヤ

`integration.Config` は `integration` パッケージにおける唯一の明示的な設定入口です。

ホストプログラム側で環境変数、設定ファイル、データベースなどを読み込み、最終的な値を `Config` に書き込んでから `integration.New(cfg)` に渡します。

### 例

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.inference_provider", "openai")
cfg.Set("llm.model", "gpt-5.4")
cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))
```

### デフォルト設定の上書き

Mister Morph 自体は CLI 引数、環境変数、`config.yaml` で設定します。
埋め込み側は好きな設定手段を使い、その最終値を `Set(key, value)` で上書きすれば十分です。`config.yaml` にある全フィールドをこの方法で設定できます。詳細は [設定フィールド](/ja/guide/config-reference) を参照してください。

### 機能フラグ

`Features.*` は機能の ON/OFF に使います。現在使える項目は次の通りです。

- `PlanTool`: runtime 補助ツール `plan_create` を登録するか。
- `Guard`: runtime に guard を注入するか。
- `Skills`: prompt 構築時に skills 読み込みを有効にするか。

### 組み込みツール

`BuiltinToolNames` で有効化する組み込みツールを指定します。空のままならすべて接続されます。

### カスタム prompt

`integration` 層で prompt を追加したい場合は `cfg.AddPromptBlock(...)` を使います。

これらの block は system prompt の末尾に自動で追加されます。

### インスペクタ

Mister Morph には、LLM の下位挙動を確認するための inspector 設定があります。

```go
cfg.Inspect.Prompt = true
cfg.Inspect.Request = true
cfg.Inspect.DumpDir = "./dump"
```

この設定で、prompt と request の詳細が dump ディレクトリに出力されます。

### LLM ルーティングポリシー

同様に、`llm.routes.*` を上書きして用途ごとに異なる LLM ルートを設定できます。

`llm.profiles` で複数 profile を定義しておけば、次のように特定の機能だけ特定 profile を使わせられます。

```go
// plan_create では reasoning という名前の profile を使う
cfg.Set("llm.routes.plan_create", "reasoning")
```

完全なルールと具体例は [LLM ルーティングポリシー](/ja/guide/llm-routing) に分けてあります。

### 実行時にメイン LLM Profile を切り替える

ホスト側でモデルピッカーを作りたい場合、`integration.Runtime` から現在の `main_loop` profile を確認し、実行時に上書きできます。

```go
profiles, err := rt.ListLLMProfiles()
if err != nil {
  panic(err)
}

selection, err := rt.GetLLMProfileSelection()
if err != nil {
  panic(err)
}

fmt.Println("mode:", selection.Mode)

if len(profiles) > 0 {
  if err := rt.SetLLMProfile(profiles[0].Name); err != nil {
    panic(err)
  }
}

rt.ResetLLMProfile()
```

ポイント:

- この状態は 1 つの `integration.Runtime` インスタンスに閉じます。
- `SetLLMProfile(...)` が上書きするのは `main_loop` だけです。
- `ResetLLMProfile()` は設定済みの `llm.routes.main_loop` ポリシーに戻します。
- `main_loop` が重み付き `candidates` の場合、`GetLLMProfileSelection()` は単一 profile を偽装せず、現在の戦略を返します。

### 設定の確定

設定が終わったら `integration.New(cfg)` を呼び、設定をスナップショット化して agent runtime を作成します。

## カスタムツール

`integration` が用意した組み込みツールを残したまま独自ツールを足したい場合は、Runtime メソッド `rt.NewRegistry()` でツールレジストリを作ります。

### カスタムツールの例

次の例では echo ツールを定義して agent に登録しています。

```go
package main

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "strings"

  "github.com/quailyquaily/mistermorph/agent"
  "github.com/quailyquaily/mistermorph/integration"
)

type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo_text" }

func (t *EchoTool) Description() string {
  return "Echoes input text as JSON."
}

func (t *EchoTool) ParameterSchema() string {
  return `{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Text to echo."}
  },
  "required": ["text"]
}`
}

func (t *EchoTool) Execute(_ context.Context, params map[string]any) (string, error) {
  text, _ := params["text"].(string)
  text = strings.TrimSpace(text)
  if text == "" {
    return "", fmt.Errorf("text is required")
  }
  b, _ := json.Marshal(map[string]any{"text": text})
  return string(b), nil
}

func main() {
  cfg := integration.DefaultConfig()
  cfg.Set("llm.inference_provider", "openai")
  cfg.Set("llm.model", "gpt-5.4")
  cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))

  rt := integration.New(cfg)
  reg := rt.NewRegistry()
  reg.Register(&EchoTool{})

  task := "Call tool echo_text with text 'hello from tool', then answer with that text."

  prepared, err := rt.NewRunEngineWithRegistry(context.Background(), task, reg)
  if err != nil {
    panic(err)
  }
  defer prepared.Cleanup()

  final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{Model: prepared.Model})
  if err != nil {
    panic(err)
  }

  fmt.Println("Agent:", final.Output)
}
```

## Runtime の実行方法

### Prepared Engine API

ライフサイクル制御、セッション再利用、明示的な cleanup、あるいは独自の上位スケジューリングをしたい場合に向いています。

- ライフサイクル制御: `Cleanup()` のタイミングを自分で決められる。
- 再利用性: 同じ `prepared.Engine` を複数回 `Run(...)` できる。
- 実行ごとの柔軟性: 各 `Run` に異なる `RunOptions` を渡せる。
- 編成しやすさ: `prepared.Model` と `Engine` を直接扱える。

```go
prepared, err := rt.NewRunEngine(context.Background(), task)
if err != nil {
  panic(err)
}
defer prepared.Cleanup()

final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{
  Model: prepared.Model,
})
```

### 省略 API

一回限りのタスクならこれで十分です。独自 registry、engine の再利用、明示的なライフサイクル管理が必要なら `PreparedRun` を使ってください。

```go
final, runCtx, err := rt.RunTask(context.Background(), task, agent.RunOptions{})
_ = final
_ = runCtx
_ = err
```

ホスト側で task id、run id、あとから観測に使う task journal が必要な場合は、`RunTaskWithOptions` を使います。

```go
result, err := rt.RunTaskWithOptions(context.Background(), task, integration.RunTaskOptions{
  Agent: agent.RunOptions{},
  PersistTask: true,
  TopicID: "support-thread-123", // 任意
  TraceID: "http-request-123",   // 任意。外部 correlation id
})
if err != nil {
  panic(err)
}

fmt.Println(result.TaskID)
fmt.Println(result.Final.Output)
```

`RunTaskWithOptions` は `PersistTask` が true の場合だけ task ライフサイクルを記録します。memory journal には書き込みません。

## Channels への接続

Mister Morph は Web UI だけでなく、Telegram や Slack のような channel も対話面として利用できます。

接続方法はかなりシンプルです。

### Telegram

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN")})
_ = tg
```

ホストプログラム側で Telegram の入出力イベントやエラーを受け取りたい場合は、`TelegramOptions.Hooks` に `TelegramHooks` を渡します。

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{
  BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN"),
  Hooks: integration.TelegramHooks{
    OnInbound: func(ev integration.TelegramInboundEvent) {
      fmt.Printf("telegram inbound: %+v\n", ev)
    },
    OnOutbound: func(ev integration.TelegramOutboundEvent) {
      fmt.Printf("telegram outbound: %+v\n", ev)
    },
    OnError: func(ev integration.TelegramErrorEvent) {
      fmt.Printf("telegram error: %+v\n", ev)
    },
  },
})
_ = tg
```

### Slack

```go
sl, _ := rt.NewSlackBot(integration.SlackOptions{
  BotToken: os.Getenv("MISTER_MORPH_SLACK_BOT_TOKEN"),
  AppToken: os.Getenv("MISTER_MORPH_SLACK_APP_TOKEN"),
})
_ = sl
```
