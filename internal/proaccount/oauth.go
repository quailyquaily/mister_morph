package proaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultOAuthBaseURL  = "https://accounts.archkumo.com/api"
	DefaultOAuthClientID = "019e4fec-75a6-7396-8d0f-a2ea589e1eea"
	DefaultOAuthScope    = "user.public usage.read llm.full"
	DefaultRouterAPIBase = "https://router.mistermorph.com/api"
	DefaultModel         = "gpt-5.4"
	deviceGrantType      = "urn:ietf:params:oauth:grant-type:device_code"
	refreshGrantType     = "refresh_token"
	defaultDeviceTTL     = 10 * time.Minute
	defaultPollInterval  = 5 * time.Second
	SlowDownIncrement    = 5 * time.Second
	refreshSkew          = 5 * time.Minute
)

var (
	ErrAuthorizationPending = errors.New("pro OAuth device authorization pending")
	ErrNotLoggedIn          = errors.New("pro account is not logged in")

	refreshMu sync.Mutex
)

type OAuthConfig struct {
	BaseURL    string
	ClientID   string
	Scope      string
	HTTPClient *http.Client
	Now        func() time.Time
}

type RouterConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

type DeviceCode struct {
	DeviceCode              string
	VerificationURL         string
	VerificationURLComplete string
	UserCode                string
	Interval                time.Duration
	ExpiresAt               time.Time
}

type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type OAuthAPIError struct {
	Status      int
	Code        string
	Description string
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type subscriptionAPIKeyResponse struct {
	Data struct {
		APIKey struct {
			Key string `json:"key"`
		} `json:"api_key"`
	} `json:"data"`
}

func (e *OAuthAPIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Description) != "" {
		return strings.TrimSpace(e.Description)
	}
	if strings.TrimSpace(e.Code) != "" {
		return strings.TrimSpace(e.Code)
	}
	if e.Status > 0 {
		return http.StatusText(e.Status)
	}
	return "Pro OAuth request failed"
}

func DefaultOAuthConfigValue() OAuthConfig {
	return OAuthConfig{
		BaseURL:  envOrDefault("MISTERMORPH_PRO_OAUTH_BASE_URL", DefaultOAuthBaseURL),
		ClientID: envOrDefault("MISTERMORPH_PRO_OAUTH_CLIENT_ID", DefaultOAuthClientID),
		Scope:    envOrDefault("MISTERMORPH_PRO_OAUTH_SCOPE", DefaultOAuthScope),
	}
}

func DefaultRouterConfigValue() RouterConfig {
	return RouterConfig{
		BaseURL: envOrDefault("MISTERMORPH_PRO_ROUTER_BASE_URL", DefaultRouterAPIBase),
	}
}

func RequestDeviceCode(ctx context.Context, cfg OAuthConfig) (DeviceCode, error) {
	cfg = normalizeOAuthConfig(cfg)
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("scope", strings.TrimSpace(cfg.Scope))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthEndpoint(cfg, "/oauth/device/code"), strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out deviceAuthorizationResponse
	if err := doJSON(cfg.HTTPClient, req, &out); err != nil {
		var apiErr *OAuthAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && scopeHas(cfg.Scope, "llm.full") {
			return DeviceCode{}, fmt.Errorf("request Pro OAuth device code: forbidden (OAuth app %q must be trusted to request llm.full)", cfg.ClientID)
		}
		return DeviceCode{}, fmt.Errorf("request Pro OAuth device code: %w", err)
	}
	deviceCode := strings.TrimSpace(out.DeviceCode)
	userCode := strings.TrimSpace(out.UserCode)
	verificationURL := strings.TrimSpace(out.VerificationURI)
	verificationURLComplete := strings.TrimSpace(out.VerificationURIComplete)
	if deviceCode == "" {
		return DeviceCode{}, fmt.Errorf("request Pro OAuth device code: device code missing")
	}
	if userCode == "" {
		return DeviceCode{}, fmt.Errorf("request Pro OAuth device code: user code missing")
	}
	if verificationURL == "" && verificationURLComplete == "" {
		return DeviceCode{}, fmt.Errorf("request Pro OAuth device code: verification URL missing")
	}
	now := cfg.now()
	return DeviceCode{
		DeviceCode:              deviceCode,
		VerificationURL:         verificationURL,
		VerificationURLComplete: verificationURLComplete,
		UserCode:                userCode,
		Interval:                deviceInterval(out.Interval),
		ExpiresAt:               deviceExpiresAt(now, out.ExpiresIn),
	}, nil
}

