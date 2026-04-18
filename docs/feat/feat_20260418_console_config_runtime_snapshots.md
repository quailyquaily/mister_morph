---
date: 2026-04-18
title: Console Config Snapshot Runtime Refactor（配置快照驱动的 Runtime 重构）
status: draft
---

# Console Config Snapshot Runtime Refactor（配置快照驱动的 Runtime 重构）

## 1) 背景

当前 Console 这条链路里，`config.yaml`、全局 `viper`、runtime reload 三者是直接耦合的：

- `cmd/mistermorph/consolecmd/agent_settings.go`
  - `PUT /settings/agent` 会写 `config.yaml`
  - 然后立刻把展开后的值写回全局 `viper`
  - 再调用 `localRuntime.ReloadAgentConfig()` 和 `managed.Restart()`
- `cmd/mistermorph/consolecmd/console_settings.go`
  - `PUT /settings/console` 同样会写文件、改全局 `viper`、再触发 runtime 更新
- `cmd/mistermorph/consolecmd/setup_repair.go`
  - 修复配置后也会把值写回全局 `viper`，再手动 reload
- `cmd/mistermorph/consolecmd/local_runtime.go`
  - `ReloadAgentConfig()` 内部重新从全局 `viper` 构建新的 task runtime / guard / MCP bundle
- `cmd/mistermorph/consolecmd/managed_runtime.go`
  - `Start/Restart/UpdateKinds()` 也是直接从全局 `viper` 读取 telegram/slack 配置

这带来几个结构性问题：

1. Web API 同时承担了“持久化配置”和“驱动运行时更新”两种职责。  
2. runtime 不是围绕不可变快照工作，而是在多个阶段反复读取进程级可变配置。  
3. 外部直接修改 `config.yaml` 时，没有一个清晰、统一、自动的快照重建路径。  
4. 并发语义不清晰：运行中的任务、heartbeat、managed runtime 到底看到的是哪一版配置，并没有明确定义。  
5. 全局 `viper` 在运行期仍会被再次写 defaults，已经出现真实 panic。  

与之相对，`integration.Runtime` 已经采用了更合理的模型：

- 它在构造时通过 `loadRuntimeSnapshot(...)` 一次性生成 `runtimeSnapshot`
- 后续运行只读这个 snapshot，不再依赖全局可变配置

因此，Console 侧现在最大的问题不是“有没有 snapshot 思想”，而是 snapshot 边界还没有成为运行时主模型。

## 1.1) 已出现的真实并发 bug

这不是纯架构洁癖问题，当前实现已经有真实 crash。

已知一条调用链是：

- `cmd/mistermorph/registry.go:loadRegistryConfigFromViper()`
  - 运行期再次调用 `configdefaults.Apply(viper.GetViper())`
  - 内部通过 `SetDefault(...)` 写全局 `viper`
- 同时其他 goroutine
  - 例如 `internal/logutil/logutil.go:LogOptionsFromViper()`
  - 正在对同一个全局 `viper` 做 `GetBool/GetInt/...` 读取

当 telegram 与 heartbeat 等 runtime 并发启动时，就可能出现：

- 一边 `SetDefault` 写全局 `viper`
- 另一边读取 logging / runtime 配置
- 最终触发 `concurrent map read and map write`

这说明当前问题不只是“reload 逻辑分散”，而是：

- 全局 `viper` 被当成了运行期共享可变状态
- 且默认值写入并没有严格限制在进程初始化阶段

因此，新方案必须显式解决两件事：

1. 默认值应用只能发生在受控的 snapshot 构建阶段，不能在 runtime 热路径里再次写全局 `viper`。  
2. runtime 运行时读取必须切到只读 snapshot，而不是继续共享读写同一个进程级 `viper`。  

## 2) 设计原则

本次重构建议明确采用以下原则：

