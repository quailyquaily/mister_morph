---
date: 2026-02-06
title: MAEP Proactive Contact Sharing (Draft)
status: in_progress
---

# MAEP 主动分享与联系人认知机制（需求理解稿）

> Implementation note (2026-02-07):
> 主动分享业务实现已开始，并按“MAEP 仅做通信层、contacts 独立业务层”落地。
> 当前代码位于 `contacts/` 与 `cmd/mistermorph/contactscmd/`，不侵入 `maep` 领域存储。

## 1) 目标与边界
本功能是 MAEP 之上的“主动分享策略层”，目标是让 agent 表现出类人的分享行为：
- 周期性主动触达联系人。
- 基于偏好交集选择要分享的内容。
- 默认最近信息优先，仅在显式关联时补历史上下文。
- 根据会话反馈实时调节分享欲望。
- 会话后持续更新联系人偏好与认知深度。
- 始终维护最多 150 个活跃联系人，其余进入冷存储。

说明：
- 传输继续复用 MAEP `agent.data.push`。
- 这一层不改动 MAEP 加密/握手协议。
- 前置依赖见 `docs/feat/feat_20260206_internal_fsstore.md`（共享 FS 存储层）。

## 2) 执行归属标记
定义：
- `RULE`：确定性规则与工程逻辑。
- `LLM`：由提示词驱动的语义判断。
- `HYBRID`：LLM 给特征，RULE 做最终决策。

| 子任务 | 归属 | 说明 |
|---|---|---|
| HEARTBEAT 触发周期任务 | RULE | 定时、抖动、重试、限频 |
| 可通信联系人过滤（trust/cooldown） | RULE | 硬门控，不交给 LLM |
| 提取联系人偏好信号 | LLM | 从历史对话和记忆摘要抽取 topic 偏好与 persona 线索 |
| 候选内容语义交集评估 | LLM | 计算语义重叠、历史关联建议 |
| 最终打分排序与 top-K 选择 | RULE | 按固定公式、固定权重 |
| 72h 最近优先与历史附带规则 | RULE | 硬约束 |
| 会话文案生成 | LLM | 风格和表达交给模型 |
| 会话内兴趣变化判断 | LLM | 从对方回复识别兴趣信号 |
| 会话停止条件判定 | RULE | interest 阈值 + N 轮上限 |
| profile 更新落盘 | RULE | LLM 只给 delta 建议，不直接写入 |
| 150 活跃上限与淘汰 | RULE | 硬约束 |
| 审计日志落盘 | RULE | 可追溯 |

## 3) 需求映射（含标记）

### 3.1 周期性主动分享
- `RULE` 使用 `HEARTBEAT.md` 触发。
- `RULE` 扫描活跃 150 联系人，挑选 1~N 个发起本轮分享。

### 3.2 偏好交集分享
- `LLM` 维护 contact 偏好向量（topics/tags）与 persona 画像（brief/traits）。
- `LLM` 对候选内容生成语义重叠特征。
- `RULE` 使用固定公式计算最终分数并选择。

### 3.3 最近优先 + 关联历史
- `RULE` 默认仅取最近 72h 候选。
- `LLM` 可建议“显式关联”的历史项。
- `RULE` 只允许附带被判定为显式关联的最小历史集。

### 3.4 会话内热度调节
- `LLM` 每轮输出兴趣信号。
- `RULE` 更新 `session_interest_level`。
- `RULE` 触发停止条件：低兴趣连续出现或达到 `N` 轮上限。

### 3.5 反馈更新偏好
- `LLM` 输出 `positive/neutral/negative` 与 topic 偏好变化建议。
- `RULE` 用固定更新公式写入 `contact_profiles`。
- `RULE` 会话内容整理进入 Memory，并且必须携带 `contact_id + contact_nickname`。
- Memory 写入的 `SessionID` 采用通道稳定键：
  - Telegram：`tg:<chat_id>`
  - MAEP：`maep:<peer_id>`（同一天同 peer 聚合到同一 memory 文件）

