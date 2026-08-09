---
date: 2026-08-09
title: API 请求路径性能审查与最小修复方案
status: draft
---

# API 请求路径性能审查与最小修复方案

## 1. 审查范围

本次检查覆盖：

- Console 注册的全部 HTTP API，包括鉴权、endpoint、settings、proxy、artifact 和 WebSocket 入口。
- daemon runtime 注册的全部 HTTP API，包括 task、topic、state、todo、contacts、persona、memory、audit、logs、stats 和 workspace。
- Console 前端对这些 API 的首次加载、轮询、预加载、缓存和重复请求行为。
- API 后面的文件读取、外部 HTTP、全量排序、projection 和 journal 路径。

检查依据是当前代码路径和已有生产日志。除 chat profile 外，本文没有生产环境的 before/after 耗时，因此会区分“已经观测到的慢请求”和“代码上确定存在、但实际影响仍取决于数据量的增长问题”。

## 2. 结论

当前有四类应直接修复的问题：

| ID | 优先级 | API | 问题 |
| --- | --- | --- | --- |
| P-01 | High | `GET /contacts/chat-profile` | GET 内串行调用平台 API；每个 Markdown 编辑器挂载时又会立即请求 |
| P-02 | High | `GET /logs/latest`、`GET /audit/logs`、`GET /observations` | 为返回尾部少量记录，每次从文件头扫描到文件尾 |
| P-03 | High | `GET /stats/llm/usage` | 每次请求刷新 projection，并从活动 journal segment 开头重复扫描 |
| P-04 | High | task/topic API | 每次 task mutation 在锁内重写全部历史 projection；列表请求复制并排序全部任务 |

另有五类增长风险。它们的代码复杂度上界有问题，但没有生产耗时证据，不应立即为它们增加复杂缓存或索引：

| ID | 优先级 | API | 风险 |
| --- | --- | --- | --- |
| P-05 | Medium | `GET /contacts/list`、`/contacts/item` | 分页发生在全量读取和排序之后；前端默认预加载全部联系人 |
| P-06 | Medium | `GET /memory/files` | 枚举并 `stat` 全部历史 memory 文件，且历史目录没有清理 |
| P-07 | Medium | `GET /settings/agent` | 每次递归扫描全部 skill root，并读取每个 `SKILL.md` frontmatter |
| P-08 | Medium | `GET /api/auth/pro/status` | token 临近过期时，状态查询会串行执行两个外部请求 |
| P-09 | Low | `GET /workspace/tree`、`GET /workspace/browse` | 为计算 `has_children`，对每个子目录再做一次目录读取 |

`GET /api/endpoints` 和 `GET /todo/tasks` 已不再包含同步 health、avatar 或 chat profile 刷新，不属于当前慢路径。`/api/endpoints` 仍可能因为内联 base64 头像而产生较大响应，但在没有 `response_bytes` 证据前只监控，不拆新 API。

## 3. 判断标准

从请求的必要结果反推工作量：

1. 读取缓存的 GET 不应顺便刷新外部数据或写 projection。
2. `limit` 和 cursor 不只限制响应大小，也应限制后端读取和排序的工作量。
3. 被轮询的 API，单次成本不能随当天日志或全部历史持续增长。
4. 显式网络操作可以等待网络，例如登录、模型测试和更新检查；状态查询、页面预加载不应隐藏同类等待。
5. projection 是可重建视图，不应让每次状态变化都同步重写全部历史。

修复目标不是把所有 API 都放进通用缓存。优先删除请求中不必要的工作，再为确实需要的顺序读取保留最小 cursor。

## 4. 详细发现

### P-01：chat profile 的 GET 执行平台刷新

现状：

- `handleContactsChatProfile` 先调用 `refreshChatProfileCandidates`，完成后才读取本地 `chat_profile.json` 并返回。
- candidate 串行处理。Telegram、Slack 和 LINE 每个过期 candidate 最多产生一个平台请求；Lark 最多产生 token 和 chat detail 两个请求。
- 请求失败没有走 `RefreshExpired` 已有的 6 小时失败重试时间。下一次 GET 会再次尝试。
- 每次 handler 都创建新的 store，多个并发 GET 之间没有共享的 in-flight 去重。
- `AppMarkdownEditor` 对 endpoint 使用 `immediate` watcher。Todo、Memory、Settings 或 Setup 只要挂载编辑器，就会请求 chat profile，即使用户没有打开会话选择器。

