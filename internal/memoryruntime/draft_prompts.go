package memoryruntime

import (
	_ "embed"
	"encoding/json"
	"text/template"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/prompttmpl"
	"github.com/quailyquaily/mistermorph/memory"
)

//go:embed prompts/memory_draft_system.md
var memoryDraftSystemPromptTemplateSource string

//go:embed prompts/memory_draft_user.md
var memoryDraftUserPromptTemplateSource string

var memoryPromptTemplateFuncs = template.FuncMap{
	"toJSON": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
}

var memoryDraftSystemPromptTemplate = prompttmpl.MustParse("memoryruntime_memory_draft_system", memoryDraftSystemPromptTemplateSource, nil)
var memoryDraftUserPromptTemplate = prompttmpl.MustParse("memoryruntime_memory_draft_user", memoryDraftUserPromptTemplateSource, memoryPromptTemplateFuncs)

type memoryDraftUserPromptData struct {
	SessionContext       memory.SessionContext
	ChatHistoryMessages  []chathistory.PromptMessageItem
	CurrentTask          string
	CurrentOutput        string
	ExistingSummaryItems []memory.SummaryItem
}

const memoryDraftExistingSummaryItemsLimit = 5

func renderMemoryDraftPrompts(
	ctxInfo memory.SessionContext,
	history []chathistory.ChatHistoryItem,
	task string,
	output string,
	existing memory.ShortTermContent,
) (string, string, error) {
	systemPrompt, err := prompttmpl.Render(memoryDraftSystemPromptTemplate, struct{}{})
	if err != nil {
		return "", "", err
	}
	userPrompt, err := prompttmpl.Render(memoryDraftUserPromptTemplate, memoryDraftUserPromptData{
		SessionContext:       ctxInfo,
		ChatHistoryMessages:  chathistory.BuildPromptMessages("", history),
		CurrentTask:          task,
		CurrentOutput:        output,
		ExistingSummaryItems: recentSummaryItems(existing.SummaryItems, memoryDraftExistingSummaryItemsLimit),
	})
	if err != nil {
		return "", "", err
	}
	return systemPrompt, userPrompt, nil
}

func recentSummaryItems(items []memory.SummaryItem, limit int) []memory.SummaryItem {
	if len(items) == 0 || limit <= 0 {
		return nil
	}
	if len(items) <= limit {
		return append([]memory.SummaryItem(nil), items...)
	}
	return append([]memory.SummaryItem(nil), items[len(items)-limit:]...)
}
