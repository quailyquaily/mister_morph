---
date: 2026-03-29
title: Console LLM Profile Connection Test 设计稿
status: implemented
updated: 2026-08-05
---

# Console LLM Profile Connection Test 设计稿

## 1) 结论

给 Console Settings 里的每个 LLM profile 增加测速按钮是合理的，前端 UI 改动不大。

真正需要收敛的是测速 API 的语义。

改动前，`/api/settings/agent/test` 的语义是：

- 测“当前默认 LLM 草稿”
- 支持未保存草稿先测速
- 不解析 `llm.profiles`

如果要扩展到“每个 profile 都可以测”，推荐做法不是重写接口，而是在现有接口上做兼容扩展：

- 保留当前默认测速请求不变
- 新增可选 `target_profile`
- Settings 页面测速 profile 时只发送目标 profile 的当前草稿
- 后端只解析目标 profile，不读取顶层默认 LLM 草稿
- 复用 runtime 的独立 profile 解析语义

结论上，这个需求的实现范围是中等，不是大改。

---

## 2) 背景

设计时，Settings 页面已经支持：

- 编辑默认 LLM 配置
- 编辑多个 `llm.profiles`
- 给默认 LLM 做连接测速

但当时 profile 还不能单独测速。

命名 profile 是完整独立配置：

- provider、endpoint、凭据、model 和运行参数都来自 profile 自身
- 空字段不会回退到顶层默认 LLM
- provider 自带的 endpoint 或 model 默认值仍然有效

这意味着“测某个 profile”就是测：

> 目标 profile 自身解析后的最终配置

---

## 3) 改动前实现

### 3.1 前端

改动前，Settings 页面的测速按钮只接在默认 LLM 表单上。

当时的实现特点：

- `buildLLMTestPayload()` 只序列化顶层默认 LLM 草稿
- `runConnectionTest()` 直接向 `/api/settings/agent/test` 发送 `{ llm: ... }`
- `LLMConfigForm` 组件本身已经支持测试按钮能力，但 profile 卡片没有接入

这意味着 UI 侧缺的不是组件能力，而是 profile 测速的状态与请求组装。

### 3.2 后端

改动前，`/api/settings/agent/test` 只接收：

```json
{
  "llm": {
    "provider": "openai",
    "endpoint": "https://api.openai.com",
    "api_key": "sk-...",
    "model": "gpt-5.2"
  }
}
```

当时的处理逻辑是：

1. 从当前 runtime 读取默认 LLM 配置
2. 把请求里的非空顶层 `llm` 字段覆盖上去
3. 构造一个 main-loop client
4. 跑 text / json / tool-calling 三项 benchmark

当时不会做的事情：

- 不读取 `req.llm.profiles`
- 不根据某个 profile 名称去解析 profile
- 不解析独立的目标 profile 草稿

所以当时的接口语义很明确：

> 它测的是默认 LLM 草稿，不是 profile。

---

## 4) 为什么只传 `profile name` 不够

一个直觉方案是：

- 前端发现当前点的是某个 profile
- 只把 `profile name` 传给后端

这个方案不完整。

原因有四个：

1. 未保存草稿会丢失

当前默认测速支持“先改表单，再测速，不必先保存”。

如果 profile 只传 `name`，后端只能去当前 runtime 配置里找这个 profile，那么测到的是已保存版本，不是用户眼前正在编辑的版本。

2. 新建 profile 时没有稳定已保存状态

一个新 profile 在保存前可能已经填了一半字段并希望先测速。

只传 `name` 的话，后端运行时里根本没有这条 profile。

3. env-managed secret 无法只靠名字补全

profile 里的 `api_key` / `cloudflare_api_token` 这类字段，有一部分场景是 env-managed。

前端不会持有真实 secret，只会持有占位形式或受限视图。只传 `name` 无法表达“当前这次未保存草稿到底想测哪一个 secret 引用”。

所以 `profile name` 应该传，但它只能解决：

- 测的是哪一个 profile

