---
date: 2026-04-28
title: Console Artifact Preview
status: draft
---

# Console Artifact Preview

## 1) 背景

Console Chat 现在已经能渲染 Markdown、代码块和多种图表。

但它还不能预览 agent 写出来的运行结果。例如 agent 在 workspace 中生成：

```text
profile.html
style.css
app.js
```

用户只能去文件树里找文件，或者把文件下载到本地再打开。这个路径太长。

这个能力一般叫：

- Artifact Preview
- Live Preview
- Sandboxed Web Preview

本文统一叫 **Artifact Preview**。

## 2) 目标

第一阶段只做一件事：

> 在 Console Chat 里预览 agent 生成的 Web artifact。

具体目标：

- 支持预览 `workspace_dir` 下的 HTML 入口文件
- 支持 HTML 的相对 CSS、JS、图片、字体资源
- 预览必须在 sandbox iframe 内运行
- 生成的 JS 不能访问 Console 主页面
- Console endpoint token 不能暴露给 iframe
- 不改造现有 Markdown renderer 的核心渲染逻辑

## 3) 非目标

第一阶段不做这些事：

- 不运行后端服务
- 不运行 npm dev server
- 不支持任意端口反向代理
- 不支持浏览器外部网络访问
- 不做完整 IDE
- 不做多文件编辑器
- 不把任意 Markdown 链接都自动变成预览
- 不自动执行所有 HTML 文件

## 4) 第一性原理

Artifact Preview 的核心对象不是 Markdown，也不是代码块。

核心对象是：

> agent 已经写到运行机器上的一个文件产物。

因此最小模型是：

```text
artifact = relative path + root alias
```

其中：

- relative path 表示入口文件
- root alias 表示文件在哪个允许目录里

对 Web 预览来说，入口文件是一个 `.html` 或 `.htm` 文件，不要求文件名必须是 `index.html`。

## 5) Artifact 引用格式

不要从自然语言里猜 artifact。

第一阶段建议用一个明确的 fenced block：

````markdown
```artifact
path=profile.html
dir_name=workspace_dir
```
````

字段：

- `path`：必填。入口文件相对 root 的路径，可以是任意 `.html` 或 `.htm` 文件名
- `dir_name`：必填。可选值为 `workspace_dir`、`file_cache_dir`、`file_state_dir`
- `topic_id`：可选。`dir_name=workspace_dir` 时前端可以用当前 topic 补齐

Console 注入给 agent 的 artifact prompt 必须按当前 topic 动态生成。当前 topic 没有绑定 workspace 时，prompt 里不要出现 `workspace_dir`，示例也只列出 `file_cache_dir|file_state_dir`。

不要规定固定 artifact 目录。新建 Web artifact 时，使用实际创建的 HTML 路径；如果目标文件已经存在，除非用户要求替换，否则换一个不冲突的描述性文件名。

为什么不直接复用 Markdown 链接：

- 普通链接没有 root alias 语义
- 普通链接无法表达 topic-scoped workspace
- 普通链接很容易误触发
- 明确 block 更容易测试，也更容易让 agent 学会输出

未来可以让 runtime 在 task result 里附带结构化 artifacts，但第一阶段用 Markdown block 成本更低。

## 6) Chat UI

Markdown renderer 遇到 `artifact` block 时，不直接渲染成代码块，而是渲染一个 Artifact 卡片。

卡片内容：

- 文件路径
- 类型标识，例如 `Web`
- 折叠、刷新、下载、全屏按钮

最新完成的 agent 消息里如果有 artifact block，Console 自动在当前消息内展开预览面板。

历史消息里的旧 artifact 不批量自动打开，避免进入一个长 topic 时同时启动多个 iframe。

预览面板包含：

- iframe 预览区域
- 失败提示

同一张卡片被用户收起后，不再自动重新打开。

原因：

- artifact block 已经是 agent 的显式预览声明
- 避免一次性加载多个 artifact
- 避免长对话中旧 artifact 自动消耗资源

## 7) API 设计

现有 `/files/download` 是附件下载接口，不适合 iframe 预览。

原因：

- 它使用 `Content-Disposition: attachment`
- iframe 不能主动带 Console 的 bearer token
- HTML 里的相对 `style.css`、`app.js` 还需要继续由 Console 代理

因此需要单独的 preview API。

### 7.1 创建预览票据

Console API：

```text
POST /api/artifacts/preview-ticket
```

请求体：

```json
{
  "endpoint_ref": "ep_console_local",
  "dir_name": "workspace_dir",
  "topic_id": "abc",
  "path": "profile.html"
}
```

返回：

```json
{
  "ticket": "...",
  "entry_url": "/api/artifacts/preview/<ticket>/profile.html",
  "expires_at": "2026-04-28T12:00:00Z"
}
```

票据规则：

- 短期有效，例如 5 分钟
- 只能由已认证的 Console session 创建
- 绑定 endpoint、dir_name、topic_id、entry path
- 只能用于 GET
- 预览展开期间，Console 前端在过期前自动续签

需要 ticket 的原因：

- iframe `src` 不能设置 Authorization header
- 不应该把 runtime endpoint token 交给浏览器
- 预览资源需要靠普通 URL 加载

### 7.2 续签预览票据

Console API：

```text
POST /api/artifacts/preview-ticket/renew
```

请求体：

```json
{
  "ticket": "..."
}
```

返回：

