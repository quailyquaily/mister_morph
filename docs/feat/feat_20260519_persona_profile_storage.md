---
date: 2026-05-19
title: Persona Profile Storage
status: draft
---

# Persona Profile Storage

## 背景

当前 persona 相关文件分散在 state 根目录：

```text
state/
  IDENTITY.md
  SOUL.md
```

`IDENTITY.md` 实际上是 Markdown 包 YAML block。随着 Settings 里提供结构化表单编辑，它已经不再适合作为自由 Markdown 文件。

同时，persona 需要开始承载非 prompt 资料，例如 agent 头像。头像是展示资产，不应该写进 prompt 文件，也不应该用 base64 放进 YAML。

## 目标

把 persona 资料收敛到独立目录，并把 identity 从 Markdown 改成纯 YAML：

```text
state/
  persona/
    identity.yaml
    soul.md
    avatar.webp
```

语义边界：

1. `persona/identity.yaml`：结构化身份，进入 prompt。
2. `persona/soul.md`：人格规则文本，进入 prompt。
3. `persona/avatar.webp`：UI 展示资产，不进入 prompt。

## 非目标

1. 不把 `soul.md` 改成 YAML。它仍然是自由 Markdown。
2. 不把头像写进 `identity.yaml`。
3. 不引入 persona manifest 或 profile JSON。
4. 不做多头像、多主题、多 profile 切换。
5. 不删除旧路径文件。旧路径只做兼容读取和迁移来源。

## 文件格式

### `persona/identity.yaml`

最小格式：

```yaml
name: ""
name_alts: []
creature: ""
vibe: ""
emoji: ""
```

规则：

1. Settings 表单只暴露当前需要编辑的字段：`name`、`creature`、`vibe`、`emoji`。
2. `name_alts` 暂时不提供 UI 入口，但文件中保留为数组字段。
3. 保存时保持合法 YAML。
4. 未识别字段必须保留，但 prompt 使用只依赖已定义字段。

### `persona/soul.md`

保持 Markdown，继续使用现有段落约束：

1. `Core Truths`
2. `Boundaries`
3. `Vibe`

### `persona/avatar.webp`

规则：

1. 固定文件名：`avatar.webp`。
2. Settings 和 Setup 允许选择 PNG、JPEG、WebP 源图。
3. 前端裁剪后统一输出 `512x512` WebP。
4. Avatar 只用于 UI 显示，不进入 prompt。
5. 删除头像时移除 `persona/avatar.webp`，UI 回退到默认头像。
6. 默认头像直接使用现有 logo，不新增一份重复图片资产。
7. 上传头像必须在前端通过裁剪组件强制裁成正方形。
8. 服务端只保存裁剪后的 WebP 文件，并校验图片类型和大小。
9. 服务端不负责交互式裁剪，也不负责把其他格式转成 WebP。

## 兼容与迁移

Canonical 路径：

1. `persona/identity.yaml`
2. `persona/soul.md`
3. `persona/avatar.webp`

旧路径兼容读取顺序：

Identity：

1. `persona/identity.yaml`
2. `persona/IDENTITY.md`
3. `IDENTITY.md`

Soul：

1. `persona/soul.md`
2. `persona/SOUL.md`
3. `SOUL.md`

迁移规则：

1. 读取时优先新路径。
2. 如果新路径不存在、旧路径存在，读取旧路径作为 fallback。
3. Settings 保存 identity 时永远写入 `persona/identity.yaml`。
4. Settings 保存 soul 时永远写入 `persona/soul.md`。
5. 启动时运行专门的迁移模块：
   - 从旧 `IDENTITY.md` 提取 YAML block，写入 `persona/identity.yaml`。
   - 从旧 `SOUL.md` 或 `persona/SOUL.md` 复制到 `persona/soul.md`。
6. 迁移不删除旧文件。
7. 如果新旧文件同时存在，以新文件为准。
8. 迁移模块不得覆盖已存在的新路径文件。
9. 迁移失败不阻断启动；错误要可诊断，已有 fallback 读取能力不能失效。

迁移模块建议：

1. 新增独立包，例如 `internal/personamigrate`。
2. 启动 console / desktop / channel runtime 前执行一次。
3. 输入只依赖 state dir。
4. 输出只写 canonical persona 路径。
5. 模块内集中处理旧路径探测、目录创建、identity YAML 提取、soul 复制和错误摘要。

## 后端接口

继续复用 state file 思路，但 persona 应有清晰边界。

建议接口：

1. `GET /persona/files`
   - 返回 `identity.yaml`、`soul.md`、`avatar.webp` 的存在状态和元信息。
