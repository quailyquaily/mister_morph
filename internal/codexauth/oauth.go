package codexauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ProviderName        = "openai_codex"
	DefaultClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	DefaultIssuer       = "https://auth.openai.com"
	DefaultAPIBase      = "https://chatgpt.com/backend-api/codex"
	DefaultModel        = "gpt-5.5"
	defaultDeviceTTL    = 15 * time.Minute
	defaultPollInterval = 5 * time.Second
)

var (
	ErrAuthorizationPending = errors.New("codex device authorization pending")
	ErrNotLoggedIn          = errors.New("codex oauth is not logged in")

	refreshMu sync.Mutex
)

type OAuthConfig struct {
	Issuer     string
	ClientID   string
	HTTPClient *http.Client
	Now        func() time.Time
}

type DeviceCode struct {
	VerificationURL string
	UserCode        string
	DeviceAuthID    string
	Interval        time.Duration
	ExpiresAt       time.Time
}

type Token struct {
	IDToken      string    `json:"id_token,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id,omitempty"`
	PlanType     string    `json:"plan_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type userCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	UserCodeAlt  string `json:"usercode"`
	Interval     any    `json:"interval"`
	ExpiresIn    any    `json:"expires_in"`
}

type tokenPollRequest struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
}

type tokenPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    any    `json:"expires_in"`
}

func DefaultOAuthConfigValue() OAuthConfig {
	return OAuthConfig{
		Issuer:   DefaultIssuer,
		ClientID: DefaultClientID,
	}
}

func RequestDeviceCode(ctx context.Context, cfg OAuthConfig) (DeviceCode, error) {
	cfg = normalizeOAuthConfig(cfg)
	endpoint := strings.TrimRight(cfg.Issuer, "/") + "/api/accounts/deviceauth/usercode"
	reqBody := map[string]string{"client_id": cfg.ClientID}

	var resp userCodeResponse
	if err := postJSON(ctx, cfg.HTTPClient, endpoint, reqBody, &resp); err != nil {
		return DeviceCode{}, err
	}

	userCode := firstNonEmpty(resp.UserCode, resp.UserCodeAlt)
	deviceAuthID := strings.TrimSpace(resp.DeviceAuthID)
	if deviceAuthID == "" || userCode == "" {
		return DeviceCode{}, fmt.Errorf("codex device auth response missing device id or user code")
	}

	interval := durationFromAny(resp.Interval, defaultPollInterval)
	expiresIn := durationFromAny(resp.ExpiresIn, defaultDeviceTTL)
	now := cfg.now()
	return DeviceCode{
		VerificationURL: strings.TrimRight(cfg.Issuer, "/") + "/codex/device",
		UserCode:        userCode,
		DeviceAuthID:    deviceAuthID,
		Interval:        interval,
		ExpiresAt:       now.Add(expiresIn),
	}, nil
}

func CompleteDeviceCodeLogin(ctx context.Context, cfg OAuthConfig, code DeviceCode) (Token, error) {
	cfg = normalizeOAuthConfig(cfg)
	poll, err := pollDeviceCode(ctx, cfg, code)
	if err != nil {
		return Token{}, err
	}
	redirectURI := strings.TrimRight(cfg.Issuer, "/") + "/deviceauth/callback"
	return exchangeAuthorizationCode(ctx, cfg, redirectURI, poll.AuthorizationCode, poll.CodeVerifier)
}

func RefreshToken(ctx context.Context, cfg OAuthConfig, refreshToken string) (Token, error) {
	cfg = normalizeOAuthConfig(cfg)
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, ErrNotLoggedIn
	}

	endpoint := strings.TrimRight(cfg.Issuer, "/") + "/oauth/token"
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	var resp tokenResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpResp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("codex refresh token request failed: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return Token{}, statusError(endpoint, httpResp.StatusCode, body)
	}
	if err := json.NewDecoder(io.LimitReader(httpResp.Body, 1<<20)).Decode(&resp); err != nil {
		return Token{}, fmt.Errorf("decode codex refresh token response: %w", err)
	}
	token := tokenFromResponse(resp, cfg.now())
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	if token.AccessToken == "" {
		return Token{}, fmt.Errorf("codex refresh response missing access token")
	}
	return token, nil
}

func ResolveToken(ctx context.Context, stateDir string, cfg OAuthConfig) (Token, error) {
	cfg = normalizeOAuthConfig(cfg)
	token, ok, err := ReadToken(stateDir)
	if err != nil {
		return Token{}, err
	}
	if !ok {
		return Token{}, ErrNotLoggedIn
	}
	if token.IsAccessTokenUsable(cfg.now()) {
		return token, nil
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return Token{}, ErrNotLoggedIn
	}

	refreshMu.Lock()
	defer refreshMu.Unlock()

	token, ok, err = ReadToken(stateDir)
	if err != nil {
		return Token{}, err
	}
	if !ok {
		return Token{}, ErrNotLoggedIn
	}
	if token.IsAccessTokenUsable(cfg.now()) {
		return token, nil
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return Token{}, ErrNotLoggedIn
	}

	refreshed, err := RefreshToken(ctx, cfg, token.RefreshToken)
	if err != nil {
		return Token{}, err
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = token.IDToken
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = token.AccountID
	}
	if refreshed.PlanType == "" {
		refreshed.PlanType = token.PlanType
	}
	if refreshed.CreatedAt.IsZero() {
		refreshed.CreatedAt = token.CreatedAt
	}
	if err := WriteToken(stateDir, refreshed); err != nil {
		return Token{}, err
	}
	return refreshed, nil
}

func (t Token) IsAccessTokenUsable(now time.Time) bool {
	if strings.TrimSpace(t.AccessToken) == "" {
		return false
	}
	if t.ExpiresAt.IsZero() {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return t.ExpiresAt.After(now.UTC().Add(60 * time.Second))
}

func IsAuthorizationPending(err error) bool {
	return errors.Is(err, ErrAuthorizationPending)
}

func pollDeviceCode(ctx context.Context, cfg OAuthConfig, code DeviceCode) (tokenPollResponse, error) {
	if strings.TrimSpace(code.DeviceAuthID) == "" || strings.TrimSpace(code.UserCode) == "" {
		return tokenPollResponse{}, fmt.Errorf("codex device auth session is invalid")
	}
	if !code.ExpiresAt.IsZero() && !code.ExpiresAt.After(cfg.now()) {
		return tokenPollResponse{}, fmt.Errorf("codex device code expired")
	}

	endpoint := strings.TrimRight(cfg.Issuer, "/") + "/api/accounts/deviceauth/token"
	reqBody := tokenPollRequest{
		DeviceAuthID: strings.TrimSpace(code.DeviceAuthID),
		UserCode:     strings.TrimSpace(code.UserCode),
	}

	var out tokenPollResponse
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return tokenPollResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqData))
	if err != nil {
		return tokenPollResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return tokenPollResponse{}, fmt.Errorf("codex device auth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return tokenPollResponse{}, ErrAuthorizationPending
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusTooEarly || resp.StatusCode == http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if isPendingBody(body) {
			return tokenPollResponse{}, ErrAuthorizationPending
		}
		return tokenPollResponse{}, statusError(endpoint, resp.StatusCode, body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return tokenPollResponse{}, statusError(endpoint, resp.StatusCode, body)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return tokenPollResponse{}, fmt.Errorf("decode codex device auth response: %w", err)
	}
	if strings.TrimSpace(out.AuthorizationCode) == "" || strings.TrimSpace(out.CodeVerifier) == "" {
		return tokenPollResponse{}, fmt.Errorf("codex device auth response missing authorization code or verifier")
	}
	return out, nil
}

func exchangeAuthorizationCode(ctx context.Context, cfg OAuthConfig, redirectURI, code, codeVerifier string) (Token, error) {
	endpoint := strings.TrimRight(cfg.Issuer, "/") + "/oauth/token"
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	form.Set("client_id", cfg.ClientID)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("codex token exchange request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Token{}, statusError(endpoint, resp.StatusCode, body)
	}
	var decoded tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return Token{}, fmt.Errorf("decode codex token exchange response: %w", err)
	}
	token := tokenFromResponse(decoded, cfg.now())
	if token.AccessToken == "" || token.RefreshToken == "" {
		return Token{}, fmt.Errorf("codex token exchange response missing access or refresh token")
	}
	return token, nil
}

func tokenFromResponse(resp tokenResponse, now time.Time) Token {
	now = now.UTC()
	var expiresAt time.Time
	if d := durationFromAny(resp.ExpiresIn, 0); d > 0 {
		expiresAt = now.Add(d)
	} else {
		if exp, ok := JWTExpiration(resp.AccessToken); ok {
			expiresAt = exp
		}
	}
	accountID := firstNonEmpty(
		JWTStringClaim(resp.IDToken, "chatgpt_account_id"),
		JWTStringClaim(resp.AccessToken, "chatgpt_account_id"),
		JWTStringClaim(resp.AccessToken, "account_id"),
	)
	planType := firstNonEmpty(
		JWTStringClaim(resp.AccessToken, "chatgpt_plan_type"),
		JWTStringClaim(resp.IDToken, "chatgpt_plan_type"),
	)
	return Token{
		IDToken:      strings.TrimSpace(resp.IDToken),
		AccessToken:  strings.TrimSpace(resp.AccessToken),
		RefreshToken: strings.TrimSpace(resp.RefreshToken),
		AccountID:    accountID,
		PlanType:     planType,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func JWTExpiration(token string) (time.Time, bool) {
	claims, ok := jwtClaims(token)
	if !ok {
		return time.Time{}, false
	}
	exp, ok := numberClaim(claims, "exp")
	if !ok || exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0).UTC(), true
}

func JWTStringClaim(token string, key string) string {
	claims, ok := jwtClaims(token)
	if !ok {
		return ""
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if v, ok := claims[key].(string); ok {
		return strings.TrimSpace(v)
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if v, ok := auth[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func jwtClaims(token string) (map[string]any, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if payload, err = base64.URLEncoding.DecodeString(parts[1]); err != nil {
			return nil, false
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

func numberClaim(claims map[string]any, key string) (float64, bool) {
	switch v := claims[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("codex oauth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statusError(endpoint, resp.StatusCode, body)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode codex oauth response: %w", err)
	}
	return nil
}

func statusError(endpoint string, status int, body []byte) error {
	message := parseErrorMessage(body)
	u, _ := url.Parse(endpoint)
	hostPath := endpoint
	if u != nil && u.Host != "" {
		hostPath = u.Host + u.Path
	}
	if message == "" {
		return fmt.Errorf("codex oauth request to %s failed with status %d", hostPath, status)
	}
	return fmt.Errorf("codex oauth request to %s failed with status %d: %s", hostPath, status, message)
}

func parseErrorMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err == nil {
		for _, key := range []string{"error_description", "message", "detail", "code"} {
			if v, ok := decoded[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		if nested, ok := decoded["error"].(map[string]any); ok {
			for _, key := range []string{"message", "code"} {
				if v, ok := nested[key].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
		if v, ok := decoded["error"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return ""
	}
	text := string(body)
	if len(text) > 240 {
		text = text[:240]
	}
	return strings.TrimSpace(text)
}

func isPendingBody(body []byte) bool {
	message := strings.ToLower(parseErrorMessage(body))
	return strings.Contains(message, "authorization_pending") ||
		strings.Contains(message, "pending") ||
		strings.Contains(message, "slow_down")
}

func normalizeOAuthConfig(cfg OAuthConfig) OAuthConfig {
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if cfg.Issuer == "" {
		cfg.Issuer = DefaultIssuer
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return cfg
}

func (cfg OAuthConfig) now() time.Time {
	if cfg.Now != nil {
		return cfg.Now().UTC()
	}
	return time.Now().UTC()
}

func durationFromAny(raw any, fallback time.Duration) time.Duration {
	switch v := raw.(type) {
	case nil:
		return fallback
	case float64:
		if v <= 0 {
			return fallback
		}
		return time.Duration(v) * time.Second
	case int:
		if v <= 0 {
			return fallback
		}
		return time.Duration(v) * time.Second
	case int64:
		if v <= 0 {
			return fallback
		}
		return time.Duration(v) * time.Second
	case json.Number:
		i, err := v.Int64()
		if err != nil || i <= 0 {
			return fallback
		}
		return time.Duration(i) * time.Second
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return fallback
		}
		if seconds, err := strconv.ParseInt(v, 10, 64); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		return fallback
	default:
		return fallback
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
