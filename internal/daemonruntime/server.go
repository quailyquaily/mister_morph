package daemonruntime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
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
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	serverpolicy "github.com/quailyquaily/mistermorph/internal/httpserver"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pagination"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type SubmitFunc func(ctx context.Context, req SubmitTaskRequest) (SubmitTaskResponse, error)
type StopFunc func(ctx context.Context, req StopTaskRequest) (StopTaskResponse, error)
type OverviewFunc func(ctx context.Context) (map[string]any, error)
type PokeFunc func(ctx context.Context, input awarenessdomain.PokeInput) error
type CronRunFunc func(ctx context.Context, task cronstore.Task) error
type WorkspaceResolution struct {
	WorkspaceDir string `json:"workspace_dir"`
	Source       string `json:"source"`
}

type WorkspaceGetFunc func(ctx context.Context, topicID string) (WorkspaceResolution, error)
type WorkspacePutFunc func(ctx context.Context, topicID string, workspaceDir string) (WorkspaceResolution, error)
type WorkspaceDeleteFunc func(ctx context.Context, topicID string) (WorkspaceResolution, error)
type WorkspaceOpenFunc func(ctx context.Context, topicID string, targetPath string) error

type WorkspaceTreeEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	HasChildren bool   `json:"has_children"`
	SizeBytes   int64  `json:"size_bytes"`
}

type WorkspaceTreeListing struct {
	RootPath string               `json:"root_path,omitempty"`
	Path     string               `json:"path"`
	Items    []WorkspaceTreeEntry `json:"items"`
	StateDir string               `json:"state_dir,omitempty"`
	CacheDir string               `json:"cache_dir,omitempty"`
}

type WorkspaceTreeFunc func(ctx context.Context, topicID string, treePath string) (WorkspaceTreeListing, error)
type WorkspaceBrowseFunc func(ctx context.Context, treePath string, showHidden bool) (WorkspaceTreeListing, error)
type WorkspaceCreateDirFunc func(ctx context.Context, parentPath string, name string) (string, error)

type uploadedFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	DirName   string `json:"dir_name"`
	SizeBytes int64  `json:"size_bytes"`
}

type TopicMetadataFunc func(ctx context.Context, topicID string) (TopicMetadata, error)

type TopicMetadata struct {
	TopicID         string                 `json:"topic_id"`
	ConversationKey string                 `json:"conversation_key,omitempty"`
	Workspace       TopicMetadataWorkspace `json:"workspace"`
	Context         TopicMetadataContext   `json:"context"`
}

type TopicMetadataWorkspace struct {
	WorkspaceDir string `json:"workspace_dir"`
	Source       string `json:"source"`
}

type TopicMetadataContext struct {
	Available                bool    `json:"available"`
	Model                    string  `json:"model,omitempty"`
	NormalizedModel          string  `json:"normalized_model,omitempty"`
	ContextWindowTokens      int64   `json:"context_window_tokens,omitempty"`
	ContextWindowSource      string  `json:"context_window_source,omitempty"`
	UsedInputTokens          int64   `json:"used_input_tokens,omitempty"`
	CachedInputTokens        int64   `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens,omitempty"`
	UsageRatio               float64 `json:"usage_ratio,omitempty"`
	LastRunID                string  `json:"last_run_id,omitempty"`
	LastOriginEventID        string  `json:"last_origin_event_id,omitempty"`
	UpdatedAt                string  `json:"updated_at,omitempty"`
}

type TaskTopicRoutes struct {
	TaskReader    TaskReader
	TopicReader   TopicReader
	TopicDeleter  TopicDeleter
	Submit        SubmitFunc
	Stop          StopFunc
	TopicMetadata TopicMetadataFunc
}

type ApprovalRoutes struct {
	List    ApprovalListFunc
	Approve ApprovalDecisionFunc
	Deny    ApprovalDecisionFunc
}

type WorkspaceRoutes struct {
	Get        WorkspaceGetFunc
	Put        WorkspacePutFunc
	Delete     WorkspaceDeleteFunc
	DefaultDir string
	Open       WorkspaceOpenFunc
	Tree       WorkspaceTreeFunc
	Browse     WorkspaceBrowseFunc
	CreateDir  WorkspaceCreateDirFunc
}

