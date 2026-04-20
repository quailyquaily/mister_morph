package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
)

const mentionUserSnapshotLimit = 12

func collectMentionCandidates(msg *telegramMessage, botUser string) []string {
	if msg == nil {
		return nil
	}
	var out []string
	add := func(username string) {
		username = strings.TrimSpace(username)
		if username == "" {
			return
		}
		if strings.HasPrefix(username, "@") {
			username = strings.TrimSpace(username[1:])
		}
		if username == "" {
			return
		}
		if botUser != "" && strings.EqualFold(username, botUser) {
			return
		}
		out = append(out, "@"+username)
	}
	if msg.From != nil && !msg.From.IsBot {
		add(msg.From.Username)
	}
	if msg.ReplyTo != nil && msg.ReplyTo.From != nil && !msg.ReplyTo.From.IsBot {
		add(msg.ReplyTo.From.Username)
	}
	addEntities := func(text string, entities []telegramEntity) {
		if strings.TrimSpace(text) == "" || len(entities) == 0 {
			return
		}
		for _, e := range entities {
			switch strings.ToLower(strings.TrimSpace(e.Type)) {
			case "text_mention":
				if e.User != nil {
					add(e.User.Username)
				}
			case "mention":
				mention := strings.TrimSpace(sliceByUTF16(text, e.Offset, e.Length))
				if mention == "" {
					continue
				}
				add(strings.TrimPrefix(mention, "@"))
			}
		}
	}
	addEntities(msg.Text, msg.Entities)
	addEntities(msg.Caption, msg.CaptionEntities)
	return out
}

func addKnownUsernames(known map[int64]map[string]string, chatID int64, usernames []string) {
	if chatID == 0 || len(usernames) == 0 {
		return
	}
	set := known[chatID]
	if set == nil {
		set = make(map[string]string)
		known[chatID] = set
	}
	for _, username := range usernames {
		username = strings.TrimSpace(username)
		if username == "" {
			continue
		}
		if strings.HasPrefix(username, "@") {
			username = strings.TrimSpace(username[1:])
		}
		if username == "" {
			continue
		}
		key := strings.ToLower(username)
		if _, ok := set[key]; ok {
			continue
		}
		set[key] = "@" + username
	}
}

func isGroupChat(chatType string) bool {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	return chatType == "group" || chatType == "supergroup"
}

func busErrorCodeString(err error) string {
	if err == nil {
		return ""
	}
	return string(busruntime.ErrorCodeOf(err))
}

func publishTelegramBusOutbound(ctx context.Context, inprocBus *busruntime.Inproc, chatID int64, text string, replyTo string, correlationID string) (string, error) {
	if inprocBus == nil {
		return "", fmt.Errorf("bus is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	replyTo = strings.TrimSpace(replyTo)
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
		ReplyTo:   replyTo,
	})
	if err != nil {
		return "", err
	}
	conversationKey, err := busruntime.BuildTelegramChatConversationKey(strconv.FormatInt(chatID, 10))
	if err != nil {
		return "", err
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		correlationID = "telegram:" + messageID
	}
	outbound := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(messageID),
		CorrelationID:   correlationID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			SessionID: sessionID,
			ReplyTo:   replyTo,
		},
	}
	if err := inprocBus.PublishValidated(ctx, outbound); err != nil {
		return "", err
	}
	return messageID, nil
}

func applyTelegramInboundFeedback(
	ctx context.Context,
	svc *contacts.Service,
	chatID int64,
	chatType string,
	userID int64,
	username string,
	now time.Time,
) error {
	_ = ctx
	_ = svc
	_ = chatID
	_ = chatType
	_ = userID
	_ = username
	_ = now
	return nil
}

func telegramMemoryContactID(username string, userID int64) string {
	username = strings.TrimSpace(username)
	username = strings.TrimPrefix(username, "@")
	username = strings.TrimSpace(username)
	if username != "" {
		return "tg:@" + username
	}
	if userID > 0 {
		return fmt.Sprintf("tg:%d", userID)
	}
	return ""
}

const (
	telegramHistoryCapTalkative = 16
	telegramHistoryCapDefault   = 8
	telegramStickySkillsCap     = 3
)

func telegramHistoryCapForMode(mode string) int {
	if strings.EqualFold(strings.TrimSpace(mode), "talkative") {
		return telegramHistoryCapTalkative
	}
	return telegramHistoryCapDefault
}

