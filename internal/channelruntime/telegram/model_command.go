package telegram

import (
	"context"
	htmlstd "html"
	"strings"
)

func executeTelegramProfileCommand(d Dependencies, api *telegramAPI, chatID int64, messageThreadID int64, text string) bool {
	if d.HandleModelCommand == nil {
		return false
	}
	output, handled, err := d.HandleModelCommand(text)
	if !handled {
		return false
	}
	if err != nil {
		output = "error: " + strings.TrimSpace(err.Error())
	}
	_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(output), true)
	return true
}

func executeTelegramSkillCommand(d Dependencies, api *telegramAPI, chatID int64, messageThreadID int64, currentSkills []string) bool {
	if d.HandleSkillCommand == nil {
		return false
	}
	output, err := d.HandleSkillCommand(append([]string(nil), currentSkills...))
	if err != nil {
		output = "error: " + strings.TrimSpace(err.Error())
	}
	_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(output), true)
	return true
}
