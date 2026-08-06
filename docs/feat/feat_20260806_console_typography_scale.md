---
date: 2026-08-06
title: Console 字号层级收敛方案
status: implemented
---

# Console 字号层级收敛方案

## 1. 决定

Console 主 UI 只使用五个固定字号和一个响应式 Hero 字号：

| 语义 | CSS 变量 | 字号 | 默认用途 |
| --- | --- | --- | --- |
| 辅助信息 | `--font-size-meta` | `0.75rem`，通常为 12px | 时间、路径、hint、状态说明、次要 metadata |
| 默认 UI | `--font-size-ui` | `0.875rem`，通常为 14px | 按钮、输入框、下拉菜单、导航、列表标题、表单 label |
| 正文 | `--font-size-body` | `1rem`，通常为 16px | Chat、编辑器、长文本、主要内容 |
| 常规标题 | `--font-size-title` | `1.25rem`，通常为 20px | 页面栏、panel、card 和 section 标题 |
| 展示标题 | `--font-size-display` | `2rem`，通常为 32px | Setup、Repair 等独立页面的主标题 |
| Hero | `--font-size-hero` | `clamp(2.5rem, 2rem + 2vw, 4rem)`，通常为 40–64px | Stats、Runtime 的唯一主指标 |

前五个字号不随视口变化。同一语义在桌面端、移动端和不同页面中保持同一字号。

`clamp()` 只保留给 Hero。普通 UI 不使用连续变化的字号。

全局正文基准保留为 `1rem`。14px 是 UI 控件的默认字号，不是所有内容的全局字号。这样可以让界面紧凑，同时保留 Chat 和长文本的可读性。

## 2. 当前事实

Console 先加载 `quail-ui/dist/index.css`，再加载 `styles/base.css`。Quail UI 当前设置：

- `body`：16px；
- `QButton` 默认和 `sm`：14px；
- `QInput` 默认：15px；
- `QInput sm`：14px；
- `QInput xs`：12px；
- Toast：14px；
- Tooltip：13px。

MisterMorph 自有 CSS 当前有 216 个 `font-size` 声明：

- 190 个固定字号；
- 15 个 `clamp()`，包含 13 种公式；
- 11 个 `inherit`；
- JavaScript 中另有一个 14px 编辑器字号。

固定字号包含 7、8、10、11、12、13、13.32、13.76、14、15、16、18、18.4 和 19px，共 14 个实际值。显式声明最多的是 12px；视觉上常见的是 14px，因为按钮和导航会重复出现。

当前问题不是某一个字号不合适，而是没有稳定的语义映射：

1. 10、11、12、13、14 和 15px 同时承担辅助信息、正文、label 和标题。
2. 13.32px、13.76px、18.4px 等单点值没有形成可复用层级。
3. 普通文件名、空态标题、列表标题和统计值也使用 `clamp()`。
4. 相同信息层级在不同页面使用不同字号。
5. 组件为了适配局部空间而缩小文字，掩盖了布局问题。

## 3. 目标

1. 用户可以稳定判断标题、正文、控件和辅助信息的层级。
2. 默认 UI 以 14px 为主，长文本以 16px 为主。
3. 不再通过增加 1px 或增加一条 `clamp()` 解决局部视觉问题。
4. 同一组件在不同页面使用相同字号。
5. 移动端通过换行、截断和布局变化适配，不连续缩放普通文字。
6. 保留浏览器缩放和用户根字号设置的能力。

## 4. 边界

本方案只处理 Console 主 UI，包括：

- app shell 和导航；
- 页面栏和 workspace sidebar；
- 表单、按钮、菜单和对话框，包括 MisterMorph 直接使用的原生 HTML 控件；
- Chat composer 与 Chat 消息外层 UI；
- Settings、Todo、Audit、Memory、Stats、Runtime 等页面自有样式。

以下内容不属于本方案：

1. Markdown 文档内部的 `h1`–`h6`、代码块和引用层级。
2. KaTeX、Mermaid、语法高亮和其他 vendor 样式。
3. 字体家族、颜色、间距和圆角的整体重做。
4. Quail UI 全组件库的字体系统重写。
5. 用户可配置字体大小功能。
6. 非 `font-size` 属性中的 `clamp()`，例如页面 gutter 和 card margin。
7. 编辑器通过 JavaScript 参数维护的现有字号。

