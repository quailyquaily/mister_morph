package consolecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/proaccount"
)

type proLoginSession struct {
	DeviceCode proaccount.DeviceCode
	ExpiresAt  time.Time
}

type proLoginStore struct {
	mu       sync.Mutex
	sessions map[string]proLoginSession
}

func newProLoginStore() *proLoginStore {
	return &proLoginStore{sessions: map[string]proLoginSession{}}
}

func (s *proLoginStore) Create(code proaccount.DeviceCode) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil Pro login store")
	}
	id, err := randomOpaqueID()
	if err != nil {
		return "", err
	}
	expiresAt := code.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(10 * time.Minute)
	}
	s.mu.Lock()
	s.pruneLocked(time.Now().UTC())
	s.sessions[id] = proLoginSession{
		DeviceCode: code,
		ExpiresAt:  expiresAt,
	}
	s.mu.Unlock()
	return id, nil
}

func (s *proLoginStore) Get(id string) (proLoginSession, bool) {
	if s == nil {
		return proLoginSession{}, false
	}
	id = strings.TrimSpace(id)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	item, ok := s.sessions[id]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.sessions, id)
		return proLoginSession{}, false
	}
	return item, true
}

func (s *proLoginStore) SlowDown(id string) (proLoginSession, bool) {
	if s == nil {
		return proLoginSession{}, false
	}
	id = strings.TrimSpace(id)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	item, ok := s.sessions[id]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.sessions, id)
		return proLoginSession{}, false
	}
	item.DeviceCode.Interval += proaccount.SlowDownIncrement
	s.sessions[id] = item
	return item, true
}

func (s *proLoginStore) Delete(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, strings.TrimSpace(id))
	s.mu.Unlock()
}

func (s *proLoginStore) pruneLocked(now time.Time) {
	for id, item := range s.sessions {
		if !item.ExpiresAt.After(now) {
			delete(s.sessions, id)
		}
	}
}

func (s *server) handleProAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, _, err := proaccount.ResolveSession(
		r.Context(),
		s.cfg.stateDir,
		proaccount.DefaultOAuthConfigValue(),
		proaccount.DefaultRouterConfigValue(),
	); err != nil {
		writeError(w, http.StatusBadGateway, "failed to refresh Pro account session")
		return
	}
	writeJSON(w, http.StatusOK, proaccount.ReadStatus(s.cfg.stateDir, time.Now().UTC()))
}

func (s *server) handleProAuthLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deviceCode, err := proaccount.RequestDeviceCode(r.Context(), proaccount.DefaultOAuthConfigValue())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sessionID, err := s.proLogins.Create(deviceCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create Pro login session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"session_id":                sessionID,
		"verification_url":          deviceCode.VerificationURL,
		"verification_url_complete": deviceCode.VerificationURLComplete,
		"user_code":                 deviceCode.UserCode,
		"expires_at":                deviceCode.ExpiresAt.Format(time.RFC3339),
		"interval_seconds":          int(deviceCode.Interval.Seconds()),
	})
}

func (s *server) handleProAuthLoginPoll(w http.ResponseWriter, r *http.Request) {
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
	session, ok := s.proLogins.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "Pro login session not found or expired")
		return
	}
	stored, err := proaccount.CompleteDeviceCodeLogin(
		r.Context(),
		proaccount.DefaultOAuthConfigValue(),
		proaccount.DefaultRouterConfigValue(),
		session.DeviceCode,
	)
	if proaccount.IsAuthorizationPending(err) {
		writeProAuthPending(w, session.DeviceCode.Interval)
		return
	}
	if proaccount.IsSlowDown(err) {
		if updated, ok := s.proLogins.SlowDown(sessionID); ok {
			writeProAuthPending(w, updated.DeviceCode.Interval)
			return
		}
		writeError(w, http.StatusNotFound, "Pro login session not found or expired")
		return
	}
	if proaccount.IsDeviceCodeExpired(err) {
		s.proLogins.Delete(sessionID)
		writeError(w, http.StatusBadRequest, "Pro OAuth device code expired")
		return
	}
	if proaccount.IsAccessDenied(err) {
		s.proLogins.Delete(sessionID)
		writeError(w, http.StatusForbidden, "Pro OAuth authorization denied")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := proaccount.WriteSession(s.cfg.stateDir, stored); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save Pro account session")
		return
	}
	s.proLogins.Delete(sessionID)
	settingsUpdated := false
	if req.SetDefault {
		if err := s.setProAsDefaultLLM(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settingsUpdated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"pending":          false,
		"settings_updated": settingsUpdated,
		"status":           proaccount.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	})
}

func writeProAuthPending(w http.ResponseWriter, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"pending":          true,
		"interval_seconds": int(interval.Seconds()),
	})
}

func (s *server) setProAsDefaultLLM() error {
	inferenceProvider := llmutil.InferenceProviderMisterMorphPro
	model := proaccount.DefaultModel
	empty := ""
	update := agentsettings.AgentSettingsUpdate{
		LLM: agentsettings.LLMSettingsUpdate{
			LLMConfigFieldsUpdate: agentsettings.LLMConfigFieldsUpdate{
				InferenceProvider:   &inferenceProvider,
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

func (s *server) handleProAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	removed, err := proaccount.DeleteSession(s.cfg.stateDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"status":  proaccount.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	})
}
