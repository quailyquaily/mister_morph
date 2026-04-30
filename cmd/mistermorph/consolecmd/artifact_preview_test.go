package consolecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHandleArtifactPreviewTicket(t *testing.T) {
	s := &server{
		cfg:              serveConfig{basePath: "/console"},
		artifactPreviews: newArtifactPreviewStore(),
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {Ref: "ep_main", Client: &stubRuntimeEndpointClient{}},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/console/api/artifacts/preview-ticket", strings.NewReader(`{
		"endpoint_ref": "ep_main",
		"dir_name": "workspace_dir",
		"topic_id": "topic_a",
		"path": "demos/todo/index.html"
	}`))
	rec := httptest.NewRecorder()
	s.handleArtifactPreviewTicket(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Ticket   string `json:"ticket"`
		EntryURL string `json:"entry_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if strings.TrimSpace(payload.Ticket) == "" {
		t.Fatalf("ticket is empty")
	}
	if !strings.HasPrefix(payload.EntryURL, "/console/api/artifacts/preview/"+payload.Ticket+"/demos/todo/index.html") {
		t.Fatalf("entry_url = %q", payload.EntryURL)
	}
}

func TestHandleArtifactPreviewTicketAcceptsNonIndexHTMLEntry(t *testing.T) {
	s := &server{
		cfg:              serveConfig{basePath: "/console"},
		artifactPreviews: newArtifactPreviewStore(),
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {Ref: "ep_main", Client: &stubRuntimeEndpointClient{}},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/console/api/artifacts/preview-ticket", strings.NewReader(`{
		"endpoint_ref": "ep_main",
		"dir_name": "workspace_dir",
		"topic_id": "topic_a",
		"path": "profile.html"
	}`))
	rec := httptest.NewRecorder()
	s.handleArtifactPreviewTicket(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Ticket   string `json:"ticket"`
		EntryURL string `json:"entry_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !strings.HasPrefix(payload.EntryURL, "/console/api/artifacts/preview/"+payload.Ticket+"/profile.html") {
		t.Fatalf("entry_url = %q", payload.EntryURL)
	}
}

func TestHandleArtifactPreviewTicketRejectsMissingDirName(t *testing.T) {
	s := &server{
		cfg:              serveConfig{basePath: "/console"},
		artifactPreviews: newArtifactPreviewStore(),
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {Ref: "ep_main", Client: &stubRuntimeEndpointClient{}},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/console/api/artifacts/preview-ticket", strings.NewReader(`{
		"endpoint_ref": "ep_main",
		"topic_id": "topic_a",
		"path": "profile.html"
	}`))
	rec := httptest.NewRecorder()
	s.handleArtifactPreviewTicket(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleArtifactPreviewTicketRenew(t *testing.T) {
	oldExpiresAt := time.Now().UTC().Add(time.Minute)
	s := &server{
		cfg: serveConfig{basePath: "/console"},
		artifactPreviews: &artifactPreviewStore{
			tickets: map[string]artifactPreviewTicket{
				"ticket_a": {
					EndpointRef: "ep_main",
					DirName:     "workspace_dir",
					TopicID:     "topic_a",
					EntryPath:   "demos/todo/index.html",
					EntryDir:    "demos/todo",
					ExpiresAt:   oldExpiresAt,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/console/api/artifacts/preview-ticket/renew", strings.NewReader(`{
		"ticket": "ticket_a"
	}`))
	rec := httptest.NewRecorder()
	s.handleArtifactPreviewTicketRenew(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Ticket    string `json:"ticket"`
		EntryURL  string `json:"entry_url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Ticket != "ticket_a" {
		t.Fatalf("ticket = %q, want ticket_a", payload.Ticket)
	}
	if !strings.HasPrefix(payload.EntryURL, "/console/api/artifacts/preview/ticket_a/demos/todo/index.html") {
		t.Fatalf("entry_url = %q", payload.EntryURL)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
	if err != nil {
		t.Fatalf("Parse(expires_at) error = %v", err)
	}
	if !expiresAt.After(oldExpiresAt) {
		t.Fatalf("expires_at = %s, want after %s", expiresAt.Format(time.RFC3339Nano), oldExpiresAt.Format(time.RFC3339Nano))
	}
}