本方案不新增组件、不增加包装层、不引入新的 CSS 框架。

## 5. 语义规则

### 5.1 辅助信息：12px

12px 只用于短小、次要、非正文的信息：

- 时间和路径；
- API、任务和连接状态的补充说明；
- 输入框 hint；
- badge 和 compact metadata；
- divider label；
- card marker 和其他仍在显示的微型标记；
- 空态的短小补充说明。

关键错误、操作说明和需要连续阅读的内容不能使用 12px。短错误和成功状态使用 14px；多行错误和诊断正文使用 16px。

不再把 7px、8px、10px 和 11px 作为文字层级。已经确认会渲染的可见文字先迁移到 12px，再检查布局。未被 Console 渲染的 Quail UI 样式不在迁移范围内。

### 5.2 默认 UI：14px

14px 是 Console 出现频率最高的交互字号：

- 主导航和移动端导航；
- 按钮、tab 和菜单项；
- 输入框、textarea 和下拉选择器；
- 表单 label；
- workspace sidebar item title；
- 普通列表 item title；
- compact dialog 内容；
- 短错误、成功状态和操作结果；
- 单行日志和表格内容。

同一控件不能因为所在页面不同而改成 13px 或 15px。

MisterMorph 直接使用的原生 `button`、`input`、`textarea`、`select` 和 `label` 与对应的 Quail UI 控件遵循同一规则。原生元素不是单独的字号层级。

### 5.3 正文：16px

16px 用于需要连续阅读或输入的内容：

- Chat 消息正文；
- Markdown 编辑器正文；
- 多行只读文本；
- 多行错误、诊断信息和日志正文；
- 较长的说明和空态正文；
- 需要明显高于 UI chrome 的主要值。

不能为了让长文本塞进固定容器而降到 14px。应先处理容器宽度、换行或截断。

### 5.4 常规标题：20px

20px 用于页面栏、panel、card 和 section 的标题。标题之间通过字重、颜色和位置区分，不再增加 18px、19px、21px 或 22px 层级。

小对话框标题和紧凑空态标题使用 16px。页面级空态标题使用 20px。它们不需要增加新的中间字号。

### 5.5 展示标题：32px

32px 只用于拥有独立页面结构的主标题，例如 Setup 和 Repair。普通设置 panel、空态和列表标题不能使用该字号。

### 5.6 Hero：40–64px

Hero 只用于 Stats 或 Runtime 页面中唯一的主指标。一个视图最多有一个 Hero 层级。

其他统计值使用 16px 或 20px。不能为每种统计卡片创建独立的响应式字号。

### 5.7 跨组件规则

以下内容按信息角色选择字号，不因实现组件或字体家族不同而改变：

| 内容 | 字号 |
| --- | --- |
| divider label、短 metadata、仍在显示的微型标记 | 12px |
| tab、菜单项、表单控件、短错误和成功状态 | 14px |
| dialog 标题、紧凑空态标题 | 16px |
| 多行错误、诊断信息、日志正文和长说明 | 16px |
| 页面级空态标题 | 20px |

等宽字体只表示内容类型，不形成新的字号层级。路径、ID、checksum 和单行代码 metadata 可以使用 12px；表格中的代码值使用 14px；需要连续阅读的代码、日志和诊断正文使用 16px。

`font: inherit` 和 `font-size: inherit` 可以保留，但元素最终计算出的字号必须符合其信息角色。不能以“继承”为由让原生控件意外使用 16px，或让正文意外缩小到 14px。

## 6. CSS 变量

字号变量放在 `web/console/src/styles/base.css` 的 `:root` 中：

```css
:root {
  --font-size-meta: 0.75rem;
  --font-size-ui: 0.875rem;
  --font-size-body: 1rem;
  --font-size-title: 1.25rem;
  --font-size-display: 2rem;
  --font-size-hero: clamp(2.5rem, 2rem + 2vw, 4rem);
}
```

变量按语义命名，不按数值命名。禁止使用 `--font-size-14` 之类的名称。

第一阶段只增加字号变量。line-height 保持少量固定规则，不为每个组件增加新的 line-height 变量：

| 语义 | 建议 line-height |
| --- | --- |
| 辅助信息 | 1.4 |
| 默认 UI | 1.4 |
| 正文 | 1.5 |
| 常规标题 | 1.2 |
| 展示标题 | 1.1 |
| Hero | 1 |

