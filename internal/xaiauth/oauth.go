package xaiauth

import (
	"context"
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
	ProviderName   = "xai_oauth"
	DefaultIssuer  = "https://auth.x.ai"
	DefaultAPIBase = "https://api.x.ai/v1"
	DefaultModel   = "grok-4.5"
	// xAI's shared public OAuth client is also used by OpenClaw. MisterMorph
	// requests basic profile data but not email.
	DefaultClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	DefaultScope    = "openid profile offline_access grok-cli:access api:access"

	deviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	defaultPollInterval = 5 * time.Second
)

var (
	ErrAuthorizationPending  = errors.New("xAI device authorization pending")
	ErrSlowDown              = errors.New("xAI device authorization polling must slow down")
	ErrAccessDenied          = errors.New("xAI device authorization denied")
	ErrDeviceCodeExpired     = errors.New("xAI device authorization expired")
	ErrNotLoggedIn           = errors.New("xAI OAuth is not logged in; run `mistermorph auth xai login`")
	ErrRevocationUnsupported = errors.New("xAI OAuth revocation endpoint is unavailable")

	refreshMu sync.Mutex
)

type OAuthConfig struct {
	Issuer     string
	ClientID   string
	Scope      string
	HTTPClient *http.Client
	Now        func() time.Time
}

type DeviceCode struct {
	VerificationURL         string
	VerificationURLComplete string
	UserCode                string
	Interval                time.Duration
	ExpiresAt               time.Time

	deviceCode    string
	tokenEndpoint string
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type discoveryDocument struct {
	Issuer                      string `json:"issuer"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	RevocationEndpoint          string `json:"revocation_endpoint"`
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               any    `json:"expires_in"`
	Interval                any    `json:"interval"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    any    `json:"expires_in"`
}

type oauthErrorResponse struct {
	Error string `json:"error"`
}

func RequestDeviceCode(ctx context.Context, cfg OAuthConfig) (DeviceCode, error) {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return DeviceCode{}, err
	}
	discovery, err := discover(ctx, cfg)
	if err != nil {
		return DeviceCode{}, err
	}

	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("scope", cfg.Scope)
	var response deviceCodeResponse
	if err := postForm(ctx, cfg.HTTPClient, discovery.DeviceAuthorizationEndpoint, form, &response); err != nil {
		return DeviceCode{}, fmt.Errorf("request xAI device code: %w", err)
	}

	deviceCode := strings.TrimSpace(response.DeviceCode)
	userCode := strings.TrimSpace(response.UserCode)
	verificationURL := strings.TrimSpace(response.VerificationURI)
	if deviceCode == "" || userCode == "" || verificationURL == "" {
		return DeviceCode{}, fmt.Errorf("xAI device code response is missing required fields")
	}
	if err := validateHTTPSURL("verification_uri", verificationURL, "accounts.x.ai", "auth.x.ai"); err != nil {
		return DeviceCode{}, err
	}
	verificationURLComplete := strings.TrimSpace(response.VerificationURIComplete)
	if verificationURLComplete != "" {
		if err := validateHTTPSURL("verification_uri_complete", verificationURLComplete, "accounts.x.ai", "auth.x.ai"); err != nil {
			return DeviceCode{}, err
		}
	}
	expiresIn := durationSeconds(response.ExpiresIn, 0)
	if expiresIn <= 0 {
		return DeviceCode{}, fmt.Errorf("xAI device code response has invalid expires_in")
	}
	interval := durationSeconds(response.Interval, defaultPollInterval)
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return DeviceCode{
		VerificationURL:         verificationURL,
		VerificationURLComplete: verificationURLComplete,
		UserCode:                userCode,
		Interval:                interval,
		ExpiresAt:               cfg.now().Add(expiresIn),
		deviceCode:              deviceCode,
		tokenEndpoint:           discovery.TokenEndpoint,
	}, nil
}

func PollDeviceCode(ctx context.Context, cfg OAuthConfig, code DeviceCode) (Token, error) {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(code.deviceCode) == "" || strings.TrimSpace(code.tokenEndpoint) == "" {
		return Token{}, fmt.Errorf("xAI device authorization session is invalid")
	}
	if !code.ExpiresAt.IsZero() && !code.ExpiresAt.After(cfg.now()) {
		return Token{}, ErrDeviceCodeExpired
	}
	if err := validateOAuthEndpoint("token_endpoint", code.tokenEndpoint); err != nil {
		return Token{}, err
	}

	form := url.Values{}
	form.Set("grant_type", deviceCodeGrantType)
	form.Set("client_id", cfg.ClientID)
	form.Set("device_code", code.deviceCode)

	var response tokenResponse
	err = postForm(ctx, cfg.HTTPClient, code.tokenEndpoint, form, &response)
	if err != nil {
		switch oauthErrorCode(err) {
		case "authorization_pending":
			return Token{}, ErrAuthorizationPending
		case "slow_down":
			return Token{}, ErrSlowDown
		case "access_denied":
			return Token{}, ErrAccessDenied
		case "expired_token":
			return Token{}, ErrDeviceCodeExpired
		default:
			return Token{}, fmt.Errorf("poll xAI device authorization: %w", err)
		}
	}
	token, err := tokenFromResponse(response, cfg.now(), true)
	if err != nil {
		return Token{}, err
	}
	return token, nil
}