1. `config.yaml` 是持久化真相，不是运行时对象。  
2. runtime 只消费“配置快照”，不直接依赖全局 `viper` 的即时值。  
3. 当 `config.yaml` 变化时，系统重新生成一份新快照。  
4. 每个 runtime 自己决定如何原子切换到新快照，并自己保证并发安全。  
5. Console Web API 只负责更新 `config.yaml`，不直接负责更新快照。  
6. 无效配置不能污染当前运行中的最后一份有效快照。  

核心结论就是一句话：

> 配置写入是持久化动作；快照切换是运行时动作；这两者不应该在 HTTP handler 里直接耦合。

## 3) 目标

本方案的目标是：

1. 让 `console serve` 先运行在“配置文件 -> 快照 -> runtime”模型上。  
2. 让 `consoleLocalRuntime`、`managedRuntimeSupervisor`、未来其他长生命周期 runtime 都以 snapshot 为输入。  
3. 让配置变更无论来自 Web API 还是外部编辑器，都走同一条快照重建路径。  
4. 明确运行中任务与新任务对配置版本的可见性规则。  
5. 收缩全局 `viper` 在 Console 进程中的职责，使其主要退回到“进程启动参数 + config 路径发现”。  
6. 消除“运行期再次 `SetDefault` 全局 `viper`”这一类并发 panic 根因。  

这里补一个范围约束：

- 当前主需求是 `console serve` 进程内的配置快照化
- 即“一个进程内有多个 runtime / 子系统消费同一份 snapshot”
- 不是先做一个覆盖所有子命令、所有进程的通用配置总线

## 4) 非目标

本次方案不打算：

1. 重做 `config.yaml` 的字段结构。  
2. 立即消灭整个仓库里所有 `FromViper()` 辅助函数。  
3. 在第一阶段改造所有 CLI 子命令；首要范围是 `console serve`。  
4. 改变外部 `/settings/*` API 的基本输入输出形状。  
5. 在当前阶段引入一个很泛的订阅框架或跨进程配置分发体系。  

## 5) 当前问题的本质

当前模型的问题，不是“reload 不够快”，而是“谁拥有运行时配置”这件事定义错位了。

现在实际上是：

```text
HTTP PUT /settings/*
  -> 写 config.yaml
  -> 改全局 viper
  -> 手动调用 local runtime reload
  -> 手动调用 managed runtime restart
```

更合理的模型应该是：

```text
任何地方修改 config.yaml
  -> 配置快照管理器检测变化
  -> 生成新快照（或记录失败）
  -> 各 runtime 收到/拉取新快照
  -> 各 runtime 自己完成无锁读/原子切换/必要重启
```

也就是说，真正应该稳定的是“文件到快照”的通道，而不是“某个 handler 写完文件以后别忘了再调用哪几个 reload 函数”。

从这个角度看，`registry.go` 里的 panic 只是一个更具体的症状：

- defaults 写入不该发生在运行期
- 运行期也不该继续共享一个可写的全局配置对象

只要这两点不改，哪怕把现有 reload 流程整理得更漂亮，类似竞争条件仍然会反复出现。

## 6) 总体方案

## 6.1 新增统一的配置快照管理层

这里建议先收缩成“每进程一个 manager，进程内多个 consumer”的模型。

对当前需求来说，主要就是 `console serve` 进程内：

- 一个 config manager 负责从 `config.yaml` 生成最新 snapshot
- 多个 consumer 消费这份 snapshot
  - `consoleLocalRuntime`
  - `managedRuntimeSupervisor`

不需要一上来做成通用事件总线。

不过这里有两层语义不能只让 Console 自己定义，而应按 repo 级统一：

- shared defaults 的 authority
- config path 的解析顺序

建议新增一层，例如：

- `internal/configruntime`
- 或 `internal/configsnapshot`

它负责：

