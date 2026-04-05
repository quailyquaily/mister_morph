---
date: 2026-04-05
title: Subagent / 子任务运行时实现进度
status: in_progress
---

# Subagent / 子任务运行时实现进度

## 当前范围

本轮先做一期最小实现：

- `spawn` 从“直接 new 子 engine”切到可注入的 subtask runner。
- `taskruntime.Runtime` 提供同步子任务执行入口。
- `spawn` 返回统一 envelope：
  - `status`
  - `summary`
  - `output_kind`
  - `output_schema`
  - `output`
  - `error`
- `spawn` 支持可选 `output_schema`。
- `bash` 支持显式 `run_in_subtask`。
- 保持 `spawn` 继续同步调用。

本轮明确不做：

- 异步子任务 API
- `execution_mode=auto`
- 独立 `ArtifactStore`
- `bash -> subtask` 的自动分流

## 任务清单

- [x] 建立实现进度文档
- [x] 在 `agent` 层抽出 subtask runner 接口和结果 envelope 类型
- [x] 让 `spawn` 优先走可注入 runner，旧路径作为兜底
- [x] 在 `taskruntime.Runtime` 中实现同步子任务执行
- [x] 为 envelope 和 `spawn` 增加测试
- [x] 为 `spawn.output_schema` 和 `bash.run_in_subtask` 增加测试
- [x] 跑相关测试并记录结果

## 第一轮审阅意见

### 需要修正的问题

1. [x] `spawn` / `bash.run_in_subtask` 传入的工具白名单在 `taskruntime` 路径下会失效。
   - `taskruntime.Run(...)` 会在传入 registry 之上继续注入 runtime tools。
   - 这会让“子任务只允许这些工具”在不同运行时下行为不一致。
   - 这属于 bug。

2. [x] `bash.run_in_subtask` 改变了原有 `bash` 的失败语义。
   - 普通 `bash` 非零退出时，tool 会返回 error。
   - 当前子任务路径会把失败包成 envelope，再作为“成功的 tool 返回值”交给父 agent。
   - 这会破坏上层既有的错误处理分支。
   - 这属于 bug。

3. [x] `output_schema` 契约不稳定。
   - 如果子任务把结构化结果以字符串形式返回，例如 stringified JSON，当前逻辑会把它降级成 `output_kind=text`，并丢掉 `output_schema`。
   - 这会让结构化输出的消费方拿到不稳定结果类型。
   - 这里需要兼容处理：声明了 `output_schema` 时，应优先按 JSON 契约处理，而不是静默降级。

4. [x] `bash` 的 `run_in_subtask` 参数在不同 runtime 中可用性不一致。
   - `taskruntime` 路径能用。
   - `mistermorph run` 这类直接 `agent.New(...)` 的路径没有注入 subtask runner。
   - 结果是 schema 已暴露，但部分入口一用就报错。
   - 这属于 bug。

### 过重的设计

1. [x] `bash -> subtask` 当前实现会再启一轮子 LLM。
   - 父模型已经决定执行哪条 bash 命令。
   - 现在却还会把这条命令包装成 prompt，再让子模型决定去调用一次 bash。
   - 这层额外推理没有提供新的信息，只增加了 token 成本和不确定性。
   - 更合理的做法是：显式 `bash` 子任务直接运行命令，并返回统一 envelope。

2. [x] `SubtaskResultFromFinal(...)` 里加入了过多启发式文本修复。
   - 例如字符串字面量解码、转义换行展开。
   - 这会让结构化输出和文本输出的边界变得含混。
   - 更合理的做法是：
     - 无 `output_schema` 时，按普通文本处理。
     - 有 `output_schema` 时，优先按 JSON 契约处理；字符串则先尝试 parse JSON，失败再明确报契约错误。

## 进度记录

### 2026-04-05

- 新建实现进度文档。
- 已确认现有 `spawn` 仍在 `agent` 包里直接 new 子 engine。
- 已确认 `taskruntime.Runtime` 是最合适的一期复用落点。
- 已在 `agent` 层新增：
  - `SubtaskRequest`
  - `SubtaskResult`
  - `SubtaskRunner`
  - `WithSubtaskRunner(...)`
- 已把 `spawn` 改成：
  - 优先走注入的 `SubtaskRunner`
  - runner 不存在时，保留旧的进程内子 engine 路径作为兜底
  - 无论 runner 路径还是兜底路径，都返回统一 envelope
- 已让 `taskruntime.Runtime` 实现同步 `RunSubtask(...)`，并在 `taskruntime.Run(...)` 创建 engine 时自动注入为 subtask runner。
- 当前 envelope 结构已经落下：
  - `task_id`
  - `status`
  - `summary`
  - `output_kind`
  - `output_schema`
  - `output`
  - `error`
- 当前 `output_schema` 已经入结构，但一期不会自动推导具体 schema id；目前默认保持空字符串，等待后续按具体子任务补。
- 已给 `spawn` 增加可选 `output_schema` 参数：
  - 手工触发子任务时可以显式声明期望的结构化输出 schema id
  - runner 会把这个约束补进子任务 prompt
  - 当子任务最终返回 JSON 输出时，outer envelope 会带上对应 `output_schema`
- 已给 `bash` 增加显式 `run_in_subtask` 参数：
  - `false` 或缺省时，维持原有直接执行路径
  - `true` 时，优先走 direct subtask 路径，直接运行命令并返回 `subtask.bash.result.v1` envelope
  - 如果上下文里有 subtask runner，则由 runner 负责分配 `task_id` 和子任务上下文
  - 如果上下文里没有 subtask runner，也会本地生成 `task_id` 并保持同一套 envelope 语义
  - 当 `bash` 已经运行在子任务内部时，会自动退回直接执行，避免递归进入 subtask
- 已按第一轮审阅意见收敛实现：
  - `taskruntime.RunSubtask(...)` 会显式关闭 runtime tool 注入，保证工具白名单语义稳定
  - `agent.New(...)` 现在默认会挂上本地 subtask runner，不再让 `run_in_subtask` 在裸 engine 路径下失效
  - `spawn` 统一走 subtask runner，不再保留 `spawn` 自己那条手写兜底执行链
  - `bash.run_in_subtask` 不再重启一轮子 LLM，而是直接运行命令并包装 envelope
  - `SubtaskResultFromFinal(...)` 已收敛为：
    - 无 `output_schema` 时，普通文本直接处理
    - 有 `output_schema` 时，优先按 JSON 契约处理，并兼容 stringified JSON
    - JSON 契约不满足时，返回明确失败结果
- 测试结果：
  - `go test ./agent ./internal/channelruntime/taskruntime ./tools/builtin` 通过
  - `go test ./...` 通过

## 当前实现边界

- `spawn` 仍然只支持同步调用，没有 `mode` 参数。
- 还没有异步子任务句柄，也没有 `task_get` / `task_wait` / `task_cancel`。
- 还没有独立 `ArtifactStore`。
- `bash` 只接了显式 `run_in_subtask`；还没有自动分流，也没有专门的流式 bash 子任务观察器。
- `spawn` 和 direct subtask 现在都依赖统一的 subtask runner 契约，但仍然只有同步调用，没有后台任务句柄。