### 3.6 认知深度增减
- `RULE` 高频互动时提升深度，低互动按遗忘曲线衰减。
- `RULE` 深度直接影响“可分享深度上限”和“主动触达优先级”。

### 3.7 活跃 150 上限
- `RULE` 固定 `active_contact_profiles <= 150`。
- `RULE` 超限后按保留分排序淘汰到冷存储。
- `RULE` 冷存储仅在对方主动联系或分数回升时再激活。

## 4) 决策公式与策略（固定）

### 4.1 硬门控（先过滤）
任一条件不满足直接跳过，不进入 LLM：
- agent 联系人：`peer_id` 必须非空，且 `trust_state` 若存在必须为 `verified`。
- human 联系人：受 `contacts.human.*` 配置门控，不使用 MAEP trust_state 作为硬门槛。
- `now >= cooldown_until`。
- 联系人必须在 `active_contact_profiles` 中。

### 4.2 候选内容打分
记：
- `overlap_semantic ∈ [0,1]`：LLM 输出。
- `age_hours`：内容距当前小时数。
- `freshness_bias = exp(-age_hours / 24)`，并在 `age_hours > 72` 时先置为 0。
- `depth_fit ∈ [0,1]`：内容深度与 contact 深度匹配度（RULE 计算）。
- `reciprocity_norm ∈ [0,1]`：近 30 天互惠度（RULE 计算）。
- `novelty ∈ [0,1]`：与最近已分享内容的差异度（RULE 计算）。
- `sensitivity_penalty`：若 `trust_state=tofu` 且内容敏感度高，则惩罚 0.25，否则 0（保留项；当前 agent 门控下通常不触发）。

候选分：
- `candidate_score = 0.40*overlap_semantic + 0.25*freshness_bias + 0.15*depth_fit + 0.10*reciprocity_norm + 0.10*novelty - sensitivity_penalty`
- 最终 `candidate_score` 夹紧到 `[0,1]`。

### 4.3 联系人分与选择
- `contact_score = max(candidate_score_i)`。
- 每轮选择 `top-K` 联系人，`K = min(config.max_targets_per_tick, eligible_contacts)`。
- 每个联系人只发 1 个主候选。

### 4.4 历史上下文附带策略
- 默认不带历史。
- 仅当 LLM 输出 `explicit_history_links` 且置信度 `>= 0.7` 时才允许附带。
- 最多附带 `config.max_linked_history_items`（建议 2）。

### 4.5 会话兴趣更新
- 初始 `interest_0 = 0.6`。
- LLM 每轮输出：`signal_positive`、`signal_negative`、`signal_bored`，范围 `[0,1]`。
- 更新公式：
  - `interest_{t+1} = clip(interest_t + 0.35*signal_positive - 0.30*signal_negative - 0.10*signal_bored, 0, 1)`

停止规则（任一满足即结束本会话）：
- 轮数达到 `N`（配置项）。
- `interest < 0.30` 连续 2 轮。
- LLM 建议 `next_action=wrap_up` 且 `confidence >= 0.7`。
- 对方明确结束会话。

### 4.6 联系人认知更新
记：
- `alpha_topic = 0.20`
- `target_topic ∈ [0,1]`：由 LLM 偏好提取输出（`topic_affinity`）。

topic 权重更新：
- `w_new = clip((1-alpha_topic)*w_old + alpha_topic*target_topic, 0, 1)`
- 更新后执行归一化：canonical key、删除 `< 0.08`、按分值排序并仅保留 top-16。

persona traits 更新（按同一 `alpha_topic` 融合）：
- `trait_new = clip((1-alpha_topic)*trait_old + alpha_topic*trait_suggested, 0, 1)`
- `persona_brief` 仅在以下条件同时满足时覆盖：新摘要非空、`confidence` 达阈值、`persona_traits` 非空、且与旧值不同。

认知深度更新：
- 会话后即时：`depth = clip(depth + 8*engagement_quality - 3*negative_signal, 0, 100)`
- 每日衰减：`depth = max(0, depth - 0.4*idle_days)`

