package slack

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
	slacktools "github.com/quailyquaily/mistermorph/tools/slack"
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

func runSlackTask(
	ctx context.Context,
	d Dependencies,
	logger *slog.Logger,
	logOpts agent.LogOptions,
	client llm.Client,
	baseReg *tools.Registry,
	api *slackAPI,
	sharedGuard *guard.Guard,
	cfg agent.Config,
	model string,
	job slackJob,
	history []chathistory.ChatHistoryItem,
	historyCap int,
	stickySkills []string,
	allowedChannelIDs map[string]bool,
	availableEmojiNames []string,
	fileCacheDir string,
	runtimeOpts runtimeTaskOptions,
	sendSlackText func(context.Context, string, string) error,
) (*agent.Final, *agent.Context, []string, *slacktools.Reaction, error) {
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	task := strings.TrimSpace(job.Text)
	if task == "" {
		return nil, nil, nil, nil, fmt.Errorf("empty slack task")
	}
	historyMsg, currentMsg, err := buildSlackPromptMessages(history, job)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var llmHistory []llm.Message
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
	}

	if baseReg == nil {
		return nil, nil, nil, nil, fmt.Errorf("base registry is nil")
	}
	reg := buildSlackRegistry(baseReg, job.ChatType)
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    client,
		DefaultModel:     model,
		PlanCreateClient: runtimeOpts.PlanCreateClient,
		PlanCreateModel:  runtimeOpts.PlanCreateModel,
	})
	toolsutil.SetTodoUpdateToolAddContext(reg, todoResolveContextForSlack(job))
	toolAPI := newSlackToolAPI(api)
	if api != nil && strings.TrimSpace(job.ChannelID) != "" {
		reg.Register(slacktools.NewSendFileTool(toolAPI, job.ChannelID, job.ThreadTS, allowedChannelIDs, fileCacheDir, 0))
	}
	var reactTool *slacktools.ReactTool
	if api != nil &&
		strings.TrimSpace(job.ChannelID) != "" &&
		strings.TrimSpace(job.MessageTS) != "" {
		reactTool = slacktools.NewReactTool(toolAPI, job.ChannelID, job.MessageTS, allowedChannelIDs, availableEmojiNames)
		reg.Register(reactTool)
	}

	promptSpec, loadedSkills, err := depsutil.PromptSpecFromCommon(d, ctx, logger, logOpts, task, client, model, stickySkills)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	promptprofile.AppendSlackRuntimeBlocks(&promptSpec, isSlackGroupChat(job.ChatType), job.MentionUsers, strings.Join(availableEmojiNames, ","))

	memSubjectID := slackMemorySubjectID(job)
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" && runtimeOpts.MemoryInjectionEnabled {
		snap, memErr := runtimeOpts.MemoryOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      memSubjectID,
			RequestContext: slackMemoryRequestContext(job.ChatType),
			MaxItems:       runtimeOpts.MemoryInjectionMaxItems,
		})
		if memErr != nil {
			if logger != nil {
				logger.Warn("memory_injection_error", "source", "slack", "subject_id", memSubjectID, "error", memErr.Error())
			}
		} else if strings.TrimSpace(snap) != "" {
			promptprofile.AppendMemorySummariesBlock(&promptSpec, snap)
			if logger != nil {
				logger.Info("memory_injection_applied", "source", "slack", "subject_id", memSubjectID, "channel_id", job.ChannelID, "snapshot_len", len(snap))
			}
		}
	}

	engine := agent.New(
		client,
		reg,
		cfg,
		promptSpec,
		func() []agent.Option {
			opts := []agent.Option{
				agent.WithLogger(logger),
				agent.WithLogOptions(logOpts),
				agent.WithGuard(sharedGuard),
			}
			if sendSlackText != nil {
				planUpdateHook := func(runCtx *agent.Context, update agent.PlanStepUpdate) {
					if runCtx == nil || runCtx.Plan == nil {
						return
					}
					msg := generateSlackPlanProgressMessage(runCtx.Plan, update)
					if strings.TrimSpace(msg) == "" {
						return
					}
					correlationID := fmt.Sprintf("slack:plan:%s:%s", job.ChannelID, job.MessageTS)
					if err := sendSlackText(context.Background(), msg, correlationID); err != nil {
						logger.Warn("slack_bus_publish_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "bus_error_code", busErrorCodeString(err), "error", err.Error())
					}
				}
				opts = append(opts, agent.WithPlanStepUpdate(planUpdateHook))
			}
			return opts
		}()...,
	)

	meta := map[string]any{
		"trigger":            "slack",
		"slack_team_id":      job.TeamID,
		"slack_channel_id":   job.ChannelID,
		"slack_chat_type":    job.ChatType,
		"slack_message_ts":   job.MessageTS,
		"slack_thread_ts":    job.ThreadTS,
		"slack_from_user_id": job.UserID,
	}
	final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{
		Model:          model,
		Scene:          "slack.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
	})
	if err != nil {
		return final, runCtx, loadedSkills, nil, err
	}

	var reaction *slacktools.Reaction
	if reactTool != nil {
		reaction = reactTool.LastReaction()
		if reaction != nil && logger != nil {
			logger.Info("message_reaction_applied",
				"channel_id", reaction.ChannelID,
				"message_ts", reaction.MessageTS,
				"emoji", reaction.Emoji,
				"source", reaction.Source,
			)
		}
	}

	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" {
		finalOutput := strings.TrimSpace(depsutil.FormatFinalOutput(final))
		recordedAt := time.Now().UTC()
		recordOffset, memErr := runtimeOpts.MemoryOrchestrator.Record(memoryruntime.RecordRequest{
			TaskRunID:      slackMemoryTaskRunID(job),
			SessionID:      slackMemorySessionID(job),
			SubjectID:      memSubjectID,
			Channel:        "slack",
			Participants:   slackMemoryParticipants(job),
			TaskText:       task,
			FinalOutput:    finalOutput,
			SourceHistory:  buildSlackMemoryHistory(history, job, finalOutput, recordedAt, historyCap),
			SessionContext: slackMemorySessionContext(job),
		})
		if memErr != nil {
			if logger != nil {
				logger.Warn("memory_record_error", "source", "slack", "subject_id", memSubjectID, "error", memErr.Error())
			}
		} else {
			if logger != nil {
				logger.Debug("memory_record_ok", "source", "slack", "subject_id", memSubjectID, "offset_file", recordOffset.File, "offset_line", recordOffset.Line)
			}
			if runtimeOpts.MemoryProjectionWorker != nil {
				runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
			}
		}
	}

	return final, runCtx, loadedSkills, reaction, nil
}

