package lark

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
)

type larkTextContent struct {
	Text string `json:"text,omitempty"`
}

type larkImageContent struct {
	ImageKey string `json:"image_key,omitempty"`
	Text     string `json:"text,omitempty"`
}

func inboundMessageFromSDKEvent(event *larkim.P2MessageReceiveV1, allowedChats map[string]bool) (larkbus.InboundMessage, bool, error) {
	if event == nil || event.Event == nil {
		return larkbus.InboundMessage{}, false, nil
	}
	if event.Event.Sender == nil || !strings.EqualFold(strings.TrimSpace(larkcore.StringValue(event.Event.Sender.SenderType)), "user") {
		return larkbus.InboundMessage{}, false, nil
	}
	message := event.Event.Message
	if message == nil {
		return larkbus.InboundMessage{}, false, nil
	}
	chatID := strings.TrimSpace(larkcore.StringValue(message.ChatId))
	if chatID == "" {
		return larkbus.InboundMessage{}, false, fmt.Errorf("chat_id is required")
	}
	if len(allowedChats) > 0 && !allowedChats[chatID] {
		return larkbus.InboundMessage{}, false, nil
	}
	messageID := strings.TrimSpace(larkcore.StringValue(message.MessageId))
	if messageID == "" {
		return larkbus.InboundMessage{}, false, fmt.Errorf("message_id is required")
	}
	chatType, err := normalizeLarkInboundChatType(larkcore.StringValue(message.ChatType))
	if err != nil {
		return larkbus.InboundMessage{}, false, err
	}
	fromUserID := larkUserOpenID(event.Event.Sender.SenderId)
	if fromUserID == "" {
		return larkbus.InboundMessage{}, false, fmt.Errorf("from_user_id is required")
	}
	text, imageKeys, ok, err := extractLarkMessageContent(
		larkcore.StringValue(message.MessageType),
		larkcore.StringValue(message.Content),
	)
	if err != nil {
		return larkbus.InboundMessage{}, false, err
	}
	if !ok {
		return larkbus.InboundMessage{}, false, nil
	}
	return larkbus.InboundMessage{
		ChatID:       chatID,
		MessageID:    messageID,
		SentAt:       parseLarkEventTime(larkcore.StringValue(message.CreateTime)),
		ChatType:     chatType,
		FromUserID:   fromUserID,
		Text:         text,
		MentionUsers: collectLarkMentionUsers(message.Mentions),
		EventID:      larkSDKEventID(event),
		ImageKeys:    imageKeys,
	}, true, nil
}

func larkSDKEventID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil {
		return ""
	}
	return strings.TrimSpace(event.EventV2Base.Header.EventID)
}

func larkUserOpenID(id *larkim.UserId) string {
	if id == nil {
		return ""
	}
	if openID := strings.TrimSpace(larkcore.StringValue(id.OpenId)); openID != "" {
		return openID
	}
	if userID := strings.TrimSpace(larkcore.StringValue(id.UserId)); userID != "" {
		return userID
	}
	return strings.TrimSpace(larkcore.StringValue(id.UnionId))
}

func normalizeLarkInboundChatType(raw string) (string, error) {
	chatType := strings.ToLower(strings.TrimSpace(raw))
	switch chatType {
	case "group", "topic_group":
		return "group", nil
	case "p2p", "private":
		return "private", nil
	case "":
		return "", fmt.Errorf("chat_type is required")
	default:
		return "", fmt.Errorf("unsupported lark chat_type: %s", chatType)
	}
}

func extractLarkMessageContent(messageType, content string) (string, []string, bool, error) {
	messageType = strings.ToLower(strings.TrimSpace(messageType))
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil, false, nil
	}
	switch messageType {
	case "text":
		var textContent larkTextContent
		if err := json.Unmarshal([]byte(content), &textContent); err != nil {
			return "", nil, false, fmt.Errorf("invalid text content")
		}
		text := strings.TrimSpace(textContent.Text)
		if text == "" {
			return "", nil, false, nil
		}
		return text, nil, true, nil
	case "image":
		var imageContent larkImageContent
		if err := json.Unmarshal([]byte(content), &imageContent); err != nil {
			return "", nil, false, fmt.Errorf("invalid image content")
		}
		imageKey := strings.TrimSpace(imageContent.ImageKey)
		if imageKey == "" {
			return "", nil, false, nil
		}
		text := strings.TrimSpace(imageContent.Text)
		if text == "" {
			text = "User sent an image."
		}
		return text, []string{imageKey}, true, nil
	default:
		return "", nil, false, nil
	}
}

func collectLarkMentionUsers(items []*larkim.MentionEvent) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		openID := larkUserOpenID(item.Id)
		if openID == "" || seen[openID] {
			continue
		}
		seen[openID] = true
		out = append(out, openID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLarkEventTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC()
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Unix(0, ms*int64(time.Millisecond)).UTC()
}
