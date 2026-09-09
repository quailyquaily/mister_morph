package consolecmd

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

	"github.com/gorilla/websocket"
)

type daemonTaskClient struct {
	baseURL        string
	authToken      string
	client         *http.Client
	downloadClient *http.Client
}

func newDaemonTaskClient(baseURL, authToken string) *daemonTaskClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	authToken = strings.TrimSpace(authToken)
	downloadTransport := http.DefaultTransport.(*http.Transport).Clone()
	downloadTransport.ResponseHeaderTimeout = 20 * time.Second
	return &daemonTaskClient{
		baseURL:        baseURL,
		authToken:      authToken,
		client:         &http.Client{Timeout: 20 * time.Second},
		downloadClient: &http.Client{Transport: downloadTransport},
	}
}

func (c *daemonTaskClient) readyBaseURL() error {
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("daemon server url is not configured")
	}
	return nil
}

func (c *daemonTaskClient) ready() error {
	if err := c.readyBaseURL(); err != nil {
		return err
	}
	if strings.TrimSpace(c.authToken) == "" {
		return fmt.Errorf("daemon server auth token is not configured")
	}
	return nil
}

func (c *daemonTaskClient) Health(ctx context.Context) (runtimeEndpointHealth, error) {
	if err := c.readyBaseURL(); err != nil {
		return runtimeEndpointHealth{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return runtimeEndpointHealth{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return runtimeEndpointHealth{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return parseHealthResponse(resp.StatusCode, raw)
}

func (c *daemonTaskClient) Proxy(ctx context.Context, method, endpointPath string, body []byte, contentType string) (int, []byte, error) {
	if err := c.ready(); err != nil {
		return 0, nil, err
	}
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = "/"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.TrimSpace(method), c.baseURL+endpointPath, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken)
	if len(body) > 0 {
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}

	client := c.client
	proxyPath, _, _ := strings.Cut(endpointPath, "?")
	if req.Method == http.MethodPost && strings.HasPrefix(proxyPath, "/topics/") && strings.HasSuffix(proxyPath, "/regenerate-title") {
		// Naming uses the runtime's configured LLM timeout and retries. The
		// ordinary proxy timeout must not cut that work short; caller cancellation
		// still propagates through the request context.
		namingClient := *client
		namingClient.Timeout = 0
		client = &namingClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, raw, nil
}

func (c *daemonTaskClient) Download(ctx context.Context, endpointPath string) (runtimeEndpointDownload, error) {
	if err := c.ready(); err != nil {
		return runtimeEndpointDownload{}, err
	}
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = "/"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpointPath, nil)
	if err != nil {
		return runtimeEndpointDownload{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken)

	client := c.downloadClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return runtimeEndpointDownload{}, err
	}
	return runtimeEndpointDownload{
		Status: resp.StatusCode,
		Header: resp.Header.Clone(),
		Body:   resp.Body,
	}, nil
}

func (c *daemonTaskClient) OpenTaskStream(ctx context.Context, taskID string) (*websocket.Conn, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	target, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid daemon server url: %w", err)
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	default:
		return nil, fmt.Errorf("daemon server url must use http or https")
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/stream/ws"
	query := target.Query()
	query.Set("task_id", taskID)
	target.RawQuery = query.Encode()

	header := http.Header{"Authorization": []string{"Bearer " + c.authToken}}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, target.String(), header)
	if err == nil {
		return conn, nil
	}
	if resp == nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	message := strings.TrimSpace(string(raw))
	if message == "" {
		message = err.Error()
	}
	return nil, fmt.Errorf("daemon stream http %d: %s", resp.StatusCode, message)
}

func parseHealthResponse(statusCode int, raw []byte) (runtimeEndpointHealth, error) {
	if statusCode < 200 || statusCode >= 300 {
		return runtimeEndpointHealth{}, fmt.Errorf("daemon health http %d: %s", statusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Mode          string `json:"mode"`
		AgentName     string `json:"agent_name"`
		AvatarURL     string `json:"agent_avatar_url"`
		SubmitEnabled bool   `json:"submit_enabled"`
		InstanceID    string `json:"instance_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return runtimeEndpointHealth{}, fmt.Errorf("invalid daemon health response: %w", err)
	}
	return runtimeEndpointHealth{
		Mode:       strings.ToLower(strings.TrimSpace(out.Mode)),
		AgentName:  strings.TrimSpace(out.AgentName),
		AvatarURL:  strings.TrimSpace(out.AvatarURL),
		CanSubmit:  out.SubmitEnabled,
		InstanceID: strings.TrimSpace(out.InstanceID),
	}, nil
}