func CompleteDeviceCodeLogin(ctx context.Context, oauthCfg OAuthConfig, routerCfg RouterConfig, device DeviceCode) (StoredSession, error) {
	token, err := ExchangeDeviceCode(ctx, oauthCfg, device.DeviceCode)
	if err != nil {
		return StoredSession{}, err
	}
	userInfo, err := FetchUserInfo(ctx, oauthCfg, token.AccessToken)
	if err != nil {
		return StoredSession{}, err
	}
	subscriptionAPIKey, err := RotateSubscriptionAPIKey(ctx, routerCfg, token.AccessToken)
	if err != nil {
		return StoredSession{}, err
	}
	return NewStoredSession(token, userInfo, subscriptionAPIKey, normalizeOAuthConfig(oauthCfg).now()), nil
}

func ExchangeDeviceCode(ctx context.Context, cfg OAuthConfig, deviceCode string) (TokenResponse, error) {
	cfg = normalizeOAuthConfig(cfg)
	form := url.Values{}
	form.Set("grant_type", deviceGrantType)
	form.Set("device_code", strings.TrimSpace(deviceCode))
	form.Set("client_id", cfg.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthEndpoint(cfg, "/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out TokenResponse
	if err := doJSON(cfg.HTTPClient, req, &out); err != nil {
		return TokenResponse{}, err
	}
	if strings.TrimSpace(out.Error) != "" {
		return TokenResponse{}, &OAuthAPIError{
			Status:      http.StatusOK,
			Code:        strings.TrimSpace(out.Error),
			Description: strings.TrimSpace(out.ErrorDescription),
		}
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return TokenResponse{}, fmt.Errorf("exchange Pro OAuth device code: access token missing")
	}
	return out, nil
}

func RefreshToken(ctx context.Context, cfg OAuthConfig, refreshToken string) (TokenResponse, error) {
	cfg = normalizeOAuthConfig(cfg)
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return TokenResponse{}, ErrNotLoggedIn
	}
	form := url.Values{}
	form.Set("grant_type", refreshGrantType)
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", cfg.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthEndpoint(cfg, "/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out TokenResponse
	if err := doJSON(cfg.HTTPClient, req, &out); err != nil {
		return TokenResponse{}, err
	}
	if strings.TrimSpace(out.Error) != "" {
		return TokenResponse{}, &OAuthAPIError{
			Status:      http.StatusOK,
			Code:        strings.TrimSpace(out.Error),
			Description: strings.TrimSpace(out.ErrorDescription),
		}
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return TokenResponse{}, fmt.Errorf("refresh Pro OAuth token: access token missing")
	}
	return out, nil
}

func FetchUserInfo(ctx context.Context, cfg OAuthConfig, accessToken string) (map[string]any, error) {
	cfg = normalizeOAuthConfig(cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthEndpoint(cfg, "/oauth/userinfo"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))

	var out map[string]any
	if err := doJSON(cfg.HTTPClient, req, &out); err != nil {
		return nil, fmt.Errorf("fetch Pro account userinfo: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	unionID := UnionID(out)
	if unionID == "" {
		return nil, fmt.Errorf("fetch Pro account userinfo: union_id missing")
	}
	out["union_id"] = unionID
	return out, nil
}

func RotateSubscriptionAPIKey(ctx context.Context, cfg RouterConfig, accessToken string) (string, error) {
	cfg = normalizeRouterConfig(cfg)
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("rotate Pro subscription API key: access token missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, routerEndpoint(cfg, "/users/me/api-keys/subscription"), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	var out subscriptionAPIKeyResponse
	if err := doJSON(cfg.HTTPClient, req, &out); err != nil {
		return "", fmt.Errorf("rotate Pro subscription API key: %w", err)
	}
	key := strings.TrimSpace(out.Data.APIKey.Key)
	if key == "" {
		return "", fmt.Errorf("rotate Pro subscription API key: key missing")
	}
	return key, nil
}

func ResolveSession(ctx context.Context, stateDir string, oauthCfg OAuthConfig, routerCfg RouterConfig) (StoredSession, bool, error) {
	session, ok, err := ReadSession(stateDir)
	if err != nil || !ok {
		return StoredSession{}, ok, err
	}
	oauthCfg = normalizeOAuthConfig(oauthCfg)
	if session.IsAccessTokenUsable(oauthCfg.now()) {
		return session, true, nil
	}
	if strings.TrimSpace(session.RefreshToken) == "" {
		return session, true, nil
	}

	refreshMu.Lock()
	defer refreshMu.Unlock()

	session, ok, err = ReadSession(stateDir)
	if err != nil || !ok {
		return StoredSession{}, ok, err
	}
	if session.IsAccessTokenUsable(oauthCfg.now()) {
		return session, true, nil
	}
	if strings.TrimSpace(session.RefreshToken) == "" {
		return session, true, nil
	}

	token, err := RefreshToken(ctx, oauthCfg, session.RefreshToken)
	if err != nil {
		if RefreshTokenInvalid(err) {
			if _, deleteErr := DeleteSession(stateDir); deleteErr != nil {
				return StoredSession{}, false, deleteErr
			}
			return StoredSession{}, false, nil
		}
		return StoredSession{}, false, err
	}
	updated := updateSessionFromRefresh(session, token, oauthCfg.now())
	subscriptionAPIKey, rotateErr := RotateSubscriptionAPIKey(ctx, routerCfg, updated.AccessToken)
	if rotateErr == nil {
		updated.SubscriptionAPIKey = subscriptionAPIKey
	}
	if err := WriteSession(stateDir, updated); err != nil {
		return StoredSession{}, false, err
	}
	if rotateErr != nil {
		return StoredSession{}, false, rotateErr
	}
	return updated, true, nil
}

func IsAuthorizationPending(err error) bool {
	var apiErr *OAuthAPIError
	return errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) == "authorization_pending"
}

func IsSlowDown(err error) bool {
	var apiErr *OAuthAPIError
	return errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) == "slow_down"
}

func IsDeviceCodeExpired(err error) bool {
	var apiErr *OAuthAPIError
	return errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) == "expired_token"
}

func IsAccessDenied(err error) bool {
	var apiErr *OAuthAPIError
	return errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) == "access_denied"
}

