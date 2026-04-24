package consolecmd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

type codexLoginSession struct {
	DeviceCode codexauth.DeviceCode
	ExpiresAt  time.Time
}

type codexLoginStore struct {
	mu       sync.Mutex
	sessions map[string]codexLoginSession
}

func newCodexLoginStore() *codexLoginStore {
	return &codexLoginStore{sessions: map[string]codexLoginSession{}}
}

func (s *codexLoginStore) Create(code codexauth.DeviceCode) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil codex login store")
	}
	id, err := randomOpaqueID()
	if err != nil {
		return "", err
	}
	expiresAt := code.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(15 * time.Minute)
	}
	s.mu.Lock()
	s.pruneLocked(time.Now().UTC())
	s.sessions[id] = codexLoginSession{
		DeviceCode: code,
		ExpiresAt:  expiresAt,
	}
	s.mu.Unlock()
	return id, nil
}

func (s *codexLoginStore) Get(id string) (codexLoginSession, bool) {
	if s == nil {
		return codexLoginSession{}, false
	}
	id = strings.TrimSpace(id)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	item, ok := s.sessions[id]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.sessions, id)
		return codexLoginSession{}, false
	}
	return item, true
}

func (s *codexLoginStore) Delete(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, strings.TrimSpace(id))
	s.mu.Unlock()
}

func (s *codexLoginStore) pruneLocked(now time.Time) {
	for id, item := range s.sessions {
		if !item.ExpiresAt.After(now) {
			delete(s.sessions, id)
		}
	}
}

func randomOpaqueID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *server) handleCodexAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, codexauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()))
}

func (s *server) handleCodexAuthLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deviceCode, err := codexauth.RequestDeviceCode(r.Context(), codexauth.DefaultOAuthConfigValue())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sessionID, err := s.codexLogins.Create(deviceCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create login session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"session_id":       sessionID,
		"verification_url": deviceCode.VerificationURL,
		"user_code":        deviceCode.UserCode,
		"expires_at":       deviceCode.ExpiresAt.Format(time.RFC3339),
		"interval_seconds": int(deviceCode.Interval.Seconds()),
	})
}

func (s *server) handleCodexAuthLoginPoll(w http.ResponseWriter, r *http.Request) {
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
	session, ok := s.codexLogins.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "codex login session not found or expired")
		return
	}
	token, err := codexauth.CompleteDeviceCodeLogin(r.Context(), codexauth.DefaultOAuthConfigValue(), session.DeviceCode)
	if codexauth.IsAuthorizationPending(err) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"pending": true,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := codexauth.WriteToken(s.cfg.stateDir, token); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save codex token")
		return
	}
	s.codexLogins.Delete(sessionID)
	settingsUpdated := false
	if req.SetDefault {
		if err := s.setCodexAsDefaultLLM(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settingsUpdated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"pending":          false,
		"settings_updated": settingsUpdated,
		"status":           codexauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	})
}

func (s *server) setCodexAsDefaultLLM() error {
	provider := codexauth.ProviderName
	model := codexauth.DefaultModel
	empty := ""
	update := llmSettingsUpdatePayload{
		llmConfigFieldsUpdatePayload: llmConfigFieldsUpdatePayload{
			Provider:            &provider,
			Model:               &model,
			Endpoint:            &empty,
			APIKey:              &empty,
			CloudflareAPIToken:  &empty,
			CloudflareAccountID: &empty,
		},
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		return err
	}
	serialized, err := writeAgentSettingsUpdate(configPath, agentSettingsUpdatePayload{LLM: update})
	if err != nil {
		return err
	}
	effectiveLLM := resolveAgentSettingsLLMFromReader(s.currentRuntimeConfigReader(), update)
	if _, err := validateAgentConfigDocument(serialized, effectiveLLM); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600})
}

func (s *server) handleCodexAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	removed, err := codexauth.DeleteToken(s.cfg.stateDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"status":  codexauth.ReadStatus(s.cfg.stateDir, time.Now().UTC()),
	})
}
