---
date: 2026-04-22
title: Context Window Budget and Compression
status: draft
---

# Context Window Budget and Compression

## 1) 目标

这次需求解决的是主 agent loop 的上下文窗口失控问题。

当前问题很具体：

- 不同上游模型的上下文窗口不同
- 当前 `max_token_budget` 放在 agent 顶层，不适合表达“这是模型本身的窗口约束”
- 当前预算判断是事后累计，不是请求前保护
- 当消息越积越多时，下一次 `llm.Chat` 可能直接被上游拒绝

这次要做的事也保持最小：

1. 把 `max_token_budget` 的配置语义移到 `llm` 和 `llm.profiles`
2. 对大多数已知模型，按归一化后的 model name 内置上下文窗口静态表
3. 当用户没有显式配置 `max_token_budget` 时，自动取 `floor(context_window * 0.8)`
4. 在主请求发出前做预算检查；达到阈值时，先做上下文压缩，再继续主流程
5. 压缩后裁剪 `messages`，只保留原始 system prompt 和压缩后的上下文摘要
6. 让模型明确知道它拿到的是“压缩后的历史上下文”，不是原始逐条消息
7. 旧的顶层 `max_token_budget` 配置直接删除，不保留兼容读取
8. 上下文压缩默认开启，不提供开关

## 2) 现状问题

当前实现的问题不是“预算值不够精细”，而是放错了层。

`max_token_budget` 现在是 agent loop 级别的参数。它更像“本次运行累计花了多少 token”这一类控制，而不是“当前模型最多能吃多少上下文”。

这样会带来两个直接后果：

- 同一套 agent 配置切到不同 LLM profile 时，预算不会自然跟着模型窗口变化
- 预算检查发生在请求成功返回之后，挡不住“下一次请求直接超窗口”这种错误

这正是当前行为不合理的地方：

- 90k prompt 发得出去，不代表下一轮还发得出去
- 真正需要保护的是“即将发出的这次请求”
- 所以判断点必须前移到 `llm.Chat` 之前

## 3) 范围

V1 只覆盖主 agent loop 的上下文压缩。

纳入范围：

- `agent.Engine` 主循环里的请求前预算检查
- 基于当前生效 LLM profile 的预算解析
- 达到阈值后的独立压缩请求
- 压缩后的消息裁剪和继续执行
- 对未知模型的预算解析和告警策略

暂不纳入范围：

- `addressing`
- `heartbeat`
- `memory_draft`
- 其他一次性、专用的内部 LLM 调用

原因很简单：

- 这次要解决的是“主上下文滚大以后下一轮炸掉”
- 先把主链路做对，比一口气改所有子调用更重要

## 4) 非目标

这次明确不做下面这些事：

- 不重做整个 memory 系统
- 不引入另一套专门的“廉价摘要模型”路由
- 不把上下文压缩做成通用多层递归摘要框架
- 不要求 v1 就完全对齐所有供应商的精确 tokenizer
- 不把触发比例开放成一堆用户可调参数
- 不保留原始逐条对话的完整措辞
- 不把压缩结果暴露成面向用户的产品输出

V1 的目标很朴素：

- 在主请求真正溢出前，稳定地把上下文压小
- 让 agent 能继续工作

## 5) 第一性原理

### 5.1 窗口限制属于模型，不属于 agent

`gpt-5.2`、`gpt-4.1-mini`、`grok-4.1-fast-reasoning`、`gemini` 系列的上下文窗口本来就不同。

所以预算应该跟着当前 LLM profile 走，而不是跟着 agent 全局走。

### 5.2 保护点必须在请求前

如果预算检查发生在请求成功之后，它只能统计“已经花掉了多少”。

这对防止窗口溢出没有帮助。

真正需要的是：

- 在每次主 `llm.Chat` 之前
- 针对“这一次将要发送的 request”
- 估算输入 token
- 超阈值就先压缩

### 5.3 压缩是运行时工作内存，不是用户摘要

压缩结果的目标不是“写得好看”，而是“保住后续推理必需的信息”。

