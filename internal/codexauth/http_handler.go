package codexauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type HTTPHandlerOptions struct {
	StateDir   string
	OAuth      OAuthConfig
	SetDefault func(context.Context) error
}

type HTTPHandler struct {
	stateDir   string
	oauth      OAuthConfig
	sessions   *loginSessionStore
	setDefault func(context.Context) error
}

type loginSession struct {
	deviceCode DeviceCode
	expiresAt  time.Time
}

type loginSessionStore struct {
	mu       sync.Mutex
	sessions map[string]loginSession
}

func NewHTTPHandler(opts HTTPHandlerOptions) *HTTPHandler {
	return &HTTPHandler{
		stateDir:   strings.TrimSpace(opts.StateDir),
		oauth:      normalizeOAuthConfig(opts.OAuth),
		sessions:   &loginSessionStore{sessions: map[string]loginSession{}},
		setDefault: opts.SetDefault,
	}
}

func (h *HTTPHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeHTTPJSON(w, http.StatusOK, ReadStatus(h.stateDir, h.oauth.now()))
}

func (h *HTTPHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := ReadStatus(h.stateDir, h.oauth.now())
	needsRefresh := status.RefreshTokenPresent && (!status.AccessTokenPresent || status.AccessTokenExpired)
	refreshed := false
	if needsRefresh {
		if _, err := ResolveToken(r.Context(), h.stateDir, h.oauth); err != nil {
			if !errors.Is(err, ErrNotLoggedIn) {
				writeHTTPError(w, http.StatusBadGateway, err.Error())
				return
			}
		} else {
			refreshed = true
		}
		status = ReadStatus(h.stateDir, h.oauth.now())
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"refreshed":      refreshed,
		"requires_login": !status.LoggedIn,
		"status":         status,
	})
}

func (h *HTTPHandler) LoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deviceCode, err := RequestDeviceCode(r.Context(), h.oauth)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	sessionID, err := h.sessions.create(deviceCode, h.oauth.now())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "failed to create login session")
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"session_id":       sessionID,
		"verification_url": deviceCode.VerificationURL,
		"user_code":        deviceCode.UserCode,
		"expires_at":       deviceCode.ExpiresAt.Format(time.RFC3339),
		"interval_seconds": int(deviceCode.Interval.Seconds()),
	})
}

func (h *HTTPHandler) LoginPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		SessionID  string `json:"session_id"`
		SetDefault bool   `json:"set_default"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	session, ok := h.sessions.get(sessionID, h.oauth.now())
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "codex login session not found or expired")
		return
	}
	token, err := CompleteDeviceCodeLogin(r.Context(), h.oauth, session.deviceCode)
	if IsAuthorizationPending(err) {
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"pending": true,
		})
		return
	}
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := WriteToken(h.stateDir, token); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "failed to save codex token")
		return
	}
	h.sessions.delete(sessionID)
	settingsUpdated := false
	if req.SetDefault {
		if h.setDefault == nil {
			writeHTTPError(w, http.StatusServiceUnavailable, "agent settings are unavailable")
			return
		}
		if err := h.setDefault(r.Context()); err != nil {
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settingsUpdated = true
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"pending":          false,
		"settings_updated": settingsUpdated,
		"status":           ReadStatus(h.stateDir, h.oauth.now()),
	})
}

func (h *HTTPHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	removed, err := DeleteToken(h.stateDir)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"status":  ReadStatus(h.stateDir, h.oauth.now()),
	})
}

func (s *loginSessionStore) create(code DeviceCode, now time.Time) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil codex login store")
	}
	id, err := newLoginSessionID()
	if err != nil {
		return "", err
	}
	expiresAt := code.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = now.UTC().Add(defaultDeviceTTL)
	}
	s.mu.Lock()
	s.pruneLocked(now.UTC())
	s.sessions[id] = loginSession{deviceCode: code, expiresAt: expiresAt}
	s.mu.Unlock()
	return id, nil
}

func (s *loginSessionStore) get(id string, now time.Time) (loginSession, bool) {
	if s == nil {
		return loginSession{}, false
	}
	id = strings.TrimSpace(id)
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	item, ok := s.sessions[id]
	if !ok || !item.expiresAt.After(now) {
		delete(s.sessions, id)
		return loginSession{}, false
	}
	return item, true
}

func (s *loginSessionStore) delete(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, strings.TrimSpace(id))
	s.mu.Unlock()
}

func (s *loginSessionStore) pruneLocked(now time.Time) {
	for id, item := range s.sessions {
		if !item.expiresAt.After(now) {
			delete(s.sessions, id)
		}
	}
}

func newLoginSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func writeHTTPJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Vary", "Authorization")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	writeHTTPJSON(w, status, map[string]any{"error": strings.TrimSpace(message)})
}
