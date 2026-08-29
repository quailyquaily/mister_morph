---
date: 2026-08-29
title: 跨平台本地命令沙箱
status: draft
---

# 跨平台本地命令沙箱

## 1. 背景

Morph 目前通过 `bash` 和 `powershell` 工具直接在宿主机启动命令。Guard 可以要求用户批准命令，但批准与隔离解决的是两个不同问题：

- 审批决定某个动作是否得到授权。
- 沙箱限制已经开始执行的命令实际能访问什么。

当前 Guard 开启审批后，每次 Shell 调用都需要批准。这个策略能阻止未授权执行，但低风险的查看文件、修改 workspace 和运行测试也会反复打断用户。批准后的命令仍直接获得 Morph 进程的宿主机权限。

目标是采用与 Codex 相同的基本模型：普通命令默认在受限环境中运行；只有需要越过边界时才请求批准。OpenAI 对 Codex 的说明也明确区分了这两个控制：sandbox 定义技术边界，approval policy 决定何时可以跨越边界。

沙箱是 Morph 的统一能力，不是 Bubblewrap 的别名。不同平台使用不同的系统机制：

- Linux 和 WSL2：Bubblewrap。
- macOS：系统内置的 Seatbelt。
- 原生 Windows：Windows 原生进程、文件系统和网络隔离。

工具参数、Guard 审批和审计语义必须跨平台一致。平台差异只留在命令执行后端。

参考：

