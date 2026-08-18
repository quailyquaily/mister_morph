//go:build wailsdesktop

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultConsoleBasePath = "/"
	defaultStartupTimeout  = 25 * time.Second
	defaultHealthInterval  = 350 * time.Millisecond
)

type DesktopHostConfig struct {
	ConsoleBasePath    string
	ConsoleBinaryPath  string
	ConfigPath         string
	StartupTimeout     time.Duration
	HealthPollInterval time.Duration
	LogWriter          io.Writer
}

type DesktopHost struct {
	cfg DesktopHostConfig

	mu         sync.RWMutex
	cmd        *exec.Cmd
	procDone   chan error
	listenAddr string
}

type consoleLauncher struct {
	execPath string
	argsHead []string
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

	args := buildConsoleServeArgs(launcher.argsHead, h.cfg, listenAddr)
	_, _ = fmt.Fprintf(h.stderrWriter(), "desktop host launching backend: %s %s\n", launcher.execPath, strings.Join(args, " "))
	cmd := exec.Command(launcher.execPath, args...)
	applyDesktopChildProcessAttrs(cmd)
	cmd.Env = buildDesktopChildEnv(os.Environ())
	cmd.Stdout = h.stdoutWriter()
	cmd.Stderr = h.stderrWriter()
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

	h.mu.Lock()
	h.cmd = cmd
	h.procDone = procDone
	h.listenAddr = listenAddr
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
		}, nil
	}

	if desktopBackendAutoDownloadEnabled() {
		version := desktopBackendVersion()
		path, err := downloadMistermorphBinary(ctx, version)
		if err == nil && isExecutableFile(path) {
			return consoleLauncher{
				execPath: path,
				argsHead: []string{"console", "serve"},
			}, nil
		}
		if err != nil {
			_, _ = fmt.Fprintf(h.stderrWriter(), "warning: download mistermorph backend failed: %v\n", err)
		}
	}

	return consoleLauncher{}, fmt.Errorf("cannot find runnable mistermorph backend binary; set %s or place %s next to the desktop app", desktopBackendBinEnv, desktopBackendBinaryBaseName())
}

func (h *DesktopHost) stdoutWriter() io.Writer {
	if h != nil && h.cfg.LogWriter != nil {
		return io.MultiWriter(os.Stdout, h.cfg.LogWriter)
	}
	return os.Stdout
}

func (h *DesktopHost) stderrWriter() io.Writer {
	if h != nil && h.cfg.LogWriter != nil {
		return io.MultiWriter(os.Stderr, h.cfg.LogWriter)
	}
	return os.Stderr
}

func sameExecutablePath(a, b string) bool {
	a = normalizeDesktopPathCandidate(a)
	b = normalizeDesktopPathCandidate(b)
	if a == "" || b == "" {
		return false
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

func buildConsoleServeArgs(argsHead []string, cfg DesktopHostConfig, listenAddr string) []string {
	args := make([]string, 0, len(argsHead)+7)
	args = append(args, argsHead...)
	args = append(args,
		"--console-listen", listenAddr,
		"--console-base-path", normalizeConsoleBasePath(cfg.ConsoleBasePath),
		"--allow-empty-password",
	)
	if cfg.ConfigPath != "" {
		args = append(args, "--config", cfg.ConfigPath)
	}
	return args
}

func buildDesktopChildEnv(base []string) []string {
	if !desktopChildNeedsSanitizedEnv(base) {
		return append([]string(nil), base...)
	}

	blocked := map[string]struct{}{
		"APPDIR":           {},
		"APPIMAGE":         {},
		"APPDIR_EXEC_PATH": {},
		"ARGV0":            {},
		"OWD":              {},
		"LD_LIBRARY_PATH":  {},
		"LD_PRELOAD":       {},
	}
	out := make([]string, 0, len(base))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, drop := blocked[key]; drop {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func desktopChildNeedsSanitizedEnv(base []string) bool {
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		switch key {
		case "APPDIR", "APPIMAGE":
			return true
		}
	}
	return false
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