func trimChatHistoryItems(items []chathistory.ChatHistoryItem, max int) []chathistory.ChatHistoryItem {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[len(items)-max:]
}

var telegramAtMentionPattern = regexp.MustCompile(`@[A-Za-z0-9_]{3,64}`)

func formatTelegramPersonReference(nickname string, username string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	nickname = sanitizeTelegramReferenceLabel(nickname)
	if nickname == "" {
		nickname = username
	}
	if nickname == "" {
		return ""
	}
	if username == "" {
		return nickname
	}
	return "[" + nickname + "](tg:@" + username + ")"
}

func sanitizeTelegramReferenceLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	label = strings.ReplaceAll(label, "[", "")
	label = strings.ReplaceAll(label, "]", "")
	label = strings.ReplaceAll(label, "(", "")
	label = strings.ReplaceAll(label, ")", "")
	label = strings.Join(strings.Fields(label), " ")
	return strings.TrimSpace(label)
}

func formatTelegramAtMentionsForHistory(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	matches := telegramAtMentionPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var out strings.Builder
	out.Grow(len(text) + len(matches)*12)
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if start < last || start < 0 || end > len(text) || start >= end {
			continue
		}
		username := strings.TrimPrefix(text[start:end], "@")
		lower := strings.ToLower(text)
		if start >= 4 && lower[start-4:start] == "tg:@" {
			continue
		}
		out.WriteString(text[last:start])
		ref := formatTelegramPersonReference(username, username)
		if ref == "" {
			out.WriteString(text[start:end])
		} else {
			out.WriteString(ref)
		}
		last = end
	}
	out.WriteString(text[last:])
	return strings.TrimSpace(out.String())
}

func ensureMarkdownBlockquote(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			lines[i] = ">"
			continue
		}
		if strings.HasPrefix(line, ">") {
			lines[i] = line
			continue
		}
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

func stripMarkdownBlockquote(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		lines[i] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractQuoteSenderRef(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	firstLine := text
	if idx := strings.Index(firstLine, "\n"); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(firstLine), ">"))
	if firstLine == "" {
		return ""
	}
	end := strings.Index(firstLine, ")")
	if end <= 0 {
		return ""
	}
	candidate := strings.TrimSpace(firstLine[:end+1])
	if strings.HasPrefix(candidate, "[") && strings.Contains(candidate, "](tg:@") {
		return candidate
	}
	return ""
}

func splitTaskQuoteForHistory(task string) (string, *chathistory.ChatHistoryQuote) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", nil
	}
	const (
		prefix = "Quoted message:\n"
		sep    = "\n\nUser request:\n"
	)
	if !strings.HasPrefix(task, prefix) {
		return task, nil
	}
	rest := strings.TrimPrefix(task, prefix)
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return task, nil
	}
	quoteRaw := strings.TrimSpace(rest[:idx])
	mainText := strings.TrimSpace(rest[idx+len(sep):])
	quoteRaw = formatTelegramAtMentionsForHistory(quoteRaw)
	block := ensureMarkdownBlockquote(quoteRaw)
	if block == "" {
		return mainText, nil
	}
	return mainText, &chathistory.ChatHistoryQuote{
		SenderRef:     extractQuoteSenderRef(block),
		Text:          stripMarkdownBlockquote(block),
		MarkdownBlock: block,
	}
}

func telegramSenderFromJob(job telegramJob) chathistory.ChatHistorySender {
	username := strings.TrimPrefix(strings.TrimSpace(job.FromUsername), "@")
	nickname := strings.TrimSpace(job.FromDisplayName)
	if nickname == "" {
		first := strings.TrimSpace(job.FromFirstName)
		last := strings.TrimSpace(job.FromLastName)
		switch {
		case first != "" && last != "":
			nickname = first + " " + last
		case first != "":
			nickname = first
		case last != "":
			nickname = last
		default:
			nickname = username
		}
	}
	return chathistory.ChatHistorySender{
		UserID:     strconv.FormatInt(job.FromUserID, 10),
		Username:   username,
		Nickname:   nickname,
		DisplayRef: formatTelegramPersonReference(nickname, username),
	}
}

