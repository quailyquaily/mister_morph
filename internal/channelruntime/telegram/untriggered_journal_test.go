package telegram

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestTelegramRejectedGroupMessageWritesOneJournalEvent(t *testing.T) {
	dir := t.TempDir()
	recorder, err := runtimecore.NewUntriggeredRecorder(dir, 0)
	if err != nil {
		t.Fatalf("NewUntriggeredRecorder() error = %v", err)
	}
	state := &telegramRuntimeState{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		allowedChatIDs:      map[int64]bool{-1001: true},
		botUser:             "morphbot",
		botID:               999,
		groupTriggerMode:    "strict",
		history:             make(map[string][]chathistory.ChatHistoryItem),
		sharedRuntime:       runtimecore.ChannelRuntimeBundle{TaskRuntime: &taskruntime.Runtime{}},
		untriggeredRecorder: recorder,
	}
	state.handleUpdate(telegramUpdate{Message: &telegramMessage{
		MessageID:       42,
		MessageThreadID: 8,
		Date:            1786071845,
		Chat:            &telegramChat{ID: -1001, Type: "supergroup"},
		From:            &telegramUser{ID: 10001, FirstName: "Ada"},
		Text:            "release at nine",
	}})
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	count := 0
	if err := domainjournal.ReplayDir(dir, func(rec domainjournal.Record) error {
		count++
		if rec.Event.Domain != "conversation" || rec.Event.Type != "untriggered_inbound" {
			t.Fatalf("event = %#v", rec.Event)
		}
		var payload struct {
			ConversationKey string `json:"conversation_key"`
			MessageID       string `json:"message_id"`
			SenderID        string `json:"sender_id"`
		}
		if err := json.Unmarshal(rec.Event.Payload, &payload); err != nil {
			return err
		}
		if payload.ConversationKey != "tg:-1001_8" || payload.MessageID != "42" || payload.SenderID != "10001" {
			t.Fatalf("payload = %#v", payload)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}
