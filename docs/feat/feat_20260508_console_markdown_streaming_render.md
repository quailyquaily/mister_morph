---
date: 2026-05-08
title: Console Markdown 完整流式渲染计划
status: implemented
---

# Console Markdown 完整流式渲染计划

## 0) 当前实现摘要

本分支已经实现 renderer-owned streaming path：

- Console 继续传完整 Markdown 快照，只额外传 `streaming`、`streamMode`、`streamProfiler`。
- `web/markdown-renderer` 内部做 smoothing、stream repair、block queue 和 block 级更新。
- 普通文本 block 做字符 fade；代码、表格、公式、图表、HTML 等 heavy block 跳过字符动画。
- streaming 阶段走完整 Markdown HTML 路径，inline KaTeX 和 sanitized raw HTML 可见。
- code block 同步生成最终 wrapper、语言标签和复制按钮，Shiki 只异步更新 `code` 内部 token。
- 完整 diagram/math fence 会在 streaming 中增强；未闭合 fence 先按代码显示。
- 容器尺寸变化通过 `ResizeObserver` 回传，用户在底部时聊天列表继续贴底。
- streaming/final 的 block 间距、代码块外壳和闭合 fence 换行已对齐。
- DOM 高度只来自真实内容；没有高度地板，也没有额外尾部预留高度。
- profiler 里的 `reserveChars` 是内部释放缓冲，只影响吐字节奏，不参与 DOM 高度。

## 1) 背景

Console Local 已经能把 agent 最终回答的部分文本通过 WebSocket 推到前端。

改造前链路是：

```text
LLM OnStream
  -> FinalOutputStreamer 提取 final.output 快照
  -> consoleStreamHub 发布完整文本快照
  -> ChatView 写入 history item.text
  -> ChatRichContent 拆 artifact 段
  -> MarkdownContent 调 MarkdownRenderer.update(source)
  -> MarkdownRenderer 整篇 Markdown 转 HTML、清空 DOM、重建 DOM、再增强代码块和图表
```

后端推完整文本快照是合理的。provider 原始 delta 是 JSON 流，浏览器不应该直接展示它。

改造前的问题在前端渲染层：每次快照变化，renderer 都把整段 Markdown 全量重渲染。

本计划的目标不是做一个简化版，而是做完整流式渲染体验：平滑释放、block 级增量渲染、队列控制、普通文本字符动画、重组件延迟增强、性能统计。

## 2) 设计原则

流式 Markdown 渲染需要处理这些事实：

1. 平滑释放不是固定 cps，而是根据 chunk、backlog、输入活跃状态动态调整。
2. block queue 是核心，不只是锦上添花。
3. 字符动画只作用于普通文本，必须跳过 code、pre、table、svg、katex。
4. 流式中要先修补半截 Markdown，再切 block。
5. profiler 要记录 input、animation frame、block render、block lex、root commit。

## 3) 目标

1. 后端 stream 协议不变，继续接收完整文本快照。
2. 前端 renderer 内部维护 target source 和 displayed source。
3. displayed source 用动态 smoothing 释放，而不是固定速度。
4. Markdown 先做 streaming repair，再按顶层 block 拆分。
5. block 级增量 DOM 更新，已稳定 block 不重建。
6. block queue 控制主线程压力。
7. 普通文本节点做字符级 fade，代码、表格、公式、图表不做字符动画。
8. streaming 过程中让重组件保持稳定外壳，block 稳定或最终完成后再完整增强。
9. 提供 debug profiler，能量化平滑程度和卡顿来源。
10. 最终 `streaming=false` 后，渲染结果必须与完整 Markdown 一致。

## 4) 非目标

1. 不把 provider 原始 delta 发给浏览器。
2. 不改 `FinalOutputStreamer` 的语义。
3. 不把 stream delta 写入持久化事实源。
4. 不把 artifact preview 合并进 MarkdownRenderer；artifact 仍由 `ChatRichContent` 负责拆分。
5. 不为了动画牺牲最终 Markdown 正确性。

## 5) 目标体验

普通 Markdown 回答应该达到：

