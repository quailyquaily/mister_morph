//go:build wailsdesktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const desktopLinuxWebviewGPUEnv = "MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY"
const desktopAppBindingPrefix = "main.App."

var desktopRuntimeJavaScript = buildDesktopRuntimeJavaScript(runtime.GOOS, desktopOSVersion())

func buildDesktopRuntimeJavaScript(goos, osVersion string) string {
	version, err := json.Marshal(strings.TrimSpace(desktopVersion))
	if err != nil {
		version = []byte(`"dev"`)
	}
	platform, err := json.Marshal(struct {
		OS      string `json:"os"`
		Version string `json:"version"`
	}{
		OS:      strings.TrimSpace(goos),
		Version: strings.TrimSpace(osVersion),
	})
	if err != nil {
		platform = []byte(`{"os":"","version":""}`)
	}
	return "window.__MISTERMORPH_DESKTOP_RUNTIME__ = true;" +
		"window.__MISTERMORPH_DESKTOP_VERSION__ = " + string(version) + ";" +
		"window.__MISTERMORPH_DESKTOP_PLATFORM__ = " + string(platform) + ";" +
		"window.__MISTERMORPH_DESKTOP_BINDINGS__ = {" +
		`"CheckUpdate":"` + desktopAppBindingPrefix + `CheckUpdate",` +
		`"OpenDesktopLog":"` + desktopAppBindingPrefix + `OpenDesktopLog",` +
		`"OpenWindow":"` + desktopAppBindingPrefix + `OpenWindow",` +
		`"QuitApp":"` + desktopAppBindingPrefix + `QuitApp",` +
		`"ReportFrontendReady":"` + desktopAppBindingPrefix + `ReportFrontendReady",` +
		`"RequestNotificationPermission":"` + desktopAppBindingPrefix + `RequestNotificationPermission",` +
		`"RestartApp":"` + desktopAppBindingPrefix + `RestartApp",` +
		`"ShowNotification":"` + desktopAppBindingPrefix + `ShowNotification"` +
		"};"
}

func desktopOSVersion() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	output, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

const (
	defaultDesktopMainWindowWidth     = 1360
	defaultDesktopMainWindowHeight    = 860
	defaultDesktopMainWindowMinWidth  = 1000
	defaultDesktopMainWindowMinHeight = 680
)

func main() {
	startedAt := time.Now()
	args := os.Args[1:]
	cfgPath, explicit := resolveDesktopConfigPath(args)
	printDesktopConfigPath("desktop app", cfgPath, explicit)

	desktopCfg, desktopCfgErr := loadDesktopRuntimeConfig(cfgPath)
	if hasDesktopCheckUpdateArg(args) {
		if desktopCfgErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "check update failed: %v\n", desktopCfgErr)
			os.Exit(1)
		}
		if err := runDesktopCheckUpdateCommand(context.Background(), desktopCfg, os.Stdout); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "check update failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if desktopCfgErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: load desktop config failed: %v\n", desktopCfgErr)
	}

	logFile, logPath, logErr := openDesktopLogFile()
	if logErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "open desktop log file failed: %v\n", logErr)
		logPath = ""
	}
	if logFile != nil {
		defer logFile.Close()
	}

	host := NewDesktopHost(DesktopHostConfig{
		ConsoleBasePath: defaultConsoleBasePath,
		ConfigPath:      cfgPath,
		LogWriter:       logFile,
	})
	hostStartAt := time.Now()
	startupErr := host.Start(context.Background())
	if startupErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start desktop host: %v\n", startupErr)
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, "failed to start desktop host: %v\n", startupErr)
		}
	} else {
		if logFile != nil {
			_, _ = fmt.Fprintf(
				logFile,
				"desktop_startup_backend_ready duration_ms=%d host_start_ms=%d\n",
				time.Since(startedAt).Milliseconds(),
				time.Since(hostStartAt).Milliseconds(),
			)
		}
		defer host.Stop()
	}

	appBinding := NewApp(host.ConsoleURL(), logPath, startedAt, logFile)
	appBinding.SetAutoUpdateConfig(desktopCfg.AutoUpdate)
	app := application.New(buildDesktopAppOptions(host, appBinding))
	appBinding.Attach(app)
	if startupErr != nil {
		info := classifyDesktopStartupError(startupErr)
		info.LogPath = logPath
		newDesktopStartupErrorWindow(app, info)
	} else {
		newDesktopMainWindow(app, host.ConsoleURL())
		startDesktopAutoUpdateCheck(context.Background(), desktopCfg.AutoUpdate, logFile)
	}

	err := app.Run()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "desktop app exited with error: %v\n", err)
		os.Exit(1)
	}
}

func buildDesktopAppOptions(host *DesktopHost, appBinding *App) application.Options {
	return application.Options{
		Name:        "MisterMorph",
		Description: "MisterMorph Desktop",
		Icon:        desktopAppIconPNG,
		Linux: application.LinuxOptions{
			ProgramName: "com.mistermorph",
		},
		Assets: application.AssetOptions{
			// Linux custom-scheme requests can lose JSON bodies; load the console over
			// the local HTTP host instead of proxying the UI through the asset handler.
			Handler: http.NotFoundHandler(),
		},
		OnShutdown: host.Stop,
		RawMessageHandler: func(window application.Window, message string, _ *application.OriginInfo) {
			appBinding.HandleRawMessage(window, message)
		},
		Services: []application.Service{
			application.NewService(appBinding),
			application.NewService(appBinding.notificationService),
		},
	}
}

