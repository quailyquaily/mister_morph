---
date: 2026-07-15
title: 上下文 checkpoint 与自动压缩
status: draft
---

# 上下文 checkpoint 与自动压缩

## 1) 目标

当 agent 主循环的输入达到阈值时，将较早的 messages 压缩为一条新的 checkpoint message。

这是一次 message 替换：

```text
压缩前：FixedMessages + MessagesToCompact + RetainedTail
压缩后：FixedMessages + CheckpointMessage + RetainedTail
```

只有新 checkpoint 生成、验证并持久化成功后，`MessagesToCompact` 才会被删除。

本设计需要满足：

1. 达到上下文占用阈值时触发压缩。
2. checkpoint 是所有参与压缩 messages 的语义替代物。
3. 压缩成功后，删除所有参与压缩的原始 messages。
4. checkpoint 至少保留用户意图，相关文件、目录和 URL，当前进度，以及阶段性结果。
5. 不拆开 assistant tool call 与其对应的 tool results。
6. 压缩 usage 和 cost 计入当次 run。
7. checkpoint 在下一轮仍然生效，已被压缩的外部历史不再重复注入。
8. profile 切换后立即使用新 route 的 model 和 context window。

## 2) 不改变的现有语义

### 2.1 `max_token_budget`

顶层 `max_token_budget` 继续表示一次 agent run 中所有 LLM 请求的累计 token 上限。它用于限制成本和循环长度，不表示单次请求的 context window。

压缩请求也是 LLM 请求，必须计入 `max_token_budget`。

### 2.2 context window

模型窗口仍来自：

1. 当前已解析 route 的 `context_window_tokens`。
2. 未显式配置时，查询现有 `llm/model_context_windows.yaml`。
3. 两者都未知时，不猜测窗口大小。

### 2.3 `/ctx`

`/ctx` 继续显示最近一次成功的主循环 `input_tokens`。压缩请求使用单独 scene，不能覆盖 `/ctx` 的主循环样本。

### 2.4 memory

memory 保存跨会话的长期事实。checkpoint 只是当前会话的压缩历史。两者不互相替代。

## 3) Message 分类

在发送 provider 前，prompt manager 将 messages 分为两类。

### 3.1 FixedMessages

每一轮按当前运行状态重建，不参与压缩：

1. system prompt。
2. runtime meta。
3. memory context。

### 3.2 TranscriptBlocks

按时间顺序组成会话和当前 run 的可持久历史：

1. 已有 checkpoint message。
2. 外部聊天历史。
3. 用户 message。
4. assistant message。
5. tool exchange。
6. 已经应用的 steer message。
7. 已经完成作用的 protocol message。

tool exchange 是原子 block：

```text
assistant message with tool_calls
+ all tool result messages referenced by those tool_call IDs
```

压缩切分点只能出现在 block 之间。不能留下没有 tool result 的 tool call，也不能留下没有对应 tool call 的 tool result。

尚未执行或正在等待审批的 tool exchange 始终属于 `RetainedTail`。

## 4) 压缩前后的 message 结构

### 4.1 压缩前

下面的例子表示当前 run 已经完成 ToolExchangeBlock A，并且还保留着更近的 ToolExchangeBlock B。

```text
[0] system    Fixed: system prompt
[1] user      Fixed: runtime meta
[2] user      Fixed: memory context

[3] user      Transcript: old checkpoint             optional
[4] user      Transcript: current user message U7
[5] assistant Transcript: ToolExchangeBlock A
              tool_calls=[{id:"call_a", name:"read_file", ...}]
[6] tool      Transcript: ToolExchangeBlock A
              tool_call_id="call_a", content="..."
[7] assistant Transcript: ToolExchangeBlock B
              tool_calls=[{id:"call_b", name:"bash", ...}]
[8] tool      Transcript: ToolExchangeBlock B
              tool_call_id="call_b", content="..."
```

设本次选中的 `MessagesToCompact` 是 `[3]` 至 `[6]`：

1. old checkpoint。
2. 当前用户 message `U7`。
3. 完整的 ToolExchangeBlock A。

`[7]` 和 `[8]` 是 `RetainedTail`。

### 4.2 压缩后

压缩成功后，`[3]` 至 `[6]` 全部删除，并在原来第一条被删除 message 的位置插入 checkpoint：

```text
[0] system    Fixed: system prompt                   unchanged
[1] user      Fixed: runtime meta                    unchanged
[2] user      Fixed: memory context                  unchanged

[3] user      Transcript: new checkpoint             summary(old, U7, A)
[4] assistant Transcript: ToolExchangeBlock B        unchanged
[5] tool      Transcript: ToolExchangeBlock B        unchanged
```

