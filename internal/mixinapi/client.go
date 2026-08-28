package mixinapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIBaseURL      = "https://api.mixin.one"
	maxMessageRequestBytes = 128 << 10
	maxAPIResponseBytes    = 4 << 20
	maxMessageSendAttempts = 4
	defaultRetryBaseDelay  = time.Second
)

const (
	ConversationCategoryContact = "CONTACT"
	ConversationCategoryGroup   = "GROUP"
)

var ErrRequestTooLarge = errors.New("mixin request is too large")

const (
	MessageCategoryPlainText    = "PLAIN_TEXT"
	MessageCategoryPlainPost    = "PLAIN_POST"
	MessageCategoryPlainImage   = "PLAIN_IMAGE"
	MessageCategoryPlainAudio   = "PLAIN_AUDIO"
	MessageCategoryPlainData    = "PLAIN_DATA"
	MessageCategoryPlainVideo   = "PLAIN_VIDEO"
	MessageCategoryPlainSticker = "PLAIN_STICKER"
	MessageCategoryAppButton    = "APP_BUTTON_GROUP"
	MessageCategorySystem       = "SYSTEM_CONVERSATION"
)

type User struct {
	UserID         string `json:"user_id"`
	IdentityNumber string `json:"identity_number"`
	FullName       string `json:"full_name"`
	AvatarURL      string `json:"avatar_url"`
	AppID          string `json:"app_id"`
}

type ConversationParticipant struct {
	UserID string `json:"user_id"`
	Role   string `json:"role,omitempty"`
}

type Conversation struct {
	ConversationID string                    `json:"conversation_id"`
	Category       string                    `json:"category"`
	Name           string                    `json:"name"`
	IconURL        string                    `json:"icon_url"`
	Announcement   string                    `json:"announcement"`
	Participants   []ConversationParticipant `json:"participants"`
}

type CreateConversationRequest struct {
	ConversationID string                    `json:"conversation_id"`
	Category       string                    `json:"category"`
	Name           string                    `json:"name,omitempty"`
	Participants   []ConversationParticipant `json:"participants"`
}

type MessageRequest struct {
	ConversationID   string `json:"conversation_id"`
	RecipientID      string `json:"recipient_id,omitempty"`
	MessageID        string `json:"message_id"`
	Category         string `json:"category"`
	DataBase64       string `json:"data_base64"`
	RepresentativeID string `json:"representative_id,omitempty"`
	QuoteMessageID   string `json:"quote_message_id,omitempty"`
	Silent           bool   `json:"silent,omitempty"`
}

type Attachment struct {
	AttachmentID string            `json:"attachment_id"`
	UploadURL    string            `json:"upload_url"`
	ViewURL      string            `json:"view_url"`
	Headers      map[string]string `json:"headers"`
}

type APIError struct {
	HTTPStatus  int
	Status      int
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	if e == nil {
		return "mixin api error"
	}
	description := strings.TrimSpace(e.Description)
	if description == "" {
		description = http.StatusText(e.HTTPStatus)
	}
	if e.Code != 0 {
		return fmt.Sprintf("mixin api error: http=%d code=%d description=%s", e.HTTPStatus, e.Code, description)
	}
	return fmt.Sprintf("mixin api error: http=%d description=%s", e.HTTPStatus, description)
}

func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.HTTPStatus == http.StatusUnauthorized || apiErr.Status == http.StatusUnauthorized || apiErr.Code == http.StatusUnauthorized)
}

type ClientOptions struct {
	BaseURL      string
	HTTPClient   *http.Client
	Now          func() time.Time
	NewRequestID func() string
}

type Client struct {
	credentials  Credentials
	baseURL      *url.URL
	httpClient   *http.Client
	now          func() time.Time
	newRequestID func() string
}