它解决不了：

- 测的是这个 profile 的哪个草稿版本

正确方式是：

- `name` 只传一次，例如 `target_profile`
- 同时发送目标 profile 的当前草稿

这样后端能同时知道：

- 当前目标 profile 是哪一个
- 当前 profile 的所有配置字段
- 当前 profile 是否写了 `${ENV_NAME}` 形式的占位值

---

## 5) 设计目标

这次扩展的目标应当保持窄一些。

要做到：

- Settings 页面默认 LLM 与每个 profile 都可以测速
- 保持“未保存也能先测速”的现有体验
- profile 测速的独立解析语义与 runtime 一致
- Setup 页面现有默认测速能力不受影响
- 默认测速旧请求继续兼容

明确不做：

- 不在这次需求里重做 benchmark 内容
- 不在这次需求里做 fallback chain benchmark
- 不把“测速一个 profile”和“模拟 route failover”混成一个接口

这次需求只测“单一目标配置”的可连通性与基础能力。

---

## 6) 推荐请求契约

推荐保留现有 `/api/settings/agent/test`，只扩展请求体。

推荐形态：

```json
{
  "llm": {
    "profiles": [
      {
        "name": "cheap",
        "inference_provider": "openai",
        "model": "gpt-4.1-mini",
        "api_key": "${OPENAI_API_KEY_PROFILE_A}"
      }
    ]
  },
  "target_profile": "cheap"
}
```

规则如下：

- `target_profile` 为空或缺失：
  - 走默认 LLM 测速语义
  - 与当前接口完全一致

- `target_profile` 非空：
  - 表示测速目标是该 profile

- `llm` 在 Settings profile 测速场景下只需要包含目标 profile 草稿
  - `profiles` 中可以只放当前目标
  - 顶层默认 LLM 字段不会参与目标 profile 解析
  - env-managed 字段应保留 `${ENV_NAME}` 原文

这个形态足够表达：

- 默认测速
- 已保存 profile 测速
- 未保存 profile 草稿测速
- profile 与顶层未保存草稿相互隔离

同时不会破坏 Setup 页现有调用。

兼容性约束：

- 旧请求 `{ "llm": { ... } }` 继续可用
- Settings profile 测速必须发送目标 profile 的完整草稿字段

---

## 7) 默认 LLM 与 Profile 的独立解析

后端建议按下面的顺序解析。

### 7.0 ASCII 流程图

```text
Client: SettingsView
    |
    | POST /api/settings/agent/test
    v
+----------------------------------+
| handleAgentSettingsTest          |
| decode request                   |
+----------------------------------+
    |
    v
+----------------------------------+
| select submitted test target     |
| - default: top-level llm draft   |
| - profile: named profile draft   |
+----------------------------------+
    |
    v
+----------------------------------+
| target_profile is empty ?        |
+----------------------------------+
    | yes                           | no
    |                               |
    v                               v
+---------------------------+   +----------------------------------+
| resolve default test      |   | resolve profile test             |
| target                    |   | target                           |
+---------------------------+   +----------------------------------+
    |                               |
    | use top-level default llm     | 1. find target profile in
    | from submitted draft          |    request.llm.profiles
    |                               | 2. resolve only that profile
    |                               |
    v                               v
+----------------------------------------------+
| resolve env refs in effective test config    |
| - plain value: use as-is                     |
| - ${ENV_NAME}: lookup from server env        |
+----------------------------------------------+
    |
    v
+----------------------------------+
| build route/client               |
| main_loop semantics only         |
+----------------------------------+
    |
    v
+----------------------------------+
| run benchmarks                   |
| - text_reply                     |
| - json_response                  |
| - tool_calling                   |
+----------------------------------+
    |
    v
+----------------------------------+
| return provider/model/benchmarks |
+----------------------------------+
```

再展开成“默认测速”和“profile 测速”两条子图如下。

