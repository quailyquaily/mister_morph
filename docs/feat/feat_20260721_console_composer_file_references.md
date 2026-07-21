---
date: 2026-07-21
title: Console Composer 文件引用
status: implemented
---

# Console Composer 文件引用

## 1) 背景

Console Composer 需要让用户上传文件，并在下一次 task 中使用这些文件。

把上传后的路径直接写进 textarea 会产生三个问题：

- 文件引用和用户指令混在一起
- 用户无法单独预览或移除文件
- runtime 只能从自然语言中猜哪些路径是本次 task 的输入

文件和文本承担不同职责：

- 文本是用户指令
- 文件是 task 的独立输入

因此，Composer 应分别保存文本和文件引用，并在提交 task 时一起发送。

## 2) 目标

首版只完成下面这些能力：

1. 用户可以从 Composer 的 `+` 菜单上传一个或多个文件。
2. 已上传文件显示为独立 item，不修改 textarea 内容。
3. 文件 item 可以预览，也可以从本次 task 中移除。
4. task 使用结构化文件引用，不要求 runtime 从文本中解析路径。
5. 有 attached workspace 时，文件上传到 `workspace_dir`；否则上传到 `file_cache_dir`。
6. 上传、预览、移除和提交在桌面端与移动端保持相同语义。

`+` 菜单中的两个入口固定为：

- `Upload Files`
- `Attach Workspace`

## 3) 非目标

首版不做这些事：

- 不设计通用附件平台
- 不增加附件 ID 或附件数据库
- 不增加上传会话
- 不做分片上传或断点续传
- 不做文件夹上传
- 不做拖拽上传
- 不把文件内容直接放进 task JSON
- 不删除 workspace 或 cache 中的真实文件
- 不保证刷新浏览器后恢复尚未提交的文件 item
- 不改变现有 task 文本不能为空的规则

## 4) 用户流程

### 4.1 上传

1. 用户点击 Composer 左侧的 `+`。
2. 用户选择 `Upload Files`。
3. 系统打开浏览器文件选择器，允许多选。
4. 每个文件在 Composer 中显示上传状态。
5. 上传成功后，文件 item 进入可提交状态。
6. textarea 中的原有文本保持不变。

上传目标由当前 Composer 状态决定：

- 当前已有 attached workspace：上传到该 `workspace_dir`
- 新 topic 已选择但尚未提交的 workspace：上传到该 `workspace_dir`
- 当前没有 attached workspace：上传到 `file_cache_dir`

### 4.2 预览

用户可以从文件 item 打开只读预览。

首版至少支持：

- 常见图片
- PDF
- UTF-8 文本

文本预览必须转义内容，不能把上传内容作为 HTML 执行。

不支持预览的文件类型应明确显示“无法预览”，不能尝试执行 HTML、JavaScript 或其他可执行内容。

同一条用户消息包含多个文件时，预览框应在内容左右两侧显示浮动的上一项和下一项按钮。按钮不能占用内容布局空间，用户不需要关闭预览框再打开另一个文件。

### 4.3 移除

用户从 Composer 移除文件 item 时：

- 只移除本次 task 的文件引用
- 不删除 `workspace_dir` 中的文件
- 不删除 `file_cache_dir` 中的文件

这里的操作语义是 `Remove from task`，不是 `Delete file`。

### 4.4 提交

提交 task 时，Console 同时发送：

- 用户输入的原始文本
- 当前处于可提交状态的文件引用

提交成功后清空文件 item。

对应的用户消息在右侧显示独立文件列表气泡。文件气泡只显示文件名，不显示大小；点击文件名可以打开只读预览。历史重新加载后，文件气泡通过 task 中保存的 `file_references` 恢复。

提交失败时保留文件 item，用户可以修改文本后重试。

## 5) 最小文件引用协议

文件引用只需要两个字段：

```json
{
  "dir_name": "workspace_dir",
  "path": "report.pdf"
}
```

字段规则：

- `dir_name` 只允许 `workspace_dir` 或 `file_cache_dir`
- `path` 是对应目录下的相对路径
- `path` 不能为空
- `path` 不能是绝对路径
- `path` 不能通过 `..` 逃出对应目录

首版不增加 `id`、`kind`、`url` 或绝对路径字段。

## 6) 上传响应

