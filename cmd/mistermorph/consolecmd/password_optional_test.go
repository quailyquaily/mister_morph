package consolecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadServeConfigAllowEmptyPasswordFlag(t *testing.T) {
	cmd := newServeCmd()
	if err := cmd.Flags().Set("allow-empty-password", "true"); err != nil {
		t.Fatalf("set allow-empty-password flag: %v", err)
	}

	cfg, err := loadServeConfig(cmd)
	if err != nil {
		t.Fatalf("loadServeConfig() error = %v", err)
	}
	if !cfg.passwordOptional {
		t.Fatalf("cfg.passwordOptional = false, want true")
	}
}

func TestWithAuthBypassesWhenPasswordIsOptionalAndUnset(t *testing.T) {
	srv := &server{
		cfg: serveConfig{
			passwordOptional: true,
			sessionTTL:       time.Hour,
		},
		sessions: newSessionStore(""),
	}

	called := false
	handler := srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !called {
		t.Fatalf("wrapped handler was not called")
	}
}

func TestWithAuthStillRequiresTokenWhenPasswordConfigured(t *testing.T) {
	srv := &server{
		cfg: serveConfig{
			passwordOptional: true,
			password:         "secret",
			sessionTTL:       time.Hour,
		},
		sessions: newSessionStore(""),
	}

	handler := srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleLoginReturnsSessionWhenPasswordIsOptionalAndUnset(t *testing.T) {
	srv := &server{
		cfg: serveConfig{
			passwordOptional: true,
			sessionTTL:       time.Hour,
		},
		sessions: newSessionStore(""),
		limiter:  newLoginLimiter(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.handleLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		t.Fatalf("access_token is empty")
	}
}

func TestHandleAuthConfigReportsPasswordRequirement(t *testing.T) {
	t.Run("password_not_required", func(t *testing.T) {
		srv := &server{cfg: serveConfig{passwordOptional: true}}
		req := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
		rec := httptest.NewRecorder()
		srv.handleAuthConfig(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			PasswordRequired bool `json:"password_required"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if payload.PasswordRequired {
			t.Fatalf("password_required = true, want false")
		}
	})

	t.Run("password_required", func(t *testing.T) {
		srv := &server{cfg: serveConfig{passwordOptional: true, password: "secret"}}
		req := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
		rec := httptest.NewRecorder()
		srv.handleAuthConfig(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			PasswordRequired bool `json:"password_required"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !payload.PasswordRequired {
			t.Fatalf("password_required = false, want true")
		}
	})
}