var (
	ErrPokeBusy = errors.New("poke already running")
	ErrCronBusy = errors.New("cron task already running")
)

const (
	fileUploadMaxBytes    int64 = 64 << 20
	fileUploadMemoryBytes int64 = 8 << 20
)

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

func serveFileDownload(w http.ResponseWriter, r *http.Request, filePath string) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file does not exist", http.StatusNotFound)
			return
		}
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		return
	}
	if info.IsDir() {
		http.Error(w, "file is a directory", http.StatusBadRequest)
		return
	}
	if !info.Mode().IsRegular() {
		http.Error(w, "file is not a regular file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", attachmentContentDisposition(info.Name()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func serveFilePreview(w http.ResponseWriter, r *http.Request, filePath string) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if !previewExtensionAllowed(filePath) {
		http.Error(w, "file type is not previewable", http.StatusBadRequest)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file does not exist", http.StatusNotFound)
			return
		}
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		return
	}
	if info.IsDir() {
		http.Error(w, "file is a directory", http.StatusBadRequest)
		return
	}
	if !info.Mode().IsRegular() {
		http.Error(w, "file is not a regular file", http.StatusBadRequest)
		return
	}
	ctype := previewContentType(filePath)
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Security-Policy", previewContentSecurityPolicy())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func previewExtensionAllowed(filePath string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))) {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".svg",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico",
		".woff", ".woff2", ".ttf", ".otf":
		return true
	default:
		return false
	}
}

func previewContentType(filePath string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	}
	return strings.TrimSpace(mime.TypeByExtension(filepath.Ext(filePath)))
}

func previewContentSecurityPolicy() string {
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

func resolveFilesDownloadPath(ctx context.Context, workspaceGet WorkspaceGetFunc, paths runtimepaths.Paths, dirName string, topicID string, itemPath string) (string, error) {
	dirName = strings.TrimSpace(dirName)
	itemPath = strings.TrimSpace(itemPath)
	if dirName == "" {
		return "", BadRequest("dir_name is required")
	}
	if itemPath == "" {
		return "", BadRequest("path is required")
	}

	switch dirName {
	case "workspace_dir":
		if workspaceGet == nil {
			return "", fmt.Errorf("workspace is unavailable")
		}
		topicID = strings.TrimSpace(topicID)
		if topicID == "" {
			return "", BadRequest("topic_id is required")
		}
		resolution, err := workspaceGet(ctx, topicID)
		if err != nil {
			return "", err
		}
		workspaceDir := resolution.WorkspaceDir
		if strings.TrimSpace(workspaceDir) == "" {
			return "", BadRequest("workspace is not attached")
		}
		return ResolveFileReferencePath(workspaceDir, itemPath)

	case "file_state_dir":
		return ResolveFileReferencePath(paths.StateDir, itemPath)

	case "file_cache_dir":
		return ResolveFileReferencePath(paths.CacheDir, itemPath)

	default:
		return "", BadRequest("invalid dir_name")
	}
}

func resolveFilesUploadRoot(ctx context.Context, workspaceGet WorkspaceGetFunc, paths runtimepaths.Paths, topicID string, defaultWorkspaceDir string, pendingWorkspaceDir string) (string, string, error) {
	pendingWorkspaceDir = strings.TrimSpace(pendingWorkspaceDir)
	if pendingWorkspaceDir != "" {
		dir, err := validateFileUploadDir(pendingWorkspaceDir)
		if err != nil {
			return "", "", BadRequest(strings.TrimSpace(err.Error()))
		}
		return dir, "workspace_dir", nil
	}

	topicID = strings.TrimSpace(topicID)
	if topicID != "" && workspaceGet != nil {
		resolution, err := workspaceGet(ctx, topicID)
		if err != nil {
			return "", "", err
		}
		dir := resolution.WorkspaceDir
		if strings.TrimSpace(dir) != "" {
			dir, err = validateFileUploadDir(dir)
			if err != nil {
				return "", "", BadRequest(strings.TrimSpace(err.Error()))
			}
			return dir, "workspace_dir", nil
		}
	}

	defaultWorkspaceDir = strings.TrimSpace(defaultWorkspaceDir)
	if defaultWorkspaceDir != "" {
		dir, err := validateFileUploadDir(defaultWorkspaceDir)
		if err != nil {
			return "", "", BadRequest(strings.TrimSpace(err.Error()))
		}
		return dir, "workspace_dir", nil
	}

	cacheRoot := strings.TrimSpace(paths.CacheDir)
	if cacheRoot == "" {
		return "", "", fmt.Errorf("file cache directory is not configured")
	}
	cacheDir, err := filepath.Abs(filepath.Join(cacheRoot, "console"))
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create file cache directory: %w", err)
	}
	return cacheDir, "file_cache_dir", nil
}