已有生产日志中，两次失败的 Telegram profile 请求分别耗时 493ms 和 751ms，整个候选刷新耗时 1247ms，请求总耗时 1248ms。这里已经有直接证据。

最小改法：

1. `GET /contacts/chat-profile` 只读本地 store，不访问平台 API。
2. 把过期刷新交给一个 runtime 所有的异步入口，复用现有 TTL 和失败重试规则；不要在 HTTP handler 中再实现一套刷新状态。
3. Markdown 编辑器挂载时不加载 chat profile。用户打开会话选择器时才加载，并按 endpoint 复用现有 resource cache 和 in-flight 去重。

验收条件：

- GET 的执行过程中没有 Telegram、Slack、LINE 或 Lark HTTP 请求。
- 同一 endpoint 的并发读取不会触发重复刷新。
- 只挂载 Markdown 编辑器时，`/contacts/chat-profile` 请求数为 0。
- 平台刷新失败后，在失败重试时间到达前不再请求同一 profile。

### P-02：日志分页每次完整扫描文件

现状：

- `readLogFilePage` 使用 `bufio.Scanner` 从当天日志开头扫描到 EOF，只为保留最后 `before + limit` 行。
- Logs 页面在接近底部时每 4 秒调用一次 `GET /logs/latest?limit=300`。请求未完成时没有 in-flight guard，超过 4 秒便可能出现重叠扫描。
- `readAuditLogChunk` 使用相同的全文件扫描方式。guard audit 单个文件默认可到 100 MiB，每翻一页都会重新扫描。
- `/observations` 虽然用 domain journal index 找事件，随后仍通过 `/logs/latest` 的实现完整扫描当天日志，再从最后 1000 行中匹配 trace id。
- audit 轮转文件没有 retention；`GET /audit/files` 的目录枚举也会随历史文件数量增长。

返回 50、300 或 1000 行时，工作量仍是 `O(目标文件字节数)`。Logs 页的轮询会把这个成本持续放大。

最小改法：

1. 增加一个实际负责反向分块读取的 tail reader，从文件末尾向前读取，收集到足够行后停止。
2. cursor 保存文件名和 byte offset。加载更早内容时从上一次起点继续向前，不再使用“从文件头数到第几行”的 cursor。
3. 不为保留页码新增 line index。Logs 和 Audit 都使用 `has_older` 与 cursor；没有索引时不返回精确总行数和总页数。
4. Logs 前端在已有请求未完成时跳过下一次轮询。
5. `/observations` 复用同一 tail reader，最多反向读取需要检查的尾部窗口。

验收条件：

- 返回固定数量的尾部记录时，读取字节数不再随文件总大小线性增长。
- 1 MiB、50 MiB 和 100 MiB 文件的尾页耗时保持在同一数量级。
- Logs 页面任一时刻最多有一个 latest 请求。
- 加载旧页不会重新扫描已越过的文件区间。

### P-03：LLM usage GET 重放活动 segment

现状：

- `/stats/llm/usage` 每次创建 `ProjectionStore` 并同步调用 `Refresh()`。
- 为验证 line-based offset，`offsetValidForSegments` 先完整统计目标 segment 行数。
- `scanJournalFrom` 随后又从 segment 开头读取，逐行跳过已经投影的记录。
- LLM usage segment 默认最大 64 MiB。活动 segment 接近上限时，一次没有新记录的 warm GET 仍可能顺序读取该 segment 两次。
- 即使没有新记录，handler 仍重写 projection 文件。

最小改法：

1. projection offset 增加 byte offset。
2. 用 segment 文件大小验证 offset，并直接 `Seek` 到 byte offset，只解析新增内容。
3. 没有新增记录且 pricing digest 未变化时，不重写 projection。
4. pricing 或 schema 变化仍允许完整 rebuild；这是低频显式例外，不需要引入常驻聚合服务。

验收条件：

- warm GET 且 journal 未变化时，不扫描活动 segment，也不写 projection。
- 增加一条 usage record 后，只读取 offset 后面的新增字节。
- pricing digest 变化时仍能得到完整、正确的重建结果。

### P-04：task projection 的写放大和全量排序

现状：

