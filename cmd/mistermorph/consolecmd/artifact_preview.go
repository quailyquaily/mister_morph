package consolecmd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const artifactPreviewTicketTTL = 5 * time.Minute

type artifactPreviewRequest struct {
	EndpointRef string `json:"endpoint_ref"`
	DirName     string `json:"dir_name"`
	TopicID     string `json:"topic_id"`
	Path        string `json:"path"`
}

type artifactPreviewRenewRequest struct {
	Ticket string `json:"ticket"`
}

type artifactPreviewTicket struct {
	EndpointRef string
	DirName     string
	TopicID     string
	EntryPath   string
	EntryDir    string
	ExpiresAt   time.Time
}

type artifactPreviewStore struct {
	mu      sync.Mutex
	tickets map[string]artifactPreviewTicket
}

func newArtifactPreviewStore() *artifactPreviewStore {
	return &artifactPreviewStore{
		tickets: map[string]artifactPreviewTicket{},
	}
}

func (s *artifactPreviewStore) Create(item artifactPreviewTicket, ttl time.Duration) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("nil artifact preview store")
	}
	if ttl <= 0 {
		ttl = artifactPreviewTicketTTL
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	ticket := base64.RawURLEncoding.EncodeToString(buf)
	item.ExpiresAt = time.Now().UTC().Add(ttl)

	s.mu.Lock()
	s.pruneExpiredLocked(time.Now().UTC())
	s.tickets[ticket] = item
	s.mu.Unlock()

	return ticket, item.ExpiresAt, nil
}

func (s *artifactPreviewStore) Validate(ticket string) (artifactPreviewTicket, bool) {
	if s == nil {
		return artifactPreviewTicket{}, false
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return artifactPreviewTicket{}, false
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	item, ok := s.tickets[ticket]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.tickets, ticket)
		return artifactPreviewTicket{}, false
	}
	return item, true
}

func (s *artifactPreviewStore) Renew(ticket string, ttl time.Duration) (artifactPreviewTicket, time.Time, bool) {
	if s == nil {
		return artifactPreviewTicket{}, time.Time{}, false
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return artifactPreviewTicket{}, time.Time{}, false
	}
	if ttl <= 0 {
		ttl = artifactPreviewTicketTTL
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	item, ok := s.tickets[ticket]
	if !ok || !item.ExpiresAt.After(now) {
		delete(s.tickets, ticket)
		return artifactPreviewTicket{}, time.Time{}, false
	}
	item.ExpiresAt = now.Add(ttl)
	s.tickets[ticket] = item
	return item, item.ExpiresAt, true
}

func (s *artifactPreviewStore) pruneExpiredLocked(now time.Time) {
	for key, item := range s.tickets {
		if !item.ExpiresAt.After(now) {
			delete(s.tickets, key)
		}
	}
}

func (s *server) handleArtifactPreviewTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.artifactPreviews == nil {
		writeError(w, http.StatusServiceUnavailable, "artifact preview is unavailable")
		return
	}

	var req artifactPreviewRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	item, err := s.buildArtifactPreviewTicket(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ticket, expiresAt, err := s.artifactPreviews.Create(item, artifactPreviewTicketTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create preview ticket")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"entry_url":  artifactPreviewEntryURL(s.cfg.basePath, ticket, item.EntryPath),
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	})
}

func (s *server) handleArtifactPreviewTicketRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.artifactPreviews == nil {
		writeError(w, http.StatusServiceUnavailable, "artifact preview is unavailable")
		return
	}

	var req artifactPreviewRenewRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ticket := strings.TrimSpace(req.Ticket)
	if ticket == "" {
		writeError(w, http.StatusBadRequest, "ticket is required")
		return
	}
	item, expiresAt, ok := s.artifactPreviews.Renew(ticket, artifactPreviewTicketTTL)
	if !ok {
		writeError(w, http.StatusUnauthorized, "preview ticket expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"entry_url":  artifactPreviewEntryURL(s.cfg.basePath, ticket, item.EntryPath),
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	})
}

func (s *server) buildArtifactPreviewTicket(req artifactPreviewRequest) (artifactPreviewTicket, error) {
	endpointRef := strings.TrimSpace(req.EndpointRef)
	if endpointRef == "" {
		return artifactPreviewTicket{}, fmt.Errorf("endpoint_ref is required")
	}
	if s == nil || s.endpointByRef == nil {
		return artifactPreviewTicket{}, fmt.Errorf("invalid endpoint")
	}
	if _, ok := s.endpointByRef[endpointRef]; !ok {
		return artifactPreviewTicket{}, fmt.Errorf("invalid endpoint")
	}

	dirName := strings.TrimSpace(req.DirName)
	switch dirName {
	case "workspace_dir", "file_state_dir", "file_cache_dir":
	default:
		return artifactPreviewTicket{}, fmt.Errorf("invalid dir_name")
	}
	topicID := strings.TrimSpace(req.TopicID)
	if dirName == "workspace_dir" && topicID == "" {
		return artifactPreviewTicket{}, fmt.Errorf("topic_id is required")
	}

	entryPath, err := cleanArtifactPreviewPath(req.Path)
	if err != nil {
		return artifactPreviewTicket{}, err
	}
	if !artifactPreviewEntryExtensionAllowed(entryPath) {
		return artifactPreviewTicket{}, fmt.Errorf("artifact entry must be an html file")
	}

	return artifactPreviewTicket{
		EndpointRef: endpointRef,
		DirName:     dirName,
		TopicID:     topicID,
		EntryPath:   entryPath,
		EntryDir:    path.Dir(entryPath),
	}, nil
}

