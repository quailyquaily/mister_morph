package larkapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL        = "https://open.feishu.cn/open-apis"
	DefaultRefreshBefore  = 30 * time.Minute
	TenantAccessTokenPath = "/auth/v3/tenant_access_token/internal"
)

type TenantTokenClient struct {
	http          *http.Client
	baseURL       string
	appID         string
	appSecret     string
	refreshBefore time.Duration
	now           func() time.Time

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

type tenantAccessTokenRequest struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type tenantAccessTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

func NewTenantTokenClient(httpClient *http.Client, baseURL, appID, appSecret string) *TenantTokenClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &TenantTokenClient{
		http:          httpClient,
		baseURL:       baseURL,
		appID:         strings.TrimSpace(appID),
		appSecret:     strings.TrimSpace(appSecret),
		refreshBefore: DefaultRefreshBefore,
		now:           time.Now,
	}
}

func (c *TenantTokenClient) Token(ctx context.Context) (string, error) {
	if c == nil {
		return "", fmt.Errorf("lark tenant token client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.appID == "" {
		return "", fmt.Errorf("lark app id is required")
	}
	if c.appSecret == "" {
		return "", fmt.Errorf("lark app secret is required")
	}

	now := c.nowTime()
	if token := c.cachedToken(now); token != "" {
		return token, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now = c.nowTime()
	if token := c.cachedToken(now); token != "" {
		return token, nil
	}

	token, expiresIn, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	if expiresIn <= 0 {
		return "", fmt.Errorf("lark tenant_access_token returned invalid expire=%d", expiresIn)
	}

	c.cached = token
	c.expiresAt = c.nowTime().Add(time.Duration(expiresIn) * time.Second)
	return token, nil
}

func (c *TenantTokenClient) cachedToken(now time.Time) string {
	if c.cached == "" || !c.expiresAt.After(now.Add(c.refreshBefore)) {
		return ""
	}
	return c.cached
}

func (c *TenantTokenClient) nowTime() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

func (c *TenantTokenClient) fetchToken(ctx context.Context) (string, int, error) {
	bodyRaw, err := json.Marshal(tenantAccessTokenRequest{
		AppID:     c.appID,
		AppSecret: c.appSecret,
	})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+TenantAccessTokenPath, bytes.NewReader(bodyRaw))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("lark tenant_access_token http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out tenantAccessTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", 0, fmt.Errorf("decode lark tenant_access_token response: %w", err)
	}
	if out.Code != 0 {
		return "", 0, fmt.Errorf("lark tenant_access_token api code %d: %s", out.Code, strings.TrimSpace(out.Msg))
	}

	token := strings.TrimSpace(out.TenantAccessToken)
	if token == "" {
		return "", 0, fmt.Errorf("lark tenant_access_token response missing token")
	}
	return token, out.Expire, nil
}
