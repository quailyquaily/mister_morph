package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
)

type xaiLoginSession struct {
	DeviceCode   xaiauth.DeviceCode
	ExpiresAt    time.Time
	PollInterval time.Duration
}

type xaiLoginStore struct {
	mu       sync.Mutex
	sessions map[string]xaiLoginSession
}

func newXAILoginStore() *xaiLoginStore {
	return &xaiLoginStore{sessions: map[string]xaiLoginSession{}}
}

func (s *xaiLoginStore) Create(code xaiauth.DeviceCode) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil xAI login store")
	}
	id, err := randomOpaqueID()
	if err != nil {
		return "", err
	}
	expiresAt := code.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(15 * time.Minute)
	}
	interval := code.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.mu.Lock()
	s.pruneLocked(time.Now().UTC())
	s.sessions[id] = xaiLoginSession{
		DeviceCode:   code,
		ExpiresAt:    expiresAt,
		PollInterval: interval,
	}
	s.mu.Unlock()
	return id, nil
}

func (s *xaiLoginStore) Get(id string) (xaiLoginSession, bool) {
	if s == nil {
		return xaiLoginSession{}, false
	}
	id = strings.TrimSpace(id)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	item, ok := s.sessions[id]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.sessions, id)
		return xaiLoginSession{}, false
	}
	return item, true
}

func (s *xaiLoginStore) SlowDown(id string) time.Duration {
	if s == nil {
		return 0
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return 0
	}
	item.PollInterval += 5 * time.Second
	s.sessions[id] = item
	return item.PollInterval
}

func (s *xaiLoginStore) Delete(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, strings.TrimSpace(id))
	s.mu.Unlock()
}

func (s *xaiLoginStore) pruneLocked(now time.Time) {
	for id, item := range s.sessions {
		if !item.ExpiresAt.After(now) {
			delete(s.sessions, id)
		}
	}
}

func (s *server) handleXAIAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, xaiauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()))
}

func (s *server) handleXAIAuthLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deviceCode, err := xaiauth.RequestDeviceCode(r.Context(), s.xaiOAuth)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sessionID, err := s.xaiLogins.Create(deviceCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create xAI login session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"session_id":                sessionID,
		"verification_url":          deviceCode.VerificationURL,
		"verification_url_complete": deviceCode.VerificationURLComplete,
		"user_code":                 deviceCode.UserCode,
		"expires_at":                deviceCode.ExpiresAt.Format(time.RFC3339),
		"interval_seconds":          pollIntervalSeconds(deviceCode.Interval),
	})
}

func (s *server) handleXAIAuthLoginPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		SessionID  string `json:"session_id"`
		SetDefault bool   `json:"set_default"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	session, ok := s.xaiLogins.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "xAI login session not found or expired")
		return
	}
	token, err := xaiauth.PollDeviceCode(r.Context(), s.xaiOAuth, session.DeviceCode)
	switch {
	case errors.Is(err, xaiauth.ErrAuthorizationPending):
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"pending":          true,
			"interval_seconds": pollIntervalSeconds(session.PollInterval),
		})
		return
	case errors.Is(err, xaiauth.ErrSlowDown):
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"pending":          true,
			"interval_seconds": pollIntervalSeconds(s.xaiLogins.SlowDown(sessionID)),
		})
		return
	case errors.Is(err, xaiauth.ErrAccessDenied), errors.Is(err, xaiauth.ErrDeviceCodeExpired):
		s.xaiLogins.Delete(sessionID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := xaiauth.WriteToken(s.cfg.stateDir, token); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save xAI token")
		return
	}
	s.xaiLogins.Delete(sessionID)

	settingsUpdated := false
	if req.SetDefault {
		if err := s.setXAIAsDefaultLLM(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settingsUpdated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"pending":          false,
		"settings_updated": settingsUpdated,
		"status":           xaiauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	})
}

func (s *server) setXAIAsDefaultLLM() error {
	provider := xaiauth.ProviderName
	model := xaiauth.DefaultModel
	empty := ""
	update := agentsettings.AgentSettingsUpdate{
		LLM: agentsettings.LLMSettingsUpdate{
			LLMConfigFieldsUpdate: agentsettings.LLMConfigFieldsUpdate{
				InferenceProvider:   &provider,
				Provider:            &provider,
				Model:               &model,
				Endpoint:            &empty,
				APIKey:              &empty,
				CloudflareAPIToken:  &empty,
				CloudflareAccountID: &empty,
				BedrockAWSKey:       &empty,
				BedrockAWSSecret:    &empty,
				BedrockRegion:       &empty,
				BedrockModelARN:     &empty,
			},
		},
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		return err
	}
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{ConfigPath: configPath, Reader: s.currentRuntimeConfigReader()})
	_, err = owner.Update(context.Background(), update)
	return err
}

func (s *server) handleXAIAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token, present, readErr := xaiauth.ReadToken(s.cfg.stateDir)
	var revokeErr error
	if readErr != nil {
		revokeErr = readErr
	} else if present {
		revokeErr = xaiauth.RevokeToken(r.Context(), s.xaiOAuth, token)
	}
	removed, err := xaiauth.DeleteToken(s.cfg.stateDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{
		"ok":      true,
		"removed": removed,
		"revoked": present && revokeErr == nil,
		"status":  xaiauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	}
	if revokeErr != nil {
		response["revocation_warning"] = "xAI authorization could not be revoked remotely; the local token was deleted"
	}
	writeJSON(w, http.StatusOK, response)
}

func pollIntervalSeconds(interval time.Duration) int {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return int((interval + time.Second - 1) / time.Second)
}
