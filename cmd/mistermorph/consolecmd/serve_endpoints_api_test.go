package consolecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleEndpointTest(t *testing.T) {
	runtimeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mode":"serve"}`))
			return
		case "/overview":
			if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer good-token" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	t.Cleanup(runtimeSrv.Close)

	s := &server{}

	t.Run("success", func(t *testing.T) {
		reqBody := map[string]any{
			"url":        runtimeSrv.URL,
			"auth_token": "good-token",
		}
		raw, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/console/api/endpoints/test", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		s.handleEndpointTest(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Connected bool   `json:"connected"`
			Mode      string `json:"mode"`
			Detail    string `json:"detail"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !payload.Connected {
			t.Fatalf("connected = false, detail = %q", payload.Detail)
		}
		if payload.Mode != "serve" {
			t.Fatalf("mode = %q, want serve", payload.Mode)
		}
	})

	t.Run("bad token", func(t *testing.T) {
		reqBody := map[string]any{
			"url":        runtimeSrv.URL,
			"auth_token": "bad-token",
		}
		raw, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/console/api/endpoints/test", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		s.handleEndpointTest(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Connected bool   `json:"connected"`
			Detail    string `json:"detail"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Connected {
			t.Fatalf("connected = true, expected false")
		}
		if !strings.Contains(payload.Detail, "401") {
			t.Fatalf("detail = %q, want HTTP 401 detail", payload.Detail)
		}
	})
}

func TestHandleEndpointCreateAndEdit(t *testing.T) {
	runtimeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mode":"serve"}`))
			return
		case "/overview":
			if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer good-token" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	t.Cleanup(runtimeSrv.Close)

	s := &server{
		cfg:           serveConfig{basePath: "/console"},
		endpoints:     []runtimeEndpoint{},
		endpointByRef: map[string]runtimeEndpoint{},
	}

	createBody := map[string]any{
		"name":       "Main",
		"url":        runtimeSrv.URL,
		"auth_token": "good-token",
	}
	createRaw, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/console/api/endpoints", bytes.NewReader(createRaw))
	createRec := httptest.NewRecorder()
	s.handleEndpoints(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (%s)", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	if len(s.endpoints) != 1 {
		t.Fatalf("len(endpoints) = %d, want 1", len(s.endpoints))
	}
	ref := s.endpoints[0].Ref
	if ref == "" {
		t.Fatalf("created endpoint ref is empty")
	}

	dupReq := httptest.NewRequest(http.MethodPost, "/console/api/endpoints", bytes.NewReader(createRaw))
	dupRec := httptest.NewRecorder()
	s.handleEndpoints(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want %d", dupRec.Code, http.StatusConflict)
	}

	editBody := map[string]any{
		"name":       "Main Updated",
		"url":        runtimeSrv.URL,
		"auth_token": "good-token",
	}
	editRaw, _ := json.Marshal(editBody)
	editReq := httptest.NewRequest(http.MethodPut, "/console/api/endpoints/"+ref, bytes.NewReader(editRaw))
	editRec := httptest.NewRecorder()
	s.handleEndpointByRef(editRec, editReq)
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit status = %d, want %d (%s)", editRec.Code, http.StatusOK, editRec.Body.String())
	}
	if got := s.endpointByRef[ref].Name; got != "Main Updated" {
		t.Fatalf("endpoint name = %q, want Main Updated", got)
	}
}