- 首段文本出现延迟小于 100ms。
- 流式过程中没有整篇闪烁。
- 已稳定段落不重排、不重新动画。
- 当前段落逐字或小批量柔和显示。
- 大段落不会慢到拖尾数秒。
- 用户停在底部时，气泡高度增长后列表继续贴底。
- 用户输入、滚动、复制按钮不被明显阻塞。
- 任务结束后代码高亮、复制按钮、KaTeX、图表恢复完整功能。

复杂内容的策略：

- 代码块：流式中同步生成最终外壳和复制按钮，Shiki 只异步更新 `code` 内部 token。
- 表格：流式中正常显示，但不做字符动画。
- KaTeX：流式中走完整 Markdown HTML 路径，失败则最终重渲染。
- Mermaid / Graphviz / Infographic：完整 fence 在流式中渲染图；未闭合 fence 先按代码显示。
- artifact fence：由 `ChatRichContent` 继续拆分，renderer 不接管。

## 6) 架构

### 6.1 Console 接线层

只做状态传递，不做 Markdown 逻辑。

改动文件：

- `web/console/src/views/ChatView.js`
- `web/console/src/components/ChatRichContent.js`
- `web/console/src/components/MarkdownContent.js`

新增 props：

```js
streaming: Boolean
streamMode: "realtime" | "balanced" | "silky"
streamProfiler: Boolean
```

`ChatView` 判断：

```text
item.role === "agent" && !isTerminalStatus(item.status)
```

满足时传 `streaming=true`。

### 6.2 MarkdownRenderer 流式状态

核心改动放在 `web/markdown-renderer/src/index.js`。

`MarkdownRenderer` 新增状态：

```text
targetSource          最新收到的完整 source
displayedSource       当前实际渲染 source
targetChars           targetSource 的 unicode char 数组
displayedCharCount    displayedSource 已释放字符数
streamRAF             requestAnimationFrame id
streamWakeTimer       延迟唤醒 timer
streamOptions         当前 stream mode / profiler / animation 配置
streamBlocks          当前 block 列表
blockEntries          blockKey -> block entry
revealedBlockCount    已完全 revealed 的 block 数
charBirthsByBlock     blockKey -> char birth timestamps
```

非 append-only 更新直接走全量渲染：

- source 被替换。
- theme 变化。
- format 变化。
- streaming 从 false 变 true 但旧状态不可复用。
- source 清空。
- segment identity 变化。

### 6.3 渲染模式

保留现有完整路径，新增 streaming 路径。

```text
update(source, options)
  -> if !streaming: renderFull(source)
  -> if streaming and append-only: updateTarget + scheduleSmoothReveal
  -> if streaming but not append-only: syncImmediate + renderStream(source)
```

完整路径：

- 复用当前 `markdownToHtml(...)`。
- 复用当前 `enhanceMarkdown(...)`。
- 清理 streaming state。

流式路径：

- 平滑释放 displayed source。
- repair Markdown。
- split blocks。
- update block DOM。
- 对普通文本做字符动画。
- 对重组件做 block 级增强，代码块先固定最终外壳。

## 7) 动态 smoothing

不要用固定 `cps`。固定 cps 无法同时处理慢 token、突发 chunk、任务结束 drain。

每个 preset 定义：

```text
defaultCps
minCps
maxCps
activeInputWindowMs
settleAfterMs
targetBufferMs
largeAppendChars
flushCps
maxFlushCps
settleDrainMinMs
settleDrainMaxMs
```

建议初值：

```text
realtime:
  defaultCps 50
  minCps 24
  maxCps 96
  flushCps 170
  maxFlushCps 360
  targetBufferMs 40
  largeAppendChars 180

balanced:
  defaultCps 38
  minCps 18
  maxCps 72
  flushCps 120
  maxFlushCps 280
  targetBufferMs 120
  largeAppendChars 120

silky:
  defaultCps 28
  minCps 14
  maxCps 56
  flushCps 96
  maxFlushCps 220
  targetBufferMs 170
  largeAppendChars 100
```

算法要点：

1. 记录 append chunk 大小的 EMA。
2. 记录 append 到达速度的 EMA。
3. 输入活跃时保留少量 target lag，避免每个 token 都触发渲染。
4. backlog 超过目标 lag 时加速。
5. 输入停止一段时间后进入 settling，限制尾部 drain 时长。
6. 一次 append 超过 `largeAppendChars` 时提高追赶压力，但仍按帧释放，避免一整行或多行突然出现。
7. RAF 的 `dt` 上限设为 50ms，避免后台切回一次释放过多。
8. backlog 清空后再次收到内容时进入 resume warmup，先限制单帧释放量，再逐步回到正常速度。

