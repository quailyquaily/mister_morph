package mixin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
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
	mixintools "github.com/quailyquaily/mistermorph/tools/mixin"
)

type mixinDeliveryReceipts struct {
	mu      sync.Mutex
	pending map[string]chan error
}

func newMixinDeliveryReceipts() *mixinDeliveryReceipts {
	return &mixinDeliveryReceipts{pending: make(map[string]chan error)}
}

func (r *mixinDeliveryReceipts) register(messageID string) (<-chan error, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pending[messageID]; exists {
		return nil, fmt.Errorf("mixin delivery receipt already registered: %s", messageID)
	}
	result := make(chan error, 1)
	r.pending[messageID] = result
	return result, nil
}

func (r *mixinDeliveryReceipts) remove(messageID string) {
	r.mu.Lock()
	delete(r.pending, messageID)
	r.mu.Unlock()
}

func (r *mixinDeliveryReceipts) complete(messageID string, err error) {
	r.mu.Lock()
	result := r.pending[messageID]
	delete(r.pending, messageID)
	r.mu.Unlock()
	if result != nil {
		result <- err
	}
}

const (
	mixinStickySkillsCap = 16
	mixinLLMMaxImages    = 3
)

type mixinJob struct {
	TaskID           string
	ConversationKey  string
	ConversationID   string
	ChatType         string
	MessageID        string
	QuoteMessageID   string
	FromUserID       string
	IdentityNumber   string
	DisplayName      string
	FromIsAgent      bool
	Text             string
	ImagePaths       []string
	Images           []chathistory.ChatHistoryImage
	WorkspaceDir     string
	FileCacheDir     string
	Route            *llmutil.ResolvedRoute
	ResumeApprovalID string
	SentAt           time.Time
	Version          uint64
	MentionUsers     []string
	EventID          string
	Generation       *runtimecore.RuntimeGenerationLease
}

func (j mixinJob) runtimeBundle() *runtimecore.ChannelRuntimeBundle {
	if j.Generation == nil {
		return nil
	}
	return j.Generation.Bundle()
}

func (j mixinJob) releaseGeneration() {
	if j.Generation != nil {
		j.Generation.Release()
	}
}

func (j mixinJob) approvalGuard() *guard.Guard {
	bundle := j.runtimeBundle()
	if bundle == nil || bundle.TaskRuntime == nil {
		return nil
	}
	return bundle.TaskRuntime.SharedGuard
}

