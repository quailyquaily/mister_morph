//go:build wailsdesktop

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const desktopLinuxWebviewGPUEnv = "MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY"
const desktopRuntimeJavaScript = "window.__MISTERMORPH_DESKTOP_RUNTIME__ = true;"

const (
	defaultDesktopMainWindowWidth     = 1360
	defaultDesktopMainWindowHeight    = 860
	defaultDesktopMainWindowMinWidth  = 1000
	defaultDesktopMainWindowMinHeight = 680
)

func main() {
	cfgPath, explicit := resolveDesktopConfigPath(os.Args[1:])
	printDesktopConfigPath("desktop app", cfgPath, explicit)

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
	startupErr := host.Start(context.Background())
	if startupErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start desktop host: %v\n", startupErr)
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, "failed to start desktop host: %v\n", startupErr)
		}
	} else {
		defer host.Stop()
	}

	appBinding := NewApp(host.ConsoleURL(), logPath)
	app := application.New(buildDesktopAppOptions(host, appBinding))
	appBinding.Attach(app)
	if startupErr != nil {
		info := classifyDesktopStartupError(startupErr)
		info.LogPath = logPath
		newDesktopStartupErrorWindow(app, info)
	} else {
		newDesktopMainWindow(app, host.ConsoleURL())
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
			ProgramName: "MisterMorph",
		},
		Assets: application.AssetOptions{
			// Linux custom-scheme requests can lose JSON bodies; load the console over
			// the local HTTP host instead of proxying the UI through the asset handler.
			Handler: http.NotFoundHandler(),
		},
		OnShutdown: host.Stop,
		RawMessageHandler: func(_ application.Window, message string, _ *application.OriginInfo) {
			appBinding.HandleRawMessage(message)
		},
		Services: []application.Service{
			application.NewService(appBinding),
		},
	}
}

func buildDesktopWindowOptions(consoleURL string, savedState *desktopMainWindowState) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Title:     "MisterMorph",
		Width:     defaultDesktopMainWindowWidth,
		Height:    defaultDesktopMainWindowHeight,
		MinWidth:  defaultDesktopMainWindowMinWidth,
		MinHeight: defaultDesktopMainWindowMinHeight,
		URL:       consoleURL,
		JS:        desktopRuntimeJavaScript,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	}
	if savedState != nil {
		state, ok := normalizeDesktopMainWindowState(*savedState)
		if ok {
			opts.Width = state.Width
			opts.Height = state.Height
			opts.InitialPosition = application.WindowXY
			opts.X = state.X
			opts.Y = state.Y
		}
	}
	return opts
}

type desktopWindowLifecycleTarget interface {
	Hide() application.Window
	Position() (int, int)
	RegisterHook(events.WindowEventType, func(*application.WindowEvent)) func()
	Size() (int, int)
}

func newDesktopMainWindow(app *application.App, consoleURL string) *application.WebviewWindow {
	statePath, err := desktopMainWindowStatePath()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "resolve desktop window state path failed: %v\n", err)
	}
	savedState, hasSavedState := loadDesktopMainWindowState(statePath)
	var state *desktopMainWindowState
	if hasSavedState {
		state = &savedState
	}

	window := app.Window.NewWithOptions(buildDesktopWindowOptions(consoleURL, state))
	configureDesktopMainWindowLifecycle(window, statePath)
	return window
}

func configureDesktopMainWindowLifecycle(window desktopWindowLifecycleTarget, statePath string) {
	configureDesktopMainWindowStatePersistence(window, statePath)
	configureDesktopMainWindowLifecycleForGOOS(window, runtime.GOOS)
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

func configureDesktopMainWindowLifecycleForGOOS(window desktopWindowLifecycleTarget, goos string) {
	if goos != "darwin" {
		return
	}
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
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