## 8) Markdown repair

流式内容经常出现半截结构：

- 未闭合 code fence。
- 未闭合 HTML。
- 未结束 table row。
- 未结束 list。
- 半截 blockquote。

在 block lexer 前需要 repair。

建议方案：

1. 优先评估新增 `remend` 依赖，用它做流式 Markdown 修补。
2. 如果不引入依赖，则先实现最小 repair：
   - 未闭合 fenced code 自动补 closing fence。
   - 未闭合 HTML 不强行渲染 raw HTML。
   - 末尾 table 行不完整时把最后一行留在 streaming block。

完整版本建议使用 `remend`，因为它直接覆盖 streaming Markdown repair 这个问题。新增依赖时需要同步检查 license，并更新 credits。

## 9) Block 切分

当前项目已经依赖 unified / remark。两条可选路径：

1. 使用 `marked.lexer` 切 block，行为接近常见 Markdown block parser。
2. 使用 remark AST 顶层节点 offset 切 block，依赖更少，和现有 pipeline 一致。

建议第一版完整实现使用 remark AST：

```text
remarkParse + remarkGfm + remarkMath
```

从顶层节点 `position.start.offset` 和 `position.end.offset` 切 raw block。

如果 offset 缺失或 parse 失败，退回单 block。

block 结构：

```text
key
raw
startOffset
endOffset
charCount
kind
heavy
state
```

`kind` 可取：

```text
paragraph
heading
list
blockquote
code
table
html
math
diagram
other
```

`heavy=true` 的 block：

- code
- table
- math
- diagram
- raw HTML

这些 block 不做字符动画。

## 10) Block queue

block 状态：

```text
revealed   已稳定，直接显示
animating  旧 tail 之后的新 block，正在完成 reveal
streaming  当前最后一个仍在增长的 block
queued     暂不渲染
```

规则：

1. 最后一个 block 是 `streaming`。
2. 当新 block 出现时，之前的 tail 同步晋升为 `revealed`，避免重新动画。
3. `revealedCount` 之后第一个非 tail block 为 `animating`。
4. `animating` 完成后推进 `revealedCount`。
5. `queued` block 不创建 DOM。

这样可以避免上游一次推来大量文本时，浏览器一次性渲染所有后续 block。

## 11) DOM 增量更新

`MarkdownRenderer` 维护：

```text
blockEntries: Map<blockKey, {
  raw
  element
  cleanupFns
  enhanced
  codeHighlightState
  charBirths
  state
}>
```

更新规则：

1. 新 block：创建 `.mmr-markdown-block`。
2. raw 未变：复用 DOM。
3. raw 变化：只替换该 block 内部 DOM。
4. block 消失：执行该 block cleanup。
5. queued block：不渲染。
6. revealed block：不再应用字符动画 plugin。

流式路径不能再调用 `root.replaceChildren()` 清整篇文章。

## 12) 字符动画

完整版本要做字符级 fade，但范围必须窄。

允许动画的节点：

- `p`
- `h1` 到 `h6`
- `li`

跳过：

- `pre`
- `code`
- `table`
- `svg`
- `.katex`
- `.mermaid`
- `.mmr-diagram`
- raw HTML 容器

实现方式：

1. 在 streaming render 的 DOM fragment 上遍历普通文本节点。
2. 每个字符包一层 `<span class="mmr-stream-char">`。
3. 每个 block 维护 char birth timestamps。
4. 新字符分配 birth time。
5. 已 revealed 字符使用 `.mmr-stream-char-revealed`，关闭动画。

阈值：

- 单 block 超过 1200 字符时，不做字符级 span，退回 block fade。
- 单次 frame 新增超过 200 字符时，合并成 chunk span，避免 DOM 暴涨。
- 用户设置 `prefers-reduced-motion: reduce` 时关闭字符动画。

CSS：

```css
.mmr-stream-char {
  opacity: 0;
  animation: mmr-stream-char-in 280ms cubic-bezier(0.33, 0, 0.67, 1) forwards;
}

.mmr-stream-char-revealed {
  opacity: 1;
  animation: none;
}
```

