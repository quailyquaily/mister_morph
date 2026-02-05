package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

const (
	reactionEmojiConfirm   = "\u2705"     // check mark
	reactionEmojiAgree     = "\U0001F44D" // thumbs up
	reactionEmojiSeen      = "\U0001F440" // eyes
	reactionEmojiCelebrate = "\U0001F389" // party popper
	reactionEmojiThanks    = "\U0001F64F" // folded hands
	reactionEmojiCancel    = "\U0001F44E" // thumbs down
	reactionEmojiOK        = "\U0001F44C" // OK hand
	reactionEmojiWarn      = "\u2757"     // exclamation
	reactionEmojiSmile     = "\U0001F60A" // smiling face
)

type telegramReactionConfig struct {
	Enabled       bool
	Allow         []string
	MaxPerMessage int
}

type telegramReactionDecision struct {
	ShouldReact bool
	Emoji       string
	Reason      string
	Category    string
	Intent      agent.Intent
	HasIntent   bool
}

type telegramReaction struct {
	ChatID    int64
	MessageID int64
	Emoji     string
	Source    string
}

func readTelegramReactionConfig() telegramReactionConfig {
	cfg := telegramReactionConfig{
		Enabled:       viper.GetBool("telegram.reactions.enabled"),
		Allow:         normalizeReactionAllowList(viper.GetStringSlice("telegram.reactions.allow")),
		MaxPerMessage: viper.GetInt("telegram.reactions.max_per_message"),
	}
	if cfg.MaxPerMessage <= 0 {
		cfg.MaxPerMessage = 1
	}
	if len(cfg.Allow) == 0 {
		cfg.Allow = defaultReactionAllowList()
	}
	return cfg
}

func defaultReactionAllowList() []string {
	return []string{
		reactionEmojiConfirm,
		reactionEmojiAgree,
		reactionEmojiSeen,
		reactionEmojiCelebrate,
		reactionEmojiThanks,
		reactionEmojiCancel,
		reactionEmojiOK,
		reactionEmojiWarn,
		reactionEmojiSmile,
	}
}

func normalizeReactionAllowList(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func buildReactionAllowSet(list []string) map[string]bool {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, v := range list {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out[v] = true
	}
	return out
}

func reactionHistoryNote(emoji string) string {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return "[reacted]"
	}
	return "[reacted: " + emoji + "]"
}

func decideTelegramReaction(ctx context.Context, client llm.Client, model string, task string, history []llm.Message, intentCfg agent.Config, reactCfg telegramReactionConfig) (telegramReactionDecision, error) {
	decision := telegramReactionDecision{}
	if !reactCfg.Enabled {
		return decision, nil
	}
	if client == nil {
		return decision, nil
	}
	if !intentCfg.IntentEnabled {
		return decision, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return decision, nil
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return decision, nil
	}

	intentCtx := ctx
	var cancel context.CancelFunc
	if intentCfg.IntentTimeout > 0 {
		intentCtx, cancel = context.WithTimeout(ctx, intentCfg.IntentTimeout)
	}
	if cancel != nil {
		defer cancel()
	}
	intent, err := agent.InferIntent(intentCtx, client, model, task, history, intentCfg.IntentMaxHistory)
	if err != nil {
		return decision, err
	}
	if intent.Empty() {
		return decision, nil
	}

	decision.Intent = intent
	decision.HasIntent = true

	if intent.Ask || len(intent.Ambiguities) > 0 {
		decision.Reason = "intent_ask_or_ambiguous"
		return decision, nil
	}
	if reactionBlockedByText(task) {
		decision.Reason = "text_blocked"
		return decision, nil
	}
	if intentLooksHeavy(intent, task) {
		decision.Reason = "heavy_intent"
		return decision, nil
	}
	match := classifyReactionCategory(intent, task)
	if match.Category == "" {
		intentMatch, err := classifyReactionCategoryViaIntent(ctx, client, model, intent, task)
		if err == nil && intentMatch.Category != "" {
			match = intentMatch
		}
	}
	if match.Category == "" && intentLooksLight(intent) {
		match = reactionMatch{Category: "seen", Source: "intent"}
	}
	if match.Category == "" {
		decision.Reason = "no_light_category"
		return decision, nil
	}
	if match.Source == "task" && !reactionTextLooksLight(task) {
		decision.Reason = "task_too_long"
		return decision, nil
	}
	emoji := pickReactionEmoji(match.Category, reactCfg.Allow)
	if emoji == "" {
		decision.Reason = "no_allowed_emoji"
		return decision, nil
	}
	decision.ShouldReact = true
	decision.Emoji = emoji
	decision.Category = match.Category
	decision.Reason = "lightweight"
	return decision, nil
}

