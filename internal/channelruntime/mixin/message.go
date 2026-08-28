package mixin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

type mixinAttachmentPayload struct {
	AttachmentID string `json:"attachment_id"`
	MimeType     string `json:"mime_type"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
}

func decodeMixinAttachment(category, dataBase64 string) (mixinAttachmentPayload, bool, error) {
	switch strings.ToUpper(strings.TrimSpace(category)) {
	case mixinapi.MessageCategoryPlainImage, mixinapi.MessageCategoryPlainAudio, mixinapi.MessageCategoryPlainData:
	default:
		return mixinAttachmentPayload{}, false, nil
	}
	raw, err := decodeMixinBase64(strings.TrimSpace(dataBase64))
	if err != nil {
		return mixinAttachmentPayload{}, true, fmt.Errorf("decode mixin attachment: %w", err)
	}
	var payload mixinAttachmentPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return mixinAttachmentPayload{}, true, fmt.Errorf("decode mixin attachment: %w", err)
	}
	payload.AttachmentID = strings.TrimSpace(payload.AttachmentID)
	payload.MimeType = strings.TrimSpace(payload.MimeType)
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.AttachmentID == "" {
		return mixinAttachmentPayload{}, true, fmt.Errorf("decode mixin attachment: attachment_id is required")
	}
	return payload, true, nil
}

func decodeMixinText(category, dataBase64 string) (string, bool, error) {
	switch strings.ToUpper(strings.TrimSpace(category)) {
	case mixinapi.MessageCategoryPlainText, mixinapi.MessageCategoryPlainPost:
	default:
		return "", false, nil
	}
	raw, err := decodeMixinBase64(strings.TrimSpace(dataBase64))
	if err != nil {
		return "", true, fmt.Errorf("decode mixin text: %w", err)
	}
	if !utf8.Valid(raw) {
		return "", true, fmt.Errorf("decode mixin text: payload is not UTF-8")
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", true, fmt.Errorf("decode mixin text: payload is empty")
	}
	return text, true, nil
}

func decodeMixinBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if raw, err := encoding.DecodeString(value); err == nil {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}

func mixinBotMentioned(text, identityNumber string) bool {
	return findMixinBotMention(text, identityNumber) >= 0
}

func stripMixinBotMention(text, identityNumber string) string {
	mention := "@" + strings.TrimSpace(identityNumber)
	if mention == "@" {
		return strings.TrimSpace(text)
	}
	for {
		index := findMixinBotMention(text, identityNumber)
		if index < 0 {
			return strings.TrimSpace(text)
		}
		text = text[:index] + text[index+len(mention):]
	}
}

func findMixinBotMention(text, identityNumber string) int {
	identityNumber = strings.TrimSpace(identityNumber)
	if identityNumber == "" {
		return -1
	}
	mention := "@" + identityNumber
	from := 0
	for {
		index := strings.Index(text[from:], mention)
		if index < 0 {
			return -1
		}
		index += from
		after := index + len(mention)
		if after == len(text) || text[after] < '0' || text[after] > '9' {
			return index
		}
		from = after
	}
}

func mixinGroupCommandAllowed(text string, explicit bool) bool {
	command, _ := chatcommands.ParseCommand(text)
	if chatcommands.NormalizeCommand(command) == "" {
		return true
	}
	return explicit
}

func splitMixinText(text string, maxBytes int) []string {
	if maxBytes <= 0 || len([]byte(text)) <= maxBytes {
		return []string{text}
	}
	parts := make([]string, 0, len(text)/maxBytes+1)
	start := 0
	size := 0
	for index, r := range text {
		runeBytes := utf8.RuneLen(r)
		if size > 0 && size+runeBytes > maxBytes {
			parts = append(parts, text[start:index])
			start = index
			size = 0
		}
		size += runeBytes
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
}
