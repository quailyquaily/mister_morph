package daemonruntime

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/filecache"
)

func (routes *routeRegistration) registerWorkspaceRoutes() {
	mux := routes.mux
	opts := routes.options.Workspace
	authToken := routes.authToken
	capturedPaths := routes.paths
	fileCacheLimits := routes.fileCacheLimits
	workspaceGet := opts.Get
	workspacePut := opts.Put
	workspaceDelete := opts.Delete
	workspaceDefaultDir := strings.TrimSpace(opts.DefaultDir)
	workspaceOpen := opts.Open
	workspaceTree := opts.Tree
	workspaceBrowse := opts.Browse
	workspaceCreateDir := opts.CreateDir
	var consoleCacheMu sync.Mutex

	mux.HandleFunc("/workspace", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		writeWorkspaceResponse := func(topicID string, resolution WorkspaceResolution) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"topic_id":      strings.TrimSpace(topicID),
				"workspace_dir": strings.TrimSpace(resolution.WorkspaceDir),
				"source":        strings.TrimSpace(resolution.Source),
			})
		}
		handleWorkspaceError := func(err error) {
			if err == nil {
				return
			}
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		}

		switch r.Method {
		case http.MethodGet:
			if workspaceGet == nil {
				http.Error(w, "workspace is unavailable", http.StatusServiceUnavailable)
				return
			}
			topicID := strings.TrimSpace(r.URL.Query().Get("topic_id"))
			if topicID == "" {
				http.Error(w, "topic_id is required", http.StatusBadRequest)
				return
			}
			resolution, err := workspaceGet(r.Context(), topicID)
			if err != nil {
				handleWorkspaceError(err)
				return
			}
			writeWorkspaceResponse(topicID, resolution)
			return

		case http.MethodPut:
			if workspacePut == nil {
				http.Error(w, "workspace is unavailable", http.StatusServiceUnavailable)
				return
			}
			var req struct {
				TopicID      string `json:"topic_id"`
				WorkspaceDir string `json:"workspace_dir"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			req.TopicID = strings.TrimSpace(req.TopicID)
			if req.TopicID == "" {
				http.Error(w, "topic_id is required", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(req.WorkspaceDir) == "" {
				http.Error(w, "workspace_dir is required", http.StatusBadRequest)
				return
			}
			resolution, err := workspacePut(r.Context(), req.TopicID, req.WorkspaceDir)
			if err != nil {
				handleWorkspaceError(err)
				return
			}
			writeWorkspaceResponse(req.TopicID, resolution)
			return

		case http.MethodDelete:
			if workspaceDelete == nil {
				http.Error(w, "workspace is unavailable", http.StatusServiceUnavailable)
				return
			}
			topicID := strings.TrimSpace(r.URL.Query().Get("topic_id"))
			if topicID == "" {
				http.Error(w, "topic_id is required", http.StatusBadRequest)
				return
			}
			resolution, err := workspaceDelete(r.Context(), topicID)
			if err != nil {
				handleWorkspaceError(err)
				return
			}
			writeWorkspaceResponse(topicID, resolution)
			return

		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	mux.HandleFunc("/workspace/tree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if workspaceTree == nil {
			http.Error(w, "workspace tree is unavailable", http.StatusServiceUnavailable)
			return
		}
		topicID := strings.TrimSpace(r.URL.Query().Get("topic_id"))
		if topicID == "" {
			http.Error(w, "topic_id is required", http.StatusBadRequest)
			return
		}
		treePath := strings.TrimSpace(r.URL.Query().Get("path"))
		payload, err := workspaceTree(r.Context(), topicID, treePath)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/workspace/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if workspaceOpen == nil {
			http.Error(w, "workspace open is unavailable", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			TopicID string `json:"topic_id"`
			Path    string `json:"path"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.TopicID = strings.TrimSpace(req.TopicID)
		req.Path = strings.TrimSpace(req.Path)
		if req.TopicID == "" {
			http.Error(w, "topic_id is required", http.StatusBadRequest)
			return
		}
		if req.Path == "" {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}
		if err := workspaceOpen(r.Context(), req.TopicID, req.Path); err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	mux.HandleFunc("/files/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		filePath, err := resolveFilesDownloadPath(
			r.Context(),
			workspaceGet,
			capturedPaths,
			r.URL.Query().Get("dir_name"),
			r.URL.Query().Get("topic_id"),
			r.URL.Query().Get("path"),
		)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		serveFileDownload(w, r, filePath)
	})

	mux.HandleFunc("/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, fileUploadMaxBytes)
		if err := r.ParseMultipartForm(fileUploadMemoryBytes); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "upload exceeds 64 MiB", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		fileHeaders := r.MultipartForm.File["files"]
		if len(fileHeaders) == 0 {
			http.Error(w, "files are required", http.StatusBadRequest)
			return
		}

		rootDir, dirName, err := resolveFilesUploadRoot(
			r.Context(),
			workspaceGet,
			capturedPaths,
			r.FormValue("topic_id"),
			workspaceDefaultDir,
			r.FormValue("workspace_dir"),
		)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		if dirName == "file_cache_dir" {
			consoleCacheMu.Lock()
			defer consoleCacheMu.Unlock()
		}

		files := make([]uploadedFile, 0, len(fileHeaders))
		createdPaths := make([]string, 0, len(fileHeaders))
		committed := false
		defer func() {
			if committed {
				return
			}
			for _, createdPath := range createdPaths {
				_ = os.Remove(createdPath)
			}
		}()
		for _, header := range fileHeaders {
			src, err := header.Open()
			if err != nil {
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusBadRequest)
				return
			}
			uploaded, saveErr := saveUploadedFile(rootDir, header.Filename, src)
			_ = src.Close()
			if saveErr != nil {
				if msg, ok := badRequestMessage(saveErr); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(saveErr.Error()), http.StatusServiceUnavailable)
				return
			}
			uploaded.DirName = dirName
			createdPath := filepath.Join(rootDir, uploaded.Name)
			createdPaths = append(createdPaths, createdPath)
			if dirName == "file_cache_dir" {
				uploaded.Path = filepath.ToSlash(filepath.Join("console", uploaded.Path))
			}
			files = append(files, uploaded)
		}
		if dirName == "file_cache_dir" {
			protected := make(map[string]bool, len(createdPaths))
			for _, createdPath := range createdPaths {
				protected[createdPath] = true
			}
			if err := filecache.Cleanup(rootDir, fileCacheLimits, protected); err != nil {
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"files": files}); err != nil {
			return
		}
		committed = true
	})

	mux.HandleFunc("/files/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		filePath, err := resolveFilesDownloadPath(
			r.Context(),
			workspaceGet,
			capturedPaths,
			r.URL.Query().Get("dir_name"),
			r.URL.Query().Get("topic_id"),
			r.URL.Query().Get("path"),
		)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		serveFilePreview(w, r, filePath)
	})

	mux.HandleFunc("/workspace/browse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if workspaceBrowse == nil {
			http.Error(w, "workspace browser is unavailable", http.StatusServiceUnavailable)
			return
		}
		treePath := strings.TrimSpace(r.URL.Query().Get("path"))
		showHiddenValue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("show_hidden")))
		showHidden := showHiddenValue == "1" || showHiddenValue == "true" || showHiddenValue == "yes" || showHiddenValue == "on"
		payload, err := workspaceBrowse(r.Context(), treePath, showHidden)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		stateDir := strings.TrimSpace(capturedPaths.StateDir)
		if stateDir != "" {
			payload.StateDir = filepath.Clean(stateDir)
		}
		cacheDir := strings.TrimSpace(capturedPaths.CacheDir)
		if cacheDir != "" {
			payload.CacheDir = filepath.Clean(cacheDir)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/workspace/directory", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if workspaceCreateDir == nil {
			http.Error(w, "workspace directory creation is unavailable", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			ParentPath string `json:"parent_path"`
			Name       string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.ParentPath = strings.TrimSpace(req.ParentPath)
		req.Name = strings.TrimSpace(req.Name)
		if req.ParentPath == "" {
			http.Error(w, "parent_path is required", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		createdPath, err := workspaceCreateDir(r.Context(), req.ParentPath, req.Name)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"path": createdPath})
	})

}
