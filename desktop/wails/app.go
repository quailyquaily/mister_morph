//go:build wailsdesktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	desktopOpenURLMessagePrefix = "mistermorph:open-url:"
	desktopOpenWindowPrefix     = "mistermorph:open-window:"
	desktopLogMessagePrefix     = "mistermorph:desktop-log:"
	desktopWindowMessagePrefix  = "mistermorph:window-message:"
	desktopHideWindowMessage    = "mistermorph:hide-window"
)

const desktopWindowMessageEvent = "mistermorph:desktop-window-message"

const (
	defaultDesktopChildWindowWidth     = 720
	defaultDesktopChildWindowHeight    = 560
	defaultDesktopChildWindowMinWidth  = 480
	defaultDesktopChildWindowMinHeight = 360
	maxDesktopChildWindowWidth         = 2200
	maxDesktopChildWindowHeight        = 1600
)

type App struct {
	wailsApp      *application.App
	consoleURL    string
	logPath       string
	startedAt     time.Time
	logWriter     io.Writer
	windowParents map[string]string
	autoUpdate    desktopAutoUpdateConfig
	autoUpdateMu  sync.RWMutex
	windowMu      sync.RWMutex
	restartMu     sync.Mutex
	restarting    bool
	readyOnce     sync.Once
}

type DesktopWindowRequest struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MinWidth  int    `json:"min_width"`
	MinHeight int    `json:"min_height"`
	Position  string `json:"position"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
}

type DesktopWindowMessage struct {
	Target     string          `json:"target,omitempty"`
	WindowID   string          `json:"window_id,omitempty"`
	Type       string          `json:"type"`
	RequestID  string          `json:"request_id,omitempty"`
	Source     string          `json:"source,omitempty"`
	DeliveryID string          `json:"_delivery_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func NewApp(consoleURL string, logPath string, startedAt time.Time, logWriter io.Writer) *App {
	return &App{
		consoleURL:    strings.TrimSpace(consoleURL),
		logPath:       strings.TrimSpace(logPath),
		startedAt:     startedAt,
		logWriter:     logWriter,
		windowParents: make(map[string]string),
	}
}

func (a *App) SetAutoUpdateConfig(cfg desktopAutoUpdateConfig) {
	if a == nil {
		return
	}
	a.autoUpdateMu.Lock()
	defer a.autoUpdateMu.Unlock()
	a.autoUpdate = cfg
}

func (a *App) Attach(wailsApp *application.App) {
	a.wailsApp = wailsApp
}

func (a *App) HandleRawMessage(window application.Window, message string) {
	switch {
	case strings.HasPrefix(message, desktopOpenURLMessagePrefix):
		if err := a.OpenExternalURL(message[len(desktopOpenURLMessagePrefix):]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "open external URL failed: %v\n", err)
		}
	case strings.HasPrefix(message, desktopLogMessagePrefix):
		a.logDesktopFrontendMessage(window, strings.TrimPrefix(message, desktopLogMessagePrefix))
	case strings.HasPrefix(message, desktopOpenWindowPrefix):
		req, err := parseDesktopOpenWindowMessage(message)
		a.logDesktopEvent(
			"open_window_request source=%q path=%q title=%q width=%d height=%d",
			desktopWindowName(window),
			req.Path,
			req.Title,
			req.Width,
			req.Height,
		)
		if err == nil {
			err = a.openWindow(req, window)
		}
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "open desktop window failed: %v\n", err)
			if a.logWriter != nil {
				_, _ = fmt.Fprintf(a.logWriter, "open desktop window failed: %v\n", err)
			}
		} else {
			a.logDesktopEvent("open_window_ok source=%q path=%q", desktopWindowName(window), req.Path)
		}
	case strings.HasPrefix(message, desktopWindowMessagePrefix):
		msg, err := parseDesktopWindowMessage(message)
		if err == nil {
			err = a.relayDesktopWindowMessage(window, msg)
		}
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "relay desktop window message failed: %v\n", err)
			if a.logWriter != nil {
				_, _ = fmt.Fprintf(a.logWriter, "relay desktop window message failed: %v\n", err)
			}
		}
	case message == desktopHideWindowMessage:
		if window != nil {
			a.logDesktopEvent("hide_window source=%q", window.Name())
			a.notifyDesktopWindowHidden(window)
			window.Hide()
		}
	}
}