func buildDesktopWindowOptions(consoleURL string, savedState *desktopMainWindowState, visibleArea *desktopWindowArea) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Title:              "MisterMorph",
		Width:              defaultDesktopMainWindowWidth,
		Height:             defaultDesktopMainWindowHeight,
		MinWidth:           defaultDesktopMainWindowMinWidth,
		MinHeight:          defaultDesktopMainWindowMinHeight,
		URL:                consoleURL,
		JS:                 desktopRuntimeJavaScript,
		UseApplicationMenu: false,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	}
	if savedState != nil {
		state, ok := normalizeDesktopMainWindowState(*savedState)
		if ok {
			if visibleArea != nil {
				if constrained, constrainedOK := constrainDesktopMainWindowStateToArea(state, *visibleArea); constrainedOK {
					state = constrained
				}
			}
			opts.Width = state.Width
			opts.Height = state.Height
			if opts.MinWidth > opts.Width {
				opts.MinWidth = opts.Width
			}
			if opts.MinHeight > opts.Height {
				opts.MinHeight = opts.Height
			}
			opts.InitialPosition = application.WindowXY
			opts.X = state.X
			opts.Y = state.Y
		}
	}
	return opts
}

func desktopWindowAreaFromScreen(screen *application.Screen) (desktopWindowArea, bool) {
	if screen == nil {
		return desktopWindowArea{}, false
	}
	rects := []application.Rect{
		screen.WorkArea,
		screen.Bounds,
		{
			X:      screen.X,
			Y:      screen.Y,
			Width:  screen.Size.Width,
			Height: screen.Size.Height,
		},
	}
	for _, rect := range rects {
		if rect.Width > 0 && rect.Height > 0 {
			return desktopWindowArea{
				X:      rect.X,
				Y:      rect.Y,
				Width:  rect.Width,
				Height: rect.Height,
			}, true
		}
	}
	return desktopWindowArea{}, false
}

type desktopWindowLifecycleTarget interface {
	Hide() application.Window
	Position() (int, int)
	RegisterHook(events.WindowEventType, func(*application.WindowEvent)) func()
	Size() (int, int)
}

type desktopMainWindowTarget interface {
	desktopWindowLifecycleTarget
	Focus()
	Show() application.Window
}

func newDesktopMainWindow(app *application.App, consoleURL string) {
	statePath, err := desktopMainWindowStatePath()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "resolve desktop window state path failed: %v\n", err)
	}
	savedState, hasSavedState := loadDesktopMainWindowState(statePath)
	var state *desktopMainWindowState
	if hasSavedState {
		state = &savedState
	}

	app.Event.OnApplicationEvent(desktopMainWindowStartupEvent(runtime.GOOS), func(*application.ApplicationEvent) {
		var visibleArea *desktopWindowArea
		if state != nil {
			deadline := time.Now().Add(500 * time.Millisecond)
			for {
				if app.Screen != nil {
					area, ok := desktopWindowAreaFromScreen(app.Screen.GetPrimary())
					if ok {
						visibleArea = &area
						break
					}
				}
				if time.Now().After(deadline) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}

		window := app.Window.NewWithOptions(buildDesktopWindowOptions(consoleURL, state, visibleArea))
		activateDesktopMainWindow(window, statePath, runtime.GOOS, app.Quit)
	})
}

func desktopMainWindowStartupEvent(goos string) events.ApplicationEventType {
	switch goos {
	case "linux":
		return events.Linux.ApplicationStartup
	case "darwin":
		return events.Mac.ApplicationDidFinishLaunching
	case "windows":
		return events.Windows.ApplicationStarted
	default:
		return events.Common.ApplicationStarted
	}
}

func activateDesktopMainWindow(window desktopMainWindowTarget, statePath string, goos string, quit func()) {
	configureDesktopMainWindowLifecycle(window, statePath, goos, quit)
	window.Show()
	window.Focus()
}

func configureDesktopMainWindowLifecycle(window desktopWindowLifecycleTarget, statePath string, goos string, quit func()) {
	configureDesktopMainWindowStatePersistence(window, statePath)
	configureDesktopMainWindowLifecycleForGOOS(window, goos, quit)
}

func configureDesktopMainWindowStatePersistence(window desktopWindowLifecycleTarget, statePath string) {
	if strings.TrimSpace(statePath) == "" {
		return
	}
	saveState := func(*application.WindowEvent) {
		if err := saveDesktopMainWindowStateFromWindow(statePath, window); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "save desktop window state failed: %v\n", err)
		}
	}
	window.RegisterHook(events.Common.WindowDidMove, saveState)
	window.RegisterHook(events.Common.WindowDidResize, saveState)
	window.RegisterHook(events.Common.WindowClosing, saveState)
}

func configureDesktopMainWindowLifecycleForGOOS(window desktopWindowLifecycleTarget, goos string, quit func()) {
	if goos == "darwin" {
		window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			window.Hide()
			event.Cancel()
		})
		return
	}
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quit != nil {
			quit()
		}
	})
}

func resolveLinuxWebviewGPUPolicy() application.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopLinuxWebviewGPUEnv))) {
	case "", "ondemand", "on_demand", "on-demand":
		return application.WebviewGpuPolicyOnDemand
	case "always":
		return application.WebviewGpuPolicyAlways
	case "never", "off", "disabled":
		return application.WebviewGpuPolicyNever
	default:
		return application.WebviewGpuPolicyOnDemand
	}
}
