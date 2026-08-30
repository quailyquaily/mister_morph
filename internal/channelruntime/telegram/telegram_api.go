package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Telegram API

type telegramAPI struct {
	http    *http.Client
	baseURL string
	token   string
}

func newTelegramAPI(httpClient *http.Client, baseURL, token string) *telegramAPI {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &telegramAPI{
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
	}
}

type telegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message,omitempty"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
	// Some clients/users may @mention by editing an existing message.
	EditedMessage     *telegramMessage `json:"edited_message,omitempty"`
	ChannelPost       *telegramMessage `json:"channel_post,omitempty"`
	EditedChannelPost *telegramMessage `json:"edited_channel_post,omitempty"`
}

type telegramCallbackQuery struct {
	ID      string           `json:"id,omitempty"`
	From    *telegramUser    `json:"from,omitempty"`
	Message *telegramMessage `json:"message,omitempty"`
	Data    string           `json:"data,omitempty"`
}

type telegramMessage struct {
	MessageID       int64            `json:"message_id"`
	MessageThreadID int64            `json:"message_thread_id,omitempty"`
	IsTopicMessage  bool             `json:"is_topic_message,omitempty"`
	Date            int64            `json:"date,omitempty"`
	Chat            *telegramChat    `json:"chat,omitempty"`
	From            *telegramUser    `json:"from,omitempty"`
	ReplyTo         *telegramMessage `json:"reply_to_message,omitempty"`
	Entities        []telegramEntity `json:"entities,omitempty"`
	Text            string           `json:"text,omitempty"`
	Caption         string           `json:"caption,omitempty"`
	// Entities inside caption text.
	CaptionEntities []telegramEntity `json:"caption_entities,omitempty"`

	// Attachments (subset).
	Document *telegramDocument   `json:"document,omitempty"`
	Photo    []telegramPhotoSize `json:"photo,omitempty"`

	// Telegram uses the forum topic creation service message as the topic root.
	ForumTopicCreated json.RawMessage `json:"forum_topic_created,omitempty"`
}

type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type,omitempty"` // private|group|supergroup|channel
}

type telegramUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

func telegramDisplayName(u *telegramUser) string {
	if u == nil {
		return ""
	}
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	username := strings.TrimSpace(u.Username)
	switch {
	case first != "" && last != "":
		return first + " " + last
	case first != "":
		return first
	case last != "":
		return last
	case username != "":
		return "@" + username
	default:
		return ""
	}
}

type telegramEntity struct {
	Type   string        `json:"type"`
	Offset int           `json:"offset"`
	Length int           `json:"length"`
	User   *telegramUser `json:"user,omitempty"` // for text_mention
}

type telegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type telegramPhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type telegramGetUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []telegramUpdate `json:"result"`
}

type telegramGetMeResponse struct {
	OK     bool         `json:"ok"`
	Result telegramUser `json:"result"`
}

type telegramUserProfilePhotosResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Photos [][]telegramPhotoSize `json:"photos"`
	} `json:"result"`
}

func (api *telegramAPI) getMe(ctx context.Context) (*telegramUser, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", api.baseURL, api.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, telegramTransportError(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out telegramGetMeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram getMe: ok=false")
	}
	return &out.Result, nil
}

func (api *telegramAPI) getUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]telegramUpdate, int64, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=%d", api.baseURL, api.token, secs)
	if offset > 0 {
		url += fmt.Sprintf("&offset=%d", offset)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, offset, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, offset, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, offset, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out telegramGetUpdatesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, offset, err
	}
	if !out.OK {
		return nil, offset, fmt.Errorf("telegram getUpdates: ok=false")
	}

	next := offset
	for _, u := range out.Result {
		if u.UpdateID >= next {
			next = u.UpdateID + 1
		}
	}
	return out.Result, next, nil
}

func isTelegramPollTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client.timeout exceeded")
}

