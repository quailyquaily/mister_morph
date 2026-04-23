package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

type Config struct {
	Model          string
	Region         string
	AccessKey      string
	SecretKey      string
	SessionToken   string
	AWSProfile     string
	CABundle       string
	ReadTimeout    time.Duration
	ConnectTimeout time.Duration
}

type Client struct {
	model          string
	region         string
	accessKey      string
	secretKey      string
	sessionToken   string
	awsProfile     string
	caBundle       string
	readTimeout    time.Duration
	connectTimeout time.Duration
}

func New(cfg Config) *Client {
	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 5 * time.Minute
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 60 * time.Second
	}
	return &Client{
		model:          strings.TrimSpace(cfg.Model),
		region:         strings.TrimSpace(cfg.Region),
		accessKey:      strings.TrimSpace(cfg.AccessKey),
		secretKey:      strings.TrimSpace(cfg.SecretKey),
		sessionToken:   strings.TrimSpace(cfg.SessionToken),
		awsProfile:     strings.TrimSpace(cfg.AWSProfile),
		caBundle:       strings.TrimSpace(cfg.CABundle),
		readTimeout:    readTimeout,
		connectTimeout: connectTimeout,
	}
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	start := time.Now()

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		modelID = c.model
	}
	if modelID == "" {
		return llm.Result{}, fmt.Errorf("bedrock: model is required")
	}

	payload, err := c.buildRequest(modelID, req)
	if err != nil {
		return llm.Result{}, err
	}

	inputJSON, err := json.Marshal(payload)
	if err != nil {
		return llm.Result{}, fmt.Errorf("bedrock: marshal request: %w", err)
	}

	inputFile, err := os.CreateTemp("", "mistermorph-bedrock-*.json")
	if err != nil {
		return llm.Result{}, fmt.Errorf("bedrock: create temp request file: %w", err)
	}
	inputPath := inputFile.Name()
	defer os.Remove(inputPath)
	if _, err := inputFile.Write(inputJSON); err != nil {
		_ = inputFile.Close()
		return llm.Result{}, fmt.Errorf("bedrock: write temp request file: %w", err)
	}
	if err := inputFile.Close(); err != nil {
		return llm.Result{}, fmt.Errorf("bedrock: close temp request file: %w", err)
	}

	args := []string{"bedrock-runtime", "converse", "--cli-input-json", "file://" + inputPath, "--output", "json"}
	if c.region != "" {
		args = append(args, "--region", c.region)
	}
	if c.readTimeout > 0 {
		args = append(args, "--cli-read-timeout", strconv.Itoa(int(c.readTimeout/time.Second)))
	}
	if c.connectTimeout > 0 {
		args = append(args, "--cli-connect-timeout", strconv.Itoa(int(c.connectTimeout/time.Second)))
	}
	if c.caBundle != "" {
		args = append(args, "--ca-bundle", c.caBundle)
	}

	cmd := exec.CommandContext(ctx, "aws", args...)
	cmd.Env = c.commandEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return llm.Result{}, fmt.Errorf("bedrock: converse failed: %s", msg)
	}

	var resp converseResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return llm.Result{}, fmt.Errorf("bedrock: decode response: %w", err)
	}

	result := llm.Result{
		Text:      flattenText(resp.Output.Message.Content),
		ToolCalls: toToolCalls(resp.Output.Message.Content),
		Usage: llm.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Duration: time.Since(start),
	}
	return result, nil
}

func (c *Client) commandEnv() []string {
	env := os.Environ()
	if c.accessKey != "" || c.secretKey != "" || c.sessionToken != "" {
		env = setEnvValue(env, "AWS_ACCESS_KEY_ID", c.accessKey)
		env = setEnvValue(env, "AWS_SECRET_ACCESS_KEY", c.secretKey)
		env = setEnvValue(env, "AWS_SESSION_TOKEN", c.sessionToken)
	}
	if c.awsProfile != "" {
		env = setEnvValue(env, "AWS_PROFILE", c.awsProfile)
	}
	if c.region != "" {
		env = setEnvValue(env, "AWS_REGION", c.region)
		env = setEnvValue(env, "AWS_DEFAULT_REGION", c.region)
	}
	return env
}

func setEnvValue(env []string, key string, value string) []string {
	prefix := key + "="
	filtered := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		filtered = append(filtered, item)
	}
	if strings.TrimSpace(value) != "" {
		filtered = append(filtered, prefix+value)
	}
	return filtered
}

func (c *Client) buildRequest(modelID string, req llm.Request) (map[string]any, error) {
	system, messages, err := toConverseMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"modelId":  modelID,
		"messages": messages,
	}
	if len(system) > 0 {
		payload["system"] = system
	}
	if len(req.Tools) > 0 {
		toolConfig, err := toToolConfig(req.Tools)
		if err != nil {
			return nil, err
		}
		payload["toolConfig"] = toolConfig
	}

	inference := map[string]any{}
	if req.Parameters != nil {
		if v, ok := floatFromAny(req.Parameters["temperature"]); ok {
			inference["temperature"] = v
		}
		if v, ok := floatFromAny(req.Parameters["top_p"]); ok {
			inference["topP"] = v
		}
		if v, ok := intFromAny(req.Parameters["max_tokens"]); ok && v > 0 {
			inference["maxTokens"] = v
		}
		if v, ok := stringSliceFromAny(req.Parameters["stop"]); ok && len(v) > 0 {
			inference["stopSequences"] = v
		}
	}
	if len(inference) > 0 {
		payload["inferenceConfig"] = inference
	}

	return payload, nil
}

