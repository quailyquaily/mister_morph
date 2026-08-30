package line

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type lineAPI struct {
	http               *http.Client
	baseURL            string
	channelAccessToken string
}

func newLineAPI(httpClient *http.Client, baseURL, channelAccessToken string) *lineAPI {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		baseURL = "https://api.line.me"
	}
	return &lineAPI{
		http:               httpClient,
		baseURL:            baseURL,
		channelAccessToken: strings.TrimSpace(channelAccessToken),
	}
}

type lineTextMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type lineBotInfoResponse struct {
	UserID string `json:"userId,omitempty"`
}

type lineUserProfile struct {
	DisplayName string `json:"displayName,omitempty"`
	UserID      string `json:"userId,omitempty"`
	PictureURL  string `json:"pictureUrl,omitempty"`
}

type lineReplyRequest struct {
	ReplyToken string            `json:"replyToken"`
	Messages   []lineTextMessage `json:"messages"`
}

type linePushRequest struct {
	To       string            `json:"to"`
	Messages []lineTextMessage `json:"messages"`
}

func (api *lineAPI) replyMessage(ctx context.Context, replyToken string, text string) error {
	if api == nil {
		return fmt.Errorf("line api is not initialized")
	}
	replyToken = strings.TrimSpace(replyToken)
	if replyToken == "" {
		return fmt.Errorf("line reply token is required")
	}
	return api.postJSON(ctx, "/v2/bot/message/reply", lineReplyRequest{
		ReplyToken: replyToken,
		Messages:   []lineTextMessage{{Type: "text", Text: strings.TrimSpace(text)}},
	})
}

func (api *lineAPI) pushMessage(ctx context.Context, chatID string, text string) error {
	if api == nil {
		return fmt.Errorf("line api is not initialized")
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("line chat id is required")
	}
	return api.postJSON(ctx, "/v2/bot/message/push", linePushRequest{
		To:       chatID,
		Messages: []lineTextMessage{{Type: "text", Text: strings.TrimSpace(text)}},
	})
}

func (api *lineAPI) botUserID(ctx context.Context) (string, error) {
	if api == nil {
		return "", fmt.Errorf("line api is not initialized")
	}
	url := api.baseURL + "/v2/bot/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+api.channelAccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return "", err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", parseLineAPIError(resp.StatusCode, raw)
	}
	var out lineBotInfoResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	userID := strings.TrimSpace(out.UserID)
	if userID == "" {
		return "", fmt.Errorf("line bot info returned empty user id")
	}
	return userID, nil
}

func (api *lineAPI) userProfile(ctx context.Context, chatType, chatID, userID string) (lineUserProfile, error) {
	if api == nil {
		return lineUserProfile{}, fmt.Errorf("line api is not initialized")
	}
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return lineUserProfile{}, fmt.Errorf("line user id is required")
	}
	var path string
	switch chatType {
	case "user", "private":
		path = "/v2/bot/profile/" + url.PathEscape(userID)
	case "group":
		if chatID == "" {
			return lineUserProfile{}, fmt.Errorf("line group id is required")
		}
		path = "/v2/bot/group/" + url.PathEscape(chatID) + "/member/" + url.PathEscape(userID)
	default:
		return lineUserProfile{}, fmt.Errorf("unsupported line chat type %q", chatType)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api.baseURL+path, nil)
	if err != nil {
		return lineUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+api.channelAccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return lineUserProfile{}, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return lineUserProfile{}, parseLineAPIError(resp.StatusCode, raw)
	}
	var profile lineUserProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return lineUserProfile{}, err
	}
	return profile, nil
}

func (api *lineAPI) messageContent(ctx context.Context, messageID string, maxBytes int64) ([]byte, string, error) {
	if api == nil {
		return nil, "", fmt.Errorf("line api is not initialized")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, "", fmt.Errorf("line message id is required")
	}
	if maxBytes <= 0 {
		return nil, "", fmt.Errorf("line max bytes must be positive")
	}
	endpoint := api.contentBaseURL() + "/v2/bot/message/" + url.PathEscape(messageID) + "/content"
	return api.fetchMessageContentOnce(ctx, endpoint, maxBytes)
}

