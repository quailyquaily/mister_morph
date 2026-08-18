package line

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

type runtimeTaskOptions struct {
	MemoryEnabled           bool
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	FileCacheDir            string
	MemoryOrchestrator      *memoryruntime.Orchestrator
	MemoryProjectionWorker  *memoryruntime.ProjectionWorker
}

const lineStickySkillsCap = 16

func runLineTask(
	ctx context.Context,
	rt *taskruntime.Runtime,
	job lineJob,
	history []chathistory.ChatHistoryItem,
	stickySkills []string,
	runtimeOpts runtimeTaskOptions,
	steerSource agent.SteerSource,
) (*agent.Final, *agent.Context, []string, error) {
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("line task runtime is nil")
	}
	ctx = llmstats.WithMetadata(ctx, job.TaskID, job.EventID)
	ctx = topiccontext.WithScope(ctx, topiccontext.Scope{
		Runtime:         "line",
		ConversationKey: job.ConversationKey,
		TopicID:         job.ChatID,
	})
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	ctx = builtin.WithContactsSendRuntimeContext(ctx, contactsSendRuntimeContextForLine(job))
	logger := rt.Logger
	task := strings.TrimSpace(job.Text)
	routePurpose := ""
	reasoningEffort := ""
	if thinkTask, ok := chatcommands.ExtractThinkTask(task); ok {
		task = strings.TrimSpace(thinkTask)
		job.Text = task
		routePurpose = llmutil.RoutePurposeThink
		reasoningEffort = llmutil.ReasoningEffortXHigh
	}
	if task == "" {
		return nil, nil, nil, fmt.Errorf("empty line task")
	}
	var mainRoute llmutil.ResolvedRoute
	if job.Route != nil {
		mainRoute = *job.Route
	} else {
		resolvedRoute, err := rt.ResolveRouteForRun(ctx, routePurpose)
		if err != nil {
			return nil, nil, nil, err
		}
		mainRoute = resolvedRoute
	}
	if reasoningEffort != "" {
		mainRoute = llmutil.ResolvedRouteWithReasoningEffort(mainRoute, reasoningEffort)
	}
	mainModel := strings.TrimSpace(mainRoute.ClientConfig.Model)
	checkpointHistory, err := rt.PrepareContextHistory(ctx, job.ConversationKey, history, newLineInboundHistoryItem(job))
	if err != nil {
		return nil, nil, nil, err
	}
	historyMsg, currentMsg, err := buildLinePromptMessagesWithImageNotes(checkpointHistory.History, job, mainModel, mainRoute.Values.SupportsImageParts, runtimeOpts.FileCacheDir, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	var llmHistory []llm.Message
	var historyBoundaries []string
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
		historyBoundaries = []string{checkpointHistory.HistoryBoundary}
	}

	memSubjectID := lineMemorySubjectID(job)
	memoryHooks := taskruntime.MemoryHooks{
		Source:    "line",
		SubjectID: memSubjectID,
		LogFields: map[string]any{"chat_id": job.ChatID},
	}
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" {
		memoryHooks.InjectionEnabled = runtimeOpts.MemoryInjectionEnabled
		memoryHooks.InjectionMaxItems = runtimeOpts.MemoryInjectionMaxItems
		memoryHooks.PrepareInjection = func(maxItems int) (string, error) {
			return runtimeOpts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
				SubjectID:      memSubjectID,
				RequestContext: lineMemoryRequestContext(job.ChatType),
				MaxItems:       maxItems,
			})
		}
		memoryHooks.Record = func(_ *agent.Final, finalOutput string) error {
			recordedAt := time.Now().UTC()
			return runtimeOpts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
				TaskRunID:      strings.TrimSpace(job.TaskID),
				SessionID:      lineMemorySessionID(job),
				SubjectID:      memSubjectID,
				Channel:        "line",
				Participants:   lineMemoryParticipants(job),
				TaskText:       task,
				FinalOutput:    strings.TrimSpace(finalOutput),
				SourceHistory:  buildLineMemoryHistory(history, job, finalOutput, recordedAt),
				SessionContext: lineMemorySessionContext(job),
			})
		}
		memoryHooks.NotifyRecorded = func() {
			if runtimeOpts.MemoryProjectionWorker != nil {
				runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
			}
		}
	}

	meta := map[string]any{
		"trigger":         "line",
		"line_chat_id":    job.ChatID,
		"line_chat_type":  job.ChatType,
		"line_user_id":    job.FromUserID,
		"line_message_id": job.MessageID,
	}
	meta = taskruntime.ApplyObservationMeta(meta, taskruntime.ObservationMetaIDs{
		TaskID:        job.TaskID,
		TraceID:       job.TaskID,
		TopicID:       job.ChatID,
		OriginEventID: job.EventID,
	})
	result, err := rt.Run(ctx, taskruntime.RunRequest{
		Task:                    task,
		Model:                   mainModel,
		Route:                   &mainRoute,
		RoutePurpose:            routePurpose,
		ReasoningEffortOverride: reasoningEffort,
		Scene:                   "line.loop",
		History:                 llmHistory,
		Meta:                    meta,
		CurrentMessage:          currentMsg,
		StickySkills:            stickySkills,
		Registry:                buildLineRegistry(rt.BaseRegistry, job.ChatType),
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
			toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForLine(job))
			promptprofile.AppendLineRuntimeBlocks(spec, isLineGroupChat(job.ChatType))
		},
		SteerSource:            steerSource,
		Memory:                 memoryHooks,
		ImageToolScope:         strings.TrimSpace(job.ConversationKey),
		ImageToolRetention:     toolsutil.ImageToolRetentionCountdown,
		ContextCheckpointStore: checkpointHistory.Store,
		HistoryBoundaries:      historyBoundaries,
		CurrentMessageBoundary: checkpointHistory.CurrentMessageBoundary,
	})
	if err != nil {
		return result.Final, result.Context, result.LoadedSkills, err
	}
	return result.Final, result.Context, result.LoadedSkills, nil
}

