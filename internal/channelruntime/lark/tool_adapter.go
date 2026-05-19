package lark

import (
	"context"
	"fmt"
	"strings"

	larktools "github.com/quailyquaily/mistermorph/tools/lark"
)

type larkToolAPI struct {
	api *larkAPI
}

func newLarkToolAPI(api *larkAPI) larktools.API {
	if api == nil {
		return nil
	}
	return &larkToolAPI{api: api}
}

func (a *larkToolAPI) SendFile(ctx context.Context, chatID string, filePath string, filename string, caption string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("lark api not available")
	}
	if err := a.api.sendFile(ctx, chatID, filePath, filename, caption); err != nil {
		return annotateLarkToolError(err, "send file", "message send and file upload permissions")
	}
	return nil
}

func (a *larkToolAPI) SendPhoto(ctx context.Context, chatID string, filePath string, filename string, caption string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("lark api not available")
	}
	if err := a.api.sendPhoto(ctx, chatID, filePath, filename, caption); err != nil {
		return annotateLarkToolError(err, "send photo", "message send and image upload permissions")
	}
	return nil
}

func (a *larkToolAPI) SendVoice(ctx context.Context, chatID string, filePath string, filename string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("lark api not available")
	}
	if err := a.api.sendVoice(ctx, chatID, filePath, filename); err != nil {
		return annotateLarkToolError(err, "send voice", "message send and file upload permissions for OPUS audio")
	}
	return nil
}

func (a *larkToolAPI) SetEmojiReaction(ctx context.Context, messageID string, emojiType string) error {
	if a == nil || a.api == nil {
		return fmt.Errorf("lark api not available")
	}
	if err := a.api.setEmojiReaction(ctx, messageID, emojiType); err != nil {
		return annotateLarkToolError(err, "add reaction", "message reaction permission")
	}
	return nil
}

func annotateLarkToolError(err error, operation string, permission string) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "permission") ||
		strings.Contains(msg, "scope") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "unauthorized") {
		return fmt.Errorf("lark %s failed; check %s: %w", operation, permission, err)
	}
	return err
}