### 4.7 150 活跃上限与淘汰
定义归一化分：
- `depth_norm ∈ [0,1]`
- `recency_norm ∈ [0,1]`
- `reciprocity_norm ∈ [0,1]`

保留分：
- `retain_score = 0.45*depth_norm + 0.35*recency_norm + 0.20*reciprocity_norm`

策略：
- 按 `retain_score` 排序保留前 150 为活跃，其余转冷存储。
- 冷存储激活条件：
  - 对方主动来信，或
  - `retain_score` 超过活跃尾部分 + `0.05`（hysteresis）。

## 5) Pseudo Prompt 与 JSON 契约

### 5.1 Prompt A: 提取联系人偏好信号（LLM）
用途：从最近会话 + memory 摘要中抽取 topic 偏好与 persona 摘要。

Pseudo Prompt:
```text
System:
You are a feature extractor. Return valid JSON only.
No prose, no markdown.

User:
Given contact history and memory summaries, extract:
1) topic affinity
2) persona brief and traits
3) confidence

Input:
- contact_profile_snapshot: {{...}}
- recent_dialogue_snippets: {{...}}
- memory_summaries: {{...}}

Constraints:
- Scores must be in [0,1].
- Keep top 12 topics.
- If uncertain, lower confidence.
```

JSON 输出：
```json
{
  "topic_affinity": [
    {"topic": "maep", "score": 0.82, "evidence": ["...", "..."]}
  ],
  "persona_brief": "情感细腻，表达克制，偶尔忧郁",
  "persona_traits": {
    "warm": 0.72,
    "sensitive": 0.81,
    "melancholic": 0.44
  },
  "confidence": 0.79
}
```

### 5.2 Prompt B: 候选语义交集与历史关联（LLM）
用途：给每个候选打语义重叠分，并识别是否显式关联历史。

Pseudo Prompt:
```text
System:
You rank candidate items for one contact.
Return JSON only.

User:
For each candidate item, output semantic overlap score [0,1],
optional explicit history links, and confidence.

Input:
- contact_profile: {{...}}
- candidates: {{...}}
- recent_sent_items: {{...}}

Constraints:
- explicit_history_links must be empty unless linkage is explicit.
- Max 2 links per candidate.
```

JSON 输出：
```json
{
  "ranked_candidates": [
    {
      "item_id": "mem_123",
      "overlap_semantic": 0.86,
      "explicit_history_links": ["mem_044"],
      "confidence": 0.81
    }
  ]
}
```

### 5.3 Prompt C: 会话反馈分类（LLM）
用途：识别本轮回复中的兴趣信号和下一动作建议。

Pseudo Prompt:
```text
System:
Classify conversational feedback into numeric signals.
Return JSON only.

User:
Given last turns, output positive/negative/bored signals and next action.

Input:
- recent_turns: {{...}}
- current_session_state: {{...}}

Allowed next_action: continue | wrap_up | switch_topic
```

JSON 输出：
```json
{
  "signal_positive": 0.22,
  "signal_negative": 0.58,
  "signal_bored": 0.67,
  "next_action": "wrap_up",
  "confidence": 0.84
}
```

### 5.4 Prompt D: Profile 增量建议（LLM）
用途：根据整场会话输出偏好更新建议（不直接落盘）。

Pseudo Prompt:
```text
System:
Propose profile deltas, not final state.
Return JSON only.

User:
Given session summary and old profile, propose deltas for:
- topics
- persona
- depth
- cooldown

Input:
- old_profile: {{...}}
- session_summary: {{...}}
```

JSON 输出：
```json
{
  "topic_weight_deltas": [
    {"topic": "distributed-systems", "delta": 0.14}
  ],
  "persona_delta": {
    "persona_brief": "偏理性、强调事实，偶尔会自嘲",
    "persona_traits": {
      "analytical": 0.77,
      "humorous": 0.31
    }
  },
  "depth_delta_suggested": 2.4,
  "cooldown_hours_suggested": 0,
  "confidence": 0.77
}
```

