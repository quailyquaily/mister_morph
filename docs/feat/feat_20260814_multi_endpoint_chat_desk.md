---
date: 2026-08-14
title: 多 Endpoint Chat 工作台
status: implemented
---

# 多 Endpoint Chat 工作台

## 1. 需求

Console 的普通 Chat 一次只操作一个 endpoint。`/chat/desk` 提供一个类似平铺窗口管理器的工作模式，让用户在同一屏幕中操作多个 endpoint 的 Chat。

工作台不是仪表盘，也不是卡片网格。Chat panes 使用除 48px 纵向 tab bar 之外的全部窗口。原有全局侧栏和 page bar 不在该路由中显示。

入口位于 Agent Switcher 内。普通单 Agent Chat 保持不变。

## 2. 纵向 view tabs

左侧 tab bar 固定为 48px 宽。每个 tab 对应一套独立的 pane 布局，使用一个 48px 正方形按钮和较小的 emoji label。emoji 随 tab 保存，不会因为刷新或修改 pane 布局而改变。

tab 的 tooltip 和无障碍 label 包含 tab 顺序、pane 数量和离线状态。当前 tab 使用主题色标记；含离线 pane 的 tab 显示灰色状态点。

tab bar 底部保留三个入口：

- Plus QIcon：新建空 tab，初始 pane 数量为 0；
- Keyboard QIcon：查看完整快捷键；
- Logout QIcon：退出工作台，进入当前 pane 对应的完整 Chat。

除 tab 自己的 emoji 外，tab bar 中的操作入口统一使用 QIcon。

这不是原有全局导航的缩窄版本。Contacts、Memory、TODO、Settings 等入口不放进工作台 tab bar。

## 3. Pane 交互

第一次进入且没有已保存状态时，只创建一个空 tab。用户点击内容区的“添加分屏”后，才使用当前 endpoint 创建第一个 pane。每个 pane 的标题栏提供：

- 通过头像选择该 pane 使用的 endpoint；
- 选择或新建 topic；
- 向右分割；
- 向下分割；
- 关闭 pane。

分割后，新 pane 优先使用尚未打开的在线 endpoint；没有其他可用 endpoint 时沿用当前 endpoint。用户可随时在 pane 标题栏中更换 endpoint。更换 endpoint 会清空该 pane 保存的 topic，但不会改变其他 panes 或布局。

拖动分隔线可调整相邻 panes 的比例。分隔线支持键盘方向键，比例限制为 20% 到 80%。关闭 pane 后，它的兄弟节点自动占满原来的父区域。最后一个 pane 关闭或对应 Chat 被服务端删除后，当前 tab 回到空状态。

当前 pane 不改变自身边框或标题栏。围绕 pane 的 1px 主题色轮廓与分隔槽中央的视觉分割线重合；工作区边缘会裁掉外侧轮廓，因此只有实际围绕该 pane 的分隔线会高亮。分隔槽保留 5px 命中区，静止时只显示 1px 分隔线；hover 或拖动时，最近的指针位置显示较宽的拖动把手。鼠标离开只隐藏把手，不改变它的位置；尚无指针位置时使用分隔线中点。快捷键切换 pane 后，焦点直接进入该 pane 的 composer；鼠标点击只改变当前 pane，不抢走点击目标的焦点。

每个 pane 独立管理 endpoint、topic、草稿、历史、loading、error、task polling、展开状态和审批。一个 pane 的异步结果不能写入另一个 pane。

## 4. 键盘操作

工作台采用 tmux 式两步快捷键。先按下并松开 `Ctrl+B`，再按命令键。前缀在 2.2 秒后自动取消，`⌨️` tab 会在等待命令期间显示主题色状态。

| 按键 | 操作 |
| --- | --- |
| `Ctrl+B`，再按方向键或 `H/J/K/L` | 按空间方向激活 pane，并聚焦 composer |
| `Ctrl+B`，再按 `1` 至 `9` | 按 pane 顺序激活 pane，并聚焦 composer |
| `Ctrl+B`，再按 `N/P` | 激活下一个或上一个 pane，并聚焦 composer |
| `Ctrl+B`，再按 `%` 或 `V` | 向右分割当前 pane |
| `Ctrl+B`，再按 `"` 或 `S` | 向下分割当前 pane |
| `Ctrl+B`，再按 `X` | 关闭当前 pane |
| `Ctrl+B`，再按 `Enter` | 聚焦当前 Chat 输入框 |
| `Ctrl+B`，再按 `E` | 退出工作台 |
| `Ctrl+B`，再按 `?` | 打开快捷键列表 |
| `Tab` 聚焦分隔线，再按方向键 | 调整分割比例 |

前缀以外的普通按键不由工作台捕获，因此 Chat 输入不受影响。

## 5. 布局和持久状态

每个 tab 持有一棵独立的二叉分割树：

```text
tab(id, emoji, activePaneID)
└── layout: null | pane | split

pane(endpointRef, topicID)

split(row | column, ratio)
├── first: pane | split
└── second: pane | split
```

`layout: null` 表示 tab 内没有 pane。split tree 仍只表达局部分割所需的数据：方向、比例和 pane。新增 tab 容器不改变原来的平铺算法。

浏览器保存：

