# Mister Morph

面向本地或多渠道 Agent 的桌面 App、CLI，以及可复用的 Go 运行时。

其他语言：[English](../../README.md) | [日本語](../ja-JP/README.md)

如果你只是想先体验 Mister Morph，最简单的入口已经是桌面 App。它会自带 Console UI、自动启动本地后端，并在首次启动时引导完成配置。

## 为什么选择 Mister Morph

- 🖥️ App 优先的上手路径：桌面 App 去掉了过去那种多终端启动流程，但 CLI 仍然保留，适合脚本和自动化。
- 🧩 可复用的 Go 核心：既可以把 Mister Morph 当桌面 App、CLI、Console 后端来运行，也可以嵌入到其他 Go 项目里。
- 🤝 Connection：基于 [Aqua](https://mistermorph.com/aqua)，多个 agent 可以彼此对话，一起规划和协作。
- 🛠️ 实用的扩展模型：内置工具、`SKILL.md` 技能系统，以及 Go 嵌入层，覆盖本地使用、自动化和集成。
- 🔒 从设计上考虑安全：auth profiles、出站策略、审批与脱敏都属于运行时模型的一部分。

## 快速开始

### 桌面 App（推荐）

1. 从 [GitHub Releases](https://github.com/quailyquaily/mistermorph/releases) 页面下载对应平台的安装包：
   - macOS: `mistermorph-desktop-darwin-arm64.dmg`
   - Linux: `mistermorph-desktop-linux-amd64.AppImage` 或 `mistermorph-desktop-linux-amd64.deb`
   - Windows: `mistermorph-desktop-windows-amd64.zip`
2. 启动 App。
3. 在 App 内完成首次配置。
4. 直接使用 Console UI，无需再手动运行 `mistermorph console serve`。

构建、打包与平台说明见：[../app.md](../app.md)

### CLI

先安装 CLI：

```bash
curl -fsSL -o /tmp/install-mistermorph.sh https://raw.githubusercontent.com/quailyquaily/mistermorph/refs/heads/master/scripts/install-release.sh
sudo bash /tmp/install-mistermorph.sh
```

或者从源码安装：

```bash
go install github.com/quailyquaily/mistermorph/cmd/mistermorph@latest
```

初始化工作目录、设置 API Key，并运行一个任务：

```bash
mistermorph install
export MISTER_MORPH_LLM_API_KEY="YOUR_API_KEY"
mistermorph run --task "Hello!"
```

如果当前还没有 `config.yaml`，`mistermorph install` 会启动初始化向导并写入所需的工作区文件。

CLI 模式与配置说明见：[../modes.md](../modes.md)、[../configuration.md](../configuration.md)

## Mister Morph 包含什么

- 桌面 App：适合本地使用，带首次配置流程与内置 Console UI。
- CLI：适合单次任务、脚本调用、自动化与服务模式。
- 本地 Console 服务：提供浏览器中的设置、运行态管理与监控界面。
- 渠道运行时：Telegram、Slack、LINE、Lark。
- 可嵌入的 Go 集成层：方便在其他项目中复用 Mister Morph。
- 内置工具与基于 `SKILL.md` 的技能系统。
- 面向真实运行环境的安全控制：auth profiles、出站策略、审批与脱敏。

## 文档导航

入门：

- [桌面 App](../app.md)
- [运行模式](../modes.md)
- [配置](../configuration.md)
- [故障排查](../troubleshoots.md)

参考：

- [Console](../console.md)
- [Aqua 通讯](../aqua.md)
- [Tools](../tools.md)
- [Skills](../skills.md)
- [Security](../security.md)
- [Integration](../integration.md)
- [Architecture](../arch.md)

渠道接入：

- [Telegram](../telegram.md)
- [Slack](../slack.md)
- [LINE](../line.md)
- [Lark](../lark.md)
- [Mixin Messenger](../mixin.md)

完整文档索引：[../README.md](../README.md)

## 开发

常用本地命令：

```bash
./scripts/build-backend.sh --output ./bin/mistermorph
./scripts/build-desktop.sh --release
go test ./...
```

Console 前端位于 `web/console/`，使用 `pnpm`。本地构建细节见：[../console.md](../console.md) 与 [../app.md](../app.md)。

## 配置模板

规范配置模板在 [../../assets/config/config.example.yaml](../../assets/config/config.example.yaml)。
环境变量统一使用 `MISTER_MORPH_` 前缀。完整配置说明与常用 flags 见 [../configuration.md](../configuration.md)。