func validateFileUploadDir(rawDir string) (string, error) {
	dir, err := filepath.Abs(pathutil.ExpandHomePath(strings.TrimSpace(rawDir)))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace dir does not exist: %s", dir)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace dir is not a directory: %s", dir)
	}
	return dir, nil
}

func saveUploadedFile(rootDir string, rawName string, src io.Reader) (uploadedFile, error) {
	name := strings.TrimSpace(rawName)
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsAny(name, `/\\`) {
		return uploadedFile{}, BadRequest("file name must be a single path segment")
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem = name
		ext = ""
	}
	for index := 0; index < 10_000; index += 1 {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, index, ext)
		}
		targetPath := filepath.Join(rootDir, candidate)
		dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return uploadedFile{}, err
		}

		sizeBytes, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(targetPath)
			if copyErr != nil {
				return uploadedFile{}, copyErr
			}
			return uploadedFile{}, closeErr
		}
		return uploadedFile{
			Name:      candidate,
			Path:      candidate,
			SizeBytes: sizeBytes,
		}, nil
	}
	return uploadedFile{}, fmt.Errorf("could not allocate a unique file name for %s", name)
}

func ResolveFileReferencePath(rootDir string, itemPath string) (string, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return "", fmt.Errorf("file root is not configured")
	}
	rootAbs, err := filepath.Abs(pathutil.ExpandHomePath(rootDir))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file root does not exist")
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("file root is not a directory")
	}
	rootEval, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	rootEval, err = filepath.Abs(rootEval)
	if err != nil {
		return "", err
	}

	itemPath = strings.TrimSpace(itemPath)
	if itemPath == "" || itemPath == "." {
		return "", BadRequest("path is required")
	}
	if filepath.IsAbs(itemPath) {
		return "", BadRequest("path must be relative")
	}
	cleanPath := filepath.Clean(itemPath)
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", BadRequest("path is outside the requested directory")
	}
	targetPath, err := filepath.Abs(filepath.Join(rootAbs, cleanPath))
	if err != nil {
		return "", err
	}
	if filepath.Clean(targetPath) != filepath.Clean(rootAbs) && !pathutil.IsWithinDir(rootAbs, targetPath) {
		return "", BadRequest("path is outside the requested directory")
	}
	targetEval, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return targetPath, nil
		}
		return "", err
	}
	targetEval, err = filepath.Abs(targetEval)
	if err != nil {
		return "", err
	}
	if filepath.Clean(targetEval) != filepath.Clean(rootEval) && !pathutil.IsWithinDir(rootEval, targetEval) {
		return "", BadRequest("path is outside the requested directory")
	}
	return targetEval, nil
}

func attachmentContentDisposition(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", asciiHeaderFilename(name), url.PathEscape(name))
}

func asciiHeaderFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "download"
	}
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r >= 0x7f || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "download"
	}
	return out
}

func parseTopicMetadataPath(rawPath string) (string, bool) {
	suffix := strings.TrimPrefix(strings.TrimSpace(rawPath), "/topic/")
	if suffix == rawPath || !strings.HasSuffix(suffix, "/metadata") {
		return "", false
	}
	topicPart := strings.TrimSuffix(suffix, "/metadata")
	if strings.TrimSpace(topicPart) == "" || strings.Contains(topicPart, "/") {
		return "", false
	}
	topicID, err := url.PathUnescape(topicPart)
	if err != nil || strings.TrimSpace(topicID) == "" {
		return "", false
	}
	return strings.TrimSpace(topicID), true
}