1. 按 repo 级统一规则解析配置路径。  
2. 用独立的临时 `viper` 读取并展开 `config.yaml`。  
3. 应用 defaults、环境变量展开、必要的 normalize。  
4. 生成一份不可变的 `ProcessConfigSnapshot`。  
5. 给这份快照分配 `generation`、`loaded_at`、`source_path`、`content_hash`。  
6. 在配置无效时保留“最后一份有效快照”，并记录最近一次加载失败。  

其中第 1 条建议明确固定为：

- 先看显式 `--config` / `config`
- 再看当前工作目录下的 `config.yaml`
- 再看 `~/.morph/config.yaml`

并且要区分两种 mode：

- read mode：三者都不存在时返回空路径，由启动装配层决定是否允许“无配置启动”
- write mode：三者都不存在时回落到当前工作目录下的 `config.yaml`，作为新文件落点

其中第 3 条也建议明确 authority：

- shared defaults 的唯一 authority 应该是 `internal/configdefaults.Apply`
- `integration.ApplyViperDefaults` 如果保留，应只作为 isolated reader 的薄包装，并先调用 `internal/configdefaults.Apply`
- Console 内部不应再额外发明一套 defaults 语义

建议接口形态：

```go
type ProcessConfigSnapshot struct {
    Generation  uint64
    LoadedAt    time.Time
    SourcePath  string
    ContentHash string

    Console       ConsoleSnapshot
    Agent         AgentRuntimeSnapshot
    Managed       ManagedRuntimeSnapshot
}

type SnapshotConsumer interface {
    ApplySnapshot(context.Context, ProcessConfigSnapshot) error
}

type ConfigManager interface {
    Current() ProcessConfigSnapshot
    Reload(context.Context) error
    LastError() error
    Close() error
}
```

这里建议直接避免把 `Reader` 暴露到 snapshot 对外结构里。

也就是说：

- 加载路径内部可以临时使用独立 `viper` 或其他 reader
- 但产出的 `ProcessConfigSnapshot` 应该已经是 typed snapshot
- runtime consumer 不能回退去读一个隐藏的配置 reader

否则只是“把全局 `viper` 换成 snapshot 内部 reader”，复杂度降了，但边界还是不干净。

也就是说，Console 里的 manager 可以先只服务 `console serve`，但它不应该再自己定义另一套 config path/defaults 规则；这两层应与 repo 其他入口共享。

当前需求下，manager 的分发语义也可以简单一点：

- manager 持有固定的一组 consumer
- reload 成功后，在进程内按确定顺序 fan-out 调用 `ApplySnapshot(...)`
- 当前先不暴露通用 `Watch/Subscribe` 接口

这样更符合当前问题规模，也更容易测试。

## 6.2 用 watcher 驱动快照重建

既然目标是“当 `config.yaml` 变化时就生成新快照”，那就不应该只依赖 Web API 这一个入口。

建议在 `console serve` 内部引入文件变更监听：

1. watcher 绑定的是 canonical config resolver 语义，而不是某一次启动时碰巧命中的单一路径。  
2. 如果显式指定了 `--config`，就监听该路径。  
3. 如果没有显式路径，就按 `./config.yaml` -> `~/.morph/config.yaml` 的候选顺序解析当前 source，并在 source 切换时重新绑定 watcher。  
4. 使用 debounce + content hash 去抖。  
5. 文件变化后重新加载 snapshot。  
6. 解析成功则发布新 generation。  
7. 解析失败则保留旧 generation，并暴露错误状态。  

实现上可以用：

- `fsnotify`
- 再配合一次内容 hash 校验，避免 editor 的 rename/write 行为造成重复应用

这样才真正满足：

- Console Web API 改配置只需要写文件
- 用户手工编辑 `config.yaml` 也能触发同样的效果
- 启动时没有配置文件、之后再创建 `config.yaml` 或 `~/.morph/config.yaml` 的场景也不会漏掉

## 6.3 每个 runtime 自己持有快照

这里不建议继续让 manager 直接“替 runtime 决定怎么 reload”。