- tabs、tab emoji 和当前 tab；
- 每个 tab 的完整分割树和比例；
- pane ID、endpoint ref、topic ID 和当前聚焦 pane。

每次进入工作台都恢复这些状态。消息、审批、token 和服务端任务结果不写入 local storage；pane 挂载后从对应 runtime 重新读取。

恢复已保存 topic 时，先读取最近 topic 列表；目标不在列表中时，再直接读取该 topic，避免为了查找旧 Chat 遍历全部历史。直接读取返回 `404` 说明 Chat 已删除，对应 pane 随即从布局中移除。其他网络错误不会删除 pane。

endpoint 连接失败时，该 endpoint 仍保留在布局中。pane 显示离线状态，不读取历史，不允许切换或新建 topic，也不显示 composer。用户仍可切换 endpoint、关闭 pane 或退出工作台。

## 6. Chat 能力

在线 pane 支持：

- 选择和新建 topic；
- 查看最近的对话历史；
- 发送文本任务和停止运行中的任务；
- 查看计划、进度和推理；
- 处理审批；
- 跳转到完整 Chat。

工作台不复制普通 Chat 的 topic 侧栏、Workspace 文件树、目录浏览和文件预览。这些需要较大操作空间的功能继续留在完整 Chat。

任务到 Chat history item 的转换由普通 Chat 和工作台共同使用。运行中的任务以 `(endpointRef, taskID)` 标识，不能把不同 runtime 的相同 `taskID` 当成一个任务。

## 7. API

每个 pane 使用现有 endpoint-scoped 请求：

```text
GET  /api/proxy?endpoint=<ref>&uri=/topics
GET  /api/proxy?endpoint=<ref>&uri=/topics/<topic-id>
GET  /api/proxy?endpoint=<ref>&uri=/tasks
GET  /api/proxy?endpoint=<ref>&uri=/tasks/<task-id>
POST /api/proxy?endpoint=<ref>&uri=/tasks
POST /api/proxy?endpoint=<ref>&uri=/tasks/<task-id>/stop
GET  /api/proxy?endpoint=<ref>&uri=/approvals
POST /api/proxy?endpoint=<ref>&uri=/approvals/<approval-id>/<decision>
```

请求目标来自 pane 自己的 endpoint 状态，不能读取全局 `endpointState.selectedRef`。异步结果写入界面前，需要确认 pane 仍属于请求发起时的 endpoint 和加载版本。

Console Local 和远端 Console endpoint 都使用现有 task stream 帧格式。浏览器始终连接当前 Console 的 `/api/stream/ws`；远端 pane 会在连接参数中携带 endpoint ref，当前 Console 再使用已配置的 runtime token 连接目标 Console 的 `/runtime/stream/ws`。runtime token 不进入浏览器。WebSocket 不可用或断开时继续使用 `/tasks/<task-id>` polling。

## 8. 移动端

移动端保留同一个 48px 纵向 tab bar，不把桌面分割树缩成窄列。内容区一次显示当前 tab 的当前 pane；同一 tab 内的其他 panes 保持挂载，运行任务和 polling 不因 pane 切换而停止。

分割操作仍然修改同一棵布局树，因此返回桌面端后恢复原布局。移动端隐藏分隔线，不提供无效的比例拖动。

## 9. 性能边界

- 每个运行中的远程任务最多有一个 polling loop。
- 每个支持 stream 的运行中任务最多有一个 WebSocket。
- 已完成任务不继续 polling。
- 历史首次只读取固定数量的最近任务。
- 查找已保存的旧 topic 使用单个 topic API，不遍历全部 topic。
- 多个 panes 独立并行加载，不互相等待。
- 关闭 pane 时清理 timer、WebSocket 和未完成的界面更新。
- 拖动分隔线时只更新布局比例，local storage 写入延后合并。
- 工作台不预加载原侧栏使用的 contacts 和 persona 数据。

## 10. 非目标

第一版不实现：

- 浮动窗口；
- pane 拖拽换位；
- tab 重命名、排序和自定义 emoji；
- 自定义快捷键；
- 布局预设和撤销历史；
- 跨浏览器同步布局；
- 跨 endpoint 合并 topic 或 task；

## 11. 验收

1. Agent Switcher 中有工作台入口，路由为 `/chat/desk`。
2. 工作台隐藏原有全局侧栏和 page bar，仅保留 48px emoji view tabs。
3. 侧栏只显示 tabs；新 tab 初始包含 0 个 pane，操作入口使用 QIcon。
4. pane 可向右或向下分割；分隔线可拖动和使用键盘调整。
5. `Ctrl+B` 前缀支持方向、顺序、数字导航，以及分割、关闭、输入聚焦、帮助和退出；激活 pane 后 composer 获得焦点。
6. 每个 pane 可独立选择 endpoint、topic、发送任务、跟踪结果和处理审批。
7. 刷新或重新进入后恢复 tabs、布局、比例、endpoint、topic、tab emoji 和焦点。
8. 已删除 Chat 的 pane 被移除；网络错误不误删 pane。
9. 离线 endpoint 的 pane 保留并显示离线状态，Chat 交互禁用。
10. 移动端一次显示一个 pane，其他 panes 保持运行。
11. production build 和现有前端测试通过，普通 Chat 行为不回归。