它至少要保住：

- 当前任务是什么
- 已经做到了哪里
- 哪些决定已经形成
- 哪些事实不能丢
- 哪些文件、URL、标识符、工具结果以后可能还要回查

### 5.4 压缩后必须明确标识

被裁掉的原始消息不会继续留在 prompt 里。

所以压缩结果必须显式告诉模型：

- 这是 earlier context 的压缩摘要
- 原始逐条消息已经被裁掉
- 这里是历史工作内存，不是新的用户指令

## 6) 配置契约

### 6.1 配置落点

新的预算语义放在：

- `llm.max_token_budget`
- `llm.profiles.<name>.max_token_budget`

示例：

```yaml
llm:
  provider: openai
  model: gpt-5.2

  profiles:
    cheap:
      model: gpt-4.1-mini

    long_ctx:
      model: gemini-2.5-pro
      max_token_budget: 160000
```

### 6.2 配置语义

- `max_token_budget` 表示“主请求输入预算阈值”，不再表示“整次 run 的累计 token 上限”
- 用户显式配置正整数时，直接使用该值
- 用户未配置时，按模型静态表自动推导
- 上下文压缩默认开启，不提供配置开关

### 6.3 静态上下文窗口表

内置推理服务供应商的 model 最大上下文窗口，放进一份静态 YAML。

这份 YAML 只表达一件事：

- 某个归一化 model name 对应的最大上下文窗口

建议形态：

```yaml
models:
  gpt-5.2:
    context_window_tokens: 400000
  gpt-4.1-mini:
    context_window_tokens: 1000000
```

这份表是预算自动推导的基础，不承载别的能力开关。

首批纳入范围应先覆盖当前主流推理 API 供应商的主力模型。

第一轮至少包括：

- OpenAI
- Anthropic
- Google Gemini
- xAI
- DeepSeek

如果后面确认仓库里还稳定支持别的主流推理供应商，再继续补表。

## 7) 预算解析规则

预算解析按下面顺序进行：

1. 当前生效 profile 的显式 `max_token_budget`
2. 顶层 `llm.max_token_budget`
3. 按归一化后的 model name 查内置上下文窗口静态 YAML

如果走到第 3 步，预算值按下面公式得到：

- `max_token_budget = floor(context_window_tokens * 0.8)`

这里的 model name 归一化规则应和现有 LLM 能力判断保持一致：

- trim
- lowercase
- 必要时保留 provider/model 这种规范化后的统一判断口径

如果模型不在内置 YAML 里，且用户也没有显式给预算：

- 不做猜测
- 不静默写死一个默认数
- 可以打印告警
- 主流程照常发请求
- 如果上游因为上下文不足报错，直接透传该错误

## 8) 触发时机

预算检查不是每次主 `llm.Chat` 之前都做完整预判。

检查对象不是累计 usage，而是“这次即将发出的 request input”。

这里至少要计入：

- `messages`
- tool schemas
- provider 会计入 prompt 的固定请求部分

触发规则：

- 第一次主请求，必须做一次完整 token 预判
- 后续主请求，不默认每次都重新跑 tokenizer
- 只有当“自上次预判以来的累计 token 数”达到最大窗口的 70% 时，才再次做完整预判
- 如果完整预判后，估算输入 token 小于预算阈值，直接发主请求
- 如果完整预判后，估算输入 token 大于等于预算阈值，先做压缩，再重新估算，再发主请求

这一步是主链路的常规保护，不是异常兜底。

### 8.1 如何预估 token

“预估”不能只是按字符数拍脑袋。

V1 的估算对象必须是：

- 当前即将发出的、已经按 provider 适配完成的真实请求形态

也就是先做这些事，再做计数：

1. 先完成 provider/model 解析
2. 先把 `messages`、tools、parts、system instruction 等内容整理成该 provider 实际要发送的请求结构
3. 再对这个结构做 token 估算

所有供应商统一使用本地 tokenizer / 本地估算。

