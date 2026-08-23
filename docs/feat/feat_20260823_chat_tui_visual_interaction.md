---
date: 2026-08-23
title: Chat TUI 视觉与交互优化
status: implemented
---

# Chat TUI 视觉与交互优化

## 1. 目标

优化 `mistermorph chat` 的视觉层级和交互反馈，让用户随时能回答四个问题：

1. 当前在和哪个 Agent、模型及 workspace 对话？
2. Agent 正在做什么，是否仍在运行？
3. 当前是否需要用户审批或补充输入？
4. 此时按 Enter，消息会开始新任务、转向当前任务，还是进入队列？

本次不把 Chat 改成图形界面，也不追求展示更多信息。目标是用稳定的位置和明确的状态，减少用户从滚动日志中推断运行情况的成本。

## 2. 当前实现

当前 Chat 已有多行输入、粘贴折叠、运行中转向、停止、审批、计划、工具调用、diff、模型切换和 context compact 等能力。主要问题不在功能数量，而在信息组织。

### 2.1 会话历史与实时状态混在一起

`chatModel.View()` 只渲染输入区。用户消息、Agent 回复、工具事件、计划和错误都通过 `tea.Println` 写入终端原生 scrollback。这种实现简单，也保留了终端搜索和复制能力，但有几个直接后果：

- `used bash`、计划步骤和运行提示都是一次性文本，不能在原位置更新；
- 当前步骤会被后续输出推走；
- 用户无法快速区分长期结果和短期运行状态；
- spinner 只说明“还在运行”，没有说明当前动作和进度。

### 2.2 启动信息很快失效

非 compact 模式先打印大幅 ASCII banner，再打印 model、workspace 和 file state 目录。这些信息随后进入 scrollback，不再持续可见。大 banner 占用首屏，但不能帮助用户判断后续状态。

### 2.3 输入行为缺少上下文提示

- 运行中仍可输入，但界面没有明确说明 Enter 会转向当前任务；
- idle 时按 `Ctrl+C` 会立即退出，容易误操作；
- 多行输入中，Up/Down 总是切换历史，而不是先移动光标；
- Tab 只在硬编码命令列表中依次补全，没有候选说明；
- 硬编码列表已经漏掉 `/ctx` 和 `/think`，与实际命令 registry 不一致。

### 2.4 审批不是一个明确的交互状态

审批内容先作为普通文本打印，输入框仍保持普通 Chat 的形态。用户输入其他内容后才收到“请先审批”的提示。审批实际上会阻塞任务，界面也应暂时进入审批状态。

### 2.5 视觉语义不稳定

当前 spinner 使用固定 24-bit RGB 动画，成功、运行、警告和错误没有一套共同规则。在浅色终端、低色彩终端和不同主题中，颜色可能失真。信息主要靠散落的文字和符号区分。

## 3. 同类产品的做法

以下对照只采用各产品的官方文档或官方仓库，观察的是交互结构，不照搬其全部功能。

| 产品 | 有价值的做法 | 不直接照搬的部分 |
| --- | --- | --- |
| Claude Code | 输入区之外有持续状态；`/` 打开命令选择；多行输入先移动光标、到边界后才浏览历史；运行、审批、任务列表和 transcript detail 有明确入口 | 全屏 transcript viewer、后台 Agent 和复杂 permission modes 超出本次范围 |
| OpenAI Codex | 底部区域同时承担 composer、运行提示、context status 和临时选择界面；审批或选择器出现时临时替换 composer，但保留草稿；footer 根据宽度和状态改变内容 | 可配置 status line、线程分支和大量模式不应进入第一版 |
| Gemini CLI | 弹层和建议可以用 Esc 关闭；运行中断、退出、外部编辑器和工具详情都有明确快捷键；长输出仍可使用原生 scrollback | 主题系统、Vim 模式和 debug console 不是当前主要问题 |
| OpenCode | `/` 是命令入口，session、tool details、thinking、undo 等能力通过同一命令体系发现 | session 分享、文件级 undo/redo 需要额外的数据和恢复语义 |
| Crush / Aider | command palette、外部编辑器、session 和 compact 等高频能力有短路径；终端失焦时可通知 | 通知和完整 session 管理可等实际需求出现后再做 |

参考资料：

