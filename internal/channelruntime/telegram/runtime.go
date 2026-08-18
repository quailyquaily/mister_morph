package telegram

import (
	"context"
	"errors"
	"fmt"
	htmlstd "html"
	randv2 "math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
)

type telegramJob struct {
	TaskID           string
	ConversationKey  string
	ChatID           int64
	MessageThreadID  int64
	MessageID        int64
	ReplyToMessageID int64
	SentAt           time.Time
	ChatType         string
	FromUserID       int64
	FromUsername     string
	FromFirstName    string
	FromLastName     string
	FromDisplayName  string
	FromIsAgent      bool
	Text             string
	ImagePaths       []string
	Images           []chathistory.ChatHistoryImage
	WorkspaceDir     string
	Route            *llmutil.ResolvedRoute
	ResumeApprovalID string
	Version          uint64
	Meta             map[string]any
	MentionUsers     []string
	Generation       *runtimecore.RuntimeGenerationLease
}

func (j telegramJob) runtimeBundle(fallback *runtimecore.ChannelRuntimeBundle) *runtimecore.ChannelRuntimeBundle {
	if j.Generation != nil {
		if bundle := j.Generation.Bundle(); bundle != nil {
			return bundle
		}
	}
	return fallback
}

func (j telegramJob) releaseGeneration() {
	if j.Generation != nil {
		j.Generation.Release()
	}
}

func (j telegramJob) approvalGuard(fallback *guard.Guard) *guard.Guard {
	bundle := j.runtimeBundle(nil)
	if bundle != nil && bundle.TaskRuntime != nil && bundle.TaskRuntime.SharedGuard != nil {
		return bundle.TaskRuntime.SharedGuard
	}
	return fallback
}

type telegramPlanProgressLine struct {
	Text  string
	Emoji string
}

type telegramPlanProgressEditState struct {
	CorrelationID string
	MessageID     int64
	Lines         []telegramPlanProgressLine
}

func sendTelegramUnauthorizedMessage(api *telegramAPI, chatID int64, messageThreadID int64, chatType string) {
	chatType = strings.TrimSpace(chatType)
	if chatType == "" {
		chatType = "unknown"
	}
	msg := fmt.Sprintf("You don't have permission to use this bot. Please contact the admin.\nchat_id: `%d`, type: `%s`", chatID, chatType)
	_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, msg, true)
}

func shouldPublishTelegramText(final *agent.Final) bool {
	if final == nil {
		return true
	}
	return !final.IsLightweight
}

func runTelegramLoop(ctx context.Context, d Dependencies, opts RunOptions) error {
	state, err := bootstrapTelegramRuntimeState(ctx, d, opts)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	defer state.close()

	listen := strings.TrimSpace(state.options.Server.Listen)
	if listen != "" {
		if strings.TrimSpace(state.options.Server.AuthToken) == "" {
			state.logger.Warn("telegram_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /runtime/tasks")
		}
		if err := state.serveDaemon(); err != nil {
			state.logger.Warn("telegram_daemon_server_start_error", "addr", listen, "error", err.Error())
		}
	}
	return state.poll()
}
func telegramOutboundEventFromBusMessage(msg busruntime.BusMessage) (OutboundEvent, error) {
	chatID, messageThreadID, err := telegrambus.ConversationPartsFromBusMessage(msg)
	if err != nil {
		return OutboundEvent{}, err
	}
	env, err := msg.Envelope()
	if err != nil {
		return OutboundEvent{}, err
	}
	replyToRaw := strings.TrimSpace(msg.Extensions.ReplyTo)
	if replyToRaw == "" {
		replyToRaw = strings.TrimSpace(env.ReplyTo)
	}
	replyToMessageID := int64(0)
	if replyToRaw != "" {
		if parsed, parseErr := strconv.ParseInt(replyToRaw, 10, 64); parseErr == nil && parsed > 0 {
			replyToMessageID = parsed
		}
	}
	return OutboundEvent{
		ChatID:           chatID,
		MessageThreadID:  messageThreadID,
		ReplyToMessageID: replyToMessageID,
		Text:             strings.TrimSpace(env.Text),
		CorrelationID:    strings.TrimSpace(msg.CorrelationID),
		Kind:             telegramOutboundKind(msg.CorrelationID),
	}, nil
}

