package bus

import (
	"fmt"
	"strconv"
	"strings"
)

func BuildConversationKey(channel Channel, id string) (string, error) {
	if !isValidChannel(channel) {
		return "", fmt.Errorf("channel is invalid")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("conversation id is required")
	}
	if strings.Contains(id, " ") {
		return "", fmt.Errorf("conversation id must not contain spaces")
	}
	return fmt.Sprintf("%s:%s", conversationKeyPrefix(channel), id), nil
}

func BuildTelegramChatConversationKey(chatID string) (string, error) {
	return BuildConversationKey(ChannelTelegram, chatID)
}

func BuildTelegramTopicConversationKey(chatID string, messageThreadID int64) (string, error) {
	chatID = strings.TrimSpace(chatID)
	if messageThreadID <= 0 {
		return BuildTelegramChatConversationKey(chatID)
	}
	return BuildTelegramChatConversationKey(chatID + "_" + strconv.FormatInt(messageThreadID, 10))
}

func ParseTelegramConversationKey(conversationKey string) (int64, int64, error) {
	const prefix = "tg:"
	key := strings.TrimSpace(conversationKey)
	if !strings.HasPrefix(strings.ToLower(key), prefix) {
		return 0, 0, fmt.Errorf("telegram conversation key is invalid")
	}
	raw := strings.TrimSpace(key[len(prefix):])
	if raw == "" {
		return 0, 0, fmt.Errorf("telegram chat id is required")
	}
	parts := strings.Split(raw, "_")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("telegram conversation key is invalid")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("telegram chat id is invalid: %w", err)
	}
	if chatID == 0 {
		return 0, 0, fmt.Errorf("telegram chat id is required")
	}
	if len(parts) == 1 {
		return chatID, 0, nil
	}
	messageThreadID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("telegram message thread id is invalid: %w", err)
	}
	if messageThreadID <= 0 {
		return 0, 0, fmt.Errorf("telegram message thread id is invalid")
	}
	return chatID, messageThreadID, nil
}

func BuildSlackChannelConversationKey(channelID string) (string, error) {
	return BuildConversationKey(ChannelSlack, channelID)
}

func BuildLineConversationKey(chatID string) (string, error) {
	return BuildConversationKey(ChannelLine, chatID)
}

func BuildLarkConversationKey(chatID string) (string, error) {
	return BuildConversationKey(ChannelLark, chatID)
}

func BuildLineGroupConversationKey(groupID string) (string, error) {
	return BuildLineConversationKey(groupID)
}

func isValidChannel(channel Channel) bool {
	switch channel {
	case ChannelConsole, ChannelTelegram, ChannelSlack, ChannelLine, ChannelLark, ChannelDiscord:
		return true
	default:
		return false
	}
}

func conversationKeyPrefix(channel Channel) string {
	switch channel {
	case ChannelConsole:
		return "console"
	case ChannelTelegram:
		return "tg"
	case ChannelSlack:
		return "slack"
	case ChannelLine:
		return "line"
	case ChannelLark:
		return "lark"
	case ChannelDiscord:
		return "discord"
	default:
		return ""
	}
}