type telegramSendMessageRequest struct {
	ChatID                int64                         `json:"chat_id"`
	MessageThreadID       int64                         `json:"message_thread_id,omitempty"`
	Text                  string                        `json:"text"`
	ParseMode             string                        `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                          `json:"disable_web_page_preview,omitempty"`
	ReplyToMessageID      int64                         `json:"reply_to_message_id,omitempty"`
	ReplyMarkup           *telegramInlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type telegramDirectMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type telegramEditMessageTextRequest struct {
	ChatID                int64  `json:"chat_id"`
	MessageID             int64  `json:"message_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

type telegramInlineKeyboardMarkup struct {
	InlineKeyboard [][]telegramInlineKeyboardButton `json:"inline_keyboard"`
}

type telegramInlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type telegramAnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

type telegramSendChatActionRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Action          string `json:"action"`
}

type telegramReactionType struct {
	Type          string `json:"type"`
	Emoji         string `json:"emoji,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

type telegramSetMessageReactionRequest struct {
	ChatID    int64                  `json:"chat_id"`
	MessageID int64                  `json:"message_id"`
	Reaction  []telegramReactionType `json:"reaction,omitempty"`
	IsBig     *bool                  `json:"is_big,omitempty"`
}

type telegramOKResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code,omitempty"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type telegramFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
}

type telegramGetFileResponse struct {
	OK     bool         `json:"ok"`
	Result telegramFile `json:"result"`
}

func (api *telegramAPI) getFile(ctx context.Context, fileID string) (*telegramFile, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("missing file_id")
	}
	url := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", api.baseURL, api.token, url.QueryEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, telegramTransportError(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out telegramGetFileResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram getFile: ok=false")
	}
	if strings.TrimSpace(out.Result.FilePath) == "" {
		return nil, fmt.Errorf("telegram getFile: missing file_path")
	}
	return &out.Result, nil
}

func (api *telegramAPI) contactAvatar(ctx context.Context, userID int64) ([]byte, bool, error) {
	if userID <= 0 {
		return nil, false, fmt.Errorf("telegram user id is required")
	}
	endpoint := fmt.Sprintf("%s/bot%s/getUserProfilePhotos?user_id=%d&offset=0&limit=1", api.baseURL, api.token, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, false, telegramTransportError(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out telegramUserProfilePhotosResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, err
	}
	if !out.OK {
		return nil, false, fmt.Errorf("telegram getUserProfilePhotos: ok=false")
	}
	if len(out.Result.Photos) == 0 || len(out.Result.Photos[0]) == 0 {
		return nil, false, nil
	}
	photo := out.Result.Photos[0][0]
	for _, candidate := range out.Result.Photos[0][1:] {
		if candidate.Width*candidate.Height > photo.Width*photo.Height {
			photo = candidate
		}
	}
	file, err := api.getFile(ctx, photo.FileID)
	if err != nil {
		return nil, false, err
	}
	fileResponse, err := api.openFile(ctx, file.FilePath)
	if err != nil {
		return nil, false, err
	}
	defer fileResponse.Body.Close()
	const maxBytes = int64(5 << 20)
	raw, err = io.ReadAll(io.LimitReader(fileResponse.Body, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, false, fmt.Errorf("telegram contact avatar too large")
	}
	if len(raw) == 0 {
		return nil, false, fmt.Errorf("telegram contact avatar is empty")
	}
	return raw, true, nil
}

func (api *telegramAPI) openFile(ctx context.Context, filePath string) (*http.Response, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, fmt.Errorf("missing file_path")
	}
	endpoint := fmt.Sprintf("%s/file/bot%s/%s", api.baseURL, api.token, strings.TrimLeft(filePath, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, telegramTransportError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("telegram download http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return resp, nil
}

func telegramTransportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return fmt.Errorf("telegram request failed: %w", err)
}

func (api *telegramAPI) downloadFileTo(ctx context.Context, filePath, dstPath string, maxBytes int64) (int64, bool, error) {
	filePath = strings.TrimSpace(filePath)
	dstPath = strings.TrimSpace(dstPath)
	if filePath == "" {
		return 0, false, fmt.Errorf("missing file_path")
	}
	if dstPath == "" {
		return 0, false, fmt.Errorf("missing dst_path")
	}
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}

	resp, err := api.openFile(ctx, filePath)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	limited := io.LimitReader(resp.Body, maxBytes+1)
	n, err := io.Copy(f, limited)
	if err != nil {
		return n, false, err
	}
	if n > maxBytes {
		return n, true, fmt.Errorf("telegram file too large (>%d bytes)", maxBytes)
	}
	if err := f.Close(); err != nil {
		return n, false, err
	}
	if err := os.Chmod(dstPath, 0o600); err != nil {
		return n, false, err
	}
	return n, false, nil
}

func (api *telegramAPI) sendMessageHTMLInThread(ctx context.Context, chatID int64, messageThreadID int64, text string, disablePreview bool) error {
	_, err := api.sendMessageHTMLReplyInThreadWithMessageID(ctx, chatID, messageThreadID, text, disablePreview, 0)
	return err
}

func (api *telegramAPI) sendMessageHTMLReplyInThreadWithMessageID(ctx context.Context, chatID int64, messageThreadID int64, text string, disablePreview bool, replyToMessageID int64) (int64, error) {
	return api.sendMessageHTMLReplyInThreadWithMessageIDAndMarkup(ctx, chatID, messageThreadID, text, disablePreview, replyToMessageID, nil)
}

func (api *telegramAPI) sendMessageHTMLReplyInThreadWithMessageIDAndMarkup(ctx context.Context, chatID int64, messageThreadID int64, text string, disablePreview bool, replyToMessageID int64, replyMarkup *telegramInlineKeyboardMarkup) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty)"
	}
	converted, convErr := renderTelegramHTMLFromMarkdown(text)
	if convErr != nil {
		slog.Warn("failed to render telegram html", "text", text, "error", convErr)
		return api.sendMessageWithParseModeReplyAndMessageIDAndMarkup(ctx, chatID, messageThreadID, text, disablePreview, "", replyToMessageID, replyMarkup)
	}

	messageID, err := api.sendMessageWithParseModeReplyAndMessageIDAndMarkup(ctx, chatID, messageThreadID, converted, disablePreview, "HTML", replyToMessageID, replyMarkup)
	if err != nil {
		if !isTelegramEntityParseError(err) {
			slog.Warn("failed to send telegram html message", "text", text, "error", err)
			return 0, err
		}
		slog.Warn("failed to parse telegram html entities; send plain-text fallback", "text", text, "error", err)
		return api.sendMessageWithParseModeReplyAndMessageIDAndMarkup(ctx, chatID, messageThreadID, text, disablePreview, "", replyToMessageID, replyMarkup)
	}
	return messageID, nil
}

func (api *telegramAPI) editMessageHTML(ctx context.Context, chatID int64, messageID int64, text string, disablePreview bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty)"
	}
	converted, convErr := renderTelegramHTMLFromMarkdown(text)
	if convErr != nil {
		slog.Warn("failed to render telegram html", "text", text, "error", convErr)
		return api.editMessageWithParseMode(ctx, chatID, messageID, text, disablePreview, "")
	}
	err := api.editMessageWithParseMode(ctx, chatID, messageID, converted, disablePreview, "HTML")
	if err != nil {
		if !isTelegramEntityParseError(err) {
			slog.Warn("failed to edit telegram html message", "text", text, "error", err)
			return err
		}
		slog.Warn("failed to parse telegram html entities while editing; use plain-text fallback", "text", text, "error", err)
		return api.editMessageWithParseMode(ctx, chatID, messageID, text, disablePreview, "")
	}
	return nil
}

type telegramRequestError struct {
	StatusCode  int
	ErrorCode   int
	Description string
	Body        string
}

func (e *telegramRequestError) Error() string {
	if e == nil {
		return "telegram request failed"
	}
	desc := strings.TrimSpace(e.Description)
	if desc != "" {
		if e.StatusCode > 0 {
			return fmt.Sprintf("telegram http %d: %s", e.StatusCode, desc)
		}
		return "telegram: " + desc
	}
	body := strings.TrimSpace(e.Body)
	if e.StatusCode > 0 {
		if body != "" {
			return fmt.Sprintf("telegram http %d: %s", e.StatusCode, body)
		}
		return fmt.Sprintf("telegram http %d", e.StatusCode)
	}
	if body != "" {
		return "telegram: " + body
	}
	return "telegram request failed"
}

func isTelegramEntityParseError(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *telegramRequestError
	if errors.As(err, &reqErr) {
		desc := strings.ToLower(strings.TrimSpace(reqErr.Description))
		if strings.Contains(desc, "can't parse entities") || strings.Contains(desc, "can't parse entity") {
			return true
		}
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "can't parse entities") || strings.Contains(msg, "can't parse entity")
}

func isTelegramMessageNotModified(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *telegramRequestError
	if errors.As(err, &reqErr) {
		desc := strings.ToLower(strings.TrimSpace(reqErr.Description))
		if strings.Contains(desc, "message is not modified") {
			return true
		}
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "message is not modified")
}

func (api *telegramAPI) sendMessageChunkedReplyInThread(ctx context.Context, chatID int64, messageThreadID int64, text string, replyToMessageID int64) error {
	_, err := api.sendMessageChunkedReplyInThreadWithFirstMessageID(ctx, chatID, messageThreadID, text, replyToMessageID)
	return err
}

func (api *telegramAPI) sendMessageChunkedReplyInThreadWithFirstMessageID(ctx context.Context, chatID int64, messageThreadID int64, text string, replyToMessageID int64) (int64, error) {
	const max = 3500
	text = strings.TrimSpace(text)
	if text == "" {
		return api.sendMessageHTMLReplyInThreadWithMessageID(ctx, chatID, messageThreadID, "(empty)", true, replyToMessageID)
	}
	firstMessageID := int64(0)
	isFirstChunk := true
	for len(text) > 0 {
		chunk := text
		if len(chunk) > max {
			chunk = chunk[:max]
		}
		chunkReplyTo := int64(0)
		if isFirstChunk {
			chunkReplyTo = replyToMessageID
		}
		messageID, err := api.sendMessageHTMLReplyInThreadWithMessageID(ctx, chatID, messageThreadID, chunk, true, chunkReplyTo)
		if err != nil {
			return 0, err
		}
		if isFirstChunk {
			firstMessageID = messageID
		}
		text = strings.TrimSpace(text[len(chunk):])
		isFirstChunk = false
	}
	return firstMessageID, nil
}

func (api *telegramAPI) sendMessageWithParseModeReplyAndMessageIDAndMarkup(ctx context.Context, chatID int64, messageThreadID int64, text string, disablePreview bool, parseMode string, replyToMessageID int64, replyMarkup *telegramInlineKeyboardMarkup) (int64, error) {
	reqBody := telegramSendMessageRequest{
		ChatID:                chatID,
		MessageThreadID:       messageThreadID,
		Text:                  text,
		ParseMode:             strings.TrimSpace(parseMode),
		DisableWebPagePreview: disablePreview,
		ReplyToMessageID:      replyToMessageID,
		ReplyMarkup:           replyMarkup,
	}
	return api.postSendMessage(ctx, reqBody)
}

func (api *telegramAPI) sendDirectText(ctx context.Context, chatTarget, text string) error {
	chatTarget = strings.TrimSpace(chatTarget)
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(chatTarget, "@") || len(chatTarget) == 1 {
		return fmt.Errorf("Telegram direct target must be a username")
	}
	if text == "" {
		return fmt.Errorf("Telegram direct message is empty")
	}
	_, err := api.postSendMessage(ctx, telegramDirectMessageRequest{ChatID: chatTarget, Text: text})
	return err
}

func (api *telegramAPI) postSendMessage(ctx context.Context, reqBody any) (int64, error) {
	b, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/bot%s/sendMessage", api.baseURL, api.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return 0, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var out telegramOKResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &telegramRequestError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   out.ErrorCode,
			Description: out.Description,
			Body:        strings.TrimSpace(string(raw)),
		}
	}
	if !out.OK {
		return 0, &telegramRequestError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   out.ErrorCode,
			Description: out.Description,
			Body:        strings.TrimSpace(string(raw)),
		}
	}
	if len(out.Result) == 0 {
		return 0, nil
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(out.Result, &result); err != nil {
		return 0, nil
	}
	return result.MessageID, nil
}

func (api *telegramAPI) answerCallbackQuery(ctx context.Context, callbackQueryID, text string, showAlert bool) error {
	callbackQueryID = strings.TrimSpace(callbackQueryID)
	if callbackQueryID == "" {
		return fmt.Errorf("telegram callback_query_id is required")
	}
	reqBody := telegramAnswerCallbackQueryRequest{
		CallbackQueryID: callbackQueryID,
		Text:            strings.TrimSpace(text),
		ShowAlert:       showAlert,
	}
	b, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/bot%s/answerCallbackQuery", api.baseURL, api.token)
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
	var out telegramOKResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !out.OK {
		return &telegramRequestError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   out.ErrorCode,
			Description: out.Description,
			Body:        strings.TrimSpace(string(raw)),
		}
	}
	return nil
}

func (api *telegramAPI) editMessageWithParseMode(ctx context.Context, chatID int64, messageID int64, text string, disablePreview bool, parseMode string) error {
	reqBody := telegramEditMessageTextRequest{
		ChatID:                chatID,
		MessageID:             messageID,
		Text:                  text,
		ParseMode:             strings.TrimSpace(parseMode),
		DisableWebPagePreview: disablePreview,
	}
	b, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/bot%s/editMessageText", api.baseURL, api.token)
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
	var out telegramOKResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &telegramRequestError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   out.ErrorCode,
			Description: out.Description,
			Body:        strings.TrimSpace(string(raw)),
		}
	}
	if !out.OK {
		return &telegramRequestError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   out.ErrorCode,
			Description: out.Description,
			Body:        strings.TrimSpace(string(raw)),
		}
	}
	return nil
}

func (api *telegramAPI) sendDocumentInThread(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, messageThreadID, filePath, filename, caption, "sendDocument", "document", "file")
}

func (api *telegramAPI) sendPhotoInThread(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, messageThreadID, filePath, filename, caption, "sendPhoto", "photo", "photo")
}

func (api *telegramAPI) sendVoiceInThread(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, messageThreadID, filePath, filename, caption, "sendVoice", "voice", "voice.ogg")
}

func (api *telegramAPI) sendMultipartFile(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string, method string, formField string, fallbackFilename string) error {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("missing file path")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("path is a directory: %s", filePath)
	}

	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = filepath.Base(filePath)
	}
	if filename == "" {
		filename = fallbackFilename
	}
	caption = strings.TrimSpace(caption)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		defer mw.Close()

		_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
		if messageThreadID > 0 {
			_ = mw.WriteField("message_thread_id", strconv.FormatInt(messageThreadID, 10))
		}
		if caption != "" {
			_ = mw.WriteField("caption", caption)
		}

		part, err := mw.CreateFormFile(formField, filename)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}()

	url := fmt.Sprintf("%s/bot%s/%s", api.baseURL, api.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := api.http.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var ok telegramOKResponse
	_ = json.Unmarshal(raw, &ok)
	if !ok.OK {
		return fmt.Errorf("telegram %s: ok=false", method)
	}
	return nil
}

func (api *telegramAPI) setMessageReaction(ctx context.Context, chatID int64, messageID int64, reactions []telegramReactionType, isBig *bool) error {
	if messageID == 0 {
		return fmt.Errorf("missing message_id")
	}
	reqBody := telegramSetMessageReactionRequest{
		ChatID:    chatID,
		MessageID: messageID,
		Reaction:  reactions,
		IsBig:     isBig,
	}
	b, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/bot%s/setMessageReaction", api.baseURL, api.token)
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
	var ok telegramOKResponse
	_ = json.Unmarshal(raw, &ok)
	if !ok.OK {
		return fmt.Errorf("telegram setMessageReaction: ok=false")
	}
	return nil
}
