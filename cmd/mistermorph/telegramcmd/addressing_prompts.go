package telegramcmd

import (
	_ "embed"
	"encoding/json"
	"text/template"

	"github.com/quailyquaily/mistermorph/internal/prompttmpl"
)

//go:embed prompts/telegram_addressing_system.tmpl
var telegramAddressingSystemPromptTemplateSource string

//go:embed prompts/telegram_addressing_user.tmpl
var telegramAddressingUserPromptTemplateSource string

var addressingPromptTemplateFuncs = template.FuncMap{
	"toJSON": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
}

var telegramAddressingSystemPromptTemplate = prompttmpl.MustParse("telegram_addressing_system", telegramAddressingSystemPromptTemplateSource, nil)
var telegramAddressingUserPromptTemplate = prompttmpl.MustParse("telegram_addressing_user", telegramAddressingUserPromptTemplateSource, addressingPromptTemplateFuncs)

type telegramAddressingUserPromptData struct {
	BotUsername string
	Aliases     []string
	Message     string
	Note        string
}

func renderTelegramAddressingPrompts(botUser string, aliases []string, text string) (string, string, error) {
	systemPrompt, err := prompttmpl.Render(telegramAddressingSystemPromptTemplate, struct{}{})
	if err != nil {
		return "", "", err
	}
	userPrompt, err := prompttmpl.Render(telegramAddressingUserPromptTemplate, telegramAddressingUserPromptData{
		BotUsername: botUser,
		Aliases:     aliases,
		Message:     text,
		Note:        "An alias keyword was detected somewhere in the message, but a simple heuristic was not confident.",
	})
	if err != nil {
		return "", "", err
	}
	return systemPrompt, userPrompt, nil
}