V1 不调用远端 token count / tokenize API。

原因：

- 预算检查是主链路热路径
- 远端计数本身也是网络调用，会放大延迟和失败面
- 需求已经明确统一走本地 tokenizer

因此计数策略只有两层：

1. 对 provider-ready request 做本地 tokenizer 估算
2. 对 tokenizer 还不能精确覆盖的 provider-specific 字段，补一层本地保守近似估算

### 8.2 本地估算的要求

本地估算不能只数 message 文本，还要把这些内容算进去：

- system prompt
- 普通 text parts
- multimodal parts 的已知 token 规则
- tools schema
- provider 会自动包进 prompt 计算范围的 request 字段

本地估算的输入源，应是序列化前的结构化 request，而不是运行日志里的 markdown dump。

### 8.3 何时重新跑 tokenizer

这里需要降低每轮都做 tokenizer 的成本。

规则固定如下：

1. 第一次主请求，必须跑 tokenizer 预判
2. 记录这次预判对应的 checkpoint
3. 在后续请求之间，累计“自上次预判以来新增进入上下文的 token 数”
4. 如果这个累计值低于“最大窗口的 70%”，就不重新跑 tokenizer
5. 如果这个累计值大于等于“最大窗口的 70%”，就再次跑 tokenizer 预判，并刷新 checkpoint

这个累计值不是指模型总 usage，也不是历史总 token。

这里指的是：

- 自上次完整预判之后
- 新增进入当前上下文工作集的 token 增量

这样做的目的很直接：

- 首次请求先把基线算准
- 后续小增量阶段不重复付 tokenizer 成本
- 当上下文已经明显涨大时，再重新精确判断

### 8.4 预算比较规则

预算比较只看“预计输入 token”是否达到 `max_token_budget`。

原因：

- 默认预算已经是 `context_window * 0.8`
- 这 20% 就是给输出和推理 token 留出的空间

因此：

- 默认路径不再额外叠加第二层比例 buffer
- 如果用户显式把 `max_token_budget` 配得过高，溢出风险由该显式配置自行承担

## 9) 压缩请求契约

上下文压缩本质上是一次独立的 `llm.Chat` 请求。

### 9.1 使用哪一个模型

使用当前主 loop 已解析出来的同一个 LLM profile。

也就是：

- 同一个 provider
- 同一个 model
- 同一套认证和 provider-specific 配置

不额外切 route，不额外引入“摘要专用模型”。

### 9.2 不携带什么

这个压缩请求不应携带：

- 全局 system prompt
- tools
- 当前主 agent 的 response-format 协议
- 跟正常工具调用相关的 prompt augment

原因很直接：

- 这里不是在继续主任务
- 这里只是在生成一份工作内存摘要
- 把主 system prompt 整包再带一遍，只会浪费窗口

### 9.3 压缩请求要带什么

压缩请求只带两类内容：

1. 一段专门的压缩指令
2. 当前待压缩的上下文载荷

待压缩的上下文载荷指：

- 当前 `messages` 里除原始 system prompt 外的全部消息

这样做有两个好处：

- 原始 system prompt 会在压缩后继续保留，不需要再总结一遍
- 压缩目标聚焦在“运行时累积上下文”，而不是模型行为规则

## 10) 压缩结果契约

压缩结果必须是结构化 JSON，而不是一段随意 prose。

建议最小结构如下：

```json
{
  "summary_version": 1,
  "current_task": "当前用户要解决的问题",
  "current_state": "目前已经做到哪里",
  "important_facts": [
    "不能丢的事实"
  ],
  "open_items": [
    "还没完成的点"
  ],
  "lookup_index": [
    {
      "label": "需要回查的外部资料",
      "where": "https://example.com/spec",
      "why": "这里有原始 API 约束"
    }
  ]
}
```

### 10.1 必须保住的信息

压缩时不能只保“结论”，还要保住后续执行需要继续依赖的状态：

- 当前任务和目标
- 当前进度
- 已经得到但后面还会继续用的关键事实
- 文件路径、URL、对象 ID、配置键、命令名、错误码等硬信息

