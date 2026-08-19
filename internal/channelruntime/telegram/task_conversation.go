package telegram

import (
	"strconv"
	"strings"

	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func telegramTaskConversation(conversationID string, inbound telegrambus.InboundMessage, botUsername string, botID int64) *taskdomain.TaskConversation {
	senderName := strings.TrimSpace(inbound.FromDisplayName)
	if senderName == "" {
		senderName = strings.TrimSpace(strings.Join([]string{inbound.FromFirstName, inbound.FromLastName}, " "))
	}
	botUsername = normalizeTelegramMentionID(botUsername)
	botIDString := ""
	if botID > 0 {
		botIDString = strconv.FormatInt(botID, 10)
	}
	participants := make([]taskdomain.TaskParticipant, 0, 1+len(inbound.MentionParticipants)+len(inbound.MentionUsers))
	seen := make(map[string]bool, cap(participants))
	add := func(participant taskdomain.TaskParticipant) {
		participant.ID = strings.TrimSpace(participant.ID)
		if participant.ID == "" || strings.EqualFold(participant.ID, botUsername) || participant.ID == botIDString {
			return
		}
		key := strings.ToLower(participant.ID)
		if seen[key] {
			return
		}
		seen[key] = true
		participants = append(participants, participant)
	}
	add(taskdomain.TaskParticipant{
		ID:       telegramSenderParticipantID(inbound.FromUsername, inbound.FromUserID),
		Nickname: senderName,
	})
	for _, participant := range inbound.MentionParticipants {
		add(taskdomain.TaskParticipant{ID: participant.ID, Nickname: participant.Nickname})
	}
	for _, raw := range inbound.MentionUsers {
		if mentionID := normalizeTelegramMentionID(raw); mentionID != "" {
			add(taskdomain.TaskParticipant{ID: mentionID})
		}
	}
	return taskdomain.NormalizeTaskConversation(&taskdomain.TaskConversation{
		ConversationID:   conversationID,
		ConversationType: inbound.ChatType,
		Participants:     participants,
	})
}

func telegramSenderParticipantID(username string, userID int64) string {
	username = normalizeTelegramMentionID(username)
	if username != "" {
		return username
	}
	if userID > 0 {
		return strconv.FormatInt(userID, 10)
	}
	return ""
}

func normalizeTelegramMentionID(raw string) string {
	id := strings.TrimSpace(raw)
	id = strings.TrimPrefix(id, "@")
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return "@" + id
}
