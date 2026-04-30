---
date: 2026-04-28
title: Console 通用文件下载 API
status: draft
---

# Console 通用文件下载 API

## 1) 背景

Console 现在需要从运行 agent 的机器下载文件。

最初只考虑 workspace 侧栏，所以接口自然会长成：

```text
GET /workspace/download?topic_id=<topic_id>&path=<path>
```

但从路径语义看，下载不是 workspace 专属能力。agent 能产生或维护文件的位置主要有三类：

- `workspace_dir`：当前 topic 绑定的项目目录
- `file_cache_dir`：下载文件、转换产物、临时产物
- `file_state_dir`：memory、TODO、contacts、skills、guard 等状态文件

因此下载接口应该表达“从一个允许的根目录下载文件”，而不是表达“从 workspace 下载文件”。

## 2) 目标

这版能力只解决一件事：

> 浏览器通过 Console，从远程 runtime 机器的允许目录中下载一个普通文件。

具体目标：

- 支持从 `workspace_dir` 下载文件
- 支持从 `file_state_dir` 下载文件
- 支持从 `file_cache_dir` 下载文件
- 保留 Console 鉴权和 runtime endpoint token 隔离
- 不把下载 proxy 变成任意 raw proxy
- 不引入新的文件管理抽象

## 3) 非目标

这版不做这些事：

- 不支持上传
- 不支持目录打包下载
- 不支持任意绝对路径下载
- 不做文件预览协议
- 不重做现有 `/state/files`、`/memory/files` 等文本编辑 API
- 不把 `file_state_dir` 和 `file_cache_dir` 暴露成完整文件浏览器

## 4) API

建议新增 runtime API：

```text
GET /files/download?dir_name=<dir_name>&path=<path>[&topic_id=<topic_id>]
```

`dir_name` 只允许三个值：

- `workspace_dir`
- `file_state_dir`
- `file_cache_dir`

`path` 是对应根目录下的相对路径。

`topic_id` 只在 `dir_name=workspace_dir` 时需要。原因是 Console 的 workspace 绑定在 topic 上，不是进程全局值。

示例：

```text
GET /files/download?dir_name=workspace_dir&topic_id=abc&path=src/main.go
GET /files/download?dir_name=file_state_dir&path=TODO.md
GET /files/download?dir_name=file_cache_dir&path=telegram/photo.jpg
```

## 5) 路径规则

路径规则必须简单：

- `dir_name` 必须是允许列表里的 root alias
- `path` 必须是相对路径
- `path` 不能为空
- 清理路径后不能是 `.`
- 禁止 `..` 逃逸
- 禁止绝对路径
- 最终目标必须是普通文件
- 目录下载返回错误
- 允许 root 内的 symlink 指向 root 外部

这里的边界是请求路径的字面边界，不是 symlink 的真实路径边界。也就是说，`path` 本身不能用 `..` 或绝对路径逃出 root，但如果 root 内某个 symlink 指向 root 外，下载允许跟随它。

## 6) 响应

成功时返回文件内容：

```text
200 OK
Content-Disposition: attachment; filename="<name>"; filename*=UTF-8''<encoded-name>
X-Content-Type-Options: nosniff
```

`Content-Type` 可以交给 `http.ServeContent` 或按扩展名设置。下载语义主要由 `Content-Disposition: attachment` 决定。

错误建议：

- `400 Bad Request`：参数缺失、`dir_name` 非法、路径逃逸、目标是目录
- `401 Unauthorized`：鉴权失败
- `404 Not Found`：目标文件不存在
- `503 Service Unavailable`：runtime 无法解析 workspace 或读取文件

## 7) Console Proxy

浏览器不应该直接访问 runtime endpoint。

原因有两个：

- runtime endpoint token 不能暴露给浏览器
- Console 需要按当前选中的 endpoint 代理请求

因此 Console server 保留下载代理：

```text
GET /api/proxy/download?endpoint=<endpoint_ref>&uri=<encoded_runtime_uri>
```

但这个 proxy 只能允许：

```text
/files/download
```

也就是：

```text
Browser
  -> /api/proxy/download?endpoint=...&uri=/files/download?dir_name=...
  -> selected runtime endpoint
  -> /files/download?dir_name=...
```

不要让这个 proxy 转发任意路径的二进制响应。

## 8) 和现有 API 的关系

旧的 workspace 下载接口不保留，直接迁移到新接口：

```text
/workspace/download
```

迁移后的前端调用：

```text
/files/download?dir_name=workspace_dir&topic_id=<topic_id>&path=<path>
```

`/state/files` 等接口继续负责文本文件列表和编辑，不承担二进制下载职责。

## 9) 前端行为

Workspace 侧栏：

- 选中文件时显示下载按钮
- 选中目录时禁用下载按钮
- 调用 `dir_name=workspace_dir`
- 附带当前 `topic_id`

State files 页面：

- 可以给当前文档加下载按钮
- 调用 `dir_name=file_state_dir`
- `path` 使用该文档相对 `file_state_dir` 的路径

Cache 文件下载：

- 需要先有列表或明确路径来源
- 调用 `dir_name=file_cache_dir`

浏览器侧仍然用 object URL 触发保存。文件名优先取服务端 `Content-Disposition`；如果取不到，就用当前选中文件名。

## 10) 实现草图

runtime 侧保持一个下载解析函数即可：

```go
type FileDownloadRequest struct {
    DirName string
    Path    string
    TopicID string
}

type FileDownloadFunc func(ctx context.Context, req FileDownloadRequest) (string, error)
```

解析规则：

- `workspace_dir`：用 `topic_id` 找到当前 topic 的 workspace，再解析 `path`
- `file_state_dir`：用运行时配置里的 `file_state_dir` 作为 root
- `file_cache_dir`：用运行时配置里的 `file_cache_dir` 作为 root

最终返回一个已经校验过的本机文件路径。

真正写 HTTP 响应的函数只负责：

- 打开文件
- 拒绝目录
- 设置下载响应头
- 调用 `http.ServeContent`

## 11) 测试点

后端测试至少覆盖：

- `workspace_dir` 下载普通文件
- `file_state_dir` 下载普通文件
- `file_cache_dir` 下载普通文件
- 缺少 `topic_id` 时下载 workspace 失败
- 非法 `dir_name` 失败
- 绝对路径失败
- `..` 逃逸失败
- 目录下载失败
- proxy 拒绝非 `/files/download` 的 URI

前端测试或手工验证：

- workspace 文件按钮可点击
- workspace 目录按钮禁用
- state file 能下载当前文档
- 下载失败时显示现有错误提示