建议改成：

- manager 负责生产 snapshot，并向本进程内 consumer 顺序分发
- runtime 负责消费 snapshot，并自行保证切换时的并发安全

换句话说：

- snapshot manager 负责“新配置长什么样”
- runtime 负责“我怎么安全地切过去”

这和你提出的原则是一致的。

## 7) Runtime 侧改造建议

## 7.1 `consoleLocalRuntime`

当前 `consoleLocalRuntime.ReloadAgentConfig()` 的问题是：

- 它不是基于显式 snapshot 参数重建
- 而是内部再去读取全局 `viper`

建议改造成：

```go
func (r *consoleLocalRuntime) ApplySnapshot(ctx context.Context, snap AgentRuntimeSnapshot) error
```

其中 `AgentRuntimeSnapshot` 应至少包含：

- task runtime bootstrap 所需配置
- guard 配置
- runtime tools 配置
- MCP / ACP / skills / log 配置
- heartbeat 相关运行参数
- 默认 provider/model 视图

切换策略建议是：

1. 先用新 snapshot 在堆上完整构建新 bundle。  
2. 构建成功后再原子替换当前 bundle。  
3. 替换完成后清理旧 bundle 的 MCP host 等资源。  
4. 运行中的 task 继续持有旧 bundle 直到完成；新 task 使用新 bundle。  

这会形成清晰语义：

- in-flight task 看到旧 generation
- 新提交 task 看到新 generation

这是最稳妥的 snapshot 语义。

## 7.2 `managedRuntimeSupervisor`

当前 supervisor 的问题是更明显的：

- `Start/Restart/UpdateKinds()` 读取全局 `viper`
- telegram/slack 的 token、allowlist、trigger mode、agent runtime 配置都混在一起
- handler 还需要显式决定调用 `Restart()` 还是 `UpdateKinds()`

建议拆成两层：

1. `managedRuntimeSupervisor.ApplySnapshot(snap ManagedRuntimeSnapshot)`  
2. 每个 child runtime 自己维护自己的 active snapshot / run generation

`ManagedRuntimeSnapshot` 至少应包含：

- `console.managed_runtimes`
- 每个 kind 的启停所需 transport 配置
- 每个 kind 的 task runtime / guard / tools / llm 相关配置

supervisor 的职责应该变成：

1. 对比旧 snapshot 和新 snapshot。  
2. 识别：
   - 新增 kind
   - 删除 kind
   - 同 kind 配置是否变化
3. 针对变化的 child 做最小化重建或重启。  

这里建议明确两种配置变化：

1. transport 变化  
   - 如 token、poll/socket 参数、allowed IDs、group trigger mode  
   - 需要重启 child loop
2. task-execution 变化  
   - 如 llm、tools、guard、skills  
   - 可以通过 child 内部 bundle snapshot 切换解决

如果第一阶段不想做那么细，也可以先统一为“child 全量重启”，但接口边界应先设计成 snapshot 驱动，避免以后继续和 `viper` 绑死。

## 7.3 其他长生命周期逻辑

以下逻辑也应逐步从“读全局 `viper`”切到“读 runtime snapshot”：

- heartbeat loop
- `routesOptions().Overview(...)` 中的配置可见性
- setup repair 之后的运行时可见状态
- health / diagnostics 页面对当前 generation 的展示

## 8) Web API 职责重划

重构后，`PUT /api/settings/agent`、`PUT /api/settings/console`、setup repair 的职责都应该收缩为：

1. 读取当前 `config.yaml` 文档  
2. 合并 patch
3. 校验 YAML / 结构有效性
4. 写回 `config.yaml`
5. 返回持久化结果

不再负责：

1. `viper.Set(...)`
2. `localRuntime.ReloadAgentConfig()`
3. `managed.Restart()`
4. `managed.UpdateKinds(...)`

这样做的直接收益是：