- [Claude Code interactive mode](https://code.claude.com/docs/en/interactive-mode)
- [Claude Code status line](https://code.claude.com/docs/en/statusline)
- [OpenAI Codex bottom pane](https://github.com/openai/codex/blob/main/codex-rs/tui/src/bottom_pane/mod.rs)
- [OpenAI Codex footer](https://github.com/openai/codex/blob/main/codex-rs/tui/src/bottom_pane/footer.rs)
- [OpenAI Codex TUI tooltips](https://github.com/openai/codex/blob/main/codex-rs/tui/tooltips.txt)
- [Gemini CLI keyboard shortcuts](https://google-gemini.github.io/gemini-cli/docs/cli/keyboard-shortcuts.html)
- [Gemini CLI commands](https://google-gemini.github.io/gemini-cli/docs/cli/commands.html)
- [OpenCode TUI](https://dev.opencode.ai/docs/tui/)
- [Crush](https://github.com/charmbracelet/crush)
- [Aider commands](https://aider.chat/docs/usage/commands.html)

共同规律不是“做成全屏 TUI”，而是把内容分成三类：

1. 会话结果进入可滚动的 transcript；
2. 当前运行状态在固定区域原地更新；
3. 当前可执行操作紧邻输入区显示，并随状态变化。

## 4. 第一性原理

### 4.1 每类信息只有一个位置

- 用户输入、Agent 最终回复、错误结果和重要 diff 属于 transcript；
- 当前工具、当前计划步骤、耗时和等待状态属于 activity；
- model、workspace 和 context 使用量属于 session status；
- 当前按键能做什么属于 contextual hints。

短期状态不能不断写入长期历史。长期结果也不能只存在于会消失的 spinner 中。

### 4.2 状态优先于装饰

颜色只作辅助，文本必须能独立表达 `Running`、`Approval`、`Stopped` 和 `Failed`。不使用大面积背景、层层边框或持续闪烁。界面只保留一个强调色，以及 success、warning、error 三种语义色。

### 4.3 输入含义必须可见

同一个 Enter 在不同状态下含义不同，就必须在输入区附近写明。不能要求用户记住隐藏规则。

### 4.4 优先保留终端原生能力

原生 scrollback 已经提供滚动、搜索、复制、tmux copy mode 和远程 SSH 兼容性。第一版不接管整个 viewport，也不维护第二份 transcript 布局。只有当原生 scrollback 确实无法支持折叠、定位或恢复时，才评估独立 transcript viewer。

## 5. 目标结构

Chat 由两个区域组成：

```text
terminal native scrollback
┌──────────────────────────────────────────────────────────┐
│ 用户消息、Agent 回复、已完成的工具摘要、错误和 diff      │
└──────────────────────────────────────────────────────────┘

Bubble Tea fixed bottom surface
┌──────────────────────────────────────────────────────────┐
│ activity：当前动作、计划进度、耗时或审批                 │
│ composer：输入草稿或临时交互界面                         │
│ footer：当前状态下最有用的按键提示或 session status      │
└──────────────────────────────────────────────────────────┘
```

底部区域不是通用窗口系统。它只需要四种主状态：

```text
idle ──submit──> running ──done──> idle
                    │
                    ├──approval──> awaiting_approval
                    │                  │
                    │            approve / deny
                    │                  │
                    └<─────────────────┘

任一状态都可产生 error；显示错误后回到可输入的 idle。
```

命令选择是临时视图，不增加新的任务状态。审批是明确的 `awaiting_approval` 状态。两者暂时占用 composer 的位置，关闭后恢复原草稿。

## 6. 视觉方案

### 6.1 启动

默认不再显示大幅 ASCII banner。启动只打印一行身份，再进入 composer：

```text
MisterMorph · gpt-5.2 · project-name

› _
  Enter send · Alt+Enter newline · / commands
```

完整路径、file state 目录、版本和完整模型名由新增的 `/status` 查看，不占首屏。compact 模式可进一步省略启动身份行。

### 6.2 Idle

```text
› Explain why this test is flaky_
  gpt-5.2 · project-name · ctx 18%              / commands
```

- `›` 是输入焦点，不使用 `username>` 这类较长 prompt；
- footer 空闲时显示 model、workspace basename 和 context 百分比；
- context 数据不可用时直接省略，不显示占位符；
- 有草稿时仍保持同一布局，不上下跳动。

### 6.3 Running

```text
● Running · bash · {"cmd":"go test ./...","timeout":120} · 00:13
› Add a regression test for Windows_
  Enter steer · Ctrl+C stop
```

- activity 固定在 composer 上方并原地更新；
- 优先显示真实的当前工具或计划步骤，不显示泛化的 `assistant is thinking...`；
- 没有具体 activity 时才显示 `Running · waiting for model`；
- 用户输入草稿不因 activity 更新而丢失；
- Enter 的提示明确写成 `steer`，避免用户误以为会开始独立任务。

工具完成后只向 scrollback 写一行稳定摘要：

```text
✓ bash · {"cmd":"go test ./...","timeout":120}
```

运行中的 activity 保持单行，宽度不足时只裁剪这一行的显示。工具结束后写入 scrollback 的记录包含完整的 `tool_params` 紧凑 JSON，不挑选 `cmd`、`path` 或 `query` 等少数字段。失败记录同时保留完整参数和错误内容。计划首次生成时向 scrollback 打印一次完整列表，activity 随后只显示当前步骤和 `i/n`，不重复打印全部步骤。

### 6.4 Approval

审批临时替换 composer，原草稿保留：

```text
! Approval · bash
  Writes to a remote repository
  cmd
    $ git push origin master
  timeout
    30

  y approve    n deny
```

- 信息顺序与 Web UI 一致：工具、reasons、全部具名参数、操作；
- `cmd` 排在第一个，其余参数按名称排序；非字符串值使用完整 JSON 显示；
- reasons 直接作为正文逐行显示，不再加冗余 label；
- 参数默认全部显示，不再设置会隐藏内容的 details 状态；
- `y`、`n` 可立即操作；
- 不接受普通 Chat 输入，避免先输入再报错；
- 结果进入 scrollback，并带上审批对象：`Approved · bash · git push …` 或 `Denied · bash · git push …`。

### 6.5 Command picker

在行首输入 `/` 后，composer 上方显示匹配命令：

```text
› /wo_

› /workspace          show or change workspace
  /workspace attach   attach a workspace
  /workspace detach   detach current workspace

  ↑↓ select · Tab complete · Enter run · Esc close
```

命令名称、可用性和说明必须来自同一个 command registry。不能继续维护一份只供 TUI 使用的硬编码命令列表。排序先按前缀匹配，再按注册顺序或使用频率；第一版不需要模糊搜索算法。

### 6.6 Error 和 stop

错误用短摘要进入 scrollback，底部恢复输入能力：

```text
× Model overloaded · retry later
```

服务端原始 JSON、stack 或长错误另起缩进行显示，不和摘要挤在同一行。第一版不为此增加可折叠 transcript。停止使用中性结果：

```text
■ Stopped by user · 12.8s
```

### 6.7 宽度适配

footer 不换行。空间不足时按以下顺序删除：

1. context 百分比；
2. workspace；
3. model 的 provider 前缀；
4. 次要快捷键提示。

最窄时只保留状态和一个主要动作：

```text
● Running · 00:13
› _
  Ctrl+C stop
```

activity 在可用宽度内裁成一行，不能把 composer 挤出屏幕。终端高度不足时，审批标题和操作固定显示，参数正文使用 Up/Down 逐行滚动，不丢弃参数。

### 6.8 排版规则

TUI 不能控制用户的终端字体和字号，因此排版只使用字符列、行、缩进、字重和颜色表达层级，不模拟 Web 卡片。

#### 水平对齐

底部区域使用两列：2 列宽的 marker，以及从第 3 列开始的正文。activity、composer、footer 和临时视图都遵守同一条左对齐线。

```text
● Running · bash · go test ./... · 00:13
› Add a regression test_
  Enter steer · Ctrl+C stop
  ↑正文统一从第 3 列开始
```

- marker 固定使用一个字符和一个空格：`● `、`› `、`! `、`× `、`✓ `、`■ `；
- 没有 marker 的次要信息仍保留两个空格，不能顶到第 1 列；
- 不使用 Tab 对齐，只使用空格；
- 计算宽度必须使用终端显示宽度，不能使用 UTF-8 字节数或 rune 数。中文、日文和全角字符通常占两列；
- 右侧的耗时、`i/n` 或 context 只有在左右内容之间至少能保留 3 个空格时才右对齐，否则按宽度适配规则省略；
- picker 的描述从当前最长命令后的第 2 个空格开始；终端不足 60 列时，描述移到下一行并与正文线对齐。

#### 垂直节奏

- transcript 中相邻的两个 turn 之间保留 1 个空行；
- 同一个 turn 内，工具摘要、计划摘要和 Agent 正文之间不额外插入空行；
- transcript 与底部区域之间保留 1 个空行，不能使用整行边框代替；
- idle 底部区域固定为 composer 和 footer 两行；
- running 增加 1 行 activity，activity 始终位于 composer 上方；
- composer 高度继续为 1 至 5 行，输入增长只向上扩展，footer 始终处于最底行；
- command picker 最多显示 6 个候选，更多候选在列表内部滚动；
- approval 展示全部参数，不以固定行数省略审批依据；
- 终端总高度不足 12 行时，composer 上限降为 3 行，picker 最多显示 3 个候选。

#### 文字层级

| 层级 | 内容 | 样式 |
| --- | --- | --- |
| Primary | 用户草稿、Agent 正文、审批命令 | 终端默认前景色，不加粗 |
| Active | `Running`、当前 picker 选项、需要立即处理的审批 | 强调色；文字或 marker 加粗，不使用整行反色背景 |
| Secondary | footer、工具名、耗时、context、命令说明 | dim |
| Success | 已完成的工具或计划 | `✓` 加 success 色，正文保持默认色 |
| Warning | 审批和可恢复警告 | `!` 加 warning 色，同时保留明确文字 |
| Error | 失败结果 | `×` 加 error 色，错误摘要保持默认色 |

不使用斜体，因为部分终端会把它显示成普通文字或错误字形。不使用 underline 表达选中状态；picker 使用 `›` marker。颜色不铺满整行，也不为 composer、activity 或 approval 增加方框。

#### 换行与截断

- 用户输入和 Agent 正文允许自然换行；续行与第 3 列正文线对齐；
- activity 和 footer 必须保持单行；工具完成记录保留全部参数，由终端自然换行；
- 只对临时单行状态使用省略；写入 scrollback 的参数不省略；
- 错误详情、审批参数和 diff 可以多行显示，续行缩进两个空格；
- markdown code/pre 保留原有渲染，不再额外套一层边框；
- 清除或替换内容时必须覆盖旧行尾，不能留下上一次较长状态的残字符。

排版效果应依靠对齐和留白形成层级，而不是依靠 box、背景块或更多颜色。

### 6.9 完整排版样例

以下样例展示整个终端，而不是单个组件。代码块中的空行也是排版的一部分。颜色无法在 Markdown 中表达，以 marker 和文字为准。

#### 80 列：运行中

```text
MisterMorph · gpt-5.2 · mistermorph

› 检查 Chat 输入历史为什么在多行时跳转错误

我先检查 textarea 的按键处理和现有测试。
✓ read_file · cmd/mistermorph/chatcmd/bubble.go

● Running · bash · go test ./cmd/mistermorph/chatcmd · 00:13        2/4
› 同时覆盖包含中文的输入_
  Enter steer · Ctrl+C stop
```

这里的视觉顺序是：已经发生的内容留在上方；唯一的动态行是 `Running`；输入草稿紧随其后；最后一行只说明此刻可执行的操作。

#### 80 列：任务完成并回到 idle

```text
✓ bash · go test ./cmd/mistermorph/chatcmd · 4.2s

问题来自 Up/Down 在 textarea 处理光标之前截获按键。现在只有光标已经
位于首行或末行边界时，才会进入历史记录。

› _
  gpt-5.2 · mistermorph · ctx 18%                       / commands
```

Agent 正文允许自然换行。footer 保持一行，并使用 dim 样式，因此阅读顺序先落在结果，再落在输入框，最后才是 session 信息。

#### 80 列：等待审批

```text
我已经完成本地检查。下一步需要推送分支。

! Approval · bash
  Writes to a remote repository
  cmd
    $ git push origin fix/chat-history
  timeout
    30

  y approve    n deny
```

此时 composer 和 idle footer 都不显示。审批内容直接占据底部交互区域，防止用户误以为可以继续发送普通消息。审批完成后恢复原草稿。

#### 80 列：选择命令

```text
› /wo_

› /workspace          show or change workspace
  /workspace attach   attach a workspace
  /workspace detach   detach current workspace

  ↑↓ select · Tab complete · Enter run · Esc close
```

输入和选中项都使用 `›`，但位于不同的行。未选中项和说明文字从第 3 列开始。选择器不使用边框或整行背景。

#### 40 列：运行中

```text
● Running · bash · 00:13
› 同时覆盖中文输入_
  Ctrl+C stop
```

窄终端优先保留状态、当前工具、输入和停止入口。命令详情、计划总数、workspace、context 和次要快捷键依次省略，不允许 footer 自动折成两行。

#### 多行 composer

```text
› 请检查这两个条件：
  1. 光标在首行时才读取历史
  2. 光标在末行时才读取下一条_
  Enter send · Alt+Enter newline
```

续行从第 3 列开始，与首行正文对齐。输入增长到 5 行后在 composer 内滚动，不能继续向上挤压 transcript。

### 6.10 tmux 和 GNU Screen

本方案可以使用 tmux 和 GNU Screen 的 scrollback，但必须保留 inline rendering。Bubble Tea 1.3.10 的 `tea.Println` 会把输出持久写在程序区域上方；进入 alternate screen 后，这类输出不会写入原生 history。因此实现需要遵守以下约束：

- `tea.NewProgram` 不能增加 `tea.WithAltScreen()`，运行中也不能发送 `tea.EnterAltScreen`；
- transcript 只通过 Bubble Tea message queue 和 `tea.Println` 输出，程序启动后不能从其他 goroutine 直接向 stdout 写入；
- `View()` 继续只拥有底部区域，不能把已经进入 scrollback 的 transcript 再次放进 `View()`；
- activity 使用静态 marker。耗时最多每秒刷新一次，不能继续使用每秒 12 帧的 spinner；
- 每一行底部内容都根据 `tea.WindowSizeMsg` 中的 pane 宽度裁剪，并在最右侧保留 1 个空列，避免边界自动换行破坏 cursor 位置；
- transcript 不预先按当前窗口宽度插入硬换行，由 tmux 或 Screen 保存和显示自然换行；窗口 resize 后的历史 reflow 结果由 multiplexer 决定；
- 不启用 Bubble Tea mouse mode。鼠标滚轮是否进入 copy mode 由 tmux、Screen 和终端配置决定；键盘 copy mode 始终可用；
- 用户进入 copy mode 后，Agent 仍可继续运行并产生输出。应用不尝试检测或退出 copy mode；离开 copy mode 后显示最新底部状态。

默认操作为：

| 环境 | 进入 scrollback | 容量限制 |
| --- | --- | --- |
| tmux | `Ctrl+B`，再按 `[` | 由 `history-limit` 控制 |
| GNU Screen | `Ctrl+A`，再按 `[` | `scrollback` 默认只有 100 行 |

长会话中，用户可以自行提高 multiplexer 的 history：

```text
# tmux.conf
set -g history-limit 10000

# screenrc
defscrollback 10000
```

这只是 multiplexer 的保存容量，不是 MisterMorph 配置。应用不能绕过用户设置的 history 上限，也不应为了补偿该上限而新增一份 TUI transcript 缓存。

参考资料：

- [Bubble Tea 1.3.10 standard renderer](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/standard_renderer.go)
- [tmux copy mode](https://github.com/tmux/tmux/wiki/Getting-Started#copy-and-paste)
- [GNU Screen copy and scrollback](https://www.gnu.org/software/screen/manual/html_node/Copy.html)

## 7. 交互规则

### 7.1 输入与退出

| 状态 | 按键 | 行为 |
| --- | --- | --- |
| idle | Enter | 发送新任务 |
| running | Enter | 将当前输入作为 steer 发送 |
| 任意 composer | Alt+Enter 或 Ctrl+J | 插入换行 |
| 多行输入 | Up/Down | 先移动光标；到首行或末行边界后才浏览历史 |
| 有草稿 | Ctrl+C | 清空草稿，不退出 |
| 空草稿且 idle | Ctrl+C | 第一次提示再次按下退出；两秒内第二次退出 |
| 空草稿且 idle | Ctrl+D | 退出 |
| running | Ctrl+C | 停止当前任务，不退出 Chat |
| `/init` 或 `/update` 运行中 | Ctrl+C | 直接中止当前命令，不等待 slash command 返回 |
| awaiting approval | Up/Down | 滚动审批原因和完整参数，标题与操作保持可见 |
| awaiting approval | y / n | 批准或拒绝；第一次决定提交后忽略重复按键 |
| awaiting approval | Ctrl+C | 拒绝审批，不退出 Chat |
| picker | Esc | 关闭 picker，保留草稿 |

这组规则避免一次误按退出，同时保留终端用户熟悉的中断和 EOF 语义。

### 7.2 历史与粘贴

- 保留现有多行粘贴折叠，placeholder 使用 dim style，与普通输入区分；
- 提交后的 scrollback 内容保持现状，本次不改变粘贴内容的保存和显示语义；
- 保留现有持久输入历史，不同时修改存储位置和作用域；
- 输入历史的存储位置和作用域属于数据策略，不与视觉改造绑定。

### 7.3 工具输出

默认 transcript 只打印工具完成摘要，失败则显示必要的参数和错误。diff 保留现有渲染。第一版不增加 compact / verbose 模式，也不实现可折叠历史节点。原生 scrollback 中已经打印的文本无法可靠折叠，强行模拟只会引入第二套 transcript 状态。

## 8. 最小实现边界

### 8.1 保留的部分

- 继续使用 Bubble Tea、Bubbles textarea 和 Lip Gloss；
- 继续使用 `tea.Println` 保存 transcript；
- 保留现有 REPL、agent 回调、审批恢复和 steer 通道；
- 保留 1 至 5 行 composer 和粘贴 placeholder；
- 不改变 Chat 后端协议。

### 8.2 需要调整的部分

1. `chatModel` 保存明确的 `idle`、`running` 和 `awaiting_approval` 状态，以及当前 activity；
2. `View()` 固定渲染 activity、composer 或临时视图、footer；
3. 工具和计划回调更新 activity，工具结束后打印包含完整参数的稳定记录；
4. 审批请求进入 `chatModel`，不再只打印文本后等待普通输入；
5. command registry 成为命令名称和说明的唯一来源，TUI 从 registry 读取候选；
6. 统一少量自适应 style：accent、muted、success、warning、error，不新增主题配置；
7. 根据终端宽度裁剪 activity 和 footer，禁止它们换行导致布局抖动；
8. 小终端中的审批只滚动参数正文，固定保留标题和批准、拒绝操作。

不需要建立通用 widget framework、event bus 或主题插件系统。现有消息类型可以扩充为结构化的 activity、approval 和 result 消息；只有被两个以上真实界面重复使用的渲染逻辑才抽成组件。

## 9. 实施阶段

### Phase 1：稳定底部区域

- 去掉默认大 banner，增加单行启动身份；
- 增加 `/status`，承接不再常驻显示的完整 session 信息；
- 增加 idle/running footer 和自适应宽度；
- 显示当前工具、计划步骤、耗时和 `i/n`；
- 明确 running 时 Enter 是 steer；
- 修正 Ctrl+C、Ctrl+D、多行 Up/Down 和 Ctrl+J；
- 使用自适应的有限语义色，移除固定 RGB spinner。

这一阶段不改变 scrollback 数据结构，风险和改动范围最小。

### Phase 2：临时交互面

- 审批临时替换 composer，并保留草稿；
- 长审批正文使用 Up/Down 滚动，重复审批按键只提交第一次决定；
- `/` 打开 command picker；
- 候选来自 command registry，删除硬编码 autocomplete；

### Phase 3：输出降噪

- 工具开始只更新 activity，不立即写 scrollback；
- 工具完成写一条包含完整参数的稳定记录，失败同时保留错误详情；
- 计划只在底部更新当前步骤，完成后打印一次总结；
- 为长错误、参数、粘贴和 diff 提供一致的摘要规则。

Phase 3 依赖回调事件能稳定区分 started、completed 和 failed。若现有事件不够，只补这些明确字段，不先设计通用 UI 事件协议。

## 10. 不做的内容

第一轮不实现：

- 全屏 alternate screen；
- 自管 transcript viewport；
- 鼠标交互；
- 自定义主题和自定义 status 脚本；
- `Ctrl+R` 历史搜索和外部编辑器；
- 工具详情模式和可折叠 transcript；
- session 列表、fork、undo/redo；
- 后台 Agent 面板；
- 可拖动布局、tab 或多 pane；
- 为每个工具单独设计卡片；
- 动画化进度条。

这些功能会显著扩大状态管理和终端兼容范围，但不能直接解决当前四个核心问题。

## 11. 验收标准

1. idle、running 和 awaiting approval 在不读取旧输出的情况下可以一眼区分。
2. running 时始终显示当前 activity；工具或步骤变化在原位置更新，不连续写入历史。
3. footer 明确说明 Enter 和 Ctrl+C 在当前状态下的行为。
4. Agent 运行时输入和发送 steer 不会丢失草稿。
5. 审批出现后，普通 Chat 输入暂停；审批对象、全部参数和 reasons 在底部可见，终端高度不足时可逐行浏览。
6. 输入 `/` 后可选择所有实际注册的命令；新增命令不需要再修改 TUI 硬编码列表。
7. 多行输入使用 Up/Down 时先移动光标，到边界后才切换历史。
8. 40、60、80 和 120 列终端中，activity 与 footer 都不换行，composer 不被挤出。
9. 浅色、深色和 16 色终端中，状态仍能靠文字和符号辨认。
10. 原生 scrollback、终端搜索、复制、tmux 和 SSH 行为保持可用。
11. activity、composer、footer、picker 和 approval 的正文都从第 3 列开始，包含 CJK 字符时仍保持对齐。
12. transcript 的 turn 间距、底部区域行数和窄终端候选数量符合第 6.8 节，不因状态更新留下残字符。
13. tmux 和 GNU Screen 的 copy mode 中可以浏览已输出的 transcript；运行中的状态更新不会把视图强制拉回底部。
14. tmux 或 GNU Screen resize、detach/attach 后，底部区域按新的 pane 尺寸完整重绘，不出现残行或重复 transcript。
15. 实现中没有 alternate screen、Bubble Tea mouse mode 或绕过 Bubble Tea renderer 的并发 stdout 写入。

## 12. 结论

最值得先做的不是重写整个 TUI，而是建立一个稳定的底部交互区域。它把实时 activity、composer、审批和 contextual footer 放在固定位置，同时继续让 transcript 使用终端原生 scrollback。

这能直接解决状态不清、输入含义不明、审批不突出和输出噪声四个问题。等这套结构运行稳定后，再根据真实使用情况决定是否需要 transcript viewer、session 管理或更复杂的主题能力。

## 13. 实施 Checklist

- [x] 完成现有 Chat TUI 检查、同类产品对照和方案设计。
- [x] Phase 1：稳定底部区域。
  - [x] 用测试固定 idle、running、窄终端和多行输入行为。
  - [x] 使用单行启动身份替换默认大 banner。
  - [x] 增加 `/status`。
  - [x] 增加 activity、composer 和 contextual footer。
  - [x] 修正 Enter、Ctrl+C、Ctrl+D、Ctrl+J 和 Up/Down 的状态语义。
  - [x] 移除 12 FPS 彩色 spinner，activity 最多每秒刷新一次。
- [x] Phase 2：临时交互界面。
  - [x] 用测试固定审批、草稿恢复和命令选择行为。
  - [x] 审批界面临时替换 composer，并显示审批对象、参数和 reasons。
  - [x] 固定审批标题和操作，小终端中允许滚动完整参数，并阻止重复决定。
  - [x] command picker 从 command registry 读取名称和说明。
  - [x] 删除硬编码 slash command autocomplete。
- [x] Phase 3：输出降噪。
  - [x] 用测试固定工具、计划和错误的 transcript 输出规则。
  - [x] 工具和计划开始事件只更新 activity。
  - [x] 工具结束后打印一次包含完整参数的稳定记录，失败同时保留错误。
  - [x] 完整计划只打印一次，后续步骤在 activity 原地更新。
- [ ] tmux copy mode、resize、detach/attach 人工检查通过。
- [x] GNU Screen copy mode、resize、detach/attach 人工检查通过。
- [x] `go test ./...` 通过。
- [x] `go vet ./...` 通过。
