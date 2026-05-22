---
date: 2026-05-21
title: Console 前端性能优化计划
status: draft
---

# Console 前端性能优化计划

## 1) 目标

Console 前端优化只解决当前真实问题：

1. 页面切换时减少无关 API 请求。
2. 高频输入不触发整页重算。
3. 大列表和大 Markdown 不一次性渲染。
4. warm enter 明显快于 cold enter。
5. 性能改动必须可测量。
6. UI 质量问题持续小批量修复。

不做这些事：

1. 不引入 TanStack Query、Vue Query 或同类 query 框架。
2. 不做完整 local-first sync engine。
3. 不做大型主题系统或设计系统重写。
4. 不把所有页面数据放进 Pinia。
5. 不写通用虚拟列表系统，除非两个以上页面真实需要。
6. 不把 AI 能力都塞进一个通用聊天框。

## 2) 通用标准

每个性能任务都必须满足：

1. 有 baseline。
2. 有 before/after。
3. 有可复现场景。
4. 有失败标准。
5. 没有达标时，不继续叠新优化。

开发约束：

1. 采集代码只在 dev 或显式 debug 参数下启用。
2. 生产路径不能因为采集变慢。
3. Pinia 只放共享 UI 状态、会话状态和跨页面复用的小型资源。
4. 页面本地接口数据留在页面内，除非多个页面真实复用。
5. 展示型大数组优先用 `shallowRef`。
6. 不使用深层 watcher。
7. 不在输入路径上的 computed 里反复 `JSON.stringify` 或 `JSON.parse`。

## 3) 指标定义

| 指标 | 含义 | 采集方式 |
| --- | --- | --- |
| `route_interactive_ms` | 路由切换到页面可操作的近似时间 | route enter mark 到 router afterEach 后第一个 `requestAnimationFrame`；页面级 loading 结束口径后续单独补 |
| `input_to_paint_ms` | 输入事件到下一帧绘制的时间 | input mark 到下一次 `requestAnimationFrame` |
| `api_request_count` | 页面切换或交互触发的 API 请求数 | fetch wrapper dev counter |
| `api_request_count_by_source` | 请求来源分布，区分 `bootstrap`、`setup-readiness`、`shared-preload`、`page` | fetch wrapper dev counter |
| `long_task_count` | 超过 50ms 的 main-thread task 数 | `PerformanceObserver` longtask |
| `long_task_total_ms` | long task 总耗时 | longtask duration 求和 |
| `long_animation_frame_count` | 超过 50ms 的 animation frame 数 | `PerformanceObserver` long-animation-frame |
| `long_animation_frame_total_ms` | long animation frame 总耗时 | long-animation-frame duration 求和 |
| `markdown_mounted_count` | 页面打开时挂载的 Markdown renderer 数 | `MarkdownContent` dev counter |
| `markdown_render_ms_p95` | 单个 Markdown block 渲染耗时 p95 | renderer update 前后 mark |
| `component_update_count` | 指定交互触发的组件更新数 | Vue devtools performance 或 dev counter |
| `component_update_total_ms` | 指定组件 update 总耗时 | 组件 `onBeforeUpdate` 到 `onUpdated` |
| `component_update_max_ms` | 指定组件单次 update 最大耗时 | 组件 `onBeforeUpdate` 到 `onUpdated` |
| `snapshot_build_count` | Settings/TODO 输入时 snapshot builder 调用次数 | dev counter |
| `dom_row_count` | 当前页面实际 DOM 行数 | dev counter |
| `js_heap_delta_mb` | 场景执行后的 JS heap 增量 | browser memory API 或 performance profile |
| `bundle_chunk_kb` | 相关入口或 route chunk 体积 | `pnpm build` 输出 |

## 4) 测试场景

至少保留三组测试数据：

1. Small：20 条 chat history，5 条 agent Markdown。
2. Medium：100 条 chat history，40 条 agent Markdown，3 条 artifact。
3. Large：500 条 logs 或 workspace rows；真实数据不足时用 fixture。

每组都区分：

1. cold enter：首次进入，chunk 和数据都未加载。
2. warm enter：相关 chunk 或小资源已缓存。

## 5) 当前采集用法

开发环境打开页面时加 `?perf=1`，性能状态挂在：

```js
window.__MISTERMORPH_PERF__
```

复制全量日志：

```js
copy(JSON.stringify(window.__MISTERMORPH_PERF__, null, 2))
```

只复制当前 route、请求、Markdown 和组件 update：

```js
copy(JSON.stringify({
  route: window.__MISTERMORPH_PERF__.currentRoute,
  requests: window.__MISTERMORPH_PERF__.requests.slice(-20),
  markdown: window.__MISTERMORPH_PERF__.markdown,
  components: window.__MISTERMORPH_PERF__.components
}, null, 2))
```

测试输入路径前，先清计数：

```js
const p = window.__MISTERMORPH_PERF__;
p.inputs.length = 0;
p.componentUpdates.length = 0;
p.components = {};
p.snapshots = {};
p.currentRoute.component_update_count = 0;
p.currentRoute.component_update_total_ms = 0;
p.currentRoute.component_update_max_ms = 0;
p.currentRoute.snapshot_build_count = 0;
```

输入后复制：

```js
copy(JSON.stringify({
  inputs: window.__MISTERMORPH_PERF__.inputs,
  snapshots: window.__MISTERMORPH_PERF__.snapshots,
  components: window.__MISTERMORPH_PERF__.components,
  componentUpdates: window.__MISTERMORPH_PERF__.componentUpdates,
  route: window.__MISTERMORPH_PERF__.currentRoute
}, null, 2))
```

请求来源定义：

1. `bootstrap`：鉴权、setup integrity、endpoint 列表等路由守卫需要的请求。
2. `setup-readiness`：判断 setup 是否 ready 的请求。每次浏览器刷新后允许发生一次，同一 tab 内页面切换不应重复。
3. `shared-preload`：contacts、persona 这类小型共享资源预热。
4. `page`：当前页面自己声明或直接需要的资源。

判断页面是否有无关请求时，主要看当前 route 的 `page` 请求。`bootstrap` 和 `shared-preload` 要单独评估，不要混进页面本身成本。

组件 update 数据结构：

```json
{
  "update_count": 3,
  "total_update_ms": 1.2,
  "max_update_ms": 0.7,
  "last_update_ms": 0.2
}
```

现在已经采集的重点组件：

1. `chat.history_list`
2. `chat.history_item`

开发环境还打开了 Vue 自带 performance marks。需要更细的 render / patch 信息时，用 Chrome Performance 面板看 Vue marks。

## 6) 任务

### Task 1: 增加 dev-only 性能采集

当前状态：已完成打样。

Checklist：