不建议第一版加 translate 位移。纯 opacity 更稳，也更不容易引发布局抖动。

## 13) 重组件增强

`enhanceMarkdown` 保持单一路径，但接收 block 级选项。

- code block 在 streaming 阶段同步生成最终 wrapper、语言标签和复制按钮。
- Shiki streaming tokenizer 只更新 `code` 内部 token，不替换外层 DOM。
- math fence 和 diagram 只在 fence 完整时渲染。
- 未闭合 diagram/math fence 先按代码显示。
- inline code copy 继续由 `enhanceMarkdown` 处理。

这样可以保留完整能力，同时避免代码块在普通 `<pre>` 和增强 wrapper 之间反复切换。

## 14) 最终渲染收尾

当 Console 传入 `streaming=false`：

1. 停止 RAF。
2. displayed source 立即追到 target source。
3. 保留字符动画 800-1000ms，不立刻清掉。
4. 延迟后执行 full enhancement。
5. 最终触发一次 `rendered` 事件。

这样尾部动画不会被最终完整渲染打断。

如果最终 full enhancement 检测到 DOM 与完整 Markdown 不一致，以完整 Markdown 为准。

## 15) Profiler

新增 lightweight profiler，只在 debug option 开启时记录。

记录项：

- input append count / chars。
- target chars / displayed chars / backlog。
- reveal chars。
- RAF frame interval。
- animation frame JS duration。
- slow frame count。
- markdown repair duration。
- block lex duration。
- block count。
- block render duration。
- block mount / update count。
- enhanced block count。
- full enhancement duration。

慢帧定义：

```text
animation frame JS duration >= 4ms
long frame interval >= 50ms
```

Console UI 里第一版不用做面板，可以先挂到：

```text
window.__MISTER_MORPH_MARKDOWN_STREAM_PROFILER__
```

后续需要时再做可视化面板。

## 16) 文件改动计划

### 16.1 Markdown renderer

`web/markdown-renderer/src/index.js`

- 新增 streaming options。
- 新增 smoothing controller。
- 新增 stream state reset / cleanup。
- 新增 repair streaming source。
- 新增 split blocks。
- 新增 block queue。
- 新增 block DOM cache。
- 新增 char animation wrapping。
- 拆分 light/full enhancement。
- 保留现有 full render 行为。

`web/markdown-renderer/src/styles.css`

- 新增 `.mmr-markdown-block`。
- 新增 `.mmr-stream-char`。
- 新增 block fade。
- 新增 `prefers-reduced-motion` fallback。

`web/markdown-renderer/README.md`

- 更新 streaming notes。
- 记录 debug profiler 开关。

`web/markdown-renderer/package.json`

- 如果使用 `remend`，新增依赖。

`assets/credits/data.json`

- 如果新增依赖，补 license 记录。

### 16.2 Console

`web/console/src/components/MarkdownContent.js`

- 增加 `streaming`、`streamMode`、`streamProfiler` props。
- watch key 纳入这些 props。
- 传给 renderer update。
- 使用 `ResizeObserver` 把 streaming 高度变化转成 `rendered` 事件，让聊天列表在底部时持续贴底。

`web/console/src/components/ChatRichContent.js`

- 透传 `streaming`、`streamMode`、`streamProfiler`。
- source segment 变化时强制 renderer reset。

`web/console/src/views/ChatView.js`

- 增加 `historyItemStreaming(item)`。
- agent message 未终止时传 `streaming=true`。
- 可通过 debug flag 打开 profiler。
- `rendered` 事件在 streaming 时节流。

## 17) 实施顺序

### Step 1：先加完整状态模型，不改变视觉

目标：接线完整，行为仍接近当前。

任务：

1. Console 传 `streaming` 到 renderer。
2. renderer 识别 streaming/full 两种 update。
3. append-only 检测。
4. streaming state reset。
5. 保持全量渲染，先不启用 smoothing。

验收：

- 现有 Markdown 渲染无回归。
- streaming/full 状态切换正确。

### Step 2：动态 smoothing

目标：source 更新不直接等于 DOM 更新。

任务：