- 默认配置启用 console task persistence。
- `ConsoleFileStore` 和 `FileTaskStore` 每次 upsert 或 update 都先复制全部 task，排序，再把完整 `projection.json` 原子重写。
- 复制、排序、JSON 编码和文件写入都发生在 store 写锁内。此时 task detail、list、approval list 等读取会等待。
- progress、pending、terminal 等状态变化会重复经过这条路径；历史 task 越多，每次状态变化越慢。
- `GET /tasks?limit=N` 虽然限制响应数量，store 仍复制并排序全部匹配 task 后才截断。
- `GET /topics` 没有 limit，返回并排序全部 topic。
- 非持久化 `MemoryStore` 有 `maxItems`；持久化 store 没有同样的视图上限，传入 `NewTaskViewForTarget` 的 `maxItems` 在持久化分支中没有使用。

最小改法：

1. journal append 保持事实提交点。projection snapshot 改为单一 worker 合并写入，不再让每个 mutation 同步重写完整文件。
2. 不把 transient progress 的每次变化都变成完整 snapshot；snapshot 落后时，启动可从 journal cursor 继续 replay。
3. store 在加载时建立按 `CreatedAt` 排列的 task id 列表。列表查询从 cursor 向后遍历，得到 `limit + 1` 条后停止，不再每次复制并排序整个 map。
4. 为 topics 增加与 tasks 一致的 limit/cursor。第一版不增加数据库或通用 query engine。

验收条件：

- task 状态变化的同步路径只承担必要的 journal append 和内存更新，不写完整 projection。
- 多次连续状态变化可以合并成一次 snapshot write。
- 无 filter 的第一页查询在得到 `limit + 1` 条后停止。
- task detail 和 list 不因后台 snapshot 文件写入而长时间等待同一把锁。

### P-05：contacts 的分页没有限制读取工作量

现状：

- `/contacts/list` 先完整读取并解析 `ACTIVE.md` 和 `INACTIVE.md`，合并、排序后才应用 offset 和 limit。
- 不传 limit 时默认返回全部联系人。当前 contacts store 和 shared preload 都不传 limit。
- active contacts 最多 150 个，但溢出的记录会移入 inactive；inactive 没有数量上限。
- App shell 对每个已选择 endpoint 预加载全部联系人。每个 Markdown 编辑器也会请求 store，虽然 resource cache 通常能去重。
- `/contacts/item` 为返回一个联系人，除了查找 YAML block，还会再次列出并排序全部联系人。

最小方向：

- mention picker 只需要 active contacts，应按需请求 active 集合，不加载 inactive。
- Contacts 页面再单独加载 inactive，并使用 cursor；不要为了精确 total 先解析全部记录。
- 在有生产耗时或明显文件增长前，不为 Markdown 文件增加通用索引。

### P-06：memory 列表扫描全部历史

现状：

- `/memory/files` 读取 memory root 下的全部日期目录，再读取每个目录下的全部 session 文件，并对每个文件执行 `stat`。
- `memory.short_term_days` 只限制 prompt injection 读取最近几天，不删除旧目录。
- Memory 页面首次进入就请求完整列表，API 没有 limit 或 cursor。

最小方向：

- 默认只列出 long-term index 和最近一页 short-term 文件。
- 以日期和文件名作为 cursor，用户请求更早记录时再继续读取。
- 不因列表性能自动删除历史 memory；retention 是另一项数据策略。

### P-07：agent settings 每次递归发现 skills

现状：

- `GET /settings/agent` 在读取 LLM 和 tools 配置时，也调用 `BuildSkillStatus`。
- `skills.Discover` 对每个 root 执行 `WalkDir`，随后读取每个 `SKILL.md` 最多 64 KiB 的 frontmatter。
- skill root 较大、位于慢磁盘或包含很多非 skill 目录时，settings GET 会随整棵目录树增长。

最小方向：

- 先记录该 handler 的 `skills_ms` 和发现数量。
- 确认它占主要耗时后，再把 skill status 放进 runtime config generation 的已有快照；配置 reload 或用户显式刷新时重建。
- 不增加文件 watcher、通用 TTL cache 或新的 settings framework。

### P-08：Pro status 隐藏外部刷新

现状：