func telegramConversationMapKey(chatID int64, messageThreadID int64) string {
	key, err := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID)
	if err != nil {
		return fmt.Sprintf("tg:%d", chatID)
	}
	return key
}

func telegramOutboundKind(correlationID string) string {
	id := strings.ToLower(strings.TrimSpace(correlationID))
	switch {
	case strings.Contains(id, ":plan:"):
		return "plan_progress"
	case strings.Contains(id, ":error:") || strings.Contains(id, "file_download_error"):
		return "error"
	default:
		return "message"
	}
}

func nextTelegramPlanProgressState(state telegramPlanProgressEditState, correlationID string, line string) (telegramPlanProgressEditState, string) {
	correlationID = strings.TrimSpace(correlationID)
	line = strings.TrimSpace(line)
	next := telegramPlanProgressEditState{
		CorrelationID: correlationID,
	}
	if line == "" {
		return next, ""
	}

	if state.MessageID > 0 && strings.EqualFold(strings.TrimSpace(state.CorrelationID), correlationID) {
		next.MessageID = state.MessageID
		next.Lines = append(next.Lines, state.Lines...)
	}

	next.Lines = append(next.Lines, telegramPlanProgressLine{
		Text:  line,
		Emoji: emojiForTelegramPlanStep(line),
	})
	return next, renderTelegramPlanProgressExpandable(next.Lines)
}

func renderTelegramPlanProgressExpandable(lines []telegramPlanProgressLine) string {
	reversed := make([]string, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i].Text)
		if line == "" {
			continue
		}
		emoji := strings.TrimSpace(lines[i].Emoji)
		if emoji == "" {
			emoji = emojiForTelegramPlanStep(line)
		}
		reversed = append(reversed, fmt.Sprintf("%s %d. %s", emoji, i+1, htmlstd.EscapeString(line)))
	}
	if len(reversed) == 0 {
		return ""
	}
	return "<blockquote expandable>" + strings.Join(reversed, "<br>") + "</blockquote>"
}

func emojiForTelegramPlanStep(step string) string {
	lower := strings.ToLower(strings.TrimSpace(step))
	switch {
	case strings.Contains(lower, "web_search"):
		return "🔎"
	case strings.Contains(lower, "url_fetch"):
		return "🧭"
	case strings.Contains(lower, "read_file"):
		return "📖"
	case strings.Contains(lower, "write_file"):
		return "✍️"
	case strings.Contains(lower, "_send_file"):
		return "🗂️"
	case strings.Contains(lower, "_send_photo"):
		return "📷"
	case strings.Contains(lower, "_send_voice"):
		return "🎙️"
	case strings.Contains(lower, "bash"):
		return "🧑‍💻"
	case strings.Contains(lower, "todo_update"):
		return "🗓️"
	case strings.Contains(lower, "contacts_send"), strings.Contains(lower, "agent_send"):
		return "✉️"
	default:
		if randv2.IntN(2) == 0 {
			return "💭"
		}
		return "🤔"
	}
}

func telegramTaskID(chatID int64, messageThreadID int64, messageID int64) string {
	if messageThreadID > 0 {
		return daemonruntime.BuildTaskID("tg", chatID, messageThreadID, messageID)
	}
	return daemonruntime.BuildTaskID("tg", chatID, messageID)
}

func telegramApprovalDecisionError(err error) error {
	if errors.Is(err, guard.ErrApprovalNotFound) {
		return daemonruntime.BadRequest("approval not found")
	}
	if errors.Is(err, guard.ErrApprovalNotPending) {
		return daemonruntime.BadRequest("approval is not pending")
	}
	return err
}

func markTelegramApprovalResumeFailed(store daemonruntime.TaskUpdater, taskID string, msg string) error {
	taskID = strings.TrimSpace(taskID)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "unknown error"
	}
	displayErr := "approval resume failed: " + msg
	if store != nil && taskID != "" {
		finishedAt := time.Now().UTC()
		if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskFailed
			info.Error = displayErr
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		}); err != nil {
			return fmt.Errorf("%s: persist task state: %w", displayErr, err)
		}
	}
	return fmt.Errorf("%s", displayErr)
}

