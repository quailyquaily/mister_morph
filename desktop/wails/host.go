//go:build wailsdesktop

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const (
	defaultConsoleBasePath   = "/"
	defaultStartupTimeout    = 25 * time.Second
	defaultHealthInterval    = 350 * time.Millisecond
	desktopConsoleServeArgV1 = "--desktop-console-serve"
)

type DesktopHostConfig struct {
	ConsoleBasePath    string
	ConsoleStaticDir   string
	ConsoleBinaryPath  string
	ConfigPath         string
	StartupTimeout     time.Duration
	HealthPollInterval time.Duration
}

type DesktopHost struct {
	cfg DesktopHostConfig

	mu         sync.RWMutex
	cmd        *exec.Cmd
	procDone   chan error
	listenAddr string
	proxy      *httputil.ReverseProxy
}

type consoleLauncher struct {
	execPath string
	argsHead []string
	source   string
}

func NewDesktopHost(cfg DesktopHostConfig) *DesktopHost {
	cfg.ConsoleBasePath = normalizeConsoleBasePath(cfg.ConsoleBasePath)
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = defaultStartupTimeout
	}
	if cfg.HealthPollInterval <= 0 {
		cfg.HealthPollInterval = defaultHealthInterval
	}
	return &DesktopHost{cfg: cfg}
}

func (h *DesktopHost) Start(ctx context.Context) error {
	if h == nil {
		return fmt.Errorf("desktop host is nil")
	}

	h.mu.Lock()
	if h.cmd != nil {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	staticDir, err := resolveConsoleStaticDir(h.cfg.ConsoleStaticDir)
	if err != nil {
		return err
	}
	listenAddr, err := reserveLoopbackAddr()
	if err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	launcher, err := h.resolveConsoleLauncher(ctx, exePath)
	if err != nil {
		return err
	}

	args := buildConsoleServeArgs(launcher.argsHead, h.cfg, listenAddr, staticDir)
	cmd := exec.Command(launcher.execPath, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if wd, wdErr := os.Getwd(); wdErr == nil {
		cmd.Dir = wd
	}

	procDone := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start desktop console host: %w", err)
	}
	go func() {
		procDone <- cmd.Wait()
	}()

	target, err := url.Parse("http://" + listenAddr)
	if err != nil {
		_ = stopProcess(cmd, procDone)
		return fmt.Errorf("build console url: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		msg := "desktop host is unavailable"
		if proxyErr != nil {
			msg = "desktop host is unavailable: " + strings.TrimSpace(proxyErr.Error())
		}
		http.Error(w, msg, http.StatusBadGateway)
	}

	h.mu.Lock()
	h.cmd = cmd
	h.procDone = procDone
	h.listenAddr = listenAddr
	h.proxy = proxy
	h.mu.Unlock()

	if err := h.waitUntilReady(ctx, listenAddr, procDone); err != nil {
		h.Stop()
		return err
	}
	return nil
}

func (h *DesktopHost) resolveConsoleLauncher(ctx context.Context, selfExePath string) (consoleLauncher, error) {
	candidates := resolveDesktopBackendCandidates(selfExePath, h.cfg.ConsoleBinaryPath)
	for _, candidate := range candidates {
		if sameExecutablePath(candidate, selfExePath) {
			continue
		}
		if !isExecutableFile(candidate) {
			continue
		}
		return consoleLauncher{
			execPath: candidate,
			argsHead: []string{"console", "serve"},
			source:   "local_binary",
		}, nil
	}

	if desktopBackendAutoDownloadEnabled() {
		version := desktopBackendVersion()
		path, err := downloadMistermorphBinary(ctx, version)
		if err == nil && isExecutableFile(path) {
			return consoleLauncher{
				execPath: path,
				argsHead: []string{"console", "serve"},
				source:   "downloaded_binary",
			}, nil
		}
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: download mistermorph backend failed: %v\n", err)
		}
	}

	if !isExecutableFile(selfExePath) {
		return consoleLauncher{}, fmt.Errorf("desktop executable is not runnable: %s", selfExePath)
	}
	return consoleLauncher{
		execPath: selfExePath,
		argsHead: []string{desktopConsoleServeArgV1},
		source:   "embedded_mode",
	}, nil
}

func sameExecutablePath(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aEval, aErr := filepath.EvalSymlinks(a)
	if aErr == nil {
		a = filepath.Clean(aEval)
	}
	bEval, bErr := filepath.EvalSymlinks(b)
	if bErr == nil {
		b = filepath.Clean(bEval)
	}
	return a == b
}

func (h *DesktopHost) Stop() {
	if h == nil {
		return
	}

	h.mu.Lock()
	cmd := h.cmd
	procDone := h.procDone
	h.cmd = nil
	h.procDone = nil
	h.listenAddr = ""
	h.proxy = nil
	h.mu.Unlock()

	if cmd == nil {
		return
	}
	_ = stopProcess(cmd, procDone)
}

func (h *DesktopHost) ConsoleURL() string {
	if h == nil {
		return ""
	}
	h.mu.RLock()
	addr := strings.TrimSpace(h.listenAddr)
	basePath := h.cfg.ConsoleBasePath
	h.mu.RUnlock()
	if addr == "" {
		return ""
	}
	return "http://" + addr + ensureTrailingSlash(basePath)
}

func (h *DesktopHost) ProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h == nil {
			http.Error(w, "desktop host is unavailable", http.StatusServiceUnavailable)
			return
		}

		h.mu.RLock()
		proxy := h.proxy
		basePath := h.cfg.ConsoleBasePath
		h.mu.RUnlock()

		if proxy == nil {
			http.Error(w, "desktop host is unavailable", http.StatusServiceUnavailable)
			return
		}

		if basePath != "/" && (r.URL.Path == "" || r.URL.Path == "/") {
			http.Redirect(w, r, ensureTrailingSlash(basePath), http.StatusTemporaryRedirect)
			return
		}

		proxy.ServeHTTP(w, r)
	})
}

