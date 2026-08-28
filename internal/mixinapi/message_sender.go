package mixinapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const messageBatchLimit = 100

type MessageSenderClient interface {
	ReadConversation(context.Context, string) (Conversation, error)
	SendMessages(context.Context, []MessageRequest) error
}

// MessageSender turns a conversation-level message into Mixin's per-recipient
// requests and caches group membership until a conversation system event.
type MessageSender struct {
	client    MessageSenderClient
	botUserID string
	onSent    func(conversationID, messageID string)

	mu         sync.RWMutex
	recipients map[string][]string
}

func NewMessageSender(client MessageSenderClient, botUserID string, onSent func(string, string)) *MessageSender {
	return &MessageSender{
		client: client, botUserID: strings.TrimSpace(botUserID), onSent: onSent, recipients: make(map[string][]string),
	}
}

func (s *MessageSender) SendMessages(ctx context.Context, messages []MessageRequest) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("mixin message sender is not configured")
	}
	expanded := make([]MessageRequest, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.RecipientID) != "" {
			expanded = append(expanded, message)
			continue
		}
		recipients, err := s.conversationRecipients(ctx, message.ConversationID)
		if err != nil {
			return err
		}
		messageID, err := uuid.Parse(strings.TrimSpace(message.MessageID))
		if err != nil || messageID == uuid.Nil {
			return fmt.Errorf("mixin message_id is invalid")
		}
		for _, recipientID := range recipients {
			item := message
			item.RecipientID = recipientID
			item.MessageID = uuid.NewSHA1(messageID, []byte(recipientID)).String()
			expanded = append(expanded, item)
		}
	}
	batches, err := messageBatches(expanded)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if err := s.client.SendMessages(ctx, batch); err != nil {
			return fmt.Errorf("send mixin message batch: %w", err)
		}
		if s.onSent != nil {
			for _, message := range batch {
				s.onSent(message.ConversationID, message.MessageID)
			}
		}
	}
	return nil
}

func (s *MessageSender) InvalidateConversation(conversationID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.recipients, strings.TrimSpace(conversationID))
	s.mu.Unlock()
}

func (s *MessageSender) conversationRecipients(ctx context.Context, conversationID string) ([]string, error) {
	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	recipients, found := s.recipients[conversationID]
	s.mu.RUnlock()
	if found {
		return append([]string(nil), recipients...), nil
	}
	conversation, err := s.client.ReadConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("read mixin conversation: %w", err)
	}
	seen := make(map[string]bool, len(conversation.Participants))
	for _, participant := range conversation.Participants {
		recipientID := strings.TrimSpace(participant.UserID)
		parsed, parseErr := uuid.Parse(recipientID)
		if parseErr != nil || parsed == uuid.Nil {
			return nil, fmt.Errorf("mixin conversation contains invalid participant user_id %q", participant.UserID)
		}
		recipientID = parsed.String()
		if strings.EqualFold(recipientID, s.botUserID) || seen[recipientID] {
			continue
		}
		seen[recipientID] = true
		recipients = append(recipients, recipientID)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("mixin conversation has no recipients")
	}
	sort.Strings(recipients)
	s.mu.Lock()
	s.recipients[conversationID] = append([]string(nil), recipients...)
	s.mu.Unlock()
	return recipients, nil
}

func messageBatches(messages []MessageRequest) ([][]MessageRequest, error) {
	var batches [][]MessageRequest
	current := make([]MessageRequest, 0, min(len(messages), messageBatchLimit))
	for _, message := range messages {
		candidate := append(current, message)
		raw, err := json.Marshal(candidate)
		if err != nil {
			return nil, fmt.Errorf("encode mixin message batch: %w", err)
		}
		if len(candidate) <= messageBatchLimit && len(raw) <= maxMessageRequestBytes {
			current = candidate
			continue
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("%w: one mixin message exceeds %d bytes", ErrRequestTooLarge, maxMessageRequestBytes)
		}
		batches = append(batches, current)
		current = []MessageRequest{message}
		raw, err = json.Marshal(current)
		if err != nil {
			return nil, fmt.Errorf("encode mixin message batch: %w", err)
		}
		if len(raw) > maxMessageRequestBytes {
			return nil, fmt.Errorf("%w: one mixin message exceeds %d bytes", ErrRequestTooLarge, maxMessageRequestBytes)
		}
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches, nil
}
