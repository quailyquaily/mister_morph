package daemonruntime

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/viper"
)

type SubmitFunc func(ctx context.Context, req SubmitTaskRequest) (SubmitTaskResponse, error)
type OverviewFunc func(ctx context.Context) (map[string]any, error)
type PokeFunc func(ctx context.Context, input PokeInput) error

var ErrPokeBusy = errors.New("poke already running")

type badRequestError struct {
	msg string
}

func (e badRequestError) Error() string {
	return strings.TrimSpace(e.msg)
}

func BadRequest(msg string) error {
	return badRequestError{msg: msg}
}

func badRequestMessage(err error) (string, bool) {
	var reqErr badRequestError
	if errors.As(err, &reqErr) {
		return strings.TrimSpace(reqErr.msg), true
	}
	return "", false
}

type RoutesOptions struct {
	Mode          string
	AuthToken     string
	TaskReader    TaskReader
	Submit        SubmitFunc
	Overview      OverviewFunc
	Poke          PokeFunc
	HealthEnabled bool
}

const (
	auditDefaultLineLimit int64 = 50
	auditMinLineLimit     int64 = 1
	auditMaxLineLimit     int64 = 500
	auditMaxCursorLines   int64 = 200 * 1000
	contactsMaxPageSize   int64 = 2000
	contactsMaxOffset     int64 = 200 * 1000
)