func runMixinTask(ctx context.Context, rt *taskruntime.Runtime, toolAPI mixintools.AttachmentAPI, job mixinJob, history []chathistory.ChatHistoryItem, stickySkills []string, steerSource agent.SteerSource) (*agent.Final, *agent.Context, []string, error) {
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("mixin task runtime is nil")
	}
	ctx = llmstats.WithMetadata(ctx, job.TaskID, job.EventID)
	ctx = topiccontext.WithScope(ctx, topiccontext.Scope{Runtime: "mixin", ConversationKey: job.ConversationKey, TopicID: job.ConversationID})
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	ctx = builtin.WithContactsSendRuntimeContext(ctx, contactsSendRuntimeContextForMixin(job))
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
		return nil, nil, nil, fmt.Errorf("empty mixin task")
	}
	var mainRoute llmutil.ResolvedRoute
	if job.Route != nil {
		mainRoute = *job.Route
	} else {
		resolved, err := rt.ResolveRouteForRun(ctx, routePurpose)
		if err != nil {
			return nil, nil, nil, err
		}
		mainRoute = resolved
	}
	if reasoningEffort != "" {
		mainRoute = llmutil.ResolvedRouteWithReasoningEffort(mainRoute, reasoningEffort)
	}
	checkpoint, err := rt.PrepareContextHistory(ctx, job.ConversationKey, history, newMixinInboundHistoryItem(job))
	if err != nil {
		return nil, nil, nil, err
	}
	historyMessage, currentMessage, err := buildMixinPromptMessages(checkpoint.History, job, strings.TrimSpace(mainRoute.ClientConfig.Model), mainRoute.Values.SupportsImageParts, rt.Logger)
	if err != nil {
		return nil, nil, nil, err
	}
	var llmHistory []llm.Message
	var boundaries []string
	if historyMessage != nil {
		llmHistory = append(llmHistory, *historyMessage)
		boundaries = []string{checkpoint.HistoryBoundary}
	}
	registry := buildMixinRegistry(rt.BaseRegistry, job.ChatType)
	if err := registerMixinChannelTools(registry, toolAPI, job.ConversationID, job.FromUserID, job.FileCacheDir, mixinFileMaxBytes); err != nil {
		return nil, nil, nil, err
	}
	meta := taskruntime.ApplyObservationMeta(map[string]any{
		"trigger":               "mixin",
		"mixin_conversation_id": job.ConversationID,
		"mixin_chat_type":       job.ChatType,
		"mixin_user_id":         job.FromUserID,
		"mixin_message_id":      job.MessageID,
		"mixin_conversation":    job.ConversationKey,
	}, taskruntime.ObservationMetaIDs{TaskID: job.TaskID, TraceID: job.TaskID, TopicID: job.ConversationID, OriginEventID: job.EventID})
	runRequest := taskruntime.RunRequest{
		Task:                    task,
		Model:                   strings.TrimSpace(mainRoute.ClientConfig.Model),
		Route:                   &mainRoute,
		RoutePurpose:            routePurpose,
		ReasoningEffortOverride: reasoningEffort,
		Scene:                   "mixin.loop",
		History:                 llmHistory,
		CurrentMessage:          currentMessage,
		Meta:                    meta,
		StickySkills:            stickySkills,
		Registry:                registry,
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
			toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForMixin(job))
			promptprofile.AppendMixinRuntimeBlocks(spec, isMixinGroup(job.ChatType))
		},
		SteerSource:            steerSource,
		ImageToolScope:         strings.TrimSpace(job.ConversationKey),
		ImageToolRetention:     toolsutil.ImageToolRetentionCountdown,
		ContextCheckpointStore: checkpoint.Store,
		HistoryBoundaries:      boundaries,
		CurrentMessageBoundary: checkpoint.CurrentMessageBoundary,
	}
	if job.FromIsAgent && !isMixinGroup(job.ChatType) {
		runRequest.ToolTriggers = map[string]bool{toolsutil.BuiltinContactsSend: true}
	}
	var result taskruntime.RunResult
	if approvalID := strings.TrimSpace(job.ResumeApprovalID); approvalID != "" {
		result, err = rt.Resume(ctx, approvalID, runRequest)
	} else {
		result, err = rt.Run(ctx, runRequest)
	}
	return result.Final, result.Context, result.LoadedSkills, err
}

func buildMixinPromptMessages(history []chathistory.ChatHistoryItem, job mixinJob, model string, supportsImageParts *bool, logger *slog.Logger) (*llm.Message, *llm.Message, error) {
	var historyMessage *llm.Message
	if content := strings.TrimSpace(chathistory.RenderHistoryContext(history)); content != "" {
		message := llm.Message{Role: "user", Content: content}
		historyMessage = &message
	}
	currentRaw := chathistory.RenderCurrentMessage(newMixinInboundHistoryItem(job))
	if len(job.Images) > 0 {
		currentRaw = imageinput.AppendImageMetadataNotes(currentRaw, job.Images)
	} else {
		currentRaw = imageinput.AppendImagePathNotes(currentRaw, job.ImagePaths, job.FileCacheDir)
	}
	current, err := imageinput.BuildUserMessage(currentRaw, model, job.ImagePaths, imageinput.MessageOptions{
		MaxImages: mixinLLMMaxImages, MaxBytes: mixinImageMaxBytes,
		SupportsImageParts: supportsImageParts, Logger: logger, LogPrefix: "mixin",
	})
	if err != nil {
		return nil, nil, err
	}
	return historyMessage, &current, nil
}

