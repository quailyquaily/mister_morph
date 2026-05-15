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

func main() {
	cfgPath, explicit := resolveDesktopConfigPath(os.Args[1:])
	printDesktopConfigPath("desktop app", cfgPath, explicit)

	host := NewDesktopHost(DesktopHostConfig{
		ConsoleBasePath: defaultConsoleBasePath,
		ConfigPath:      cfgPath,
	})
	startupErr := host.Start(context.Background())
	if startupErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start desktop host: %v\n", startupErr)
	} else {
		defer host.Stop()
	}

	appBinding := NewApp(host.ConsoleURL())
	app := application.New(buildDesktopAppOptions(host, appBinding))
	appBinding.Attach(app)
	if startupErr != nil {
		newDesktopStartupErrorWindow(app, classifyDesktopStartupError(startupErr))
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

func buildDesktopWindowOptions(consoleURL string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:     "MisterMorph",
		Width:     1360,
		Height:    860,
		MinWidth:  1000,
		MinHeight: 680,
		URL:       consoleURL,
		JS:        desktopRuntimeJavaScript,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	}
}

type desktopWindowLifecycleTarget interface {
	Hide() application.Window
	RegisterHook(events.WindowEventType, func(*application.WindowEvent)) func()
}

func newDesktopMainWindow(app *application.App, consoleURL string) *application.WebviewWindow {
	window := app.Window.NewWithOptions(buildDesktopWindowOptions(consoleURL))
	configureDesktopMainWindowLifecycle(window)
	return window
}

func configureDesktopMainWindowLifecycle(window desktopWindowLifecycleTarget) {
	configureDesktopMainWindowLifecycleForGOOS(window, runtime.GOOS)
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
