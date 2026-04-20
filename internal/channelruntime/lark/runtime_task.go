package lark

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

type runtimeTaskOptions struct {
	MemoryEnabled           bool
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	MemoryOrchestrator      *memoryruntime.Orchestrator
	MemoryProjectionWorker  *memoryruntime.ProjectionWorker
}

type larkJob struct {
	TaskID          string
	ConversationKey string
	ChatID          string
	ChatType        string
	MessageID       string
	FromUserID      string
	DisplayName     string
	Text            string
	WorkspaceDir    string
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
	EventID         string
}

const larkStickySkillsCap = 16

func runLarkTask(
	ctx context.Context,
	rt *taskruntime.Runtime,
	job larkJob,
	history []chathistory.ChatHistoryItem,
	stickySkills []string,
	runtimeOpts runtimeTaskOptions,
) (*agent.Final, *agent.Context, []string, error) {
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("lark task runtime is nil")
	}
	ctx = llmstats.WithMetadata(ctx, job.TaskID, job.EventID)
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	ctx = builtin.WithContactsSendRuntimeContext(ctx, contactsSendRuntimeContextForLark(job))
	task := strings.TrimSpace(job.Text)
	if task == "" {
		return nil, nil, nil, fmt.Errorf("empty lark task")
	}
	historyMsg, currentMsg, err := buildLarkPromptMessages(history, job)
	if err != nil {
		return nil, nil, nil, err
	}
	var llmHistory []llm.Message
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
	}

	memSubjectID := larkMemorySubjectID(job)
	memoryHooks := taskruntime.MemoryHooks{
		Source:    "lark",
		SubjectID: memSubjectID,
		LogFields: map[string]any{"chat_id": job.ChatID},
	}
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" {
		memoryHooks.InjectionEnabled = runtimeOpts.MemoryInjectionEnabled
		memoryHooks.InjectionMaxItems = runtimeOpts.MemoryInjectionMaxItems
		memoryHooks.PrepareInjection = func(maxItems int) (string, error) {
			return runtimeOpts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
				SubjectID:      memSubjectID,
				RequestContext: larkMemoryRequestContext(job.ChatType),
				MaxItems:       maxItems,
			})
		}
		memoryHooks.Record = func(_ *agent.Final, finalOutput string) error {
			recordedAt := time.Now().UTC()
			_, err := runtimeOpts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
				TaskRunID:      strings.TrimSpace(job.TaskID),
				SessionID:      larkMemorySessionID(job),
				SubjectID:      memSubjectID,
				Channel:        "lark",
				Participants:   larkMemoryParticipants(job),
				TaskText:       task,
				FinalOutput:    strings.TrimSpace(finalOutput),
				SourceHistory:  buildLarkMemoryHistory(history, job, finalOutput, recordedAt),
				SessionContext: larkMemorySessionContext(job),
			})
			return err
		}
		memoryHooks.NotifyRecorded = func() {
			if runtimeOpts.MemoryProjectionWorker != nil {
				runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
			}
		}
	}

	meta := map[string]any{
		"trigger":           "lark",
		"lark_chat_id":      job.ChatID,
		"lark_chat_type":    job.ChatType,
		"lark_open_id":      job.FromUserID,
		"lark_message_id":   job.MessageID,
		"lark_conversation": job.ConversationKey,
	}
	result, err := rt.Run(ctx, taskruntime.RunRequest{
		Task:           task,
		Scene:          "lark.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
		StickySkills:   stickySkills,
		Registry:       buildLarkRegistry(rt.BaseRegistry, job.ChatType),
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
			toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForLark(job))
			promptprofile.AppendLarkRuntimeBlocks(spec, isLarkGroupChat(job.ChatType))
		},
		Memory: memoryHooks,
	})
	if err != nil {
		return result.Final, result.Context, result.LoadedSkills, err
	}
	return result.Final, result.Context, result.LoadedSkills, nil
}

func buildLarkPromptMessages(history []chathistory.ChatHistoryItem, job larkJob) (*llm.Message, *llm.Message, error) {
	historyRaw, err := chathistory.RenderHistoryContext(chathistory.ChannelLark, history)
	if err != nil {
		return nil, nil, fmt.Errorf("render lark history context: %w", err)
	}
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg := llm.Message{Role: "user", Content: historyRaw}
		historyMsg = &msg
	}
	currentRaw, err := chathistory.RenderCurrentMessage(newLarkInboundHistoryItem(job))
	if err != nil {
		return nil, nil, fmt.Errorf("render lark current message: %w", err)
	}
	current := llm.Message{Role: "user", Content: currentRaw}
	return historyMsg, &current, nil
}

func todoResolveContextForLark(job larkJob) todo.AddResolveContext {
	speaker := strings.TrimSpace(job.FromUserID)
	if speaker != "" {
		speaker = "lark_user:" + speaker
	}
	mentions := normalizeLarkMentionUsersForTodo(job.MentionUsers)
	return todo.AddResolveContext{
		Channel:          "lark",
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		SpeakerUsername:  speaker,
		MentionUsernames: mentions,
		UserInputRaw:     job.Text,
	}
}

