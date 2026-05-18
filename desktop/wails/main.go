//go:build wailsdesktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const desktopLinuxWebviewGPUEnv = "MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY"
const desktopAppBindingPrefix = "main.App."

var desktopRuntimeJavaScript = buildDesktopRuntimeJavaScript()

func buildDesktopRuntimeJavaScript() string {
	version, err := json.Marshal(strings.TrimSpace(desktopVersion))
	if err != nil {
		version = []byte(`"dev"`)
	}
	return "window.__MISTERMORPH_DESKTOP_RUNTIME__ = true;" +
		"window.__MISTERMORPH_DESKTOP_VERSION__ = " + string(version) + ";" +
		"window.__MISTERMORPH_DESKTOP_BINDINGS__ = {" +
		`"CheckUpdate":"` + desktopAppBindingPrefix + `CheckUpdate",` +
		`"OpenDesktopLog":"` + desktopAppBindingPrefix + `OpenDesktopLog",` +
		`"OpenWindow":"` + desktopAppBindingPrefix + `OpenWindow",` +
		`"QuitApp":"` + desktopAppBindingPrefix + `QuitApp",` +
		`"ReportFrontendReady":"` + desktopAppBindingPrefix + `ReportFrontendReady",` +
		`"RestartApp":"` + desktopAppBindingPrefix + `RestartApp"` +
		"};"
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
			ProgramName: "MisterMorph",
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
		},
	}
}

func buildDesktopWindowOptions(consoleURL string, savedState *desktopMainWindowState) application.WebviewWindowOptions {
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
	configureDesktopMainWindowLifecycle(window, statePath, runtime.GOOS, app.Quit)
	return window
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
