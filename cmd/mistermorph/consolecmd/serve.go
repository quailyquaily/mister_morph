package consolecmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	serverpolicy "github.com/quailyquaily/mistermorph/internal/httpserver"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type serveConfig struct {
	listen           string
	basePath         string
	staticDir        string
	staticFS         fs.FS
	inspectPrompt    bool
	inspectRequest   bool
	sessionTTL       time.Duration
	passwordOptional bool
	password         string
	passwordHash     string
	version          string
	endpoints        []runtimeEndpointConfig
	endpointWarnings []string
	stateDir         string
	configPath       string
	runtimeOverrides consoleRuntimeOverrides
}

type runtimeEndpointConfig struct {
	Ref       string
	Name      string
	URL       string
	AuthToken string
}

type runtimeEndpointConfigRaw struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
	// AuthToken is the auth token for the runtime endpoint.
	// Use ${ENV_VAR} syntax to reference environment variables.
	// Example:
	//   auth_token: ${MISTER_MORPH_ENDPOINT_AUTH_TOKEN}
	AuthToken string `mapstructure:"auth_token"`
}

type runtimeEndpointClient interface {
	Health(ctx context.Context) (runtimeEndpointHealth, error)
	Proxy(ctx context.Context, method, endpointPath string, body []byte, contentType string) (int, []byte, error)
	Download(ctx context.Context, endpointPath string) (runtimeEndpointDownload, error)
}

type runtimeEndpointHealth struct {
	Mode       string
	AgentName  string
	AvatarURL  string
	CanSubmit  bool
	InstanceID string
}

type runtimeEndpointDownload struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

type runtimeEndpoint struct {
	Ref    string
	Name   string
	URL    string
	Client runtimeEndpointClient
}

type endpointCachedState struct {
	Health      runtimeEndpointHealth
	Connected   bool
	HealthReady bool
	AvatarURL   string
	AvatarReady bool
}

type server struct {
	cfg                         serveConfig
	startedAt                   time.Time
	password                    *passwordVerifier
	sessions                    *sessionStore
	streamTickets               *sessionStore
	artifactPreviews            *artifactPreviewStore
	limiter                     *loginLimiter
	xaiLogins                   *xaiLoginStore
	xaiOAuth                    xaiauth.OAuthConfig
	proLogins                   *proLoginStore
	endpoints                   []runtimeEndpoint
	endpointByRef               map[string]runtimeEndpoint
	endpointStateMu             sync.RWMutex
	endpointStates              []endpointCachedState
	endpointWorkersWG           sync.WaitGroup
	localRuntime                *consoleLocalRuntime
	managed                     *managedRuntimeSupervisor
	agentSettingsConnectionTest func(context.Context, llmSettingsPayload, agentSettingsConnectionTestOptions) (agentSettingsTestResult, error)
	reloadRuntimeConfigFunc     func() error
	runtimeConfigPollerWG       sync.WaitGroup
	webSockets                  consoleWebSocketHandlers
	secretStore                 secref.OSStore
}

const endpointHealthTimeout = 2 * time.Second
const endpointHealthRefreshInterval = 30 * time.Second
const endpointAvatarTimeout = 20 * time.Second
const endpointAvatarMaxBytes = 2 << 20
const proxyUpstreamResponseHeader = "X-MisterMorph-Proxy-Upstream"
const consoleRuntimeAPIPath = "/runtime"

func newServeCmd(version ...string) *cobra.Command {
	buildVersion := ""
	if len(version) > 0 {
		buildVersion = version[0]
	}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run console API + SPA server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadServeConfig(cmd, buildVersion)
			if err != nil {
				return err
			}
			for _, warning := range cfg.endpointWarnings {
				_, _ = fmt.Fprintf(os.Stderr, "warn: %s\n", warning)
			}
			if cfg.authDisabled() {
				_, _ = fmt.Fprintln(os.Stderr, "warn: console password is not configured; authentication is disabled by --allow-empty-password")
			}
			srv, err := newServer(cfg)
			if err != nil {
				return err
			}
			return srv.run(cmd.Context())
		},
	}

	cmd.Flags().String("console-listen", "127.0.0.1:9080", "Console server listen address.")
	cmd.Flags().String("console-base-path", "/", "Console base path.")
	cmd.Flags().String("console-static-dir", "", "Mistermorph Console SPA static directory.")
	cmd.Flags().Duration("console-session-ttl", 12*time.Hour, "Session TTL for console bearer token.")
	cmd.Flags().Bool("allow-empty-password", false, "Allow console to run without console.password/console.password_hash. If a password is configured, login is still required.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_console_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_console_YYYYMMDD_HHmmss.md.")

	return cmd
}