func TestHandleArtifactPreviewTicketRenewRejectsMissingTicket(t *testing.T) {
	s := &server{
		cfg:              serveConfig{basePath: "/console"},
		artifactPreviews: newArtifactPreviewStore(),
	}

	req := httptest.NewRequest(http.MethodPost, "/console/api/artifacts/preview-ticket/renew", strings.NewReader(`{
		"ticket": "missing"
	}`))
	rec := httptest.NewRecorder()
	s.handleArtifactPreviewTicketRenew(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleArtifactPreviewProxiesPreviewFile(t *testing.T) {
	client := &stubRuntimeEndpointClient{
		downloadStatus: http.StatusOK,
		downloadHeader: http.Header{
			"Content-Type": []string{"text/javascript; charset=utf-8"},
		},
		downloadRaw: []byte("console.log('ok')"),
	}
	s := &server{
		cfg: serveConfig{basePath: "/console"},
		artifactPreviews: &artifactPreviewStore{
			tickets: map[string]artifactPreviewTicket{
				"ticket_a": {
					EndpointRef: "ep_main",
					DirName:     "workspace_dir",
					TopicID:     "topic_a",
					EntryPath:   "demos/todo/index.html",
					EntryDir:    "demos/todo",
					ExpiresAt:   time.Now().UTC().Add(time.Minute),
				},
			},
		},
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {Ref: "ep_main", Client: client},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console/api/artifacts/preview/ticket_a/demos/todo/app.js", nil)
	rec := httptest.NewRecorder()
	s.handleArtifactPreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "console.log('ok')" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'none'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	u, err := url.Parse(client.lastDownloadPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if u.Path != "/files/preview" {
		t.Fatalf("runtime path = %q", u.Path)
	}
	q := u.Query()
	if q.Get("dir_name") != "workspace_dir" || q.Get("topic_id") != "topic_a" || q.Get("path") != "demos/todo/app.js" {
		t.Fatalf("runtime query = %q", u.RawQuery)
	}
}

func TestHandleArtifactPreviewRejectsPathOutsideEntryDir(t *testing.T) {
	client := &stubRuntimeEndpointClient{}
	s := &server{
		cfg: serveConfig{basePath: "/console"},
		artifactPreviews: &artifactPreviewStore{
			tickets: map[string]artifactPreviewTicket{
				"ticket_a": {
					EndpointRef: "ep_main",
					DirName:     "workspace_dir",
					TopicID:     "topic_a",
					EntryPath:   "demos/todo/index.html",
					EntryDir:    "demos/todo",
					ExpiresAt:   time.Now().UTC().Add(time.Minute),
				},
			},
		},
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {Ref: "ep_main", Client: client},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console/api/artifacts/preview/ticket_a/demos/shared.js", nil)
	rec := httptest.NewRecorder()
	s.handleArtifactPreview(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if client.lastDownloadPath != "" {
		t.Fatalf("client download path = %q, want empty", client.lastDownloadPath)
	}
}

func TestArtifactPreviewPathWithinRootEntryDir(t *testing.T) {
	if !artifactPreviewPathWithinEntryDir(".", "app..bundle.js") {
		t.Fatalf("root entry dir should allow clean root asset names")
	}
	if !artifactPreviewPathWithinEntryDir(".", "assets/app.js") {
		t.Fatalf("root entry dir should allow nested clean asset paths")
	}
}

func TestConsoleArtifactPreviewPromptBlockWithWorkspace(t *testing.T) {
	block, err := consoleArtifactPreviewPromptBlock("/project")
	if err != nil {
		t.Fatalf("consoleArtifactPreviewPromptBlock() error = %v", err)
	}
	content := strings.TrimSpace(block.Content)
	for _, want := range []string{
		"```artifact",
		"path=path/to/profile.html",
		"dir_name=workspace_dir|file_cache_dir|file_state_dir",
		"`dir_name` selects one allowed root: `workspace_dir`, `file_cache_dir`, `file_state_dir`",
		"Only include it after the HTML entry file exists",
		"Use the actual HTML path you created",
		"Do not overwrite an existing preview file",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, content)
		}
	}
}

func TestConsoleArtifactPreviewPromptBlockWithoutWorkspace(t *testing.T) {
	block, err := consoleArtifactPreviewPromptBlock("")
	if err != nil {
		t.Fatalf("consoleArtifactPreviewPromptBlock() error = %v", err)
	}
	content := strings.TrimSpace(block.Content)
	for _, want := range []string{
		"```artifact",
		"path=path/to/profile.html",
		"dir_name=file_cache_dir|file_state_dir",
		"`dir_name` selects one allowed root: `file_cache_dir`, `file_state_dir`",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "workspace_dir") {
		t.Fatalf("prompt block should not mention workspace_dir without an attached workspace:\n%s", content)
	}
}
