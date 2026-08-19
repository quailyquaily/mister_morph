package lark

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	larktools "github.com/quailyquaily/mistermorph/tools/lark"
)

type runtimeTaskOptions struct {
	FileCacheDir     string
	ToolAPI          larktools.API
	ToolFileMaxBytes int64
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
	ImagePaths      []string
	Images          []chathistory.ChatHistoryImage
	WorkspaceDir    string
	Route           *llmutil.ResolvedRoute
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
	EventID         string
	Generation      *runtimecore.RuntimeGenerationLease
}

func (j larkJob) runtimeBundle() *runtimecore.ChannelRuntimeBundle {
	if j.Generation == nil {
		return nil
	}
	return j.Generation.Bundle()
}

func (j larkJob) releaseGeneration() {
	if j.Generation != nil {
		j.Generation.Release()
	}
}

const larkStickySkillsCap = 16

const larkToolFileMaxBytes = int64(20 * 1024 * 1024)

func runLarkTask(
	ctx context.Context,
	rt *taskruntime.Runtime,
	job larkJob,
	history []chathistory.ChatHistoryItem,
	stickySkills []string,
	runtimeOpts runtimeTaskOptions,
	steerSource agent.SteerSource,
) (*agent.Final, *agent.Context, []string, error) {
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("lark task runtime is nil")
	}
	ctx = llmstats.WithMetadata(ctx, job.TaskID, job.EventID)
	ctx = topiccontext.WithScope(ctx, topiccontext.Scope{
		Runtime:         "lark",
		ConversationKey: job.ConversationKey,
		TopicID:         job.ChatID,
	})
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	ctx = builtin.WithContactsSendRuntimeContext(ctx, contactsSendRuntimeContextForLark(job))
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
		return nil, nil, nil, fmt.Errorf("empty lark task")
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
	checkpointHistory, err := rt.PrepareContextHistory(ctx, job.ConversationKey, history, newLarkInboundHistoryItem(job))
	if err != nil {
		return nil, nil, nil, err
	}
	historyMsg, currentMsg, err := buildLarkPromptMessagesWithImageNotes(checkpointHistory.History, job, mainModel, mainRoute.Values.SupportsImageParts, runtimeOpts.FileCacheDir, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	var llmHistory []llm.Message
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
	}
	var historyBoundaries []string
	if historyMsg != nil {
		historyBoundaries = []string{checkpointHistory.HistoryBoundary}
	}

	reg := buildLarkRegistry(rt.BaseRegistry, job.ChatType)
	reactTool, err := registerLarkChannelTools(reg, runtimeOpts.ToolAPI, job.ChatID, job.MessageID, runtimeOpts.FileCacheDir, runtimeOpts.ToolFileMaxBytes)
	if err != nil {
		return nil, nil, nil, err
	}

	meta := map[string]any{
		"trigger":           "lark",
		"lark_chat_id":      job.ChatID,
		"lark_chat_type":    job.ChatType,
		"lark_open_id":      job.FromUserID,
		"lark_message_id":   job.MessageID,
		"lark_conversation": job.ConversationKey,
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
		Scene:                   "lark.loop",
		History:                 llmHistory,
		Meta:                    meta,
		CurrentMessage:          currentMsg,
		StickySkills:            stickySkills,
		Registry:                reg,
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
			toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForLark(job))
			promptprofile.AppendLarkRuntimeBlocks(spec, isLarkGroupChat(job.ChatType), strings.Join(larktools.StandardReactionEmojiTypes(), ","))
		},
		SteerSource:            steerSource,
		ImageToolScope:         strings.TrimSpace(job.ConversationKey),
		ImageToolRetention:     toolsutil.ImageToolRetentionCountdown,
		ContextCheckpointStore: checkpointHistory.Store,
		HistoryBoundaries:      historyBoundaries,
		CurrentMessageBoundary: checkpointHistory.CurrentMessageBoundary,
	})
	if err != nil {
		return result.Final, result.Context, result.LoadedSkills, err
	}
	if reactTool != nil {
		reaction := reactTool.LastReaction()
		if reaction != nil && logger != nil {
			logger.Info("message_reaction_applied",
				"message_id", reaction.MessageID,
				"emoji_type", reaction.EmojiType,
				"source", reaction.Source,
			)
		}
	}
	return result.Final, result.Context, result.LoadedSkills, nil
}