- [x] 路由切换记录 `route_interactive_ms`。
- [x] fetch wrapper 记录请求数量、耗时、endpoint、uri。
- [x] 请求按 `bootstrap`、`setup-readiness`、`shared-preload`、`page` 分类。
- [x] 输入事件记录 `input_to_paint_ms`。
- [x] 支持 long task 和 long animation frame 采集。
- [x] Markdown mount/update 有计数和耗时。
- [x] TODO / Settings snapshot builder 有计数。
- [x] 重点组件有 update 次数和耗时。
- [x] 生产构建没有可见采集 UI。
- [x] `pnpm build` 已验证通过。

做什么：

1. 增加开发环境性能计数器。
2. 采集 route enter、fetch 请求数、long task、long animation frame、Markdown mount/update、Settings/TODO snapshot 调用次数。
3. 结果可以先输出到 console 或轻量面板。

怎么做：

1. 在 fetch wrapper 记录 endpoint、uri、route、触发时间。
2. 在 router after enter 记录 route 可操作时间。
3. 在输入处理路径记录 input 到下一帧 paint。
4. 在 Markdown 和 snapshot builder 入口增加 dev counter。
5. 对 `long-animation-frame` 做 feature detection，不支持时不报错。

验收标准：

1. 打开 Chat、TODO、Settings 时，能看到 `route_interactive_ms` 和 `api_request_count`。
2. 输入 Chat composer、TODO editor、Settings field 时，能看到 `input_to_paint_ms`。
3. 支持 LoAF 的浏览器能看到 `long_animation_frame_count`。
4. 生产构建不显示采集 UI，不主动输出采集日志。

失败标准：

1. 采集代码进入生产可见路径。
2. 不支持 LoAF 的浏览器报错。
3. 采集本身产生超过 50ms 的 long task。

### Task 2: 页面资源按需请求

当前状态：已完成打样。

Checklist：

- [x] setup readiness 每次浏览器刷新后重新检查一次。
- [x] setup readiness 在同一 tab 页面切换时复用缓存。
- [x] TODO 进入不请求 `/state/files`。
- [x] TODO 进入不请求 `/audit/files`。
- [x] TODO 进入不请求 `/tasks?limit=20`。
- [x] contacts 和 persona 走 Pinia 小型共享资源缓存。
- [x] shared preload 不计入页面自身 `page` 请求。
- [x] endpoint 不同时不复用旧 endpoint 的 contacts/persona store 状态。
- [x] Settings 只加载当前 section 需要的数据。
- [x] `/settings/agent` 不预加载 persona、console、desktop settings。
- [x] `pnpm build` 已验证通过。

做什么：

1. 每个页面声明自己需要的资源。
2. 切换侧栏时只请求当前页面资源。
3. contacts/persona 作为小型共享资源，在进入应用后预热，进入对应页面时刷新。
4. setup readiness 放 session storage，每次刷新页面重新获取，不设 TTL。

怎么做：

1. 保留 Pinia 管 endpoint、auth、locale、contacts summary、persona summary。
2. 页面本地资源继续用 `core/resources` 显式加载。
3. endpoint 切换时清理旧 endpoint 的页面本地数据。
4. route preload 只做 contacts/persona/setup readiness 这类小资源，不预加载大列表。

验收标准：

1. 刷新后首次访问时 setup readiness 请求发生 1 次。
2. 同一浏览器 tab 内切换页面时，setup readiness 请求发生 0 次。
3. TODO 页面不请求 `/state/files`，除非页面真实展示该数据。
4. contacts/persona 预热后，被 TODO、Overview 或 Settings 复用时不重复请求。
5. 任意侧栏切换的无关 API 请求数为 0。
6. `/settings/agent` 的 page 请求只包含 agent settings 和当前可见卡片需要的辅助请求。

失败标准：

1. 进入 TODO 仍请求 audit、tasks list 或 state files 等无关资源。
2. endpoint 切换后显示旧 endpoint 数据。
3. 为缓存引入新的全局数据垃圾场。

### Task 3: Chat 输入和历史渲染隔离

当前状态：已完成打样。

Checklist：

- [x] `ChatView` 拆出 `ChatHistoryList`。
- [x] `ChatView` 拆出 `ChatHistoryItem`。
- [x] Chat history 大数组使用 `shallowRef`。
- [x] composer 输入不触发 `chat.history_list` update。
- [x] composer 输入不触发 `chat.history_item` update。
- [x] topic 切换时 list 只因整体 history 替换 update。
- [x] Markdown render-ready 状态下沉到单个 `ChatHistoryItem`。
- [x] 稳定 history item 使用 `v-memo`。
- [x] 不拆无实际收益的 agent/user 包装组件。
- [x] `pnpm build` 已验证通过。

做什么：

1. 把 Chat 历史列表拆成稳定子组件。
2. composer 输入只更新 composer 自己。
3. 历史项只在自身状态、文本、计划、activity、展开态或 copied 状态变化时更新。

怎么做：

1. 拆出 `ChatHistoryList` 和 `ChatHistoryItem`。
2. `ChatView` 保留 route、endpoint、submit、stream/polling、页面级状态。
3. 大历史数组使用 `shallowRef`，列表项按整体替换更新。
4. 对稳定历史项使用 `v-memo`。
5. Markdown render-ready 状态保留在单个 `ChatHistoryItem` 内部，不放在父组件共享对象里。
6. 不拆 `ChatAgentMessage` / `ChatUserMessage`，除非它们持有独立状态或能减少可测 update。

验收标准：

1. Medium 场景下，Chat composer typing 的 `input_to_paint_ms` p95 小于 32ms。
2. composer 输入时，已有历史项更新数为 0。
3. 轮询一个 task 时，无关历史项更新数为 0。
4. Chat warm enter / Medium 的 `route_interactive_ms` 比 baseline 降低 30% 以上，或小于 700ms。

失败标准：

1. 输入 composer 仍触发历史列表更新。
2. stream 更新一个 item 时，无关 item 更新。
3. 为拆组件增加无实际价值的薄包装组件。

### Task 4: Markdown 可见区渲染

当前状态：已实现，并已用当前 Chat 样本验证。代码侧已经让非 streaming 的 agent Markdown 进入视口附近后再挂载；streaming 消息仍立即挂载。当前样本只有 8 条历史消息，不足以代表 Medium/Large。

Checklist：

- [ ] 记录 Chat Medium 进入时的 `markdown.mounts`、`markdown.updates`、`long_task_count` baseline。
- [x] 用 `IntersectionObserver` 判断旧 agent 消息是否接近视口。
- [x] 非 streaming 的 offscreen Markdown 不立即挂载 renderer。
- [x] 当前 streaming 消息保持及时渲染。
- [x] offscreen 占位有稳定高度或近似高度。
- [x] 滚动到旧消息后内容完整渲染。
- [x] 当前样本滚动没有明显跳动。
- [ ] 记录 before/after 指标。
- [ ] 收益低于 10% 或滚动不稳定时回退。