func parseDesktopOpenWindowMessage(message string) (DesktopWindowRequest, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(message, desktopOpenWindowPrefix))
	if raw == "" {
		return DesktopWindowRequest{}, fmt.Errorf("empty desktop window request")
	}
	var req DesktopWindowRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return DesktopWindowRequest{}, fmt.Errorf("parse desktop window request: %w", err)
	}
	return req, nil
}

func parseDesktopWindowMessage(message string) (DesktopWindowMessage, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(message, desktopWindowMessagePrefix))
	if raw == "" {
		return DesktopWindowMessage{}, fmt.Errorf("empty desktop window message")
	}
	var msg DesktopWindowMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return DesktopWindowMessage{}, fmt.Errorf("parse desktop window message: %w", err)
	}
	msg.Target = strings.TrimSpace(msg.Target)
	msg.WindowID = strings.TrimSpace(msg.WindowID)
	msg.Type = strings.TrimSpace(msg.Type)
	msg.RequestID = strings.TrimSpace(msg.RequestID)
	msg.DeliveryID = strings.TrimSpace(msg.DeliveryID)
	msg.Source = ""
	if msg.Type == "" {
		return DesktopWindowMessage{}, fmt.Errorf("empty desktop window message type")
	}
	if msg.Target == "" && msg.WindowID == "" {
		return DesktopWindowMessage{}, fmt.Errorf("empty desktop window message target")
	}
	return msg, nil
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

func (a *App) OpenWindow(req DesktopWindowRequest) (err error) {
	return a.openWindow(req, nil)
}

func (a *App) openWindow(req DesktopWindowRequest, parent application.Window) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("open desktop window panic for path %q: %v", req.Path, recovered)
			_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
			if a.logWriter != nil {
				_, _ = fmt.Fprintf(a.logWriter, "%v\n", err)
			}
		}
	}()
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
	if windowName := desktopChildWindowName(req.Path); windowName != "" {
		if parent != nil {
			a.rememberDesktopWindowParent(windowName, parent.Name())
		}
		if existing, ok := a.wailsApp.Window.GetByName(windowName); ok {
			a.logDesktopEvent("open_window_reuse name=%q parent=%q url=%q", windowName, desktopWindowName(parent), targetURL)
			existing.SetTitle(opts.Title)
			existing.SetURL(targetURL)
			existing.SetResizable(false)
			existing.SetMinSize(opts.MinWidth, opts.MinHeight)
			existing.SetSize(opts.Width, opts.Height)
			existing.Show()
			existing.Focus()
			return nil
		}
		opts.Name = windowName
		window := a.wailsApp.Window.NewWithOptions(opts)
		a.logDesktopEvent("open_window_create name=%q parent=%q url=%q", windowName, desktopWindowName(parent), targetURL)
		window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			event.Cancel()
			a.logDesktopEvent("window_closing_hide name=%q", window.Name())
			a.notifyDesktopWindowHidden(window)
			window.Hide()
		})
		window.Show()
		window.Focus()
		return nil
	}
	window := a.wailsApp.Window.NewWithOptions(opts)
	window.Show()
	window.Focus()
	return nil
}

func (a *App) rememberDesktopWindowParent(childName, parentName string) {
	childName = strings.TrimSpace(childName)
	parentName = strings.TrimSpace(parentName)
	if childName == "" || parentName == "" || childName == parentName {
		return
	}
	a.windowMu.Lock()
	if a.windowParents == nil {
		a.windowParents = make(map[string]string)
	}
	a.windowParents[childName] = parentName
	a.windowMu.Unlock()
	a.logDesktopEvent("remember_parent child=%q parent=%q", childName, parentName)
}

