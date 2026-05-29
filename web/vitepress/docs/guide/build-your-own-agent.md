---
title: Create Your Own AI Agent in 24 Lines
description: Use the integration package provided by Mister Morph to create your own AI agent.
---

# Embed an Agent into Your Program

`integration` is a wrapper around the agent capabilities provided by Mister Morph.

You can use it to embed AI agent capabilities into your own Go program with very little setup.

## Minimal Example

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
  cfg.AddPromptBlock("[[ Project Policy ]]\n- Answer in French by default.")
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

In this example:

- `cfg.AddPromptBlock(...)`
  Adds a custom prompt block.
- `cfg.Set(...)`
  Sets agent configuration. Any field from `config.yaml` can be set here. See [Config Fields](/guide/config-reference).
- `rt.RunTask(...)`
  Runs a task quickly and returns the result in the return value.