type reactionMatch struct {
	Category string
	Source   string
}

type reactionCategory struct {
	Name     string
	Emoji    string
	Keywords []string
}

var reactionCategories = []reactionCategory{
	{
		Name:  "thanks",
		Emoji: reactionEmojiThanks,
		Keywords: []string{
			"thanks", "thank you", "thx", "appreciate", "much appreciated",
			"谢谢", "感谢", "多谢", "辛苦了",
		},
	},
	{
		Name:  "celebrate",
		Emoji: reactionEmojiCelebrate,
		Keywords: []string{
			"done", "finished", "completed", "resolved", "fixed", "shipped",
			"完成", "已完成", "搞定", "修好了", "解决了",
		},
	},
	{
		Name:  "cancel",
		Emoji: reactionEmojiCancel,
		Keywords: []string{
			"cancel", "no need", "never mind", "nm", "stop", "drop it",
			"算了", "不用了", "取消", "不需要", "别做", "不用管",
		},
	},
	{
		Name:  "confirm",
		Emoji: reactionEmojiConfirm,
		Keywords: []string{
			"ok", "okay", "got it", "roger", "ack", "noted",
			"收到", "明白了", "了解", "好的", "没问题", "确认", "已收",
		},
	},
	{
		Name:  "agree",
		Emoji: reactionEmojiAgree,
		Keywords: []string{
			"agree", "lgtm", "looks good", "sounds good",
			"赞同", "同意", "没错", "确实", "对的",
		},
	},
	{
		Name:  "seen",
		Emoji: reactionEmojiSeen,
		Keywords: []string{
			"fyi", "for your info", "just fyi", "no reply needed",
			"供参考", "仅供参考", "仅通知", "仅告知", "已阅", "已看", "看到", "知道了",
		},
	},
	{
		Name:  "wait",
		Emoji: reactionEmojiSeen,
		Keywords: []string{
			"wait", "hold on", "one sec", "one second", "one moment", "give me a moment", "let me think", "thinking", "brb",
			"等等", "等下", "等一下", "稍等", "稍后", "等会", "等一会", "让我想想", "我想想", "再想想",
		},
	},
}

func classifyReactionCategory(intent agent.Intent, task string) reactionMatch {
	deliverable := strings.ToLower(strings.TrimSpace(intent.Deliverable))
	goal := strings.ToLower(strings.TrimSpace(intent.Goal))
	taskText := strings.ToLower(strings.TrimSpace(task))

	for _, cat := range reactionCategories {
		if containsAny(deliverable, cat.Keywords) {
			return reactionMatch{Category: cat.Name, Source: "deliverable"}
		}
	}
	for _, cat := range reactionCategories {
		if containsAny(goal, cat.Keywords) {
			return reactionMatch{Category: cat.Name, Source: "goal"}
		}
	}
	for _, cat := range reactionCategories {
		if containsAny(taskText, cat.Keywords) {
			return reactionMatch{Category: cat.Name, Source: "task"}
		}
	}
	return reactionMatch{}
}

type reactionCategoryDecision struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func classifyReactionCategoryViaIntent(ctx context.Context, client llm.Client, model string, intent agent.Intent, task string) (reactionMatch, error) {
	if client == nil {
		return reactionMatch{}, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return reactionMatch{}, nil
	}
	payload := map[string]any{
		"intent": map[string]any{
			"goal":        intent.Goal,
			"deliverable": intent.Deliverable,
			"constraints": intent.Constraints,
		},
		"task": strings.TrimSpace(task),
		"categories": []string{
			"confirm", "agree", "seen", "thanks", "celebrate", "cancel", "wait", "none",
		},
		"rules": []string{
			"Pick the best reaction category for a lightweight acknowledgement.",
			"Return none if the intent does not match any category.",
			"Use the same language as the user for the reason, but keep it short.",
		},
	}
	b, _ := json.Marshal(payload)
	sys := "You classify reaction category. Return ONLY JSON: {\"category\":\"one of the given categories\",\"reason\":\"short\"}."
	req := llm.Request{
		Model:     model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: string(b)},
		},
		Parameters: map[string]any{
			"max_tokens": 120,
		},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := client.Chat(ctx, req)
	if err != nil {
		return reactionMatch{}, err
	}
	raw := strings.TrimSpace(res.Text)
	if raw == "" {
		return reactionMatch{}, nil
	}
	var out reactionCategoryDecision
	if err := jsonutil.DecodeWithFallback(raw, &out); err != nil {
		return reactionMatch{}, err
	}
	cat := strings.ToLower(strings.TrimSpace(out.Category))
	if cat == "" || cat == "none" {
		return reactionMatch{}, nil
	}
	return reactionMatch{Category: cat, Source: "intent"}, nil
}