```json
{
  "ticket": "...",
  "entry_url": "/api/artifacts/preview/<ticket>/profile.html",
  "expires_at": "2026-04-28T12:05:00Z"
}
```

续签成功时，iframe URL 不变，页面不重载。

如果 Console 进程重启，内存 ticket 会丢失。前端续签失败后，重新调用 `POST /api/artifacts/preview-ticket` 创建新 ticket，并用新的 `entry_url` 重载 iframe。

### 7.3 预览资源代理

Console API：

```text
GET /api/artifacts/preview/<ticket>/<relative_asset_path>
```

行为：

1. 校验 ticket
2. 把 `<relative_asset_path>` 解析到 entry file 所在目录下
3. 调用 runtime preview API 读取文件
4. 设置 inline 响应头
5. 返回文件内容

例如：

```text
entry: profile.html
asset: style.css
runtime path: style.css
```

如果入口是：

```text
demos/todo/index.html
```

那么：

```text
asset: app.js
runtime path: demos/todo/app.js
```

禁止资源路径逃出 entry 所在目录。

### 7.4 Runtime 预览接口

Runtime API：

```text
GET /files/preview?dir_name=<dir_name>&path=<path>[&topic_id=<topic_id>]
```

它和 `/files/download` 使用同一套路由参数和路径规则，但响应头不同：

- 不设置 `Content-Disposition: attachment`
- 设置正确 `Content-Type`
- 设置 preview 专用 CSP

第一阶段允许：

- `.html`
- `.css`
- `.js`
- 图片
- 字体
- `.json`
- `.svg`

目录仍然拒绝。

## 8) 路径规则

沿用通用文件下载 API 的 root alias：

- `workspace_dir`
- `file_state_dir`
- `file_cache_dir`

第一阶段 UI 只暴露 `workspace_dir`。

路径规则：

- `dir_name` 必须来自允许列表
- `path` 必须是相对路径
- `path` 不能为空
- 禁止绝对路径
- 禁止 `..` 逃逸
- `workspace_dir` 必须带 `topic_id`
- symlink 行为和 `/files/download` 保持一致

## 9) Sandbox 与 CSP

预览必须使用 iframe sandbox。

建议：

```html
<iframe sandbox="allow-scripts allow-forms"></iframe>
```

不要加：

- `allow-same-origin`
- `allow-top-navigation`
- `allow-popups`

这样生成页面可以运行脚本，但不能和 Console 主页面共享 origin。

Preview 响应建议加 CSP：

```text
default-src 'none';
script-src 'self' 'unsafe-inline' blob: data:;
style-src 'self' 'unsafe-inline';
img-src 'self' data: blob:;
font-src 'self' data:;
connect-src 'none';
frame-src 'none';
form-action 'none';
base-uri 'none';
```

这允许常见的单页 HTML demo 运行，同时阻止它主动访问外部网络。

## 10) 和 Markdown renderer 的关系

不要把 preview 逻辑写进通用 Markdown renderer 深处。

建议在 Console 侧做一层轻量扩展：

1. MarkdownContent 仍然负责普通 Markdown
2. Console Chat 在渲染前解析 `artifact` block
3. 普通 Markdown 片段交给 MarkdownContent
4. Artifact block 渲染成 ArtifactPreviewCard

这样可以避免 vendor markdown renderer 变复杂，也不会影响其他页面。

## 11) Agent 输出约定

需要在 Console prompt 或系统提示里加入一条简单约定：

当 agent 生成可预览 Web artifact 时，在最终回复里附带：

````markdown
```artifact
path=profile.html
dir_name=workspace_dir
```
````

如果 `topic_id` 不方便让 agent 填，前端可以在渲染时补当前 topic。

`path` 和 `dir_name` 必须明确。`type` 和 `title` 不属于 artifact block 契约。

不要为了预览固定写 `index.html`，也不要为了预览固定写进某个 artifact 目录。使用这次实际创建的 HTML 文件路径；如果会覆盖已有文件，换一个新的描述性文件名。

## 12) 失败处理

常见失败：

- 文件不存在
- workspace 未绑定
- `topic_id` 缺失
- 路径越界
- HTML 引用的资源不存在
- 资源 MIME 类型不支持
- ticket 过期

UI 上只需要给出短错误：

- `Preview unavailable`
- `File not found`
- `Preview ticket expired`

不要把服务端绝对路径显示给用户。

## 13) 后续扩展

后续可以支持更多 artifact 类型：

- 图片预览
- SVG 预览
- Markdown 文件预览
- Mermaid 文件预览
- JSON 表格预览
- PDF 预览

也可以把 artifact 从 Markdown block 升级为 task result 的结构化字段：

```json
{
  "artifacts": [
    {
      "dir_name": "workspace_dir",
      "path": "profile.html"
    }
  ]
}
```

## 14) 测试点

后端测试：

- 创建 preview ticket 成功
- ticket 过期后拒绝访问
- iframe entry 能返回 HTML
- 相对 CSS/JS 能返回
- 资源路径不能逃出 entry 目录
- proxy 不暴露 runtime endpoint token
- 非允许扩展名拒绝

前端测试或手工验证：

- `artifact` block 渲染成卡片
- 最新完成消息中的 artifact 自动加载 iframe
- Refresh 能重新加载 iframe
- 收起后停止 iframe
- HTML 能加载同目录 CSS/JS
- iframe 内 JS 不能访问 parent
- iframe 内网络请求被 CSP 阻止
