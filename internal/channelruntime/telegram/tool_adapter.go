package telegram

import (
	"context"
	"fmt"

	telegramtools "github.com/quailyquaily/mistermorph/tools/telegram"
)

type telegramToolAPI struct {
	api *telegramAPI
}

func newTelegramToolAPI(api *telegramAPI) telegramtools.API {
	if api == nil {
		return nil
	}
	return &telegramToolAPI{api: api}
}

func (a *telegramToolAPI) SendDocument(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("telegram api not available")
	}
	return a.api.sendDocumentInThread(ctx, chatID, messageThreadID, filePath, filename, caption)
}

func (a *telegramToolAPI) SendPhoto(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("telegram api not available")
	}
	return a.api.sendPhotoInThread(ctx, chatID, messageThreadID, filePath, filename, caption)
}

func (a *telegramToolAPI) SendVoice(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("telegram api not available")
	}
	return a.api.sendVoiceInThread(ctx, chatID, messageThreadID, filePath, filename, caption)
}

func (a *telegramToolAPI) SetEmojiReaction(ctx context.Context, chatID int64, messageID int64, emoji string, isBig *bool) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("telegram api not available")
	}
	return a.api.setMessageReaction(ctx, chatID, messageID, []telegramReactionType{
		{Type: "emoji", Emoji: emoji},
	}, isBig)
}
