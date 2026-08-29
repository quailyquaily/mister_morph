package mixin

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

// mixinMessageSender retains the full runtime API while routing message sends
// through the shared conversation sender.
type mixinMessageSender struct {
	mixinAPI
	sender *mixinapi.MessageSender
}

func newMixinMessageSender(api mixinAPI, botUserID string) *mixinMessageSender {
	return &mixinMessageSender{mixinAPI: api, sender: mixinapi.NewMessageSender(api, botUserID, nil)}
}

func (s *mixinMessageSender) SendMessages(ctx context.Context, messages []mixinapi.MessageRequest) error {
	return s.sender.SendMessages(ctx, messages)
}

func (s *mixinMessageSender) InvalidateConversation(conversationID string) {
	s.sender.InvalidateConversation(conversationID)
}