转换规则是：

```text
old checkpoint + U7 + ToolExchangeBlock A
→ new checkpoint
```

新 checkpoint 完全取代左边的 messages。它不是在保留原 messages 的同时另外追加一条 summary。

ToolExchangeBlock B 的 role、content、tool calls 和 tool call IDs 都不修改。

### 4.3 下一轮

下一条用户消息 `U8` 到达时，runtime 重建 fixed messages，加载已持久化 checkpoint，再追加 checkpoint 未覆盖的新历史：

```text
[0] system    Fixed: system prompt for new run
[1] user      Fixed: runtime meta for new run
[2] user      Fixed: memory context for new run
[3] user      Transcript: persisted checkpoint
[4] ...       Transcript: history created after checkpoint boundary
[5] user      Transcript: current user message U8
```

已被 checkpoint 覆盖的原始 messages 不能再出现。

## 5) Checkpoint message 格式

checkpoint 使用 `user` role，内容是 runtime 生成的结构化 JSON。压缩 LLM 不能自行选择 role，也不能返回任意 message 数组。

```json
{
  "runtime_context": {
    "kind": "context_checkpoint",
    "summary": "A short overall summary of the compacted messages.",
    "user_intent": [
      "The user's goals and explicit requirements."
    ],
    "references": {
      "files": [],
      "directories": [],
      "urls": []
    },
    "progress": {
      "completed": [],
      "in_progress": [],
      "pending": []
    },
    "intermediate_results": []
  },
  "instruction": "Continue from this checkpoint. Do not repeat completed work."
}
```

必填字段：

1. `summary`：所有参与压缩 messages 的短摘要。
2. `user_intent`：用户的目标、要求、约束和已明确的偏好。
3. `references.files`：相关文件路径。
4. `references.directories`：相关目录路径。
5. `references.urls`：相关 URL。
6. `progress.completed`：已完成的工作。
7. `progress.in_progress`：正在进行的工作。
8. `progress.pending`：尚未完成的工作。
9. `intermediate_results`：已得到但尚不代表整个任务结束的阶段性结果。

所有字段必须存在。对应内容不存在时，数组使用空值，不删除字段。`summary` 和 `user_intent` 不得同时为空。

checkpoint 不保存 API key、header、system prompt 全文、runtime meta 或 memory 原文。

## 6) 压缩触发

### 6.1 可用输入空间

```text
input_limit = context_window_tokens - output_reserve_tokens
trigger     = input_limit * trigger_ratio
```

`output_reserve_tokens` 优先取当前请求明确设置的 `max_tokens`。未设置时使用：

```text
default_reserve = min(context_window_tokens / 2,
                      min(32768, max(4096, context_window_tokens / 10)))
```

### 6.2 计数优先级

1. provider 返回的 `Usage.InputTokens` 是事实值。
2. 同一 run 内，在已知事实值的基础上，只对新追加的 blocks 做保守估算。
3. 新 run 的首次请求没有可靠 usage 时，估算只用来发现明显过大的输入。
4. provider 返回 context-length error 是最终事实。

不引入一个假装通用的 OpenAI tokenizer。不同 provider 的 message framing、tool schema、image 和 tokenizer 不同，单一 encoding 不能精确计算所有 provider-ready request。

### 6.3 触发时机

主动触发：

1. 已知 `InputTokens` 达到 trigger。
2. 或已知值加新 blocks 的估算值达到 trigger。
3. 或首次请求的保守估算明显超过 input limit。

被动触发：

1. 主请求返回 context-length error。
2. 引擎尝试压缩一次。
3. 压缩成功后重试原主请求一次。
4. 同一 step 不再进行第二次压缩重试。

context-length error 由 `llm.IsContextLengthError(err)` 统一分类。agent 不直接解析各 provider 的错误文本。

## 7) 选择参与压缩的 messages

prompt manager 从 TranscriptBlocks 的最旧端选择连续前缀。

规则：

1. 已有 checkpoint 如果位于前缀中，与其他 messages 一起参与新一轮压缩。
2. 用户 message 可以参与压缩。它被删除后，用户意图必须完整进入新 checkpoint。
3. tool exchange 只能整块选中或整块保留。
4. 至少保留一个最近的完整 transcript block，如果当前存在可保留 block。
5. 待执行、待审批或正在执行的 tool exchange 不参与压缩。
6. 尚未应用的 steer 和当前正在生效的 protocol instruction 不参与压缩。
7. 不从 message content、parts 或已序列化 JSON 中间截断。

