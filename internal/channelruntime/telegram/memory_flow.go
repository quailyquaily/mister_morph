package telegram

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/memory"
)

func recordMemoryFromJob(logger *slog.Logger, orchestrator *memoryruntime.Orchestrator, job telegramJob, history []chathistory.ChatHistoryItem, historyCap int, final *agent.Final) error {
	taskRunID := strings.TrimSpace(job.TaskID)
	if taskRunID == "" {
		return fmt.Errorf("telegram task run id is required")
	}
	output := outputfmt.FormatFinalOutput(final)
	now := time.Now().UTC()
	meta := buildMemoryWriteMeta(job)

	ctxInfo := memory.SessionContext{
		ConversationType:   strings.TrimSpace(job.ChatType),
		CounterpartyName:   strings.TrimSpace(job.FromDisplayName),
		CounterpartyHandle: strings.TrimSpace(job.FromUsername),
	}
	if job.ChatID != 0 {
		ctxInfo.ConversationID = telegramConversationID(job)
	}
	if job.FromUserID > 0 {
		ctxInfo.CounterpartyID = strconv.FormatInt(job.FromUserID, 10)
	}
	if ctxInfo.CounterpartyName == "" {
		ctxInfo.CounterpartyName = strings.TrimSpace(strings.Join([]string{job.FromFirstName, job.FromLastName}, " "))
	}
	ctxInfo.CounterpartyLabel = buildMemoryCounterpartyLabel(meta, ctxInfo)

	if err := orchestrator.Record(memoryruntime.RecordRequest{
		TaskRunID:      taskRunID,
		SessionID:      telegramMemorySessionID(job),
		SubjectID:      telegramMemorySessionID(job),
		Channel:        "telegram",
		Participants:   telegramMemoryParticipants(job),
		TaskText:       strings.TrimSpace(job.Text),
		FinalOutput:    output,
		SourceHistory:  buildMemoryDraftHistory(history, job, output, now, historyCap),
		SessionContext: ctxInfo,
	}); err != nil {
		return err
	}
	if logger != nil {
		logger.Debug("memory_record_ok",
			"source", "telegram",
			"subject_id", telegramMemorySessionID(job),
		)
	}
	return nil
}

func buildMemoryWriteMeta(job telegramJob) memory.WriteMeta {
	meta := memory.WriteMeta{SessionID: telegramMemorySessionID(job)}
	if contactID := telegramMemoryContactID(job.FromUsername, job.FromUserID); contactID != "" {
		meta.ContactIDs = []string{contactID}
	}
	contactNickname := strings.TrimSpace(job.FromDisplayName)
	if contactNickname == "" {
		contactNickname = strings.TrimSpace(strings.Join([]string{job.FromFirstName, job.FromLastName}, " "))
	}
	if contactNickname != "" {
		meta.ContactNicknames = []string{contactNickname}
	}
	return meta
}

func telegramMemorySessionID(job telegramJob) string {
	return "tg:" + telegramConversationID(job)
}

func telegramConversationID(job telegramJob) string {
	id := strconv.FormatInt(job.ChatID, 10)
	if job.MessageThreadID > 0 {
		id += "_" + strconv.FormatInt(job.MessageThreadID, 10)
	}
	return id
}

func telegramMemoryRequestContext(chatType string) memory.RequestContext {
	if strings.EqualFold(strings.TrimSpace(chatType), "private") {
		return memory.ContextPrivate
	}
	return memory.ContextPublic
}

func telegramMemoryParticipants(job telegramJob) []memory.MemoryParticipant {
	seen := make(map[string]bool, 1+len(job.MentionUsers))
	out := make([]memory.MemoryParticipant, 0, 1+len(job.MentionUsers))
	appendParticipant := func(id string, nickname string) {
		id = strings.TrimSpace(id)
		nickname = strings.TrimSpace(nickname)
		if id == "" || nickname == "" {
			return
		}
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, memory.MemoryParticipant{
			ID:       id,
			Nickname: nickname,
			Protocol: "tg",
		})
	}

	if senderID := telegramSenderParticipantID(job.FromUsername, job.FromUserID); senderID != "" {
		senderName := strings.TrimSpace(job.FromDisplayName)
		if senderName == "" {
			senderName = strings.TrimSpace(strings.Join([]string{job.FromFirstName, job.FromLastName}, " "))
		}
		if senderName == "" {
			senderName = senderID
		}
		appendParticipant(senderID, senderName)
	}
	for _, raw := range job.MentionUsers {
		mentionID := normalizeTelegramMentionID(raw)
		if mentionID == "" {
			continue
		}
		appendParticipant(mentionID, mentionID)
	}

	if len(out) == 0 {
		return nil
	}
	return out
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

func buildMemoryDraftHistory(history []chathistory.ChatHistoryItem, job telegramJob, output string, now time.Time, maxItems int) []chathistory.ChatHistoryItem {
	out := append([]chathistory.ChatHistoryItem{}, history...)
	inbound := newTelegramInboundHistoryItem(job)
	inbound.Images = imagehistory.WithDescription(inbound.Images, output, "agent_final")
	out = append(out, inbound)
	if strings.TrimSpace(output) != "" {
		out = append(out, newTelegramOutboundAgentHistoryItem(job.ChatID, job.ChatType, output, now, ""))
	}
	if maxItems > 0 && len(out) > maxItems {
		out = out[len(out)-maxItems:]
	}
	return out
}

func buildMemoryCounterpartyLabel(meta memory.WriteMeta, ctxInfo memory.SessionContext) string {
	contactID := firstNonEmptyString(meta.ContactIDs...)
	if contactID == "" {
		handle := strings.TrimPrefix(strings.TrimSpace(ctxInfo.CounterpartyHandle), "@")
		if handle != "" {
			contactID = "tg:@" + handle
		}
	}
	nickname := firstNonEmptyString(meta.ContactNicknames...)
	if nickname == "" {
		nickname = strings.TrimSpace(ctxInfo.CounterpartyName)
	}
	if nickname == "" {
		nickname = strings.TrimPrefix(strings.TrimPrefix(contactID, "tg:@"), "tg:")
	}
	nickname = sanitizeTelegramReferenceLabel(nickname)
	if nickname != "" && contactID != "" {
		return "[" + nickname + "](" + contactID + ")"
	}
	if nickname != "" {
		return nickname
	}
	return strings.TrimSpace(contactID)
}

func firstNonEmptyString(values ...string) string {
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v != "" {
			return v
		}
	}
	return ""
}
