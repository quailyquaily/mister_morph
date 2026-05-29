---
title: "24 行代码创建自己的 AI Agent"
description: "使用 Mister Morph 提供的 integration 包来创建自己的 AI Agent"
---

# 嵌入 Agent 到自己的程序

`integration` 是 Mister Murphy 提供的 Agent 能力的封装。

你可以很方便地使用它，把 AI Agent 的能力嵌入到自己的 Golang 程序中。

## 最小示例

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
	cfg.AddPromptBlock("[[ Project Policy ]]\n- 默认用法文回答。")
	cfg.Set("llm.inference_provider", "openai")
	cfg.Set("llm.model", "gpt-5.4")
	cfg.Set("llm.api_key", "YOUR_API_KEY_HERE")
  
	rt := integration.New(cfg)

	task := "Hello!"
	final, _, err := rt.RunTask(context.Background(), task, agent.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println("Agent:", final.Output)
}
```

其中，

- `cfg.AddPromptBlock` 用于添加自定义 prompt。
- `cfg.Set` 用于设置 Agent 配置，所有 `config.yaml` 中的字段都可以设置，可参考[配置字段](/zh/guide/config-reference)。
- `rt.RunTask` 是用于快速运行任务的方法，并且将结果放入返回值。
