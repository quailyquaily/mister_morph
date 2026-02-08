package telegramcmd

import (
	_ "embed"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/prompttmpl"
)

//go:embed prompts/reaction_category_system.tmpl
var reactionCategorySystemPromptTemplateSource string

//go:embed prompts/reaction_category_user.tmpl
var reactionCategoryUserPromptTemplateSource string

var reactionPromptTemplateFuncs = template.FuncMap{
	"toJSON": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
}

var reactionCategorySystemPromptTemplate = prompttmpl.MustParse("reaction_category_system", reactionCategorySystemPromptTemplateSource, nil)
var reactionCategoryUserPromptTemplate = prompttmpl.MustParse("reaction_category_user", reactionCategoryUserPromptTemplateSource, reactionPromptTemplateFuncs)

type reactionCategoryIntentPayload struct {
	Goal        string   `json:"goal"`
	Deliverable string   `json:"deliverable"`
	Constraints []string `json:"constraints"`
}

type reactionCategoryUserPromptData struct {
	Intent     reactionCategoryIntentPayload `json:"intent"`
	Task       string                        `json:"task"`
	Categories []string                      `json:"categories"`
	Rules      []string                      `json:"rules"`
}

func renderReactionCategoryPrompts(intent agent.Intent, task string) (string, string, error) {
	systemPrompt, err := prompttmpl.Render(reactionCategorySystemPromptTemplate, struct{}{})
	if err != nil {
		return "", "", err
	}
	userPrompt, err := prompttmpl.Render(reactionCategoryUserPromptTemplate, reactionCategoryUserPromptData{
		Intent: reactionCategoryIntentPayload{
			Goal:        intent.Goal,
			Deliverable: intent.Deliverable,
			Constraints: intent.Constraints,
		},
		Task: strings.TrimSpace(task),
		Categories: []string{
			"confirm", "agree", "seen", "thanks", "celebrate", "cancel", "wait", "none",
		},
		Rules: []string{
			"Pick the best reaction category for a lightweight acknowledgement.",
			"Return none if the intent does not match any category.",
			"Use the same language as the user for the reason, but keep it short.",
		},
	})
	if err != nil {
		return "", "", err
	}
	return systemPrompt, userPrompt, nil
}