压缩应一次释放足够空间，避免每次新增少量 messages 都重复压缩。V1 在触发后将新 prompt 目标控制在 `input_limit` 的 60% 左右。

如果没有可以安全选择的连续前缀，不硬截断单个过大 block。引擎返回明确错误，并记录哪个 block 无法安全压缩。

## 8) 压缩 LLM 请求

压缩使用当前 run 已解析的 main client 和 model，不重新读取 session 启动时的 route。

请求只包含两条 messages：

```text
[0] system  固定的 checkpoint 生成规则和 JSON schema
[1] user    {
              "messages_to_compact": [
                {"role":"user", "content":"..."},
                {"role":"assistant", "tool_calls":[...]},
                {"role":"tool", "tool_call_id":"...", "content":"..."}
              ]
            }
```

`messages_to_compact` 就是成功后将被删除的完整 messages。如果其中已经有 checkpoint message，也按原样作为其中一条 message 传入。

请求属性：

1. scene 为 `<runtime>.context_compact`。
2. 不提供 tools。
3. 不使用主 agent 的 plan/final 输出协议。
4. 输出使用严格 JSON。
5. 设置明确的最大输出 token。
6. 不携带 system prompt、runtime meta、memory 或 `RetainedTail`。

压缩 LLM 只返回 `runtime_context` 的内容。role、外层 wrapper 和 instruction 由程序生成。

## 9) 验证、持久化和替换

压缩结果先进入 candidate，不立即修改当前 messages。

candidate 必须通过：

1. JSON 结构合法。
2. 第 5 节的必填字段全部存在。
3. `summary` 和 `user_intent` 不同时为空。
4. 字段长度和总输出大小未超限。
5. 新 message 的 role 固定为 `user`。
6. `RetainedTail` 和 FixedMessages 与压缩前完全相同。
7. 新 message 列表没有不完整的 tool exchange。

提交顺序：

```text
生成 candidate checkpoint
    ↓
验证 candidate
    ↓
原子保存 checkpoint 和 covered-through boundary
    ↓
删除 MessagesToCompact
    ↓
在原位置插入 CheckpointMessage
    ↓
继续主请求
```

持久化失败时，原 messages 保持不变。不允许出现“当前 run 已经删除，但下一轮又恢复旧历史”的状态。

## 10) 跨轮持久化

checkpoint 需要带有不进入 prompt 内容的存储元数据：

```go
type ContextCheckpoint struct {
	Version         int
	Revision        int64
	Message         llm.Message
	CoveredThrough  string
	SourceModel     string
	SourceRunID     string
	CompactionCount int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
```

`CoveredThrough` 是 runtime 生成的不透明历史 boundary。下一轮在 `RenderHistoryContext` 之前排除该 boundary 及之前的外部历史。

agent 通过已绑定 conversation key 的小接口访问存储：

```go
type ContextCheckpointStore interface {
	Load(context.Context) (ContextCheckpoint, bool, error)
	Save(context.Context, int64, ContextCheckpoint) error
}
```

`Save` 的第二个参数是 expected revision。存储层在文件锁内检查 revision，防止较旧的并发 run 覆盖新 checkpoint。

文件存储必须使用原子 replace，并且只允许当前用户读写。checkpoint 可能包含敏感会话摘要。

## 11) 失败语义

### 11.1 主动压缩失败

如果原请求尚未被 provider 拒绝，则：

1. 不删除任何 messages。
2. 不写入部分 checkpoint。
3. 记录 warning 和 failure event。
4. 在原 prompt 仍有可能被 provider 接受时，尝试原主请求。

### 11.2 context-length error 后压缩失败

返回原 context-length error，并附上压缩失败原因。不隐藏 provider 原错误，不循环重试。

### 11.3 压缩后重试仍过长

同一 step 不再发起第二次压缩。返回重试错误，并记录压缩前后的 message 数和已知 token 数。

## 12) Profile 切换

每次 run 的 model 和 context window 都从本次已解析 route 传入 engine。压缩器不保存、不缓存 session 启动时的 profile budget。

解析顺序：

1. 本次 route 的 `ClientConfig.ContextWindowTokens`。
2. 本次实际 model 在内置 catalog 中的值。
3. 都不知道时，关闭主动触发，仅保留 context-length error 后的被动路径。

checkpoint 可以跨 model 使用，因为它保存的是结构化语义状态，不保存 tokenizer 状态。

## 13) Usage 和可观测性

