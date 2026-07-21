package daemonruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMemoryFileID(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantID    string
		wantGroup string
		wantDate  string
		wantOK    bool
	}{
		{
			name:      "long_term_index",
			raw:       "index.md",
			wantID:    "index.md",
			wantGroup: "long_term",
			wantOK:    true,
		},
		{
			name:      "short_term_encoded",
			raw:       "2026-02-24%2Ftg_123.md",
			wantID:    "2026-02-24/tg_123.md",
			wantGroup: "short_term",
			wantDate:  "2026-02-24",
			wantOK:    true,
		},
		{
			name:   "reject_parent_dir",
			raw:    "..%2Findex.md",
			wantOK: false,
		},
		{
			name:   "reject_wrong_top_file",
			raw:    "other.md",
			wantOK: false,
		},
		{
			name:   "reject_invalid_day",
			raw:    "2026-02-31/a.md",
			wantOK: false,
		},
		{
			name:   "reject_nested_path",
			raw:    "2026-02-24%2Fa%2Fb.md",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMemoryFileID(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got=%#v)", ok, tc.wantOK, got)
			}
			if !tc.wantOK {
				return
			}
			if got.ID != tc.wantID {
				t.Fatalf("id = %q, want %q", got.ID, tc.wantID)
			}
			if got.Group != tc.wantGroup {
				t.Fatalf("group = %q, want %q", got.Group, tc.wantGroup)
			}
			if got.Date != tc.wantDate {
				t.Fatalf("date = %q, want %q", got.Date, tc.wantDate)
			}
		})
	}
}

func TestMemoryFilesRoutes(t *testing.T) {
	stateDir := t.TempDir()

	memoryDir := filepath.Join(stateDir, "memory")
	shortDir := filepath.Join(memoryDir, "2026-02-24")
	if err := os.MkdirAll(shortDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", shortDir, err)
	}
	indexPath := filepath.Join(memoryDir, "index.md")
	shortPath := filepath.Join(shortDir, "tg_123.md")
	if err := os.WriteFile(indexPath, []byte("long"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", indexPath, err)
	}
	if err := os.WriteFile(shortPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", shortPath, err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/memory/files", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}

		var payload struct {
			DefaultID string           `json:"default_id"`
			Items     []memoryFileSpec `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if payload.DefaultID != "index.md" {
			t.Fatalf("default_id = %q, want index.md", payload.DefaultID)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(payload.Items))
		}
		if payload.Items[0].ID != "index.md" {
			t.Fatalf("first item id = %q, want index.md", payload.Items[0].ID)
		}
	})

	t.Run("get_short_term", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/memory/files/2026-02-24%2Ftg_123.md", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			ID      string `json:"id"`
			Group   string `json:"group"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if payload.ID != "2026-02-24/tg_123.md" {
			t.Fatalf("id = %q, want 2026-02-24/tg_123.md", payload.ID)
		}
		if payload.Group != "short_term" {
			t.Fatalf("group = %q, want short_term", payload.Group)
		}
		if payload.Content != "short" {
			t.Fatalf("content = %q, want short", payload.Content)
		}
	})

	t.Run("put_short_term_existing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/memory/files/2026-02-24%2Ftg_123.md", bytes.NewBufferString(`{"content":"short-updated"}`))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		raw, err := os.ReadFile(shortPath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", shortPath, err)
		}
		if string(raw) != "short-updated" {
			t.Fatalf("short file = %q, want short-updated", string(raw))
		}
	})

	t.Run("put_short_term_missing_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/memory/files/2026-02-24%2Ftg_missing.md", bytes.NewBufferString(`{"content":"x"}`))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("put_index_can_create", func(t *testing.T) {
		if err := os.Remove(indexPath); err != nil {
			t.Fatalf("Remove(%s): %v", indexPath, err)
		}
		req := httptest.NewRequest(http.MethodPut, "/memory/files/index.md", bytes.NewBufferString(`{"content":"long-new"}`))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		raw, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", indexPath, err)
		}
		if string(raw) != "long-new" {
			t.Fatalf("index file = %q, want long-new", string(raw))
		}
	})

	t.Run("invalid_path_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/memory/files/..%2Fhack.md", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
}