func parseApprovalDecisionPath(rawPath string) (approvalID string, action string, ok bool) {
	suffix := strings.TrimPrefix(strings.TrimSpace(rawPath), "/approvals/")
	if suffix == rawPath || suffix == "" {
		return "", "", false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", false
	}
	id = strings.TrimSpace(id)
	action = strings.TrimSpace(strings.ToLower(parts[1]))
	if id == "" {
		return "", "", false
	}
	switch action {
	case "approve", "deny":
		return id, action, true
	default:
		return "", "", false
	}
}

type RoutesOptions struct {
	Mode                 string
	AgentName            string
	AgentNameFunc        func() string
	AuthToken            string
	TaskTopic            TaskTopicRoutes
	Approvals            ApprovalRoutes
	Workspace            WorkspaceRoutes
	Overview             OverviewFunc
	Poke                 PokeFunc
	CronRun              CronRunFunc
	AgentSettingsEnabled bool
	AgentSettingsOwner   agentsettings.Owner
	AgentSettingsReader  agentsettings.Reader
	RuntimePaths         runtimepaths.Paths
	FileCacheLimits      filecache.Limits
	HealthEnabled        bool
}

const (
	auditDefaultLineLimit int64 = 50
	auditMinLineLimit     int64 = 1
	auditMaxLineLimit     int64 = 500
	logDefaultLineLimit   int64 = 300
	logMinLineLimit       int64 = 1
	logMaxLineLimit       int64 = 1000
)

var (
	memoryDayPattern      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	memoryFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.md$`)
	logFilenamePattern    = regexp.MustCompile(`^mistermorph-\d{4}-\d{2}-\d{2}\.jsonl$`)
)

type auditFileItem struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
	Current   bool   `json:"current"`
}

type auditLogChunk struct {
	pagination.Page[string]
	File      string `json:"file"`
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
}

type logChunk struct {
	pagination.Page[string]
	File      string `json:"file,omitempty"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time,omitempty"`
}

func RegisterRoutes(mux *http.ServeMux, opts RoutesOptions) {
	if mux == nil {
		return
	}
	mode := strings.TrimSpace(opts.Mode)
	settingsReader := agentsettings.NewReaderSnapshot(opts.AgentSettingsReader)
	capturedPaths := opts.RuntimePaths
	if capturedPaths == (runtimepaths.Paths{}) {
		capturedPaths = runtimepaths.FromReader(settingsReader)
	}
	fileCacheLimits := opts.FileCacheLimits
	if settingsReader != nil {
		if fileCacheLimits.MaxAge <= 0 {
			fileCacheLimits.MaxAge = settingsReader.GetDuration("file_cache.max_age")
		}
		if fileCacheLimits.MaxFiles <= 0 {
			fileCacheLimits.MaxFiles = settingsReader.GetInt("file_cache.max_files")
		}
		if fileCacheLimits.MaxTotalBytes <= 0 {
			fileCacheLimits.MaxTotalBytes = settingsReader.GetInt64("file_cache.max_total_bytes")
		}
	}
	if fileCacheLimits.MaxAge <= 0 {
		fileCacheLimits.MaxAge = configdefaults.DefaultFileCacheMaxAge
	}
	if fileCacheLimits.MaxFiles <= 0 {
		fileCacheLimits.MaxFiles = configdefaults.DefaultFileCacheMaxFiles
	}
	if fileCacheLimits.MaxTotalBytes <= 0 {
		fileCacheLimits.MaxTotalBytes = configdefaults.DefaultFileCacheMaxTotalBytes
	}
	statePaths := runtimeStatePathsFrom(capturedPaths)
	startedAt := time.Now().UTC()
	authToken := strings.TrimSpace(opts.AuthToken)
	instanceID := buildRuntimeInstanceID(capturedPaths.StateDir)
	(&routeRegistration{
		mux:             mux,
		options:         opts,
		mode:            mode,
		paths:           capturedPaths,
		statePaths:      statePaths,
		fileCacheLimits: fileCacheLimits,
		startedAt:       startedAt,
		authToken:       authToken,
		instanceID:      instanceID,
		settingsReader:  settingsReader,
	}).register()
}

type ServerOptions struct {
	Listen string
	Routes RoutesOptions
}