单行截断组件可以使用像素 line-height 与固定高度对齐，但不能因此创建新字号。

## 7. `clamp()` 使用规则

`font-size: clamp(...)` 必须同时满足以下条件：

1. 元素是 Stats 或 Runtime 的唯一 Hero。
2. 最小值和最大值来自 `--font-size-hero`。
3. 所有页面共用同一个变量。
4. 字号变化不是为了解决 overflow。

以下元素禁止使用响应式字号：

- button、input、textarea、select 和 menu item；
- 导航、tab、sidebar 和普通 list item；
- label、hint、metadata 和状态；
- Chat 正文和编辑器正文；
- 页面栏、panel、card 和 section 标题；
- 文件名、路径、空态标题和普通统计值。

组件和页面 CSS 中不再直接出现 `font-size: clamp(...)`。唯一公式定义在 `--font-size-hero` 中。

## 8. Quail UI 适配

不修改 `node_modules`，不复制 Quail UI 组件。

优先顺序如下：

1. Quail UI 现有字号已经匹配时直接使用，例如 14px 的 `QButton`。
2. 组件提供 size 或 CSS 变量时使用现有接口。
3. 组件默认值与 Console 层级冲突时，在 `base.css` 中集中适配。
4. 多个项目都需要同一能力时，再给 Quail UI 增加主题变量并升级依赖。

第一轮需要统一的组件值：

| 组件 | 当前值 | 目标值 |
| --- | --- | --- |
| `body` | 16px | `1rem` |
| `QButton` 默认、sm | 14px | 14px，保持 |
| `QInput` 默认 | 15px | 14px |
| `QInput sm` | 14px | 14px，保持 |
| `QTextarea` | 15px | 14px |
| 常规 dropdown action | 15px 或继承 | 14px |
| Toast | 14px | 14px，保持 |
| Tooltip | 13px | 12px |
| Badge | 11、12、13px | 12px，通过 padding 区分尺寸 |
| 常规 card title | 20–26.4px `clamp()` | 20px |

集中适配只处理字号，不改变组件高度、padding 或交互行为。

## 9. 现有值迁移

迁移不能只做数值替换，必须先判断元素语义：

| 现有值 | 默认迁移 |
| --- | --- |
| 7、8px | 已确认渲染的可见文字先改为 12px；未渲染的 Quail UI 样式不处理 |
| 10、11px | 已确认渲染的可见文字改为 12px |
| 12px | 12px；若内容是主要说明或正文则升到 14px 或 16px |
| 13、13.32、13.76、14、15px | 默认归到 14px |
| 16px | 正文保留 16px；UI 控件和列表标题降到 14px |
| 18、18.4、19px | 根据语义归到 16px 或 20px |
| 普通 UI 的 `clamp()` | 归到 14、16 或 20px |
| Hero 的 `clamp()` | 改用 `--font-size-hero` |

已确认在页面中渲染的 card marker、plate spec 和类似微型文字，第一轮直接升到 12px，检查换行、截断和容器高度。若 12px 放不下，应调整布局或删除非必要文字，不能继续保留更小字号。

Provider logo 当前 7px、8px 的文字 badge 不应成为正式字号层级。确认它在当前 Console 中实际渲染后，也先改为 12px。若 badge 文案在 12px 下放不进 logo，应改为图形标记、tooltip 或更大的展示区域，而不是继续缩小文字。

## 10. 实施顺序

### Phase 1：基础层

1. 在 `base.css` 增加六个字号变量。
2. 将 `body` 改为 `font-size: var(--font-size-body)`。
3. 集中适配 QButton、QInput、QTextarea、dropdown、tooltip、badge 和 card title。
4. 将 MisterMorph 直接使用的原生 button、input、textarea、select 和 label 纳入同一语义映射。
5. 统一 `AppNavList`、`AppDialogShell`、workspace sidebar 和共享标题样式。

这一阶段完成后，导航、表单、菜单和 sidebar 应先形成稳定基线。

### Phase 2：共享组件

按组件处理：

1. Chat composer 和 status card；
2. setup picker、provider picker 和 connection test dialog；
3. raw text、raw JSON、Codex OAuth 和 image dialog；
4. credits、kicker 和 sidebar controls。

每个元素按语义选择变量。禁止为了减少 diff 而按旧数值机械映射。

### Phase 3：页面样式