var (
	memoryDayPattern      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	memoryFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.md$`)
)

type auditFileItem struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
	Current   bool   `json:"current"`
}

type auditLogChunk struct {
	File        string   `json:"file"`
	Path        string   `json:"path"`
	Exists      bool     `json:"exists"`
	SizeBytes   int64    `json:"size_bytes"`
	Limit       int64    `json:"limit"`
	TotalLines  int64    `json:"total_lines"`
	TotalPages  int64    `json:"total_pages"`
	CurrentPage int64    `json:"current_page"`
	Before      int64    `json:"before"`
	From        int64    `json:"from"`
	To          int64    `json:"to"`
	HasOlder    bool     `json:"has_older"`
	Lines       []string `json:"lines"`
}

func RegisterRoutes(mux *http.ServeMux, opts RoutesOptions) {
	if mux == nil {
		return
	}
	mode := strings.TrimSpace(opts.Mode)
	startedAt := time.Now().UTC()
	authToken := strings.TrimSpace(opts.AuthToken)
	reader := opts.TaskReader
	submit := opts.Submit
	overview := opts.Overview
	poke := opts.Poke
	var pokeMu sync.RWMutex
	lastPokeAt := ""
	if overview == nil {
		overview = func(ctx context.Context) (map[string]any, error) {
			return buildDefaultOverviewPayload(mode, startedAt), nil
		}
	}

	if opts.HealthEnabled {
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead:
			default:
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			payload := map[string]any{
				"ok":   true,
				"time": time.Now().Format(time.RFC3339Nano),
			}
			if mode != "" {
				payload["mode"] = mode
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodHead {
				return
			}
			_ = json.NewEncoder(w).Encode(payload)
		})
	}

	mux.HandleFunc("/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		payload, err := overview(r.Context())
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		if payload == nil {
			payload = map[string]any{}
		}
		if _, ok := payload["health"]; !ok {
			payload["health"] = "ok"
		}
		if _, ok := payload["mode"]; !ok && mode != "" {
			payload["mode"] = mode
		}
		if _, ok := payload["started_at"]; !ok {
			payload["started_at"] = startedAt.Format(time.RFC3339)
		}
		if _, ok := payload["uptime_sec"]; !ok {
			payload["uptime_sec"] = int(time.Since(startedAt).Seconds())
		}
		pokeMu.RLock()
		currentLastPokeAt := lastPokeAt
		pokeMu.RUnlock()
		if strings.TrimSpace(currentLastPokeAt) != "" {
			payload["last_poke_at"] = currentLastPokeAt
		}
		if rawVersion, ok := payload["version"].(string); !ok || strings.TrimSpace(rawVersion) == "" {
			payload["version"] = buildVersion()
		}
		ensureRuntimeMetrics(payload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/poke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		input, err := readPokeInput(r)
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusBadRequest)
			return
		}
		if poke == nil {
			http.Error(w, "poke unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := poke(r.Context(), input); err != nil {
			if errors.Is(err, ErrPokeBusy) {
				http.Error(w, "heartbeat already running", http.StatusConflict)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		pokedAt := time.Now().UTC().Format(time.RFC3339Nano)
		pokeMu.Lock()
		lastPokeAt = pokedAt
		pokeMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"mode":     mode,
			"poked_at": pokedAt,
		})
	})

	mux.HandleFunc("/stats/llm/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		store := llmstats.NewProjectionStore(statepaths.LLMUsageJournalDir(), statepaths.LLMUsageProjectionPath())
		proj, err := store.Refresh()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"generated_at":      time.Now().UTC().Format(time.RFC3339),
			"updated_at":        proj.UpdatedAt,
			"projected_offset":  proj.ProjectedOffset,
			"projected_records": proj.ProjectedRecords,
			"skipped_records":   proj.SkippedRecords,
			"summary":           proj.Summary,
			"api_hosts":         proj.APIHosts,
			"models":            proj.Models,
		})
	})

	mux.HandleFunc("/system/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		checks := []map[string]any{
			{"id": "runtime_mode", "ok": strings.TrimSpace(mode) != "", "detail": strings.TrimSpace(mode)},
			diagnoseDirWritable("file_state_dir", paths.stateDir),
			diagnoseDirWritable("file_cache_dir", paths.cacheDir),
			diagnoseFileReadable("contacts_active", paths.contactsActive),
			diagnoseFileReadable("contacts_inactive", paths.contactsInactive),
			diagnoseFileReadable("todo_wip", paths.todoWIP),
			diagnoseFileReadable("todo_done", paths.todoDone),
			diagnoseFileReadable("persona_identity", paths.identityPath),
			diagnoseFileReadable("persona_soul", paths.soulPath),
			diagnoseFileReadable("heartbeat_checklist", paths.heartbeatPath),
			diagnoseFileReadable("audit_jsonl", paths.auditPath),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"started_at": startedAt.Format(time.RFC3339),
			"version":    buildVersion(),
			"checks":     checks,
		})
	})

	mux.HandleFunc("/state/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, ""),
		})
	})
	mux.HandleFunc("/state/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/state/files/"))
		spec, ok := resolveStateFileSpec(paths, "", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})

	mux.HandleFunc("/todo/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "todo"),
		})
	})
	mux.HandleFunc("/todo/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/todo/files/"))
		spec, ok := resolveStateFileSpec(paths, "todo", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})

	mux.HandleFunc("/contacts/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "contacts"),
		})
	})
	mux.HandleFunc("/contacts/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/contacts/files/"))
		spec, ok := resolveStateFileSpec(paths, "contacts", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})
	mux.HandleFunc("/contacts/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		offset, err := parseInt64QueryParamInRange(r.URL.Query().Get("offset"), 0, 0, contactsMaxOffset)
		if err != nil {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		limit, err := parseInt64QueryParamInRange(r.URL.Query().Get("limit"), 0, 0, contactsMaxPageSize)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		paths := resolveRuntimeStatePaths()
		service := contacts.NewService(contacts.NewFileStore(paths.contactsDir))
		items, err := listContactsForConsole(r.Context(), service)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		total := int64(len(items))
		paged, hasMore := sliceConsoleContacts(items, offset, limit)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":    paged,
			"total":    total,
			"offset":   offset,
			"limit":    limit,
			"has_more": hasMore,
		})
	})

	mux.HandleFunc("/persona/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "persona"),
		})
	})
	mux.HandleFunc("/persona/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/persona/files/"))
		spec, ok := resolveStateFileSpec(paths, "persona", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})

	mux.HandleFunc("/memory/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		items, err := listMemoryFiles(paths.memoryDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_id": "index.md",
			"items":      items,
		})
	})
	mux.HandleFunc("/memory/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		rawID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/memory/files/"))
		spec, ok := resolveMemoryFileSpec(paths.memoryDir, rawID)
		if !ok {
			http.Error(w, "invalid file id", http.StatusBadRequest)
			return
		}
		handleMemoryFileDetail(w, r, spec)
	})

	mux.HandleFunc("/audit/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		items, err := listAuditFiles(paths.auditPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_file": filepath.Base(strings.TrimSpace(paths.auditPath)),
			"items":        items,
		})
	})

	mux.HandleFunc("/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := resolveRuntimeStatePaths()
		filePath, err := resolveAuditFilePath(paths.auditPath, strings.TrimSpace(r.URL.Query().Get("file")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		limit, err := parseInt64QueryParamInRange(r.URL.Query().Get("limit"), auditDefaultLineLimit, auditMinLineLimit, auditMaxLineLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		cursorRaw := strings.TrimSpace(r.URL.Query().Get("cursor"))
		if cursorRaw == "" {
			cursorRaw = strings.TrimSpace(r.URL.Query().Get("before"))
		}
		cursor, err := parseInt64QueryParamInRange(cursorRaw, 0, 0, auditMaxCursorLines)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		chunk, err := readAuditLogChunk(filePath, cursor, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		chunk.File = filepath.Base(filePath)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chunk)
	})

	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if reader == nil {
				http.Error(w, "task reader is unavailable", http.StatusServiceUnavailable)
				return
			}
			rawStatus := strings.TrimSpace(r.URL.Query().Get("status"))
			status, ok := ParseTaskStatus(rawStatus)
			if !ok {
				http.Error(w, "invalid status", http.StatusBadRequest)
				return
			}
			limit := 20
			if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
				parsed, err := strconv.Atoi(rawLimit)
				if err != nil || parsed <= 0 {
					http.Error(w, "invalid limit", http.StatusBadRequest)
					return
				}
				limit = parsed
			}
			items := reader.List(status, limit)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
			return

		case http.MethodPost:
			if submit == nil {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req SubmitTaskRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			req.Task = strings.TrimSpace(req.Task)
			if req.Task == "" {
				http.Error(w, "missing task", http.StatusBadRequest)
				return
			}
			resp, err := submit(r.Context(), req)
			if err != nil {
				if msg, ok := badRequestMessage(err); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if reader == nil {
			http.Error(w, "task reader is unavailable", http.StatusServiceUnavailable)
			return
		}
		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/tasks/"))
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		info, ok := reader.Get(id)
		if !ok || info == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	})
}

type ServerOptions struct {
	Listen string
	Routes RoutesOptions
}

func StartServer(ctx context.Context, logger *slog.Logger, opts ServerOptions) (*http.Server, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	listen := strings.TrimSpace(opts.Listen)
	if listen == "" {
		return nil, errors.New("empty daemon listen address")
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, opts.Routes)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("daemon_server_error", "addr", listen, "error", err.Error())
		}
	}()

	logger.Info("daemon_server_start",
		"addr", listen,
		"mode", strings.TrimSpace(opts.Routes.Mode),
		"health_enabled", opts.Routes.HealthEnabled,
		"tasks_path", "/tasks",
	)
	return srv, nil
}

func checkAuth(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	want := "Bearer " + token
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func buildDefaultOverviewPayload(mode string, startedAt time.Time) map[string]any {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return map[string]any{
		"version":    buildVersion(),
		"mode":       mode,
		"started_at": startedAt.Format(time.RFC3339),
		"uptime_sec": int(time.Since(startedAt).Seconds()),
		"health":     "ok",
		"channel":    channelOverviewFromMode(mode),
		"runtime":    buildRuntimeMetrics(),
	}
}

func ensureRuntimeMetrics(payload map[string]any) {
	if payload == nil {
		return
	}
	defaults := buildRuntimeMetrics()
	current, ok := payload["runtime"].(map[string]any)
	if !ok || current == nil {
		payload["runtime"] = defaults
		return
	}
	for key, value := range defaults {
		if _, exists := current[key]; !exists {
			current[key] = value
		}
	}
	payload["runtime"] = current
}

func buildRuntimeMetrics() map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{
		"go_version":       runtime.Version(),
		"goroutines":       runtime.NumGoroutine(),
		"heap_alloc_bytes": mem.HeapAlloc,
		"heap_sys_bytes":   mem.HeapSys,
		"heap_objects":     mem.HeapObjects,
		"gc_cycles":        mem.NumGC,
	}
}

func channelOverviewFromMode(mode string) map[string]any {
	telegramRunning := mode == "telegram"
	slackRunning := mode == "slack"
	return map[string]any{
		"configured":          telegramRunning || slackRunning,
		"telegram_configured": telegramRunning,
		"slack_configured":    slackRunning,
		"running":             mode,
		"telegram_running":    telegramRunning,
		"slack_running":       slackRunning,
	}
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "dev"
	}
	if strings.TrimSpace(info.Main.Version) == "" || strings.TrimSpace(info.Main.Version) == "(devel)" {
		return "dev"
	}
	return strings.TrimSpace(info.Main.Version)
}

type runtimeStatePaths struct {
	stateDir         string
	cacheDir         string
	memoryDir        string
	contactsDir      string
	contactsActive   string
	contactsInactive string
	identityPath     string
	soulPath         string
	heartbeatPath    string
	todoWIP          string
	todoDone         string
	auditPath        string
}

func resolveRuntimeStatePaths() runtimeStatePaths {
	stateDir := statepaths.FileStateDir()
	cacheDir := pathutil.ExpandHomePath(viper.GetString("file_cache_dir"))
	contactsDir := statepaths.ContactsDir()
	return runtimeStatePaths{
		stateDir:         stateDir,
		cacheDir:         cacheDir,
		memoryDir:        statepaths.MemoryDir(),
		contactsDir:      contactsDir,
		contactsActive:   filepath.Join(contactsDir, "ACTIVE.md"),
		contactsInactive: filepath.Join(contactsDir, "INACTIVE.md"),
		identityPath:     filepath.Join(stateDir, "IDENTITY.md"),
		soulPath:         filepath.Join(stateDir, "SOUL.md"),
		heartbeatPath:    statepaths.HeartbeatChecklistPath(),
		todoWIP:          statepaths.TODOWIPPath(),
		todoDone:         statepaths.TODODONEPath(),
		auditPath:        resolveGuardAuditPath(stateDir),
	}
}

func resolveGuardAuditPath(stateDir string) string {
	configured := pathutil.ExpandHomePath(viper.GetString("guard.audit.jsonl_path"))
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	guardDir := pathutil.ResolveStateChildDir(stateDir, strings.TrimSpace(viper.GetString("guard.dir_name")), "guard")
	return filepath.Join(guardDir, "audit", "guard_audit.jsonl")
}

func describeFile(name, p string) map[string]any {
	_, err := os.Stat(p)
	return map[string]any{
		"name":   name,
		"path":   p,
		"exists": err == nil,
	}
}

type stateFileSpec struct {
	Name  string
	Group string
	Path  string
}

type memoryFileSpec struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Group     string `json:"group"`
	Date      string `json:"date,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time,omitempty"`
}

type consoleContact struct {
	contacts.Contact
	Status contacts.Status `json:"status"`
}

func runtimeStateFileSpecs(paths runtimeStatePaths) []stateFileSpec {
	return []stateFileSpec{
		{Name: "TODO.md", Group: "todo", Path: paths.todoWIP},
		{Name: "TODO.DONE.md", Group: "todo", Path: paths.todoDone},
		{Name: "ACTIVE.md", Group: "contacts", Path: paths.contactsActive},
		{Name: "INACTIVE.md", Group: "contacts", Path: paths.contactsInactive},
		{Name: "IDENTITY.md", Group: "persona", Path: paths.identityPath},
		{Name: "SOUL.md", Group: "persona", Path: paths.soulPath},
		{Name: "HEARTBEAT.md", Group: "heartbeat", Path: paths.heartbeatPath},
	}
}

func describeStateFiles(paths runtimeStatePaths, group string) []map[string]any {
	group = strings.ToLower(strings.TrimSpace(group))
	specs := runtimeStateFileSpecs(paths)
	items := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		if group != "" && spec.Group != group {
			continue
		}
		item := describeFile(spec.Name, spec.Path)
		item["group"] = spec.Group
		items = append(items, item)
	}
	return items
}

func resolveStateFileSpec(paths runtimeStatePaths, group string, name string) (stateFileSpec, bool) {
	group = strings.ToLower(strings.TrimSpace(group))
	name = strings.TrimSpace(name)
	if name == "" {
		return stateFileSpec{}, false
	}
	specs := runtimeStateFileSpecs(paths)
	for _, spec := range specs {
		if group != "" && spec.Group != group {
			continue
		}
		if strings.EqualFold(spec.Name, name) {
			return spec, true
		}
	}
	return stateFileSpec{}, false
}

func listContactsForConsole(ctx context.Context, svc *contacts.Service) ([]consoleContact, error) {
	if svc == nil {
		return nil, errors.New("contacts service unavailable")
	}
	active, err := svc.ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return nil, err
	}
	inactive, err := svc.ListContacts(ctx, contacts.StatusInactive)
	if err != nil {
		return nil, err
	}
	out := make([]consoleContact, 0, len(active)+len(inactive))
	out = append(out, attachContactStatus(active, contacts.StatusActive)...)
	out = append(out, attachContactStatus(inactive, contacts.StatusInactive)...)
	sort.SliceStable(out, func(i, j int) bool {
		left := consoleContactInteractionTimestamp(out[i])
		right := consoleContactInteractionTimestamp(out[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		leftName := strings.ToLower(strings.TrimSpace(out[i].ContactNickname))
		rightName := strings.ToLower(strings.TrimSpace(out[j].ContactNickname))
		if leftName != rightName {
			return leftName < rightName
		}
		return strings.ToLower(strings.TrimSpace(out[i].ContactID)) < strings.ToLower(strings.TrimSpace(out[j].ContactID))
	})
	return out, nil
}

func attachContactStatus(items []contacts.Contact, status contacts.Status) []consoleContact {
	out := make([]consoleContact, 0, len(items))
	for _, item := range items {
		out = append(out, consoleContact{
			Contact: item,
			Status:  status,
		})
	}
	return out
}

func consoleContactInteractionTimestamp(item consoleContact) time.Time {
	if item.LastInteractionAt == nil || item.LastInteractionAt.IsZero() {
		return time.Time{}
	}
	return item.LastInteractionAt.UTC()
}

func sliceConsoleContacts(items []consoleContact, offset, limit int64) ([]consoleContact, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset >= int64(len(items)) {
		return []consoleContact{}, false
	}
	start := int(offset)
	end := len(items)
	if limit > 0 && start+int(limit) < end {
		end = start + int(limit)
	}
	out := append([]consoleContact(nil), items[start:end]...)
	return out, int64(end) < int64(len(items))
}

func listMemoryFiles(memoryDir string) ([]memoryFileSpec, error) {
	memoryDir = strings.TrimSpace(memoryDir)
	if memoryDir == "" {
		return []memoryFileSpec{}, nil
	}

	items := make([]memoryFileSpec, 0, 16)
	if indexSpec, ok := resolveMemoryFileSpec(memoryDir, "index.md"); ok {
		items = append(items, describeMemoryFile(indexSpec))
	}

	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return items, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		day := strings.TrimSpace(entry.Name())
		if !isValidMemoryDay(day) {
			continue
		}
		dayDir := filepath.Join(memoryDir, day)
		dayEntries, err := os.ReadDir(dayDir)
		if err != nil {
			return nil, err
		}
		for _, dayEntry := range dayEntries {
			if dayEntry.IsDir() {
				continue
			}
			filename := strings.TrimSpace(dayEntry.Name())
			if !isValidMemoryFilename(filename) {
				continue
			}
			id := filepath.ToSlash(filepath.Join(day, filename))
			spec, ok := resolveMemoryFileSpec(memoryDir, id)
			if !ok {
				continue
			}
			items = append(items, describeMemoryFile(spec))
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group == "long_term"
		}
		if items[i].Date != items[j].Date {
			return items[i].Date > items[j].Date
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func resolveMemoryFileSpec(memoryDir string, rawID string) (memoryFileSpec, bool) {
	info, ok := parseMemoryFileID(rawID)
	if !ok {
		return memoryFileSpec{}, false
	}
	memoryDir = strings.TrimSpace(memoryDir)
	if memoryDir == "" {
		return memoryFileSpec{}, false
	}

	base := filepath.Clean(memoryDir)
	abs := filepath.Clean(filepath.Join(base, filepath.FromSlash(info.ID)))
	if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
		return memoryFileSpec{}, false
	}
	return memoryFileSpec{
		ID:        info.ID,
		Name:      info.Name,
		Group:     info.Group,
		Date:      info.Date,
		SessionID: info.SessionID,
		Path:      abs,
	}, true
}

func describeMemoryFile(spec memoryFileSpec) memoryFileSpec {
	fi, err := os.Stat(spec.Path)
	if err != nil {
		spec.Exists = false
		spec.SizeBytes = 0
		spec.ModTime = ""
		return spec
	}
	spec.Exists = true
	spec.SizeBytes = fi.Size()
	spec.ModTime = fi.ModTime().UTC().Format(time.RFC3339)
	return spec
}

type parsedMemoryFileID struct {
	ID        string
	Name      string
	Group     string
	Date      string
	SessionID string
}

func parseMemoryFileID(rawID string) (parsedMemoryFileID, bool) {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return parsedMemoryFileID{}, false
	}
	decoded, err := url.PathUnescape(rawID)
	if err != nil {
		return parsedMemoryFileID{}, false
	}
	decoded = strings.TrimSpace(strings.ReplaceAll(decoded, "\\", "/"))
	if decoded == "" {
		return parsedMemoryFileID{}, false
	}
	for _, part := range strings.Split(decoded, "/") {
		if strings.TrimSpace(part) == ".." {
			return parsedMemoryFileID{}, false
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+decoded), "/")
	if clean == "." || clean == "" {
		return parsedMemoryFileID{}, false
	}
	if clean == "index.md" {
		return parsedMemoryFileID{
			ID:    "index.md",
			Name:  "index.md",
			Group: "long_term",
		}, true
	}

	parts := strings.Split(clean, "/")
	if len(parts) != 2 {
		return parsedMemoryFileID{}, false
	}
	day := strings.TrimSpace(parts[0])
	filename := strings.TrimSpace(parts[1])
	if !isValidMemoryDay(day) || !isValidMemoryFilename(filename) {
		return parsedMemoryFileID{}, false
	}

	sessionID := strings.TrimSpace(strings.TrimSuffix(filename, ".md"))
	if sessionID == "" {
		return parsedMemoryFileID{}, false
	}
	return parsedMemoryFileID{
		ID:        day + "/" + filename,
		Name:      filename,
		Group:     "short_term",
		Date:      day,
		SessionID: sessionID,
	}, true
}

func isValidMemoryDay(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !memoryDayPattern.MatchString(raw) {
		return false
	}
	_, err := time.Parse("2006-01-02", raw)
	return err == nil
}

func isValidMemoryFilename(raw string) bool {
	raw = strings.TrimSpace(raw)
	return memoryFilenamePattern.MatchString(raw)
}

func handleTextFileDetail(w http.ResponseWriter, r *http.Request, name, filePath string) {
	switch r.Method {
	case http.MethodGet:
		content, exists, err := fsstore.ReadText(filePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    name,
			"content": content,
		})
		return

	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := fsstore.EnsureDir(filepath.Dir(filePath), 0o700); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := fsstore.WriteTextAtomic(filePath, req.Content, fsstore.FileOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"name": name,
		})
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func handleMemoryFileDetail(w http.ResponseWriter, r *http.Request, spec memoryFileSpec) {
	switch r.Method {
	case http.MethodGet:
		content, exists, err := fsstore.ReadText(spec.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         spec.ID,
			"name":       spec.Name,
			"group":      spec.Group,
			"date":       spec.Date,
			"session_id": spec.SessionID,
			"content":    content,
		})
		return

	case http.MethodPut:
		if spec.Group == "short_term" {
			_, exists, err := fsstore.ReadText(spec.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !exists {
				http.Error(w, "file not found", http.StatusNotFound)
				return
			}
		}
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := fsstore.EnsureDir(filepath.Dir(spec.Path), 0o700); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := fsstore.WriteTextAtomic(spec.Path, req.Content, fsstore.FileOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"id":   spec.ID,
			"name": spec.Name,
		})
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func diagnoseDirWritable(id, p string) map[string]any {
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]any{"id": id, "ok": false, "detail": err.Error()}
	}
	if !fi.IsDir() {
		return map[string]any{"id": id, "ok": false, "detail": "not a directory"}
	}
	fd, err := os.CreateTemp(p, ".diag_write_*")
	if err != nil {
		return map[string]any{"id": id, "ok": false, "detail": err.Error()}
	}
	name := fd.Name()
	_ = fd.Close()
	_ = os.Remove(name)
	return map[string]any{"id": id, "ok": true}
}

func diagnoseFileReadable(id, p string) map[string]any {
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]any{"id": id, "ok": false, "detail": err.Error()}
	}
	if fi.IsDir() {
		return map[string]any{"id": id, "ok": false, "detail": "is a directory"}
	}
	fd, err := os.Open(p)
	if err != nil {
		return map[string]any{"id": id, "ok": false, "detail": err.Error()}
	}
	_ = fd.Close()
	return map[string]any{"id": id, "ok": true}
}

func listAuditFiles(basePath string) ([]auditFileItem, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return []auditFileItem{}, nil
	}

	dirPath := filepath.Dir(basePath)
	baseName := filepath.Base(basePath)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []auditFileItem{}, nil
		}
		return nil, err
	}

	type sortableAuditFileItem struct {
		item  auditFileItem
		unixN int64
	}
	items := make([]sortableAuditFileItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		if name != baseName && !strings.HasPrefix(name, baseName+".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime().UTC()
		items = append(items, sortableAuditFileItem{
			item: auditFileItem{
				Name:      name,
				Path:      filepath.Join(dirPath, name),
				SizeBytes: info.Size(),
				ModTime:   modTime.Format(time.RFC3339),
				Current:   name == baseName,
			},
			unixN: modTime.UnixNano(),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].item.Current != items[j].item.Current {
			return items[i].item.Current
		}
		if items[i].unixN != items[j].unixN {
			return items[i].unixN > items[j].unixN
		}
		return items[i].item.Name > items[j].item.Name
	})

	out := make([]auditFileItem, 0, len(items))
	for _, it := range items {
		out = append(out, it.item)
	}
	return out, nil
}

func resolveAuditFilePath(basePath, name string) (string, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "", fmt.Errorf("guard audit path is not configured")
	}
	baseName := filepath.Base(basePath)
	name = strings.TrimSpace(name)
	if name == "" {
		return basePath, nil
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid file name")
	}
	if name != baseName && !strings.HasPrefix(name, baseName+".") {
		return "", fmt.Errorf("invalid file name")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(basePath), name)), nil
}

func parseInt64QueryParamInRange(raw string, fallback, min, max int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	if v < min || v > max {
		return 0, fmt.Errorf("out of range")
	}
	return v, nil
}

func readAuditLogChunk(filePath string, cursor int64, limit int64) (auditLogChunk, error) {
	chunk := auditLogChunk{
		Path: strings.TrimSpace(filePath),
	}
	if chunk.Path == "" {
		return chunk, fmt.Errorf("missing audit file path")
	}

	fd, err := os.Open(chunk.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return chunk, nil
		}
		return chunk, err
	}
	defer fd.Close()

	fi, err := fd.Stat()
	if err != nil {
		return chunk, err
	}
	if fi.IsDir() {
		return chunk, fmt.Errorf("audit log path is a directory")
	}

	chunk.Exists = true
	chunk.SizeBytes = fi.Size()
	if chunk.SizeBytes <= 0 {
		return chunk, nil
	}
	if limit <= 0 {
		limit = auditDefaultLineLimit
	}
	chunk.Limit = limit
	if cursor < 0 {
		cursor = 0
	}
	maxNeed := auditMaxCursorLines + auditMaxLineLimit
	need := cursor + limit
	if need < limit || need > maxNeed {
		need = maxNeed
	}

	scanner := bufio.NewScanner(fd)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	tail := make([]string, int(need))
	var total int64
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		tail[int(total%need)] = line
		total++
	}
	if err := scanner.Err(); err != nil {
		return chunk, err
	}
	if cursor > total {
		cursor = total
	}
	chunk.TotalLines = total
	if total > 0 && limit > 0 {
		chunk.TotalPages = (total + limit - 1) / limit
		chunk.CurrentPage = (cursor / limit) + 1
		if chunk.CurrentPage > chunk.TotalPages {
			chunk.CurrentPage = chunk.TotalPages
		}
	}

	end := total - cursor
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	pageCount := end - start

	chunk.Before = cursor
	chunk.To = cursor
	chunk.HasOlder = start > 0
	if chunk.HasOlder {
		chunk.From = cursor + pageCount
	} else {
		chunk.From = cursor
	}
	if pageCount <= 0 {
		return chunk, nil
	}

	tailCount := total
	if tailCount > need {
		tailCount = need
	}
	tailStart := total - tailCount
	localStart := start - tailStart
	localEnd := end - tailStart
	lines := make([]string, 0, int(pageCount))
	for i := localStart; i < localEnd; i++ {
		idx := (tailStart + i) % need
		if idx < 0 {
			idx += need
		}
		lines = append(lines, tail[int(idx)])
	}
	chunk.Lines = lines
	return chunk, nil
}

func IsContextDeadline(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "context canceled")
}

func TruncateUTF8(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars])
}

func BuildTaskID(prefix string, parts ...any) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "task"
	}
	buf := make([]string, 0, len(parts)+1)
	buf = append(buf, prefix)
	for _, part := range parts {
		buf = append(buf, sanitizeTaskIDPart(fmt.Sprint(part)))
	}
	return strings.Join(buf, "_")
}

func sanitizeTaskIDPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return "x"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_", "?", "_", "#", "_", "&", "_", "=", "_", ".", "_")
	part = replacer.Replace(part)
	part = strings.Trim(part, "_")
	if part == "" {
		return "x"
	}
	return part
}
