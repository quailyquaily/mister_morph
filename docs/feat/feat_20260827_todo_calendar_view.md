---
date: 2026-08-27
title: TODO 日历视图
status: implemented
---

# TODO 日历视图

## 1. 需求

当前 TODO 页面用列表展示 `cron.yaml` 中的任务。列表适合编辑单个任务，但不容易回答这些问题：

- 某天会运行哪些任务？
- 一次性任务是否集中在同一时间？
- 每日、每周和每月任务在一个月内如何分布？
- 停用任务原本会出现在哪些日期？

新增 Calendar 视图，用月历展示一次性任务和重复任务。Calendar 只是现有 TODO 的另一种视图，不成为新的任务数据源。

## 2. 目标

- 在 TODO 页面提供 `List` 和 `Calendar` 两种视图。
- 月历同时展示一次性任务和重复任务。
- 启用任务使用蓝色状态方块，停用任务使用灰色状态方块。
- 点击日历条目后进入现有 TODO 编辑器，不新增第二套编辑表单。
- 日历时间和 runtime 实际调度时间使用相同的 cron、时区和 DST 语义。
- 当前 Console endpoint 和远程 Console endpoint 行为一致。
- Calendar 复用 TODO 页面已经加载的完整 task 列表，不增加请求。

## 3. 非目标

第一版不实现：

- 周视图、日视图和时间轴视图；
- 拖拽任务改变时间；
- 在日期格内直接编辑任务；
- 外部日历同步或 iCalendar 导入、导出；
- 新的日历文件或事件数据库；
- 任务执行历史和成功、失败状态；
- 在月历中展示 Heartbeat。Heartbeat 是系统任务，频率高，不属于用户 TODO 日程。

## 4. 第一性原理

### 4.1 `cron.yaml` 仍是唯一数据源

日历条目由现有 task 推导，不保存 occurrence，也不增加 calendar event ID。修改、保存、删除和手动运行仍使用现有 TODO API。

这样不会出现列表、日历和 runtime 各自保存一份状态的问题。

### 4.2 调度计算必须复用 runtime 规则

任务可以使用 IANA 时区、固定 UTC 偏移、步进、范围、列表，以及 `day-of-month` 和 `day-of-week` 的 OR 语义。浏览器端现有 cron preview 只负责文案，不应成为第二个调度器。

Calendar 使用一个纯前端 projection 函数实现与 `internal/cron` 相同的五段数字 Cron 规则。该函数只计算可见的 42 天，每个 task、每天最多返回一项。规则由单元测试与 runtime 的既有测试对照，不让 UI 组件散落调度判断。

### 4.3 第一版只做月视图

月视图已经能比较一次性任务与日、周、月重复任务。周视图和日视图需要新的时间轴布局、碰撞处理和移动端交互；在有真实需求前不增加。

### 4.4 继续使用现有编辑器

Calendar 负责浏览。点击条目后保持 Calendar 视图，并使用现有 TODO 编辑器。新增任务也继续使用当前 `Add TODO` 流程。

桌面端在月历右侧常驻详情区域。默认选中今天并显示当天的全部 task；点击其他日期后更新该列表，点击具体 task 后在原位显示现有编辑器。移动端继续在月历下方显示当天 agenda，进入编辑页后返回仍是原来的月份。这避免维护 dialog 或 calendar event editor，也不会让两套表单的校验行为产生差异。

### 4.5 不引入完整 Calendar 依赖

第一版只需要固定的七列月历、月份导航和日期格 overflow。使用 Vue、CSS Grid、`Intl` 和现有 Quail UI 组件即可，不增加 FullCalendar 一类依赖。

## 5. 路由和状态

使用现有 endpoint-scoped TODO 路由，通过 query 表达视图和月份：

```text
/e/:endpoint_ref/todo
/e/:endpoint_ref/todo?view=calendar&month=2026-08
```

- 缺少 `view` 时保持当前 List 视图。
- `month` 使用 `YYYY-MM`；缺少或无效时使用浏览器当前月份。
- 月份和视图写入 URL，不再写一份 local storage 状态。
- 浏览器返回、前进和刷新后恢复同一视图与月份。
- 当前选中 task 保持现有页面内状态，不在本需求中改变路由语义。

## 6. 桌面布局

List 模式保持当前两栏布局。Calendar 不保留内部 TODO 列表侧栏，桌面端固定使用月历和编辑区域两栏布局。点击条目或新增任务只更新右侧编辑器，不改变月历宽度。

```text
┌──────────────────────────────────────────────┬──────────────────────┐
│ [ List | Calendar ]  +   ‹ August ›  Today  │ TODO editor          │
├─────┬─────┬─────┬─────┬─────┬─────┬────────┤                      │
│ Sun │ Mon │ Tue │ Wed │ Thu │ Fri │ Sat    │ selected task fields │
├─────┼─────┼─────┼─────┼─────┼─────┼────────┤                      │
│ ... │ ... │ ... │ ... │ ... │ ... │ ...    │                      │
└─────┴─────┴─────┴─────┴─────┴─────┴────────┴──────────────────────┘
```