### 10.2 `lookup_index` 的要求

这里的“索引”不是为了好看，而是为了告诉后续模型“去哪儿查原始东西”。

`lookup_index` 应优先放可复查的稳定指针：

- 文件路径
- URL
- 资源标识符
- 工具名加关键参数
- 以后可重新执行或重新读取的定位信息

不应主要依赖这类脆弱引用：

- “前面第几条消息”
- “刚才提过一次”
- “上文提到”

因为原始消息压缩后已经不在 prompt 里了。

## 11) 压缩后的消息裁剪规则

压缩成功后，`messages` 要被重建，而不是在旧数组后面继续追加。

裁剪后的 `messages` 只保留：

1. 原始 system prompt
2. 一个新的“压缩上下文”消息

这个“压缩上下文”消息应明确标记，例如：

- `[[ Compressed Context ]]`
- 说明它是 earlier context 的摘要
- 说明原始逐条消息已被裁掉
- 说明它是背景工作内存，不是新的用户请求

这里不保留旧的 meta/history/tool-result/user turns。

原因：

- 需求的核心就是把膨胀过的上下文真正裁掉
- 如果只是把摘要追加到原消息后面，窗口并不会被释放

一个 run 里允许多次压缩。

当后续消息再次膨胀并重新达到阈值时：

- 允许再次触发压缩
- 新的压缩结果覆盖旧的“压缩上下文”消息
- 不保留旧版本摘要

## 12) 主流程上的行为变化

主流程会变成这样：

1. 组装主请求
2. 估算本次输入 token
3. 如果未到阈值，直接发主请求
4. 如果到达阈值，先发压缩请求
5. 用压缩结果重建 `messages`
6. 重新估算重建后的主请求
7. 再发主请求

这意味着：

- 压缩是主链路中的一个显式阶段
- 它发生在真正溢出之前
- 它不是“等报错再说”

## 13) 失败处理

### 13.1 压缩请求本身过大

如果压缩请求本身就因为上下文过大而失败，不做递归摘要算法。

V1 只做一次额外收缩：

- 保留第一条非 system prompt
- 再保留后 50% 的 messages
- 用这个更小的载荷重试一次压缩请求

这样做的目标很直接：

- 尽量保留任务起点
- 尽量保留最近一半的工作轨迹

如果这一次重试仍然因为上下文不足失败：

- 直接透传上游错误

### 13.2 压缩后主请求仍然失败

如果压缩已经成功，主请求发出后仍被上游以 context-length 类错误拒绝：

- 直接透传上游错误

不再追加额外的本地兜底算法。

## 14) 兼容性与迁移

这次需求会改变 `max_token_budget` 的语义，也会删除旧的顶层配置入口。

旧语义：

- agent run 的累计 token 上限

新语义：

- 当前 LLM profile 的主请求输入预算阈值

因此需要明确：

- 旧配置文档全部迁到 `llm.*`
- CLI 帮助和配置模板里的解释同步修改
- 旧顶层字段直接删除
- 运行日志里应能看出预算来自哪里

建议日志里至少打出：

- active profile
- resolved model
- budget source
- resolved max_token_budget
- whether compression is enabled

## 15) `/ctx` 指令

需要新增一个 `/ctx` 指令，用来查看当前上下文使用情况。

这个指令的目标不是调试底层 tokenizer 细节，而是给运行时一个直接可读的状态面板。

### 15.1 输出字段

`/ctx` 至少返回这三个字段：

- `current_tokens`
- `context_window`
- `compression_count`

建议文案形态：

```text
current_tokens: 58234
context_window: 400000
compression_count: 2
```

### 15.2 字段语义

`current_tokens`：

- 表示当前上下文工作集的最新估算 token 数
- 不是上游 usage 里的累计总消耗
- 不是整次 run 的 `total_tokens`
- 它的计算口径应和预算检查口径一致

更具体地说：