func toConverseMessages(messages []llm.Message) ([]map[string]any, []map[string]any, error) {
	system := make([]map[string]any, 0, 1)
	out := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "user"
		}
		if role == "system" {
			if strings.TrimSpace(msg.Content) != "" {
				system = append(system, map[string]any{"text": msg.Content})
			}
			for _, part := range msg.Parts {
				if strings.EqualFold(strings.TrimSpace(part.Type), llm.PartTypeText) && strings.TrimSpace(part.Text) != "" {
					system = append(system, map[string]any{"text": part.Text})
				}
			}
			continue
		}

		bedrockRole := role
		if bedrockRole == "tool" {
			bedrockRole = "user"
		}
		if bedrockRole != "user" && bedrockRole != "assistant" {
			bedrockRole = "user"
		}

		content, err := toContentBlocks(msg)
		if err != nil {
			return nil, nil, err
		}
		if len(content) == 0 {
			continue
		}

		// Bedrock requires all toolResult blocks for a given assistant turn to be
		// in a single user message. Merge consecutive same-role messages.
		if len(out) > 0 && out[len(out)-1]["role"] == bedrockRole {
			existing := out[len(out)-1]["content"].([]map[string]any)
			out[len(out)-1]["content"] = append(existing, content...)
		} else {
			out = append(out, map[string]any{
				"role":    bedrockRole,
				"content": content,
			})
		}
	}

	return system, out, nil
}

func toContentBlocks(msg llm.Message) ([]map[string]any, error) {
	content := make([]map[string]any, 0, 1+len(msg.Parts)+len(msg.ToolCalls))

	if strings.TrimSpace(msg.ToolCallID) != "" {
		block := map[string]any{
			"toolResult": map[string]any{
				"toolUseId": msg.ToolCallID,
				"content": []map[string]any{
					{"text": msg.Content},
				},
			},
		}
		content = append(content, block)
		return content, nil
	}

	if strings.TrimSpace(msg.Content) != "" {
		content = append(content, map[string]any{"text": msg.Content})
	}

	for _, part := range msg.Parts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case llm.PartTypeText:
			if strings.TrimSpace(part.Text) != "" {
				content = append(content, map[string]any{"text": part.Text})
			}
		case llm.PartTypeImageBase64:
			if strings.TrimSpace(part.DataBase64) == "" {
				continue
			}
			imageBytes, err := base64.StdEncoding.DecodeString(part.DataBase64)
			if err != nil {
				return nil, fmt.Errorf("bedrock: decode image base64: %w", err)
			}
			format := imageFormatFromMIME(part.MIMEType)
			if format == "" {
				return nil, fmt.Errorf("bedrock: unsupported image mime type %q", part.MIMEType)
			}
			content = append(content, map[string]any{
				"image": map[string]any{
					"format": format,
					"source": map[string]any{
						"bytes": base64.StdEncoding.EncodeToString(imageBytes),
					},
				},
			})
		}
	}

	for _, call := range msg.ToolCalls {
		input := call.Arguments
		if input == nil {
			input = map[string]any{}
		}
		content = append(content, map[string]any{
			"toolUse": map[string]any{
				"toolUseId": firstNonEmpty(call.ID, call.Name),
				"name":      call.Name,
				"input":     input,
			},
		})
	}

	return content, nil
}

func imageFormatFromMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return ""
	}
}

func toToolConfig(tools []llm.Tool) (map[string]any, error) {
	items := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		schema := map[string]any{"type": "object"}
		if strings.TrimSpace(tool.ParametersJSON) != "" {
			var raw any
			if err := json.Unmarshal([]byte(tool.ParametersJSON), &raw); err != nil {
				return nil, fmt.Errorf("bedrock: invalid tool schema for %s: %w", name, err)
			}
			schema = map[string]any{"json": raw}
		} else {
			schema = map[string]any{"json": map[string]any{"type": "object"}}
		}
		items = append(items, map[string]any{
			"toolSpec": map[string]any{
				"name":        name,
				"description": strings.TrimSpace(tool.Description),
				"inputSchema": schema,
			},
		})
	}
	return map[string]any{
		"tools": items,
		"toolChoice": map[string]any{
			"auto": map[string]any{},
		},
	}, nil
}

func flattenText(content []contentBlock) string {
	var parts []string
	for _, block := range content {
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func toToolCalls(content []contentBlock) []llm.ToolCall {
	out := make([]llm.ToolCall, 0)
	for _, block := range content {
		if block.ToolUse == nil {
			continue
		}
		rawArgs := ""
		if len(block.ToolUse.Input) > 0 {
			if data, err := json.Marshal(block.ToolUse.Input); err == nil {
				rawArgs = string(data)
			}
		}
		out = append(out, llm.ToolCall{
			ID:           strings.TrimSpace(block.ToolUse.ToolUseID),
			Type:         "function",
			Name:         strings.TrimSpace(block.ToolUse.Name),
			Arguments:    block.ToolUse.Input,
			RawArguments: rawArgs,
		})
	}
	return out
}

func floatFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}

func stringSliceFromAny(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, text)
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type converseResponse struct {
	Output struct {
		Message struct {
			Role    string         `json:"role"`
			Content []contentBlock `json:"content"`
		} `json:"message"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
		TotalTokens  int `json:"totalTokens"`
	} `json:"usage"`
}

type contentBlock struct {
	Text       string           `json:"text,omitempty"`
	ToolUse    *toolUseBlock    `json:"toolUse,omitempty"`
	ToolResult *toolResultBlock `json:"toolResult,omitempty"`
}

type toolUseBlock struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type toolResultBlock struct {
	ToolUseID string `json:"toolUseId"`
}