### 5.5 JSON 校验与降级策略（RULE）
- 必须严格 JSON 解析。
- 数值越界立即夹紧或丢弃字段。
- 缺字段时使用默认值，不中断流程。
- LLM 输出仅作为“建议特征”，最终写入必须经过 RULE 公式。
- 每次 LLM 输出与最终决策都写审计日志。

## 6) 数据实体与存储建议
建议新增文件：
- `active.md`
- `inactive.md`
- `share_sessions.json`
- `share_candidates.json`
- `share_decisions_audit.jsonl`

说明（当前实现）：
- 联系人画像在逻辑上等价于 `contact_profiles`，物理上拆分为：
  - `active.md`：活跃联系人画像。
  - `inactive.md`：非活跃/冷联系人画像。
- `active.md` / `inactive.md` 采用 Markdown 列表，每个 item 是 JSON 对象，便于人工排查与脚本读取。

核心实体：
- `ContactProfile`: `contact_id`, `contact_nickname`, `persona_brief`, `persona_traits`, `kind`, `subject_id`, `node_id`, `peer_id`, `understanding_depth`, `topic_weights`, `cooldown_until`, `retain_score`
- `ShareCandidate`: `item_id`, `topic`, `topics`, `content_type`, `payload_base64`, `sensitivity_level`, `linked_history_ids`, `source_chat_id`, `source_chat_type`, `source_ref`, `created_at`
- `SessionState`: `session_id`, `contact_id`, `session_interest_level`, `turn_count`, `started_at`, `ended_at`

联系人 identity / 昵称规则（冻结）：
- 每个 profile 必须有 `contact_id`，并允许 `contact_nickname` 为空。
- 人类（Telegram 来源）：
  - `contact_id` 优先使用 Telegram `username`（建议规范化为 `tg:@<username>`）。
  - `contact_nickname` 初始使用 Telegram 昵称（display name / first+last name）。
- agent：
  - `contact_id` 使用 `node_id`（例如 `maep:<peer_id>`）。
  - `contact_nickname` 初始可为空。
- 其他没有昵称的人类：
  - `contact_id` 使用业务层 subject id。
  - `contact_nickname` 初始为空。
- `contact_nickname` 可由系统在理解加深后逐步补全或更新（需写审计）。
- 当前实现尚未落地“对方自报昵称”字段（例如 MAEP `profile.intro.v1`）。
- 当前展示优先级：`contact_nickname` > `contact_id`。

自动昵称触发（已实现）：
- 触发条件：`contact_nickname` 为空且 `understanding_depth >= 45`。
- 触发时机：`contacts proactive tick`（使用全局 `llm.*` 配置，LLM 可用）。
- 生成方式：由 LLM 基于 `contact_profile`（topics/persona/depth/interaction signals）输出昵称建议与置信度。
- 生效条件：`confidence >= 0.70`。
- 审计：
  - 成功：`action=contact_nickname.auto_assigned`。
  - 失败：`action=contact_nickname.auto_assign_failed`。

Telegram 人类发送 chat 路由（已实现）：
- 目标从 `ContactProfile.telegram_chats` 与 `ShareDecision.source_chat_*` 联合决定。
- 优先级：
  1. `source_chat_id` 命中联系人已知 chat -> 发送到该 chat。
  2. `source_chat_type`（`private|group|supergroup`）命中 -> 发送到该类型最近 chat。
  3. 回退到联系人最近 `private` chat。
  4. 再回退到 `subject_id/contact_id`（`tg:*` 或 `tg:@*`）。
- 含义：
  - 如果候选内容来自私聊 memory，设置 `source_chat_type=private`，会优先发回私聊。
  - 如果候选内容来自某个群 memory，设置对应 `source_chat_id`，会优先发回该群。