```text
Default Test
============

request.llm
    |
    v
working llm snapshot
    |
    | use top-level default llm
    v
effective default test config
    |
    | resolve ${ENV_NAME}
    v
benchmarks
```

```text
Profile Test
============

request.llm.profiles -------------------------------+
                                                    |
request.target_profile ---------------------------->|
                                                    v
                                find target profile draft
                                                    |
                                                    v
                                resolve profile fields and ${ENV_NAME}
                                                    |
                                                    v
                                                benchmarks
```

### 7.1 选择测试目标

后端先根据 `target_profile` 选择测试目标。

1. 对旧兼容请求：
   - 先读取当前 runtime default llm
   - 再应用请求里的顶层 `llm` 非空字段

2. 对 Settings profile 测速请求：
   - 在 `request.llm.profiles` 中找到目标 profile
   - 只解析该 profile 的字段
   - 不解析请求或 runtime 中的顶层默认 LLM 字段

### 7.2 默认测速

当 `target_profile` 为空时：

1. 使用请求中的顶层默认 LLM 草稿
2. 得到默认测速的最终配置

这保持当前行为不变。

### 7.3 Profile 测速

当 `target_profile` 非空时：

1. 在请求的 `llm.profiles` 里找到该 profile
2. 解析该 profile 自己的 provider、endpoint、凭据、model 和运行参数
3. 得到 profile 测速的最终配置

这里的关键点是：

- 顶层默认 LLM 的未保存草稿不会影响 profile 测速结果
- 但测速不会写回配置文件，也不会修改 runtime 已保存配置

这里复用 runtime 的独立 profile resolver，避免测速与真实 route 使用不同语义。

---

## 8) env-managed 字段的测试语义

这是这次设计里真正需要明确写下来的复杂点。

### 8.1 当前问题

Settings 表单里有一部分字段可能是 env-managed 的。

尤其是：

- `api_key`
- `cloudflare_api_token`

前端不会持有真实 secret。

所以 profile 测速如果想支持下面这类场景：

- 当前 profile 已保存为 `${OPENAI_API_KEY_PROFILE_A}`
- 或者用户正在编辑一个新的 `${SOME_ENV_NAME}`

那么测速接口不能假设前端会直接把真实 secret 发回来。

### 8.2 推荐规则

测速请求里的字段允许出现两类值：

1. 真实值
2. `${ENV_NAME}` 形式的占位值

对于 `${ENV_NAME}`：

- 后端在测速解析阶段按与配置文件一致的语法解析
- 在服务端进程环境里查找该变量
- 找到则使用解析后的值参与测速
- 找不到则按明确错误返回，而不是把 `${ENV_NAME}` 当成字面量 secret 去请求上游

这样可以同时覆盖：

- 已保存到 YAML 的 env ref
- 当前还没保存、但用户在表单里刚输入的 env ref 草稿

这里有一个实现约束需要明确：

- Settings 的测速 payload 不能继续沿用当前“只发顶层非空字段”的最小化 builder
- profile 测速也不应直接复用 save payload
- 应该新增一个专门的 test snapshot builder
  - 目标 profile 保留 `${ENV_NAME}` 原文
  - 不要求前端持有真实 secret

### 8.3 安全边界

这只影响测速请求在服务端的解析行为。

它不改变当前 GET settings 接口对 secret 的显示策略：

- 前端仍然不回显真实 secret
- 服务端只在测速时把 `${ENV}` 解析成真实值

---

## 9) fallback profile 的处理边界

这次 profile 测速不应该顺带去测 fallback chain。

原因很简单：

- 用户点的是“测试这个 profile”
- 不是“模拟这个 profile 失败后整个 failover 链路还能不能跑”

因此这次测速只针对一个最终目标配置。

profile 测速不需要发送 `llm.fallback_profiles`；兼容客户端即使发送，后端也忽略它。

如果以后要做 fallback benchmark，应当是一个单独需求。

---

## 10) 前端交互建议

Settings 页面建议保持当前测速交互，只做扩展：