顶部区域包含：

- `List / Calendar` 视图切换；
- 上一个月、下一个月和 `Today`；
- 当前月份标题；
- 位于视图切换旁的现有 `Add TODO` 操作。

月份导航和添加按钮使用 QIcon。Calendar 不增加第二层 page bar。

第一版固定使用周日到周六的列顺序，不增加首日配置。

## 7. 日历条目

每个 task 在同一个日期格内最多出现一次：

- 一次性任务显示具体时间和标题；
- 重复任务显示 repeat QIcon、现有 schedule preview 和标题；
- 同一重复任务一天运行多次时仍只显示一行，schedule preview 说明频率；
- 启用任务显示蓝色状态方块；
- 停用任务显示灰色状态方块并降低文字对比度；
- 日期格内先排启用任务，再按当天第一次执行时间排序，最后使用 `cron.yaml` 顺序稳定排序。

日期格默认展示前三项。更多条目显示 `+N more`；点击后使用锚定在日期格上的菜单展示当天完整列表。菜单只在打开时渲染该日条目。

点击条目：

1. 选中对应 task；
2. 保持 Calendar 和当前月份；
3. 桌面端更新右侧常驻编辑器，移动端进入现有编辑页；
4. 移动端返回后继续浏览原来的月份；
5. 不自动保存或运行任务。

点击 `Add TODO` 使用相同方式打开新 task。只有用户明确点击 `List` 时才切换视图。

整个日期格都可以选择。选中日期时清除具体 task 选择，桌面右栏和移动端 agenda 显示当天的全部 task；点击列表中的 task 后再进入现有编辑器。进入当前月份时默认选中今天，浏览其他月份时默认选中该月 1 日。

点击空白日期不会创建任务。第一版不为点击日期增加隐含操作。

## 8. 移动端

移动端保留月视图，但日期格只显示状态点和条目数量。选中日期后，在月历下方展示当天 agenda。

```text
┌────────────────────────────┐
│ ‹   August 2026   ›  Today │
│ Sun Mon Tue Wed Thu Fri Sat │
│  3   4   5   6   7   8   9 │
│  •   ••  •   •   •   •   • │
├────────────────────────────┤
│ August 4                   │
│ ■ 09:00  Morning brief     │
│ ■ Repeat  Weekly review    │
│ □ 18:00  Archived reminder │
└────────────────────────────┘
```

- 月历和 agenda 使用同一份本地 projection，不按日期请求数据。
- 点击 agenda 条目进入现有移动端 TODO 编辑页；返回后恢复 Calendar 和原月份。
- 不把桌面条目文字强行压进窄日期格。

## 9. 时间语义

### 9.1 调度时区与显示时区

- task 的 `at` 或 `cron` 在 task 自己的 `tz` 中求值。
- Calendar 使用浏览器时区展示。
- 前端先按 task 时区求出执行 instant，再转换到 Calendar 显示时区和日期。
- Console 创建的 task 总会写入明确的 `tz`。手工编写且缺少 `tz` 的旧 task 在 Calendar 中按浏览器时区解释；runtime 仍按自己的本地时区执行。
- Calendar header 不显示浏览器时区；task 的时区继续在现有编辑器中查看和修改。

### 9.2 日期范围

月历 projection 只计算完整的可见六周网格：

- `from` 包含；
- `to` 不包含；
- 最大范围为 42 天；
- 范围边界按 Calendar 显示时区的本地午夜解释。

### 9.3 一次性任务

一次性任务只出现在 `at` 对应的显示日期。过去但仍留在 `cron.yaml` 中的任务仍出现在过去日期，不移动到今天。

### 9.4 重复任务

重复任务在可见范围内每个匹配日期出现一次。即使表达式一天匹配多次，也不为每一分钟生成一个 DOM item。

DST、不存在的本地分钟和重复的本地分钟沿用 runtime 当前语义。projection 在真实时间线上匹配 task 的本地时间，不自行修正规则。

## 10. 数据契约

不新增 Calendar API。TODO 页面继续只读取现有接口：

```text
GET /todo/tasks
```

该接口已经全量返回 task 的 `id`、`enabled`、`at`、`cron` 和 `tz`。Calendar 直接使用这些字段，不复制 title、content 等数据，也不增加 endpoint proxy 分支、授权方式或服务端 DTO。

## 11. Projection 实现

在 `web/console/src/core/todo-calendar.js` 提供一个纯函数。输入是完整 task 列表、可见范围和显示时区，输出是按日期分组所需的最小条目和无效 task ID。

它实现 runtime 当前支持的规则：