func buildLinePromptMessagesWithImageNotes(history []chathistory.ChatHistoryItem, job lineJob, model string, supportsImageParts *bool, fileCacheDir string, logger *slog.Logger) (*llm.Message, *llm.Message, error) {
	historyRaw := chathistory.RenderHistoryContext(history)
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg := llm.Message{Role: "user", Content: historyRaw}
		historyMsg = &msg
	}

	currentRaw := chathistory.RenderCurrentMessage(newLineInboundHistoryItem(job))
	if len(job.Images) > 0 {
		currentRaw = imageinput.AppendImageMetadataNotes(currentRaw, job.Images)
	} else {
		currentRaw = imageinput.AppendImagePathNotes(currentRaw, job.ImagePaths, fileCacheDir)
	}
	currentMsg, err := buildLineCurrentMessage(currentRaw, model, supportsImageParts, job.ImagePaths, logger)
	if err != nil {
		return nil, nil, err
	}
	return historyMsg, &currentMsg, nil
}

func todoResolveContextForLine(job lineJob) todo.AddResolveContext {
	speaker := strings.TrimSpace(job.FromUserID)
	if speaker != "" {
		speaker = "line:" + speaker
	}
	return todo.AddResolveContext{
		Channel:          "line",
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		SpeakerUsername:  speaker,
		MentionUsernames: normalizeLineMentionUsersForTodo(job.MentionUsers),
		UserInputRaw:     job.Text,
	}
}

func contactsSendRuntimeContextForLine(job lineJob) builtin.ContactsSendRuntimeContext {
	ids := make([]string, 0, 2)
	if userID := strings.TrimSpace(job.FromUserID); userID != "" {
		ids = append(ids, "line_user:"+userID)
	}
	if chatID := strings.TrimSpace(job.ChatID); chatID != "" && !isLineGroupChat(job.ChatType) {
		ids = append(ids, "line:"+chatID)
	}
	return builtin.ContactsSendRuntimeContext{ForbiddenTargetIDs: ids}
}

func normalizeLineMentionUsersForTodo(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		out = append(out, "line:"+item)
	}
	return out
}