func NewHandler(opts RoutesOptions) http.Handler {
	mux := http.NewServeMux()
	RegisterRoutes(mux, opts)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func daemonBodyReadTimeout(r *http.Request) time.Duration {
	if r == nil {
		return 0
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return 0
	}
	if r.URL != nil && r.URL.Path == "/files/upload" {
		return serverpolicy.UploadBodyReadTimeout
	}
	return serverpolicy.BodyReadTimeout
}

func newDaemonHTTPServer(opts ServerOptions) *http.Server {
	return &http.Server{
		Addr: opts.Listen,
		Handler: serverpolicy.WithBodyReadDeadline(
			NewHandler(opts.Routes),
			daemonBodyReadTimeout,
		),
		ReadHeaderTimeout: serverpolicy.ReadHeaderTimeout,
		IdleTimeout:       serverpolicy.IdleTimeout,
	}
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

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}

	opts.Listen = listen
	srv := newDaemonHTTPServer(opts)

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

func buildRuntimeInstanceID(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.Clean(stateDir)))
	return "inst_" + hex.EncodeToString(sum[:8])
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
	personaDir       string
	identityPath     string
	soulPath         string
	avatarPath       string
	heartbeatPath    string
	cronPath         string
	auditPath        string
}

func runtimeStatePathsFrom(paths runtimepaths.Paths) runtimeStatePaths {
	stateDir := paths.StateDir
	contactsDir := paths.ContactsDir
	return runtimeStatePaths{
		stateDir:         stateDir,
		cacheDir:         paths.CacheDir,
		memoryDir:        paths.MemoryDir,
		contactsDir:      contactsDir,
		contactsActive:   filepath.Join(contactsDir, "ACTIVE.md"),
		contactsInactive: filepath.Join(contactsDir, "INACTIVE.md"),
		personaDir:       paths.PersonaDir,
		identityPath:     filepath.Join(paths.PersonaDir, statepaths.IdentityFilename),
		soulPath:         filepath.Join(paths.PersonaDir, statepaths.SoulFilename),
		avatarPath:       filepath.Join(paths.PersonaDir, statepaths.AvatarFilename),
		heartbeatPath:    paths.HeartbeatPath,
		cronPath:         paths.CronPath,
		auditPath:        paths.AuditPath,
	}
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
		{Name: "cron.yaml", Group: "cron", Path: paths.cronPath},
		{Name: "ACTIVE.md", Group: "contacts", Path: paths.contactsActive},
		{Name: "INACTIVE.md", Group: "contacts", Path: paths.contactsInactive},
		{Name: statepaths.IdentityFilename, Group: "persona", Path: paths.identityPath},
		{Name: statepaths.SoulFilename, Group: "persona", Path: paths.soulPath},
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

func getConsoleContactByID(ctx context.Context, svc *contacts.Service, contactID string) (consoleContact, bool, error) {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return consoleContact{}, false, nil
	}
	items, err := listContactsForConsole(ctx, svc)
	if err != nil {
		return consoleContact{}, false, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.ContactID) == contactID {
			return item, true, nil
		}
	}
	return consoleContact{}, false, nil
}