- handler 变成纯粹的“配置编辑器”
- runtime 更新链路统一收敛到 watcher + snapshot manager
- setup repair / console settings / agent settings 不会再各自复制一套 reload 逻辑

## 9) 全局 `viper` 的收缩方向

本次不一定要一口气删掉全局 `viper`，但需要把它从“运行时事实来源”降级为“启动期输入”。

建议目标状态如下：

### 启动期允许使用全局 `viper`

- 解析 `--config`
- 启动时读取第一版 `config.yaml`
- 解析 `console serve` 的基础启动参数

### 运行期尽量不再使用全局 `viper`

- `consoleLocalRuntime`
- `managedRuntimeSupervisor`
- managed child runtime
- settings / repair 之后的 reload 路径

长期方向是：

- `FromViper()` 只保留在启动装配层
- runtime 内统一改成 `FromReader()` 或直接吃 typed snapshot

## 10) 状态可观测性

如果 Web API 不再同步触发 reload，Console 需要补一个简单但明确的可观测性模型。

建议在 health/overview 中先暴露：

- `config_generation`
- `config_loaded_at`
- `config_source_path`
- `config_last_error`
- `config_last_error_at`
- `consumers.local.applied_generation`
- `consumers.local.last_error`
- `consumers.managed.applied_generation`
- `consumers.managed.last_error`

如果后续需要更细粒度，再继续扩展到 per-kind 状态，例如 `telegram/slack/line/lark` 各自的 generation。

这里不建议继续保留单一的 `applied_generation` 字段。

原因很直接：

- manager 是顺序 fan-out 给多个 consumer
- local runtime 和 managed runtime 可能不会在同一时刻完成切换
- 一旦出现部分 apply 成功、部分 apply 失败，单一字段就无法表达真实状态

这样前端可以区分三种状态：

1. 配置已写入且已生效  
2. 配置已写入但新快照尚未应用  
3. 配置已写入但快照生成失败，当前仍运行在旧快照上  

这对于“Web API 只写文件”的模型很重要，否则用户会误以为写入成功就等于运行时已生效。

## 11) 迁移步骤

建议按下面的顺序改，风险最小。

### Phase 1：引入快照管理器，但先不改外部行为

1. 新增 repo 级共享的 config path resolver，明确 read/write 两种 mode 和 `--config` -> `./config.yaml` -> `~/.morph/config.yaml` 的顺序。  
2. 新增 `ConfigManager` 和 `ProcessConfigSnapshot`。  
3. 启动时加载第一版 snapshot。  
4. 把 shared defaults authority 收敛到 `internal/configdefaults.Apply`；`integration.ApplyViperDefaults` 若保留，则先调用前者。  
5. manager 内先挂 `consoleLocalRuntime` 与 `managedRuntimeSupervisor` 两个 consumer。  
6. 把 `cmd/mistermorph/registry.go` 那条 `configdefaults.Apply(viper.GetViper())` 调用链移回主启动路径，只在进程初始化阶段执行一次。  
7. runtime 路径后续彻底不再碰 `SetDefault(...)` 或其他全局 defaults 写入。  
8. 加入 watcher，但先只做日志和状态记录。  
9. 在此基础上，暂时保留现有 handler 中的 `viper.Set + reload` 逻辑，确保行为不变。  

目标：

- 先把“统一快照生成”做出来
- 先统一 defaults authority 和 config path 语义
- 先消掉已知的全局 `viper` 并发写 panic
- phase 1 的最小可测成果是：运行期不再存在 `configdefaults.Apply(viper.GetViper())`
- 避免一上来同时改配置加载和 runtime 生命周期

### Phase 2：让 `consoleLocalRuntime` 支持 `ApplySnapshot`

1. 把 `ReloadAgentConfig()` 改造成基于显式 snapshot 的重建函数。  
2. bundle 改为原子替换。  
3. task 执行路径显式绑定提交时拿到的 bundle。  