func RefreshToken(ctx context.Context, cfg OAuthConfig, refreshToken string) (Token, error) {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return Token{}, err
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, ErrNotLoggedIn
	}
	discovery, err := discover(ctx, cfg)
	if err != nil {
		return Token{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", cfg.ClientID)
	form.Set("refresh_token", refreshToken)

	var response tokenResponse
	if err := postForm(ctx, cfg.HTTPClient, discovery.TokenEndpoint, form, &response); err != nil {
		if oauthErrorCode(err) == "invalid_grant" {
			return Token{}, ErrNotLoggedIn
		}
		return Token{}, fmt.Errorf("refresh xAI OAuth token: %w", err)
	}
	token, err := tokenFromResponse(response, cfg.now(), false)
	if err != nil {
		return Token{}, err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	return token, nil
}

func ResolveToken(ctx context.Context, stateDir string, cfg OAuthConfig) (Token, error) {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return Token{}, err
	}
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

	return refreshStoredToken(ctx, stateDir, cfg, token)
}

// RefreshRejectedToken refreshes the token that an inference request received
// a 401 for. If another caller has already replaced it, the newer stored token
// is returned without performing another refresh.
func RefreshRejectedToken(ctx context.Context, stateDir string, cfg OAuthConfig, rejectedAccessToken string) (Token, error) {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return Token{}, err
	}
	rejectedAccessToken = strings.TrimSpace(rejectedAccessToken)

	refreshMu.Lock()
	defer refreshMu.Unlock()

	token, ok, err := ReadToken(stateDir)
	if err != nil {
		return Token{}, err
	}
	if !ok || strings.TrimSpace(token.RefreshToken) == "" {
		return Token{}, ErrNotLoggedIn
	}
	if token.AccessToken != "" && token.AccessToken != rejectedAccessToken && token.IsAccessTokenUsable(cfg.now()) {
		return token, nil
	}
	return refreshStoredToken(ctx, stateDir, cfg, token)
}

func refreshStoredToken(ctx context.Context, stateDir string, cfg OAuthConfig, token Token) (Token, error) {
	refreshed, err := RefreshToken(ctx, cfg, token.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrNotLoggedIn) {
			if _, deleteErr := DeleteToken(stateDir); deleteErr != nil {
				return Token{}, fmt.Errorf("%w; delete unusable local token: %v", err, deleteErr)
			}
		}
		return Token{}, err
	}
	if refreshed.Scope == "" {
		refreshed.Scope = token.Scope
	}
	if refreshed.TokenType == "" {
		refreshed.TokenType = token.TokenType
	}
	if refreshed.CreatedAt.IsZero() {
		refreshed.CreatedAt = token.CreatedAt
	}
	if err := WriteToken(stateDir, refreshed); err != nil {
		return Token{}, err
	}
	return refreshed, nil
}