func consoleContactInteractionTimestamp(item consoleContact) time.Time {
	if item.LastInteractionAt == nil || item.LastInteractionAt.IsZero() {
		return time.Time{}
	}
	return item.LastInteractionAt.UTC()
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

const personaAvatarMaxBytes = 2 << 20

func handlePersonaAvatar(w http.ResponseWriter, r *http.Request, avatarPath string) {
	switch r.Method {
	case http.MethodGet:
		raw, err := os.ReadFile(avatarPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "avatar not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return

	case http.MethodPut:
		contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if contentType != "" {
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err != nil || !strings.EqualFold(mediaType, "image/webp") {
				http.Error(w, "avatar must be image/webp", http.StatusBadRequest)
				return
			}
		}
		limited := io.LimitReader(r.Body, personaAvatarMaxBytes+1)
		raw, err := io.ReadAll(limited)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(raw) == 0 {
			http.Error(w, "avatar body is required", http.StatusBadRequest)
			return
		}
		if len(raw) > personaAvatarMaxBytes {
			http.Error(w, "avatar body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := fsstore.WriteBytesAtomic(avatarPath, raw, fsstore.FileOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
		})
		return

	case http.MethodDelete:
		if err := os.Remove(avatarPath); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
		})
		return

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

type todoRuntimeSettingsReader interface {
	GetBool(string) bool
	GetDuration(string) time.Duration
}

func todoRuntimeSettings(reader agentsettings.Reader) todoRuntimeSettingsReader {
	if reader == nil {
		reader = agentsettings.NewReaderSnapshot(nil)
	}
	return reader
}

func todoSystemTasks(reader agentsettings.Reader) []cronstore.Task {
	settings := todoRuntimeSettings(reader)
	if !settings.GetBool("cron.enabled") || !settings.GetBool("heartbeat.enabled") {
		return nil
	}
	schedule, _, _, ok := cronstore.HeartbeatIntervalScheduleWithFallback(
		settings.GetDuration("heartbeat.interval"),
		configdefaults.DefaultHeartbeatInterval,
	)
	if !ok {
		return nil
	}
	return []cronstore.Task{cronstore.HeartbeatTask(schedule)}
}

type runtimeLLMProfileOption struct {
	Name              string `json:"name"`
	InferenceProvider string `json:"inference_provider,omitempty"`
	Model             string `json:"model,omitempty"`
}

type runtimeLLMProfileCatalog struct {
	Default runtimeLLMProfileOption   `json:"default"`
	Items   []runtimeLLMProfileOption `json:"items"`
}

func runtimeLLMProfiles(reader agentsettings.Reader) (runtimeLLMProfileCatalog, error) {
	catalog := runtimeLLMProfileCatalog{
		Default: runtimeLLMProfileOption{Name: llmutil.RouteProfileDefault},
		Items:   []runtimeLLMProfileOption{},
	}
	if reader == nil {
		return catalog, nil
	}
	values, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return runtimeLLMProfileCatalog{}, err
	}
	catalog.Default.InferenceProvider = strings.TrimSpace(values.InferenceProvider)
	catalog.Default.Model = strings.TrimSpace(values.Model)
	if route, resolveErr := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop); resolveErr == nil {
		catalog.Default.InferenceProvider = strings.TrimSpace(route.Values.InferenceProvider)
		if catalog.Default.InferenceProvider == "" {
			catalog.Default.InferenceProvider = strings.TrimSpace(route.ClientConfig.Provider)
		}
		catalog.Default.Model = strings.TrimSpace(route.ClientConfig.Model)
	}
	names := make([]string, 0, len(values.Profiles))
	profileConfigs := make(map[string]llmutil.ProfileConfig, len(values.Profiles))
	for name, config := range values.Profiles {
		if name = strings.TrimSpace(name); name != "" && name != llmutil.RouteProfileDefault {
			names = append(names, name)
			profileConfigs[name] = config
		}
	}
	sort.Strings(names)
	profiles := make([]runtimeLLMProfileOption, 0, len(names))
	for _, name := range names {
		option := runtimeLLMProfileOption{Name: name}
		profile, err := llmutil.ResolveProfile(values, name)
		if err == nil {
			option.InferenceProvider = strings.TrimSpace(profile.Values.InferenceProvider)
			if option.InferenceProvider == "" {
				option.InferenceProvider = strings.TrimSpace(profile.ClientConfig.Provider)
			}
			option.Model = strings.TrimSpace(profile.ClientConfig.Model)
		} else {
			config := profileConfigs[name]
			option.InferenceProvider = strings.TrimSpace(config.InferenceProvider)
			option.Model = strings.TrimSpace(config.Model)
		}
		profiles = append(profiles, option)
	}
	catalog.Items = profiles
	return catalog, nil
}