目标：

- local runtime 先脱离全局 `viper`

### Phase 3：让 `managedRuntimeSupervisor` 支持 `ApplySnapshot`

1. supervisor 从 snapshot diff 决定 child 的启停。  
2. child runtime 不再直接读全局 `viper`。  
3. 把 `UpdateKinds()` / `Restart()` 逐步收缩为内部实现细节。  

目标：

- managed runtime 也改为 snapshot 驱动

### Phase 4：删除 Web API 中的运行时副作用

1. 删除 `agent_settings.go` 中的 `viper.Set + ReloadAgentConfig + managed.Restart`。  
2. 删除 `console_settings.go` 中的 `viper.Set + UpdateKinds/ReloadAgentConfig`。  
3. 删除 `setup_repair.go` 中的对应 reload 链路。  
4. watcher 检测到文件变化后统一发布新 snapshot。  

目标：

- 完成职责切分

### Phase 5：清理与测试回收

1. 调整依赖全局 `viper` 的单元测试。  
2. 给 snapshot generation / watcher / apply semantics 补测试。  
3. 视情况新增显式“重新加载配置”命令或调试入口，仅作为辅助，不作为主路径。  

## 12) 测试建议

至少补以下测试：

1. `config.yaml` 变化后会产生新的 snapshot generation。  
2. 环境变量展开发生在 snapshot 构建阶段，而不是 handler 的特殊逻辑里。  
3. 无效 YAML 不会替换当前有效 snapshot。  
4. `consoleLocalRuntime` 切换 snapshot 时，旧任务继续跑，新任务用新配置。  
   这条不能只靠纯单元测试，至少要有一个集成测试，跑真实 goroutine 和 bundle 切换时序。  
5. `managedRuntimeSupervisor` 能正确处理 kind 增删与配置变更。  
6. `PUT /settings/*` 成功后，即使不直接调用 reload，最终也能通过 watcher 生效。  
7. 并发启动 telegram / heartbeat / registry / logging 读取路径时，不会再因为运行期 `SetDefault` 全局 `viper` 而触发 data race 或 `concurrent map read and map write`。  

## 13) 后续扩展（暂不纳入当前需求）

以下方向可以保留为后续扩展，但不建议现在一起做：

1. 让单独启动的 `telegram` / `slack` / `line` / `lark` 进程也统一接入相同的 config manager 形态。  
2. 暴露通用 `Watch/Subscribe` 能力，供进程内更多组件订阅 snapshot 变化。  
3. 在观测面板里按 `local / telegram / slack` 分别展示更细粒度的 `runtime_generation`。  
4. 把目前 console 优先的 snapshot 结构继续抽象成更通用的跨命令配置运行时框架。  

## 14) 风险与取舍

### 风险 1：watcher 语义在不同编辑器下不稳定

控制方式：

- 用 `fsnotify` + debounce + content hash
- 不把单次 event 当成可信信号，只把“文件内容确实变化”当成可信信号

### 风险 2：从同步生效变成异步生效后，前端体验会变化

控制方式：

- 暴露 generation/status
- 前端在写入后短轮询配置状态，直到 generation 更新或返回加载失败

### 风险 3：运行中的 bundle 切换容易造成资源泄漏

控制方式：

- 统一 bundle 生命周期
- 所有可关闭资源只挂在 bundle 上
- 原子替换后集中关闭旧 bundle

## 15) 推荐结论

我认同这次重构方向，而且建议按“配置快照化”来做，而不是继续在现有 handler 上堆更多 reload 分支。

一句话总结：

- `config.yaml` 是持久化状态
- snapshot 是运行时状态
- Web API 只改前者
- runtime 自己安全地切后者

这会让 Console 的配置模型和 `integration.Runtime` 现有的 snapshot 思路对齐，也会让后续 managed runtime、desktop、setup repair、外部手工编辑配置这些场景都收敛到同一条路径上。
