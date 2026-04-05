---
date: 2026-04-05
title: Subagent / 子任务运行时设计
status: in_progress
---

# Subagent / 子任务运行时设计

## 1) 目标

这篇文档讨论的是一种进程内 subagent 能力，解决两个直接问题：

- 有些任务很慢，例如 `git pull`、长时间 shell 命令。
- 有些任务会产生大量高噪声上下文，例如网页 HTML、大量 stdout/stderr。

目标是：

1. 支持主 agent 手工触发一个子任务。
2. 子任务在进程内运行，不默认起新的 `mistermorph` 外部进程。
3. 子任务过程可观察，但主 loop 不吞全量过程。
4. 子任务结果统一收敛到一个通用 envelope。
5. 尽量复用现有 `taskruntime`、任务状态和流式机制。

这期范围收敛为：

- `spawn` 一期只支持同步调用。
- 不做通用 `execution_mode=auto`。
- 不抽独立 `ArtifactStore`。

---

## 2) 非目标

这期先不做下面这些事：

- 默认不通过 `bash mistermorph --task "..."` 启动外部进程版 subagent。
- 不做分布式队列。
- 不做跨机器子任务调度。
- 不做所有工具的自动委托。
- 不做独立 `ArtifactStore`。
- 不做无限递归 spawn。

---

## 3) 当前代码状态

### 3.1 已有同步版 `spawn`

`agent/spawn_tool.go` 里已经有 `spawn` 工具。

当前参数是：

- `task`
- `tools`
- `model`
- `output_schema`

当前行为是：

- 从父 registry 里挑出允许的工具子集。
- 统一走 `SubtaskRunner`。
- 同步阻塞直到子任务结束。
- 返回统一 envelope，而不是直接返回 `final.Output`。

这说明：

- 手工 `spawn` 主链路已经接到统一的子任务抽象。
- 一期仍然只有同步调用。
- 异步句柄、任务查询和取消还没做。

### 3.2 `taskruntime` 已接入子任务执行

`internal/channelruntime/taskruntime/runtime.go` 已经提供了完整的运行时封装，支持：

- `History`
- `CurrentMessage`
- `Meta`
- `Registry`
- `PromptAugment`
- `PlanStepUpdate`
- `OnStream`
- `Memory`

当前已经做到：

- `taskruntime.Runtime` 实现了同步 `RunSubtask(...)`。
- `taskruntime.Run(...)` 创建 engine 时会自动注入 subtask runner。
- 子任务路径会显式关闭 runtime tool 自动注入，保证工具白名单语义稳定。

这意味着：

- channel runtime 路径已经有统一的子任务执行层。
- 子任务白名单不会再被 `plan_create`、`todo_update` 之类的 runtime tools 悄悄破坏。

### 3.3 已有任务状态模型

`internal/daemonruntime/types.go` 已有通用任务状态：

- `queued`
- `running`
- `pending`
- `done`
- `failed`
- `canceled`

还有 `TaskInfo`、`SubmitTaskRequest`、`SubmitTaskResponse`。

这意味着：

- 子任务状态不需要另造一套。
- subagent 最合理的建模方式就是“子任务”。

### 3.4 Console 已有流式任务范式

`cmd/mistermorph/consolecmd/local_runtime.go` 和 `cmd/mistermorph/consolecmd/streaming.go` 已经实现了：

- task 入队
- task 状态更新
- stream hub
- snapshot / final / abort 发布
- 基于 `taskruntime.Run(... OnStream: ...)` 的流式观察

这意味着：

- “任务有状态、有流、有最终结果” 这套范式已经存在。
- subagent 不该重新发明一套完全不同的协议。

### 3.5 `bash` 已支持显式 direct subtask

`tools/builtin/bash.go` 当前仍然是一次性收集输出，但已经支持显式 `run_in_subtask`：

- 启动命令
- `cmd.Run()`
- 等结束
- 一次性返回 `stdout/stderr`

如果设置 `run_in_subtask=true`，当前行为是：

- 不再重启一轮子 LLM。
- 直接在子任务边界里运行命令。
- 返回 `subtask.bash.result.v1` envelope。
- 保持和普通 `bash` 一致的错误语义。

所以当前状态是：

- 已有显式 `bash -> subtask`。
- 但还没有流式 bash。
- 没有中间状态
- 没法边跑边观察
- 没法做阶段性摘要

### 3.6 `url_fetch` 只是底层原语