func (a *App) resolveDesktopWindowMessageTarget(sourceName string, msg DesktopWindowMessage) (string, error) {
	target := strings.TrimSpace(msg.Target)
	switch strings.ToLower(target) {
	case "parent":
		a.windowMu.RLock()
		parentName := a.windowParents[sourceName]
		a.windowMu.RUnlock()
		if parentName == "" {
			parentName = a.fallbackDesktopParentWindow(sourceName)
			if parentName != "" {
				a.logDesktopEvent("fallback_parent source=%q parent=%q", sourceName, parentName)
			}
		}
		if parentName == "" {
			return "", fmt.Errorf("parent window not found for %q", sourceName)
		}
		return parentName, nil
	case "self":
		if sourceName == "" {
			return "", fmt.Errorf("source window not found")
		}
		return sourceName, nil
	case "":
		windowName := desktopChildWindowName("/window/" + msg.WindowID)
		if windowName == "" {
			return "", fmt.Errorf("invalid desktop window id %q", msg.WindowID)
		}
		return windowName, nil
	default:
		return target, nil
	}
}

func (a *App) fallbackDesktopParentWindow(sourceName string) string {
	if a == nil || a.wailsApp == nil {
		return ""
	}
	sourceName = strings.TrimSpace(sourceName)
	for _, window := range a.wailsApp.Window.GetAll() {
		if window == nil {
			continue
		}
		name := strings.TrimSpace(window.Name())
		if name == "" || name == sourceName || strings.HasPrefix(name, "mistermorph-window") {
			continue
		}
		return name
	}
	return ""
}

func (a *App) relayDesktopWindowMessage(source application.Window, msg DesktopWindowMessage) error {
	if a.wailsApp == nil {
		return fmt.Errorf("desktop app is not attached")
	}
	if source == nil {
		return fmt.Errorf("source window not found")
	}
	sourceName := source.Name()
	targetName, err := a.resolveDesktopWindowMessageTarget(sourceName, msg)
	if err != nil {
		return err
	}
	target, ok := a.wailsApp.Window.GetByName(targetName)
	if !ok {
		return fmt.Errorf("target window %q not found", targetName)
	}
	msg.Source = sourceName
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal desktop window message: %w", err)
	}
	target.ExecJS(fmt.Sprintf(
		"window.dispatchEvent(new CustomEvent(%s,{detail:%s}));",
		strconv.Quote(desktopWindowMessageEvent),
		string(data),
	))
	a.logDesktopEvent(
		"relay_message source=%q target=%q type=%q target_hint=%q window_id=%q request_id=%q delivery_id=%q payload_len=%d",
		sourceName,
		targetName,
		msg.Type,
		msg.Target,
		msg.WindowID,
		msg.RequestID,
		msg.DeliveryID,
		len(msg.Payload),
	)
	return nil
}