人类联系人开关（已实现）：
- `contacts.human.enabled`：是否支持人类联系人进入 proactive 选择。
- `contacts.human.send.enabled`：`--send` 时是否允许向人类联系人发送。
- `contacts.human.send.public_enabled`：`--send` 时是否允许发到公开场合（如 Telegram 群）。

## 7) 决策流程（端到端）

### 7.1 每次 HEARTBEAT 的完整时序
1. `RULE` 触发 `proactive_share_tick_start`，记录 `tick_id`。
2. `RULE` 读取 `active.md` / `inactive.md`（联系人画像）与最近候选内容索引。
3. `RULE` 执行硬门控（trust/cooldown/active）。
4. `LLM` 为每个候选联系人抽取偏好特征（Prompt A）。
5. `LLM` 为每个联系人的候选内容计算语义交集（Prompt B）。
6. `RULE` 按 4.2/4.3 公式计算 `candidate_score` 与 `contact_score`，选 `top-K`。
7. `RULE` 冻结“本轮发送计划”并写入审计（谁、为什么、分数多少）。
8. `LLM` 按目标联系人的格式偏好生成分享文案。
9. `RULE` 发送 `agent.data.push`（topic=`share.proactive.v1`）。
10. `LLM` 解析反馈信号（Prompt C），`RULE` 更新 `session_interest_level`。
11. `LLM` 产出 profile delta 建议（Prompt D），`RULE` 按公式写入 profile。
12. `RULE` 写入 memory 摘要（必须包含 `contact_id` 与 `contact_nickname`）。
13. `RULE` 执行 150 上限淘汰/激活，追加写入 `share_decisions_audit.jsonl`。
14. `RULE` 记录 `proactive_share_tick_end`。

### 7.2 会话内循环（单联系人）
1. `RULE` 初始化 `session_interest_level=0.6`、`turn=0`。
2. `LLM` 生成第 1 条分享消息。
3. `RULE` 发送并等待对方响应（超时即结束）。
4. `LLM` 输出兴趣信号，`RULE` 应用 4.5 更新热度。
5. `RULE` 判定是否继续：
   - `turn >= N`。
   - `interest < 0.30` 连续 2 轮。
   - `LLM next_action=wrap_up` 且置信度足够。
   - 对方明确结束。
6. 若继续则回到步骤 2；否则结束会话并写汇总。

### 7.3 错误与回退策略
- LLM 输出 JSON 不合法：本轮降级为 `RULE-only`，使用历史 profile 和默认模板。
- LLM 输出字段缺失：使用默认值，不中断整个 tick。
- 推送失败：记录失败并进入短冷却，不影响其它联系人。
- 会话中断：保留当前 `session_state`，下轮恢复时只读取摘要。

## 8) MVP 边界
MVP 做：
- 固定公式与固定权重。
- 4 个结构化 prompt。
- 反馈三分类。
- 深度衰减与 150 上限淘汰。

MVP 不做：
- 强化学习自动调参。
- 群体扩散策略。
- 跨 agent 共享画像。

## 9) 验收标准
- 能按 HEARTBEAT 主动触达联系人。
- 选人和选内容可解释（含审计记录）。
- 72h 最近优先策略严格生效。
- 会话内热度变化可观测并影响是否继续。
- profile 更新和遗忘衰减可观测。
- 活跃联系人始终不超过 150。

## 10) 参数冻结与确认清单

### 10.1 已确认
- 使用 `HEARTBEAT.md` 触发主动分享任务。
- 最近信息优先窗口为 `72h`。
- 会话整理入 Memory，并记录 `contact_id + contact_nickname`（Memory `SessionID` 使用通道稳定键，而非 `contact_id`）。
- 活跃联系人上限固定为 `150`，超出进入冷存储。

