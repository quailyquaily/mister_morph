package codexauth

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
	LoggedIn            bool       `json:"logged_in"`
	AccessTokenPresent  bool       `json:"access_token_present"`
	RefreshTokenPresent bool       `json:"refresh_token_present"`
	AccessTokenExpired  bool       `json:"access_token_expired"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	AccountID           string     `json:"account_id,omitempty"`
	PlanType            string     `json:"plan_type,omitempty"`
	FileModeOK          bool       `json:"file_mode_ok"`
	FileModeWarning     string     `json:"file_mode_warning,omitempty"`
}

func TokenPath(stateDir string) string {
	return filepath.Clean(filepath.Join(pathutil.ResolveStateDir(stateDir), "auth", "codex.json"))
}

func DisplayTokenPath() string {
	return "<file_state_dir>/auth/codex.json"
}

func ReadToken(stateDir string) (Token, bool, error) {
	var token Token
	ok, err := fsstore.ReadJSON(TokenPath(stateDir), &token)
	if err != nil || !ok {
		return Token{}, ok, err
	}
	return normalizeToken(token), true, nil
}

func WriteToken(stateDir string, token Token) error {
	now := time.Now().UTC()
	token = normalizeToken(token)
	if token.CreatedAt.IsZero() {
		token.CreatedAt = now
	}
	token.UpdatedAt = now
	return fsstore.WriteJSONAtomic(TokenPath(stateDir), token, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600})
}

func DeleteToken(stateDir string) (bool, error) {
	path := TokenPath(stateDir)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return false, fmt.Errorf("codex token path is a directory")
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func ReadStatus(stateDir string, now time.Time) Status {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	path := TokenPath(stateDir)
	status := Status{
		FileModeOK: true,
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return status
		}
		status.FileModeOK = false
		status.FileModeWarning = "token file cannot be inspected"
		return status
	}
	if info.IsDir() {
		status.FileModeOK = false
		status.FileModeWarning = "token path is a directory"
		return status
	}
	if warning := fileModeWarning(info.Mode()); warning != "" {
		status.FileModeOK = false
		status.FileModeWarning = warning
	}

	token, ok, err := ReadToken(stateDir)
	if err != nil || !ok {
		status.FileModeOK = false
		status.FileModeWarning = firstNonEmpty(status.FileModeWarning, "token file cannot be decoded")
		return status
	}
	status.AccessTokenPresent = strings.TrimSpace(token.AccessToken) != ""
	status.RefreshTokenPresent = strings.TrimSpace(token.RefreshToken) != ""
	status.LoggedIn = token.IsAccessTokenUsable(now) || status.RefreshTokenPresent
	if !token.ExpiresAt.IsZero() {
		expiresAt := token.ExpiresAt
		status.ExpiresAt = &expiresAt
	}
	status.AccountID = token.AccountID
	status.PlanType = token.PlanType
	status.AccessTokenExpired = status.AccessTokenPresent && !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(now.UTC())
	return status
}

func normalizeToken(token Token) Token {
	token.IDToken = strings.TrimSpace(token.IDToken)
	token.AccessToken = strings.TrimSpace(token.AccessToken)
	token.RefreshToken = strings.TrimSpace(token.RefreshToken)
	token.AccountID = strings.TrimSpace(token.AccountID)
	token.PlanType = strings.TrimSpace(token.PlanType)
	token.CreatedAt = token.CreatedAt.UTC()
	token.UpdatedAt = token.UpdatedAt.UTC()
	token.ExpiresAt = token.ExpiresAt.UTC()
	if token.AccountID == "" {
		token.AccountID = firstNonEmpty(
			JWTStringClaim(token.IDToken, "chatgpt_account_id"),
			JWTStringClaim(token.AccessToken, "chatgpt_account_id"),
			JWTStringClaim(token.AccessToken, "account_id"),
		)
	}
	if token.PlanType == "" {
		token.PlanType = firstNonEmpty(
			JWTStringClaim(token.AccessToken, "chatgpt_plan_type"),
			JWTStringClaim(token.IDToken, "chatgpt_plan_type"),
		)
	}
	if token.ExpiresAt.IsZero() {
		if exp, ok := JWTExpiration(token.AccessToken); ok {
			token.ExpiresAt = exp
		}
	}
	return token
}

func fileModeWarning(mode os.FileMode) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if mode.Perm()&0o077 != 0 {
		return "token file permissions are wider than 0600"
	}
	return ""
}