做什么：

1. 非 streaming 的旧 agent 消息进入视口附近后再挂载 Markdown renderer。
2. 当前 streaming 消息保持及时渲染。
3. 远离视口的重 renderer 可以释放，保留稳定高度或近似高度。

怎么做：

1. 用 `IntersectionObserver` 判断消息是否接近视口。
2. 非可见消息显示稳定占位。
3. 非 streaming 消息按内容 hash 缓存渲染结果。
4. streaming 消息不走延迟挂载。

验收标准：

1. Chat enter / Medium 的 `markdown_mounted_count` 不超过可见 agent 消息数 + 3。
2. Chat enter / Medium 的 `long_task_count` 不超过 2。
3. 单个 long task 不超过 100ms。
4. Streaming message 的 visible text update delay p95 小于 120ms。
5. 滚动到旧消息后，Markdown 内容最终和完整渲染一致。

失败标准：

1. 旧消息滚动进入视口后内容丢失。
2. streaming 输出明显延迟。
3. 占位高度导致明显滚动跳动。

### Task 5: TODO 改成本地 draft 编辑

当前状态：已实现并完成输入路径验证。当前证据：TODO 输入时 `snapshots` 为空，`snapshot_build_count` 为 0；代码侧已经移除 `TodoView` 内的 `snapshotTasks` 和 `todo.tasks` 计数点，普通输入路径不能再触发整表 snapshot。

Checklist：

- [x] 记录 TODO 输入 baseline：`input_to_paint_ms`、`snapshots.todo.tasks`、组件 update。
- [x] 选择 task 时创建 `selectedTaskDraft`。
- [x] 编辑器只修改 draft，不直接改 `tasks` 列表。
- [x] 保存时 normalize draft 并写回列表。
- [x] 切换 task 时把 draft 写回本地未保存列表，避免丢失编辑。
- [x] `snapshotTasks` 不在普通输入路径调用。
- [x] 保存前仍执行整表校验。
- [x] mention suggestions 复用 contacts cache。
- [x] TODO 输入后 `snapshots.todo.tasks` 降为 0。
- [x] 记录 before/after 指标。

做什么：

1. 选择 task 时创建 `selectedTaskDraft`。
2. 编辑器只修改 draft，不直接改列表里的 task。
3. 保存或切换 task 时再 normalize 并写回列表。
4. 整表校验只在保存前执行。

怎么做：

1. 拆出 `TodoTaskList` 和 `TodoTaskEditor`。
2. 左侧列表只接收稳定 task summary。
3. 编辑区维护本地 draft。
4. `snapshotTasks` 不在输入路径调用。

验收标准：

1. TODO editor typing / 100 tasks 的 `input_to_paint_ms` p95 小于 32ms。
2. TODO editor typing 时 `snapshotTasks` 调用数为 0。
3. TODO editor typing 时左侧无关 task row 更新数为 0。
4. 保存前仍能阻止空标题、非法状态、非法 owner 等无效任务。
5. mention suggestions 继续复用 contacts cache，不新增 contacts 请求。

失败标准：

1. 输入一个字段仍扫描整张 task 表。
2. 切换 task 时丢失未保存提示。
3. 无效任务可以保存。

### Task 6: Settings 按 section 计算 dirty

当前状态：已实现并完成 LLM 输入路径验证。代码侧已经删除 `settings.agent` / `settings.console` 组合 snapshot，dirty 状态改为各 section 独立 ref，只在对应 section 变更时更新。Settings 数据加载也改为按当前 section 触发。

Checklist：

- [x] 记录 Settings 输入 baseline：`input_to_paint_ms` 和各 snapshot builder 调用次数。
- [x] 为 LLM section 保存独立 loaded snapshot。
- [x] 为 multimodal section 保存独立 loaded snapshot。
- [x] 为 skills section 保存独立 loaded snapshot。
- [x] 为 tools section 保存独立 loaded snapshot。
- [x] 为 managed runtimes section 保存独立 loaded snapshot。
- [x] 为 Telegram section 保存独立 loaded snapshot。
- [x] 为 Slack section 保存独立 loaded snapshot。
- [x] 为 guard section 保存独立 loaded snapshot。
- [x] 为 persona section 保存独立 loaded snapshot。
- [x] 删除 snapshot builder 内部 JSON parse/stringify 嵌套。
- [x] LLM 输入时无关 section snapshot 调用数为 0。
- [x] `/settings/agent` 进入时不构建 hidden section snapshot。
- [ ] 其他 Settings section 输入时无关 section snapshot 调用数为 0。
- [ ] 保存、恢复原值、endpoint 切换时 dirty 状态正确。
- [x] 记录 LLM 输入 before/after 指标。

做什么：

1. 按 LLM、multimodal、skills、tools、runtimes、Telegram、Slack、guard、persona 拆 loaded snapshot。
2. 输入时只计算当前 section 或明确相关 section。
3. 删除 snapshot builder 内部的 JSON parse/stringify 嵌套。

怎么做：

1. 每个 section 保存 normalized loaded snapshot。
2. 每个 section 维护自己的 current state。
3. Save button 汇总 section dirty 状态。
4. endpoint 切换时重置全部 section 状态。

验收标准：

1. Settings field typing 的 `input_to_paint_ms` p95 小于 32ms。
2. 输入 LLM 字段时，无关 section snapshot 调用数为 0。
3. 当前 section snapshot 每次输入不超过 1 次。
4. Save button dirty 状态在修改、保存、恢复原值三种情况下都正确。
5. 切换 endpoint 后，loading、read-only、404 错误状态仍正确。

失败标准：

1. 输入一个 section 字段触发其他 section snapshot。
2. 保存后 dirty 状态错误。
3. 404 endpoint 显示可编辑表单。

### Task 7: 长列表先验证 CSS 渲染隔离

当前状态：已实现，并已用当前真实样本做一次浏览器验证。代码侧已经在 Logs row、Audit group、Workspace tree row 上加 `content-visibility: auto` 和保守的 `contain-intrinsic-size`。当前 Logs 样本只有 242 行，Audit 当前样本没有分组，Workspace tree 当前样本没有行，还不能替代 Large 验证。

Checklist：

- [ ] 记录 Logs Large baseline。
- [ ] 记录 Audit Large baseline。
- [ ] 记录 Workspace tree Large baseline。
- [x] 只对行高相对稳定区域试 `content-visibility: auto`。
- [x] 设置接近真实高度的 `contain-intrinsic-size`。
- [ ] 验证滚动条高度稳定。
- [ ] 验证键盘导航不受影响。
- [ ] 验证可访问性树保留可见内容。
- [ ] 收益低于 10% 或出现滚动问题时回退。
- [ ] 记录 before/after 指标。