func buildLarkPromptMessagesWithImageNotes(history []chathistory.ChatHistoryItem, job larkJob, model string, supportsImageParts *bool, fileCacheDir string, logger *slog.Logger) (*llm.Message, *llm.Message, error) {
	historyRaw := chathistory.RenderHistoryContext(history)
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg := llm.Message{Role: "user", Content: historyRaw}
		historyMsg = &msg
	}
	currentRaw := chathistory.RenderCurrentMessage(newLarkInboundHistoryItem(job))
	if len(job.Images) > 0 {
		currentRaw = imageinput.AppendImageMetadataNotes(currentRaw, job.Images)
	} else {
		currentRaw = imageinput.AppendImagePathNotes(currentRaw, job.ImagePaths, fileCacheDir)
	}
	current, err := imageinput.BuildUserMessage(currentRaw, model, job.ImagePaths, imageinput.MessageOptions{
		MaxImages:          larkLLMMaxImages,
		MaxBytes:           larkLLMMaxImageBytes,
		SupportsImageParts: supportsImageParts,
		Logger:             logger,
		LogPrefix:          "lark",
	})
	if err != nil {
		return nil, nil, err
	}
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
		Text:             larkHistoryText(job.Text, len(job.ImagePaths)),
		Images:           append([]chathistory.ChatHistoryImage(nil), job.Images...),
	}
}

func larkHistoryText(text string, imageCount int) string {
	text = strings.TrimSpace(text)
	if imageCount <= 0 {
		return text
	}
	marker := fmt.Sprintf("[image attachments: %d]", imageCount)
	if text == "" {
		return marker
	}
	return text + "\n" + marker
}

func larkJobFromInbound(inbound larkbus.InboundMessage) larkJob {
	return larkJob{
		ChatID:       strings.TrimSpace(inbound.ChatID),
		ChatType:     strings.TrimSpace(inbound.ChatType),
		MessageID:    strings.TrimSpace(inbound.MessageID),
		FromUserID:   strings.TrimSpace(inbound.FromUserID),
		DisplayName:  strings.TrimSpace(inbound.DisplayName),
		Text:         strings.TrimSpace(inbound.Text),
		ImagePaths:   busruntime.ImagePathsFromAttachments(inbound.ImageAttachments),
		SentAt:       inbound.SentAt.UTC(),
		MentionUsers: append([]string(nil), inbound.MentionUsers...),
		EventID:      strings.TrimSpace(inbound.EventID),
	}
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
	reg := baseReg.Clone()
	if isLarkGroupChat(chatType) {
		reg.Remove(toolsutil.BuiltinContactsSend)
	}
	return reg
}

func registerLarkChannelTools(reg *tools.Registry, api larktools.API, chatID, messageID, fileCacheDir string, fileMaxBytes int64) (*larktools.ReactTool, error) {
	if reg == nil || api == nil {
		return nil, nil
	}
	if fileMaxBytes <= 0 {
		fileMaxBytes = larkToolFileMaxBytes
	}
	for _, tool := range []tools.Tool{
		larktools.NewSendVoiceTool(api, chatID, fileCacheDir, fileMaxBytes),
		larktools.NewSendPhotoTool(api, chatID, fileCacheDir, fileMaxBytes),
		larktools.NewSendFileTool(api, chatID, fileCacheDir, fileMaxBytes),
	} {
		if err := reg.Replace(tool); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(messageID) == "" {
		return nil, nil
	}
	reactTool := larktools.NewReactTool(api, messageID)
	if err := reg.Replace(reactTool); err != nil {
		return nil, err
	}
	return reactTool, nil
}
