package lark

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type runtimeTaskOptions struct {
	MemoryEnabled           bool
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	PlanCreateClient        llm.Client
	PlanCreateModel         string
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
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
	EventID         string
}

const larkStickySkillsCap = 16

func runLarkTask(
	ctx context.Context,
	d Dependencies,
	logger *slog.Logger,
	logOpts agent.LogOptions,
	client llm.Client,
	baseReg *tools.Registry,
	sharedGuard *guard.Guard,
	cfg agent.Config,
	model string,
	job larkJob,
	history []chathistory.ChatHistoryItem,
	stickySkills []string,
	runtimeOpts runtimeTaskOptions,
) (*agent.Final, *agent.Context, []string, error) {
	ctx = llmstats.WithMetadata(ctx, job.TaskID, job.EventID)
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

	if baseReg == nil {
		return nil, nil, nil, fmt.Errorf("base registry is nil")
	}
	reg := buildLarkRegistry(baseReg, job.ChatType)
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    client,
		DefaultModel:     model,
		PlanCreateClient: runtimeOpts.PlanCreateClient,
		PlanCreateModel:  runtimeOpts.PlanCreateModel,
	})
	toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForLark(job))

	promptSpec, loadedSkills, err := depsutil.PromptSpecFromCommon(d, ctx, logger, logOpts, task, client, model, stickySkills)
	if err != nil {
		return nil, nil, nil, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	promptprofile.AppendLarkRuntimeBlocks(&promptSpec, isLarkGroupChat(job.ChatType))
	memSubjectID := larkMemorySubjectID(job)
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" && runtimeOpts.MemoryInjectionEnabled {
		snap, memErr := runtimeOpts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      memSubjectID,
			RequestContext: larkMemoryRequestContext(job.ChatType),
			MaxItems:       runtimeOpts.MemoryInjectionMaxItems,
		})
		if memErr != nil {
			if logger != nil {
				logger.Warn("memory_injection_error", "source", "lark", "subject_id", memSubjectID, "chat_id", job.ChatID, "error", memErr.Error())
			}
		} else if strings.TrimSpace(snap) != "" {
			promptprofile.AppendMemorySummariesBlock(&promptSpec, snap)
			if logger != nil {
				logger.Info("memory_injection_applied", "source", "lark", "subject_id", memSubjectID, "chat_id", job.ChatID, "snapshot_len", len(snap))
			}
		}
	}

	engine := agent.New(
		client,
		reg,
		cfg,
		promptSpec,
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
		agent.WithGuard(sharedGuard),
	)

	meta := map[string]any{
		"trigger":           "lark",
		"lark_chat_id":      job.ChatID,
		"lark_chat_type":    job.ChatType,
		"lark_open_id":      job.FromUserID,
		"lark_message_id":   job.MessageID,
		"lark_conversation": job.ConversationKey,
	}
	final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{
		Model:          model,
		Scene:          "lark.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
	})
	if err != nil {
		return final, runCtx, loadedSkills, err
	}
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" {
		finalOutput := strings.TrimSpace(depsutil.FormatFinalOutput(final))
		recordedAt := time.Now().UTC()
		recordOffset, memErr := runtimeOpts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
			TaskRunID:      strings.TrimSpace(job.TaskID),
			SessionID:      larkMemorySessionID(job),
			SubjectID:      memSubjectID,
			Channel:        "lark",
			Participants:   larkMemoryParticipants(job),
			TaskText:       task,
			FinalOutput:    finalOutput,
			SourceHistory:  buildLarkMemoryHistory(history, job, finalOutput, recordedAt),
			SessionContext: larkMemorySessionContext(job),
		})
		if memErr != nil {
			if logger != nil {
				logger.Warn("memory_record_error", "source", "lark", "subject_id", memSubjectID, "chat_id", job.ChatID, "error", memErr.Error())
			}
		} else {
			if logger != nil {
				logger.Debug("memory_record_ok", "source", "lark", "subject_id", memSubjectID, "chat_id", job.ChatID, "offset_file", recordOffset.File, "offset_line", recordOffset.Line)
			}
			if runtimeOpts.MemoryProjectionWorker != nil {
				runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
			}
		}
	}
	return final, runCtx, loadedSkills, nil
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
