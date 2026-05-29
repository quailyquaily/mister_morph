---
title: 24行のコードで自分の AI Agent を作る
description: Mister Morph が提供する integration パッケージを使って、自分の AI Agent を作成する。
---

# Agent を自分のプログラムに組み込む

`integration` は、Mister Morph が提供する Agent 機能のラッパーです。

これを使えば、自分の Go プログラムに AI Agent の機能をかなり手軽に組み込めます。

## 最小サンプル

```go
package main

import (
  "context"
  "fmt"
  "github.com/quailyquaily/mistermorph/agent"
  "github.com/quailyquaily/mistermorph/integration"
)

func main() {
  cfg := integration.DefaultConfig()
  cfg.AddPromptBlock("[[ Project Policy ]]\n- 既定ではフランス語で答える。")
  cfg.Set("llm.inference_provider", "openai")
  cfg.Set("llm.model", "gpt-5.4")
  cfg.Set("llm.api_key", "YOUR_API_KEY_HERE")

  rt := integration.New(cfg)

  task := "Hello!"
  final, _, err := rt.RunTask(context.Background(), task, agent.RunOptions{})
  if err != nil {
    panic(err)
  }

  fmt.Println("Agent:", final.Output)
}
```

この例で使っているもの:

- `cfg.AddPromptBlock(...)`
  カスタム prompt を追加します。
- `cfg.Set(...)`
  Agent の設定を行います。`config.yaml` にある全フィールドを設定でき、詳細は [設定フィールド](/ja/guide/config-reference) を参照してください。
- `rt.RunTask(...)`
  タスクを手早く実行し、その結果を戻り値で受け取ります。