- [OpenAI Codex Sandbox](https://learn.chatgpt.com/docs/sandboxing)
- [OpenAI Codex Windows sandbox](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- [OpenAI Codex Rules](https://learn.chatgpt.com/docs/agent-configuration/rules)

## 2. 目标

完整方案提供以下行为：

1. 本地 Shell 命令默认通过当前平台的沙箱执行。
2. 沙箱内的常规命令不需要逐条审批。
3. Agent 可以明确请求在沙箱外执行一个确定的命令。
4. Guard 批准后，只在宿主机执行这一条等价命令。
5. 沙箱不可用时，绝不静默退化为宿主机直接执行。
6. 保留 Guard 的审批、审计、URL 策略和输出脱敏能力。
7. 用户可以用命令前缀规则允许、逐次审批或禁止特定的沙箱外命令。

这不是用操作系统沙箱替换 Guard。Guard 是 Shell 工具访问本地命令执行能力的唯一界面；它只决定执行模式和授权结果。下层执行器负责真正启动宿主机命令或调用平台沙箱。

## 3. 第一性原则

### 3.1 默认执行必须受技术边界约束

模型承诺“不访问某个目录”不是隔离。默认命令必须由操作系统限制文件系统、网络和进程能力。

### 3.2 越界必须是显式动作

沙箱命令失败后，系统不能自动在宿主机重试。Agent 必须重新发起带有越界意图和理由的调用，用户必须看到实际命令后再决定。

### 3.3 Agent 参数本身不是授权

`require_escalated` 只表示 Agent 请求越界，不能直接切换执行器。只有 Guard 通过并消费一次性审批后，执行层才能使用宿主机模式。Guard 未启用、审批不可用或审批已过期时，宿主机模式必须拒绝执行。

### 3.4 一次批准只对应一个确定动作

审批的 action hash 必须覆盖：

- 工具名
- 完整命令
- 工作目录
- 超时时间
- 执行模式

命令、目录、超时或执行模式发生变化，都需要新的审批。审批不能变成一个可重复使用的通用宿主机权限。

### 3.5 平台差异不能泄漏到工具协议

模型只需要理解 `use_default` 和 `require_escalated`。它不应该决定使用 Bubblewrap、Seatbelt、restricted token 或其他系统实现。

### 3.6 不因沙箱不可用而关闭整个 Morph

当前平台的沙箱不可用时，Chat、Channel、文件工具和网络工具仍应工作。只有需要本地子进程的默认沙箱执行不可用。

### 3.7 Rules 只决定沙箱外执行

命令 rules 不取代沙箱，也不限制普通沙箱内执行。它们只处理 Agent 已明确请求 `require_escalated` 的命令：

- `allow`：在宿主机执行，不再逐次询问。
- `prompt`：每次都请求 Guard 审批。
- `forbidden`：直接拒绝，不允许用户临时批准。

没有匹配规则时默认为 `prompt`。多条规则同时匹配时取最严格结果：`forbidden` 高于 `prompt`，`prompt` 高于 `allow`。

### 3.8 Guard 必须保持薄

Guard 只负责四件事：

1. 接收规范化后的本地命令请求。
2. 根据执行模式、rules 和一次性审批作出决定。
3. 将确定的执行计划交给下层执行器。
4. 记录授权决定和最终执行模式。

Guard 不生成 Bubblewrap 参数，不处理平台探测、mount、namespace、子进程输出或 timeout。Shell 工具也不直接访问 sandbox backend，否则执行策略会分散在工具、Guard 和平台实现三处。

## 4. 实现范围

### 4.1 Linux 第一版

第一版只实现 Linux backend，覆盖：

- `bash`
- `powershell` / `pwsh`

两者已经共用 `tools/builtin/shell_runner.go`，应在这一层选择沙箱或宿主机执行器，不为每个工具分别实现一套 Bubblewrap 参数。WSL2 运行的是 Linux 版本 Morph，因此复用同一 backend。

### 4.2 后续平台

后续按平台增加：

| 平台 | Backend | 安装要求 | 第一版未实现时的行为 |
| --- | --- | --- | --- |
| Linux | Bubblewrap | 使用发行版提供的 `bwrap` | 默认命令失败关闭，可请求越界审批 |
| WSL2 | Bubblewrap | 与 Linux 相同 | 与 Linux 相同 |
| macOS | Seatbelt | 系统内置 | 默认命令失败关闭，可请求越界审批 |
| 原生 Windows | Windows native sandbox | 强模式需要管理员完成一次初始化 | 默认命令失败关闭，可请求越界审批 |

macOS backend 使用系统 Seatbelt，不附带 Linux helper，也不要求安装 Bubblewrap。

Windows backend 的目标与 Codex 一致：

- 优先使用强隔离模式。管理员只负责一次性初始化专用低权限身份、文件权限和防火墙规则；沙箱命令本身不以管理员权限运行。
- 初始化不可用时，可以使用 restricted token 和 ACL 构成的弱隔离模式。
- 弱隔离必须在状态和审计中明确标记，不能伪装成强隔离。

第一版不实现 macOS 和原生 Windows backend，但公共接口不能阻碍后续接入。

### 4.3 进程入口

下一步可以覆盖：

- ACP 的 `terminal/create`

暂不覆盖：

- `coder` 启动的 Codex CLI 或 Claude Code CLI
- ACP agent 主进程
- `stdio` MCP Server
- Morph 自身的 runtime 和桌面进程

这些入口也会启动本地进程，但各自有独立生命周期或权限协议。特别是 coder CLI 可能自带沙箱与审批，直接嵌套会让子进程的越界请求无法穿过 Morph 的外层沙箱。应在明确代理语义后再接入。

以下工具不使用本地进程沙箱：

- `read_file`、`write_file`：它们在 Morph 进程内执行，继续使用路径校验。
- `url_fetch`、`web_search`：继续使用网络策略。
- Telegram、Slack、Mixin 等 Channel 工具：继续使用身份和目标权限。
- LLM Provider 调用：继续使用网络与凭据边界。

## 5. 平台 backend 约束

每个平台 backend 位于 Guard 下层，并实现相同结果：

- 报告是否可用及不可用原因。
- 将固定的 `workspace-write` 策略转换为系统约束。
- 启动命令并保持现有 stdin、stdout、stderr、timeout 和取消语义。
- 返回实际 backend 名称和隔离强度，供日志与审计使用。

不做可动态加载的 backend 插件系统。用 Go 平台文件或 build tags 选择实现即可。未实现的平台返回明确的 unavailable 状态。

## 6. 工具语义

Shell 工具新增两个可选参数：

```json
{
  "cmd": "git push origin master",
  "cwd": "workspace_dir",
  "sandbox_permissions": "require_escalated",
  "justification": "需要连接远端 Git 仓库"
}
```

`sandbox_permissions` 只有两个值：

- `use_default`：默认值，在当前平台的沙箱中运行。
- `require_escalated`：请求经 Guard 批准后在宿主机运行。

`justification` 仅用于帮助用户判断，不改变安全策略。第一版不增加 session 级授权；可重复的例外必须写成可审计的 command rule。

执行流程：

```text
Shell tool call
    │
    └─ Guard
         ├─ use_default
         │     └─ sandbox execution plan
         │
         └─ require_escalated
                └─ command rules
                     ├─ forbidden ───────> deny
                     ├─ allow ───────────> host execution plan
                     └─ prompt / no match
                               ├─ approval unavailable ─> deny
                               └─ require approval
                                         ├─ denied / expired ─> stop
                                         └─ approved ────────> host execution plan

Guard-approved execution plan
    └─ lower command executor
          ├─ sandbox plan ──> platform sandbox backend ──> result
          └─ host plan ─────> direct process execution ───> result
```

宿主机执行使用 Morph 进程当前用户，不使用 `sudo`，也不获得额外的操作系统权限。如果 Morph 本身以 root 运行，应拒绝宿主机越界执行，并要求部署者改用非特权服务账号。

### 6.1 Rule 格式

Morph 使用现有 YAML 配置，不引入 Starlark 或另一种配置语言：

```yaml
guard:
  enabled: true
  rules:
    - pattern: ["git", "status"]
      decision: allow
      justification: "只读取当前仓库状态"
    - pattern: ["git", "push"]
      decision: prompt
      justification: "修改远端仓库"
    - pattern: ["rm", "-rf"]
      decision: forbidden
      justification: "使用明确的文件删除工具"
```

第一版字段只有：

- `pattern`：非空字符串数组，按参数位置匹配命令前缀。
- `decision`：`allow`、`prompt` 或 `forbidden`。
- `justification`：可选的人类可读原因，显示在审批、拒绝结果和审计中。

`pattern` 不使用正则、glob 或子字符串匹配。需要多个变体时写多条规则，不增加 union pattern。

这保留了 Codex rules 的核心语义，但不复制它的 Starlark 文件、配置层合并和内联匹配测试。Morph 已经有一套可通过 Console 管理的 YAML 配置，再增加第二套规则文件只会产生两个配置入口。

规则在启动和配置更新时校验。空 pattern、未知 decision 或无效字段导致该次配置更新失败；不能忽略错误规则后继续运行。

### 6.2 Shell 命令匹配

规则匹配 argv，而不是直接对 `cmd` 字符串做前缀判断。例如：

```text
pattern: ["git", "push"]

match:     git push origin master
not match: git status
not match: ./git push origin master
```

Shell 工具接收的是脚本文本，因此只在能够确定语义时拆分：

- Bash 的静态单条命令可以解析成 argv。
- Bash 中只包含静态单词并由 `&&`、`||`、`;` 或 `|` 连接的线性命令，可以拆成多条命令分别匹配。
- PowerShell 第一版只解析不含操作符、展开或控制流的静态单条命令，不用 Bash 语法解析 PowerShell。
- 包含变量展开、命令替换、重定向、通配符、赋值或控制流时，不尝试推断内部命令。

线性组合命令取所有子命令的最严格结果。例如 `git status && rm -rf build` 不能因为第一条命令匹配 `allow` 就自动获得宿主机权限。

无法安全拆分的脚本不能命中针对内部命令的 `allow`。它按完整的 Shell invocation 处理：Bash 使用 `bash -lc <script>`，PowerShell 使用 `powershell -Command <script>`。没有对应规则时回到默认 `prompt`。解析失败绝不能扩大权限。

## 7. 沙箱边界

第一版提供一种固定的 `workspace-write` 边界，不增加策略语言。

### 7.1 文件系统

- `workspace_dir`：可读写。
- `file_cache_dir`：可读写。
- `file_state_dir`：只读，并隐藏配置、认证材料和 Guard 审批状态。
- 系统运行库和命令目录：只读。
- 临时目录：沙箱内独立创建。
- 宿主机 home、SSH key、云凭据和未声明目录：不可见。

如果没有 attached workspace，沙箱仍可运行，但只有独立临时目录和明确挂载的 cache 可写。

文件系统策略必须由当前平台的系统机制强制执行。Linux 使用 Bubblewrap mount namespace；macOS 和 Windows backend 必须提供等价边界。现有 `deny_paths`、`deny_tokens` 可以继续作为易读的提前报错，但不能把它们当作安全边界。

### 7.2 网络

默认不允许外网访问。Linux 通过独立 network namespace 实现；其他平台使用各自的系统网络边界。需要网络的 `git push`、包下载或远程命令应请求 `require_escalated`。

第一版不增加“保留文件沙箱、只批准网络”的第三种模式。这个模式更精确，但需要单独定义 DNS、代理、目标限制和审批 hash；在出现实际需求前不增加。

### 7.3 进程与环境

- Linux 使用独立的 PID、IPC、UTS 和 network namespace。
- Linux 使用新的 session，只挂载最小的 `/proc` 和 `/dev`，并让沙箱随 Morph 子进程退出。
- macOS 和 Windows 使用各自等价的进程与网络边界，不向公共接口暴露平台术语。
- 继续使用现有 Shell 环境变量白名单，不把 Morph 的 provider、Channel 或认证凭据传入子进程。
- 保留现有 timeout、输出大小限制和流式输出行为。

## 8. 沙箱不可用时

“不可用”包括：

- Linux 的 `PATH` 中找不到 `bwrap`。
- Linux 的 `bwrap` 存在，但内核禁止创建所需的 unprivileged user namespace。
- macOS Seatbelt 初始化或最小探测失败。
- Windows 原生沙箱没有完成初始化，且弱隔离也不可用。
- 当前平台的 backend 尚未实现。
- 当前 backend 的最小启动探测失败。

Morph 启动时应做一次能力探测，而不是等到第一条命令才发现问题。探测结果分为 `available` 和 `unavailable`，并保留可读原因。

不可用时：

1. 启动继续进行，并输出一次明确的 warning。
2. `use_default` 返回 `sandbox_unavailable`，包含安装或系统限制提示。
3. 绝不直接在宿主机执行原命令。
4. Agent 仍可重新请求 `require_escalated`。
5. 只有 Guard 审批启用且用户批准后，才执行这一条宿主机命令。
6. Guard 审批不可用时，本地命令保持不可执行。

因此，沙箱不可用不会让整个 Morph 无法启动，也不会让安全边界静默消失。它只会把本地命令从“沙箱内自动运行”降为“每条宿主机命令必须显式审批”。

### 8.1 Linux 安装与探测

Linux 安装提示：

```text
Ubuntu/Debian: sudo apt install bubblewrap
Fedora:        sudo dnf install bubblewrap
```

仅检查二进制是否存在不够。某些系统安装了 Bubblewrap，但 AppArmor 或 user namespace 设置仍会阻止其工作，所以必须运行最小探测命令。

Codex 还提供了 bundled helper 作为部分 Linux 环境的后备。Morph 第一版不复制这套分发和内核适配逻辑；依赖发行版提供的 Bubblewrap，代码更少，更新责任也更清楚。

### 8.2 macOS 和 Windows

macOS 不应提示安装 Bubblewrap。Seatbelt 是系统能力；不可用时报告具体初始化错误。

原生 Windows 强隔离需要一次管理员初始化。管理员权限只用于建立低权限身份、ACL、策略和防火墙边界，不用于运行 Agent 命令。初始化被系统或企业策略禁止时，可以选择弱隔离 backend；两者都不可用时按本节失败关闭。

WSL2 显示 Linux Bubblewrap 的安装与 user namespace 提示，不显示原生 Windows 初始化提示。

## 9. 配置

第一版不增加 sandbox 配置。只要本地 Shell 工具已启用，Morph 就自动探测当前平台 backend。backend 不可用时按上一节失败关闭；用户不能通过 sandbox 配置把默认执行改成宿主机直接执行。

`guard.rules` 是本方案唯一新增的权限配置。规则只作用于 `require_escalated`，不会改变 workspace mount、网络隔离或其他 sandbox 边界。

不新增以下配置：

- sandbox 总开关
- 自定义平台沙箱参数
- 任意 bind mount 列表
- 从一次审批自动生成永久规则
- 网络目标列表
- 每个 Shell 工具单独的 sandbox 开关

这些配置会让沙箱边界难以审计，也没有第一版的实际需求。

## 10. Guard 与审计

Shell 工具不再自行选择 sandbox backend 或宿主机执行。它将规范化后的命令、工作目录、timeout、`sandbox_permissions` 和 `justification` 交给 Guard。

Guard 的本地命令入口按以下规则生成执行计划：

- `use_default`：允许进入沙箱，不要求审批。
- `require_escalated`：匹配 command rules；`allow` 直接允许，`prompt` 或无匹配时要求审批，`forbidden` 拒绝。

Guard 整体关闭时，平台沙箱仍然生效，但所有 `require_escalated` 请求都拒绝。Guard 开启但 approvals 关闭时，`allow` rules 仍可授权宿主机执行；`prompt` 和无匹配请求因为无法审批而拒绝。

审计事件增加：

- `execution_mode`: `sandboxed` 或 `host`
- `sandbox_backend`: `bubblewrap`、`seatbelt`、`windows_elevated`、`windows_unelevated` 或空
- `sandbox_strength`: `strong`、`reduced` 或空
- `sandbox_available`
- 沙箱不可用原因
- 最终 rule decision、匹配的 pattern 和 justification

审计和 UI 必须清楚区分“沙箱内执行”与“已批准的宿主机执行”。现有输出脱敏继续作用于两种模式。

## 11. 实现边界

增加一个小的下层 command executor，职责仅限：

- 接收 Guard 已授权的 `sandboxed` 或 `host` 执行计划。
- 在 `sandboxed` 模式下探测并调用当前平台的 sandbox backend。
- 在 `host` 模式下使用现有的直接进程执行路径。
- 保持现有 stdin、stdout、stderr、timeout、取消和输出限制语义。

下层执行器不读取 rules，不发起审批，也不能自行从 `sandboxed` 降级为 `host`。平台 backend 只负责把固定边界转换成系统调用参数。

Guard 不实现进程执行细节。`shell_runner.go` 负责构造规范化请求和呈现结果，但所有请求必须通过 Guard，不能直接调用下层执行器。现有 timeout、stdout/stderr、退出码和流式事件代码应下移或复用，不能在 sandbox 与 host 分支复制。

不要为 Bash、PowerShell 分别增加薄包装，也不要把 Guard 扩展成通用 policy engine 或 backend registry。平台选择是编译期事实，不需要插件抽象。

## 12. 验收标准

- 普通 Bash 和 PowerShell 命令默认在当前可用的平台沙箱中执行。
- 沙箱内只能修改允许写入的目录。
- 沙箱内不能读取隐藏的认证与 Guard 状态。
- 沙箱内默认不能访问网络。
- 沙箱内命令不触发逐条审批。
- `require_escalated` 必须经过 Guard 审批。
- `allow` rule 可以授权匹配的宿主机命令，不产生逐次审批。
- `prompt` rule 和未匹配命令必须逐次审批。
- `forbidden` rule 不能通过临时审批绕过。
- 多条匹配规则采用最严格结果。
- 复杂或解析失败的 Shell 脚本不能意外命中内部命令的 `allow` rule。
- 批准只允许 action hash 对应的命令执行一次。
- 沙箱失败不会自动转为宿主机执行。
- 当前平台 backend 缺失或不可用时，Morph 可以启动并明确警告。
- 沙箱不可用时，默认 Shell 调用失败关闭；经批准的宿主机调用仍可执行。
- WSL2 与 Linux 使用同一 Bubblewrap 行为。
- macOS 和原生 Windows 不会收到错误平台的安装提示。
- Windows 弱隔离在状态和审计中明确标记。
- Morph 以 root 运行时拒绝宿主机越界执行。
- timeout、输出截断、流式事件和脱敏行为保持不变。

## 13. 实现清单

- [ ] 定义跨平台一致的 sandbox plan、availability 和 execution result。
- [ ] 让 Shell 工具只通过 Guard 请求本地命令执行，禁止绕过 Guard 调用下层执行器。
- [ ] 为 Guard 到下层执行器的授权边界添加回归测试。
- [ ] 增加 Linux Bubblewrap 可用性探测和固定 sandbox plan。
- [ ] 为 sandbox plan 添加文件系统、网络和环境边界测试。
- [ ] 在 Shell 工具 schema 中增加 `sandbox_permissions` 和 `justification`。
- [ ] 先为默认沙箱、越界审批、缺少 Bubblewrap 和禁止自动降级添加回归测试。
- [ ] 增加 `guard.rules` 配置解析、校验和前缀匹配测试。
- [ ] 增加多规则最严格决策、无匹配默认 prompt 和 Guard 关闭时拒绝越界的测试。
- [ ] 增加简单组合命令拆分及复杂脚本不能自动 allow 的测试。
- [ ] 让 Bash 和 PowerShell 共用新的执行模式选择。
- [ ] 修改 Guard：只对 `require_escalated` 应用 command rules 和审批。
- [ ] 将执行模式纳入 action hash 和审计事件。
- [ ] 在 TUI、Console 和 Channel 审批内容中显示宿主机执行风险。
- [ ] 更新 `assets/config/config.example.yaml`、`docs/security.md` 和工具文档。
- [ ] 在 Linux 和 WSL2 上验证 Bubblewrap 可用、缺失及 user namespace 被禁用三种环境。
- [ ] 评估 ACP `terminal/create` 接入，不与第一版 Shell 实现绑定。
- [ ] 单独设计并实现 macOS Seatbelt backend。
- [ ] 单独设计并实现 Windows 强隔离和弱隔离 backend。