1. 实现 target/displayed 状态。
2. 实现三档 preset。
3. 实现 large append immediate sync。
4. 实现 settling drain。
5. 实现 RAF cleanup。

验收：

- 慢 token 和突发 chunk 都能平滑显示。
- 任务结束后 1 秒内追平。
- source 替换时不会错误播放旧内容。

### Step 3：repair + block split

目标：把 displayed source 转成稳定 block 列表。

任务：

1. 引入或实现 streaming repair。
2. 使用 remark AST offset 切 block。
3. 标记 block kind / heavy。
4. parse 失败 fallback。

验收：

- 未闭合 code fence 不破坏后续 DOM。
- 复杂 list/table 不导致整页闪烁。

### Step 4：block DOM cache

目标：已稳定 block 不重建。

任务：

1. 建立 block entry map。
2. 新 block mount。
3. raw 不变时复用。
4. raw 变化只更新该 block。
5. 删除 block cleanup。

验收：

- 长文追加时旧段落 DOM identity 不变。
- 不再流式中整篇 `replaceChildren`。

### Step 5：block queue

目标：限制一次渲染的 block 数。

任务：

1. 实现 revealed / animating / streaming / queued。
2. 新 block 出现时旧 tail 晋升。
3. queued block 不渲染。
4. animating 完成后推进 revealed。

验收：

- 一次 append 很多 block 时，DOM 不一次性爆发增长。
- 当前 tail 继续流式显示。

### Step 6：字符动画

目标：普通文本达到细腻、稳定的流式观感。

任务：

1. 遍历 block fragment。
2. 只处理 p/h/li 文本节点。
3. 跳过 heavy block 和 skip selector。
4. 分配 char births。
5. 实现 revealed char。
6. 加阈值和 reduced motion。

验收：

- 普通段落有柔和逐字 fade。
- code/table/katex/svg 不被拆。
- 长段落不会制造过量 span。

### Step 7：light/full enhancement

目标：streaming 不卡，最终功能完整。

任务：

1. 拆 `enhanceMarkdown`。
2. streaming 使用 light。
3. final 使用 full。
4. block cleanup 与 enhancer state 挂到 block entry。

验收：

- streaming 代码块不明显卡。
- final 后代码高亮、复制按钮、图表恢复。
- 旧 cleanup 不泄漏。

### Step 8：final settle

目标：结束时不打断尾部动画。

任务：

1. streaming=false 后延迟关闭 animated path。
2. 追平 displayed source。
3. 延迟 full enhancement。
4. 最终发一次 rendered。

验收：

- 最后一批字符 fade 完成。
- 不出现 final render 闪烁。

### Step 9：profiler

目标：可测量。

任务：

1. 记录 smoothing metrics。
2. 记录 block metrics。
3. 记录 enhancement metrics。
4. 暴露 debug snapshot。

验收：

- 能看到 backlog、slow frame、block update 数。
- 能确认旧 block 没有反复重渲染。

## 18) 验收指标

### 18.1 性能

- 普通 Markdown 桌面端接近 60 FPS。
- 移动端不低于 30 FPS。
- streaming frame JS work p95 小于 8ms。
- long frame interval 大于 50ms 的比例低于 1%。
- block lex p95 小于 8ms。
- 单次 block render p95 小于 8ms。
- streaming 结束后 1 秒内 displayed 追平 target。

### 18.2 行为

- append-only 内容平滑显示。
- 非 append-only 内容立即同步。
- 用户上滑后不强制贴底。
- 用户在底部时保持贴底。
- source 清空能正确清空 DOM。
- theme/format 切换不复用旧 streaming state。

### 18.3 正确性

- 最终 Markdown 与 full render 一致。
- code fence 半截输入不污染后续 block。
- inline code copy 可用。
- code block copy 可用。
- Shiki 高亮可用。
- KaTeX 可用。
- Mermaid / Graphviz / Infographic 可用。
- artifact preview 不回归。

## 19) 测试计划

### 19.1 单元测试

新增或覆盖：

- smoothing append-only。
- smoothing non-append reset。
- large append frame release。
- empty-buffer resume warmup。
- settle drain。
- block split。
- incomplete fence repair。
- block queue state transitions。
- char animation skip tags。
- char birth reuse。

### 19.2 手工样例

准备长 Markdown，包含：

