package chathistory

import "time"

type PromptMessageItem struct {
	SentAt time.Time           `json:"sent_at"`
	Sender PromptMessageSender `json:"sender"`
	Text   string              `json:"text"`
	Quote  *PromptMessageQuote `json:"quote,omitempty"`
}

type PromptMessageSender struct {
	Username   string `json:"username,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	IsBot      bool   `json:"is_bot,omitempty"`
	DisplayRef string `json:"display_ref,omitempty"`
}

type PromptMessageQuote struct {
	SenderRef string `json:"sender_ref,omitempty"`
	Text      string `json:"text,omitempty"`
}

func BuildPromptMessages(channel string, items []ChatHistoryItem) []PromptMessageItem {
	rawItems := BuildMessages(channel, items)
	if len(rawItems) == 0 {
		return nil
	}
	out := make([]PromptMessageItem, 0, len(rawItems))
	for _, item := range rawItems {
		out = append(out, BuildPromptMessage(item))
	}
	return out
}

func BuildPromptMessage(item ChatHistoryItem) PromptMessageItem {
	return PromptMessageItem{
		SentAt: item.SentAt,
		Sender: PromptMessageSender{
			Username:   item.Sender.Username,
			Nickname:   item.Sender.Nickname,
			IsBot:      item.Sender.IsBot,
			DisplayRef: item.Sender.DisplayRef,
		},
		Text:  item.Text,
		Quote: buildPromptQuote(item.Quote),
	}
}

func buildPromptQuote(quote *ChatHistoryQuote) *PromptMessageQuote {
	if quote == nil {
		return nil
	}
	return &PromptMessageQuote{
		SenderRef: quote.SenderRef,
		Text:      quote.Text,
	}
}
