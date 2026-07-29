package xaiauth

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
	FileModeOK          bool       `json:"file_mode_ok"`
	FileModeWarning     string     `json:"file_mode_warning,omitempty"`
}

func TokenPath(stateDir string) string {
	return filepath.Clean(filepath.Join(pathutil.ResolveStateDir(stateDir), "auth", "xai.json"))
}

const DisplayTokenPath = "<file_state_dir>/auth/xai.json"

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
	path := TokenPath(stateDir)
	authDir := filepath.Dir(path)
	if err := fsstore.EnsureDir(authDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(authDir, 0o700); err != nil {
		return fmt.Errorf("secure xAI token directory: %w", err)
	}
	return fsstore.WriteJSONAtomic(path, token, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func DeleteToken(stateDir string) (bool, error) {
	path := TokenPath(stateDir)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return false, fmt.Errorf("xAI token path is a directory")
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
	status := Status{FileModeOK: true}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
	if warning := tokenFileModeWarning(info.Mode()); warning != "" {
		status.FileModeOK = false
		status.FileModeWarning = warning
	}
	if dirInfo, dirErr := os.Stat(filepath.Dir(path)); dirErr != nil {
		status.FileModeOK = false
		if status.FileModeWarning == "" {
			status.FileModeWarning = "token directory cannot be inspected"
		}
	} else if warning := tokenDirectoryModeWarning(dirInfo.Mode()); warning != "" {
		status.FileModeOK = false
		if status.FileModeWarning == "" {
			status.FileModeWarning = warning
		}
	}
	token, ok, err := ReadToken(stateDir)
	if err != nil || !ok {
		status.FileModeOK = false
		if status.FileModeWarning == "" {
			status.FileModeWarning = "token file cannot be decoded"
		}
		return status
	}
	status.AccessTokenPresent = token.AccessToken != ""
	status.RefreshTokenPresent = token.RefreshToken != ""
	status.AccessTokenExpired = status.AccessTokenPresent &&
		(token.ExpiresAt.IsZero() || !token.ExpiresAt.After(now.UTC()))
	status.LoggedIn = token.IsAccessTokenUsable(now) || status.RefreshTokenPresent
	if !token.ExpiresAt.IsZero() {
		expiresAt := token.ExpiresAt
		status.ExpiresAt = &expiresAt
	}
	return status
}

func normalizeToken(token Token) Token {
	token.AccessToken = strings.TrimSpace(token.AccessToken)
	token.RefreshToken = strings.TrimSpace(token.RefreshToken)
	token.TokenType = strings.TrimSpace(token.TokenType)
	token.Scope = strings.Join(strings.Fields(token.Scope), " ")
	token.ExpiresAt = token.ExpiresAt.UTC()
	token.CreatedAt = token.CreatedAt.UTC()
	token.UpdatedAt = token.UpdatedAt.UTC()
	return token
}

func tokenFileModeWarning(mode os.FileMode) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if mode.Perm()&0o077 != 0 {
		return "token file permissions are wider than 0600"
	}
	return ""
}

func tokenDirectoryModeWarning(mode os.FileMode) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if !mode.IsDir() {
		return "token directory path is not a directory"
	}
	if mode.Perm()&0o077 != 0 {
		return "token directory permissions are wider than 0700"
	}
	return ""
}
