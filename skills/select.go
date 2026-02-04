package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type SelectOptions struct {
	Model        string
	MaxLoad      int
	PreviewBytes int64
	CatalogLimit int
}

type Selection struct {
	SkillsToLoad []string `json:"skills_to_load"`
	Reasoning    string   `json:"reasoning"`
}

func Select(ctx context.Context, client llm.Client, task string, all []Skill, opts SelectOptions) (Selection, error) {
	if client == nil {
		return Selection{}, fmt.Errorf("missing llm client")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return Selection{}, fmt.Errorf("missing task")
	}
	if opts.MaxLoad <= 0 {
		opts.MaxLoad = 3
	}
	if opts.PreviewBytes <= 0 {
		opts.PreviewBytes = 2048
	}
	if opts.CatalogLimit <= 0 {
		opts.CatalogLimit = 200
	}
	if opts.Model == "" {
		opts.Model = "gpt-4o-mini"
	}

	type skillInfo struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Preview string `json:"preview"`
	}

	limited := all
	if len(limited) > opts.CatalogLimit {
		limited = limited[:opts.CatalogLimit]
	}

	catalog := make([]skillInfo, 0, len(limited))
	for _, s := range limited {
		preview, err := LoadPreview(s, opts.PreviewBytes)
		if err != nil {
			continue
		}
		catalog = append(catalog, skillInfo{
			ID:      s.ID,
			Name:    s.Name,
			Preview: strings.TrimSpace(preview.Contents),
		})
	}

	payload := map[string]any{
		"task":             task,
		"max_skills":       opts.MaxLoad,
		"available_skills": catalog,
	}
	payloadJSON, _ := json.Marshal(payload)

	sys := fmt.Sprintf(strings.TrimSpace(`
You are a router that selects which skills (if any) should be loaded to best complete a task.

Rules:
- Only choose skills from available_skills (use the "id" field).
- Choose at most %d skills.
- If none are helpful, choose an empty list.

Return JSON:
{
  "skills_to_load": ["id1", "id2"],
  "reasoning": "short"
}
`), opts.MaxLoad)

	res, err := client.Chat(ctx, llm.Request{
		Model:     opts.Model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: string(payloadJSON)},
		},
	})
	if err != nil {
		return Selection{}, err
	}

	var out Selection
	if err := jsonutil.DecodeWithFallback(res.Text, &out); err != nil {
		return Selection{}, err
	}

	if len(out.SkillsToLoad) > opts.MaxLoad {
		out.SkillsToLoad = out.SkillsToLoad[:opts.MaxLoad]
	}

	return out, nil
}