### 10.2 待确认（建议默认值）
| 项目 | 建议默认值 | 影响 | 状态 |
|---|---|---|---|
| `max_targets_per_tick` | `3` | 每次 heartbeat 主动联系人数上限 | 确认 |
| `max_turns_per_session (N)` | `6` | 单次会话最长轮数 | 确认 |
| `interest_stop_threshold` | `0.30` | 低于该值触发会话结束判定 | 确认 |
| `interest_low_rounds` | `2` | 连续低兴趣轮数阈值 | 确认 |
| `negative_cooldown_hours` | `72` | 负反馈后的冷却时间 | 确认 |
| `max_linked_history_items` | `4` | 附带历史上下文的上限 | 确认 |
| `tofu_sensitivity_penalty` | `0.25` | tofu 联系人高敏内容惩罚 | 保留参数（当前 agent proactive 默认不联系 tofu） |
| `cold_reactivate_margin` | `0.05` | 冷联系人重新激活滞回值 | 确认 |
| `contacts.human.enabled` | `true` | 是否让 human 联系人参与 proactive | 确认 |
| `contacts.human.send.enabled` | `true` | 是否允许 `--send` 向 human 发送 | 确认 |
| `contacts.human.send.public_enabled` | `false` | 是否允许发到群等公开场合 | 确认 |

### 10.3 策略确认问题
1. 是否允许 `tofu` 联系人进入主动分享？
> 不允许。
2. 负反馈是否应该立即停止当前会话？
> 不立即停止，允许 1 轮修正机会；若仍低兴趣则结束。
3. 冷存储联系人是否允许周期性“探测式触达”？
> 不允许，仅对方主动或分数回升时激活。

## 11) 实现进度（2026-02-07）

已完成：
- [x] 新增独立 `contacts` 业务包（`contacts/types.go`、`contacts/store.go`、`contacts/file_store.go`、`contacts/service.go`）。
- [x] 联系人文件存储落地为 `active.md` / `inactive.md`（每个 item 使用 JSON bullet），并通过 `internal/fsstore` 做原子写与跨进程锁。
- [x] `ContactProfile` 已落地 `contact_nickname` 字段，并兼容历史 `display_name`。
- [x] `ContactProfile` 已落地 `persona_brief` 与 `persona_traits` 字段（含归一化与持久化）。
- [x] `contacts_upsert` tool 已支持 Telegram 映射参数（`telegram_username` / `contact_nickname`），可自动生成 `contact_id=tg:@<username>`。
- [x] 候选内容存储：`share_candidates.json`。
- [x] 会话状态存储：`share_sessions.json`。
- [x] 审计存储：`share_decisions_audit.jsonl`（JSONL append）。
- [x] `RULE-only` proactive tick（过滤、打分、top-K、dry-run/send、150 上限重排）。
- [x] 可选 LLM 特征提取接入（Prompt B：`overlap_semantic` + `explicit_history_links`；Prompt A：联系人偏好抽取并写回 `topic_weights`，并补充 `persona_brief/persona_traits`）。
- [x] Prompt C 接入（MAEP 入站会话）：LLM 反馈分类 `signal_* + next_action`，并用于会话停止/继续判定。
- [x] 新增 CLI：`mistermorph contacts`（当前保留 `sync-maep`；其余联系人维护流程已迁移到 tools/runtime）。
- [x] `maep` 作为发送通道接入（send 模式调用 `agent.data.push`），但不承担 contacts 业务持久化。
- [x] memory 写入元信息已支持 `contact_id` + `contact_nickname`（Telegram/CLI memory 更新链路已接入）。
- [x] Telegram 入站消息（被处理的消息）会自动观察并 upsert 到 contacts（人类默认 `contact_id=tg:@username`，无 username 时回退 `tg:<uid>`）。
- [x] MAEP `serve` 入站 `agent.data.push` 会自动观察并 upsert 到 contacts（可通过 `--sync-business-contacts=false` 关闭）。
- [x] proactive 发送支持按联系人类型路由：agent 走 MAEP，Telegram 人类走 Telegram Bot API（`telegram.bot_token`）。
- [x] 人类联系人策略开关：支持/发送/公开发送（`contacts.human.*` 配置与 `proactive tick` flags）。
- [x] Agent tools 已接入：`memory_recently`、`contacts_list`、`contacts_candidate_rank`、`contacts_send`。
- [x] 单测：`contacts/file_store_test.go`、`contacts/service_test.go`。
- [x] 单测：`contacts/llm_features_test.go` 覆盖 LLM 输出解析与夹紧。
- [x] 单测：`contacts/llm_nickname_test.go` 覆盖昵称 JSON 解析。

