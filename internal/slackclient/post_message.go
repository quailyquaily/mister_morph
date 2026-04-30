package slackclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://slack.com/api"

type Client struct {
	http     *http.Client
	baseURL  string
	botToken string
}

type MessageRef struct {
	ChannelID string
	MessageTS string
}

type Block map[string]any

func New(httpClient *http.Client, baseURL, botToken string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		http:     httpClient,
		baseURL:  baseURL,
		botToken: strings.TrimSpace(botToken),
	}
}

func (c *Client) PostMessage(ctx context.Context, channelID, text, threadTS string) error {
	_, err := c.postMessage(ctx, channelID, text, threadTS, false)
	return err
}

func (c *Client) PostMessageWithResult(ctx context.Context, channelID, text, threadTS string) (MessageRef, error) {
	return c.postMessage(ctx, channelID, text, threadTS, true)
}

func (c *Client) postMessage(ctx context.Context, channelID, text, threadTS string, requireMessageTS bool) (MessageRef, error) {
	if c == nil || c.http == nil {
		return MessageRef{}, fmt.Errorf("slack client is not initialized")
	}
	if strings.TrimSpace(c.botToken) == "" {
		return MessageRef{}, fmt.Errorf("slack token is required")
	}
	channelID = strings.TrimSpace(channelID)
	text = strings.TrimSpace(text)
	threadTS = strings.TrimSpace(threadTS)
	if channelID == "" {
		return MessageRef{}, fmt.Errorf("channel_id is required")
	}
	if text == "" {
		return MessageRef{}, fmt.Errorf("text is required")
	}

	type requestBody struct {
		Channel  string `json:"channel"`
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts,omitempty"`
	}
	type responseBody struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
		Channel string `json:"channel,omitempty"`
		TS      string `json:"ts,omitempty"`
	}

	payload := requestBody{
		Channel:  channelID,
		Text:     text,
		ThreadTS: threadTS,
	}
	var out responseBody
	body, status, err := c.postJSONWithRetry(ctx, "/chat.postMessage", payload)
	if err != nil {
		return MessageRef{}, err
	}
	if status < 200 || status >= 300 {
		return MessageRef{}, fmt.Errorf("slack chat.postMessage http %d", status)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return MessageRef{}, err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return MessageRef{}, fmt.Errorf("slack chat.postMessage failed: %s", code)
	}
	ref := MessageRef{
		ChannelID: strings.TrimSpace(out.Channel),
		MessageTS: strings.TrimSpace(out.TS),
	}
	if ref.ChannelID == "" {
		ref.ChannelID = channelID
	}
	if requireMessageTS && ref.MessageTS == "" {
		return MessageRef{}, fmt.Errorf("slack chat.postMessage returned empty ts")
	}
	return ref, nil
}

func (c *Client) UpdateMessage(ctx context.Context, channelID, messageTS, text string) error {
	return c.updateMessage(ctx, channelID, messageTS, text, nil)
}

func (c *Client) UpdateMessageWithBlocks(ctx context.Context, channelID, messageTS, text string, blocks []Block) error {
	return c.updateMessage(ctx, channelID, messageTS, text, blocks)
}

func (c *Client) updateMessage(ctx context.Context, channelID, messageTS, text string, blocks []Block) error {
	if c == nil || c.http == nil {
		return fmt.Errorf("slack client is not initialized")
	}
	if strings.TrimSpace(c.botToken) == "" {
		return fmt.Errorf("slack token is required")
	}
	channelID = strings.TrimSpace(channelID)
	messageTS = strings.TrimSpace(messageTS)
	text = strings.TrimSpace(text)
	if channelID == "" {
		return fmt.Errorf("channel_id is required")
	}
	if messageTS == "" {
		return fmt.Errorf("message_ts is required")
	}
	if text == "" {
		return fmt.Errorf("text is required")
	}

	type requestBody struct {
		Channel string  `json:"channel"`
		TS      string  `json:"ts"`
		Text    string  `json:"text"`
		Blocks  []Block `json:"blocks,omitempty"`
	}
	type responseBody struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}

	payload := requestBody{
		Channel: channelID,
		TS:      messageTS,
		Text:    text,
		Blocks:  blocks,
	}
	var out responseBody
	body, status, err := c.postJSONWithRetry(ctx, "/chat.update", payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("slack chat.update http %d", status)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return fmt.Errorf("slack chat.update failed: %s", code)
	}
	return nil
}

func (c *Client) postJSONWithRetry(ctx context.Context, path string, payload any) ([]byte, int, error) {
	token := strings.TrimSpace(c.botToken)
	const maxAttempts = 3
	var lastErr error
	var lastBody []byte
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		bodyRaw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal slack payload: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(bodyRaw))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		status := 0
		headers := http.Header{}
		if err != nil {
			lastErr = err
		} else {
			status = resp.StatusCode
			headers = resp.Header
			respRaw, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if status >= 200 && status < 300 {
				return respRaw, status, nil
			} else {
				lastBody = respRaw
				lastStatus = status
				lastErr = nil
			}
		}

		if attempt >= maxAttempts {
			break
		}
		if status == 0 {
			status = http.StatusBadGateway
		}
		wait, retryable := retryDelay(status, headers, attempt)
		if !retryable {
			break
		}
		if err := sleepWithContext(ctx, wait); err != nil {
			return nil, status, err
		}
	}
	if lastErr != nil {
		return lastBody, lastStatus, lastErr
	}
	return lastBody, lastStatus, nil
}

func retryDelay(status int, headers http.Header, attempt int) (time.Duration, bool) {
	switch {
	case status == http.StatusTooManyRequests:
		retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
		if retryAfter == "" {
			return 1 * time.Second, true
		}
		secs, err := strconv.Atoi(retryAfter)
		if err != nil || secs <= 0 {
			return 1 * time.Second, true
		}
		return time.Duration(secs) * time.Second, true
	case status >= 500 && status <= 599:
		switch attempt {
		case 1:
			return 300 * time.Millisecond, true
		case 2:
			return 1 * time.Second, true
		default:
			return 2 * time.Second, true
		}
	default:
		return 0, false
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