做什么：

1. 优先在 Logs row、Audit group、Workspace tree section 上试 `content-visibility: auto`。
2. 设置接近真实高度的 `contain-intrinsic-size`。
3. Chat Markdown 不先套用，等可见区渲染完成后再评估。

怎么做：

1. 先记录 Large 场景 layout/paint baseline。
2. 只对行高相对稳定的区域加 CSS。
3. 对滚动、键盘定位和可访问性做检查。

验收标准：

1. Large 场景下 offscreen 内容变更时，layout/paint 时间比 baseline 降低 20% 以上。
2. 滚动时没有明显跳动。
3. 可访问性树没有丢失当前可见内容。
4. 如果收益低于 10% 或引入滚动问题，回退该 CSS 改动。

失败标准：

1. 滚动条高度明显不稳定。
2. 可见内容无法被屏幕阅读器或键盘访问。
3. 为 CSS 隔离加入复杂 JS 补丁。

### Task 8: 必要时才做虚拟列表

当前状态：当前真实样本不做虚拟列表。先记录真实 DOM 行数和滚动耗时；没有超过 500 行，或 CSS 渲染隔离已经达标，就不做虚拟列表。

Checklist：

- [x] 记录 logs、chat history、workspace tree、TODO tasks 的 DOM row count。
- [ ] 确认真实超过 500 行。
- [ ] 确认 CSS 渲染隔离收益不足。
- [ ] 单页面先实现最小方案。
- [ ] 行高稳定列表优先固定高度虚拟列表。
- [ ] 行高变化大的内容优先分页或可见区渲染。
- [ ] 验证键盘导航。
- [ ] 验证滚动恢复。
- [ ] 验证选中态。
- [ ] 至少两个页面有同类问题时才抽公共实现。

做什么：

1. 用采集结果确认 logs、chat history、workspace tree、TODO tasks 的真实 DOM 行数和滚动耗时。
2. 只有超过 500 行且 CSS 隔离不够时，才做虚拟列表或分页。
3. 至少两个页面有真实需求时，才抽公共实现。

怎么做：

1. 先在单页面内实现最小方案。
2. 行高稳定的列表优先固定高度虚拟列表。
3. 行高变化大的内容优先分页或可见区渲染。

采集脚本：

```js
copy(JSON.stringify({
  chat_history_items: document.querySelectorAll(".chat-history-item").length,
  todo_tasks: document.querySelectorAll(".todo-index-item").length,
  logs_rows: document.querySelectorAll(".logs-line-row").length,
  workspace_rows: document.querySelectorAll(".chat-workspace-tree-row").length,
  audit_groups: document.querySelectorAll(".audit-group").length
}, null, 2))
```

判断规则：

1. 小于 500 行：不做虚拟列表。
2. 大于 500 行但滚动和输入没有超过阈值：不做虚拟列表。
3. 大于 500 行且 CSS 隔离收益不足：只在出现问题的单页面实现最小方案。
4. 两个以上页面出现同类问题，才评估公共实现。

验收标准：

1. Logs / Large 的 DOM row count 不超过可见行 + buffer。
2. Workspace tree / Large 的 expand/collapse p95 小于 50ms。
3. 虚拟列表实现不破坏键盘导航和滚动定位。
4. 没有两个页面复用需求时，不新增通用 virtual list 抽象。

失败标准：

1. 小列表也进入虚拟列表复杂路径。
2. 键盘定位、滚动恢复或选中态损坏。
3. 为一个页面写通用框架。

### Task 9: Bundle 和功能级懒加载

当前状态：已完成当前打样。代码侧已经把 Chat route 中的 artifact preview、raw dialog、workspace browser 拆成异步组件，并且关闭状态不挂载 dialog。Chat 页面只在 idle 后预加载很小的 dialog shell；route chunk 只在用户 hover/focus 侧栏入口后预加载。

Checklist：

- [x] 记录 `pnpm build` 当前 chunk baseline。
- [x] 记录 Chat route chunk baseline。
- [x] 识别 artifact preview 首屏是否必须加载。
- [x] 识别 raw dialog 首屏是否必须加载。
- [x] 识别 workspace browser 首屏是否必须加载。
- [x] 对低频重功能使用动态 import。
- [x] 用 idle 时间只预加载小 chunk。
- [x] 用户 hover/focus 侧栏入口时，用 idle 时间预加载目标 route chunk。
- [x] 验证预加载不产生超过 50ms long task。
- [x] 记录 before/after chunk 指标。
- [x] 记录 before/after route 指标。

做什么：

1. 记录主要 entry chunk 和 route chunk 体积。
2. 对 artifact preview、raw dialog、workspace browser 等低频功能使用动态 import。
3. 只在页面可操作后用 idle 时间预加载小 chunk。
4. Markdown renderer 先按 Task 4 做可见区挂载；只有构建或 profile 证明 parse/eval 仍重时，再考虑拆分。

怎么做：

1. 从 `pnpm build` 输出记录 chunk 体积。
2. route 组件保持懒加载。
3. 低频重组件只在用户打开相关功能时加载。
4. hover/focus sidebar entry 时，用 idle 时间预加载对应 route chunk。

验收标准：

1. 相关 chunk 增长超过 10% 时，PR 必须说明原因。
2. Chat cold enter / Medium 的 `route_interactive_ms` 比 baseline 降低 25% 以上，或小于 1500ms。
3. Chat 首屏不加载 artifact preview、raw dialog、workspace browser 的代码，除非首屏真实使用。
4. 预加载不产生超过 50ms 的 long task。

失败标准：

1. app 启动时预加载大功能。
2. 懒加载导致常用操作明显等待。
3. chunk 拆分只增加请求数量，没有改善 parse/eval。

构建记录：

1. 变更前 `ChatView` chunk：430.46 kB，gzip 82.80 kB。
2. 变更后 `ChatView` chunk：423.84 kB，gzip 81.10 kB。
3. 变化：minified 减少 6.62 kB，gzip 减少 1.70 kB。
4. 新增异步 chunk：`ArtifactPreviewCard`、`RawJsonDialog`、`AppDialogShell`。
5. 加入 idle preload 后 `ChatView` chunk：424.15 kB，gzip 81.21 kB。
6. `ChatView` 相比上一轮增加 0.31 kB，gzip 增加 0.11 kB。
7. `index` chunk 从 868.14 kB / gzip 324.73 kB 变为 870.01 kB / gzip 325.28 kB。
8. `index` 增加 1.87 kB，gzip 增加 0.55 kB，原因是 route preload map 和 nav preload 事件。
9. 新增异步 chunk 仍是 `ArtifactPreviewCard`、`RawJsonDialog`、`AppDialogShell`。

