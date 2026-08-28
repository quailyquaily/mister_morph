package mixinapi

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func UniqueConversationID(userID, recipientID string) (string, error) {
	userID, err := normalizeUUID("user_id", userID)
	if err != nil {
		return "", err
	}
	recipientID, err = normalizeUUID("recipient_id", recipientID)
	if err != nil {
		return "", err
	}
	left, right := userID, recipientID
	if strings.Compare(left, right) > 0 {
		left, right = right, left
	}
	digest := md5.Sum([]byte(left + right))
	digest[6] = (digest[6] & 0x0f) | 0x30
	digest[8] = (digest[8] & 0x3f) | 0x80
	id, err := uuid.FromBytes(digest[:])
	if err != nil {
		return "", fmt.Errorf("build mixin conversation id: %w", err)
	}
	return id.String(), nil
}

func (c *Client) CreateContactConversation(ctx context.Context, peerUserID string) (Conversation, error) {
	if c == nil {
		return Conversation{}, fmt.Errorf("mixin api client is not initialized")
	}
	peerUserID, err := normalizeUUID("peer_user_id", peerUserID)
	if err != nil {
		return Conversation{}, err
	}
	conversationID, err := UniqueConversationID(c.credentials.ClientID, peerUserID)
	if err != nil {
		return Conversation{}, err
	}
	return c.CreateConversation(ctx, CreateConversationRequest{
		ConversationID: conversationID,
		Category:       ConversationCategoryContact,
		Participants:   []ConversationParticipant{{UserID: peerUserID}},
	})
}
