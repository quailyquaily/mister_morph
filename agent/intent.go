package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type Intent struct {
	Goal        string   `json:"goal"`
	Deliverable string   `json:"deliverable"`
	Constraints []string `json:"constraints"`
	Ambiguities []string `json:"ambiguities"`
	Ask         bool     `json:"ask"`
}

func (i Intent) Empty() bool {
	return strings.TrimSpace(i.Goal) == "" &&
		strings.TrimSpace(i.Deliverable) == "" &&
		len(i.Constraints) == 0 &&
		len(i.Ambiguities) == 0 &&
		!i.Ask
}

func InferIntent(ctx context.Context, client llm.Client, model string, task string, history []llm.Message, maxHistory int) (Intent, error) {
	if client == nil {
		return Intent{}, fmt.Errorf("nil llm client")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return Intent{}, fmt.Errorf("empty task")
	}
	payload := map[string]any{
		"task":    task,
		"history": trimIntentHistory(history, maxHistory),
		"rules": []string{
			"Return a compact, structured intent summary.",
			"Use the same language as the user for values.",
			"goal: the user's true objective (not the literal request).",
			"deliverable: the minimum acceptable output form (e.g., list of concrete items, decision, plan, code diff).",
			"constraints: explicit constraints like time range, quantity, sources, format, language.",
			"ambiguities: only material uncertainties that block a good answer.",
			"ask: default false; set true only if you cannot proceed safely or would risk irreversible harm without clarification.",
			"Prefer proceeding with stated assumptions over asking questions.",
			"Do not invent constraints or facts.",
		},
	}
	b, _ := json.Marshal(payload)
	sys := "You infer user intent. Return ONLY JSON with keys: " +
		"goal (string), deliverable (string), constraints (array of strings), ambiguities (array of strings), ask (boolean)."

	res, err := client.Chat(ctx, llm.Request{
		Model:     model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: string(b)},
		},
		Parameters: map[string]any{
			"max_tokens":  300,
			"temperature": 0,
		},
	})
	if err != nil {
		return Intent{}, err
	}
	raw := strings.TrimSpace(res.Text)
	if raw == "" {
		return Intent{}, fmt.Errorf("empty intent response")
	}
	var out Intent
	if err := jsonutil.DecodeWithFallback(raw, &out); err != nil {
		return Intent{}, fmt.Errorf("invalid intent json")
	}
	out = normalizeIntent(out)
	return out, nil
}

func IntentBlock(intent Intent) PromptBlock {
	payload, _ := json.MarshalIndent(intent, "", "  ")
	return PromptBlock{
		Title:   "Intent (inferred)",
		Content: "```json\n" + string(payload) + "\n```",
	}
}

func IntentSystemMessage(intent Intent) string {
	payload, _ := json.MarshalIndent(intent, "", "  ")
	return "Intent Inference (JSON):\n" + string(payload) + "\nUse this to decide deliverable and constraints."
}

func trimIntentHistory(history []llm.Message, max int) []llm.Message {
	if max <= 0 {
		return nil
	}
	out := make([]llm.Message, 0, max)
	for _, m := range history {
		role := strings.TrimSpace(strings.ToLower(m.Role))
		if role == "system" || role == "" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	if len(out) <= max {
		return out
	}
	return out[len(out)-max:]
}

func normalizeIntent(intent Intent) Intent {
	intent.Goal = strings.TrimSpace(intent.Goal)
	intent.Deliverable = strings.TrimSpace(intent.Deliverable)
	intent.Constraints = normalizeIntentSlice(intent.Constraints)
	intent.Ambiguities = normalizeIntentSlice(intent.Ambiguities)
	return intent
}

func normalizeIntentSlice(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