func RevokeToken(ctx context.Context, cfg OAuthConfig, token Token) error {
	cfg, err := normalizeOAuthConfig(cfg)
	if err != nil {
		return err
	}
	value := strings.TrimSpace(token.RefreshToken)
	hint := "refresh_token"
	if value == "" {
		value = strings.TrimSpace(token.AccessToken)
		hint = "access_token"
	}
	if value == "" {
		return nil
	}
	discovery, err := discover(ctx, cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(discovery.RevocationEndpoint) == "" {
		return ErrRevocationUnsupported
	}
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("token", value)
	form.Set("token_type_hint", hint)
	if err := postForm(ctx, cfg.HTTPClient, discovery.RevocationEndpoint, form, nil); err != nil {
		return fmt.Errorf("revoke xAI OAuth token: %w", err)
	}
	return nil
}

func (t Token) IsAccessTokenUsable(now time.Time) bool {
	if strings.TrimSpace(t.AccessToken) == "" || t.ExpiresAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return t.ExpiresAt.After(now.UTC().Add(time.Minute))
}

func discover(ctx context.Context, cfg OAuthConfig) (discoveryDocument, error) {
	endpoint := strings.TrimRight(cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discoveryDocument{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("fetch xAI OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discoveryDocument{}, newOAuthHTTPError(resp)
	}
	var discovery discoveryDocument
	if err := decodeJSON(resp.Body, &discovery); err != nil {
		return discoveryDocument{}, fmt.Errorf("decode xAI OIDC discovery: %w", err)
	}
	if strings.TrimSpace(discovery.Issuer) != DefaultIssuer {
		return discoveryDocument{}, fmt.Errorf("xAI OIDC discovery issuer does not match %s", DefaultIssuer)
	}
	if err := validateOAuthEndpoint("device_authorization_endpoint", discovery.DeviceAuthorizationEndpoint); err != nil {
		return discoveryDocument{}, err
	}
	if err := validateOAuthEndpoint("token_endpoint", discovery.TokenEndpoint); err != nil {
		return discoveryDocument{}, err
	}
	if strings.TrimSpace(discovery.RevocationEndpoint) != "" {
		if err := validateOAuthEndpoint("revocation_endpoint", discovery.RevocationEndpoint); err != nil {
			return discoveryDocument{}, err
		}
	}
	return discovery, nil
}

func normalizeOAuthConfig(cfg OAuthConfig) (OAuthConfig, error) {
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if cfg.Issuer == "" {
		cfg.Issuer = DefaultIssuer
	}
	if cfg.Issuer != DefaultIssuer {
		return OAuthConfig{}, fmt.Errorf("xAI OAuth issuer must be %s", DefaultIssuer)
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	cfg.Scope = strings.Join(strings.Fields(cfg.Scope), " ")
	if cfg.Scope == "" {
		cfg.Scope = DefaultScope
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	client := *cfg.HTTPClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	cfg.HTTPClient = &client
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return cfg, nil
}

func (cfg OAuthConfig) now() time.Time {
	now := cfg.Now()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func validateOAuthEndpoint(field, rawURL string) error {
	return validateHTTPSURL(field, rawURL, "auth.x.ai")
}

func validateHTTPSURL(field, rawURL string, allowedHosts ...string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("xAI OAuth %s must be an HTTPS URL", field)
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := false
	for _, candidate := range allowedHosts {
		if host == strings.ToLower(strings.TrimSpace(candidate)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("xAI OAuth %s uses untrusted host %q", field, host)
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return fmt.Errorf("xAI OAuth %s uses unexpected port", field)
	}
	return nil
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newOAuthHTTPError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	return decodeJSON(resp.Body, out)
}

type oauthHTTPError struct {
	StatusCode int
	Code       string
}

func (e *oauthHTTPError) Error() string {
	if e == nil {
		return "xAI OAuth request failed"
	}
	if code := displayOAuthErrorCode(e.Code); code != "" {
		return fmt.Sprintf("xAI OAuth request failed with HTTP %d (%s)", e.StatusCode, code)
	}
	return fmt.Sprintf("xAI OAuth request failed with HTTP %d", e.StatusCode)
}

func displayOAuthErrorCode(code string) string {
	switch strings.TrimSpace(code) {
	case "authorization_pending", "slow_down", "access_denied", "expired_token",
		"invalid_request", "invalid_client", "invalid_grant", "unauthorized_client",
		"unsupported_grant_type", "invalid_scope":
		return strings.TrimSpace(code)
	default:
		return ""
	}
}

func newOAuthHTTPError(resp *http.Response) error {
	var payload oauthErrorResponse
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
	return &oauthHTTPError{
		StatusCode: resp.StatusCode,
		Code:       strings.TrimSpace(payload.Error),
	}
}

func oauthErrorCode(err error) string {
	var target *oauthHTTPError
	if !errors.As(err, &target) {
		return ""
	}
	return target.Code
}

func tokenFromResponse(response tokenResponse, now time.Time, requireRefresh bool) (Token, error) {
	accessToken := strings.TrimSpace(response.AccessToken)
	refreshToken := strings.TrimSpace(response.RefreshToken)
	if accessToken == "" {
		return Token{}, fmt.Errorf("xAI OAuth token response is missing access_token")
	}
	if requireRefresh && refreshToken == "" {
		return Token{}, fmt.Errorf("xAI OAuth token response is missing refresh_token")
	}
	expiresIn := durationSeconds(response.ExpiresIn, 0)
	if expiresIn <= 0 {
		return Token{}, fmt.Errorf("xAI OAuth token response has invalid expires_in")
	}
	tokenType := strings.TrimSpace(response.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Scope:        strings.Join(strings.Fields(response.Scope), " "),
		ExpiresAt:    now.Add(expiresIn),
	}, nil
}

func durationSeconds(value any, fallback time.Duration) time.Duration {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	case float32:
		if v > 0 {
			return time.Duration(float64(v) * float64(time.Second))
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case json.Number:
		if seconds, err := strconv.ParseFloat(string(v), 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	case string:
		if seconds, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	return fallback
}

func decodeJSON(reader io.Reader, out any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}
