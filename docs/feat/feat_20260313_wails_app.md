---
date: 2026-03-13
title: Wails Desktop App 打包方案（MVP）
status: superseded
---

# Wails Desktop App 打包方案（MVP）

> 历史说明（2026-03-19）：
> 本文档中的 `setup mode` 设计和 `GET /console/api/setup/status`、`POST /console/api/setup/apply` 接口已经移除。
> 当前实现采用统一默认配置 + Console 现有设置流程（如 `/settings/agent`）完成首启配置。

## 1) 背景与问题

当前 `mistermorph` 桌面使用路径需要用户手动完成以下步骤：

- 启动 daemon（`mistermorph serve`）
- 构建并托管 Console SPA（`mistermorph console serve --console-static-dir ...`）
- 浏览器访问 `http://127.0.0.1:9080/console`

这条链路对普通桌面用户门槛较高，不符合“下载即用”的独立应用体验。

目标是基于 [Wails](https://github.com/wailsapp) 提供一个独立桌面 App（Windows/macOS/Linux），把现有核心能力（任务、文件管理、诊断）打包到单一进程体验中。

## 2) 目标与非目标

### 2.1 目标（MVP）

- 提供可直接启动的桌面 App，无需用户手动启动 daemon/console 服务。
- 复用现有 Console WebUI（`web/console`）与现有后端能力，避免重复开发。
- 保持现有 `file_state_dir`、配置体系、权限模型兼容。
- 构建可发布产物（至少支持本地打包与安装验证）。

### 2.2 非目标（MVP 不做）

- 不重写 Console 前端为全新 UI 框架。
- 不改造为云端多租户桌面控制台。
- 不引入自动更新服务（auto-update）与应用商店发布流程。
- 不在本阶段迁移到 Wails v3 Alpha。

## 3) 版本策略与关键决策

### 3.1 Wails 版本策略

- MVP 使用 **Wails v2**（稳定主线）构建桌面应用。
- Wails v3 当前仍为 alpha 迭代分支，暂不作为生产打包基线。

### 3.2 架构策略（复用优先）

- 前端：复用 `web/console`（Vue + Vite + pnpm）。
- 后端：复用现有 console backend 与 runtime 读写逻辑。
- 集成方式：由 Wails App 进程托管本地 API 服务与 WebView，不再要求用户手动起多个进程。

### 3.3 工程策略

- 新增桌面端入口模块，不污染现有 CLI 主路径。
- 采用渐进式迁移，先做到“可运行可发布”，再考虑深度原生化（如更多 Go bindings）。

## 4) 用户故事

1. 作为桌面用户，我安装后双击应用即可进入管理界面，不需要终端命令。
2. 作为桌面用户，我可以在应用内查看任务、编辑 TODO/Contacts/Persona、查看诊断状态。
3. 作为开发者，我可以继续通过现有配置体系控制行为，不需要学习一套新的配置格式。
4. 作为维护者，我可以在不改现有业务核心的前提下完成桌面发行。

## 5) 功能需求（MVP）

### 5.1 启动与生命周期

- App 启动后自动初始化运行时依赖（配置、状态目录、后端服务、UI）。
- App 退出时执行有序清理，避免状态损坏或文件句柄泄露。

### 5.2 Console 能力覆盖

- 保持当前 Console 核心能力可用：
  - task list/detail
  - TODO 文件编辑
  - Contacts 文件编辑
  - Persona 文件编辑
  - diagnostics/config 查看

### 5.3 配置与状态

- 默认沿用 `~/.morph` 状态目录行为（兼容现有数据）。
- 支持在 App 内查看关键配置状态（读即可，MVP 可不做全量可视化配置编辑）。

### 5.4 错误可见性

- 后端启动失败时，UI 显示明确错误原因与建议（例如依赖缺失、配置错误、权限问题）。
- 提供基础日志入口（至少可定位日志目录）。

### 5.5 Setup 模式（强制初始化门控）

- 后端必须支持“配置未完成”时启动（bootstrap/setup mode），不能因为缺少完整运行配置而整体起不来。
- 当处于 setup mode 时，仅开放 setup 相关 API 与最小健康检查 API。
- 非 setup API 在 setup mode 下统一返回 `setup_required`（禁止进入主功能页面）。
- Setup 提交时后端必须执行真实校验（例如对目标 LLM 配置发起最小可用请求），不能只做静态字段校验。
- Setup 提交成功后返回 `restart_required=true`，由桌面 App 触发重启并进入正常模式。

## 6) 非功能需求

- 冷启动目标：可接受范围内完成 UI 可交互（MVP 不设硬实时指标）。
- 可靠性：异常退出后再次启动不应破坏已有状态文件。
- 安全性：不新增明文敏感信息输出；继续遵守现有 secrets/guard 规则。
- 可维护性：桌面化改造尽量局限在新增模块与连接层，减少核心业务侵入。

## 7) 技术方案（MVP）

### 7.1 进程形态

单进程桌面应用，内部包含：

- Wails 主进程（窗口生命周期）
- Console API 路由服务（由 App 内部托管）
- Console 前端资源（打包后本地加载）

### 7.2 模块改造清单（建议）

- 新增：桌面应用入口与生命周期模块（例如 `cmd/mistermorph/wailscmd` 或独立 `desktop/` 目录）
- 复用：`cmd/mistermorph/consolecmd` 既有服务能力（必要时抽可复用启动器）
- 复用：`web/console` 前端产物
- 补充：桌面模式的配置装载与错误呈现层

### 7.3 与现有 CLI 的关系

- CLI 保持原有命令行为不变。
- 桌面端新增独立启动路径，不影响 `run/serve/console serve` 现有用户。

### 7.4 Setup mode 后端改造细化

#### 7.4.1 后端状态模型

- `setup_required`：最小配置不满足，仅允许 setup API。
- `ready`：最小配置满足，开放全部业务 API。

最小配置建议（MVP）：

- 至少一个可用 LLM 主路由（provider/model/key 或等价配置）。
- Console 最小运行必需参数（若保留登录，则需 password/hash；若桌面免登录，则应有明确开关）。

#### 7.4.2 API 门控策略

- setup mode 下允许：
  - `GET /console/api/setup/status`
  - `POST /console/api/setup/apply`
  - `GET /health`（只返回 setup 健康态，不代表业务可用）
- setup mode 下拒绝：
  - 现有 `/console/api/*` 业务接口（统一错误码）

统一错误响应（建议）：

- HTTP `409` + body:
  - `code = "setup_required"`
  - `message = "initial setup is required"`

#### 7.4.3 Setup API 契约（MVP 草案）

`GET /console/api/setup/status`

- 用途：查询当前是否处于 setup mode，以及缺失项。

返回示例（未完成初始化）：

```json
{
  "ok": true,
  "mode": "setup_required",
  "missing_fields": [
    "llm.provider",
    "llm.model",
    "llm.api_key"
  ],
  "can_skip_auth": true,
  "app_version": "0.0.0-dev"
}
```

返回示例（已就绪）：

```json
{
  "ok": true,
  "mode": "ready",
  "missing_fields": [],
  "can_skip_auth": true,
  "app_version": "0.0.0-dev"
}
```

`POST /console/api/setup/apply`

- 用途：提交最终配置并持久化。
- 要求：内部先执行真实校验，再原子写入配置；敏感字段不得回显明文；成功后必须要求重启。

请求示例：

```json
{
  "llm": {
    "provider": "openai",
    "model": "gpt-5.2",
    "endpoint": "https://api.openai.com/v1",
    "api_key": "sk-***"
  },
  "console": {
    "auth_mode": "desktop_local"
  }
}
```

成功响应示例：

```json
{
  "ok": true,
  "restart_required": true,
  "applied": [
    "llm",
    "console.auth_mode"
  ],
  "redacted": [
    "llm.api_key"
  ]
}
```

失败响应示例（校验失败，不写入）：

```json
{
  "ok": false,
  "code": "validation_failed",
  "errors": [
    {
      "field": "llm.api_key",
      "message": "unauthorized"
    }
  ]
}
```

业务接口在 setup mode 下的统一失败响应示例：

```json
{
  "ok": false,
  "code": "setup_required",
  "message": "initial setup is required",
  "setup_path": "/console/api/setup/status"
}
```

#### 7.4.4 配置写入与安全

- `config.yaml` 写入必须原子化（临时文件 + rename），避免中途损坏。
- 密钥字段默认优先环境变量引用或安全存储，不建议直接明文落盘。
- setup 日志禁止记录敏感值（token/api key/password）。

#### 7.4.5 重启握手

- `setup/apply` 成功后，前端进入“重启中”页面。
- Wails 宿主触发应用级重启（而不是仅刷新页面）。
- 重启后第一步调用 `GET /console/api/setup/status`，状态为 `ready` 才放行主 UI。

#### 7.4.6 兼容与迁移

- 已有用户（配置完整）应直接进入 `ready`，不经过 setup 页。
- 旧配置部分缺失时进入 setup，并尽量预填可解析字段。
- 保证 CLI 路径不受 setup mode 代码影响（仅桌面入口启用门控）。

## 8) 构建与打包需求

### 8.1 研发环境基线

- Go、Node.js、Wails CLI 满足官方要求。
- 使用 `wails doctor` 作为本机依赖检查入口。
- Linux 新系统（例如 Ubuntu 24.04）需支持 `webkit2gtk-4.1` 场景与对应构建标签策略。

### 8.2 产物要求

- 可在本地生成桌面可执行产物（Windows/macOS/Linux）。
- Windows 支持可选 NSIS 安装包能力（按发行需求启用）。
- 发布文档需明确平台依赖（WebView2、Xcode CLI tools、GTK/WebKit 等）。

## 9) 里程碑与任务清单（Checklist）

### Phase A：最小可运行（桌面壳 + 可访问 Console）

- [x] 明确 Wails 项目目录结构（建议 `desktop/wails/`）并补充简要 README。
- [x] 初始化 Wails 工程骨架（Go 入口、窗口配置、dev/build 脚本）。
- [x] 接入现有前端构建产物（复用 `web/console`，不新建第二套 UI）。
- [x] 设计并实现 App 启动流程（配置加载 -> 后端启动 -> UI ready）。
- [x] [Backend] 实现 setup mode 状态判定器（`setup_required/ready`）与最小配置检查。
- [x] [Backend] 实现 setup mode 路由门控中间件（允许 setup API，拦截业务 API）。
- [x] [Backend] 定义统一错误响应：`409 + code=setup_required`。
- [ ] 将 `consolecmd` 的可复用能力下沉为可调用模块（避免 CLI 命令层直接耦合）。
- [x] 打通桌面内 API 访问路径，确保 Console 页面能正常请求后端。
- [x] [Backend] 新增 `GET /console/api/setup/status`（缺失项、模式、版本）。
- [x] [Backend] 新增 `POST /console/api/setup/apply`（真实校验 + 原子落盘 + restart_required）。
- [x] 实现最小生命周期管理（启动成功、关闭退出、异常退出日志）。
- [x] 输出本地开发流程文档（`pnpm build` + `wails dev` + 常见错误处理）。
- [ ] 完成 smoke test：应用启动后可打开 Dashboard 且无致命报错。

### Phase B：功能对齐（能力覆盖 + 兼容验证）

- [ ] 覆盖并验证核心页面能力：Tasks、TODO、Contacts、Persona、Diagnostics。
- [ ] 统一 `file_state_dir` 行为，验证与现有 `~/.morph` 数据兼容。
- [ ] 增加后端初始化失败可见性（UI 提示 + 日志落地位置）。
- [ ] 增加关键运行状态展示（例如后端 ready、配置来源、版本信息）。
- [ ] 处理端口/实例冲突策略（例如已占用、重复启动、失败回退）。
- [x] [Frontend] 实现 setup 表单与最小步骤流（基础信息 -> 应用）。
- [x] [Frontend] 接入 `setup/apply` 校验失败反馈（字段级错误提示）。
- [x] [Backend] 实现配置原子写入与敏感字段脱敏日志。
- [x] [Wails] 实现 setup 完成后的重启闭环（apply -> 重启 -> status ready）。
- [ ] 明确并实现桌面模式下 setup 前后的认证策略（setup 阶段与正常阶段分别定义）。
- [ ] [Test] 增加 setup mode 集成测试（缺配置启动、门控生效、apply 后可用）。
- [ ] 增加基础集成测试（至少覆盖启动、关键 API 可用、退出清理）。
- [ ] 对比 CLI Console 行为，整理差异清单并逐项确认“可接受/需修复”。

### Phase C：发布准备（三平台打包 + 文档）

- [ ] 建立三平台构建命令（Windows/macOS/Linux）与产物目录约定。
- [ ] 补充平台依赖检查文档（WebView2、Xcode CLI tools、GTK/WebKit）。
- [ ] 设计并实现最小 CI 构建矩阵（至少保证可构建，不要求自动发布）。
- [ ] 约定产物命名、版本号注入与校验信息（如 checksum）。
- [ ] 编写安装/启动验收手册（首次启动、状态目录、故障排查）。
- [ ] 完成已知问题列表（例如 Linux 发行版差异、签名/公证后续项）。
- [ ] 输出发布前检查清单（release checklist）并完成一次彩排发布。

### 跨阶段共性任务

- [x] 保持 CLI 现有路径无回归（`run/serve/console serve`）。
- [x] 关键改动附带回归测试或最小复现用例。
- [x] 变更配置项时同步更新 `assets/config/config.example.yaml` 与文档。
- [ ] 保持安全基线：不在日志/UI 暴露敏感 token 或明文密钥。

## 10) 验收标准（MVP）

- 安装后无需命令行即可启动并使用主功能页面。
- 现有 Console 核心能力在桌面端可用且无明显行为回归。
- `~/.morph` 下已有数据可被正确读取。
- 至少完成一次三平台构建验证（本地或 CI）。
- 在缺少最小配置时，应用可进入 setup 页面并完成初始化；初始化前主功能不可访问。
- setup 提交（`setup/apply`）时的校验失败可正确反馈，成功后可重启进入正常模式。

## 11) 风险与应对

- 风险：平台依赖差异导致某些机器无法启动。  
  应对：将 `wails doctor` 检查与平台依赖说明纳入发布文档，并在启动失败时给出明确提示。

- 风险：桌面封装层与现有 console 代码耦合导致后续维护复杂。  
  应对：优先抽取可复用启动接口，避免复制粘贴 `consolecmd` 逻辑。

- 风险：跨平台打包行为不一致。  
  应对：建立最小构建矩阵与 smoke test（启动、登录、关键页面加载）。

## 12) Open Questions

- MVP 是否保留 Console 登录密码机制，还是桌面单机默认免登录？
- 是否需要系统托盘与“关闭到托盘”行为（可放到后续版本）？
- 首发平台优先级是否需要分批（例如先 macOS + Windows）？

## 13) 参考资料

- Wails 官方仓库：<https://github.com/wailsapp/wails>
- Wails Releases（v2/v3 状态）：<https://github.com/wailsapp/wails/releases>
- Wails v2 安装文档：<https://wails.io/docs/gettingstarted/installation/>
- Wails v2 构建文档：<https://wails.io/docs/gettingstarted/building/>
- Wails CLI 参考（v2.11）：<https://wails.io/ja/docs/reference/cli/>