func loadServeConfig(cmd *cobra.Command, version ...string) (serveConfig, error) {
	buildVersion := ""
	if len(version) > 0 {
		buildVersion = version[0]
	}
	listen := strings.TrimSpace(configutil.FlagOrViperString(cmd, "console-listen", "console.listen"))
	if listen == "" {
		listen = "127.0.0.1:9080"
	}

	basePath, err := normalizeBasePath(configutil.FlagOrViperString(cmd, "console-base-path", "console.base_path"))
	if err != nil {
		return serveConfig{}, err
	}

	staticDir, err := resolveStaticDir(configutil.FlagOrViperString(cmd, "console-static-dir", "console.static_dir"))
	if err != nil {
		return serveConfig{}, err
	}

	sessionTTL := configutil.FlagOrViperDuration(cmd, "console-session-ttl", "console.session_ttl")
	if sessionTTL <= 0 {
		sessionTTL = 12 * time.Hour
	}
	passwordOptional, err := cmd.Flags().GetBool("allow-empty-password")
	if err != nil {
		return serveConfig{}, err
	}
	inspectPrompt, err := cmd.Flags().GetBool("inspect-prompt")
	if err != nil {
		return serveConfig{}, err
	}
	inspectRequest, err := cmd.Flags().GetBool("inspect-request")
	if err != nil {
		return serveConfig{}, err
	}

	stateDir := pathutil.ResolveStateDir(viper.GetString("file_state_dir"))
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		return serveConfig{}, err
	}
	var rawEndpoints []runtimeEndpointConfigRaw
	if err := viper.UnmarshalKey("console.endpoints", &rawEndpoints); err != nil {
		return serveConfig{}, fmt.Errorf("invalid console.endpoints: %w", err)
	}
	endpoints, endpointWarnings := resolveRuntimeEndpointsForServe(rawEndpoints)
	return serveConfig{
		listen:           listen,
		basePath:         basePath,
		staticDir:        staticDir,
		staticFS:         consoleStaticFS,
		inspectPrompt:    inspectPrompt,
		inspectRequest:   inspectRequest,
		sessionTTL:       sessionTTL,
		passwordOptional: passwordOptional,
		password:         viper.GetString("console.password"),
		passwordHash:     viper.GetString("console.password_hash"),
		version:          strings.TrimSpace(buildVersion),
		endpoints:        endpoints,
		endpointWarnings: endpointWarnings,
		stateDir:         stateDir,
		configPath:       configPath,
		runtimeOverrides: captureConsoleRuntimeOverrides(cmd),
	}, nil
}

func (c serveConfig) authDisabled() bool {
	return c.passwordOptional && !consolePasswordConfigured(c.password, c.passwordHash)
}

func (c serveConfig) staticAssetsEnabled() bool {
	return strings.TrimSpace(c.staticDir) != "" || c.staticFS != nil
}

func normalizeBasePath(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "/", nil
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	v = path.Clean(v)
	if v == "." || v == "" {
		return "/", nil
	}
	if v == "/" {
		return "/", nil
	}
	v = strings.TrimRight(v, "/")
	if invalidBasePath(v) {
		return "", fmt.Errorf("invalid console base path: %q", raw)
	}
	return v, nil
}