type todoChatOption struct {
	ChatID    string    `json:"chat_id"`
	Platform  string    `json:"platform,omitempty"`
	Type      string    `json:"type,omitempty"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func handleTodoTasks(w http.ResponseWriter, r *http.Request, cronPath string, contactsDir string, mode string, settingsReader agentsettings.Reader) {
	store := cronstore.NewStore(cronPath)
	var defaultRoute runtimeLLMProfileOption
	var profiles []runtimeLLMProfileOption
	if r.Method == http.MethodGet || r.Method == http.MethodPut {
		catalog, err := runtimeLLMProfiles(settingsReader)
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
			return
		}
		defaultRoute = catalog.Default
		profiles = catalog.Items
	}
	switch r.Method {
	case http.MethodGet:
		file, exists, err := store.Read()
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":           file.Version,
			"exists":            exists,
			"task_count":        len(file.Tasks),
			"tasks":             file.Tasks,
			"system_tasks":      todoSystemTasks(settingsReader),
			"heartbeat_enabled": todoRuntimeSettings(settingsReader).GetBool("heartbeat.enabled"),
			"llm_default_route": defaultRoute,
			"llm_profiles":      profiles,
			"chat_options":      todoChatOptions(r.Context(), contactsDir, mode),
		})
		return

	case http.MethodPut:
		var req struct {
			Tasks []cronstore.Task `json:"tasks"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		tasks := req.Tasks
		if tasks == nil {
			tasks = []cronstore.Task{}
		}
		file := cronstore.File{Version: cronstore.Version, Tasks: tasks}
		if err := store.Write(file); err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"version":           file.Version,
			"task_count":        len(file.Tasks),
			"tasks":             file.Tasks,
			"system_tasks":      todoSystemTasks(settingsReader),
			"heartbeat_enabled": todoRuntimeSettings(settingsReader).GetBool("heartbeat.enabled"),
			"llm_default_route": defaultRoute,
			"llm_profiles":      profiles,
			"chat_options":      todoChatOptions(r.Context(), contactsDir, mode),
		})
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func todoChatOptions(ctx context.Context, contactsDir string, mode string) []todoChatOption {
	store := chatinfo.NewStore(contactsDir)
	items, exists, err := store.Read(ctx)
	if err != nil {
		items = nil
	}
	capacity := len(items)
	if strings.EqualFold(strings.TrimSpace(mode), "console") {
		capacity++
	}
	out := make([]todoChatOption, 0, capacity)
	if strings.EqualFold(strings.TrimSpace(mode), "console") {
		out = append(out, todoChatOption{
			ChatID:   cronstore.ConsoleNotificationChatID,
			Platform: "console",
			Type:     "user",
			Name:     "Console User",
		})
	}
	if !exists {
		return out
	}
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if strings.TrimSpace(item.ChatID) == "" || name == "" {
			continue
		}
		out = append(out, todoChatOption{
			ChatID:    strings.TrimSpace(item.ChatID),
			Platform:  strings.TrimSpace(item.Platform),
			Type:      strings.TrimSpace(item.Type),
			Name:      name,
			ExpiresAt: item.ExpiresAt,
		})
	}
	return out
}

func handleContactsChatProfile(w http.ResponseWriter, r *http.Request, contactsDir string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store := chatinfo.NewStore(contactsDir)
	items, exists, err := store.Read(r.Context())
	if err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}
	if !exists {
		items = []chatinfo.Info{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exists":     exists,
		"item_count": len(items),
		"items":      items,
	})
}