待完成（TODO）：
- [x] 接入 Prompt A（偏好抽取）并将输出写入 `topic_weights`（按置信度缩放 `alpha` 并审计）。
- [x] 接入 Prompt C（会话反馈分类）并把 `next_action` 接入会话停止/继续决策。
- [ ] 接入 Prompt D（profile delta）并按 RULE 公式落盘（含审计）。
- [x] 程序化 feedback 更新（非 tool）支持按 `topic` 更新偏好。
- [ ] 会话内多轮互动（interest 动态、wrap-up）与 `share_sessions.json` 更深度联动。
- [x] 更细粒度 cooldown 策略（按 `session_interest_level` 分层：24h/48h/72h）。
- [ ] contacts 工具权限细化（按运行模式/风险级别限制 `contacts_send`）。
- [x] 文档同步：第 12 节 tool 列表/参数已与当前实现对齐（移除 `contacts_candidate_add`、`contacts_proactive_tick`）。

## 12) CLI 命令说明（当前实现）

> 说明：CLI 是运维/人工调试入口。Agent 侧默认应通过内置 tools 调用，不应在提示词里要求执行 shell 命令。

### 12.0 Agent Tools（当前实现）

- `memory_recently`
  - 读取最近 short-term memory，返回 `contact_id`、`contact_nickname`、`session_id`、`telegram_chat_id`、`channel` 等上下文与路由线索。
- `contacts_list`
  - 查看联系人（支持按 `status` 过滤）。
- `contacts_candidate_rank`
  - 对当前候选池做“联系人 x 候选内容”打分排序，返回 top-K 决策，不发送（默认启用 LLM 特征，使用全局 `llm.*` 配置）。
- `contacts_send`
  - 发送单条消息到指定联系人（内部自动路由：agent -> MAEP，human -> Telegram）。
  - 固定发送 `chat.message`，调用方不再传 `topic`。
- `runtime feedback update`（程序流程内）
  - 在 Telegram/MAEP 入站流程内自动更新会话兴趣、reciprocity、理解深度与 topic 偏好。

### 12.0.1 Tool 参数契约（MVP）

`memory_recently`
- 输入：
  - `days` `int`，默认 `3`。
  - `limit` `int`，默认 `50`（受 `tools.memory.recently.max_items` 上限约束）。
  - `include_body` `bool`，默认 `false`。
- 输出关键字段：
  - `items[].contact_id`
  - `items[].contact_nickname`
  - `items[].session_id`
  - `items[].telegram_chat_id`
  - `items[].telegram_chat_type`
  - `items[].summary`

`contacts_list`
- 输入：
  - `status` `string`：`all|active|inactive`，默认 `all`。
  - `limit` `int`，默认不限制。
- 输出关键字段：
  - `contacts[].contact_id`
  - `contacts[].kind`
  - `contacts[].status`
  - `contacts[].peer_id`
  - `contacts[].contact_nickname`
  - `contacts[].telegram_chats`

`contacts_candidate_rank`
- 输入：
  - `limit`
  - `freshness_window`
  - `max_linked_history_items`
  - `human_public_send_enabled`
  - `push_topic`
- 输出关键字段：
  - `count`
  - `decisions[]`（包含 `contact_id`、`item_id`、`score`、`source_chat_id/source_chat_type`）

说明：
- 默认启用 LLM 特征提取。
- 不接受 `llm_features/provider/endpoint/api_key/model/llm_timeout` 参数；统一使用全局 `llm.*` 配置。

`contacts_send`
- 输入（必填）：
  - `contact_id`
  - `message_text` 或 `message_base64`
- 输入（可选）：
  - `content_type`（默认 `application/json`）
  - `session_id` / `reply_to`
