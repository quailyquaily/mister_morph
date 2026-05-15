---
date: 2026-05-15
title: Native Feel 对照优化计划
status: draft
---

# Native Feel 对照优化计划

## 背景

参考文章：[A Technical Deep Dive Into the New Raycast](https://www.raycast.com/blog/a-technical-deep-dive-into-the-new-raycast)，发布时间为 2026-05-14。

Raycast 2.0 的核心选择不是“用 WebView 做桌面应用”这么简单，而是：

1. 原生壳负责操作系统行为。
2. Web 前端负责复杂 UI。
3. 后端负责长期运行的业务逻辑。
4. 性能问题用测量和边界控制处理，不靠口号。

MisterMorph 也有两类界面：

1. 桌面版：`desktop/wails`，负责窗口、生命周期、子进程、WebView。
2. Console Web：`web/console`，负责浏览器和桌面 WebView 共用的管理界面。

本文按 Raycast 文章中的技术点逐项对照，列出我们应该改的地方，以及不应该改的地方。

## 当前事实

桌面版当前是 Wails v3 桌面壳。
它启动一个 `mistermorph console serve` 子进程，监听随机 loopback 端口，再把 Wails WebView 的请求代理到这个本地服务。

Console SPA 资源由后端 binary 持有。
桌面壳不直接托管前端资源。

桌面壳目前暴露的原生能力很少：

1. 打开外部 URL。
2. 重启应用。
3. 启动、监督、停止本地 backend。
4. 代理 WebView 请求。

Console Web 是 Vue 3 + quail-ui + Vite 的单页应用。
它已经有启动遮罩、外链处理、路由、移动端布局、设置、聊天、日志、文件、统计等页面。
当前 `web/console` 使用 `quail-ui@0.9.9`。
`quail-ui@0.9.9` 已经包含组件级 native feel 修正，组件层问题应优先通过依赖升级吸收，不在本仓库重复写局部覆盖样式。

明显缺口是：

1. 桌面前端没有稳定的 `desktop mode` 信号。
2. backend 启动失败时，用户看到的是弱错误状态，不是桌面应用应有的启动失败界面。
3. 没有桌面启动、内存、WebView 行为的基线数据。
4. 桌面壳和前端之间的消息还没有明确扩展规则。
5. 桌面版还没有一套用新 WebView 打开内容路由的多窗口机制。

## 固定约束

以下方向已经确定不做：

1. 不引入 Rust。
2. 不自己实现平台桌面壳。
3. 不把 Console Web 分裂成桌面专供前端和浏览器前端。

这些约束的含义是：

1. 桌面壳继续基于 Wails。
2. Console Web 仍然是唯一前端 GUI。
3. 桌面差异通过 desktop mode、Wails binding、新窗口路由和少量条件渲染处理。

## 设计原则

1. WebView 是渲染面，不是浏览器产品。
2. 原生壳只处理必须靠操作系统完成的事情。
3. Console Web 继续保持一套代码，同时支持浏览器和桌面。
4. 桌面差异必须能被用户感知，否则不做。
5. 性能和内存先测量，再设预算。
6. 不为了像 Raycast 而引入 Swift、C#、Rust 或自研 IPC 框架。
7. 桌面窗口只是承载方式，业务内容仍由 Console Web 组件和路由提供。

## 对照

### 1. 技术栈选择

Raycast 选择自研混合架构：

1. macOS 用 Swift/AppKit。
2. Windows 用 C#/.NET/WPF。
3. UI 用 React/TypeScript。
4. 后端用 Node。
5. 性能敏感部分用 Rust。

MisterMorph 当前选择：

1. 桌面壳用 Wails v3。
2. UI 用 Vue/JavaScript。
3. 后端和 agent runtime 用 Go。
4. 桌面壳通过子进程运行同一个 `console serve`。

建议：

1. 保留 Wails，不重写成 Swift/AppKit 或 C#/WPF。
2. 保留一套 Console Web，不拆出桌面专用前端。
3. 平台专用代码只用于 Wails 无法覆盖的系统行为，不作为新的桌面壳方向。

优先级：已确定约束。

### 2. 原生壳职责

Raycast 的原生壳负责窗口、全局快捷键、菜单栏、托盘、WebView 初始化、后端监督等平台行为。

MisterMorph 桌面壳现在主要负责：

1. 启动 backend 子进程。
2. 等待 `/health`。
3. 代理本地 HTTP。
4. 提供少量 Wails binding。

建议：

1. 增加清晰的 backend 启动失败界面。
2. 保留 backend 子进程模型，先强化监督和错误呈现。
3. 桌面壳只新增高价值原生能力，例如打开日志、打开配置、复制诊断信息、持久化窗口大小。
4. 不把业务逻辑搬进 Wails 壳。

优先级：P0/P1。

### 3. WebView 不是浏览器

Raycast 文章明确提到，桌面 WebView 如果沿用浏览器习惯，会显得不像本地应用，例如 `cursor: pointer`、网页式 hover、DOM 弹层受窗口裁剪等。

MisterMorph 当前 Console Web 同时服务浏览器和桌面。
这本身没问题，但需要区分运行环境。

建议：

1. 在前端建立稳定的 desktop mode 信号，例如根节点设置 `data-runtime="desktop"`。
2. 将 Console 依赖升级到 `quail-ui@0.9.9`，先解决组件级 native feel 问题。
3. desktop mode 下单独审查鼠标指针、hover、focus、滚动条、文本选择、右键菜单。
4. 外部链接继续通过桌面壳打开系统浏览器。
5. DOM 弹窗先保留；只有当弹窗被 WebView 边界裁剪、或需要系统级行为时，再考虑原生弹窗。
6. 设置页暂时仍作为 Console 页面，不急着做原生设置窗口。

优先级：P1。

### 4. 启动和失败状态

Raycast 花了很多精力处理 WebView 首帧、空白帧和打开窗口时的闪烁。

MisterMorph 当前已有 Console boot overlay，并且桌面壳会等 backend `/health`。
但 backend 启动失败时，还缺少面向用户的桌面级错误界面。

建议：

1. 桌面启动时先显示原生或极小 WebView 启动态。
2. backend 启动失败时显示明确错误：可重试、退出、打开日志、复制诊断信息。
3. 区分几类错误：找不到 backend、下载失败、端口失败、配置错误、健康检查超时、子进程提前退出。
4. WebView 进入主界面前避免展示空白页或代理错误页。

优先级：P0。

### 5. WebView 平台差异

Raycast 单独处理 WebKit 和 WebView2 的差异，包括后台 throttling、窗口 resize、首帧闪烁、隐藏窗口渲染等。

MisterMorph 当前已经有 Linux WebView GPU policy，但还没有系统化的桌面 WebView 行为测试。

建议建立桌面测试矩阵：

1. macOS：WebKit、窗口关闭隐藏、外链、复制粘贴、IME、滚动、长 Markdown。
2. Windows：WebView2、缩放、窗口 resize、输入法、下载、外链、更新流程。
3. Linux：WebKitGTK、GPU policy、AppImage 启动、字体、剪贴板。

优先级：P1。

### 6. IPC 边界

Raycast 用声明式接口和生成的 typed client 约束 Swift/C#/Node/WebView/Rust 之间的通信。

MisterMorph 当前 IPC 很小。
`OpenExternalURL` 使用桌面消息前缀，`RestartApp` 使用 Wails binding。

建议：

1. 现有规模下不引入 IPC 代码生成。
2. 新增桌面能力时，优先使用命名清晰的 Wails binding 方法。
3. 不继续扩张字符串前缀消息。
4. 当桌面 binding 超过少量方法，或出现前后端契约反复漂移，再考虑集中声明接口。

优先级：P1。

新增 binding 规则：

1. Wails 方法放在 `main.App` 上，方法名使用动词开头，例如 `OpenWindow`、`OpenDesktopLog`。
2. 前端只通过 `window.wails.Call.ByName("main.App.Method", payload)` 调用 binding。
3. payload 必须是小的 JSON 对象；Go 侧用显式 struct 和 `json` tag 接收。
4. 不把大对象、敏感值、完整文件内容放进 payload 或 URL。
5. 打开 Console 内容窗口时只传 route path 或 id；Go 侧必须做同源和路由前缀校验。
6. binding 失败时返回 error，由前端决定如何提示。
7. 每个新增 binding 至少补一组 Go 侧参数校验测试。
8. 旧的 `mistermorph:open-url:` raw message 只保留给外链打开，不再作为新能力的扩展方式。

### 7. 后端进程模型

Raycast 用长期运行的 Node backend，原生壳负责监督。

MisterMorph 用 Go backend 子进程运行 `console serve`。
这和 CLI、systemd、桌面共用同一条路径，成本低，也更符合当前项目。

建议：

1. 继续保留子进程模型。
2. 改善进程监督：启动阶段错误分类、退出原因、最近日志、重启动作。
3. graceful shutdown 要保持稳定，避免桌面退出时留下后台进程。
4. 不做单进程桌面 runtime，除非子进程模型出现明确瓶颈。

优先级：P0/P1。

### 8. 性能和内存

Raycast 文章给出了 WebView、后端、原生壳的内存拆分，并承认 WebView 架构有更高 baseline。

MisterMorph 当前缺少这些数字。
没有数字，就无法判断哪些优化有效。

建议先增加基线测量：

1. 桌面冷启动到首个可交互页面。
2. backend 从启动到 `/health` 成功。
3. Console 路由切换耗时。
4. 桌面壳、WebView、backend 的稳定内存。
5. 长聊天、长日志、长 Markdown 页面滚动表现。

拿到基线后再设预算。
不要在没有数据时重写架构。

优先级：P1。

### 9. 前端加载和大内容

Raycast 提到持续优化懒加载、图标、图片和前端内存。

MisterMorph Console 有聊天、Markdown、日志、文件、统计等页面。
这些页面的内容大小差异很大。

建议：

1. 检查路由级懒加载是否覆盖主要页面。
2. Markdown renderer、图表、代码高亮只在需要的页面加载。
3. 日志、长文件、长聊天优先用分页、窗口化或增量渲染。
4. 图片预览继续限制尺寸和解码成本。

优先级：P1/P2。

### 10. 原生文件索引和 Rust core

Raycast 为文件搜索写了 Rust indexer，因为它要跨平台扫描整盘并保持低延迟。

MisterMorph 当前没有同类需求。
Console 的文件、日志、memory、cron、contacts 都是已知状态文件或工作区文件。

建议：

1. 不引入 Rust core。
2. 不做全盘文件索引。
3. 只在日志、状态文件或 artifact 列表变大时做服务端分页和过滤。

优先级：P3。

### 11. 多窗口

Raycast 有 Launcher、AI Chat、Notes、Settings 等多个窗口入口。

MisterMorph 当前更像一个管理控制台，但桌面版仍需要多窗口能力。
在 Wails 里，多窗口的本质是打开新的 WebView window。
因此我们的目标不是新做一套桌面 UI，而是在 desktop mode 下让某些内容路由能被新 WebView 承载。

用户理解是正确的：需要把弹窗的内容部分从弹窗壳里拆出来。
例如：

```text
XXXDialog
  -> 浏览器形态：QDialog + XXXDialogContent
  -> 桌面形态：新 WebView 打开 /window/xxx，路由页渲染 XXXDialogContent
```

这样同一份内容组件可以被两种承载方式复用：

1. 非 desktop mode：继续嵌入 `QDialog`。
2. desktop mode：通过 Wails 打开新 WebView，WebView 访问一个专用路由。

拆分边界应当是：

1. `XXXDialogContent` 只负责内容、表单、校验、保存、取消事件。
2. `XXXDialog` 只负责 `QDialog` 的打开关闭、标题区和尺寸。
3. `XXXWindowView` 只负责从路由读取参数、加载数据、渲染 `XXXDialogContent`。
4. 需要传数据时优先传 id 或 route params，不把大对象或敏感信息塞进 URL。
5. 保存后的状态同步走现有 API、store 或重新加载，不依赖父窗口内存。

不是所有弹窗都必须第一批改。
简单 confirm、toast、轻量菜单可以继续保持当前形态。
适合新窗口化的是内容较重、需要长时间停留、会被用户并行参考的界面。

建议：

1. 先实现 desktop mode。
2. 在 Wails binding 中提供打开新窗口的能力，参数只包含路由和基础窗口选项。
3. 增加 `window` 路由分组，用于承载可独立打开的内容。
4. 将适合多窗口的弹窗按 `XXXDialogContent` 方式拆分。
5. 非 desktop mode 保持现有 QDialog 形态。

优先级：P1/P2。

### 12. 键盘、输入法和辅助功能

Raycast 的 native feel 很多来自细节：快捷键、焦点、输入、菜单、复制粘贴、弹层行为。

MisterMorph 是 agent 控制台，聊天输入、日志阅读、配置编辑都依赖这些细节。

建议：

1. 审查桌面版常用快捷键：复制、粘贴、全选、查找、关闭窗口、刷新。
2. 确认 textarea、Markdown editor、dialog 在中日英输入法下行为正确。
3. 保留清晰 focus ring。
4. 检查屏幕阅读器能读到启动状态、错误状态和主要导航。

优先级：P1。

### 13. 发布和更新

Raycast 的桌面体验也包括安装、签名、更新。

MisterMorph 已有 DMG、AppImage、Windows zip 和 updater manifest。
签名文档已经存在，但 Windows 仍是 zip 发行。

建议：

1. 先保证桌面启动、错误状态和常用交互。
2. 再补 Windows 安装器或更完整的签名路径。
3. 更新流程要有失败提示和可恢复路径。

优先级：P2。

## 推荐顺序

第一阶段先做用户会直接感知的桌面问题：

1. backend 启动失败界面。
2. desktop mode 信号。
3. `quail-ui@0.9.9` 升级。
4. 桌面外链、复制粘贴、输入法、窗口大小持久化审查。
5. 启动时间和内存基线。

第二阶段做更直接的桌面体验：

1. 新 WebView 多窗口机制。
2. 适合新窗口化的弹窗内容组件拆分。
3. Windows 签名和安装器。
4. 自动更新失败提示和恢复路径。
5. 新增桌面 binding 的命名和契约规则。

第三阶段做 WebView 和前端性能：

1. macOS、Windows、Linux WebView 行为矩阵。
2. 长聊天、长日志、长 Markdown 的渲染优化。
3. 路由和重型依赖懒加载检查。
4. 前端包体和渲染内存基线。

## 非目标

1. 不把 Wails 重写成 Swift/AppKit 或 C#/WPF。
2. 不切换到 Electron。
3. 不把 Console Web 拆成桌面和浏览器两套 UI。
4. 不引入 Rust core 或全盘文件索引。
5. 不为了两个 Wails binding 引入 IPC 代码生成。
6. 不把 agent 业务逻辑搬进桌面壳。

## Checklist

- [x] P0：增加桌面 backend 启动失败界面，包含重启、退出、复制诊断信息。
- [x] P0：为启动失败界面补打开日志入口。
- [x] P0：为 backend 启动失败建立错误分类。
- [x] P1：给 Console 前端增加稳定的 desktop mode 信号。
- [x] P1：将 `web/console` 的 `quail-ui` 升级到 `0.9.9`。
- [x] P1：审查 desktop mode 下的 cursor、hover、focus、右键菜单和滚动条。
- [x] P1：确认桌面外链统一走系统浏览器。
- [x] P1：持久化桌面窗口大小和位置。
- [x] P1：在 Wails binding 中增加打开新 WebView window 的能力。
- [x] P1：增加 desktop window 专用路由分组。
- [ ] P2：选择第一批适合新窗口化的弹窗，拆出 `XXXDialogContent`。
- [ ] P3：建立 macOS、Windows、Linux WebView 行为测试清单。
- [ ] P1：记录冷启动、backend ready、首屏可交互、内存占用基线。
- [ ] P1：检查中日英输入法、复制粘贴、全选、查找等基础交互。
- [x] P1：给新增桌面 binding 制定命名和契约规则。
- [ ] P3：检查 Console 路由和重型依赖懒加载。
- [ ] P3：优化长聊天、长日志、长 Markdown 的渲染路径。
- [ ] P2：评估 Windows 签名和安装器。
- [ ] P2：完善自动更新失败提示和恢复路径。