`tools/builtin/url_fetch.go` 当前只负责：

- 发请求
- 读响应
- 截断或下载
- 返回结果

它知道“怎么取 URL”，但不知道“主任务真正想完成什么”。

所以：

- `url_fetch` 适合做底层原语。
- 不适合自己决定是不是进入 subagent。

---

## 4) 设计判断

### 4.1 默认实现应选进程内 sub runtime

候选方案有两种：

1. 外部进程：`bash mistermorph --task "..."`
2. 进程内 sub runtime

默认方案应选第 2 种。

原因：

- 更容易复用 `taskruntime`
- 更容易继承 guard、prompt、registry、memory
- 更容易拿到结构化状态和结构化结果
- 更容易和现有 task store / stream hub 对齐

外部进程方案可以保留为未来可选 backend，但不应做默认实现。

### 4.2 observer 必须是事件驱动

不建议默认采用这种模式：

```text
每隔 N 秒
  -> 读取全量输出
  -> 发给 LLM
  -> 决定是否汇报
```

问题是：

- 即使没有新信息，也会持续烧 token。
- 输出越长，重复摘要成本越高。
- observer 自己会变成新的上下文膨胀源。

更稳的做法是：

- 子任务持续产出事件。
- observer 只在关键事件上工作。
- 需要语义归纳时才调用 LLM。

### 4.3 原始高噪声内容不应回灌主 loop

父 loop 不应自动获得：

- 原始 HTML
- 原始 stdout/stderr
- 子任务完整 transcript

父 loop 应只拿：

- `task_id`
- 当前状态
- 少量压缩后的进度摘要
- 最终结果 envelope

高噪声材料一期先这样处理：

- 默认只保留在子任务内部。
- 如果底层 tool 本来就支持写文件，例如 `url_fetch.download_path`，继续沿用它自己的文件输出。
- 这期不抽象成独立 `ArtifactStore`。

### 4.4 结果必须统一成通用 envelope

不建议为每种任务定制顶层返回结构。

建议统一 envelope：

```json
{
  "status": "done",
  "summary": "已提取文章信息",
  "output_kind": "json",
  "output_schema": "subtask.web_extract.v1",
  "output": {
    "article_url": "https://example.com/post-3",
    "last_paragraph": "..."
  },
  "error": ""
}
```

约束建议如下：

- `output_kind = "text"` 时，`output` 必须是字符串。
- `output_kind = "json"` 时，`output` 必须是合法 JSON 值。
- `output_schema` 可选；当 `output_kind = "json"` 时，建议填写稳定 schema id。

---

## 5) 当前架构

```text
parent agent / tool
  -> spawn / bash(run_in_subtask=true)
     -> SubtaskRunner
        -> prepare task_id / depth / meta
        -> run agent subtask OR direct subtask
        -> return final envelope to parent
```

更细一点：

```text
spawn
  -> SubtaskRunner
     -> agent subtask
        -> child agent loop
        -> child tools
        -> envelope

bash.run_in_subtask
  -> SubtaskRunner
     -> direct subtask
        -> run command directly
        -> envelope
```

这里的关键点是：

- `spawn` 仍然是 agent 子任务。
- `bash.run_in_subtask` 已经是 direct subtask，不再走子 LLM。
- `taskruntime` 是 channel/runtime 路径下的实际执行层。
- 裸 `agent.New(...)` 路径也有本地 `SubtaskRunner`，不再只有 `taskruntime` 才能跑子任务。
- 父 loop 最终只接收 envelope，不接收原始材料。

---

## 6) 当前核心组件

### 6.1 `SubtaskRunner`

职责：

- 接受子任务请求。
- 生成 task id。
- 注入父子 run 元信息。
- 决定走 agent subtask 还是 direct subtask。
- 返回同步结果。

接口草图：

```go
type SubtaskRequest struct {
    Task         string
    Model        string
    OutputSchema string
    Registry     *tools.Registry
    Meta         map[string]any
    RunFunc      SubtaskFunc
}

type SubtaskResult struct {
    TaskID       string
    Status       string
    Summary      string
    OutputKind   string
    OutputSchema string
    Output       any
    Error        string
}
```

说明：

- `RunFunc != nil` 表示 direct subtask。
- `RunFunc == nil` 表示 agent subtask。
- 一期仍然没有异步句柄。

### 6.2 本地 `SubtaskRunner`

