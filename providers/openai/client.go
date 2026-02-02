package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/llm"
)

const defaultMaxResponseBytes int64 = 16 * 1024 * 1024 // 16 MB

type Client struct {
	BaseURL          string
	APIKey           string
	HTTP             *http.Client
	MaxResponseBytes int64
}

func New(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &Client{
		BaseURL:          strings.TrimRight(baseURL, "/"),
		APIKey:           apiKey,
		HTTP:             &http.Client{Timeout: 90 * time.Second},
		MaxResponseBytes: defaultMaxResponseBytes,
	}
}

type chatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []llm.Message `json:"messages"`
	Temperature    float64       `json:"temperature,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	start := time.Now()

	do := func(forceJSON bool) (llm.Result, *chatCompletionResponse, int, []byte, error) {
		body := chatCompletionRequest{
			Model:       req.Model,
			Messages:    req.Messages,
			Temperature: 0,
		}
		if forceJSON {
			body.ResponseFormat = map[string]string{"type": "json_object"}
		}

		b, err := json.Marshal(body)
		if err != nil {
			return llm.Result{}, nil, 0, nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(b))
		if err != nil {
			return llm.Result{}, nil, 0, nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		}

		resp, err := c.HTTP.Do(httpReq)
		if err != nil {
			return llm.Result{}, nil, 0, nil, err
		}
		defer resp.Body.Close()

		maxResp := c.MaxResponseBytes
		if maxResp <= 0 {
			maxResp = defaultMaxResponseBytes
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResp))
		if err != nil {
			return llm.Result{}, nil, 0, nil, err
		}

		var out chatCompletionResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return llm.Result{}, nil, resp.StatusCode, raw, err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return llm.Result{}, &out, resp.StatusCode, raw, nil
		}

		if len(out.Choices) == 0 {
			return llm.Result{}, &out, resp.StatusCode, raw, fmt.Errorf("openai: empty choices")
		}

		text := out.Choices[0].Message.Content
		return llm.Result{
			Text: text,
			Usage: llm.Usage{
				InputTokens:  out.Usage.PromptTokens,
				OutputTokens: out.Usage.CompletionTokens,
				TotalTokens:  out.Usage.TotalTokens,
			},
			Duration: time.Since(start),
		}, &out, resp.StatusCode, raw, nil
	}

	res, out, status, raw, err := do(req.ForceJSON)
	if err != nil {
		return llm.Result{}, err
	}
	if status < 200 || status >= 300 {
		if req.ForceJSON && out != nil && out.Error != nil && strings.Contains(strings.ToLower(out.Error.Message), "response_format") {
			res, out, status, raw, err = do(false)
			if err != nil {
				return llm.Result{}, err
			}
			if status >= 200 && status < 300 {
				return res, nil
			}
		}
		if out != nil && out.Error != nil && out.Error.Message != "" {
			return llm.Result{}, fmt.Errorf("openai http %d: %s", status, out.Error.Message)
		}
		return llm.Result{}, fmt.Errorf("openai http %d: %s", status, string(raw))
	}
	return res, nil
}