func pickReactionEmoji(category string, allow []string) string {
	category = strings.TrimSpace(strings.ToLower(category))
	if category == "" {
		return ""
	}
	preferred := ""
	for _, cat := range reactionCategories {
		if cat.Name == category {
			preferred = cat.Emoji
			break
		}
	}
	if preferred == "" {
		return ""
	}
	if len(allow) == 0 {
		return preferred
	}
	for _, emoji := range allow {
		if emoji == preferred {
			return preferred
		}
	}
	return strings.TrimSpace(allow[0])
}

func reactionBlockedByText(task string) bool {
	task = strings.TrimSpace(task)
	if task == "" {
		return true
	}
	if strings.Contains(task, "Downloaded Telegram file(s)") {
		return true
	}
	if strings.ContainsAny(task, "?\uff1f") {
		return true
	}
	if strings.Contains(task, "\n") {
		return true
	}
	runes := utf8.RuneCountInString(task)
	if runes > 120 {
		return true
	}
	return false
}

func reactionTextLooksLight(task string) bool {
	task = strings.TrimSpace(task)
	if task == "" {
		return false
	}
	if strings.ContainsAny(task, "?\uff1f") {
		return false
	}
	if strings.Contains(task, "\n") {
		return false
	}
	runes := utf8.RuneCountInString(task)
	if runes > 60 {
		return false
	}
	return true
}

var reactionHeavyKeywords = []string{
	"list", "steps", "explain", "analysis", "plan", "code", "diff", "patch", "report", "summary", "table", "compare",
	"search", "fetch", "news", "links", "sources", "recommend",
	"delete", "remove", "erase", "overwrite", "format", "reset", "revert", "shutdown", "restart", "deploy", "release", "publish", "payment", "transfer",
	"列出", "步骤", "解释", "分析", "方案", "计划", "代码", "修复", "排查", "测试", "实现", "总结", "比较", "新闻", "来源", "推荐", "搜索", "查找",
	"删除", "清空", "覆盖", "格式化", "重置", "回滚", "关机", "重启", "部署", "发布", "上线", "付款", "转账", "支付",
}

var reactionLightKeywords = []string{
	"confirm", "ack", "acknowledge", "ok", "okay", "thanks", "thank", "gratitude", "received", "seen", "waiting",
	"确认", "已读", "收到", "感谢", "谢谢", "知道了", "了解", "稍等", "等一下", "等待", "让我想想",
}

func intentLooksHeavy(intent agent.Intent, task string) bool {
	deliverable := strings.ToLower(strings.TrimSpace(intent.Deliverable))
	goal := strings.ToLower(strings.TrimSpace(intent.Goal))
	taskText := strings.ToLower(strings.TrimSpace(task))
	if containsAny(deliverable, reactionHeavyKeywords) {
		return true
	}
	if containsAny(goal, reactionHeavyKeywords) {
		return true
	}
	if containsAny(taskText, reactionHeavyKeywords) {
		return true
	}
	return false
}

func intentLooksLight(intent agent.Intent) bool {
	deliverable := strings.ToLower(strings.TrimSpace(intent.Deliverable))
	goal := strings.ToLower(strings.TrimSpace(intent.Goal))
	if containsAny(deliverable, reactionLightKeywords) || containsAny(goal, reactionLightKeywords) {
		return true
	}
	if deliverable == "" && goal != "" {
		return true
	}
	return false
}

func containsAny(text string, keywords []string) bool {
	if text == "" || len(keywords) == 0 {
		return false
	}
	for _, k := range keywords {
		if k == "" {
			continue
		}
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}