当前 `agent.New(...)` 默认会挂一个本地 runner。

职责：

- 给裸 engine 路径提供子任务能力。
- 负责 `spawn` 和 direct subtask 的本地执行。
- 打出 `subtask_start` / `subtask_done` 日志。

### 6.3 `taskruntime.Runtime`

职责：

- 在 console / telegram / slack / line / lark 这类 runtime 里提供子任务执行。
- 继承 runtime 的 logger、guard、prompt、memory 等能力。
- 在 agent 子任务路径里关闭 runtime tool 自动注入，保证白名单稳定。

---

## 7) 观察者设计

### 7.1 事件输入

observer 的输入应该是事件流，而不是“定时器 + 全量输出”。

建议触发条件：

1. 状态变化。
2. 子任务首次产生输出。
3. 距上次摘要后新增输出超过阈值。
4. 长时间无新输出，但任务仍在运行。
5. 子任务进入 `pending`。
6. 子任务结束或失败。
7. 子任务主动发出“需要父级帮助”的信号。

### 7.2 三层处理

建议按三层处理：

1. 原始事件层  
   保存状态变化和输出片段。

2. 压缩层  
   对输出做 ring buffer、去重、截断、按行合并。

3. 汇报层  
   只在必要时把压缩后的片段交给 LLM，或者直接形成结构化进度。

### 7.3 什么时候才需要 LLM

不是每个事件都要调 LLM。

优先顺序应该是：

1. 先做本地压缩。
2. 本地压缩够用就不调 LLM。
3. 只有需要语义归纳时才调 LLM。

例如：

- `git pull` 的阶段性输出，本地规则通常就够。
- 网页抽取任务需要从候选链接里归纳“第三篇文章”，再用 LLM 更合理。

---

## 8) 触发方式

### 8.1 手工触发

这里的“手工触发”指的是：

- `spawn` 作为普通 tool 暴露在 prompt 和 tool schema 里。
- 主 agent 在推理时主动发起一次 `spawn` tool call。

不是让用户在文本里写一个特殊关键字让系统自己解析。

当前阶段对 `spawn` 的约束是：

- 不新增 `mode` 参数。
- 继续保持同步调用。
- 现有参数维持 `task`、`tools`、`model` 这组最小集合。

也就是说，一期 `spawn` 的语义非常简单：

- 父 loop 发起一次子任务。
- 阻塞等待子任务结束。
- 最终拿回通用 envelope。

示例：

```json
{
  "name": "spawn",
  "params": {
    "task": "访问 https://example.com 。目标：找到首页列出的第三篇文章，读取正文，提取最后一段。最终只返回通用结果 envelope。只允许使用 url_fetch。不要输出原始 HTML。",
    "tools": ["url_fetch"]
  }
}
```

### 8.2 由工具触发

这期不定义通用的 `execution_mode=auto`。

原因：

- 自动分流规则不稳定。
- 不同工具的判断依据完全不同。
- 会把一期的范围和复杂度明显拉高。

如果以后某个工具要支持“进入 subtask”，应该走显式设计，而不是先抽一个通用 `auto`。

### 8.3 `bash` 和 `url_fetch` 的边界

#### `bash`

`bash` 现在已经支持显式 `run_in_subtask`，但实现方式是 direct subtask，不是 agent subtask。

当前行为是：

1. 主模型显式调用 `bash(run_in_subtask=true)`。
2. `bash` 直接在子任务边界里运行命令。
3. 返回 `subtask.bash.result.v1` envelope。
4. 保持与普通 `bash` 一致的错误语义。

还没做的是：

1. 流式 bash。
2. 自动分流。
3. 针对长命令的阶段性观察。

#### `url_fetch`

`url_fetch` 不建议在工具内部自动决定进入 subagent。

原因：

- 它只知道“取某个 URL”。
- 它不知道主任务是“读正文”“找第三篇文章”“提取表格”还是“下载文件”。

更合理的做法是：

- 让上层 agent 显式 `spawn` 一个网页抽取子任务。
- 或者以后单独做复合工具，例如 `web_extract`。

---

## 9) 两个典型场景

### 9.1 场景一：`git pull` 这类长命令

目标：

- 命令能长时间运行。
- 过程中可观察。
- 主任务最终拿到简洁结果，而不是整段日志。

建议做法：

