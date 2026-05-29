---
title: LLM ルーティングポリシー
description: llm purpose ごとに profile、分流、エラー fallback を選ぶ。
---

# LLM ルーティングポリシー

Mister Morph には、次の問題を解決するための柔軟なルーティング設定があります。

1. purpose ごとに向いた llm 設定を使い分けたい。
2. llm リクエストを分流したい。
3. ある llm 設定が失敗したときに、バックアップ llm 設定へ fallback したい。

## LLM Profile

各 Profile は 1 つの LLM 設定です。トップレベルの `llm.*` 自体が default profile であり、`llm.profiles.<name>` で命名 profile を定義します。

注意点:

- 命名 profile はトップレベルの `llm.*` を継承し、変更したいフィールドだけを上書きします。
- `default` は予約名で、「トップレベルの `llm.*` をそのまま使う」という意味です。
- ユーザー向けの provider 選択には `inference_provider` を使います。`provider` は派生される protocol provider で、主に旧設定や高度な上書き用です。

下の例では、トップレベルのモデルは OpenAI の GPT-5.4 です。さらに 2 つの profile として GPT-4o mini と Claude Opus 4.6 を定義しています。

profile の名前を見るだけでも意図が分かります。GPT-4o mini は安いタスク向け、Claude Opus 4.6 はより深い思考向けです。

```yaml
llm:
  inference_provider: "openai"
  model: "gpt-5.4"
  api_key: "${OPENAI_API_KEY}"

  profiles:
    cheap:
      model: "gpt-4o-mini"
    reasoning:
      inference_provider: "anthropic"
      model: "claude-opus-4-6"
      api_key: "${CLAUDE_API_KEY}"
```

つまり、profile は**再利用可能な LLM 設定が何か**を定義し、その後の route、分流、fallback 機能がそれを使います。

## ルート

`llm.routes.*` の設定では、異なる llm purpose に対してどのモデル設定を使うかを定義します。

`main_loop` は Agent の実行そのものを担当しますが、それ以外の purpose は独立した llm 呼び出しです。単純な sub agent と考えても構いません。

### 現在サポートしている purpose

- `main_loop`: 主 Agent loop。
- `addressing`: グループチャットやチャンネルでの addressing 判定にのみ使う。
- `awareness`: 定期 awareness タスクにのみ使う。
- `heartbeat`: `awareness` の旧 alias。
- `think`: `/think <task>` コマンド prefix にのみ使う。この task だけ `reasoning_effort=xhigh` も適用します。
- `plan_create`: `plan_create` ツール内部の計画リクエストにのみ使う。
- `memory_draft`: memory 草稿整理にのみ使う。

下の例では、計画作成と `/think` では reasoning profile、つまり `claude-opus-4-6` を使い、グループチャットの addressing 判定では安い `gpt-4o-mini` を使います:

```yaml
llm:
  routes:
    plan_create: reasoning
    think: reasoning
    addressing: cheap
```

### ルートの分流

Mister Morph は LLM リクエストのトラフィック分流をサポートしています。分流表は `candidates` フィールドで定義します。

次の例では、`default_apple` と `default_banana` にトラフィックを分けています（あらかじめ `llm.profiles` で定義しておく必要があります）:

```yaml
llm:
  routes:
    main_loop:
      candidates:
        - profile: "default"
          weight: 1
        - profile: "default_apple"
          weight: 1
        - profile: "default_banana"
          weight: 1
```

ルール:

- `candidates.weight` は選択重みを決めます。
- 同じ Loop 内では 1 つの profile だけが使われ、途中で混在しません（`run_id` ベースで選ばれます）。
- 現在の llm が fallback 可能なエラーに当たった場合は、runtime はまず同じ route にある残りの candidate を試します。

### ルートの fallback

分流に加えて、Mister Morph は LLM リクエストのエラー fallback もサポートしています。例えば:

```yaml
llm:
  routes:
    plan_create:
      profile: "reasoning"
      fallback_profiles: [ "default" ]
```

現在の llm が fallback 可能なエラーに当たり、ほかの candidate も使えない場合は、`fallback_profiles` に並んだ設定を順番に試します。

## integration ではどう書くか

設定の考え方は同じで、YAML の代わりに `cfg.Set(...)` を使うだけです:

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.routes.plan_create", "reasoning")
cfg.Set("llm.routes.addressing", map[string]any{
  "profile": "cheap",
  "fallback_profiles": []string{"default"},
})
```