func todoResolveContextForMixin(job mixinJob) todo.AddResolveContext {
	mentions := make([]string, 0, len(job.MentionUsers))
	for _, userID := range job.MentionUsers {
		if userID = strings.TrimSpace(userID); userID != "" {
			mentions = append(mentions, "mixin:"+userID)
		}
	}
	return todo.AddResolveContext{
		Channel:          "mixin",
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		SpeakerUsername:  "mixin:" + strings.TrimSpace(job.FromUserID),
		MentionUsernames: mentions,
		UserInputRaw:     job.Text,
	}
}

func contactsSendRuntimeContextForMixin(job mixinJob) builtin.ContactsSendRuntimeContext {
	ids := []string{"mixin:" + strings.TrimSpace(job.FromUserID)}
	if !isMixinGroup(job.ChatType) {
		ids = append(ids, "mixin:"+strings.TrimSpace(job.ConversationID))
	}
	return builtin.ContactsSendRuntimeContext{ForbiddenTargetIDs: ids}
}

func newMixinInboundHistoryItem(job mixinJob) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelMixin,
		Kind:             chathistory.KindInboundUser,
		ChatID:           "mixin:" + strings.TrimSpace(job.ConversationID),
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		MessageID:        strings.TrimSpace(job.MessageID),
		ReplyToMessageID: strings.TrimSpace(job.QuoteMessageID),
		SentAt:           job.SentAt.UTC(),
		Sender:           mixinSenderFromJob(job, false),
		Text:             strings.TrimSpace(job.Text),
		Images:           append([]chathistory.ChatHistoryImage(nil), job.Images...),
	}
}

func newMixinOutboundHistoryItem(job mixinJob, output string, sentAt time.Time) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel: chathistory.ChannelMixin, Kind: chathistory.KindOutboundAgent,
		ChatID: "mixin:" + strings.TrimSpace(job.ConversationID), ChatType: strings.ToLower(strings.TrimSpace(job.ChatType)),
		ReplyToMessageID: strings.TrimSpace(job.MessageID), SentAt: sentAt.UTC(), Sender: mixinSenderFromJob(job, true), Text: strings.TrimSpace(output),
	}
}

func mixinSenderFromJob(job mixinJob, bot bool) chathistory.ChatHistorySender {
	if bot {
		return chathistory.ChatHistorySender{Username: "mixin-bot", Nickname: "mixin-bot", IsBot: true, DisplayRef: "mixin-bot"}
	}
	display := strings.TrimSpace(job.DisplayName)
	if display == "" {
		display = strings.TrimSpace(job.IdentityNumber)
	}
	if display == "" {
		display = strings.TrimSpace(job.FromUserID)
	}
	return chathistory.ChatHistorySender{UserID: strings.TrimSpace(job.FromUserID), Username: strings.TrimSpace(job.IdentityNumber), Nickname: display, IsBot: job.FromIsAgent, DisplayRef: display}
}

func mixinJobFromInbound(inbound mixinbus.InboundMessage) mixinJob {
	return mixinJob{
		ConversationID: inbound.ConversationID, ChatType: inbound.ChatType, MessageID: inbound.MessageID,
		QuoteMessageID: inbound.QuoteMessageID, FromUserID: inbound.FromUserID, IdentityNumber: inbound.IdentityNumber,
		DisplayName: inbound.DisplayName, FromIsAgent: inbound.FromIsAgent, Text: inbound.Text, SentAt: inbound.SentAt.UTC(),
		ImagePaths:   busruntime.ImagePathsFromAttachments(inbound.ImageAttachments),
		MentionUsers: append([]string(nil), inbound.MentionUserIDs...), EventID: inbound.MessageID,
	}
}

func mixinHistoryCap(mode string) int {
	if strings.EqualFold(strings.TrimSpace(mode), "talkative") {
		return 16
	}
	return 8
}

func trimMixinHistory(items []chathistory.ChatHistoryItem, limit int) []chathistory.ChatHistoryItem {
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]chathistory.ChatHistoryItem(nil), items...)
}

func capMixinSkills(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
		if len(out) == mixinStickySkillsCap {
			break
		}
	}
	return out
}

