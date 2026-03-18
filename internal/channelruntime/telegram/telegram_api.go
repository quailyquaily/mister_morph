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

// SendMessageHTML sends markdown text via Telegram Bot API using parse_mode=HTML.
// Markdown input is converted to Telegram-supported HTML tags before sending.
// If Telegram rejects HTML entity parsing, it automatically retries as plain text.
func SendMessageHTML(ctx context.Context, httpClient *http.Client, baseURL, token string, chatID int64, text string, disablePreview bool) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("telegram bot token is required")
	}
	if chatID == 0 {
		return fmt.Errorf("telegram chat id is required")
	}
	api := newTelegramAPI(httpClient, strings.TrimSpace(baseURL), token)
	return api.sendMessageHTML(ctx, chatID, text, disablePreview)
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
	// Some clients/users may @mention by editing an existing message.
	EditedMessage     *telegramMessage `json:"edited_message,omitempty"`
	ChannelPost       *telegramMessage `json:"channel_post,omitempty"`
	EditedChannelPost *telegramMessage `json:"edited_channel_post,omitempty"`
}

type telegramMessage struct {
	MessageID int64            `json:"message_id"`
	Date      int64            `json:"date,omitempty"`
	Chat      *telegramChat    `json:"chat,omitempty"`
	From      *telegramUser    `json:"from,omitempty"`
	ReplyTo   *telegramMessage `json:"reply_to_message,omitempty"`
	Entities  []telegramEntity `json:"entities,omitempty"`
	Text      string           `json:"text,omitempty"`
	Caption   string           `json:"caption,omitempty"`
	// Entities inside caption text.
	CaptionEntities []telegramEntity `json:"caption_entities,omitempty"`

	// Attachments (subset).
	Document *telegramDocument   `json:"document,omitempty"`
	Photo    []telegramPhotoSize `json:"photo,omitempty"`
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

func (api *telegramAPI) getMe(ctx context.Context) (*telegramUser, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", api.baseURL, api.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, err
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
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
	ReplyToMessageID      int64  `json:"reply_to_message_id,omitempty"`
}

type telegramEditMessageTextRequest struct {
	ChatID                int64  `json:"chat_id"`
	MessageID             int64  `json:"message_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

type telegramSendChatActionRequest struct {
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
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
		return nil, err
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

	url := fmt.Sprintf("%s/file/bot%s/%s", api.baseURL, api.token, strings.TrimLeft(filePath, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, false, fmt.Errorf("telegram download http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

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

func (api *telegramAPI) sendMessageHTML(ctx context.Context, chatID int64, text string, disablePreview bool) error {
	return api.sendMessageHTMLReply(ctx, chatID, text, disablePreview, 0)
}

func (api *telegramAPI) sendMessageHTMLReply(ctx context.Context, chatID int64, text string, disablePreview bool, replyToMessageID int64) error {
	_, err := api.sendMessageHTMLReplyWithMessageID(ctx, chatID, text, disablePreview, replyToMessageID)
	return err
}

func (api *telegramAPI) sendMessageHTMLReplyWithMessageID(ctx context.Context, chatID int64, text string, disablePreview bool, replyToMessageID int64) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty)"
	}
	converted, convErr := renderTelegramHTMLFromMarkdown(text)
	if convErr != nil {
		slog.Warn("failed to render telegram html", "text", text, "error", convErr)
		return api.sendMessageWithParseModeReplyAndMessageID(ctx, chatID, text, disablePreview, "", replyToMessageID)
	}

	messageID, err := api.sendMessageWithParseModeReplyAndMessageID(ctx, chatID, converted, disablePreview, "HTML", replyToMessageID)
	if err != nil {
		if !isTelegramEntityParseError(err) {
			slog.Warn("failed to send telegram html message", "text", text, "error", err)
			return 0, err
		}
		slog.Warn("failed to parse telegram html entities; send plain-text fallback", "text", text, "error", err)
		return api.sendMessageWithParseModeReplyAndMessageID(ctx, chatID, text, disablePreview, "", replyToMessageID)
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

func (api *telegramAPI) sendMessageChunkedReply(ctx context.Context, chatID int64, text string, replyToMessageID int64) error {
	_, err := api.sendMessageChunkedReplyWithFirstMessageID(ctx, chatID, text, replyToMessageID)
	return err
}

func (api *telegramAPI) sendMessageChunkedReplyWithFirstMessageID(ctx context.Context, chatID int64, text string, replyToMessageID int64) (int64, error) {
	const max = 3500
	text = strings.TrimSpace(text)
	if text == "" {
		return api.sendMessageHTMLReplyWithMessageID(ctx, chatID, "(empty)", true, replyToMessageID)
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
		messageID, err := api.sendMessageHTMLReplyWithMessageID(ctx, chatID, chunk, true, chunkReplyTo)
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

func (api *telegramAPI) sendMessageWithParseModeReplyAndMessageID(ctx context.Context, chatID int64, text string, disablePreview bool, parseMode string, replyToMessageID int64) (int64, error) {
	reqBody := telegramSendMessageRequest{
		ChatID:                chatID,
		Text:                  text,
		ParseMode:             strings.TrimSpace(parseMode),
		DisableWebPagePreview: disablePreview,
		ReplyToMessageID:      replyToMessageID,
	}
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

func (api *telegramAPI) sendDocument(ctx context.Context, chatID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, filePath, filename, caption, "sendDocument", "document", "file")
}

func (api *telegramAPI) sendPhoto(ctx context.Context, chatID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, filePath, filename, caption, "sendPhoto", "photo", "photo")
}

func (api *telegramAPI) sendVoice(ctx context.Context, chatID int64, filePath string, filename string, caption string) error {
	return api.sendMultipartFile(ctx, chatID, filePath, filename, caption, "sendVoice", "voice", "voice.ogg")
}

func (api *telegramAPI) sendMultipartFile(ctx context.Context, chatID int64, filePath string, filename string, caption string, method string, formField string, fallbackFilename string) error {
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