func buildSlackPromptMessages(history []chathistory.ChatHistoryItem, job slackJob) (*llm.Message, *llm.Message, error) {
	historyRaw, err := chathistory.RenderHistoryContext(chathistory.ChannelSlack, history)
	if err != nil {
		return nil, nil, fmt.Errorf("render slack history context: %w", err)
	}
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg := llm.Message{Role: "user", Content: historyRaw}
		historyMsg = &msg
	}
	currentRaw, err := chathistory.RenderCurrentMessage(newSlackInboundHistoryItem(job))
	if err != nil {
		return nil, nil, fmt.Errorf("render slack current message: %w", err)
	}
	current := llm.Message{Role: "user", Content: currentRaw}
	return historyMsg, &current, nil
}

func todoResolveContextForSlack(job slackJob) todo.AddResolveContext {
	user := strings.TrimSpace(job.UserID)
	if user != "" {
		user = "slack:" + user
	}
	mentions := normalizeMentionUsersForTodo(job.MentionUsers)
	return todo.AddResolveContext{
		Channel:          "slack",
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		SpeakerUsername:  user,
		MentionUsernames: mentions,
		UserInputRaw:     job.Text,
	}
}

func normalizeMentionUsersForTodo(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		out = append(out, "slack:"+item)
	}
	return out
}

func newSlackInboundHistoryItem(job slackJob) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelSlack,
		Kind:             chathistory.KindInboundUser,
		ChatID:           "slack:" + job.TeamID + ":" + job.ChannelID,
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		MessageID:        strings.TrimSpace(job.MessageTS),
		ReplyToMessageID: strings.TrimSpace(job.ThreadTS),
		SentAt:           job.SentAt.UTC(),
		Sender:           slackSenderFromJob(job, false, ""),
		Text:             strings.TrimSpace(job.Text),
	}
}

func newSlackOutboundAgentHistoryItem(job slackJob, output string, sentAt time.Time, botUserID string) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelSlack,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           "slack:" + job.TeamID + ":" + job.ChannelID,
		ChatType:         strings.ToLower(strings.TrimSpace(job.ChatType)),
		ReplyToMessageID: strings.TrimSpace(job.ThreadTS),
		SentAt:           sentAt.UTC(),
		Sender:           slackSenderFromJob(job, true, botUserID),
		Text:             strings.TrimSpace(output),
	}
}

func newSlackOutboundReactionHistoryItem(job slackJob, note, emoji string, sentAt time.Time, botUserID string) chathistory.ChatHistoryItem {
	item := newSlackOutboundAgentHistoryItem(job, note, sentAt, botUserID)
	item.Kind = chathistory.KindOutboundReaction
	if strings.TrimSpace(emoji) != "" {
		item.Text = strings.TrimSpace(note)
	}
	return item
}

func generateSlackPlanProgressMessage(plan *agent.Plan, update agent.PlanStepUpdate) string {
	if plan == nil || update.CompletedIndex < 0 {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(update.CompletedStep),
		stepByIndex(plan, update.CompletedIndex),
		strings.TrimSpace(update.StartedStep),
		stepByIndex(plan, update.StartedIndex),
	)
}