func newTelegramInboundHistoryItem(job telegramJob) chathistory.ChatHistoryItem {
	sentAt := job.SentAt.UTC()
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	text, quote := splitTaskQuoteForHistory(job.Text)
	text = formatTelegramAtMentionsForHistory(text)
	if text == "" {
		text = "(empty)"
	}
	messageID := ""
	if job.MessageID > 0 {
		messageID = strconv.FormatInt(job.MessageID, 10)
	}
	replyToMessageID := ""
	if job.ReplyToMessageID > 0 {
		replyToMessageID = strconv.FormatInt(job.ReplyToMessageID, 10)
	}
	return chathistory.ChatHistoryItem{
		Channel:          chathistory.ChannelTelegram,
		Kind:             chathistory.KindInboundUser,
		ChatID:           strconv.FormatInt(job.ChatID, 10),
		ChatType:         strings.TrimSpace(job.ChatType),
		MessageID:        messageID,
		ReplyToMessageID: replyToMessageID,
		SentAt:           sentAt,
		Sender:           telegramSenderFromJob(job),
		Text:             text,
		Quote:            quote,
	}
}

func newTelegramOutboundAgentHistoryItem(chatID int64, chatType string, text string, sentAt time.Time, botUser string) chathistory.ChatHistoryItem {
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	botUser = strings.TrimPrefix(strings.TrimSpace(botUser), "@")
	nickname := botUser
	if nickname == "" {
		nickname = "MisterMorph"
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty)"
	}
	return chathistory.ChatHistoryItem{
		Channel:  chathistory.ChannelTelegram,
		Kind:     chathistory.KindOutboundAgent,
		ChatID:   strconv.FormatInt(chatID, 10),
		ChatType: strings.TrimSpace(chatType),
		SentAt:   sentAt.UTC(),
		Sender: chathistory.ChatHistorySender{
			Username:   botUser,
			Nickname:   nickname,
			IsBot:      true,
			DisplayRef: formatTelegramPersonReference(nickname, botUser),
		},
		Text: text,
	}
}

func newTelegramOutboundReactionHistoryItem(chatID int64, chatType string, note string, emoji string, sentAt time.Time, botUser string) chathistory.ChatHistoryItem {
	item := newTelegramOutboundAgentHistoryItem(chatID, chatType, note, sentAt, botUser)
	item.Kind = chathistory.KindOutboundReaction
	if strings.TrimSpace(emoji) != "" {
		item.Text = strings.TrimSpace(note)
	}
	return item
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildReplyContext(msg *telegramMessage) string {
	if msg == nil {
		return ""
	}
	text := strings.TrimSpace(messageTextOrCaption(msg))
	if text == "" && messageHasDownloadableFile(msg) {
		text = "[attachment]"
	}
	if text == "" {
		return ""
	}
	text = truncateRunes(text, 2000)
	if msg.From != nil && strings.TrimSpace(msg.From.Username) != "" {
		return "@" + strings.TrimSpace(msg.From.Username) + ": " + text
	}
	return text
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func sliceByUTF16(s string, offset, length int) string {
	if offset < 0 {
		offset = 0
	}
	if length <= 0 || s == "" {
		return ""
	}
	start := utf16OffsetToByteIndex(s, offset)
	end := utf16OffsetToByteIndex(s, offset+length)
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start > end {
		return ""
	}
	return s[start:end]
}

func utf16OffsetToByteIndex(s string, offset int) int {
	if offset <= 0 {
		return 0
	}
	utf16Count := 0
	for i, r := range s {
		if utf16Count >= offset {
			return i
		}
		if r <= 0xFFFF {
			utf16Count++
		} else {
			utf16Count += 2
		}
	}
	return len(s)
}

func (api *telegramAPI) sendChatAction(ctx context.Context, chatID int64, action string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "typing"
	}
	reqBody := telegramSendChatActionRequest{ChatID: chatID, Action: action}
	b, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/bot%s/sendChatAction", api.baseURL, api.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func startTypingTicker(ctx context.Context, api *telegramAPI, chatID int64, action string, interval time.Duration) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	if api == nil || chatID == 0 {
		return func() {}
	}
	if interval <= 0 {
		interval = 4 * time.Second
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "typing"
	}

	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		_ = api.sendChatAction(ctx, chatID, action)
		for {
			select {
			case <-ticker.C:
				_ = api.sendChatAction(ctx, chatID, action)
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() {
		select {
		case <-done:
		default:
			close(done)
		}
		ticker.Stop()
	}
}

type telegramDownloadedFile struct {
	Kind         string
	OriginalName string
	MimeType     string
	SizeBytes    int64
	Path         string
}
