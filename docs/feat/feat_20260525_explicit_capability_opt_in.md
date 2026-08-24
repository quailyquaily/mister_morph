---
date: 2026-05-25
title: 显式启用 skill 和 tool
status: draft
---

# 显式启用 skill 和 tool

## 目标

用户可以在任务文本里写 `$name`，为当前任务显式启用一个 skill 或 tool。

`$name` 不是命令，不直接执行工具，也不是错误检查机制。它只影响当前任务的 prompt 和 tool schema。

实现上，`$name` 和自然语言 image intent 都产出同一种 tool trigger。后续注册逻辑只看本轮 tool trigger，不再让 image 注册函数单独解析 task。

## 语义

`skills.enabled=false` 关闭自动和配置加载，但不阻止任务正文里的显式引用：

```text
skills.enabled=false -> 不自动加载 skill，也不读取 skills.load
skills.enabled=false -> 没有显式引用时，system prompt 不包含 skill block
$name -> 当前任务加载匹配的 skill
```

`tools.<name>.enabled=false` 只表示默认不暴露这个 tool：

```text
tools.<name>.enabled=false -> 默认不注册 tool
$name -> 当前任务可以注册同名 tool
```

显式启用 tool 不绕过这些检查：

1. 宿主 allowlist
2. guard
3. 沙箱
4. 凭据
5. 运行时依赖
6. tool 参数校验

## 解析规则

只支持一种语法：

```text
$name
```

解析顺序：

1. 先匹配 skill；显式引用不受 `skills.enabled` 限制。
2. 没有匹配 skill 时，再匹配已知内置 tool。
3. 都没有匹配时，当作普通用户文本。

不提示“找不到”，不报错，不拦截任务。

例子：

```text
$bash 运行 go test ./...
$image_generate 生成一个图标
$url_fetch 拉取这个 URL 并总结
```

如果存在名为 `bash` 的 skill，`$bash` 会先触发 skill，不会再触发同名 tool。

## 注册规则

静态工具：

```text
selected && (enabled || trigger)
```

`selected` 是宿主传入的 tool allowlist，显式启用不能绕过它。

运行时工具：

```text
enabled || trigger
```

适用范围：

1. `plan_create`
2. `todo_update`
3. `image_generate`
4. `image_edit`
5. `spawn`
6. `acp_spawn`

`image_generate` 和 `image_edit` 仍然需要可用的 image LLM 配置。`acp_spawn` 仍然需要 ACP profile。

MCP tool 暂不支持 `$name` 启用 disabled server。原因是 server 未连接前，运行时不一定知道它会提供哪些 tool。

## 测试点

1. `skills.enabled=false` 时，不自动加载 skill，但 `$name` 仍加载匹配的 skill。
2. `skills.enabled=true` 时，配置加载和 `$name` 都可以加载匹配的 skill。
3. 同名 skill 优先于 tool。
4. disabled static tool 可以被 `$name` 当前任务启用。
5. `$name` 不能绕过 selected tool allowlist。
6. `$missing` 保持普通文本，不报错。
7. `$image_generate` 可以触发 image tool 注册，但不能绕过 image 配置。
8. `$acp_spawn` 不能绕过缺失的 ACP profile。
