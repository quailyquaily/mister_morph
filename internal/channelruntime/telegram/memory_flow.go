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
	err := orchestrator.RecordWithAdapter(telegramMemoryRecordAdapter{
		job:        job,
		history:    history,
		historyCap: historyCap,
		final:      final,
	})
	if err != nil {
		return err
	}
	if logger != nil {
		logger.Debug("memory_record_ok",
			"source", "telegram",
			"subject_id", telegramMemorySubjectID(job),
		)
	}
	return nil
}

type telegramMemoryInjectionAdapter struct {
	job telegramJob
}

func (a telegramMemoryInjectionAdapter) ResolveSubjectID() (string, error) {
	return telegramMemorySubjectID(a.job), nil
}

func (a telegramMemoryInjectionAdapter) ResolveRequestContext() (memory.RequestContext, error) {
	return telegramMemoryRequestContext(a.job.ChatType), nil
}

type telegramMemoryRecordAdapter struct {
	job        telegramJob
	history    []chathistory.ChatHistoryItem
	historyCap int
	final      *agent.Final
}

func (a telegramMemoryRecordAdapter) BuildRecordRequest() (memoryruntime.RecordRequest, error) {
	output := outputfmt.FormatFinalOutput(a.final)
	now := time.Now().UTC()
	meta := buildMemoryWriteMeta(a.job)
	taskRunID := strings.TrimSpace(a.job.TaskID)
	if taskRunID == "" {
		return memoryruntime.RecordRequest{}, fmt.Errorf("telegram task run id is required")
	}

	ctxInfo := memory.SessionContext{
		ConversationType:   strings.TrimSpace(a.job.ChatType),
		CounterpartyName:   strings.TrimSpace(a.job.FromDisplayName),
		CounterpartyHandle: strings.TrimSpace(a.job.FromUsername),
	}
	if a.job.ChatID != 0 {
		ctxInfo.ConversationID = telegramConversationID(a.job)
	}
	if a.job.FromUserID > 0 {
		ctxInfo.CounterpartyID = strconv.FormatInt(a.job.FromUserID, 10)
	}
	if ctxInfo.CounterpartyName == "" {
		ctxInfo.CounterpartyName = strings.TrimSpace(strings.Join([]string{a.job.FromFirstName, a.job.FromLastName}, " "))
	}
	ctxInfo.CounterpartyLabel = buildMemoryCounterpartyLabel(meta, ctxInfo)

	draftHistory := buildMemoryDraftHistory(a.history, a.job, output, now, a.historyCap)

	return memoryruntime.RecordRequest{
		TaskRunID:      taskRunID,
		SessionID:      telegramMemorySessionID(a.job),
		SubjectID:      telegramMemorySubjectID(a.job),
		Channel:        "telegram",
		Participants:   telegramMemoryParticipants(a.job),
		TaskText:       strings.TrimSpace(a.job.Text),
		FinalOutput:    output,
		SourceHistory:  draftHistory,
		SessionContext: ctxInfo,
	}, nil
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

func telegramMemorySubjectID(job telegramJob) string {
	return telegramMemorySessionID(job)
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