func NewClient(credentials Credentials, opts ClientOptions) (*Client, error) {
	if err := credentials.validate(); err != nil {
		return nil, fmt.Errorf("invalid mixin credentials: %w", err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("mixin api base url is invalid")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	requestID := opts.NewRequestID
	if requestID == nil {
		requestID = newRequestID
	}
	return &Client{credentials: credentials, baseURL: parsed, httpClient: httpClient, now: now, newRequestID: requestID}, nil
}

func (c *Client) Me(ctx context.Context) (User, error) {
	var user User
	if err := c.do(ctx, http.MethodGet, "/me", nil, &user, 0); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) ReadUser(ctx context.Context, userID string) (User, error) {
	userID, err := normalizeUUID("user_id", userID)
	if err != nil {
		return User{}, err
	}
	var user User
	if err := c.do(ctx, http.MethodGet, "/users/"+userID, nil, &user, 0); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) ReadConversation(ctx context.Context, conversationID string) (Conversation, error) {
	conversationID, err := normalizeUUID("conversation_id", conversationID)
	if err != nil {
		return Conversation{}, err
	}
	var conversation Conversation
	if err := c.do(ctx, http.MethodGet, "/conversations/"+conversationID, nil, &conversation, 0); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func (c *Client) CreateConversation(ctx context.Context, request CreateConversationRequest) (Conversation, error) {
	var conversation Conversation
	if err := c.do(ctx, http.MethodPost, "/conversations", request, &conversation, 0); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func (c *Client) SendMessages(ctx context.Context, messages []MessageRequest) error {
	if len(messages) == 0 {
		return fmt.Errorf("at least one mixin message is required")
	}
	if len(messages) > 100 {
		return fmt.Errorf("mixin message batch exceeds 100 messages")
	}
	var lastErr error
	for attempt := 0; attempt < maxMessageSendAttempts; attempt++ {
		lastErr = c.do(ctx, http.MethodPost, "/messages", messages, nil, maxMessageRequestBytes)
		if lastErr == nil {
			return nil
		}
		delay, retry := messageRetryDelay(lastErr, attempt)
		if !retry || attempt+1 == maxMessageSendAttempts {
			return lastErr
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func messageRetryDelay(err error, attempt int) (time.Duration, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || IsUnauthorized(err) {
		return 0, false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		status := apiErr.HTTPStatus
		if status == 0 {
			status = apiErr.Status
		}
		if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
			return 0, false
		}
		if apiErr.RetryAfter > 0 {
			return apiErr.RetryAfter, true
		}
	}
	delay := defaultRetryBaseDelay << min(attempt, 5)
	return min(delay, 30*time.Second), true
}

func (c *Client) CreateAttachment(ctx context.Context) (Attachment, error) {
	var attachment Attachment
	if err := c.do(ctx, http.MethodPost, "/attachments", nil, &attachment, 0); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (c *Client) ReadAttachment(ctx context.Context, attachmentID string) (Attachment, error) {
	attachmentID, err := normalizeUUID("attachment_id", attachmentID)
	if err != nil {
		return Attachment{}, err
	}
	var attachment Attachment
	if err := c.do(ctx, http.MethodGet, "/attachments/"+attachmentID, nil, &attachment, 0); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any, maxRequestBytes int) error {
	if c == nil {
		return fmt.Errorf("mixin api client is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal mixin api request: %w", err)
		}
		if maxRequestBytes > 0 && len(body) > maxRequestBytes {
			return fmt.Errorf("%w: %d bytes exceeds %d", ErrRequestTooLarge, len(body), maxRequestBytes)
		}
	}
	token, err := signAuthenticationToken(c.credentials, method, path, body, c.now(), c.newRequestID())
	if err != nil {
		return err
	}
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mixin api request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read mixin api response: %w", err)
	}
	if len(raw) > maxAPIResponseBytes {
		return fmt.Errorf("mixin api response exceeds %d bytes", maxAPIResponseBytes)
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Status      int    `json:"status"`
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode mixin api response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("decode mixin api response: trailing data")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Error != nil {
		apiErr := &APIError{HTTPStatus: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), c.now())}
		if envelope.Error != nil {
			apiErr.Status = envelope.Error.Status
			apiErr.Code = envelope.Error.Code
			apiErr.Description = strings.TrimSpace(envelope.Error.Description)
		}
		return apiErr
	}
	if output != nil {
		if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
			return fmt.Errorf("mixin api response data is missing")
		}
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("decode mixin api data: %w", err)
		}
	}
	return nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil {
		if delay := deadline.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}