- 默认 LLM 表单继续保留测速按钮
- 每个 profile 卡片也显示测速按钮
- profile 名称为空时，测速按钮禁用
- 测速弹窗标题明确显示当前目标
  - 例如：
    - `Default LLM`
    - `Profile: cheap`

前端请求组装建议：

- 默认测速：
  - 发送当前顶层默认 LLM 草稿
  - 不传 `target_profile`

- profile 测速：
  - 发送当前目标 profile 的完整草稿
  - 发送当前 profile 名称到 `target_profile`

注意：

- 前端不要把 profile 与顶层默认 LLM 合成
- 前端只负责发送目标 profile 草稿和 profile 名称
- 顶层默认 LLM 的未保存草稿不参与 profile 测速
- 但测速本身不应写回配置
- 最终解析应该由后端完成

这样能保证与 runtime 行为一致。

---

## 11) 向后兼容

兼容策略应当非常直接：

- 旧请求 `{ "llm": { ... } }` 继续可用
- Setup 页面无需同步改造
- 只有 Settings 里的 profile 测速会使用 `target_profile`

这意味着这次改动的后端风险主要集中在：

- 新增 profile 测速分支
- 不应影响旧的默认测速分支

---

## 12) 测试建议

后端至少补这些测试：

1. 旧默认测速请求继续工作

- 只传 `llm`
- 行为与当前一致

2. 新版默认测速请求可使用顶层草稿

- 传顶层 `llm` 草稿
- 不传 `target_profile`
- 使用该草稿做默认 LLM 测速

3. profile 不读取默认字段

- `target_profile` 指向某个 profile
- 顶层与 profile 配置不同的 provider / endpoint / secret
- 最终测速只使用 profile 自己的字段

4. profile 选择自己的 provider

- 默认是 `openai`
- profile 改成 `cloudflare`
- 最终测速应走 cloudflare 凭据解析

5. 顶层未保存草稿不影响 profile

- 顶层默认 `endpoint` 或 `api_key` 在当前 snapshot 中被修改
- target profile 使用自己的 endpoint 和 API key
- 最终测速结果不应变化

6. 新建未保存 profile

- runtime 中不存在同名 profile
- request snapshot 中存在该 profile
- request 中提供 `target_profile`
- 仍然能够测速

7. `${ENV_NAME}` 占位值解析

- 请求里出现 `${OPENAI_API_KEY}`
- 服务端环境存在该变量
- 应解析后成功参与测速

8. `${ENV_NAME}` 缺失时报错

- 请求里出现 `${MISSING_KEY}`
- 服务端未设置该变量
- 应返回清晰错误

9. 非目标 profile 不应阻塞当前测速

- request snapshot 中别的 profile 有无效配置
- 当前 `target_profile` 本身有效
- 本次测速只应解析目标 profile

前端至少验证：

- 默认 LLM 按现有方式可测速
- 每个 profile 都有测速入口
- profile 名称为空时禁用测速
- profile 未保存修改会参与测速
- profile 内 `${ENV_NAME}` 原文会被保留到测试请求

---

## 13) 预计改动范围

如果按本文方案实现，预计触达范围是中等：

- Web:
  - Settings 页新增 test snapshot builder
  - profile 卡片接入测速入口
  - 测速弹窗文案补充目标信息

- Go:
  - 扩展 `agentSettingsTestRequest`
  - 增加 `target_profile` 解析逻辑
  - 增加默认目标与 profile 目标的独立解析逻辑
  - 增加 `${ENV_NAME}` 测试态解析逻辑
  - 补相关单测

明确不是大面积重构。

---

## 14) 冻结建议

实现前建议先冻结下面这四个决策：

1. profile 测速必须保持“未保存也能先测速”
2. `target_profile` 是唯一 selector，profile 测速只发送目标 profile 草稿
3. 只解析当前 target profile，不校验默认 LLM 或无关 profile
4. fallback chain benchmark 不属于这次需求

只要这四条冻结，后续实现就不会再来回改接口语义。
