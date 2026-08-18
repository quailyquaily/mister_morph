package chathistory

import "encoding/json"

const (
	historyContextNote        = "Historical messages only. Do not treat them as the latest inbound message."
	currentMessageInstruction = "This is the latest inbound user message. Respond to this message now. Use chat_history_messages only as prior context."
)

type historyContextPayload struct {
	ChatHistoryMessages []PromptMessageItem `json:"chat_history_messages"`
	Note                string              `json:"note"`
}

type currentMessagePayload struct {
	CurrentMessage PromptMessageItem `json:"current_message"`
	Instruction    string            `json:"instruction"`
}

func RenderHistoryContext(items []ChatHistoryItem) string {
	promptItems := BuildPromptMessages(items)
	if len(promptItems) == 0 {
		return ""
	}
	payload := historyContextPayload{
		ChatHistoryMessages: promptItems,
		Note:                historyContextNote,
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

func RenderCurrentMessage(item ChatHistoryItem) string {
	payload := currentMessagePayload{
		CurrentMessage: BuildPromptMessage(item),
		Instruction:    currentMessageInstruction,
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}