func newLineInboundHistoryItem(job lineJob) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelLine,
		Kind:             chathistory.KindInboundUser,
		ChatID:           "line:" + strings.TrimSpace(job.ChatID),
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		MessageID:        strings.TrimSpace(job.MessageID),
		ReplyToMessageID: strings.TrimSpace(job.ReplyToken),
		SentAt:           job.SentAt.UTC(),
		Sender:           lineSenderFromJob(job, false),
		Text:             strings.TrimSpace(job.Text),
		Images:           append([]chathistory.ChatHistoryImage(nil), job.Images...),
	}
}

func lineJobFromInbound(inbound linebus.InboundMessage) lineJob {
	return lineJob{
		ChatID:       strings.TrimSpace(inbound.ChatID),
		ChatType:     strings.TrimSpace(inbound.ChatType),
		MessageID:    strings.TrimSpace(inbound.MessageID),
		ReplyToken:   strings.TrimSpace(inbound.ReplyToken),
		FromUserID:   strings.TrimSpace(inbound.FromUserID),
		FromUsername: strings.TrimSpace(inbound.FromUsername),
		DisplayName:  strings.TrimSpace(inbound.DisplayName),
		Text:         strings.TrimSpace(inbound.Text),
		ImagePaths:   busruntime.ImagePathsFromAttachments(inbound.ImageAttachments),
		SentAt:       inbound.SentAt.UTC(),
	}
}

func newLineOutboundAgentHistoryItem(job lineJob, output string, sentAt time.Time) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelLine,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           "line:" + strings.TrimSpace(job.ChatID),
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		ReplyToMessageID: strings.TrimSpace(job.ReplyToken),
		SentAt:           sentAt.UTC(),
		Sender:           lineSenderFromJob(job, true),
		Text:             strings.TrimSpace(output),
	}
}

func lineSenderFromJob(job lineJob, isBot bool) chathistory.ChatHistorySender {
	if isBot {
		return chathistory.ChatHistorySender{
			Username:   "line-bot",
			Nickname:   "line-bot",
			IsBot:      true,
			DisplayRef: "line-bot",
		}
	}
	nickname := strings.TrimSpace(job.DisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(job.FromUsername)
	}
	if nickname == "" {
		nickname = strings.TrimSpace(job.FromUserID)
	}
	username := strings.TrimSpace(job.FromUsername)
	if username == "" {
		username = strings.TrimSpace(job.FromUserID)
	}
	displayRef := nickname
	if displayRef == "" {
		displayRef = username
	}
	return chathistory.ChatHistorySender{
		UserID:     strings.TrimSpace(job.FromUserID),
		Username:   username,
		Nickname:   nickname,
		DisplayRef: displayRef,
	}
}

func lineHistoryCapForMode(mode string) int {
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

func publishLineBusOutbound(ctx context.Context, inprocBus *busruntime.Inproc, chatID, text, replyToken, correlationID string) (string, error) {
	if inprocBus == nil {
		return "", fmt.Errorf("bus is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	replyToken = strings.TrimSpace(replyToken)
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
		ReplyTo:   replyToken,
	})
	if err != nil {
		return "", err
	}
	conversationKey, err := busruntime.BuildLineConversationKey(chatID)
	if err != nil {
		return "", err
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		correlationID = "line:" + messageID
	}
	outbound := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelLine,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(messageID),
		CorrelationID:   correlationID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			SessionID: sessionID,
			ReplyTo:   replyToken,
			ChannelID: chatID,
		},
	}
	if err := inprocBus.PublishValidated(ctx, outbound); err != nil {
		return "", err
	}
	return messageID, nil
}

func isLineGroupChat(chatType string) bool {
	return strings.EqualFold(strings.TrimSpace(chatType), "group")
}

func buildLineRegistry(baseReg *tools.Registry, chatType string) *tools.Registry {
	reg := baseReg.Clone()
	if isLineGroupChat(chatType) {
		reg.Remove(toolsutil.BuiltinContactsSend)
	}
	return reg
}