### Task 10: Overview 卡片资源审计

当前状态：已完成。静态审计、初次进入验证和多 endpoint 点击验证都已通过。当前代码里，Overview 卡片字段来自 `/endpoints` 返回的 endpoint item，不从 persona store 或全局 selection 临时状态读取头像。

Checklist：

- [x] 写清 Overview item 字段来源。
- [x] 写清卡片刷新条件。
- [x] 写清卡片点击目标。
- [x] 写清 endpoint 绑定方式。
- [x] 头像、名称、状态直接来自 overview item。
- [x] 点击一张卡片不应影响其他卡片展示。
- [x] 浏览器验证点击一张卡片不影响其他卡片展示。
- [x] 初次进入当前 route 的 `page` 请求为 0。
- [x] Overview 自身不触发 audit、tasks、state files 等无关请求。
- [x] 没有明确行动价值的卡片移除或改为只读说明。
- [x] 记录 before/after 指标。

做什么：

1. 为每张 Overview 卡片写清数据源、刷新条件、点击目标和 endpoint 绑定方式。
2. 移除没有明确行动价值的卡片。
3. 卡片头像、名称、状态等展示数据直接来自 overview item，不从全局临时状态推导。

怎么做：

1. 在 Overview item 结构里返回卡片需要的展示字段。
2. 卡片点击只影响被点击卡片或路由跳转。
3. Overview 页面声明自己的资源，不继承其他页面资源。

当前数据契约：

1. 数据源：`/endpoints`。
2. 展示字段：`endpoint_ref`、`name`、`url`、`connected`、`agent_name`、`mode`、`can_submit`、`avatar_url`。
3. 派生字段：`title`、`location`、`channelBadges` 只由当前 endpoint item 计算。
4. 刷新条件：页面进入时刷新一次；页面停留期间每 60 秒刷新一次。
5. 点击目标：connected endpoint 点击后写入 `selectedRef`，再跳到 `/chat`。
6. endpoint 绑定：卡片用自己的 `endpoint_ref` 作为 key 和点击参数。
7. 只读卡片：disconnected endpoint 不能点击，只显示状态。
8. 初次进入复用路由守卫已经加载的 endpoints；60 秒定时刷新才强制请求 `/endpoints`。

验收标准：

1. Overview 初次进入时只请求该页面声明需要的资源。
2. 点击某张卡片时，其他卡片头像、名称、状态不发生瞬时变化。
3. 切换 endpoint 后，Overview 不显示旧 endpoint 的卡片数据。
4. Overview 的无关 API 请求数为 0。
5. 每张卡片都有明确点击目标；没有目标的卡片必须说明为什么只读。

失败标准：

1. 头像或名称来自共享临时 selection 状态。
2. 点击一张卡片影响其他卡片显示。
3. Overview 触发 audit、tasks、state files 等无关请求。

### Task 11: 小质量问题固定处理

当前状态：文档规则已写清，执行入口待定。先用现有 issue 或 PR 描述承载，不新增新的任务系统。

Checklist：

- [ ] 建立 `quality` 分类。
- [ ] 每个 quality 任务有复现步骤。
- [ ] 每个 quality 任务有完成证据。
- [ ] UI 类任务有 before/after 截图或录屏。
- [ ] 性能类任务有 before/after 指标。
- [ ] 键盘任务有明确操作路径。
- [ ] 可访问性任务有明确操作路径。
- [ ] 单个 quality 任务不做跨页面大改。

做什么：

1. 建立轻量 `quality` 分类，用于 UI、键盘、可访问性、小性能回退。
2. 每周选择 1-2 个小问题处理。
3. 每个问题必须小到能独立 review。

怎么做：

1. 记录复现步骤。
2. 记录完成证据。
3. UI 类任务给 before/after 截图或录屏。
4. 性能类任务给 before/after 指标。

记录格式：

```text
Type: quality
Area:
Problem:
Steps:
Expected:
Actual:
Evidence:
Risk:
```

边界：

1. 单个 quality 任务只处理一个可复现问题。
2. 不能借 quality 任务改无关页面。
3. 性能类问题没有数字时不能标为完成。

验收标准：

1. 每个 quality 任务有复现步骤和完成证据。
2. UI 类任务有 before/after 截图或录屏。
3. 性能类任务有 before/after 指标。
4. 键盘或可访问性任务有明确操作路径。
5. 单个 quality 任务不引入跨页面大重构。

失败标准：

1. 质量任务变成大范围重构。
2. 没有复现步骤。
3. 没有完成证据。

### Task 12: 用户可读 Changelog

当前状态：文档规则已写清，仓库当前没有用户向 Changelog 文件。发版或 PR 描述里先按下面格式写；如果后续新增 Changelog 文件，再沿用同一格式。

Checklist：

- [ ] 每个性能 PR 写用户能感知的一句话。
- [ ] 每个性能项附至少一个 before/after 数字。
- [ ] 内部结构变化放开发说明。
- [ ] 用户无感改动不单独写进用户向 Changelog。
- [ ] Changelog 不只写内部模块名。

做什么：

1. 对性能和交互修复写用户能理解的变更说明。
2. 不把内部重构当成主要描述，除非它直接改善用户体验。
3. 性能项附带关键数字。

怎么做：

1. 每个性能 PR 在描述里写用户可感知变化。
2. Changelog 只写用户能感知的内容。
3. 内部重构放到开发说明，不放到用户向说明里。

性能项格式：

```text
- Chat topic 打开更快：warm enter 从 171ms 降到 57ms；历史列表从 11 次 update 降到 1 次。
```

开发说明格式：

```text
Internal: Chat history render-ready state moved into each history item.
```

判断规则：

1. 用户能感知：写进用户向说明。
2. 只有内部结构变化：写进开发说明。
3. 没有 before/after 数字的性能项：不能写“更快”。

验收标准：

1. 每个性能修复都有用户可读的一句话说明。
2. Changelog 中的性能项至少包含一个 before/after 数字。
3. 只影响内部结构、用户无感的改动不单独占据用户向说明。

失败标准：

1. Changelog 只写内部模块名。
2. 性能声明没有数字。
3. 用户无感改动被包装成用户价值。

## 7) 建议执行顺序

1. Task 1：增加 dev-only 性能采集。
2. Task 2：页面资源按需请求。
3. Task 3：Chat 输入和历史渲染隔离。
4. Task 4：Markdown 可见区渲染。
5. Task 5：TODO 改成本地 draft 编辑。
6. Task 6：Settings 按 section 计算 dirty。
7. Task 7：长列表先验证 CSS 渲染隔离。
8. Task 9：Bundle 和功能级懒加载。
9. Task 10：Overview 卡片资源审计。
10. Task 8：必要时才做虚拟列表。
11. Task 11：小质量问题固定处理。
12. Task 12：用户可读 Changelog。

