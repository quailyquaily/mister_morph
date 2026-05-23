package proaccount

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type Status struct {
	LoggedIn                  bool           `json:"logged_in"`
	AccessTokenPresent        bool           `json:"access_token_present"`
	RefreshTokenPresent       bool           `json:"refresh_token_present"`
	AccessTokenExpired        bool           `json:"access_token_expired"`
	SubscriptionAPIKeyPresent bool           `json:"subscription_api_key_present"`
	Subscription              string         `json:"subscription,omitempty"`
	Scope                     string         `json:"scope,omitempty"`
	ExpiresAt                 *time.Time     `json:"expires_at,omitempty"`
	UserInfo                  map[string]any `json:"user,omitempty"`
	FileModeOK                bool           `json:"file_mode_ok"`
	FileModeWarning           string         `json:"file_mode_warning,omitempty"`
}

type StoredSession struct {
	Version            int            `json:"version"`
	AccessToken        string         `json:"access_token"`
	TokenType          string         `json:"token_type"`
	RefreshToken       string         `json:"refresh_token"`
	Scope              string         `json:"scope"`
	ExpiresAt          time.Time      `json:"expires_at"`
	SubscriptionAPIKey string         `json:"subscription_api_key"`
	UserInfo           map[string]any `json:"userinfo"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

func SessionPath(stateDir string) string {
	return filepath.Clean(filepath.Join(pathutil.ResolveStateDir(stateDir), "auth", "pro-oauth.json"))
}

func DisplaySessionPath() string {
	return "<file_state_dir>/auth/pro-oauth.json"
}

func ReadSession(stateDir string) (StoredSession, bool, error) {
	var session StoredSession
	ok, err := fsstore.ReadJSON(SessionPath(stateDir), &session)
	if err != nil || !ok {
		return StoredSession{}, ok, err
	}
	return normalizeSession(session), true, nil
}

func WriteSession(stateDir string, session StoredSession) error {
	session = normalizeSession(session)
	if session.Version == 0 {
		session.Version = 1
	}
	session.UpdatedAt = time.Now().UTC()
	return fsstore.WriteJSONAtomic(SessionPath(stateDir), session, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600})
}

func DeleteSession(stateDir string) (bool, error) {
	path := SessionPath(stateDir)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return false, fmt.Errorf("Pro account session path is a directory")
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func ReadSubscriptionAPIKey(stateDir string) (string, bool, error) {
	session, ok, err := ReadSession(stateDir)
	if err != nil || !ok {
		return "", ok, err
	}
	apiKey := strings.TrimSpace(session.SubscriptionAPIKey)
	if apiKey == "" {
		return "", false, nil
	}
	return apiKey, true, nil
}

func ReadStatus(stateDir string, now time.Time) Status {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	path := SessionPath(stateDir)
	status := Status{
		FileModeOK: true,
		UserInfo:   map[string]any{},
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return status
		}
		status.FileModeOK = false
		status.FileModeWarning = "Pro account session file cannot be inspected"
		return status
	}
	if info.IsDir() {
		status.FileModeOK = false
		status.FileModeWarning = "Pro account session path is a directory"
		return status
	}
	if warning := fileModeWarning(info.Mode()); warning != "" {
		status.FileModeOK = false
		status.FileModeWarning = warning
	}

	session, ok, err := ReadSession(stateDir)
	if err != nil || !ok {
		status.FileModeOK = false
		status.FileModeWarning = firstNonEmpty(status.FileModeWarning, "Pro account session file cannot be decoded")
		return status
	}
	status.AccessTokenPresent = strings.TrimSpace(session.AccessToken) != ""
	status.RefreshTokenPresent = strings.TrimSpace(session.RefreshToken) != ""
	status.AccessTokenExpired = status.AccessTokenPresent && !session.ExpiresAt.IsZero() && !session.ExpiresAt.After(now.UTC())
	status.SubscriptionAPIKeyPresent = strings.TrimSpace(session.SubscriptionAPIKey) != ""
	status.UserInfo = session.UserInfo
	status.Scope = session.Scope
	if !session.ExpiresAt.IsZero() {
		expiresAt := session.ExpiresAt
		status.ExpiresAt = &expiresAt
	}
	status.LoggedIn = UnionID(session.UserInfo) != "" && (session.IsAccessTokenUsable(now) || status.RefreshTokenPresent)
	if status.LoggedIn {
		status.Subscription = "Lifetime"
	}
	return status
}

func NewStoredSession(token TokenResponse, userInfo map[string]any, subscriptionAPIKey string, now time.Time) StoredSession {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return normalizeSession(StoredSession{
		Version:            1,
		AccessToken:        strings.TrimSpace(token.AccessToken),
		TokenType:          firstNonEmpty(token.TokenType, "Bearer"),
		RefreshToken:       strings.TrimSpace(token.RefreshToken),
		Scope:              strings.TrimSpace(token.Scope),
		ExpiresAt:          expiresAt,
		SubscriptionAPIKey: strings.TrimSpace(subscriptionAPIKey),
		UserInfo:           userInfo,
		UpdatedAt:          now,
	})
}

func (s StoredSession) IsAccessTokenUsable(now time.Time) bool {
	if strings.TrimSpace(s.AccessToken) == "" {
		return false
	}
	if s.ExpiresAt.IsZero() {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.ExpiresAt.After(now.UTC().Add(refreshSkew))
}

func normalizeSession(session StoredSession) StoredSession {
	session.AccessToken = strings.TrimSpace(session.AccessToken)
	session.TokenType = firstNonEmpty(session.TokenType, "Bearer")
	session.RefreshToken = strings.TrimSpace(session.RefreshToken)
	session.Scope = strings.TrimSpace(session.Scope)
	session.SubscriptionAPIKey = strings.TrimSpace(session.SubscriptionAPIKey)
	session.ExpiresAt = session.ExpiresAt.UTC()
	session.UpdatedAt = session.UpdatedAt.UTC()
	if session.UserInfo == nil {
		session.UserInfo = map[string]any{}
	}
	return session
}

func fileModeWarning(mode os.FileMode) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if mode.Perm()&0o077 != 0 {
		return "Pro account session file permissions are wider than 0600"
	}
	return ""
}