func stepByIndex(plan *agent.Plan, index int) string {
	if plan == nil || index < 0 || index >= len(plan.Steps) {
		return ""
	}
	return strings.TrimSpace(plan.Steps[index].Step)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func slackSenderFromJob(job slackJob, isBot bool, botUserID string) chathistory.ChatHistorySender {
	if isBot {
		return chathistory.ChatHistorySender{
			UserID:     strings.TrimSpace(botUserID),
			Username:   "slack-bot",
			Nickname:   "slack-bot",
			IsBot:      true,
			DisplayRef: "slack-bot",
		}
	}
	mentionRef := strings.TrimSpace(job.UserID)
	if mentionRef != "" {
		mentionRef = "<@" + mentionRef + ">"
	}
	nickname := strings.TrimSpace(job.DisplayName)
	if nickname == "" {
		nickname = mentionRef
	}
	displayRef := mentionRef
	if nickname != "" && mentionRef != "" && nickname != mentionRef {
		displayRef = nickname + " (" + mentionRef + ")"
	} else if nickname != "" {
		displayRef = nickname
	}
	username := strings.TrimSpace(job.Username)
	if username == "" {
		username = strings.TrimSpace(job.UserID)
	}
	return chathistory.ChatHistorySender{
		UserID:     strings.TrimSpace(job.UserID),
		Username:   username,
		Nickname:   nickname,
		IsBot:      false,
		DisplayRef: displayRef,
	}
}

func slackHistoryCapForMode(mode string) int {
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

func buildSlackConversationKey(teamID, channelID string) (string, error) {
	return busruntime.BuildSlackChannelConversationKey(strings.TrimSpace(teamID) + ":" + strings.TrimSpace(channelID))
}

func buildSlackHistoryScopeKey(teamID, channelID, threadTS string) (string, error) {
	conversationKey, err := buildSlackConversationKey(teamID, channelID)
	if err != nil {
		return "", err
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" {
		return conversationKey, nil
	}
	return conversationKey + ":thread:" + threadTS, nil
}

func slackHistoryScopeKeyForJob(job slackJob) string {
	teamID := strings.TrimSpace(job.TeamID)
	channelID := strings.TrimSpace(job.ChannelID)
	if teamID != "" && channelID != "" {
		threadTS := strings.TrimSpace(job.ThreadTS)
		// In smart group mode we may synthesize quote-reply delivery by setting
		// thread_ts to message_ts for non-thread channel mentions. Keep history
		// channel-scoped for that case to preserve the "empty inbound thread_ts"
		// behavior.
		if threadTS != "" && threadTS == strings.TrimSpace(job.MessageTS) {
			threadTS = ""
		}
		scope, err := buildSlackHistoryScopeKey(teamID, channelID, threadTS)
		if err == nil {
			scope = strings.TrimSpace(scope)
			if scope != "" {
				return scope
			}
		}
	}
	return strings.TrimSpace(job.ConversationKey)
}

func busErrorCodeString(err error) string {
	if err == nil {
		return ""
	}
	return string(busruntime.ErrorCodeOf(err))
}

func publishSlackBusOutbound(ctx context.Context, inprocBus *busruntime.Inproc, teamID, channelID, text, threadTS, correlationID string) (string, error) {
	if inprocBus == nil {
		return "", fmt.Errorf("bus is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	teamID = strings.TrimSpace(teamID)
	channelID = strings.TrimSpace(channelID)
	text = strings.TrimSpace(text)
	threadTS = strings.TrimSpace(threadTS)
	if teamID == "" {
		return "", fmt.Errorf("team_id is required")
	}
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required")
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
		ReplyTo:   threadTS,
	})
	if err != nil {
		return "", err
	}
	conversationKey, err := buildSlackConversationKey(teamID, channelID)
	if err != nil {
		return "", err
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		correlationID = "slack:" + messageID
	}
	outbound := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelSlack,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(messageID),
		CorrelationID:   correlationID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			SessionID: sessionID,
			ReplyTo:   threadTS,
			ThreadTS:  threadTS,
			TeamID:    teamID,
			ChannelID: channelID,
		},
	}
	if err := inprocBus.PublishValidated(ctx, outbound); err != nil {
		return "", err
	}
	return messageID, nil
}

func buildSlackRegistry(baseReg *tools.Registry, chatType string) *tools.Registry {
	reg := tools.NewRegistry()
	if baseReg == nil {
		return reg
	}
	groupChat := isSlackGroupChat(chatType)
	for _, t := range baseReg.All() {
		name := strings.TrimSpace(t.Name())
		if groupChat && strings.EqualFold(name, "contacts_send") {
			continue
		}
		reg.Register(t)
	}
	return reg
}