func (h *DesktopHost) waitUntilReady(ctx context.Context, listenAddr string, procDone <-chan error) error {
	deadline := h.cfg.StartupTimeout
	if deadline <= 0 {
		deadline = defaultStartupTimeout
	}
	pollInterval := h.cfg.HealthPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultHealthInterval
	}

	readyCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	healthURL := "http://" + listenAddr + "/health"
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("desktop console host did not become ready before timeout (%s)", deadline)
		case err := <-procDone:
			if err == nil {
				return fmt.Errorf("desktop console host exited before readiness")
			}
			return fmt.Errorf("desktop console host exited before readiness: %w", err)
		case <-ticker.C:
			resp, err := client.Get(healthURL)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

func buildConsoleServeArgs(argsHead []string, cfg DesktopHostConfig, listenAddr, staticDir string) []string {
	args := make([]string, 0, len(argsHead)+9)
	args = append(args, argsHead...)
	args = append(args,
		"--console-listen", listenAddr,
		"--console-base-path", normalizeConsoleBasePath(cfg.ConsoleBasePath),
		"--console-static-dir", staticDir,
		"--allow-empty-password",
	)
	if cfg.ConfigPath != "" {
		args = append(args, "--config", cfg.ConfigPath)
	}
	return args
}

func resolveConsoleStaticDir(explicit string) (string, error) {
	candidates := make([]string, 0, 8)
	if v := strings.TrimSpace(pathutil.ExpandHomePath(explicit)); v != "" {
		candidates = append(candidates, v)
	}
	if v := strings.TrimSpace(pathutil.ExpandHomePath(os.Getenv("MISTERMORPH_DESKTOP_CONSOLE_STATIC_DIR"))); v != "" {
		candidates = append(candidates, v)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "web", "console", "dist"),
			filepath.Join(wd, "..", "web", "console", "dist"),
		)
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "web", "console", "dist"),
			filepath.Join(exeDir, "..", "web", "console", "dist"),
			filepath.Join(exeDir, "resources", "web", "console", "dist"),
			filepath.Join(exeDir, "..", "Resources", "web", "console", "dist"),
		)
	}

	seen := map[string]struct{}{}
	for _, item := range candidates {
		clean := filepath.Clean(strings.TrimSpace(item))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		if isValidConsoleStaticDir(clean) {
			return clean, nil
		}
	}

	return "", fmt.Errorf("cannot find console static assets directory; set --console-static-dir in DesktopHostConfig or MISTERMORPH_DESKTOP_CONSOLE_STATIC_DIR")
}

func isValidConsoleStaticDir(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	indexPath := filepath.Join(dir, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

func reserveLoopbackAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve loopback listen addr: %w", err)
	}
	defer func() {
		_ = ln.Close()
	}()
	addr := strings.TrimSpace(ln.Addr().String())
	if addr == "" {
		return "", fmt.Errorf("reserve loopback listen addr: empty address")
	}
	return addr, nil
}

func stopProcess(cmd *exec.Cmd, procDone <-chan error) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)
	if procDone != nil {
		select {
		case <-time.After(4 * time.Second):
		case err := <-procDone:
			return err
		}
	}

	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if procDone != nil {
		select {
		case err := <-procDone:
			return err
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func ensureTrailingSlash(basePath string) string {
	basePath = normalizeConsoleBasePath(basePath)
	if strings.HasSuffix(basePath, "/") {
		return basePath
	}
	return basePath + "/"
}

func normalizeConsoleBasePath(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return defaultConsoleBasePath
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return defaultConsoleBasePath
	}
	return base
}

func extractConfigPathFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	for i := 0; i < len(args); i++ {
		item := strings.TrimSpace(args[i])
		if item == "" {
			continue
		}
		if item == "--config" && i+1 < len(args) {
			return strings.TrimSpace(pathutil.ExpandHomePath(args[i+1]))
		}
		if strings.HasPrefix(item, "--config=") {
			return strings.TrimSpace(pathutil.ExpandHomePath(strings.TrimPrefix(item, "--config=")))
		}
	}
	return ""
}