- `GET /api/auth/pro/status` 先调用 `ResolveSession`。
- access token 进入 5 分钟 refresh 窗口后，它会先 refresh OAuth token，再 rotate subscription API key。
- 两个请求串行执行，各自默认 15 秒 timeout；并发 status 请求还会等待进程级 `refreshMu`。
- 这只影响 Pro 设置或登录 UI，不是全局页面请求，但 GET status 的语义和成本不一致。

最小方向：

- status GET 只调用 `ReadStatus`。
- 真正使用 Pro session 的请求继续通过 `ResolveSession` 保证 token 可用。
- 不需要仅为 status 再增加后台刷新器；如果 UI 以后需要显式刷新，再提供明确的 POST。

### P-09：workspace tree 的 N+1 目录读取

现状：

- `listTreeEntries` 读取当前目录后，对每个子目录调用 `dirHasChildren`。
- 一个包含 N 个子目录的页面会产生一次父目录读取和 N 次子目录读取。网络挂载、机械盘或权限复杂的目录会放大延迟。
- 该 API 只在用户展开 workspace 或目录选择器时调用，频率低，因此优先级低。

最小方向：

- 目录节点直接视为可展开。用户真正展开时再读取并显示空目录状态。
- 不为文件树增加预扫描、递归缓存或文件系统 watcher。

## 5. 对 managed runtime 的影响

Console 的 `/api/proxy` 只是把请求转发给选中的 runtime。因此：

- P-01、P-02、P-03、P-05、P-06、P-07 和 P-09 来自共享 daemon routes，对 local 和 managed runtime 都有效。
- P-04 对默认持久化的 console runtime 一定有效；channel runtime 只有加入 `tasks.persistence_targets` 时才有完整 projection 重写问题。非持久化 store 仍有全量排序，但默认最多保留 1000 条。
- P-08 是 Console 自己的鉴权 API，与 managed runtime 无关。

修复应落在 daemon/store 的共同所有者中，不在 `/api/proxy` 为每个 endpoint 增加补丁。

## 6. 已检查但不需要改的路径

以下路径的工作量固定、已经异步，或者等待本来就是用户显式操作的一部分：

- `/api/endpoints`：handler 只读内存中的 health 和 avatar；health 后台更新，avatar 启动时缓存。
- `/todo/tasks`：只读 cron、settings、LLM profile 和本地 chat profile 文件，不再访问平台 API。
- `/health`、`/overview`、`/commands`、`/llm/profiles`：只读小型内存状态或固定配置。
- auth config、me、Codex/XAI status、console settings、auto-update settings、credits、setup integrity：只读固定数量的小文件或内存值。
- login/poll、模型列表、模型测试、update check、poke：显式操作本来就需要外部网络或任务执行。
- 单文件 detail、download、upload 和 preview：成本与用户明确请求的文件大小一致，且 download/preview 使用流式响应。
- stream 和 notification WebSocket：长连接是接口本身的目的，不按普通请求耗时判断。

`/api/proxy` 会把普通 JSON response 最多缓冲 16 MiB 后再返回，但当前慢请求的主要耗时都发生在 upstream handler 内。先修 upstream，不单独改 proxy。

## 7. 实现顺序

1. P-01：chat profile cache-only GET 和前端按需加载。已有生产证据，改动也最小。
2. P-02：反向 tail reader、byte cursor 和 Logs in-flight guard。它同时修复三个 API。
3. P-03：LLM usage byte offset 和 no-op refresh。
4. P-04：task snapshot 合并写入和有序 task id 列表。
5. 为 P-05 至 P-09 增加已有日志风格的分段耗时。只有生产数据确认后再实现对应最小方向。

不引入数据库、Redis、query framework、通用 cache service、文件 watcher或全局后台任务框架。

## 8. 验证方式

每项实现前先保存 baseline，至少覆盖：

- chat profile：缓存命中、两个过期 candidate、平台失败。
- logs/audit：1 MiB、50 MiB、100 MiB 文件的最新页和旧页。
- stats：空 journal、活动 segment 接近 64 MiB、只追加一条记录、pricing digest 变化。
- tasks：100、1000、10000 条历史下的 submit、status update、第一页 list 和 topic list。

记录 daemon handler duration、Console proxy upstream duration、response bytes 和实际读取字节数。验收以工作量是否受返回数量约束为主，不先设没有 baseline 支持的统一毫秒阈值。
