package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type daemonTaskClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

func newDaemonTaskClient(baseURL, authToken string) *daemonTaskClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	authToken = strings.TrimSpace(authToken)
	return &daemonTaskClient{
		baseURL:   baseURL,
		authToken: authToken,
		client:    &http.Client{Timeout: 20 * time.Second},
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

func (c *daemonTaskClient) Proxy(ctx context.Context, method, endpointPath string, body []byte) (int, []byte, error) {
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
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, raw, nil
}

func parseHealthResponse(statusCode int, raw []byte) (runtimeEndpointHealth, error) {
	if statusCode < 200 || statusCode >= 300 {
		return runtimeEndpointHealth{}, fmt.Errorf("daemon health http %d: %s", statusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Mode          string `json:"mode"`
		AgentName     string `json:"agent_name"`
		SubmitEnabled bool   `json:"submit_enabled"`
		InstanceID    string `json:"instance_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return runtimeEndpointHealth{}, fmt.Errorf("invalid daemon health response: %w", err)
	}
	return runtimeEndpointHealth{
		Mode:       strings.ToLower(strings.TrimSpace(out.Mode)),
		AgentName:  strings.TrimSpace(out.AgentName),
		CanSubmit:  out.SubmitEnabled,
		InstanceID: strings.TrimSpace(out.InstanceID),
	}, nil
}