2. `GET /persona/files/identity.yaml`
   - 返回 identity YAML 文本。
3. `PUT /persona/files/identity.yaml`
   - 写入 identity YAML。
4. `GET /persona/files/soul.md`
   - 返回 soul Markdown 文本。
5. `PUT /persona/files/soul.md`
   - 写入 soul Markdown。
6. `GET /persona/avatar`
   - 返回头像图片；不存在时返回 404。
7. `PUT /persona/avatar`
   - 上传裁剪后的 WebP 头像，服务端保存为 `persona/avatar.webp`。
8. `DELETE /persona/avatar`
   - 删除头像。

约束：

1. `PUT /persona/avatar` 只接受 WebP 图片。
2. 服务端限制头像文件大小，建议初始上限 2 MB。
3. 服务端不接受任意 persona 子路径写入，避免把接口变成通用文件管理器。
4. `identity.yaml` 写入前要做 YAML parse 校验。
5. `identity.yaml` 保存时必须保留未知字段。
6. `soul.md` 写入前继续做现有章节校验。

## Settings UI

Settings 中 `Persona` 是第一个设置入口。

布局建议：

1. `Identity` 卡片
   - 表单字段：Name、Emoji、Creature、Vibe。
   - 保存写入 `persona/identity.yaml`。
2. `Soul` 卡片
   - Markdown 编辑器。
   - 保存写入 `persona/soul.md`。
3. `Avatar` 卡片
   - 当前头像预览。
   - 上传按钮。
   - 正方形裁剪组件。
   - 删除按钮。
   - 上传成功后立即刷新预览。

UI 规则：

1. Avatar 不显示在 raw Markdown 编辑区域里。
2. Identity 表单不展示 `name_alts`。
3. 上传失败要显示明确错误。
4. 桌面版和普通 Console Web 使用同一套 UI。
5. 未设置头像时，头像预览显示默认 logo。
6. 头像上传必须先进入裁剪流程，确认后再上传。
7. 源图格式支持 PNG、JPEG、WebP。
8. 裁剪确认后上传 `512x512` WebP。

## Setup UI

Setup 里也需要允许上传头像。

建议位置：

1. Persona step 中，与 identity 表单同屏。
2. Done step 中可以显示最终头像预览，但不必再次提供完整编辑能力。

交互规则：

1. 上传行为和 Settings 使用同一套后端接口。
2. 上传前使用同一套正方形裁剪组件。
3. 未上传时显示默认 logo。
4. 跳过头像不阻塞 setup 完成。
5. 上传失败只影响头像，不影响 identity 和 soul 保存。

## Console 展示

Console 侧栏 `sidebar-brand` 使用同一头像来源。

展示规则：

1. 如果存在 `persona/avatar.webp`，`sidebar-brand` 显示该头像。
2. 如果不存在头像，`sidebar-brand` 显示默认 logo。
3. 头像更新后，Settings、Setup 和 sidebar-brand 都应刷新到同一结果。
4. 头像不改变 `sidebar-brand` 文案和导航结构。

## Prompt 组装

Prompt profile 读取新路径：

1. Identity 优先读取 `persona/identity.yaml`。
2. 找不到时 fallback 到旧 `IDENTITY.md`。
3. Soul 优先读取 `persona/soul.md`。
4. 找不到时 fallback 到旧 `persona/SOUL.md`。
5. 再找不到时 fallback 到旧 `SOUL.md`。

Prompt 输出可以继续使用稳定 block 名称：

```text
[IDENTITY]
...

[SOUL.md]
...
```

源文件改名不要求改变 prompt block 名称。

## State Files 视图

`Files` 视图需要调整：

1. Persona 分组下展示新文件：
   - `persona/identity.yaml`
   - `persona/soul.md`
2. 旧的 `IDENTITY.md`、`persona/SOUL.md`、`SOUL.md` 可以继续显示为 legacy 文件，或在存在时显示只读提示。
3. 普通用户编辑 persona 应优先使用 Settings / Persona。

## 实施顺序

### Phase A：迁移模块与读取兼容

1. 新增 persona 路径解析工具。
2. 新增专门迁移模块，启动时自动执行。
3. Prompt 加载支持新路径优先、旧路径 fallback。
4. Onboarding check 支持新路径。
5. State file listing 支持 persona 子目录。

### Phase B：identity.yaml

1. 新增 `identity.yaml` 模板。
2. 从旧 `IDENTITY.md` 提取 YAML block 的迁移函数。
3. Settings / Persona 保存 identity 时写入 `persona/identity.yaml`。
4. 删除前端对 `IDENTITY.md` Markdown wrapper 的生成逻辑。
5. Settings 保存 identity 时保留未知字段。

