package chathistory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const historyBoundaryPrefix = "history:v1:"

func BoundaryForItem(item ChatHistoryItem) string {
	stable := struct {
		Channel   string `json:"channel"`
		Kind      string `json:"kind"`
		ChatID    string `json:"chat_id"`
		MessageID string `json:"message_id"`
	}{
		Channel:   strings.TrimSpace(item.Channel),
		Kind:      strings.TrimSpace(item.Kind),
		ChatID:    strings.TrimSpace(item.ChatID),
		MessageID: strings.TrimSpace(item.MessageID),
	}
	var raw []byte
	if stable.MessageID != "" {
		raw, _ = json.Marshal(stable)
	} else {
		if stable.Channel == "" && stable.Kind == "" && stable.ChatID == "" && item.SentAt.IsZero() && strings.TrimSpace(item.Text) == "" {
			return ""
		}
		raw, _ = json.Marshal(item)
	}
	hash := sha256.Sum256(raw)
	return historyBoundaryPrefix + hex.EncodeToString(hash[:])
}

func FilterAfterBoundary(items []ChatHistoryItem, coveredThrough string) []ChatHistoryItem {
	coveredThrough = strings.TrimSpace(coveredThrough)
	if len(items) == 0 {
		return nil
	}
	if coveredThrough == "" {
		return cloneHistoryItems(items)
	}
	for index := len(items) - 1; index >= 0; index-- {
		if BoundaryForItem(items[index]) == coveredThrough {
			return cloneHistoryItems(items[index+1:])
		}
	}
	return cloneHistoryItems(items)
}

func cloneHistoryItems(items []ChatHistoryItem) []ChatHistoryItem {
	if len(items) == 0 {
		return nil
	}
	out := append([]ChatHistoryItem(nil), items...)
	for i := range out {
		if items[i].Quote != nil {
			quote := *items[i].Quote
			out[i].Quote = &quote
		}
		out[i].Images = append([]ChatHistoryImage(nil), items[i].Images...)
	}
	return out
}