- 输出关键字段：
  - `outcome.accepted`
  - `outcome.deduped`
  - `outcome.error`

### 12.0.2 推荐调用时序（给 agent）

1. `memory_recently(days=3, limit=20)` 读取最近上下文与路由线索。  
2. `contacts_list(status="active")` 读取目标池。  
3. `contacts_candidate_rank(limit=3)` 获取 top 决策。  
4. 对选中的决策逐条调用 `contacts_send(...)`。  
5. 根据响应信号由运行时流程自动更新 feedback 状态（无需 tool 调用）。  

说明：
- `contacts_send` 固定发送 `chat.message` topic，不再接收 `topic` 参数。

### 12.0.3 JSON 样例

`memory_recently` 响应样例（节选）：

```json
{
  "days": 3,
  "count": 2,
  "items": [
    {
      "contact_id": "tg:@alice",
      "contact_nickname": "Alice",
      "session_id": "tg:-100123456",
      "telegram_chat_id": -100123456,
      "telegram_chat_type": "group",
      "summary": "讨论了最近的模型评测结果"
    }
  ]
}
```

`contacts_candidate_rank` 请求样例：

```json
{
  "limit": 3,
  "human_public_send_enabled": false
}
```

`contacts_send` 请求样例：

```json
{
  "contact_id": "tg:@alice",
  "message_text": "刚刚看到一条你可能感兴趣的更新",
  "content_type": "application/json",
  "session_id": "018f6f9c-83da-7f0a-a4a5-8f0aa4a58f0a"
}
```

### 12.1 CLI 命令（运维/调试）

命令组：`mistermorph contacts`

说明：
- 旧的联系人维护子命令已移除。
- 联系人读取与写入请走 `contacts_list` / `contacts_upsert` tools。

### 12.3 `contacts candidate add` / `contacts candidate list`
用途：
- 管理“可分享内容候选池”。
- `add` 添加候选内容，`list` 查看候选内容。
- 对 Telegram 人类可附带路由线索：`--source-chat-id`、`--source-chat-type`。

示例：
- `mistermorph contacts candidate add --topic maep --text "hey"`
- `mistermorph contacts candidate add --topic story --text "..." --source-chat-id -100123456 --source-chat-type group`
- `mistermorph contacts candidate list`

### 12.4 `contacts proactive tick`
用途：
- 执行一次主动分享决策循环（筛选联系人、匹配候选、打分、top-K）。
- 默认 dry-run，仅出决策不发消息。
- `--send` 时按联系人类型发送：
  - agent：通过 MAEP `agent.data.push`。
  - Telegram 人类：通过 Telegram Bot API `sendMessage`。
- 默认启用 LLM 特征抽取（当前为 Prompt B 最小版，使用全局 `llm.*` 配置）。
- 同时启用 LLM 昵称生成（仅对空昵称 + depth 达阈值联系人）。
- `--json` 仅决定 CLI 输出格式，不影响是否发送、不影响决策逻辑。
- 人类联系人开关可通过配置与 flags 控制：`--human-enabled`、`--human-send-enabled`、`--human-public-send-enabled`。

示例：
- `mistermorph contacts proactive tick --json`
- `mistermorph contacts proactive tick --send --json`
- `mistermorph contacts proactive tick --send --telegram-bot-token "$MISTER_MORPH_TELEGRAM_BOT_TOKEN" --json`

### 12.6 `contacts audit list`
用途：
- 查询主动分享审计日志（tick 开始/结束、决策、发送成功/失败、LLM 特征提取结果）。

示例：
- `mistermorph contacts audit list --limit 100`
- `mistermorph contacts audit list --tick-id tick_xxx`

### 12.7 `contacts sync-maep`
用途：
- 将 MAEP 协议层 contacts 同步为业务层 contacts（减少重复录入）。
- 默认仅同步 `verified`；可选包含 `tofu`。

示例：
- `mistermorph contacts sync-maep`
- `mistermorph contacts sync-maep --include-tofu`