### Phase C：soul.md 迁移

1. Settings / Persona 保存 soul 时写入 `persona/soul.md`。
2. 旧 `persona/SOUL.md` 和根目录 `SOUL.md` 仅作为 fallback。
3. Repair / setup 流程使用新路径。

### Phase D：avatar.webp

1. 后端增加固定头像读写删除接口。
2. Settings / Persona 增加头像卡片。
3. 前端实现正方形头像裁剪组件。
4. Setup / Persona step 增加头像上传。
5. `sidebar-brand` 读取统一头像 URL。
6. Chat / topbar / setup done 等需要头像的位置读取统一头像 URL。

### Phase E：清理和文档

1. 更新 `docs/prompt.md`。
2. 更新 `docs/console.md`。
3. 更新 `assets/config` 模板。
4. 补充迁移说明。

## 验收标准

1. 新安装生成 `persona/identity.yaml` 和 `persona/soul.md`。
2. 旧安装只有根目录 `IDENTITY.md`、`SOUL.md` 时，应用仍能启动并读到 persona。
3. 用户在 Settings / Persona 保存后，新文件出现在 `persona/` 下。
4. `identity.yaml` 是纯 YAML，不再包 Markdown fence。
5. `identity.yaml` 保存时保留未知字段。
6. 启动时迁移模块自动创建 canonical persona 文件。
7. Avatar 上传时必须经过正方形裁剪，并输出 `512x512` WebP。
8. Avatar 上传、预览、删除可用。
9. Settings 和 Setup 都可以上传头像。
10. 未设置头像时使用默认 logo。
11. 设置头像后，Console 侧栏 `sidebar-brand` 显示该头像。
12. Avatar 不进入 prompt。
13. Prompt 内容和迁移前保持等价。
14. 旧路径文件不会被自动删除。

## 测试清单

1. Identity 读取顺序：
   - 只有 `persona/identity.yaml`
   - 只有 `persona/IDENTITY.md`
   - 只有 `IDENTITY.md`
   - 新旧同时存在
2. Soul 读取顺序：
   - 只有 `persona/soul.md`
   - 只有 `persona/SOUL.md`
   - 只有 `SOUL.md`
   - 新旧同时存在
3. 迁移：
   - 从合法 `IDENTITY.md` 提取 YAML。
   - 从旧 `SOUL.md` 迁移到 `persona/soul.md`。
   - 从旧 `persona/SOUL.md` 迁移到 `persona/soul.md`。
   - 非法 YAML 时返回可诊断错误，不覆盖新文件。
   - 旧文件存在、新文件存在时不覆盖新文件。
   - 启动时自动执行迁移模块。
4. Settings：
   - Identity 表单保存生成纯 YAML。
   - Identity 保存保留未知字段。
   - Soul 保存生成 Markdown。
   - Avatar 源图支持 PNG、JPEG、WebP。
   - Avatar 上传前必须裁剪为正方形。
   - Avatar 上传输出为 `512x512` WebP。
   - Avatar 上传限制文件类型和大小。
   - Avatar 删除后 UI 回退默认头像。
5. Setup：
   - Persona step 可以上传头像。
   - Avatar 源图支持 PNG、JPEG、WebP。
   - 上传头像前必须裁剪为正方形。
   - 上传头像输出为 `512x512` WebP。
   - 未上传头像时仍能完成 setup。
   - 上传失败不阻塞 identity 和 soul 保存。
6. Console 展示：
   - 未设置头像时 `sidebar-brand` 显示默认 logo。
   - 设置头像后 `sidebar-brand` 显示上传头像。
   - 删除头像后 `sidebar-brand` 回退默认 logo。
7. Prompt：
   - 新路径存在时使用新路径。
   - fallback 路径存在时继续工作。
   - avatar 不出现在 prompt block 中。

## 实现 Checklist

- [x] 增加 `persona/` canonical 路径定义。
- [x] 增加启动迁移模块，迁移 legacy identity 和 soul。
- [x] 后端读 prompt 时优先使用 canonical persona 文件。
- [x] 后端 persona 文件 API 支持 `identity.yaml` 和 `soul.md`。
- [x] 后端 avatar API 支持读取、上传和删除 `avatar.webp`。
- [x] Settings / Persona 使用 identity 表单、soul 编辑器和 avatar 裁剪上传。
- [x] Setup / Persona 支持上传头像。
- [x] `sidebar-brand` 支持显示上传头像，未设置时显示默认 logo。
- [x] 补充或更新测试。
- [x] 运行 Go 测试和 Console 构建验证。