依次处理 Settings、Chat、Todo、Audit、Memory、Contacts、Logs、Overview、Setup、Repair、Stats 和 Runtime。

同一阶段删除已经失去作用的局部覆盖，避免共享样式与页面样式继续竞争。

### Phase 4：删除普通 UI 的响应式字号

1. 删除 views 和 components 中的 `font-size: clamp(...)`。
2. Stats 和 Runtime Hero 改用同一个 `--font-size-hero`。
3. 普通统计值改用 16px 或 20px。
4. 紧凑空态标题改用 16px，页面级空态标题改用 20px，不根据视口变化。

暂不增加自定义 lint 规则。先用代码搜索和 review 检查；只有该问题反复出现时才考虑 CI 检查。

## 11. 响应式与可访问性

移动端不缩小语义字号。空间不足时按以下顺序处理：

1. 允许换行；
2. 调整 grid 或 flex 布局；
3. 将次要内容移动到下一行；
4. 对路径、ID 和文件名使用明确的 ellipsis；
5. 最后才考虑隐藏非必要信息。

验证范围：

- 360px、768px、1280px 和 1440px viewport；
- 100%、125%、150% 和 200% 浏览器缩放；
- 中文、英文和日文；
- 长 provider 名、长模型名、长路径和长错误信息；
- 原生表单控件，以及挂载到页面根节点之外的 dialog、dropdown 和 menu；
- 当前 Console 实际渲染的 Quail UI 组件的最终计算字号。

12px 是主 UI 文字下限。纯装饰性图形、SVG 图标和不可见辅助元素不受该限制。

## 12. 验收

1. MisterMorph 自有 CSS 中，主 UI 的 `font-size` 只使用六个字号变量或 `inherit`；变量定义本身除外。
2. views 和 components 的 CSS 中没有新的裸 `font-size` 数值。
3. views 和 components 的 CSS 中没有 `font-size: clamp(...)`。
4. 当前 Console 实际渲染的主 UI 元素，其最终计算字号属于 12、14、16、20、32px 或 Hero 范围，并符合第 5 节的语义规则。
5. 该运行时检查包括 Quail UI 组件、MisterMorph 原生 HTML 控件、dialog、dropdown 和 menu；不要求修改 Quail UI 中未被 Console 渲染的样式。
6. `inherit` 元素的最终计算字号符合其信息角色，不能只检查声明值。
7. 主要按钮、输入框、下拉菜单、导航、tab 和 sidebar item title 都是 14px。
8. Chat、Markdown 编辑、多行错误、诊断信息和长文本保持 16px。
9. dialog 和紧凑空态标题是 16px；页面栏、panel、card、section 和页面级空态标题是 20px。
10. Stats 与 Runtime 最多各有一个 Hero，且共用同一字号变量。
11. 当前 Console 实际渲染的可见文字没有小于 12px 的字号。
12. 200% 缩放时没有文字重叠、不可达操作或水平页面滚动。
13. 中文、英文和日文下的主要页面不因字号统一而截断关键操作。
14. `pnpm test` 与 `pnpm build` 通过。

## 13. 风险

### 13.1 统一后局部空间不足

现有 10px、11px 文本升到 12px 后，部分路径和状态行可能换行。应修正布局或截断策略，不能重新添加更小字号。

### 13.2 层级看起来变少

这是预期结果。相邻信息通过字重、颜色、间距和位置区分，不通过 1px 的字号差异区分。

### 13.3 Quail UI 升级覆盖适配

Quail UI 升级后重新检查 Console 实际渲染的组件，而不是扫描并修改整个组件库。检查最终计算字号，确认 button、input、dropdown、tooltip、badge、card title 等仍符合语义规则。

### 13.4 Markdown 与主 UI 混淆

Markdown 内容拥有自己的文档层级，不应被主 UI token 全局覆盖。适配选择器必须限制在应用 chrome 和组件外层，不能影响 vendor renderer。

## 14. 非目标

- 不建立完整设计系统项目。
- 不新增 Typography Vue 组件。
- 不用 utility class 替换所有现有 class。
- 不调整字体家族。
- 不统一所有 line-height、font-weight 和 letter-spacing。
- 不修改 Markdown、KaTeX、Mermaid 或代码高亮的内部字号。
- 不处理编辑器通过 JavaScript 参数维护的现有字号。
- 不清理 Quail UI 中没有被 Console 渲染的字号声明。
- 不为单个页面保留专用字号层级。