func (a *App) notifyDesktopWindowHidden(window application.Window) {
	if a == nil || a.wailsApp == nil || window == nil {
		return
	}
	sourceName := window.Name()
	a.windowMu.RLock()
	parentName := a.windowParents[sourceName]
	a.windowMu.RUnlock()
	if parentName == "" {
		parentName = a.fallbackDesktopParentWindow(sourceName)
		if parentName != "" {
			a.logDesktopEvent("fallback_parent source=%q parent=%q", sourceName, parentName)
		}
	}
	if parentName == "" {
		a.logDesktopEvent("notify_hidden_skip_parent_missing source=%q", sourceName)
		return
	}
	target, ok := a.wailsApp.Window.GetByName(parentName)
	if !ok {
		a.logDesktopEvent("notify_hidden_skip_parent_window_missing source=%q parent=%q", sourceName, parentName)
		return
	}
	payload, err := json.Marshal(map[string]string{
		"window_id": desktopWindowIDFromName(sourceName),
	})
	if err != nil {
		return
	}
	msg := DesktopWindowMessage{
		Type:     "desktop:window-hidden",
		WindowID: desktopWindowIDFromName(sourceName),
		Source:   sourceName,
		Payload:  payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	target.ExecJS(fmt.Sprintf(
		"window.dispatchEvent(new CustomEvent(%s,{detail:%s}));",
		strconv.Quote(desktopWindowMessageEvent),
		string(data),
	))
	a.logDesktopEvent("notify_hidden source=%q parent=%q window_id=%q", sourceName, parentName, desktopWindowIDFromName(sourceName))
}

func desktopWindowName(window application.Window) string {
	if window == nil {
		return ""
	}
	return strings.TrimSpace(window.Name())
}

func (a *App) logDesktopFrontendMessage(window application.Window, raw string) {
	raw = compactDesktopLogValue(raw, 2000)
	a.logDesktopEvent("frontend source=%q %s", desktopWindowName(window), raw)
}

func (a *App) logDesktopEvent(format string, args ...any) {
	line := "desktop_window " + fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintln(os.Stderr, line)
	if a != nil && a.logWriter != nil {
		_, _ = fmt.Fprintln(a.logWriter, line)
	}
}

func compactDesktopLogValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
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

func (a *App) CheckUpdate() (DesktopUpdateCheckResult, error) {
	autoDownload := false
	if a != nil {
		a.autoUpdateMu.RLock()
		autoDownload = a.autoUpdate.Enabled
		a.autoUpdateMu.RUnlock()
	}
	return checkDesktopUpdate(context.Background(), desktopUpdateCheckOptions{
		AutoDownload:   autoDownload,
		CurrentVersion: desktopVersion,
	})
}

func (a *App) SetAutoUpdateEnabled(enabled bool) bool {
	if a == nil {
		return false
	}
	a.autoUpdateMu.Lock()
	a.autoUpdate.Enabled = enabled
	a.autoUpdateMu.Unlock()
	return true
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

func desktopChildWindowName(rawPath string) string {
	refURL, err := url.Parse(strings.TrimSpace(rawPath))
	if err != nil {
		return ""
	}
	path := refURL.Path
	if path == "/window" {
		return "mistermorph-window"
	}
	if !strings.HasPrefix(path, "/window/") {
		return ""
	}
	segment := strings.TrimPrefix(path, "/window/")
	if index := strings.Index(segment, "/"); index >= 0 {
		segment = segment[:index]
	}
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("mistermorph-window-")
	for _, r := range strings.ToLower(segment) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == len("mistermorph-window-") {
		return ""
	}
	return b.String()
}

func desktopWindowIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "mistermorph-window" {
		return ""
	}
	if !strings.HasPrefix(name, "mistermorph-window-") {
		return ""
	}
	return strings.TrimPrefix(name, "mistermorph-window-")
}

func buildDesktopChildWindowOptions(targetURL string, req DesktopWindowRequest) (application.WebviewWindowOptions, error) {
	title, err := normalizeDesktopWindowTitle(req.Title)
	if err != nil {
		return application.WebviewWindowOptions{}, err
	}
	position, x, y, err := normalizeDesktopWindowPosition(req)
	if err != nil {
		return application.WebviewWindowOptions{}, err
	}
	return application.WebviewWindowOptions{
		Title:              title,
		Width:              clampDesktopWindowDimension(req.Width, defaultDesktopChildWindowWidth, defaultDesktopChildWindowMinWidth, maxDesktopChildWindowWidth),
		Height:             clampDesktopWindowDimension(req.Height, defaultDesktopChildWindowHeight, defaultDesktopChildWindowMinHeight, maxDesktopChildWindowHeight),
		MinWidth:           clampDesktopWindowDimension(req.MinWidth, defaultDesktopChildWindowMinWidth, 320, maxDesktopChildWindowWidth),
		MinHeight:          clampDesktopWindowDimension(req.MinHeight, defaultDesktopChildWindowMinHeight, 240, maxDesktopChildWindowHeight),
		InitialPosition:    position,
		X:                  x,
		Y:                  y,
		URL:                targetURL,
		DisableResize:      true,
		JS:                 desktopRuntimeJavaScript,
		UseApplicationMenu: false,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	}, nil
}

func normalizeDesktopWindowPosition(req DesktopWindowRequest) (application.WindowStartPosition, int, int, error) {
	position := strings.ToLower(strings.TrimSpace(req.Position))
	switch position {
	case "", "center":
		return application.WindowCentered, 0, 0, nil
	case "manual", "xy":
		return application.WindowXY, req.X, req.Y, nil
	default:
		return application.WindowCentered, 0, 0, fmt.Errorf("unsupported desktop window position %q", req.Position)
	}
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