func RefreshTokenInvalid(err error) bool {
	var apiErr *OAuthAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch strings.TrimSpace(apiErr.Code) {
	case "invalid_grant", "unauthorized", "expired_token", "access_denied":
		return true
	default:
		return apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden
	}
}

func UnionID(userInfo map[string]any) string {
	if userInfo == nil {
		return ""
	}
	value, ok := userInfo["union_id"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func updateSessionFromRefresh(session StoredSession, token TokenResponse, now time.Time) StoredSession {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	session.AccessToken = strings.TrimSpace(token.AccessToken)
	session.TokenType = firstNonEmpty(token.TokenType, session.TokenType, "Bearer")
	session.RefreshToken = firstNonEmpty(token.RefreshToken, session.RefreshToken)
	session.Scope = firstNonEmpty(token.Scope, session.Scope)
	if token.ExpiresIn > 0 {
		session.ExpiresAt = now.UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	session.UpdatedAt = now.UTC()
	if session.UserInfo == nil {
		session.UserInfo = map[string]any{}
	}
	return normalizeSession(session)
}

func normalizeOAuthConfig(cfg OAuthConfig) OAuthConfig {
	cfg.BaseURL = firstNonEmpty(cfg.BaseURL, DefaultOAuthBaseURL)
	cfg.ClientID = firstNonEmpty(cfg.ClientID, DefaultOAuthClientID)
	cfg.Scope = firstNonEmpty(cfg.Scope, DefaultOAuthScope)
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return cfg
}

func normalizeRouterConfig(cfg RouterConfig) RouterConfig {
	cfg.BaseURL = firstNonEmpty(cfg.BaseURL, DefaultRouterAPIBase)
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return cfg
}

func (cfg OAuthConfig) now() time.Time {
	if cfg.Now == nil {
		return time.Now().UTC()
	}
	return cfg.Now().UTC()
}

func doJSON(client *http.Client, req *http.Request, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpError(resp.StatusCode, raw)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func httpError(status int, raw []byte) error {
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	code := ""
	if v, ok := body["error"].(string); ok {
		code = strings.TrimSpace(v)
	}
	msg := ""
	if v, ok := body["error_description"].(string); ok {
		msg = strings.TrimSpace(v)
	}
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &OAuthAPIError{
		Status:      status,
		Code:        code,
		Description: msg,
	}
}

func oauthEndpoint(cfg OAuthConfig, endpointPath string) string {
	return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + endpointPath
}

func routerEndpoint(cfg RouterConfig, endpointPath string) string {
	return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + endpointPath
}

func deviceExpiresAt(now time.Time, expiresIn int64) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if expiresIn <= 0 {
		return now.UTC().Add(defaultDeviceTTL)
	}
	return now.UTC().Add(time.Duration(expiresIn) * time.Second)
}

func deviceInterval(raw int64) time.Duration {
	if raw <= 0 {
		return defaultPollInterval
	}
	return time.Duration(raw) * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func scopeHas(scope string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range strings.Fields(scope) {
		if item == target {
			return true
		}
	}
	return false
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