func markTelegramMissingApprovalHandle(store daemonruntime.TaskView, approvalID string, approved bool) (string, bool, error) {
	taskID := runtimecore.TaskIDForPendingApproval(store, approvalID)
	if taskID == "" {
		return "", false, fmt.Errorf("pending approval handle is unavailable")
	}
	if approved {
		return taskID, false, markTelegramApprovalResumeFailed(store, taskID, "pending approval handle is unavailable")
	}
	finishedAt := time.Now().UTC()
	if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskCanceled
		info.Error = telegramApprovalResultText(false)
		info.FinishedAt = &finishedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
		info.ApprovalRequestID = approvalID
	}); err != nil {
		return taskID, false, err
	}
	return taskID, false, nil
}

func telegramTaskRef(chatID int64, messageThreadID int64, messageID int64) string {
	if messageThreadID > 0 {
		return fmt.Sprintf("telegram/%d/%d/%d", chatID, messageThreadID, messageID)
	}
	return fmt.Sprintf("telegram/%d/%d", chatID, messageID)
}

func telegramManagedTopicInfo(chatID int64, messageThreadID int64, chatType string, displayName string, username string) (string, string) {
	topicID := fmt.Sprintf("telegram:%d", chatID)
	if messageThreadID > 0 {
		topicID = fmt.Sprintf("telegram:%d:%d", chatID, messageThreadID)
	}
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = strings.TrimSpace(username)
	}
	if label != "" {
		return topicID, textutil.TruncateRunes("Telegram · "+label, 72)
	}
	chatType = strings.TrimSpace(strings.ToLower(chatType))
	if chatType != "" && chatType != "private" {
		return topicID, textutil.TruncateRunes("Telegram · "+chatType+" · "+strconv.FormatInt(chatID, 10), 72)
	}
	return topicID, textutil.TruncateRunes("Telegram · "+strconv.FormatInt(chatID, 10), 72)
}

func telegramReplyToMessageLogFields(reply *telegramMessage) []any {
	if reply == nil {
		return []any{"reply_to_present", false}
	}
	text := strings.TrimSpace(messageTextOrCaption(reply))
	fields := []any{
		"reply_to_present", true,
		"reply_to_message_id", reply.MessageID,
		"reply_to_message_thread_id", reply.MessageThreadID,
		"reply_to_is_topic_message", reply.IsTopicMessage,
		"reply_to_forum_topic_created", isTelegramForumTopicRootMessage(reply),
		"reply_to_text_len", len(text),
		"reply_to_text_preview", textutil.TruncateRunes(text, 160),
		"reply_to_has_document", reply.Document != nil,
		"reply_to_photo_count", len(reply.Photo),
	}
	if reply.Chat != nil {
		fields = append(fields,
			"reply_to_chat_id", reply.Chat.ID,
			"reply_to_chat_type", strings.TrimSpace(reply.Chat.Type),
		)
	}
	if reply.From != nil {
		fields = append(fields,
			"reply_to_from_user_id", reply.From.ID,
			"reply_to_from_is_bot", reply.From.IsBot,
			"reply_to_from_username", strings.TrimSpace(reply.From.Username),
			"reply_to_from_first_name", strings.TrimSpace(reply.From.FirstName),
		)
	}
	return fields
}

func isTelegramForumTopicRootMessage(msg *telegramMessage) bool {
	return msg != nil && len(msg.ForumTopicCreated) > 0
}

func recordTelegramQueuedTask(store daemonruntime.TaskView, info daemonruntime.TaskInfo, trigger daemonruntime.TaskTrigger, topicTitle string) error {
	if store == nil {
		return nil
	}
	if writer, ok := store.(interface {
		UpsertWithTrigger(daemonruntime.TaskInfo, daemonruntime.TaskTrigger, string) error
	}); ok {
		return writer.UpsertWithTrigger(info, trigger, topicTitle)
	}
	return taskdomain.RecordTaskUpsert(store, info, trigger)
}

func isTaskContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded")
}