压缩请求经过现有 `llmstats.UsageClient`，因此会进入 usage journal。引擎还必须对压缩结果调用 `agent.Context.AddUsage`，使 `LLMRounds`、`TotalTokens` 和 `TotalCost` 包含压缩。

只要 provider 已成功返回 usage，即使后续 checkpoint JSON 验证失败，usage 也必须计入。

新增 events：

1. `context_compaction_start`
2. `context_compaction_done`
3. `context_compaction_failed`

event 不包含 checkpoint 全文或用户原始消息。

`internal/topiccontext.shouldTrackScene` 继续只跟踪 `.loop` scene，因此 `<runtime>.context_compact` 不会覆盖 `/ctx` 的主循环 usage。

## 14) 配置

不复用 `max_token_budget`。新配置只控制压缩行为：

```yaml
context_compaction:
  enabled: true
  trigger_ratio: 0.80
  output_reserve_tokens: 0
```

规则：

1. `enabled` 控制主动和被动压缩。
2. `trigger_ratio` 必须大于 0 且小于 1。
3. `output_reserve_tokens: 0` 表示使用默认规则。
4. 压缩策略是 agent runtime 行为，不放进 LLM profile。
5. V1 不增加 tokenizer 和重试次数等可调参数。

## 15) 实现阶段与测试

### Phase 1：Transcript block 和前缀选择

先添加测试，再实现：

1. FixedMessages 永远不参与压缩。
2. 选中的 messages 是 TranscriptBlocks 的连续前缀。
3. 多个并行 tool calls 和全部 results 不被拆开。
4. pending approval 的 tool exchange 不参与压缩。
5. Run/Resume 序列化前后的 block 语义一致。

### Phase 2：Checkpoint schema 和 message 替换

先添加测试，再实现：

1. 缺少必填字段的 checkpoint 被拒绝。
2. 用户 message 被压缩时，新 checkpoint 含有非空 `user_intent`。
3. 压缩成功后，所有 `MessagesToCompact` 被删除。
4. 新 checkpoint 只出现一次，位于第一条被删除 message 的原位置。
5. FixedMessages 和 `RetainedTail` 逐字段保持不变。

### Phase 3：持久化和跨轮历史

先添加测试，再实现：

1. store 原子读写、文件锁和 revision 冲突。
2. 两个 conversation key 不会串状态。
3. 下一轮加载 checkpoint message。
4. covered-through boundary 之前的外部历史不再渲染。
5. boundary 之后的新历史仍原样保留。

### Phase 4：触发、usage、错误重试和 profile 切换

先添加测试，再实现：

1. 未达阈值时不压缩。
2. 达到阈值时在下一次主请求前压缩。
3. 压缩 usage 进入 usage journal 和 run 累计。
4. 压缩失败时原 messages 不变。
5. context-length error 只触发一次压缩和一次重试。
6. `/model` 切换后使用新 profile 的 window。
7. 压缩 scene 不覆盖 `/ctx` 主循环 usage。

## 16) 验收标准

1. 达到阈值后，引擎生成一条 checkpoint message。
2. checkpoint 包含 summary、用户意图、文件/目录/URL、当前进度和阶段性结果字段。
3. 参与压缩的原始 messages 全部被删除。
4. 未参与压缩的 tail 与压缩前完全相同。
5. tool call/result 不被拆开。
6. 压缩 usage 同时进入 usage journal 和 `agent.Context.Metrics`。
7. 下一轮 prompt 包含 checkpoint，不包含 covered-through boundary 之前的原历史。
8. 压缩失败时，不删除任何原 messages。
9. context-length error 后最多压缩并重试一次。
10. `/model` 切换后，阈值使用新 profile 的 context window。

## 17) 对 PR #38 问题的对应

| PR #38 问题 | 本设计的处理 |
| --- | --- |
| `/model` 切 profile 后预算来源错误 | 每次 run 使用当前已解析 route 的 model 和 window |
| 压缩 usage 未计入 | 压缩 usage 同时进入 `llmstats` 和 `agent.Context.AddUsage` |
| 下一轮恢复原历史 | 持久化 checkpoint message 和 covered-through boundary，下一轮过滤已覆盖历史 |
| 缺少主流程测试 | 覆盖阈值触发、message 替换、跨轮持久化、usage 和 profile 切换 |
| 主动压缩失败会中止请求 | 失败时不修改原 messages，原 prompt 仍可发送时继续主请求 |
| 配置迁移破坏现有语义 | 保留 `max_token_budget`，新增独立 `context_compaction` 配置 |