func publishMixinBusOutbound(ctx context.Context, bus *busruntime.Inproc, conversationID, recipientID, text, quoteMessageID, correlationID string) (string, error) {
	message, messageID, err := newMixinBusOutbound(conversationID, recipientID, text, quoteMessageID, correlationID)
	if err != nil {
		return "", err
	}
	if ctx == nil || bus == nil {
		return "", fmt.Errorf("context and bus are required")
	}
	if err := bus.PublishValidated(ctx, message); err != nil {
		return "", err
	}
	return messageID, nil
}

func publishMixinBusOutboundAndWait(ctx context.Context, bus *busruntime.Inproc, receipts *mixinDeliveryReceipts, conversationID, recipientID, text, quoteMessageID, correlationID string) (string, error) {
	if ctx == nil || bus == nil || receipts == nil {
		return "", fmt.Errorf("context, bus, and delivery receipts are required")
	}
	message, messageID, err := newMixinBusOutbound(conversationID, recipientID, text, quoteMessageID, correlationID)
	if err != nil {
		return "", err
	}
	result, err := receipts.register(message.ID)
	if err != nil {
		return "", err
	}
	if err := bus.PublishValidated(ctx, message); err != nil {
		receipts.remove(message.ID)
		return "", err
	}
	select {
	case err := <-result:
		return messageID, err
	case <-ctx.Done():
		receipts.remove(message.ID)
		return "", ctx.Err()
	}
}

func newMixinBusOutbound(conversationID, recipientID, text, quoteMessageID, correlationID string) (busruntime.BusMessage, string, error) {
	conversationID = strings.TrimSpace(conversationID)
	recipientID = strings.TrimSpace(recipientID)
	text = strings.TrimSpace(text)
	if conversationID == "" || recipientID == "" || text == "" {
		return busruntime.BusMessage{}, "", fmt.Errorf("conversation_id, recipient_id, and text are required")
	}
	messageID := uuid.NewString()
	now := time.Now().UTC()
	session, err := uuid.NewV7()
	if err != nil {
		return busruntime.BusMessage{}, "", err
	}
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: messageID, Text: text, SentAt: now.Format(time.RFC3339), SessionID: session.String(), ReplyTo: strings.TrimSpace(quoteMessageID),
	})
	if err != nil {
		return busruntime.BusMessage{}, "", err
	}
	conversationKey, err := busruntime.BuildMixinConversationKey(conversationID)
	if err != nil {
		return busruntime.BusMessage{}, "", err
	}
	if strings.TrimSpace(correlationID) == "" {
		correlationID = "mixin:" + messageID
	}
	message := busruntime.BusMessage{
		ID: "bus_" + uuid.NewString(), Direction: busruntime.DirectionOutbound, Channel: busruntime.ChannelMixin,
		Topic: busruntime.TopicChatMessage, ConversationKey: conversationKey, ParticipantKey: recipientID, IdempotencyKey: idempotency.MessageEnvelopeKey(messageID),
		CorrelationID: correlationID, PayloadBase64: payload, CreatedAt: now,
		Extensions: busruntime.MessageExtensions{SessionID: session.String(), ReplyTo: strings.TrimSpace(quoteMessageID), ChannelID: conversationID},
	}
	return message, messageID, nil
}

func isMixinGroup(chatType string) bool {
	return strings.EqualFold(strings.TrimSpace(chatType), "GROUP")
}

func buildMixinRegistry(base *tools.Registry, chatType string) *tools.Registry {
	registry := base.Clone()
	if isMixinGroup(chatType) {
		registry.Remove(toolsutil.BuiltinContactsSend)
	}
	return registry
}

func registerMixinChannelTools(registry *tools.Registry, api mixintools.AttachmentAPI, conversationID, recipientID, fileCacheDir string, maxBytes int64) error {
	if registry == nil || api == nil {
		return nil
	}
	for _, kind := range []mixintools.AttachmentKind{mixintools.AttachmentFile, mixintools.AttachmentPhoto, mixintools.AttachmentAudio} {
		if err := registry.Replace(mixintools.NewSendAttachmentTool(api, conversationID, recipientID, fileCacheDir, maxBytes, kind)); err != nil {
			return err
		}
	}
	return nil
}