1. 已有显式 `bash.run_in_subtask`，可把长命令放进子任务边界。
2. 当前仍是一次性输出；如果要真正做到过程可观察，下一步还是要把 `bash` 改成流式执行器。
3. observer 只保留最近窗口和阶段性摘要。
4. 主 loop 最终只收到通用 envelope。

示例结果：

```json
{
  "status": "done",
  "summary": "git pull completed successfully; 12 files updated",
  "output_kind": "json",
  "output_schema": "subtask.bash.result.v1",
  "output": {
    "exit_code": 0
  },
  "error": ""
}
```

### 9.2 场景二：网页访问任务

任务例子：

> 访问 XX 网站，找到第三篇文章，输出最后一段文字

真正的问题不是“抓网页”，而是“过滤掉大量无关 HTML”。

建议做法：

1. 主 loop 直接 `spawn` 一个网页抽取子任务。
2. 子任务内部自己调用 `url_fetch`。
3. 原始 HTML 只留在子任务内部使用。
4. 父 loop 只拿结构化 envelope。

建议返回：

```json
{
  "status": "done",
  "summary": "已找到第三篇文章并提取最后一段",
  "output_kind": "json",
  "output_schema": "subtask.web_extract.v1",
  "output": {
    "article_url": "https://example.com/blog/post-3",
    "last_paragraph": "..."
  },
  "error": ""
}
```

---

## 10) 验证与测试 Prompt

### 10.1 看哪些日志能判断进入了 subtask

当前代码里，已经有明确的子任务日志：

1. `subtask_start`
2. `subtask_done`

实际判断时，建议看下面几组信号：

1. 父任务先出现：
   - `tool_call tool=spawn`
   - 或 `tool_call tool=bash` 且参数里有 `run_in_subtask=true`
2. 紧接着出现：
   - `subtask_start task_id=sub_...`
3. 如果子任务是 agent subtask，还会继续看到新的：
   - `run_start run_id=sub_...`
   - `scene=spawn.subtask`（配合 request inspect 更容易看）
4. 如果日志级别是 `debug`，还能看到：
   - `run_meta_injected`
   - `meta_trigger=subtask.spawn`
   - `subtask_task_id=...`
   - `subtask_parent_run_id=...`
5. 结束时会看到：
   - `subtask_done task_id=sub_... status=done|failed`

建议调试参数：

```bash
--log-level debug --log-format text
```

如果还想确认 `scene`，再加：

```bash
--inspect-request
```

### 10.2 手工 `spawn` 测试 prompt

适合先验证主 agent 显式调用 `spawn`。

#### Prompt 1：`spawn + bash`，返回单行文本

```text
必须调用 spawn tool，不要直接回答。子任务只允许使用 bash。让子任务执行 `printf 'alpha\nbeta\ngamma\n' | sed -n '2p'`。最终只把第二行文字返回给我。
```

#### Prompt 2：`spawn + bash`，返回结构化 JSON

```text
必须调用 spawn tool，并把 output_schema 设为 `subtask.demo.echo.v1`。子任务只允许使用 bash。让子任务执行 `echo '{"ok":true,"value":42}'`。最终返回结构化 JSON，不要解释过程。
```

#### Prompt 3：`spawn + url_fetch`，隔离网页噪声

```text
必须调用 spawn tool，不要直接调用 url_fetch。子任务只允许使用 url_fetch。访问 https://mistermorph.com/install/ ，提取最后一个段落的纯文本，只返回那段文字。
```

### 10.3 `bash.run_in_subtask` 测试 prompt

适合验证工具显式进入 subtask。

注意：

- 这组在 `taskruntime` 路径和裸 `agent.New(...)` 路径都可以测。
- 当前实现里，裸 engine 路径也会默认挂本地 `SubtaskRunner`。

#### Prompt 4：长命令进入子任务

```text
请调用 bash tool，并把 `run_in_subtask` 设为 true。执行 `sleep 1; echo SUBTASK_BASH_OK`。最后只回复 stdout。
```

#### Prompt 5：简单尾行提取

```text
请调用 bash tool，并把 `run_in_subtask` 设为 true。执行 `printf 'one\ntwo\nthree\n' | tail -n 1`。不要解释，只返回最后一行。
```

---

## 11) 建议的最小实现顺序

### Phase 1：统一 `spawn` 实现

目标：

- 让 `spawn` 不再自己手写一条子 engine 执行链。
- 改为统一走 `SubtaskRunner`。

先做：

- 同步版 `spawn`
- 通用 envelope
- `output_kind` / `output_schema`
- 本地 runner 和 `taskruntime` runner