func invalidBasePath(v string) bool {
	return strings.ContainsFunc(v, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`"'<>?#\`, r)
	})
}

func resolveStaticDir(raw string) (string, error) {
	staticDir := pathutil.ExpandHomePath(strings.TrimSpace(raw))
	if staticDir == "" {
		return "", nil
	}
	if fi, err := os.Stat(staticDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("console static dir is invalid: %s", staticDir)
	}
	indexPath := filepath.Join(staticDir, "index.html")
	if fi, err := os.Stat(indexPath); err != nil || fi.IsDir() {
		return "", fmt.Errorf("console static dir must contain index.html: %s", indexPath)
	}
	return staticDir, nil
}

func resolveRuntimeEndpointsForServe(raw []runtimeEndpointConfigRaw) ([]runtimeEndpointConfig, []string) {
	if len(raw) == 0 {
		return nil, nil
	}

	endpoints := make([]runtimeEndpointConfig, 0, len(raw))
	warnings := make([]string, 0, len(raw))
	refSet := make(map[string]struct{}, len(raw))
	for i, item := range raw {
		name := strings.TrimSpace(item.Name)
		url := strings.TrimRight(strings.TrimSpace(item.URL), "/")
		token := strings.TrimSpace(item.AuthToken)
		if name == "" || url == "" || token == "" {
			warnings = append(warnings, fmt.Sprintf("console.endpoints[%d] skipped: name, url, auth_token are required", i))
			continue
		}

		ref := buildRuntimeEndpointRef(name, url)
		if _, exists := refSet[ref]; exists {
			warnings = append(warnings, fmt.Sprintf("console.endpoints[%d] skipped: duplicate endpoint %q (%s)", i, name, url))
			continue
		}
		refSet[ref] = struct{}{}

		endpoints = append(endpoints, runtimeEndpointConfig{
			Ref:       ref,
			Name:      name,
			URL:       url,
			AuthToken: token,
		})
	}
	return endpoints, warnings
}

func buildRuntimeEndpointRef(name, url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name) + "\n" + strings.TrimSpace(url)))
	return "ep_" + hex.EncodeToString(sum[:8])
}

func newServer(cfg serveConfig) (*server, error) {
	password, passwordErr := newPasswordVerifier(cfg.password, cfg.passwordHash)
	if cfg.authDisabled() {
		password = nil
		passwordErr = nil
	}
	if passwordErr != nil {
		return nil, passwordErr
	}
	sessionStorePath := ""
	if strings.TrimSpace(cfg.stateDir) != "" {
		sessionStorePath = filepath.Join(cfg.stateDir, "console", "sessions.json")
	}

	reader, err := loadConsoleRuntimeConfig(cfg.configPath, cfg.runtimeOverrides)
	if err != nil {
		fallbackReader, fallbackErr := loadConsoleRuntimeConfig("", cfg.runtimeOverrides)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		_, _ = fmt.Fprintf(os.Stderr, "warn: console runtime config invalid; starting with defaults-only snapshot: %v\n", err)
		reader = fallbackReader
	}

	localRuntime, err := newConsoleLocalRuntime(cfg, reader)
	if err != nil {
		return nil, err
	}
	managed := newManagedRuntimeSupervisor(localRuntime, cfg.inspectPrompt, cfg.inspectRequest)
	if err := managed.ReloadConfig(reader); err != nil {
		localRuntime.Close()
		return nil, err
	}

	endpoints := make([]runtimeEndpoint, 0, len(cfg.endpoints)+1)
	endpointByRef := make(map[string]runtimeEndpoint, len(cfg.endpoints)+1)
	localEndpoint := localRuntime.Endpoint()
	endpoints = append(endpoints, localEndpoint)
	endpointByRef[localEndpoint.Ref] = localEndpoint
	for _, item := range cfg.endpoints {
		ep := runtimeEndpoint{
			Ref:    item.Ref,
			Name:   item.Name,
			URL:    item.URL,
			Client: newDaemonTaskClient(item.URL, item.AuthToken),
		}
		endpoints = append(endpoints, ep)
		endpointByRef[ep.Ref] = ep
	}

	srv := &server{
		cfg:              cfg,
		startedAt:        time.Now().UTC(),
		password:         password,
		sessions:         newSessionStore(sessionStorePath),
		streamTickets:    newSessionStore(""),
		artifactPreviews: newArtifactPreviewStore(),
		limiter:          newLoginLimiter(),
		xaiLogins:        newXAILoginStore(),
		xaiOAuth:         xaiauth.OAuthConfig{},
		proLogins:        newProLoginStore(),
		endpoints:        endpoints,
		endpointByRef:    endpointByRef,
		localRuntime:     localRuntime,
		managed:          managed,
		secretStore:      secref.NewOSStore(),
	}
	srv.ensureEndpointStates()
	return srv, nil
}

func (s *server) run(ctx context.Context) error {
	if s != nil && s.localRuntime != nil {
		defer s.localRuntime.Close()
	}
	if s != nil && s.managed != nil {
		defer s.managed.Close()
	}
	ln, err := net.Listen("tcp", s.cfg.listen)
	if err != nil {
		return err
	}
	return s.serve(ctx, ln)
}

func (s *server) serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ln == nil {
		return errors.New("console listener is nil")
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	apiPrefix := joinBasePath(s.cfg.basePath, "/api")
	httpSrv := newConsoleHTTPServer(s)
	httpSrv.BaseContext = func(net.Listener) context.Context {
		return runCtx
	}
	fatalErrCh := make(chan error, 1)
	if s != nil && s.managed != nil {
		if err := s.managed.Start(runCtx, func(err error) {
			if err == nil {
				return
			}
			select {
			case fatalErrCh <- err:
			default:
			}
			cancelRun()
		}); err != nil {
			_ = ln.Close()
			return err
		}
	}
	s.startEndpointBackground(runCtx)
	s.startRuntimeConfigPoller(runCtx)
	fmt.Fprintf(os.Stdout, "console serve listening on http://%s%s\n", ln.Addr().String(), displayBasePath(s.cfg.basePath))
	if !s.cfg.staticAssetsEnabled() {
		fmt.Fprintf(os.Stdout, "console serve static assets disabled; API available under http://%s%s\n", ln.Addr().String(), apiPrefix)
	}
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- httpSrv.Serve(ln)
	}()

	var serveErr error
	serveReturned := false
	select {
	case serveErr = <-serveErrCh:
		serveReturned = true
	case <-runCtx.Done():
	}

	// Stop accepting work before releasing runtime resources. BaseContext makes
	// cancellation visible to ordinary handlers; Shutdown then joins them.
	_ = ln.Close()
	cancelRun()
	s.webSockets.CloseAndWait()
	shutdownErr := httpSrv.Shutdown(context.Background())
	if !serveReturned {
		serveErr = <-serveErrCh
	}
	s.runtimeConfigPollerWG.Wait()
	s.endpointWorkersWG.Wait()

	select {
	case fatalErr := <-fatalErrCh:
		return fatalErr
	default:
	}
	if !isBenignServeCloseError(serveErr) {
		return serveErr
	}
	if shutdownErr != nil && !isBenignServeCloseError(shutdownErr) {
		return shutdownErr
	}
	return nil
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	apiPrefix := joinBasePath(s.cfg.basePath, "/api")
	codexAuthHandler := codexauth.NewHTTPHandler(codexauth.HTTPHandlerOptions{
		StateDir:   s.cfg.stateDir,
		SetDefault: s.setCodexAsDefaultLLM,
	})
	registerConsoleOwnedSettings := func(
		target *http.ServeMux,
		prefix string,
		authorize func(http.HandlerFunc) http.HandlerFunc,
	) {
		register := func(path string, handler http.HandlerFunc) {
			target.HandleFunc(prefix+path, authorize(handler))
		}
		register("/auth/codex/status", codexAuthHandler.Status)
		register("/auth/codex/refresh", codexAuthHandler.Refresh)
		register("/auth/codex/login/start", codexAuthHandler.LoginStart)
		register("/auth/codex/login/poll", codexAuthHandler.LoginPoll)
		register("/auth/codex/logout", codexAuthHandler.Logout)
		register("/auth/xai/status", s.handleXAIAuthStatus)
		register("/auth/xai/login/start", s.handleXAIAuthLoginStart)
		register("/auth/xai/login/poll", s.handleXAIAuthLoginPoll)
		register("/auth/xai/logout", s.handleXAIAuthLogout)
		register("/auth/pro/status", s.handleProAuthStatus)
		register("/auth/pro/login/start", s.handleProAuthLoginStart)
		register("/auth/pro/login/poll", s.handleProAuthLoginPoll)
		register("/auth/pro/logout", s.handleProAuthLogout)
		register("/settings/agent", s.handleAgentSettings)
		register("/settings/agent/models", s.handleAgentSettingsModels)
		register("/settings/agent/test", s.handleAgentSettingsTest)
		register("/settings/console", s.handleConsoleSettings)
		register("/settings/auto-update", s.handleAutoUpdateSettings)
		register("/settings/auto-update/check", s.handleAutoUpdateCheck)
		register("/setup/integrity", s.handleSetupIntegrity)
		register("/setup/file", s.handleSetupRepairFile)
		register("/setup/secret", s.handleSetupRepairSecret)
	}

	mux.HandleFunc("/health", s.handleHealth)

	mux.HandleFunc(apiPrefix+"/auth/config", s.handleAuthConfig)
	mux.HandleFunc(apiPrefix+"/auth/login", s.handleLogin)
	mux.HandleFunc(apiPrefix+"/auth/logout", s.withAuth(s.handleLogout))
	mux.HandleFunc(apiPrefix+"/auth/me", s.withAuth(s.handleAuthMe))
	registerConsoleOwnedSettings(mux, apiPrefix, s.withAuth)
	mux.HandleFunc(apiPrefix+"/endpoints", s.withAuth(s.handleEndpoints))
	mux.HandleFunc(apiPrefix+"/commands", s.withAuth(s.handleRuntimeCommands))
	mux.HandleFunc(apiPrefix+"/settings/credits", s.withAuth(s.handleCredits))
	mux.HandleFunc(apiPrefix+"/proxy", s.withAuth(s.handleProxy))
	mux.HandleFunc(apiPrefix+"/proxy/download", s.withAuth(s.handleProxyDownload))
	mux.HandleFunc(apiPrefix+"/artifacts/preview-ticket", s.withAuth(s.handleArtifactPreviewTicket))
	mux.HandleFunc(apiPrefix+"/artifacts/preview-ticket/renew", s.withAuth(s.handleArtifactPreviewTicketRenew))
	mux.HandleFunc(apiPrefix+"/artifacts/preview/", s.handleArtifactPreview)
	mux.HandleFunc(apiPrefix+"/stream/ticket", s.withAuth(s.handleStreamTicket))
	mux.HandleFunc(apiPrefix+"/stream/ws", s.handleStreamWebSocket)
	mux.HandleFunc(apiPrefix+"/notifications/ws", s.handleNotificationWebSocket)

	if s.localRuntime != nil && strings.TrimSpace(s.localRuntime.currentConfigReader().GetString("server.auth_token")) != "" {
		runtimePrefix := joinBasePath(s.cfg.basePath, consoleRuntimeAPIPath)
		runtimeFallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := s.localRuntime.currentHandler()
			if current == nil {
				http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
				return
			}
			current.ServeHTTP(w, r)
		})
		runtimeAuthorize := func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				got, ok := bearerToken(r)
				want := s.localRuntime.currentAuthToken()
				if !ok || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				next(w, r)
			}
		}
		runtimeHandler := http.NewServeMux()
		registerConsoleOwnedSettings(runtimeHandler, "", runtimeAuthorize)
		runtimeHandler.HandleFunc("/stream/ws", runtimeAuthorize(s.handleRuntimeStreamWebSocket))
		runtimeHandler.Handle("/", runtimeFallback)
		mux.Handle(runtimePrefix+"/", http.StripPrefix(runtimePrefix, runtimeHandler))
	}

	if s.cfg.staticAssetsEnabled() {
		if s.cfg.basePath == "/" {
			mux.HandleFunc("/", s.handleSPA)
		} else {
			mux.HandleFunc(s.cfg.basePath, s.handleSPA)
			mux.HandleFunc(s.cfg.basePath+"/", s.handleSPA)
		}
	}

	return serverpolicy.WithBodyReadDeadline(mux, func(r *http.Request) time.Duration {
		return consoleBodyReadTimeout(r, apiPrefix)
	})
}

func consoleBodyReadTimeout(r *http.Request, apiPrefix string) time.Duration {
	if r == nil {
		return 0
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return 0
	}
	if r.URL != nil && r.URL.Path == apiPrefix+"/proxy" {
		if target, err := url.Parse(strings.TrimSpace(r.URL.Query().Get("uri"))); err == nil && target.Path == "/files/upload" {
			return serverpolicy.UploadBodyReadTimeout
		}
	}
	basePath := strings.TrimSuffix(apiPrefix, "/api")
	if r.URL != nil && r.URL.Path == basePath+consoleRuntimeAPIPath+"/files/upload" {
		return serverpolicy.UploadBodyReadTimeout
	}
	return serverpolicy.BodyReadTimeout
}

func newConsoleHTTPServer(s *server) *http.Server {
	return &http.Server{
		Addr:              s.cfg.listen,
		Handler:           s.handler(),
		ReadHeaderTimeout: serverpolicy.ReadHeaderTimeout,
		IdleTimeout:       serverpolicy.IdleTimeout,
	}
}

func isBenignServeCloseError(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)
}

func (s *server) logger() *slog.Logger {
	if s != nil && s.localRuntime != nil {
		return s.localRuntime.currentLogger()
	}
	return slog.Default()
}

func (s *server) currentRuntimeConfigReader() *viper.Viper {
	if s != nil && s.localRuntime != nil {
		if reader := s.localRuntime.currentConfigReader(); reader != nil {
			return reader
		}
	}
	return viper.New()
}

func (s *server) startRuntimeConfigPoller(ctx context.Context) {
	if s == nil || strings.TrimSpace(s.cfg.configPath) == "" {
		return
	}
	lastFingerprint, err := fingerprintConfigPath(s.cfg.configPath)
	if err != nil {
		s.logger().Warn("console_runtime_config_poll_stat_failed", "error", err.Error())
	}
	reloadRuntimeConfig := s.reloadRuntimeConfig
	if s.reloadRuntimeConfigFunc != nil {
		reloadRuntimeConfig = s.reloadRuntimeConfigFunc
	}
	s.runtimeConfigPollerWG.Add(1)
	go func() {
		defer s.runtimeConfigPollerWG.Done()
		ticker := time.NewTicker(consoleConfigPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				nextFingerprint, err := fingerprintConfigPath(s.cfg.configPath)
				if err != nil {
					s.logger().Warn("console_runtime_config_poll_stat_failed", "error", err.Error())
					continue
				}
				if nextFingerprint == lastFingerprint {
					continue
				}
				lastFingerprint = nextFingerprint
				if err := reloadRuntimeConfig(); err != nil {
					s.logger().Warn("console_runtime_reload_failed", "error", err.Error())
					continue
				}
				s.logger().Info("console_runtime_reloaded", "config_path", strings.TrimSpace(s.cfg.configPath))
			}
		}
	}()
}

func (s *server) reloadRuntimeConfig() error {
	if s == nil {
		return nil
	}
	reader, err := loadConsoleRuntimeConfig(s.cfg.configPath, s.cfg.runtimeOverrides)
	if err != nil {
		return err
	}
	var preparedLocal *consoleLocalRuntimeGeneration
	if s.localRuntime != nil {
		preparedLocal, err = s.localRuntime.prepareGeneration(reader)
		if err != nil {
			return err
		}
	}
	var preparedManaged *managedRuntimePrepared
	if s.managed != nil {
		preparedManaged, err = s.managed.PrepareReload(reader)
		if err != nil {
			if preparedLocal != nil {
				preparedLocal.cleanupNow()
			}
			return err
		}
	}
	if preparedLocal != nil {
		if err := s.localRuntime.applyPreparedGeneration(preparedLocal); err != nil {
			preparedLocal.cleanupNow()
			if preparedManaged != nil {
				preparedManaged.cleanup()
			}
			return err
		}
	}
	if preparedManaged != nil {
		if err := s.managed.ApplyPrepared(preparedManaged); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s != nil && s.cfg.authDisabled() {
			next(w, r)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		expiresAt, ok := s.sessions.Validate(token)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r.Header.Set("X-Console-Token-Expires-At", expiresAt.Format(time.RFC3339))
		next(w, r)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"mode":           "ready",
		"setup_required": false,
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s != nil && s.cfg.authDisabled() {
		token, expiresAt, err := s.sessions.Create(s.cfg.sessionTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"token_type":   "Bearer",
			"expires_at":   expiresAt.Format(time.RFC3339),
		})
		return
	}

	now := time.Now().UTC()
	ip := clientIP(r.RemoteAddr)
	key := "console@" + ip
	if remaining, locked := s.limiter.CheckLocked(key, now); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if !s.password.Verify(req.Password) {
		lockUntil := s.limiter.RecordFailure(ip, key, now)
		time.Sleep(s.limiter.FailureDelay())
		if !lockUntil.IsZero() {
			retry := int(lockUntil.Sub(time.Now().UTC()).Seconds()) + 1
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "too many failed attempts")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	s.limiter.RecordSuccess(ip, key, now)
	token, expiresAt, err := s.sessions.Create(s.cfg.sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt.Format(time.RFC3339),
	})
}

func (s *server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"password_required": !s.cfg.authDisabled(),
	})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token, _ := bearerToken(r)
	s.sessions.Delete(token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	expires := strings.TrimSpace(r.Header.Get("X-Console-Token-Expires-At"))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"account":       "console",
		"expires_at":    expires,
	})
}

func (s *server) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	type endpointSnapshot struct {
		Ref               string
		Name              string
		URL               string
		Connected         bool
		AgentName         string
		Mode              string
		CanSubmit         bool
		SubmitEndpointRef string
		AvatarURL         string
		HealthPending     bool
	}

	s.ensureEndpointStates()
	snapshots := make([]endpointSnapshot, len(s.endpoints))
	s.endpointStateMu.RLock()
	for i, ep := range s.endpoints {
		state := s.endpointStates[i]
		snapshots[i] = endpointSnapshot{
			Ref:           ep.Ref,
			Name:          ep.Name,
			URL:           ep.URL,
			Connected:     state.Connected,
			AgentName:     state.Health.AgentName,
			Mode:          state.Health.Mode,
			CanSubmit:     state.Health.CanSubmit,
			AvatarURL:     state.AvatarURL,
			HealthPending: !state.HealthReady,
		}
	}
	s.endpointStateMu.RUnlock()

	items := make([]map[string]any, 0, len(snapshots))
	for _, item := range snapshots {
		items = append(items, map[string]any{
			"endpoint_ref":        item.Ref,
			"name":                item.Name,
			"url":                 item.URL,
			"connected":           item.Connected,
			"agent_name":          item.AgentName,
			"mode":                item.Mode,
			"can_submit":          item.CanSubmit,
			"submit_endpoint_ref": item.SubmitEndpointRef,
			"avatar_url":          item.AvatarURL,
			"health_pending":      item.HealthPending,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *server) ensureEndpointStates() {
	if s == nil {
		return
	}
	s.endpointStateMu.Lock()
	defer s.endpointStateMu.Unlock()
	if len(s.endpointStates) != len(s.endpoints) {
		s.endpointStates = make([]endpointCachedState, len(s.endpoints))
	}
}

func (s *server) setEndpointAvatar(ref string, avatarURL string) {
	if s == nil {
		return
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	s.ensureEndpointStates()
	s.endpointStateMu.Lock()
	defer s.endpointStateMu.Unlock()
	for i, endpoint := range s.endpoints {
		if endpoint.Ref != ref {
			continue
		}
		state := s.endpointStates[i]
		state.AvatarURL = avatarURL
		state.AvatarReady = true
		s.endpointStates[i] = state
		return
	}
}

func (s *server) startEndpointBackground(ctx context.Context) {
	if s == nil || len(s.endpoints) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ensureEndpointStates()
	s.endpointWorkersWG.Add(1)
	go func() {
		defer s.endpointWorkersWG.Done()
		ticker := time.NewTicker(endpointHealthRefreshInterval)
		defer ticker.Stop()
		s.refreshEndpointHealth(ctx)
		s.refreshEndpointAvatars(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshEndpointHealth(ctx)
				s.refreshEndpointAvatars(ctx)
			}
		}
	}()
}

func (s *server) refreshEndpointAvatars(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ensureEndpointStates()
	var wg sync.WaitGroup
	for i, endpoint := range s.endpoints {
		s.endpointStateMu.RLock()
		state := s.endpointStates[i]
		s.endpointStateMu.RUnlock()
		if !state.Connected || state.AvatarReady {
			continue
		}
		wg.Add(1)
		go func(i int, endpoint runtimeEndpoint) {
			defer wg.Done()
			avatarCtx, cancel := context.WithTimeout(ctx, endpointAvatarTimeout)
			avatarURL, ready := fetchEndpointAvatarDataURL(avatarCtx, endpoint.Client)
			cancel()
			if ctx.Err() != nil || !ready {
				return
			}
			s.endpointStateMu.Lock()
			state := s.endpointStates[i]
			if state.AvatarReady {
				s.endpointStateMu.Unlock()
				return
			}
			state.AvatarURL = avatarURL
			state.AvatarReady = true
			s.endpointStates[i] = state
			s.endpointStateMu.Unlock()
		}(i, endpoint)
	}
	wg.Wait()
}

func (s *server) refreshEndpointHealth(ctx context.Context) {
	if s == nil {
		return
	}
	s.ensureEndpointStates()
	var wg sync.WaitGroup
	for i, endpoint := range s.endpoints {
		wg.Add(1)
		go func(i int, endpoint runtimeEndpoint) {
			defer wg.Done()
			healthCtx, cancel := context.WithTimeout(ctx, endpointHealthTimeout)
			health, err := endpoint.Client.Health(healthCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			s.endpointStateMu.Lock()
			state := s.endpointStates[i]
			state.Health = health
			state.Connected = err == nil
			state.HealthReady = true
			if strings.TrimSpace(health.AvatarURL) != "" {
				state.AvatarURL = strings.TrimSpace(health.AvatarURL)
				state.AvatarReady = true
			}
			s.endpointStates[i] = state
			s.endpointStateMu.Unlock()
		}(i, endpoint)
	}
	wg.Wait()
}

func fetchEndpointAvatarDataURL(ctx context.Context, client runtimeEndpointClient) (string, bool) {
	if client == nil {
		return "", true
	}
	download, err := client.Download(ctx, "/persona/avatar")
	if err != nil {
		return "", false
	}
	if download.Body != nil {
		defer download.Body.Close()
	}
	if download.Status < 200 || download.Status >= 300 {
		if download.Status <= 0 || download.Status == http.StatusRequestTimeout || download.Status == http.StatusTooManyRequests || download.Status >= 500 {
			return "", false
		}
		return "", true
	}
	if download.Body == nil {
		return "", true
	}
	raw, err := io.ReadAll(io.LimitReader(download.Body, endpointAvatarMaxBytes+1))
	if err != nil {
		return "", false
	}
	avatarURL, valid := encodeEndpointAvatarDataURL(raw, download.Header.Get("Content-Type"))
	if !valid {
		return "", true
	}
	return avatarURL, true
}

func encodeEndpointAvatarDataURL(raw []byte, contentType string) (string, bool) {
	if len(raw) == 0 || len(raw) > endpointAvatarMaxBytes {
		return "", false
	}
	mediaType := strings.TrimSpace(contentType)
	if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
		mediaType = parsed
	}
	if mediaType == "" {
		mediaType = "image/webp"
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return "", false
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(raw), true
}

func (s *server) handleProxy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	endpoint, err := s.resolveRuntimeEndpoint(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	parsedURI, err := resolveProxyTargetURI(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body []byte
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
		maxBodyBytes := int64(4 << 20)
		if parsedURI.Path == "/files/upload" {
			maxBodyBytes = 64 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		body, err = io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	status, raw, err := endpoint.Client.Proxy(r.Context(), r.Method, parsedURI.RequestURI(), body, r.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if status >= 200 && status < 300 && parsedURI.Path == "/persona/avatar" {
		switch r.Method {
		case http.MethodPut:
			if avatarURL, valid := encodeEndpointAvatarDataURL(body, r.Header.Get("Content-Type")); valid {
				s.setEndpointAvatar(endpoint.Ref, avatarURL)
			}
		case http.MethodDelete:
			s.setEndpointAvatar(endpoint.Ref, "")
		}
	}
	w.Header().Set(proxyUpstreamResponseHeader, "1")
	writeJSONProxyResponse(w, status, raw)
}

func (s *server) handleProxyDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	endpoint, err := s.resolveRuntimeEndpoint(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	parsedURI, err := resolveProxyTargetURI(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch parsedURI.Path {
	case "/files/download", "/persona/avatar", "/contacts/avatar":
	default:
		writeError(w, http.StatusBadRequest, "invalid download uri")
		return
	}

	download, err := endpoint.Client.Download(r.Context(), parsedURI.RequestURI())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	w.Header().Set(proxyUpstreamResponseHeader, "1")
	if download.Body != nil {
		defer download.Body.Close()
	}
	status := download.Status
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if status < 200 || status >= 300 {
		if parsedURI.Path == "/persona/avatar" && status == http.StatusNotFound {
			s.setEndpointAvatar(endpoint.Ref, "")
		}
		raw := []byte(nil)
		if download.Body != nil {
			raw, _ = io.ReadAll(io.LimitReader(download.Body, 1<<20))
		}
		writeJSONProxyResponse(w, status, raw)
		return
	}

	if parsedURI.Path == "/contacts/avatar" {
		copyDownloadHeader(w.Header(), download.Header, "Cache-Control")
		copyDownloadHeader(w.Header(), download.Header, "Last-Modified")
	} else {
		setNoCacheHeaders(w.Header())
	}
	copyDownloadHeader(w.Header(), download.Header, "Content-Type")
	copyDownloadHeader(w.Header(), download.Header, "Content-Disposition")
	copyDownloadHeader(w.Header(), download.Header, "Content-Length")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if w.Header().Get("Content-Disposition") == "" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	if parsedURI.Path == "/persona/avatar" && download.Body != nil {
		raw, readErr := io.ReadAll(io.LimitReader(download.Body, endpointAvatarMaxBytes+1))
		if readErr == nil {
			if avatarURL, valid := encodeEndpointAvatarDataURL(raw, download.Header.Get("Content-Type")); valid {
				s.setEndpointAvatar(endpoint.Ref, avatarURL)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(raw)
		if len(raw) > endpointAvatarMaxBytes {
			_, _ = io.Copy(w, download.Body)
		}
		return
	}
	w.WriteHeader(status)
	if download.Body != nil {
		_, _ = io.Copy(w, download.Body)
	}
}

func resolveProxyTargetURI(r *http.Request) (*url.URL, error) {
	targetURI := strings.TrimSpace(r.URL.Query().Get("uri"))
	if targetURI == "" {
		return nil, fmt.Errorf("missing uri")
	}
	if !strings.HasPrefix(targetURI, "/") {
		targetURI = "/" + targetURI
	}
	parsedURI, err := url.ParseRequestURI(targetURI)
	if err != nil || parsedURI == nil || strings.TrimSpace(parsedURI.Path) == "" {
		return nil, fmt.Errorf("invalid uri")
	}
	if parsedURI.Host != "" || parsedURI.Scheme != "" {
		return nil, fmt.Errorf("invalid uri")
	}
	return parsedURI, nil
}

func copyDownloadHeader(dst http.Header, src http.Header, key string) {
	if value := strings.TrimSpace(src.Get(key)); value != "" {
		dst.Set(key, value)
	}
}

func writeJSONProxyResponse(w http.ResponseWriter, status int, raw []byte) {
	setNoCacheHeaders(w.Header())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	if json.Valid(trimmed) {
		_, _ = w.Write(trimmed)
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] != '\n' {
			_, _ = w.Write([]byte("\n"))
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": strings.TrimSpace(string(trimmed)),
	})
}

func (s *server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.cfg.basePath != "/" && r.URL.Path == s.cfg.basePath {
		target := s.cfg.basePath + "/"
		if strings.TrimSpace(r.URL.RawQuery) != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}
	apiPrefix := joinBasePath(s.cfg.basePath, "/api")
	if strings.HasPrefix(r.URL.Path, apiPrefix+"/") || r.URL.Path == apiPrefix {
		http.NotFound(w, r)
		return
	}
	runtimePrefix := joinBasePath(s.cfg.basePath, consoleRuntimeAPIPath)
	if strings.HasPrefix(r.URL.Path, runtimePrefix+"/") || r.URL.Path == runtimePrefix {
		http.NotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, strings.TrimRight(s.cfg.basePath, "/"))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		s.serveSPAIndex(w, r)
		return
	}

	clean := path.Clean("/" + rel)
	if s.serveStaticAsset(w, r, strings.TrimPrefix(clean, "/")) {
		return
	}
	s.serveSPAIndex(w, r)
}

func (s *server) serveSPAIndex(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.NotFound(w, r)
		return
	}
	raw, err := s.readStaticAsset("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body := renderSPAIndex(raw, s.cfg.basePath)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(body))
}

func renderSPAIndex(raw []byte, basePath string) []byte {
	display := html.EscapeString(displayBasePath(basePath))
	body := bytes.ReplaceAll(raw, []byte("__MISTERMORPH_BASE_PATH__"), []byte(display))
	body = bytes.ReplaceAll(body, []byte("__MISTERMORPH_BASE_HREF__"), []byte(html.EscapeString(displayBaseHref(basePath))))
	return body
}

func (s *server) serveStaticAsset(w http.ResponseWriter, r *http.Request, rel string) bool {
	if s == nil {
		return false
	}
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return false
	}
	if strings.TrimSpace(s.cfg.staticDir) != "" {
		target := filepath.Join(s.cfg.staticDir, filepath.FromSlash(rel))
		if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
			http.ServeFile(w, r, target)
			return true
		}
		return false
	}
	if s.cfg.staticFS == nil {
		return false
	}

	file, info, err := openStaticFSFile(s.cfg.staticFS, rel)
	if err != nil {
		return false
	}
	defer file.Close()

	serveStaticFSFile(w, r, rel, file, info)
	return true
}

func (s *server) readStaticAsset(rel string) ([]byte, error) {
	if s == nil {
		return nil, fs.ErrNotExist
	}
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return nil, fs.ErrNotExist
	}
	if strings.TrimSpace(s.cfg.staticDir) != "" {
		return os.ReadFile(filepath.Join(s.cfg.staticDir, filepath.FromSlash(rel)))
	}
	if s.cfg.staticFS == nil {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(s.cfg.staticFS, rel)
}

func openStaticFSFile(staticFS fs.FS, rel string) (fs.File, fs.FileInfo, error) {
	clean := strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(rel)), "/")
	if clean == "" {
		return nil, nil, fs.ErrNotExist
	}
	file, err := staticFS.Open(clean)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, nil, fs.ErrNotExist
	}
	return file, info, nil
}

func serveStaticFSFile(w http.ResponseWriter, r *http.Request, name string, file fs.File, info fs.FileInfo) {
	if ctype := mime.TypeByExtension(path.Ext(name)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	if rs, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(name), info.ModTime(), rs)
		return
	}
	body, err := io.ReadAll(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), bytes.NewReader(body))
}

func joinBasePath(basePath, suffix string) string {
	basePath = strings.TrimSpace(basePath)
	suffix = strings.TrimSpace(suffix)
	if basePath == "" || basePath == "/" {
		if suffix == "" {
			return "/"
		}
		if strings.HasPrefix(suffix, "/") {
			return suffix
		}
		return "/" + suffix
	}
	if suffix == "" {
		return basePath
	}
	if strings.HasPrefix(suffix, "/") {
		return basePath + suffix
	}
	return basePath + "/" + suffix
}

func displayBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "/"
	}
	return basePath
}

func displayBaseHref(basePath string) string {
	basePath = displayBasePath(basePath)
	if basePath == "/" {
		return "/"
	}
	return strings.TrimRight(basePath, "/") + "/"
}

func (s *server) resolveRuntimeEndpoint(r *http.Request) (runtimeEndpoint, error) {
	if s == nil || r == nil {
		return runtimeEndpoint{}, fmt.Errorf("invalid endpoint")
	}
	ref := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	if ref == "" {
		return runtimeEndpoint{}, fmt.Errorf("missing endpoint")
	}
	endpoint, ok := s.endpointByRef[ref]
	if !ok {
		return runtimeEndpoint{}, fmt.Errorf("invalid endpoint")
	}
	return endpoint, nil
}

func bearerToken(r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(raw) <= len(prefix) {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(raw[:len(prefix)])), []byte(strings.ToLower(prefix))) != 1 {
		return "", false
	}
	token := strings.TrimSpace(raw[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func clientIP(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(remoteAddr); err == nil && strings.TrimSpace(h) != "" {
			return strings.TrimSpace(h)
		}
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	setNoCacheHeaders(w.Header())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func setNoCacheHeaders(h http.Header) {
	if h == nil {
		return
	}
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	h.Set("Vary", "Authorization")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": strings.TrimSpace(msg)})
}