## 8) PR 记录格式

```text
Scenario:
Metric:
Before:
After:
Change:
Trace:
Risk:
Notes:
```

示例：

```text
Scenario: Chat warm enter / Medium
Metric: markdown_mounted_count
Before: 40
After: 8
Change: -80%
Trace: browser performance profile
Risk: delayed markdown mount may affect scroll height
Notes: only visible messages mount MarkdownContent
```

## 9) 2026-05-21 打样结果

### Chat 冷进入

场景：首次打开 `/chat?perf=1`。

结果：

1. `route_interactive_ms`: 691ms。
2. `api_request_count`: 8。
3. 请求来源：`bootstrap` 3、`setup-readiness` 1、`page` 1、`shared-preload` 3。
4. `long_task_count`: 0。

判断：

1. setup readiness 冷进入只打 1 次 `/state/files`，符合目标。
2. persona 和 contacts 已标记为 `shared-preload`，不计入 Chat 页面自身资源。
3. Chat 页面自身冷进入核心请求是 `/topics`。

### Chat warm topic enter

场景：从 Chat 进入某个 topic。

第一次打样：

1. `route_interactive_ms`: 171ms。
2. `component_update_count`: 21。
3. `chat.history_list.update_count`: 11。
4. `chat.history_list.max_update_ms`: 38ms。
5. `markdown.mounts`: 10。

原因：

1. Markdown render-ready 状态在 `ChatView` 父级对象中。
2. 每个 Markdown 渲染完成都会改父级对象。
3. 父级对象变化导致整个 history list 重跑。

修正：

1. render-ready 状态下沉到 `ChatHistoryItem`。
2. `ChatHistoryList` 不再接收 `renderedItems`。
3. 单个 Markdown 渲染完成只更新自己的 item。

修正后：

1. `route_interactive_ms`: 57ms。
2. `component_update_count`: 5。
3. `chat.history_list.update_count`: 1。
4. `chat.history_list.max_update_ms`: 21ms。
5. `chat.history_item.update_count`: 4。
6. `markdown.mounts`: 4。
7. 请求来源：`page` 2、`bootstrap` 1。

判断：

1. warm topic enter 已没有 setup readiness 请求。
2. warm topic enter 已没有 shared preload 请求。
3. list 只在 history items 整体替换时 update 1 次，符合预期。
4. item update 与实际 Markdown item 数一致，符合预期。

### Chat composer typing

场景：Chat composer 连续输入 6 次。

结果：

1. `input_to_paint_ms`: 0ms、1ms、1ms、1ms、4ms、12ms。
2. `component_update_count`: 0。
3. `componentUpdates`: 空。

判断：

1. composer 输入没有触发 `chat.history_list` update。
2. composer 输入没有触发 `chat.history_item` update。
3. 当前 Small/Medium 数据下输入响应在一帧内完成。

### TODO enter

场景：从侧栏进入 `/todo`。

结果：

1. `route_interactive_ms`: 65ms。
2. `api_request_count`: 2。
3. 请求来源：`bootstrap` 1、`page` 1。
4. page 请求只有 `/todo/tasks`。

判断：

1. TODO 进入没有请求 `/state/files`。
2. TODO 进入没有请求 `/audit/files`。
3. TODO 进入没有请求 `/tasks?limit=20`。
4. 页面资源按需请求方向成立。

### TODO typing

场景：TODO 编辑器输入 9 次。

结果：

1. `input_to_paint_ms`: 0ms 到 5ms。
2. `snapshots.todo.tasks`: 14。
3. `snapshot_build_count`: 14。

判断：

1. 当前数据量下输入响应仍快。
2. 结构上仍有问题：TODO 输入路径会反复构建整表 snapshot。
3. Task 5 应优先把编辑状态改成本地 draft，让输入时不扫描整张 task 表。

### Overview enter

场景：Google Chrome headless 打开 `/overview?perf=1`。

第一次验证：

1. `route_interactive_ms`: 54.8ms。
2. `api_request_count`: 10。
3. 请求来源：`bootstrap` 6、`setup-readiness` 1、`shared-preload` 3。
4. `/endpoints` 请求 2 次。

原因：

1. 路由守卫已经为了 setup 判断加载过 endpoints。
2. Overview 进入后又强制调用 `loadEndpoints()`。

修正后：

1. `route_interactive_ms`: 53ms。
2. `api_request_count`: 9。
3. 请求来源：`bootstrap` 5、`setup-readiness` 1、`shared-preload` 3。
4. `/endpoints` 请求 1 次。
5. 当前 route 的 `page` 请求为 0。
6. `long_task_count`: 0。

判断：

1. Overview 自身没有触发 audit、tasks、state files 等 page 请求。
2. `setup-readiness` 的 `/state/files` 属于 setup 判断，不计入 Overview 自身资源。
3. persona 和 contacts 属于 shared preload，不计入 Overview 自身资源。

### 用户浏览器复测

Overview：

1. `route_interactive_ms`: 107ms。
2. `api_request_count`: 7。
3. 请求来源：`bootstrap` 3、`setup-readiness` 1、`shared-preload` 3。
4. 当前 route 的 `page` 请求为 0。
5. `long_task_count`: 0。

TODO typing：

1. 输入 3 次，`input_to_paint_ms`: 4ms、1ms、1ms。
2. `snapshots`: 空对象。
3. `snapshot_build_count`: 0。
4. `long_task_count`: 0。

Settings agent typing 修正前：

1. 输入 5 次，`input_to_paint_ms`: 0ms 到 3ms。
2. `snapshots.settings.llm`: 7。
3. `snapshots.settings.multimodal`: 5。
4. `snapshots.settings.skills`: 1。
5. `snapshots.settings.tools`: 1。

原因：

1. Settings agent 页面同时展示 LLM 和 multimodal 两张卡片。
2. Save button 的 disabled 状态读取多个 computed dirty。
3. 这些 computed dirty 会调用各自 snapshot builder。

修正后：

1. 连续修改 LLM API Base 5 次。
2. `input_to_paint_ms`: 2.9ms、9.8ms、16.9ms、5.7ms、11.6ms。
3. `snapshots.settings.llm`: 5。
4. `settings.multimodal`、`settings.skills`、`settings.tools` 没有增长。
5. `snapshot_build_count`: 5。

按 section 加载修正后：

