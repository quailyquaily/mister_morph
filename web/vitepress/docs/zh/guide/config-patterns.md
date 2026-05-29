---
title: 配置模式
description: 安装方式、基础配置，以及常用的 profiles、routes 与工具策略配置方法。
---

# 配置模式

## 安装方式

```bash
# 发布版安装脚本
curl -fsSL -o /tmp/install-mistermorph.sh https://raw.githubusercontent.com/quailyquaily/mistermorph/refs/heads/master/scripts/install-release.sh
sudo bash /tmp/install-mistermorph.sh
```

```bash
# Go 安装
go install github.com/quailyquaily/mistermorph/cmd/mistermorph@latest
```

## 初始化文件

```bash
mistermorph install
```

默认情况下，状态目录是 `~/.morph/`，缓存目录是 `~/.cache/morph`。

`workspace_dir`、`file_cache_dir`、`file_state_dir` 的区别，见 [文件系统根目录](/zh/guide/filesystem-roots)。

## 配置来源优先级

- CLI flags
- 环境变量
- `config.yaml`

## 最小 `config.yaml`

```yaml
llm:
  inference_provider: openai
  model: gpt-5.4
  api_key: ${OPENAI_API_KEY}
```

## LLM Profiles 与 Routes

```yaml
llm:
  model: gpt-5.4
  profiles:
    cheap:
      model: gpt-4.1-mini
    reasoning:
      inference_provider: xai
      model: grok-4.1-fast-reasoning
  routes:
    main_loop:
      candidates:
        - profile: default
          weight: 1
        - profile: cheap
          weight: 1
      fallback_profiles: [reasoning]
    addressing:
      profile: cheap
    awareness: cheap
    think: reasoning
```

## 工具开关

```yaml
tools:
  bash:
    enabled: false
  url_fetch:
    enabled: true
    timeout: "30s"
```

## 运行上限

```yaml
max_steps: 20
tool_repeat_limit: 4
```

完整键定义以 `assets/config/config.example.yaml` 为准。