- 五段数字 Cron；
- `*`、列表、范围和步进；
- Sunday 的 `0`/`7` 别名；
- `day-of-month` 与 `day-of-week` 的 OR 语义；
- IANA 时区和 `UTC±offset`；
- DST 中不存在或重复的本地分钟。

每个 task 只解析一次 schedule。相同时区的 task 共用一次 wall-clock 转换，固定范围内扫描真实时间线；匹配后立即按 `task + date` 去重。projection 不复制 task 内容，组件仍按 `task_id` 关联已经加载的数据。

Calendar projection 不过滤停用 task。停用状态本身需要被展示；是否实际执行仍由 runtime 判断。Heartbeat 不在 `/todo/tasks` 的普通 task 列表中，因此不进入 projection。

## 12. 前端数据流

TODO 页面加载时仍只请求一次 `/todo/tasks`。进入 Calendar 或切换月份时：

1. 计算可见六周范围；
2. 对当前 task 列表执行本地 projection；
3. 用 `task_id` 关联 task；
4. 按日期生成固定七列网格。

切换 endpoint 后，现有 TODO 加载流程替换 task 列表，Calendar 自动重新计算。切换月份或保存 TODO 也只会重新计算当前 42 天，不增加网络状态或请求竞态。

如果存在未保存编辑，Calendar 继续显示上次保存的 schedule。不为未保存 draft 新增预览 API。

## 13. 性能边界

- List 和 Calendar 共用一次 `/todo/tasks` 请求。
- Calendar 切换月份不请求网络，也不按天或 task 发请求。
- projection 固定计算 42 天，不接受无界范围。
- 每个 task 每天最多一项，避免高频 cron 产生数万条分钟级 DOM item。
- 日期格只渲染前三项；完整 agenda 只渲染当前打开的日期。
- 第一版不分页。输出有 `task_count × 42` 的明确上界；出现真实性能数据后再决定是否调整计算方式。
- 不预取相邻月份，不增加后台刷新 timer。

## 14. Loading、空状态和错误

- 首次加载 Calendar 使用 `QSkeleton`，不使用 `QProgress`。
- 月份内没有 task 时仍展示完整日期网格，并在网格中显示 `No TODOs this month`。
- `/todo/tasks` 失败时沿用现有 TODO 页面错误处理。
- 本地 projection 发现无效 schedule 时显示一个低强度提示，不用 dialog 阻断浏览。

## 15. 无障碍

- 月份导航、视图切换、日期和 task 条目使用真实 button。
- task 的 accessible name 包含日期、时间、标题、启用状态和是否重复，不能只依赖颜色。
- 当前日期和选中日期有文字或 border 状态，不只使用背景色。
- 第一版使用正常 Tab 顺序，不实现自定义方向键 calendar grid。

## 16. 验收标准

1. TODO 页面可在 List 和 Calendar 之间切换，刷新后由 URL 恢复视图和月份。
2. Calendar 月视图包含完整六周网格，支持前月、后月和 Today。
3. 一次性和重复 task 都出现在正确的显示日期。
4. 有明确 `tz` 的 task 在浏览器时区、DST 和 cron 日期规则上与 runtime 调度一致。
5. 同一重复 task 每天最多显示一项；高频 cron 不生成分钟级 DOM item。
6. 启用 task 使用蓝色方块，停用 task 使用灰色方块，且状态可由 accessible name 读取。
7. Calendar 默认选中今天；桌面右栏显示选中日期的全部 task，点击 task 或新增任务后在原位显示编辑器，移动端返回后恢复原月份。
8. Desktop 日期格超过三项时显示 `+N more`；移动端使用日期格加 agenda。
9. Heartbeat 不进入 Calendar。
10. Local 和远程 Console endpoint 使用相同的 `/todo/tasks` API 和页面行为。
11. Calendar 不新增请求，月份切换也不请求网络。
12. production build 和前端 projection 测试通过。

## 17. 实现 Checklist

- [x] 为前端日期范围 projection 添加测试：一次性、每日、每周、每月和多次/日。
- [x] 添加 task timezone 与显示 timezone 转换测试。
- [x] 添加 DST、DOM/DOW OR、停用 task 和固定 UTC offset 测试。
- [x] 在 Console core 实现每 task、每天一项的 Calendar projection。
- [x] 确认 Calendar 只复用现有 `/todo/tasks`，不新增 API。
- [x] 在 TODO route 中加入 `view` 和 `month` query 状态。
- [x] 实现 List / Calendar 切换和桌面月历。
- [x] 实现日期格 overflow 菜单。
- [x] 实现整格日期选择、默认选中今天和桌面右栏 agenda。
- [x] 实现移动端月历与 agenda。
- [x] 复用现有 task editor、schedule preview 和状态方块样式。
- [x] 添加英文、简体中文和日文文案。
- [x] 保存 task 后重新计算当前 Calendar 范围。
- [x] 运行前端测试和 `pnpm build`。