上传接口为每个成功文件返回：

```json
{
  "name": "report.pdf",
  "size_bytes": 1234,
  "dir_name": "workspace_dir",
  "path": "report.pdf"
}
```

字段含义：

- `name`：实际保存后的文件名，用于界面显示
- `size_bytes`：实际写入的字节数；Composer 不显示该字段
- `dir_name` 和 `path`：提交 task 时使用的文件引用

如果目标目录已有同名文件，上传不能覆盖旧文件。服务端应生成一个不冲突的新文件名，并通过 `name` 和 `path` 返回实际结果。

## 7) Task 请求

`POST /tasks` 增加可选字段 `file_references`：

```json
{
  "task": "比较这两份报告的差异",
  "file_references": [
    {
      "dir_name": "workspace_dir",
      "path": "report-a.pdf"
    },
    {
      "dir_name": "workspace_dir",
      "path": "report-b.pdf"
    }
  ]
}
```

兼容规则：

- `file_references` 缺失或为空时，行为与现有 task 完全一致
- 文件引用不能被拼接进 `task` 字段
- Console 历史中的用户文本仍然只显示原始 `task`

runtime 在执行 task 前必须再次校验每个引用：

- root alias 合法
- 当前 task 可以解析该 root
- 路径没有逃逸
- 文件存在
- 目标是普通文件

校验通过后，runtime 把文件引用作为独立的 task 上下文交给 Agent。具体 message 排列不属于本需求。

### 7.1 图片引用

如果当前 task 的文件引用中包含 JPEG、PNG 或 WebP，且本次实际选择的模型支持图片输入，Console runtime 应把这些图片作为当前用户消息的多模态 image part 发送给 LLM。

限制与现有 channel 图片输入保持一致：

- 每次最多发送 3 张图片
- 每张图片最多 5 MiB
- 只处理当前 task 的图片，不把历史图片内容重新放进上下文
- 不支持图片输入的模型仍然接收原始 task 和文件引用
- PDF、文本及其他文件类型只作为普通文件引用

## 8) Scope 与生命周期

文件 item 属于当前 Composer draft，作用域与现有文本 draft 相同：

- endpoint
- topic，或当前 new topic

文件引用不能自动带到另一个 endpoint 或 topic。

首版生命周期规则：

- 上传成功：加入当前 draft
- 用户移除：从当前 draft 删除引用
- task 提交成功：清空当前 draft 的文件引用
- task 提交失败：保留当前 draft 的文件引用
- 切换 draft：不能把当前文件引用显示在另一个 draft 中
- 刷新页面：允许丢失尚未提交的文件引用

上传到磁盘的文件不随 Composer item 一起删除。`file_cache_dir` 的清理由现有 cache 策略负责，workspace 文件由用户管理。

## 9) 状态与错误

文件 item 只需要三种状态：

- `uploading`
- `ready`
- `failed`

规则：

- 只有 `ready` 文件可以进入 `file_references`
- 上传中的文件存在时不能提交 task
- 上传失败必须显示文件名和错误
- 失败 item 可以移除，也可以重新选择文件上传
- 单次上传请求总大小上限为 64 MiB

不要增加百分比进度、后台任务状态或自动重试。

## 10) 验收标准

实现完成后必须满足：

1. `Upload Files` 和 `Attach Workspace` 同时出现在 `+` 菜单中。
2. 上传文件不会改变 textarea 的文字、光标位置或选区。
3. 上传成功后，Composer 显示文件名、预览和移除操作。
4. 有 workspace、待提交 workspace 和无 workspace 三种情况下，上传目录都符合规则。
5. 移除文件 item 后，磁盘文件仍然存在。
6. 支持的文件可以只读预览；不支持的类型不会被执行。
7. task 请求通过 `file_references` 发送引用，`task` 保持用户原文。
8. 非法 alias、路径逃逸、文件不存在和目录引用都会被拒绝。
9. task 提交成功后清空文件 item，提交失败后保留。
10. 不带 `file_references` 的现有客户端和 task 行为不变。
11. 已提交文件显示在对应用户消息的独立文件气泡中，刷新历史后仍能恢复。
12. JPEG、PNG、WebP 引用在模型支持时作为当前消息的多模态 image part 发送。
13. 同一文件气泡中的多个文件可以在预览框内通过按钮切换。
