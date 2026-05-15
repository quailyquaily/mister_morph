//go:build wailsdesktop

package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const desktopOpenURLMessagePrefix = "mistermorph:open-url:"

const (
	defaultDesktopChildWindowWidth     = 960
	defaultDesktopChildWindowHeight    = 720
	defaultDesktopChildWindowMinWidth  = 640
	defaultDesktopChildWindowMinHeight = 420
	maxDesktopChildWindowWidth         = 2200
	maxDesktopChildWindowHeight        = 1600
)

type App struct {
	wailsApp   *application.App
	consoleURL string
	logPath    string
	startedAt  time.Time
	logWriter  io.Writer
	restartMu  sync.Mutex
	restarting bool
	readyOnce  sync.Once
}

type DesktopWindowRequest struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MinWidth  int    `json:"min_width"`
	MinHeight int    `json:"min_height"`
}

func NewApp(consoleURL string, logPath string, startedAt time.Time, logWriter io.Writer) *App {
	return &App{
		consoleURL: strings.TrimSpace(consoleURL),
		logPath:    strings.TrimSpace(logPath),
		startedAt:  startedAt,
		logWriter:  logWriter,
	}
}

func (a *App) Attach(wailsApp *application.App) {
	a.wailsApp = wailsApp
}

func (a *App) HandleRawMessage(message string) {
	if !strings.HasPrefix(message, desktopOpenURLMessagePrefix) {
		return
	}

	if err := a.OpenExternalURL(message[len(desktopOpenURLMessagePrefix):]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "open external URL failed: %v\n", err)
	}
}

func (a *App) OpenExternalURL(rawURL string) error {
	target, err := normalizeExternalBrowserURL(rawURL)
	if err != nil {
		return err
	}
	if err := browser.OpenURL(target); err != nil {
		return fmt.Errorf("open URL in browser: %w", err)
	}
	return nil
}

func (a *App) OpenWindow(req DesktopWindowRequest) error {
	if a.wailsApp == nil {
		return fmt.Errorf("desktop app is not attached")
	}
	targetURL, err := resolveDesktopWindowURL(a.consoleURL, req.Path)
	if err != nil {
		return err
	}
	opts, err := buildDesktopChildWindowOptions(targetURL, req)
	if err != nil {
		return err
	}
	a.wailsApp.Window.NewWithOptions(opts).Show()
	return nil
}

func (a *App) QuitApp() {
	if a.wailsApp != nil {
		a.wailsApp.Quit()
	}
}

func (a *App) OpenDesktopLog() error {
	if strings.TrimSpace(a.logPath) == "" {
		return fmt.Errorf("desktop log path is not available")
	}
	if err := browser.OpenFile(a.logPath); err != nil {
		return fmt.Errorf("open desktop log file: %w", err)
	}
	return nil
}

func (a *App) ReportFrontendReady() {
	if a == nil || a.logWriter == nil || a.startedAt.IsZero() {
		return
	}
	a.readyOnce.Do(func() {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		_, _ = fmt.Fprintf(
			a.logWriter,
			"desktop_startup_frontend_ready duration_ms=%d desktop_go_alloc_bytes=%d desktop_go_sys_bytes=%d\n",
			time.Since(a.startedAt).Milliseconds(),
			mem.Alloc,
			mem.Sys,
		)
	})
}

func normalizeExternalBrowserURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty URL")
	}
	for i, r := range rawURL {
		if r < 32 || r == 127 {
			return "", fmt.Errorf("control character at position %d not allowed", i)
		}
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("missing URL host")
	}
	return parsedURL.String(), nil
}

func resolveDesktopWindowURL(consoleURL, rawPath string) (string, error) {
	baseURL, err := normalizeDesktopConsoleURL(consoleURL)
	if err != nil {
		return "", err
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("empty desktop window path")
	}
	for i, r := range rawPath {
		if r < 32 || r == 127 {
			return "", fmt.Errorf("control character at position %d not allowed", i)
		}
	}
	refURL, err := url.Parse(rawPath)
	if err != nil {
		return "", fmt.Errorf("parse desktop window path: %w", err)
	}
	if refURL.IsAbs() || refURL.Host != "" {
		return "", fmt.Errorf("desktop window path must be same-origin")
	}
	if !strings.HasPrefix(refURL.Path, "/") {
		return "", fmt.Errorf("desktop window path must start with /")
	}
	if refURL.Path != "/window" && !strings.HasPrefix(refURL.Path, "/window/") {
		return "", fmt.Errorf("desktop window path must use /window routes")
	}
	if hasDotPathSegment(refURL.Path) {
		return "", fmt.Errorf("desktop window path cannot contain . or .. segments")
	}

	relative := *refURL
	relative.Path = strings.TrimPrefix(refURL.Path, "/")
	if refURL.RawPath != "" {
		relative.RawPath = strings.TrimPrefix(refURL.RawPath, "/")
	}
	target := baseURL.ResolveReference(&relative)
	return target.String(), nil
}

func normalizeDesktopConsoleURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("empty console URL")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse console URL: %w", err)
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported console URL scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return nil, fmt.Errorf("missing console URL host")
	}
	if parsedURL.Path == "" {
		parsedURL.Path = "/"
	}
	if !strings.HasSuffix(parsedURL.Path, "/") {
		parsedURL.Path += "/"
	}
	return parsedURL, nil
}

func hasDotPathSegment(rawPath string) bool {
	for _, segment := range strings.Split(rawPath, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func buildDesktopChildWindowOptions(targetURL string, req DesktopWindowRequest) (application.WebviewWindowOptions, error) {
	title, err := normalizeDesktopWindowTitle(req.Title)
	if err != nil {
		return application.WebviewWindowOptions{}, err
	}
	return application.WebviewWindowOptions{
		Title:              title,
		Width:              clampDesktopWindowDimension(req.Width, defaultDesktopChildWindowWidth, defaultDesktopChildWindowMinWidth, maxDesktopChildWindowWidth),
		Height:             clampDesktopWindowDimension(req.Height, defaultDesktopChildWindowHeight, defaultDesktopChildWindowMinHeight, maxDesktopChildWindowHeight),
		MinWidth:           clampDesktopWindowDimension(req.MinWidth, defaultDesktopChildWindowMinWidth, 320, maxDesktopChildWindowWidth),
		MinHeight:          clampDesktopWindowDimension(req.MinHeight, defaultDesktopChildWindowMinHeight, 240, maxDesktopChildWindowHeight),
		URL:                targetURL,
		JS:                 desktopRuntimeJavaScript,
		UseApplicationMenu: true,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	}, nil
}

func normalizeDesktopWindowTitle(rawTitle string) (string, error) {
	title := strings.TrimSpace(rawTitle)
	if title == "" {
		return "MisterMorph", nil
	}
	for i, r := range title {
		if r < 32 || r == 127 {
			return "", fmt.Errorf("control character at title position %d not allowed", i)
		}
	}
	return title, nil
}

func clampDesktopWindowDimension(value, fallback, minValue, maxValue int) int {
	if value <= 0 {
		return fallback
	}
	return min(max(value, minValue), maxValue)
}

// RestartApp relaunches the current executable and quits the current process.
func (a *App) RestartApp() error {
	a.restartMu.Lock()
	if a.restarting {
		a.restartMu.Unlock()
		return nil
	}
	a.restarting = true
	a.restartMu.Unlock()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if wd, wdErr := os.Getwd(); wdErr == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start new app process: %w", err)
	}

	if a.wailsApp != nil {
		a.wailsApp.Quit()
	}
	return nil
}