1. 打开 `/settings/agent?perf=1`。
2. 当前 route 的 `page` 请求从 9 降到 2。
3. page 请求只剩 `/settings/agent` 和一次 `/auth/codex/status`。
4. `snapshots`: `settings.llm` 1 次，`settings.multimodal` 1 次。
5. hidden section 的 `settings.skills`、`settings.tools`、`settings.console.*` 没有出现。
6. synthetic input 的 `input_to_paint_ms`: 2.2ms、2.2ms。

用户复测 backup profile model name 输入并保存：

1. 输入 2 次，`input_to_paint_ms`: 3ms、1ms。
2. `snapshots`: `settings.llm` 3 次。
3. `settings.multimodal`、`settings.skills`、`settings.tools`、`settings.console.*` 没有出现。
4. `snapshot_build_count`: 3。
5. `long_task_count`: 0。

判断：

1. LLM 输入路径只计算 LLM dirty，符合目标。
2. TODO 输入路径已经不构建整表 snapshot，符合目标。
3. Overview 资源请求符合目标。
4. Settings agent 页面不再预加载 persona、console 和 desktop settings。
5. 保存 LLM 后只刷新 LLM dirty 基线，不再刷新 hidden section。

### 浏览器自动复测

Chat detail cold enter：

1. 场景：直接打开一个现有 topic 的 `/chat/:topicId?perf=1`。
2. `route_interactive_ms`: 625.9ms。
3. `api_request_count`: 12。
4. 请求来源：`bootstrap` 5、`setup-readiness` 1、`shared-preload` 3、`page` 3。
5. page 请求：`/topics`、`/workspace?topic_id=...`、`/tasks?limit=100&topic_id=...`。
6. `long_task_count`: 1，耗时 93ms。
7. `long_animation_frame_count`: 2，总耗时 223.9ms。
8. 初始 `markdown.mounts`: 2，`markdown.updates`: 2。
9. 滚动到旧消息后 `markdown.mounts`: 3，`markdown.updates`: 3。
10. `chat_history_items`: 8。
11. 初始 `chat_markdown_nodes`: 2；滚动到旧消息后为 3。
12. `chat.history_list.update_count`: 2，`max_update_ms`: 11.9ms。
13. `chat.history_item.update_count`: 4，滚动到旧消息后为 6。

判断：

1. Markdown 可见区渲染生效：首屏没有把全部旧 Markdown 都挂载。
2. 滚动到旧消息后内容能补渲染。
3. 当前样本只有 8 条 history，不需要虚拟列表。
4. 93ms long task 需要在 Medium/Large 样本继续观察；当前 Markdown 和组件 update 总耗时都不高，暂时不为这个点加新结构。

TODO enter：

1. 场景：直接打开 `/todo?perf=1`。
2. `route_interactive_ms`: 60.8ms。
3. `api_request_count`: 8。
4. 请求来源：`bootstrap` 3、`setup-readiness` 1、`shared-preload` 3、`page` 1。
5. page 请求只有 `/todo/tasks`。
6. `todo_tasks`: 2。
7. `snapshot_build_count`: 0。
8. `long_task_count`: 0。

判断：

1. TODO 页面资源按需请求符合目标。
2. 当前真实 task 数很小，不需要虚拟列表。

Logs enter and scroll：

1. 场景：直接打开 `/logs?perf=1` 并滚动。
2. `route_interactive_ms`: 42.1ms。
3. page 请求只有 `/logs/latest?limit=300`。
4. `logs_rows`: 242。
5. `long_task_count`: 1，耗时 50ms。
6. `long_animation_frame_count`: 1，耗时 66.4ms。
7. 滚动后没有新增请求。

判断：

1. 当前 Logs 样本低于 500 行，不做虚拟列表。
2. 50ms long task 刚好到阈值，先记录为观察项。
3. 如果真实 logs 超过 500 行，或滚动时连续出现超过 50ms 的 long task，再做单页面最小虚拟列表或分页。

Audit enter and scroll：

1. 场景：直接打开 `/audit?perf=1` 并滚动。
2. `route_interactive_ms`: 54.3ms。
3. page 请求：`/audit/files` 和 `/audit/logs?file=guard_audit.jsonl&limit=50&cursor=0`。
4. 当前样本 `audit_groups`: 0。
5. `long_task_count`: 0。
6. 滚动后没有新增请求。

判断：

1. 当前样本没有发现 Audit 渲染压力。
2. 还需要有真实 Audit 分组的大样本来判断 CSS 隔离收益。

Overview enter：

1. 场景：直接打开 `/overview?perf=1`。
2. 当前样本 endpoint card 数量：1。
3. `route_interactive_ms`: 66.6ms。
4. 请求来源：`bootstrap` 5、`setup-readiness` 1、`shared-preload` 3。
5. 当前 route 的 `page` 请求为 0。

判断：

1. Overview 自身没有 page 请求，符合资源按需目标。
2. 该 headless 样本只有 1 张 card；多 endpoint 点击验证已由用户浏览器确认通过。
3. 静态代码侧已确认头像来自当前 endpoint item 的 `avatar_url`，不是 persona store 或 selected endpoint 临时状态。

Task 9 route and chunk validation：

1. 场景：桌面宽度打开 `/chat?perf=1`，等待 idle preload，然后 hover Contacts 侧栏入口。
2. Chat cold enter `route_interactive_ms`: 571.9ms。
3. Chat cold enter 请求来源：`bootstrap` 5、`setup-readiness` 1、`shared-preload` 3、`page` 1。
4. Chat cold enter 的 page 请求只有 `/topics`。
5. 初始 long task：1 个，100ms；该 long task 发生在 route startup 阶段。
6. idle 后加载了 `AppDialogShell`。
7. idle 后没有加载 `RawJsonDialog`。
8. idle 后没有加载 `ArtifactPreviewCard`。
9. hover Contacts 后加载了 `ContactsView` 和对应 CSS。
10. hover route preload 后 long task 数没有增加。

Chat warm topic enter：

1. 场景：在 Chat 页面点击一个 topic。
2. `route_interactive_ms`: 47.9ms。
3. `api_request_count`: 3。
4. 请求来源：`page` 2、`bootstrap` 1。
5. page 请求：`/workspace?topic_id=...` 和 `/tasks?limit=100&topic_id=...`。
6. `long_task_count`: 0。
7. `long_animation_frame_count`: 0。
8. `chat.history_list.update_count`: 1。

判断：

1. `AppDialogShell` 是小 shell，idle 后加载，不在 Chat route critical path 上。
2. raw JSON dialog 和 artifact preview 没有首屏加载，符合低频功能懒加载目标。
3. 侧栏 hover/focus 只预加载用户指向的 route chunk，没有触发目标页面 API。
4. route preload 没有引入新的 long task。
5. `ChatView` 和 `index` chunk 增长都低于 10%，不需要拆新抽象。