先不做：

- 异步 API
- 自动工具委托
- 独立 `ArtifactStore`
- 外部进程 backend

### Phase 2：子任务句柄与任务 API

新增：

- `task_get`
- `task_wait`
- `task_cancel`

这一阶段才真正引入异步子任务和任务句柄。

### Phase 3：流式 `bash`

当前已经有：

- 显式 `run_in_subtask`
- direct subtask
- `subtask.bash.result.v1` envelope

下一步仍需要把 `bash` 从一次性收集输出改成：

- stdout/stderr pipe
- 增量事件
- observer 可读

也就是说，当前缺的不是“能不能进子任务”，而是“进子任务后能不能流式观察”。

### Phase 4：网页高噪声场景

新增一个复合方案，二选一：

- 直接让 agent 用 `spawn + url_fetch`
- 或新增复合工具 `web_extract`

重点是：

- 不要让原始 `url_fetch` 承担“理解网页任务目标”的职责。

### Phase 5：可选外部进程 backend

只有在下面需求真的成立时再做：

- 需要更强隔离
- 需要 crash containment
- 需要限制子任务访问主进程内存

这时可以把执行 backend 抽象成：

```go
type SubtaskRunner interface {
    Start(context.Context, SubtaskRequest) (SubtaskHandle, error)
}
```

然后提供：

- `InProcRunner`
- `ProcessRunner`

但默认仍应是 `InProcRunner`。

---

## 12) 风险和约束

### 12.1 递归 spawn

必须限制：

- 最大递归深度
- 最大并发子任务数

否则很容易出现：

- 递归爆炸
- token 成本失控
- 工具竞争

### 12.2 observer 本身变成新的上下文爆炸源

必须避免：

- 定时全量回灌
- 每次事件都调 LLM

### 12.3 原始高噪声内容泄漏到父级

必须避免：

- 原始 HTML、stdout、stderr 自动进入父 loop
- 子任务输出无限增长后继续原样回灌父级

一期做法是：

- 高噪声材料只保留在子任务内部。
- 如无必要，不抽独立 artifact 组件。
- 需要持久化文件时，继续沿用底层 tool 自己的文件输出能力。

### 12.4 同进程资源竞争

要有：

- 并发上限
- 超时
- cancel
- 输出窗口限制

### 12.5 guard / approval 传播

子任务必须继承父任务的 guard 约束。

如果子任务进入 `pending`：

- 状态要能被父级看到。
- 不能把父级卡死在一个没有状态出口的等待里。

---

## 13) 最终建议

归纳成一句话：

> 默认方案应当是“内置 sub runtime + 统一 runner + 通用结果 envelope + 事件驱动观察”，而不是“起一个新的 mistermorph 进程然后靠 stdout/stderr 管理一切”。

更具体地说：

1. 用统一 `SubtaskRunner` 承接 `spawn` 和 direct subtask。
2. 一期先保持 `spawn` 同步，不新增 `mode` 参数。
3. 结果统一收敛到 `output_kind + output_schema + output` 这套 envelope。
4. 再给它补上任务句柄、状态查询和取消。
5. `bash` 已支持显式 direct subtask；长命令下一步优先补流式化。
6. 网页高噪声问题优先通过“子任务隔离上下文”解决，而不是让 `url_fetch` 自己变复杂。
7. observer 必须是事件驱动，不要默认做固定周期 LLM 轮询。

---

## 14) 当前建议对应到代码边界

最小落点建议如下：

- `agent/spawn_tool.go`
  - 手工 `spawn` 前端入口
- `agent/local_subtask_runner.go`
  - 裸 engine 路径的本地子任务执行
- `agent/subtask.go`
  - `SubtaskRequest` / `SubtaskResult` / `SubtaskRunner`
- `internal/channelruntime/taskruntime`
  - channel/runtime 路径的子任务实际执行层
- `internal/daemonruntime`
  - 任务状态模型可供后续异步句柄阶段复用
- `cmd/mistermorph/consolecmd/streaming.go`
  - 后续如要做异步子任务观察，可继续复用 stream hub 思路
- `tools/builtin/bash.go`
  - 当前已支持显式 direct subtask；后续再补流式执行
- `tools/builtin/url_fetch.go`
  - 继续保持底层 fetch 原语，不在工具内部做自动委托

这条路径和现有代码最贴合，改动面也最可控。