- 多级标题。
- 中英文长段落。
- emoji。
- 列表。
- blockquote。
- code fence。
- 未闭合再闭合的 code fence。
- table。
- inline code。
- KaTeX。
- Mermaid。
- artifact fence。

观察：

- 是否闪烁。
- 是否卡滚动。
- 是否卡输入。
- 旧段落是否重复动画。
- 代码块是否最终高亮。
- 图表是否最终渲染。

### 19.3 构建命令

Markdown renderer：

```sh
cd web/markdown-renderer
pnpm run build-console
```

Console：

```sh
cd web/console
pnpm build
```

Go：

```sh
go test ./...
```

## 20) 风险

### 20.1 DOM span 数量过多

字符动画会增加 DOM 数。

处理：

- block 字符数阈值。
- frame 新增字符数阈值。
- heavy block 跳过。
- reduced motion 跳过。

### 20.2 repair 引入依赖

如果使用 `remend`，需要检查包体积和 license。

处理：

- 先在 markdown renderer 包内评估 bundle diff。
- license 合规后更新 credits。
- 如不合适，改用最小 repair。

### 20.3 final enhancement 闪烁

最终完整增强可能替换 DOM。

处理：

- 延迟 full enhancement。
- block 级增强优先，不整篇替换。
- 必要时只对 heavy block full enhance。

### 20.4 当前 code highlight cache 以 index 为 key

block 增量后 index 不够稳。

处理：

- code highlight state 移到 block entry。
- key 使用 block start offset + hash。

### 20.5 artifact segment 变化

artifact fence 从半截变完整时，`ChatRichContent` 的 segment 结构会变化。

处理：

- segment key 变化时销毁旧 renderer。
- MarkdownRenderer 不接管 artifact。

## 21) 建议结论

完整版本应该集中改 `web/markdown-renderer`，Console 只传 streaming 状态。

执行顺序不能从字符动画开始。正确顺序是：

1. 状态模型。
2. 动态 smoothing。
3. repair + block split。
4. block DOM cache。
5. block queue。
6. 字符动画。
7. light/full enhancement。
8. final settle。
9. profiler。

这样能做到细腻的流式观感，同时保留 MisterMorph 现有的 Shiki、KaTeX、图表和 artifact preview 能力。

## 22) 实施状态

本分支已按上述顺序实现第一版完整链路：

- `web/markdown-renderer` 新增独立 streaming core，包含动态 smoothing、stream repair、block queue、block 分类、动画门控。
- `MarkdownRenderer` 新增 streaming 路径，保留原完整渲染路径。
- streaming block 走完整 Markdown HTML 路径，inline KaTeX 和 raw HTML 在流式阶段可见。
- code block 同步生成最终外壳，Shiki 只异步更新内部 token；未闭合 diagram/math fence 先按代码显示。
- 普通文本 block 支持字符 fade；代码、表格、公式、图表、HTML 等 heavy block 跳过字符动画。
- 字符 birth time 支持未来延迟，避免同一帧插入的一批字符一起淡入。
- backlog 清空后的恢复阶段会先暖启动，避免上游突发内容把释放速度瞬间拉高。
- 已排队的 RAF 会读取最新 render token，避免新快照到达后旧帧推进状态却跳过 DOM 更新。
- Markdown 容器尺寸变化通过 `ResizeObserver` 回传，用户在底部时聊天列表会继续贴底。
- streaming/final 的 block 间距、代码块外壳和闭合 fence 换行已对齐，不再需要高度地板。
- streaming DOM 高度只来自真实内容，不再额外插入尾部预留高度；同时关闭 streaming grid 行拉伸。
- Console 只传递 `streaming`、`streamMode`、`streamProfiler`，不接管 Markdown 逻辑。
- profiler 可通过 `localStorage.mistermorph_markdown_stream_profiler = "true"` 打开，采样写入 `window.__MISTER_MORPH_MARKDOWN_STREAM_PROFILER__`，包含 render/frame 历史、backlog、slow frame、slow render、queue length、内部释放缓冲 `reserveChars`、resume 和 reveal size。这里的 `reserveChars` 只影响吐字节奏，不参与 DOM 高度。

已验证：

- `pnpm test`
- `pnpm run build-console`
- `pnpm build`
- `go test ./...`