- 如果刚做过完整预判，就直接显示那次完整预判得到的当前上下文 token 数
- 如果自上次完整预判后又追加了一些消息，但还没达到 70% 门槛重新预判，就显示“上次完整预判值 + 之后累计新增 token”

`context_window`：

- 表示当前生效 model 对应的最大上下文窗口
- 来源是内置静态 YAML 或该 model 的显式配置

`compression_count`：

- 表示当前 run 已经发生过多少次上下文压缩
- 每成功完成一次压缩，加一
- 新 run 重新从零开始

### 15.3 作用域

`/ctx` 查看的是当前会话绑定的运行时上下文状态。

它反映的是：

- 当前会话
- 当前生效 profile / model
- 当前 run 上下文工作集

不是全局统计面板。

### 15.4 输出要求

这个指令保持最小，不扩成大诊断页。

V1 不要求展示：

- 每条 message 的 token 明细
- 各 provider-specific 开销拆分
- tokenizer 名称
- 上游 usage 的 input/output/reasoning 分类
- 上次压缩摘要内容

## 16) 验收标准

满足下面这些条件，才算这次需求完成：

1. 未显式配置预算时，已知模型能按内置窗口表自动得到 `floor(window * 0.8)` 的预算
2. 不同 `llm.profiles` 切换后，预算会跟着 profile 一起变化
3. 主 `llm.Chat` 发出前会做输入预算检查，而不是等成功返回后才判断
4. 达到阈值时，会先执行独立压缩请求
5. 压缩请求不携带全局 system prompt，不携带 tools
6. 压缩成功后，`messages` 会被重建成“system prompt + compressed context”
7. 压缩上下文里明确声明这是裁剪后的历史摘要
8. 压缩结果里保留当前任务、关键事实、未完成事项和 lookup index
9. 未知模型且无显式预算时，不做静默猜测；主流程照常请求，若上游报上下文不足则直接透传
10. 压缩请求本身过大时，只做一次“第一条非 system + 后 50% messages”的压缩重试
11. 一个 run 里允许多次压缩，新的摘要覆盖旧摘要
12. 上下文压缩默认开启，不提供配置开关
13. 预估 token 时，所有供应商统一走本地 tokenizer / 本地估算，不调用远端 count/tokenize API
14. 首批静态 YAML 会先覆盖主流推理 API 供应商的主力模型窗口大小
15. 第一次主请求一定做完整 token 预判；后续只有在“自上次预判以来的累计 token 数”达到最大窗口 70% 时才重新预判
16. `/ctx` 能返回当前上下文工作集的最新估算 token 数、当前上下文窗口、当前已压缩次数

## 17) 实现约束

这次实现应保持收敛，不要过度设计。

具体约束：

- 不增加一层新的通用“上下文管理服务”
- 不增加一套和 `llm.profiles` 平行的摘要模型配置体系
- 不把触发阈值做成一堆 per-route 可调参数
- 不把压缩摘要和长期 memory 混成一个概念
- 不把上下文压缩做成一个用户可关闭的选项

最小正确模型只有三件事：

- 预算跟着 LLM profile 走
- 预算默认来自模型窗口静态 YAML
- 超阈值时压缩并裁剪消息

## 18) 实施顺序

实施顺序也要固定，避免先写逻辑再补数据。

1. 先查官方资料，确定主流推理 API 供应商主力模型的上下文窗口大小
2. 把这些窗口大小落进内置静态 YAML
3. 建立 model name 归一化匹配
4. 明确 provider 级本地 tokenizer / 本地估算路径
5. 接入“首次必预判，后续按 70% 门槛重跑”的预算检查
6. 接入上下文压缩与消息裁剪
7. 接入 `/ctx` 指令与对应状态读取

第一步必须做扎实：

- 只看官方文档或官方 API 元数据
- 不用第三方整理表
- 记录每个窗口值的来源，方便后续更新 YAML

## 19) Checklist

### 19.1 窗口数据