func (api *lineAPI) fetchMessageContentOnce(ctx context.Context, endpoint string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+api.channelAccessToken)
	req.Header.Set("Accept", "*/*")
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, "", parseLineAPIError(resp.StatusCode, raw)
	}
	limited := io.LimitReader(resp.Body, maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", fmt.Errorf("line image too large: %d bytes > %d bytes", len(raw), maxBytes)
	}
	contentType := strings.TrimSpace(strings.ToLower(resp.Header.Get("Content-Type")))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	return raw, contentType, nil
}

func (api *lineAPI) contentBaseURL() string {
	if api == nil {
		return "https://api-data.line.me"
	}
	base := strings.TrimSpace(strings.TrimRight(api.baseURL, "/"))
	if base == "" {
		return "https://api-data.line.me"
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	if strings.EqualFold(strings.TrimSpace(parsed.Host), "api.line.me") {
		parsed.Host = "api-data.line.me"
		return strings.TrimRight(parsed.String(), "/")
	}
	return base
}

type lineAPIError struct {
	Status  int
	Message string
	Details []string
}

func (e *lineAPIError) Error() string {
	if e == nil {
		return "line api error"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "unknown_error"
	}
	if len(e.Details) == 0 {
		return fmt.Sprintf("line api http %d: %s", e.Status, msg)
	}
	return fmt.Sprintf("line api http %d: %s (%s)", e.Status, msg, strings.Join(e.Details, "; "))
}

type lineErrorResponse struct {
	Message string `json:"message,omitempty"`
	Details []struct {
		Message  string `json:"message,omitempty"`
		Property string `json:"property,omitempty"`
	} `json:"details,omitempty"`
}

func (api *lineAPI) postJSON(ctx context.Context, path string, payload any) error {
	if api == nil {
		return fmt.Errorf("line api is not initialized")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("line api path is required")
	}
	bodyRaw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := api.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyRaw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+api.channelAccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return parseLineAPIError(resp.StatusCode, raw)
}

func parseLineAPIError(status int, raw []byte) error {
	out := lineErrorResponse{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return &lineAPIError{
			Status:  status,
			Message: strings.TrimSpace(string(raw)),
		}
	}
	details := make([]string, 0, len(out.Details))
	for _, detail := range out.Details {
		part := strings.TrimSpace(detail.Message)
		property := strings.TrimSpace(detail.Property)
		if property != "" {
			if part != "" {
				part = property + ": " + part
			} else {
				part = property
			}
		}
		if part == "" {
			continue
		}
		details = append(details, part)
	}
	return &lineAPIError{
		Status:  status,
		Message: strings.TrimSpace(out.Message),
		Details: details,
	}
}

func shouldFallbackToLinePush(err error) bool {
	var apiErr *lineAPIError
	if !errors.As(err, &apiErr) || apiErr == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(apiErr.Message))
	if msg == "" {
		msg = strings.ToLower(strings.TrimSpace(strings.Join(apiErr.Details, " ")))
	}
	msg = strings.ReplaceAll(msg, "_", " ")
	return strings.Contains(msg, "reply token") || strings.Contains(msg, "replytoken")
}

func sendLineText(ctx context.Context, api *lineAPI, logger *slog.Logger, chatID string, text string, replyToken string) error {
	if api == nil {
		return fmt.Errorf("line api is not initialized")
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("line chat id is required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("line text is required")
	}
	replyToken = strings.TrimSpace(replyToken)
	if replyToken != "" {
		if err := api.replyMessage(ctx, replyToken, text); err == nil {
			return nil
		} else if shouldFallbackToLinePush(err) {
			if logger != nil {
				logger.Warn("line_reply_failed_fallback_push",
					"chat_id", chatID,
					"error_class", "reply_token_invalid_or_expired",
					"error", err.Error(),
				)
			}
		} else {
			return err
		}
	}
	return api.pushMessage(ctx, chatID, text)
}
