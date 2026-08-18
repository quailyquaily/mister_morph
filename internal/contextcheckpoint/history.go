package contextcheckpoint

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/llm"
)

type PreparedHistory struct {
	Store                  *FileStore
	History                []chathistory.ChatHistoryItem
	HistoryBoundary        string
	CurrentMessageBoundary string
}

func PrepareHistory(ctx context.Context, root string, conversationKey string, history []chathistory.ChatHistoryItem, current chathistory.ChatHistoryItem) (PreparedHistory, error) {
	store, err := NewFileStore(root, conversationKey)
	if err != nil {
		return PreparedHistory{}, err
	}
	checkpoint, found, err := store.Load(ctx)
	if err != nil {
		return PreparedHistory{}, fmt.Errorf("load context checkpoint: %w", err)
	}
	coveredThrough := ""
	if found {
		coveredThrough = checkpoint.CoveredThrough
	}
	filtered := chathistory.FilterAfterBoundary(history, coveredThrough)
	prepared := PreparedHistory{
		Store:                  store,
		History:                filtered,
		CurrentMessageBoundary: chathistory.BoundaryForItem(current),
	}
	if len(filtered) > 0 {
		prepared.HistoryBoundary = chathistory.BoundaryForItem(filtered[len(filtered)-1])
	}
	return prepared, nil
}

func Reset(ctx context.Context, root string, conversationKey string) error {
	if strings.TrimSpace(conversationKey) == "" {
		return nil
	}
	store, err := NewFileStore(root, conversationKey)
	if err != nil {
		return err
	}
	checkpoint, found, err := store.Load(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return store.Delete(ctx, checkpoint.Revision)
}

func FilterMessageHistory(messages []llm.Message, boundaries []string, coveredThrough string) ([]llm.Message, []string) {
	start := 0
	coveredThrough = strings.TrimSpace(coveredThrough)
	if coveredThrough != "" {
		for index := len(messages) - 1; index >= 0; index-- {
			if index < len(boundaries) && strings.TrimSpace(boundaries[index]) == coveredThrough {
				start = index + 1
				break
			}
		}
	}
	filteredMessages := append([]llm.Message(nil), messages[start:]...)
	filteredBoundaries := make([]string, len(filteredMessages))
	for index := range filteredMessages {
		sourceIndex := start + index
		if sourceIndex < len(boundaries) {
			filteredBoundaries[index] = boundaries[sourceIndex]
		}
	}
	return filteredMessages, filteredBoundaries
}