func contactsSendRuntimeContextForLark(job larkJob) builtin.ContactsSendRuntimeContext {
	ids := make([]string, 0, 2)
	if openID := strings.TrimSpace(job.FromUserID); openID != "" {
		ids = append(ids, "lark_user:"+openID)
	}
	if chatID := strings.TrimSpace(job.ChatID); chatID != "" && !isLarkGroupChat(job.ChatType) {
		ids = append(ids, "lark:"+chatID)
	}
	return builtin.ContactsSendRuntimeContext{ForbiddenTargetIDs: ids}
}

func normalizeLarkMentionUsersForTodo(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		out = append(out, "lark_user:"+item)
	}
	return out
}

func newLarkInboundHistoryItem(job larkJob) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelLark,
		Kind:             chathistory.KindInboundUser,
		ChatID:           "lark:" + strings.TrimSpace(job.ChatID),
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		MessageID:        strings.TrimSpace(job.MessageID),
		ReplyToMessageID: strings.TrimSpace(job.MessageID),
		SentAt:           job.SentAt.UTC(),
		Sender:           larkSenderFromJob(job, false),
		Text:             strings.TrimSpace(job.Text),
	}
}

func larkJobFromInbound(inbound larkbus.InboundMessage) larkJob {
	return larkJob{
		ChatID:       strings.TrimSpace(inbound.ChatID),
		ChatType:     strings.TrimSpace(inbound.ChatType),
		MessageID:    strings.TrimSpace(inbound.MessageID),
		FromUserID:   strings.TrimSpace(inbound.FromUserID),
		DisplayName:  strings.TrimSpace(inbound.DisplayName),
		Text:         strings.TrimSpace(inbound.Text),
		SentAt:       inbound.SentAt.UTC(),
		MentionUsers: append([]string(nil), inbound.MentionUsers...),
		EventID:      strings.TrimSpace(inbound.EventID),
	}
}

func newLarkInboundHistoryItemFromInbound(inbound larkbus.InboundMessage) chathistory.ChatHistoryItem {
	return newLarkInboundHistoryItem(larkJobFromInbound(inbound))
}

func newLarkOutboundAgentHistoryItem(job larkJob, output string, sentAt time.Time) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelLark,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           "lark:" + strings.TrimSpace(job.ChatID),
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		ReplyToMessageID: strings.TrimSpace(job.MessageID),
		SentAt:           sentAt.UTC(),
		Sender:           larkSenderFromJob(job, true),
		Text:             strings.TrimSpace(output),
	}
}

func larkSenderFromJob(job larkJob, isBot bool) chathistory.ChatHistorySender {
	if isBot {
		return chathistory.ChatHistorySender{
			Username:   "lark-bot",
			Nickname:   "lark-bot",
			IsBot:      true,
			DisplayRef: "lark-bot",
		}
	}
	nickname := strings.TrimSpace(job.DisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(job.FromUserID)
	}
	return chathistory.ChatHistorySender{
		UserID:     strings.TrimSpace(job.FromUserID),
		Username:   strings.TrimSpace(job.FromUserID),
		Nickname:   nickname,
		IsBot:      false,
		DisplayRef: nickname,
	}
}

func larkHistoryCapForMode(mode string) int {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "talkative":
		return 16
	default:
		return 8
	}
}

func trimChatHistoryItems(items []chathistory.ChatHistoryItem, limit int) []chathistory.ChatHistoryItem {
	if limit <= 0 || len(items) <= limit {
		return append([]chathistory.ChatHistoryItem(nil), items...)
	}
	return append([]chathistory.ChatHistoryItem(nil), items[len(items)-limit:]...)
}

func capUniqueStrings(items []string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func publishLarkBusOutbound(ctx context.Context, inprocBus *busruntime.Inproc, chatID, text, replyToMessageID, correlationID string) (string, error) {
	if inprocBus == nil {
		return "", fmt.Errorf("bus is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	replyToMessageID = strings.TrimSpace(replyToMessageID)
	if chatID == "" {
		return "", fmt.Errorf("chat_id is required")
	}
	if text == "" {
		return "", fmt.Errorf("text is required")
	}

	now := time.Now().UTC()
	messageID := "msg_" + uuid.NewString()
	sessionUUID, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	sessionID := sessionUUID.String()
	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: messageID,
		Text:      text,
		SentAt:    now.Format(time.RFC3339),
		SessionID: sessionID,
		ReplyTo:   replyToMessageID,
	})
	if err != nil {
		return "", err
	}
	conversationKey, err := busruntime.BuildLarkConversationKey(chatID)
	if err != nil {
		return "", err
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		correlationID = "lark:" + messageID
	}
	outbound := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelLark,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(messageID),
		CorrelationID:   correlationID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			SessionID: sessionID,
			ReplyTo:   replyToMessageID,
			ChannelID: chatID,
		},
	}
	if err := inprocBus.PublishValidated(ctx, outbound); err != nil {
		return "", err
	}
	return messageID, nil
}

func isLarkGroupChat(chatType string) bool {
	return strings.EqualFold(strings.TrimSpace(chatType), "group")
}

func buildLarkRegistry(baseReg *tools.Registry, chatType string) *tools.Registry {
	reg := tools.NewRegistry()
	if baseReg == nil {
		return reg
	}
	groupChat := isLarkGroupChat(chatType)
	for _, t := range baseReg.All() {
		name := strings.TrimSpace(t.Name())
		if groupChat && strings.EqualFold(name, "contacts_send") {
			continue
		}
		reg.Register(t)
	}
	return reg
}

func shouldPublishLarkText(final *agent.Final) bool {
	_ = final
	return true
}
