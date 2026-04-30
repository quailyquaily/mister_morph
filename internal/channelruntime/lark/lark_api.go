package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type larkAPI struct {
	http        *http.Client
	baseURL     string
	tokenClient *TenantTokenClient
}

type larkSendMessageRequest struct {
	ReceiveID string `json:"receive_id"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
	UUID      string `json:"uuid,omitempty"`
}

type larkReplyMessageRequest struct {
	Content       string `json:"content"`
	MsgType       string `json:"msg_type"`
	ReplyInThread bool   `json:"reply_in_thread,omitempty"`
	UUID          string `json:"uuid,omitempty"`
}

type larkMessageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		MessageID string `json:"message_id"`
		ChatID    string `json:"chat_id"`
	} `json:"data"`
}

func newLarkAPI(httpClient *http.Client, baseURL string, tokenClient *TenantTokenClient) *larkAPI {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		baseURL = defaultLarkBaseURL
	}
	return &larkAPI{http: httpClient, baseURL: baseURL, tokenClient: tokenClient}
}

func (api *larkAPI) sendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	if api == nil {
		return fmt.Errorf("lark api is not initialized")
	}
	receiveIDType = strings.TrimSpace(receiveIDType)
	receiveID = strings.TrimSpace(receiveID)
	text = strings.TrimSpace(text)
	if receiveIDType == "" {
		return fmt.Errorf("lark receive_id_type is required")
	}
	if receiveID == "" {
		return fmt.Errorf("lark receive_id is required")
	}
	if text == "" {
		return fmt.Errorf("lark text is required")
	}
	contentRaw, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	endpoint := api.baseURL + "/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	return api.postJSON(ctx, endpoint, larkSendMessageRequest{
		ReceiveID: receiveID,
		MsgType:   "text",
		Content:   string(contentRaw),
		UUID:      uuid.NewString(),
	})
}

func (api *larkAPI) replyText(ctx context.Context, messageID, text string) error {
	if api == nil {
		return fmt.Errorf("lark api is not initialized")
	}
	messageID = strings.TrimSpace(messageID)
	text = strings.TrimSpace(text)
	if messageID == "" {
		return fmt.Errorf("lark message id is required")
	}
	if text == "" {
		return fmt.Errorf("lark text is required")
	}
	contentRaw, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	endpoint := api.baseURL + "/im/v1/messages/" + url.PathEscape(messageID) + "/reply"
	return api.postJSON(ctx, endpoint, larkReplyMessageRequest{
		Content: string(contentRaw),
		MsgType: "text",
		UUID:    uuid.NewString(),
	})
}

func (api *larkAPI) postJSON(ctx context.Context, endpoint string, payload any) error {
	if api == nil {
		return fmt.Errorf("lark api is not initialized")
	}
	if api.tokenClient == nil {
		return fmt.Errorf("lark token client is not initialized")
	}
	bodyRaw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	token, err := api.tokenClient.Token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyRaw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	resp, err := api.http.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lark http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out larkMessageResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode lark response: %w", err)
	}
	if out.Code != 0 {
		return fmt.Errorf("lark api code %d: %s", out.Code, strings.TrimSpace(out.Msg))
	}
	return nil
}

func (api *larkAPI) messageResource(ctx context.Context, messageID, fileKey, fileType string, maxBytes int64) ([]byte, string, error) {
	if api == nil {
		return nil, "", fmt.Errorf("lark api is not initialized")
	}
	if api.tokenClient == nil {
		return nil, "", fmt.Errorf("lark token client is not initialized")
	}
	messageID = strings.TrimSpace(messageID)
	fileKey = strings.TrimSpace(fileKey)
	fileType = strings.TrimSpace(fileType)
	if messageID == "" {
		return nil, "", fmt.Errorf("lark message id is required")
	}
	if fileKey == "" {
		return nil, "", fmt.Errorf("lark file key is required")
	}
	if fileType == "" {
		return nil, "", fmt.Errorf("lark file type is required")
	}
	if maxBytes <= 0 {
		return nil, "", fmt.Errorf("lark max bytes must be positive")
	}
	token, err := api.tokenClient.Token(ctx)
	if err != nil {
		return nil, "", err
	}
	endpoint := api.baseURL + "/im/v1/messages/" + url.PathEscape(messageID) + "/resources/" + url.PathEscape(fileKey) + "?type=" + url.QueryEscape(fileType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := api.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if readErr != nil {
		return nil, "", readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("lark resource download http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", fmt.Errorf("lark resource too large: > %d bytes", maxBytes)
	}
	return raw, resp.Header.Get("Content-Type"), nil
}