func handleTodoTaskRun(w http.ResponseWriter, r *http.Request, cronPath string, cronRun CronRunFunc) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cronRun == nil {
		http.Error(w, "cron runner is unavailable", http.StatusServiceUnavailable)
		return
	}
	id, ok := parseTodoTaskRunID(r.URL)
	if !ok {
		http.Error(w, "invalid cron task run path", http.StatusBadRequest)
		return
	}
	store := cronstore.NewStore(cronPath)
	task, found, err := store.FindByID(id)
	if err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "cron task not found", http.StatusNotFound)
		return
	}
	if err := cronstore.ValidateTask(task); err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusBadRequest)
		return
	}
	if err := cronRun(r.Context(), task); err != nil {
		if errors.Is(err, ErrCronBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":           true,
		"id":           strings.TrimSpace(task.ID),
		"triggered_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func parseTodoTaskRunID(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	rawPath := u.EscapedPath()
	if rawPath == "" {
		rawPath = u.Path
	}
	rest := strings.TrimPrefix(rawPath, "/todo/tasks/")
	if rest == rawPath || !strings.HasSuffix(rest, "/run") {
		return "", false
	}
	idRaw := strings.TrimSuffix(rest, "/run")
	if strings.TrimSpace(idRaw) == "" || strings.Contains(idRaw, "/") {
		return "", false
	}
	id, err := url.PathUnescape(idRaw)
	if err != nil {
		return "", false
	}
	id = strings.TrimSpace(id)
	return id, id != ""
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

func isAuditFamilyFileName(baseName, name string) bool {
	baseName = strings.TrimSpace(baseName)
	name = strings.TrimSpace(name)
	if baseName == "" || name == "" {
		return false
	}
	if name == baseName || strings.HasPrefix(name, baseName+".") {
		return true
	}
	ext := filepath.Ext(baseName)
	if ext == "" {
		return false
	}
	stem := strings.TrimSuffix(baseName, ext)
	if stem == "" || !strings.HasPrefix(name, stem+".") {
		return false
	}
	suffix := strings.TrimPrefix(name, stem+".")
	if suffix == "" {
		return false
	}
	return strings.HasSuffix(suffix, ext) || strings.Contains(suffix, ext+".")
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
		if !isAuditFamilyFileName(baseName, name) {
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
	if !isAuditFamilyFileName(baseName, name) {
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

func readAuditLogChunk(filePath string, cursor string, limit int64) (auditLogChunk, error) {
	if limit <= 0 {
		limit = auditDefaultLineLimit
	}
	chunk := auditLogChunk{
		Page: pagination.PageWithCursor([]string{}, int(limit), ""),
		Path: strings.TrimSpace(filePath),
	}
	if chunk.Path == "" {
		return chunk, fmt.Errorf("missing audit file path")
	}
	fileID := filepath.Base(chunk.Path)
	page, err := fsstore.ReadLineFilesPage([]fsstore.LineFile{{ID: fileID, Path: chunk.Path}}, cursor, int(limit))
	if err != nil {
		if errors.Is(err, pagination.ErrInvalidCursor) {
			return chunk, BadRequest("invalid cursor")
		}
		return chunk, err
	}
	chunk.Page = page.Page
	chunk.Exists = page.Exists
	chunk.SizeBytes = page.SizeBytes
	return chunk, nil
}

type logFileRef struct {
	Name string
	Path string
	Date time.Time
}

func readLatestLogChunk(dirPath string, cursorRaw string, limit int64) (logChunk, error) {
	if limit <= 0 {
		limit = logDefaultLineLimit
	}
	chunk := logChunk{
		Page: pagination.PageWithCursor([]string{}, int(limit), ""),
	}
	dirPath = strings.TrimSpace(dirPath)
	if dirPath == "" {
		return chunk, fmt.Errorf("log directory is not configured")
	}

	files, err := listLogFiles(dirPath)
	if err != nil {
		return chunk, err
	}
	if len(files) == 0 {
		return chunk, nil
	}

	if strings.TrimSpace(cursorRaw) == "" {
		today := "mistermorph-" + time.Now().Local().Format("2006-01-02") + ".jsonl"
		for i, item := range files {
			if item.Name == today {
				files = files[i:]
				break
			}
		}
	}
	lineFiles := make([]fsstore.LineFile, 0, len(files))
	for _, file := range files {
		lineFiles = append(lineFiles, fsstore.LineFile{ID: file.Name, Path: file.Path})
	}
	page, err := fsstore.ReadLineFilesPage(lineFiles, cursorRaw, int(limit))
	if err != nil {
		if errors.Is(err, pagination.ErrInvalidCursor) {
			return chunk, BadRequest("invalid cursor")
		}
		return chunk, err
	}
	chunk.Page = page.Page
	chunk.File = page.FileID
	chunk.Exists = page.Exists
	chunk.SizeBytes = page.SizeBytes
	if !page.ModTime.IsZero() {
		chunk.ModTime = page.ModTime.Format(time.RFC3339)
	}
	return chunk, nil
}

func listLogFiles(dirPath string) ([]logFileRef, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []logFileRef{}, nil
		}
		return nil, err
	}
	files := make([]logFileRef, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if !logFilenamePattern.MatchString(name) {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, "mistermorph-"), ".jsonl"), time.Local)
		if err != nil {
			continue
		}
		files = append(files, logFileRef{
			Name: name,
			Path: filepath.Join(dirPath, name),
			Date: date,
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		if !files[i].Date.Equal(files[j].Date) {
			return files[i].Date.After(files[j].Date)
		}
		return files[i].Name > files[j].Name
	})
	return files, nil
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
