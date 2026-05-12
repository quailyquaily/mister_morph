package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type LLMSemanticResolver struct {
	Client llm.Client
	Model  string
}

func NewLLMSemanticResolver(client llm.Client, model string) *LLMSemanticResolver {
	return &LLMSemanticResolver{Client: client, Model: strings.TrimSpace(model)}
}

func (r *LLMSemanticResolver) MatchTaskIndex(ctx context.Context, query string, tasks []Task) (int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return -1, fmt.Errorf("content is required")
	}
	if len(tasks) == 0 {
		return -1, fmt.Errorf("no matching cron task in cron.yaml")
	}
	if r == nil || r.Client == nil || strings.TrimSpace(r.Model) == "" {
		return -1, fmt.Errorf("cron semantic resolver is not configured")
	}
	items := make([]map[string]any, 0, len(tasks))
	for i, task := range tasks {
		items = append(items, map[string]any{
			"index":   i,
			"id":      strings.TrimSpace(task.ID),
			"at":      strings.TrimSpace(task.At),
			"cron":    strings.TrimSpace(task.Cron),
			"tz":      strings.TrimSpace(task.TZ),
			"content": strings.TrimSpace(task.Content),
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"query": query,
		"items": items,
	})
	systemPrompt := strings.Join([]string{
		"You pick exactly one cron.yaml task to delete, using semantic matching.",
		"Return strict JSON only.",
		"Output schema:",
		"{\"status\":\"matched\",\"index\":1} OR {\"status\":\"no_match\"} OR {\"status\":\"ambiguous\",\"candidate_indices\":[1,3]}",
		"If there is no confident match, return no_match.",
		"If multiple entries are plausible, return ambiguous with candidate_indices.",
		"Index values must refer to existing input entries.",
	}, " ")
	res, err := r.Client.Chat(ctx, llm.Request{
		Model:     r.Model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(payload)},
		},
		Parameters: map[string]any{
			"temperature": 0,
			"max_tokens":  500,
		},
	})
	if err != nil {
		return -1, err
	}
	var out struct {
		Status           string `json:"status"`
		Index            *int   `json:"index,omitempty"`
		CandidateIndices []int  `json:"candidate_indices,omitempty"`
	}
	if err := jsonutil.DecodeWithFallback(res.Text, &out); err != nil {
		return -1, fmt.Errorf("invalid semantic_match response: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(out.Status)) {
	case "matched":
		if out.Index == nil {
			return -1, fmt.Errorf("semantic match missing index")
		}
		if *out.Index < 0 || *out.Index >= len(tasks) {
			return -1, fmt.Errorf("semantic match index out of range: %d", *out.Index)
		}
		return *out.Index, nil
	case "no_match":
		return -1, fmt.Errorf("no matching cron task in cron.yaml")
	case "ambiguous":
		if len(out.CandidateIndices) == 0 {
			return -1, fmt.Errorf("ambiguous cron task match")
		}
		for _, idx := range out.CandidateIndices {
			if idx < 0 || idx >= len(tasks) {
				return -1, fmt.Errorf("semantic ambiguous index out of range: %d", idx)
			}
		}
		return -1, fmt.Errorf("ambiguous cron task match")
	default:
		return -1, fmt.Errorf("invalid semantic match status: %s", strings.TrimSpace(out.Status))
	}
}