- [ ] 确认当前仓库实际需要优先支持的主流推理供应商与主力模型范围
- [ ] 逐个查官方资料，记录每个模型的最大上下文窗口和来源链接
- [ ] 设计内置静态 YAML 的结构，只保留模型名、窗口大小和必要元数据
- [ ] 将首批模型窗口数据写入内置 YAML
- [ ] 建立这份 YAML 的加载路径和读取入口

### 19.2 模型匹配

- [ ] 明确 model name 归一化规则
- [ ] 将归一化后的 model name 映射到内置 YAML 条目
- [ ] 处理 alias、带 provider 前缀的 model 名和版本化 model 名
- [ ] 定义“找不到模型窗口数据”时的运行时行为

### 19.3 配置迁移

- [ ] 删除旧的顶层 `max_token_budget` 配置入口
- [ ] 在 `llm` 和 `llm.profiles` 上接入新的 `max_token_budget`
- [ ] 将预算解析逻辑切到当前生效 LLM profile
- [ ] 更新配置模板
- [ ] 更新配置文档和 CLI 帮助说明

### 19.4 本地 Token 预估

- [ ] 确认本地 tokenizer 方案和 provider 分层结构
- [ ] 建立 provider-ready request 到本地 token 估算的统一入口
- [ ] 让估算逻辑覆盖 `messages`
- [ ] 让估算逻辑覆盖 tools schema
- [ ] 让估算逻辑覆盖 multimodal parts 的本地规则
- [ ] 为 tokenizer 还不能精确覆盖的 provider-specific 字段补上保守近似估算

### 19.5 预算检查

- [ ] 接入“第一次请求必做完整预判”的逻辑
- [ ] 接入“自上次预判以来累计 token 数”的运行时状态
- [ ] 接入“低于最大窗口 70% 不重跑预判”的门控
- [ ] 接入完整预判后的预算比较逻辑
- [ ] 将预算阈值默认值切到 `floor(context_window * 0.8)`

### 19.6 上下文压缩

- [ ] 设计压缩请求的最小 prompt 契约
- [ ] 实现压缩请求不携带全局 system prompt
- [ ] 实现压缩请求不携带 tools
- [ ] 实现压缩结果的最小 JSON schema 校验
- [ ] 实现压缩成功后的 `messages` 重建
- [ ] 实现一个 run 内可多次压缩，并覆盖旧摘要

### 19.7 压缩失败路径

- [ ] 实现“压缩请求本身过大”时的第一次失败识别
- [ ] 实现“第一条非 system + 后 50% messages”的一次性缩减重试
- [ ] 实现重试失败后直接透传上游错误
- [ ] 实现压缩后主请求仍然 context overflow 时直接透传上游错误

### 19.8 `/ctx` 指令

- [ ] 增加 `/ctx` 指令入口
- [ ] 输出 `current_tokens`
- [ ] 输出 `context_window`
- [ ] 输出 `compression_count`
- [ ] 保证 `/ctx` 读取的是当前会话 / 当前 run 的运行时状态

### 19.9 状态与日志

- [ ] 为当前 run 增加上下文预算相关运行时状态
- [ ] 记录上次完整预判值和 checkpoint
- [ ] 记录自上次预判以来的累计新增 token
- [ ] 记录当前已压缩次数
- [ ] 在日志中输出当前 profile、resolved model、context window、budget 和压缩行为

### 19.10 测试

- [ ] 为模型窗口 YAML 加载和匹配补测试
- [ ] 为预算解析补测试
- [ ] 为“首次必预判 / 70% 门控”补测试
- [ ] 为压缩前后 `messages` 重建补测试
- [ ] 为“压缩请求过大后的单次缩减重试”补测试
- [ ] 为未知模型行为补测试
- [ ] 为 `/ctx` 输出补测试

### 19.11 收尾

- [ ] 更新相关设计文档与实现文档中的旧语义
- [ ] 检查现有运行时是否还引用旧的顶层 `max_token_budget`
- [ ] 检查 inspect / debug 输出是否需要补上下文预算信息
- [ ] 运行相关测试并整理剩余风险