虚拟列表判断：

1. 当前真实样本：Chat history 8、TODO tasks 2、Logs rows 242、Workspace rows 0、Audit groups 0。
2. 这些数据都没有超过 500 行。
3. 现在不做虚拟列表，也不抽公共列表实现。

## 10) 后续调优判断规则

1. 先看 `input_to_paint_ms`。超过 32ms，再看 component update 和 snapshot 次数。
2. 页面切换先看 `api_request_count_by_source`。只把 `page` 算作页面自身成本。
3. `setup-readiness` 同一 tab 内页面切换应为 0。
4. `shared-preload` 不应在每次侧栏切换时重复出现。
5. Chat composer 输入时，`chat.history_list` 和 `chat.history_item` update 应为 0。
6. Chat topic 切换时，`chat.history_list` update 应接近 1。
7. TODO 输入时，`todo.tasks` snapshot 应降到 0。
8. Settings 输入时，只有当前 section 的 snapshot 可以变化。
9. 如果 `long_task_count` 大于 0，先看对应时段的 Markdown、component update、snapshot，再决定是否拆渲染或延迟加载。
10. 不为一次局部优化写通用框架；只有两个以上页面有同一类可测问题时，才抽公共实现。

## 11) 调优命令和步骤

### 本地命令

安装依赖：

```sh
cd web/console
pnpm install
```

启动开发服务：

```sh
cd web/console
pnpm dev
```

构建验证：

```sh
cd web/console
pnpm build
```

### 通用调优流程

1. 启动开发服务。
2. 打开目标页面并加 `?perf=1`。
3. 执行一次 baseline 操作。
4. 复制 `window.__MISTERMORPH_PERF__` 中的相关数据。
5. 修改代码。
6. 重新执行同一组操作。
7. 只比较同一场景、同一数据量、同一浏览器 tab 条件下的数据。
8. 记录 before/after 到 PR 记录格式。
9. 跑 `pnpm build`。

### Chat 输入路径

打开：

```text
/chat?perf=1
```

清计数：

```js
const p = window.__MISTERMORPH_PERF__;
p.inputs.length = 0;
p.componentUpdates.length = 0;
p.components = {};
p.currentRoute.component_update_count = 0;
p.currentRoute.component_update_total_ms = 0;
p.currentRoute.component_update_max_ms = 0;
```

在 composer 连续输入后复制：

```js
copy(JSON.stringify({
  inputs: window.__MISTERMORPH_PERF__.inputs,
  components: window.__MISTERMORPH_PERF__.components,
  componentUpdates: window.__MISTERMORPH_PERF__.componentUpdates,
  route: window.__MISTERMORPH_PERF__.currentRoute
}, null, 2))
```

通过标准：

1. `input_to_paint_ms` p95 小于 32ms。
2. `chat.history_list` update 为 0。
3. `chat.history_item` update 为 0。

### Chat topic 切换

打开：

```text
/chat?perf=1
```

点击一个 topic 后复制：

```js
copy(JSON.stringify({
  route: window.__MISTERMORPH_PERF__.currentRoute,
  requests: window.__MISTERMORPH_PERF__.requests.slice(-20),
  markdown: window.__MISTERMORPH_PERF__.markdown,
  components: window.__MISTERMORPH_PERF__.components
}, null, 2))
```

通过标准：

1. `setup-readiness` 请求为 0。
2. `shared-preload` 请求为 0。
3. `chat.history_list.update_count` 接近 1。
4. 无关 `chat.history_item` update 不增长。

### TODO 输入路径

打开：

```text
/todo?perf=1
```

清计数：

```js
const p = window.__MISTERMORPH_PERF__;
p.inputs.length = 0;
p.snapshots = {};
p.currentRoute.snapshot_build_count = 0;
```

编辑 task 内容后复制：

```js
copy(JSON.stringify({
  inputs: window.__MISTERMORPH_PERF__.inputs,
  snapshots: window.__MISTERMORPH_PERF__.snapshots,
  route: window.__MISTERMORPH_PERF__.currentRoute
}, null, 2))
```

通过标准：

1. `input_to_paint_ms` p95 小于 32ms。
2. 普通输入时 `snapshots.todo.tasks` 为 0。
3. 保存前仍能阻止无效 task。

### Settings 输入路径

打开：

```text
/settings?perf=1
```

清计数：

```js
const p = window.__MISTERMORPH_PERF__;
p.inputs.length = 0;
p.snapshots = {};
p.currentRoute.snapshot_build_count = 0;
```

编辑某个 section 的字段后复制：

```js
copy(JSON.stringify({
  inputs: window.__MISTERMORPH_PERF__.inputs,
  snapshots: window.__MISTERMORPH_PERF__.snapshots,
  route: window.__MISTERMORPH_PERF__.currentRoute
}, null, 2))
```

通过标准：

1. `input_to_paint_ms` p95 小于 32ms。
2. 只有当前 section 的 snapshot 计数增长。
3. 无关 section snapshot 计数为 0。

### Bundle 和首屏代码

构建：

```sh
cd web/console
pnpm build
```

记录：

1. 保存 `dist/assets` 中相关 route chunk 的 minified 和 gzip 体积。
2. 对比 `ChatView`、`TodoView`、`SettingsView`、`OverviewView` 的 chunk 变化。
3. 如果某个 route chunk 增长超过 10%，记录原因。
4. 如果拆出新的异步 chunk，确认它不在首屏直接请求，除非首屏真实使用。

浏览器验证：

```js
copy(JSON.stringify({
  route: window.__MISTERMORPH_PERF__.currentRoute,
  requests: window.__MISTERMORPH_PERF__.requests.filter((item) => item.routeID === window.__MISTERMORPH_PERF__.currentRoute.id),
  longTasks: window.__MISTERMORPH_PERF__.longTasks,
  longAnimationFrames: window.__MISTERMORPH_PERF__.longAnimationFrames
}, null, 2))
```

通过标准：

1. 首屏不加载当前没有打开的 dialog、preview、browser 类功能代码。
2. 新增异步 chunk 不产生超过 50ms 的 long task。
3. 常用点击打开低频功能时没有明显空白等待。

### 页面资源请求

打开任意页面后复制：

```js
copy(JSON.stringify({
  route: window.__MISTERMORPH_PERF__.currentRoute,
  requests: window.__MISTERMORPH_PERF__.requests.filter((item) => item.routeID === window.__MISTERMORPH_PERF__.currentRoute.id)
}, null, 2))
```

通过标准：

1. 当前页面的 `page` 请求只包含该页面真实需要的资源。
2. 同一 tab 内切换页面时 `setup-readiness` 为 0。
3. `shared-preload` 不在每次侧栏切换时重复出现。