func (s *server) handleArtifactPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.artifactPreviews == nil {
		writeError(w, http.StatusServiceUnavailable, "artifact preview is unavailable")
		return
	}
	ticket, assetPath, err := s.parseArtifactPreviewRequestPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, ok := s.artifactPreviews.Validate(ticket)
	if !ok {
		writeError(w, http.StatusUnauthorized, "preview ticket expired")
		return
	}
	assetPath, err = cleanArtifactPreviewPath(assetPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !artifactPreviewPathWithinEntryDir(item.EntryDir, assetPath) {
		writeError(w, http.StatusBadRequest, "preview path is outside the artifact directory")
		return
	}

	endpoint, ok := s.endpointByRef[item.EndpointRef]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid endpoint")
		return
	}
	download, err := endpoint.Client.Download(r.Context(), artifactPreviewRuntimePath(item, assetPath))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if download.Body != nil {
		defer download.Body.Close()
	}
	status := download.Status
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if status < 200 || status >= 300 {
		raw := []byte(nil)
		if download.Body != nil {
			raw, _ = io.ReadAll(io.LimitReader(download.Body, 1<<20))
		}
		writeJSONProxyResponse(w, status, raw)
		return
	}

	setNoCacheHeaders(w.Header())
	copyDownloadHeader(w.Header(), download.Header, "Content-Type")
	copyDownloadHeader(w.Header(), download.Header, "Content-Length")
	w.Header().Set("Content-Security-Policy", artifactPreviewCSP())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if download.Body != nil {
		_, _ = io.Copy(w, download.Body)
	}
}

func (s *server) parseArtifactPreviewRequestPath(r *http.Request) (string, string, error) {
	if s == nil || r == nil || r.URL == nil {
		return "", "", fmt.Errorf("invalid preview request")
	}
	prefix := joinBasePath(s.cfg.basePath, "/api/artifacts/preview/")
	rel := strings.TrimPrefix(r.URL.Path, prefix)
	if rel == r.URL.Path {
		const fallback = "/artifacts/preview/"
		idx := strings.Index(r.URL.Path, fallback)
		if idx < 0 {
			return "", "", fmt.Errorf("invalid preview request")
		}
		rel = strings.TrimPrefix(r.URL.Path[idx+len(fallback):], "/")
	}
	rel = strings.TrimLeft(rel, "/")
	parts := strings.SplitN(rel, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid preview request")
	}
	return strings.TrimSpace(parts[0]), parts[1], nil
}

func artifactPreviewRuntimePath(item artifactPreviewTicket, assetPath string) string {
	q := new(url.URL)
	params := q.Query()
	params.Set("dir_name", strings.TrimSpace(item.DirName))
	params.Set("path", strings.TrimSpace(assetPath))
	if strings.TrimSpace(item.TopicID) != "" {
		params.Set("topic_id", strings.TrimSpace(item.TopicID))
	}
	return "/files/preview?" + params.Encode()
}

func artifactPreviewEntryURL(basePath string, ticket string, entryPath string) string {
	prefix := joinBasePath(basePath, "/api/artifacts/preview")
	return strings.TrimRight(prefix, "/") + "/" + url.PathEscape(strings.TrimSpace(ticket)) + "/" + escapedPathSegments(entryPath)
}

func escapedPathSegments(rawPath string) string {
	clean := strings.Trim(strings.TrimSpace(path.Clean("/"+rawPath)), "/")
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func cleanArtifactPreviewPath(rawPath string) (string, error) {
	rawPath = strings.TrimSpace(strings.ReplaceAll(rawPath, "\\", "/"))
	if rawPath == "" || rawPath == "." {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(rawPath, "/") || filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("path must be relative")
	}
	clean := strings.Trim(path.Clean("/"+rawPath), "/")
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path is outside the requested directory")
	}
	return clean, nil
}

func artifactPreviewEntryExtensionAllowed(filePath string) bool {
	switch strings.ToLower(path.Ext(strings.TrimSpace(filePath))) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func artifactPreviewPathWithinEntryDir(entryDir string, assetPath string) bool {
	entryDir = strings.Trim(strings.TrimSpace(path.Clean("/"+entryDir)), "/")
	assetPath = strings.Trim(strings.TrimSpace(path.Clean("/"+assetPath)), "/")
	if assetPath == "" {
		return false
	}
	if entryDir == "." {
		entryDir = ""
	}
	if entryDir == "" {
		return true
	}
	return assetPath == entryDir || strings.HasPrefix(assetPath, entryDir+"/")
}

func artifactPreviewCSP() string {
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self' 'unsafe-inline' blob: data:",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'none'",
		"frame-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
	}, "; ")
}
